// SPDX-License-Identifier: MIT
// Functional Majority Voting (FMV) on fix proposals — spec §3.2 step 4 +
// §1 Q8 B (arXiv:2604.15618). FMV is the runtime-test-driven sibling of
// Plurality (voting.go): rather than voting on the textual classification
// label, each candidate fix is APPLIED in an isolated worktree and the
// project's test suite is run; the candidate with the highest pass count
// wins. This is voting on observed behaviour, not on review summaries —
// per Q8 B, a stronger signal than agreement among review prose.
//
// # Algorithm
//
// 1. For each candidate, lease a fresh worktree from the pool.
// 2. ApplyFix the candidate's patch into the worktree.
// 3. Run the project's test suite; collect (pass_count, fail_count).
// 4. Release the worktree.
// 5. Sort by (pass_count desc, supporting_reviewers desc).
// 6. Outcome:
// a. all-failed (no candidate has pass>0 AND every candidate had
// either an apply error, a run error, or fail>0) → ErrFMVAllFailed
// b. true tie at the top (top-two share both pass+supporters) → ErrFMVTie
// c. otherwise → winner
//
// # Disjoint-axis discipline
//
// FMV operates on FixProposal (an applicable patch + review-attribution
// metadata). Plurality operates on Class (a label from a single tactical
// reviewer). They are NOT alternative implementations of the same thing —
// the orchestrator runs Plurality first to decide whether a fix is
// needed at all, and then runs FMV to pick which fix to apply (spec §5.4).
//
// # Audit-trail discipline (context.WithoutCancel)
//
// Both VotingDecisionMade and FMVAllFailed audit emissions use
// context.WithoutCancel(ctx) so cancellation of the FMV.Run caller does
// NOT drop the audit row. The decision actually happened — the audit log
// MUST reflect that even if the caller's ctx fired between Run returning
// and the emit landing. This matches the H-6 escalation.go discipline
// (HandleDisagreement uses context.WithoutCancel(context.Background())
// for the same reason) and recordCompletion. Cancellation guards
// the lease/apply/run loop ABOVE the emit; the emit itself runs on a
// detached audit context.
//
// # Degradation hooks (I-5 + I-7)
//
// I-4 shipped the happy path + ErrPoolExhausted bubble-up + true-tie +
// all-failed. I-5 replaces the bubble-up with degradeMidRun: when
// Pool.Lease returns ErrPoolExhausted mid-FMV, the algorithm picks the
// winner by plurality on SupportingReviewers over the FULL candidate
// set, preserves the partial trace input-order for audit, and emits
// EvtFMVDegradedToPlurality (regardless of whether the plurality
// fallback itself ties — the audit signal is independent of the
// outcome). I-7 ships degradeToPlurality which short-circuits FMV.Run
// at entry on FMVOptions.BudgetMode==BudgetPressure: no lease/apply/
// test fan-out runs at all, and the same EvtFMVDegradedToPlurality
// event family carries reason="cost_pressure" (single typed audit
// surface, structured discrimination via the Reason field).

package hra

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

type FixProposal struct {
	// ID is the candidate's stable identifier (the proposal text or a
	// content-addressed hash supplied by the L2 aggregator). Empty
	// strings are accepted at FMV-level (no validation required for
	// vote correctness); the upstream aggregator MUST emit non-empty
	// IDs to keep VotingDecisionMade.Winner audit-meaningful.
	ID string

	Patch []byte

	SupportingReviewers int
}

type BudgetMode int

const (
	BudgetNormal BudgetMode = 0

	BudgetPressure BudgetMode = 1
)

type Lease interface {
	Path() string

	// Release returns the worktree to the pool. The caller MUST invoke
	// this exactly once. Errors are best-effort observability data —
	// they DO NOT abort FMV.Run.
	Release(ctx context.Context) error
}

// Pool is the narrow lease-allocator FMV consumes. Implementations MUST
// be safe for concurrent use (the I-4 algorithm is sequential — one
// Lease at a time — but I-5+ may parallelise candidate evaluation).
//
// AdaptPool is the canonical wrapper around *worktreepool.Pool;
// hra_test ships a fakePool for unit testing.
type Pool interface {
	Lease(ctx context.Context) (Lease, error)
}

type ApplyEngine interface {
	ApplyFix(ctx context.Context, dir string, c FixProposal) error
}

type TestRunner interface {
	Run(ctx context.Context, dir string) (passCount int, failCount int, err error)
}

type FMVOptions struct {
	BudgetMode BudgetMode
}

type FMVTrace struct {
	Candidate FixProposal

	PassCount int

	FailCount int

	ApplyErr error

	RunErr error
}

type FMVResult struct {
	Winner FixProposal

	Trace []FMVTrace

	// Degraded is true when the algorithm fell back to plurality on
	// SupportingReviewers (release I-5: pool-exhausted; I-7: cost-
	// pressure). False on the happy / tie / all-failed paths. Callers
	// that observe Degraded==true MUST consume Reason for the
	// degradation cause — the audit-event emission is bound to the
	// same Reason value.
	Degraded bool

	Reason string
}

var ErrPoolExhausted = errors.New("hra: FMV worktree pool exhausted")

type FMVDeps struct {
	Pool       Pool
	Apply      ApplyEngine
	TestRunner TestRunner
	EventLog   eventlog.Appender

	Clock clock.Clock

	SessionID string
	ProjectID string
}

type FMV struct {
	pool      Pool
	apply     ApplyEngine
	runner    TestRunner
	emitter   eventlog.Appender
	clk       clock.Clock
	sessionID string
	projectID string
}

// NewFMV constructs an FMV from the supplied dependencies. Panics on
// any nil collaborator OR empty session/project ID — these are
// orchestrator-init contract violations the caller MUST notice
// immediately, not defer to first Run.
func NewFMV(deps FMVDeps) *FMV {
	if deps.Pool == nil {
		panic("hra.NewFMV: nil Pool")
	}
	if deps.Apply == nil {
		panic("hra.NewFMV: nil ApplyEngine")
	}
	if deps.TestRunner == nil {
		panic("hra.NewFMV: nil TestRunner")
	}
	if deps.EventLog == nil {
		panic("hra.NewFMV: nil EventLog")
	}
	if deps.SessionID == "" {
		panic("hra.NewFMV: empty SessionID")
	}
	if deps.ProjectID == "" {
		panic("hra.NewFMV: empty ProjectID")
	}
	clk := deps.Clock
	if clk == nil {
		clk = clock.Real{}
	}
	return &FMV{
		pool:      deps.Pool,
		apply:     deps.Apply,
		runner:    deps.TestRunner,
		emitter:   deps.EventLog,
		clk:       clk,
		sessionID: deps.SessionID,
		projectID: deps.ProjectID,
	}
}

func (f *FMV) Run(ctx context.Context, candidates []FixProposal, opts FMVOptions) (FMVResult, error) {
	if len(candidates) == 0 {
		return FMVResult{}, ErrNoVotes
	}

	if opts.BudgetMode == BudgetPressure {
		return f.degradeToPlurality(ctx, candidates, "cost_pressure")
	}

	trace := make([]FMVTrace, 0, len(candidates))
	for i, c := range candidates {
		ls, err := f.pool.Lease(ctx)
		if err != nil {
			if errors.Is(err, ErrPoolExhausted) {
				// I-5: degrade to plurality on the FULL candidate set
				// (not just the evaluated subset). The partial trace
				// is preserved in input-order for audit. This branch
				// MUST NOT bubble the error up — pool exhaustion is a
				// cost-pressure signal, not a substrate failure, and
				// the orchestrator can still pick a winner from the
				// candidates' SupportingReviewers axis.
				return f.degradeMidRun(ctx, candidates, trace, i, "pool_exhausted")
			}

			return FMVResult{Trace: trace},
				fmt.Errorf("hra: FMV lease candidate %d (%q): %w", i, c.ID, err)
		}

		row := f.evaluateCandidate(ctx, ls, c)
		trace = append(trace, row)
	}

	return f.pickWinner(ctx, trace)
}

func (f *FMV) evaluateCandidate(ctx context.Context, ls Lease, c FixProposal) FMVTrace {
	row := FMVTrace{Candidate: c}
	if applyErr := f.apply.ApplyFix(ctx, ls.Path(), c); applyErr != nil {

		row.ApplyErr = applyErr
	} else {
		pass, fail, runErr := f.runner.Run(ctx, ls.Path())
		row.PassCount = pass
		row.FailCount = fail
		row.RunErr = runErr
	}

	if rel := ls.Release(ctx); rel != nil {
		row.RunErr = errors.Join(row.RunErr, fmt.Errorf("hra: FMV release: %w", rel))
	}
	return row
}

func (f *FMV) pickWinner(ctx context.Context, trace []FMVTrace) (FMVResult, error) {

	totalFailures := 0
	anyPassed := false
	for _, r := range trace {
		totalFailures += r.FailCount
		if r.ApplyErr == nil && r.PassCount > 0 {
			anyPassed = true
		}
	}
	if !anyPassed {
		f.emitAllFailed(ctx, len(trace), totalFailures)
		return FMVResult{Trace: trace}, ErrFMVAllFailed
	}

	sort.SliceStable(trace, func(i, j int) bool {
		if trace[i].PassCount != trace[j].PassCount {
			return trace[i].PassCount > trace[j].PassCount
		}
		return trace[i].Candidate.SupportingReviewers > trace[j].Candidate.SupportingReviewers
	})

	if len(trace) >= 2 {
		top, runnerUp := trace[0], trace[1]

		runnerUpReal := runnerUp.ApplyErr == nil && runnerUp.PassCount > 0
		if runnerUpReal &&
			runnerUp.PassCount == top.PassCount &&
			runnerUp.Candidate.SupportingReviewers == top.Candidate.SupportingReviewers {
			return FMVResult{Trace: trace}, ErrFMVTie
		}
	}

	winner := trace[0]
	f.emitWinner(ctx, winner.Candidate.ID)
	return FMVResult{Winner: winner.Candidate, Trace: trace}, nil
}

func (f *FMV) emitWinner(ctx context.Context, winnerID string) {
	auditCtx := context.WithoutCancel(ctx)
	_, _ = f.emitter.Append(auditCtx, eventlog.Event{
		Type:      eventlog.EvtVotingDecisionMade,
		SessionID: f.sessionID,
		ProjectID: f.projectID,
		Timestamp: f.clk.Now(),
		Payload: map[string]any{
			"mechanism": "fmv",
			"winner":    winnerID,
		},
	})
}

func (f *FMV) emitAllFailed(ctx context.Context, candidateCount, testFailures int) {
	auditCtx := context.WithoutCancel(ctx)
	_, _ = f.emitter.Append(auditCtx, eventlog.Event{
		Type:      eventlog.EvtFMVAllFailed,
		SessionID: f.sessionID,
		ProjectID: f.projectID,
		Timestamp: f.clk.Now(),
		Payload: map[string]any{
			"candidate_count": candidateCount,
			"test_failures":   testFailures,
		},
	})
}

// degradeMidRun is invoked when a Pool.Lease call mid-FMV returns
// ErrPoolExhausted. Per spec §1 Q8 A × Q8 B + spec §3.2, the
// orchestrator falls back to plurality voting on the proposers'
// SupportingReviewers counts: highest agreement wins. A tie on
// agreement at the top of the ranking → ErrFMVTie (caller escalates
// to L3). The partial trace (candidates already evaluated before the
// pool exhaustion) is preserved verbatim — pickByAgreementOnly does
// NOT modify or sort it, because audit consumers rely
// on the trace being the literal sequence of candidates the
// orchestrator was able to evaluate before degradation.
//
// This function MUST always emit EvtFMVDegradedToPlurality, even when
// the plurality fallback itself ties: the audit signal "we degraded"
// is independent of the plurality outcome.
//
// I-7 reuses this surface with reason="cost_pressure" via
// degradeToPlurality, which branches at FMV.Run entry on
// opts.BudgetMode==BudgetPressure (no lease/apply/test cycle runs).
func (f *FMV) degradeMidRun(ctx context.Context, all []FixProposal, partial []FMVTrace, atIdx int, reason string) (FMVResult, error) {
	res, err := f.pickByAgreementOnly(all, partial, reason)
	f.emitDegraded(ctx, reason, len(all), atIdx, res, err)
	return res, err
}

func (f *FMV) degradeToPlurality(ctx context.Context, candidates []FixProposal, reason string) (FMVResult, error) {
	res, err := f.pickByAgreementOnly(candidates, nil, reason)
	f.emitDegraded(ctx, reason, len(candidates), 0, res, err)
	return res, err
}

// pickByAgreementOnly returns the highest-agreement candidate (or
// ErrFMVTie when the top two share the same SupportingReviewers).
// Trace is preserved input-order; the ranked copy is private. all
// MUST be non-empty (caller guarantees: Run rejects len==0 before
// degradation can fire).
func (f *FMV) pickByAgreementOnly(all []FixProposal, partial []FMVTrace, reason string) (FMVResult, error) {
	ranked := make([]FixProposal, len(all))
	copy(ranked, all)
	sort.SliceStable(ranked, func(i, j int) bool {
		return ranked[i].SupportingReviewers > ranked[j].SupportingReviewers
	})
	if len(ranked) >= 2 && ranked[0].SupportingReviewers == ranked[1].SupportingReviewers {
		return FMVResult{Trace: partial, Degraded: true, Reason: reason}, ErrFMVTie
	}
	return FMVResult{
		Winner:   ranked[0],
		Trace:    partial,
		Degraded: true,
		Reason:   reason,
	}, nil
}

// emitDegraded writes the EvtFMVDegradedToPlurality audit row.
// Audit-trail discipline: detached audit context (context.WithoutCancel)
// so a cancellation between the lease error and this emit does NOT
// drop the row.
//
// The Append error is dropped here to keep the algorithm flow narrow
// (mirrors emitWinner / emitAllFailed). If the eventlog ever starts
// returning structured errors that the orchestrator MUST react to,
// this becomes a follow-up wiring change in lockstep with the other
// emit helpers.
//
// Field-name set ("reason", "candidate_count", "completed_count",
// "winner_id", "tie") matches the eventlog.FMVDegradedToPlurality
// typed struct's JSON tags so the durable wire shape round-trips
// identically whether the event was emitted via this map[string]any
// path OR a future typed-Append path.
func (f *FMV) emitDegraded(ctx context.Context, reason string, candidateCount, completed int, res FMVResult, err error) {
	auditCtx := context.WithoutCancel(ctx)
	_, _ = f.emitter.Append(auditCtx, eventlog.Event{
		Type:      eventlog.EvtFMVDegradedToPlurality,
		SessionID: f.sessionID,
		ProjectID: f.projectID,
		Timestamp: f.clk.Now(),
		Payload: map[string]any{
			"reason":          reason,
			"candidate_count": candidateCount,
			"completed_count": completed,
			"winner_id":       res.Winner.ID,
			"tie":             errors.Is(err, ErrFMVTie),
		},
	})
}
