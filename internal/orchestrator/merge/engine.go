// SPDX-License-Identifier: MIT
// Package merge — Phase D Task D-7 (engine pipeline complete).
//
// engine.go ships the MergeEngine SURFACE + the full pipeline body:
//
//	D-4 → MergeEngine interface + Deps + EngineConfig + realEngine + NewEngine.
//	D-5 → Steps 1–3 (Validate + cache lookup + serialize-per-target).
//	D-6 → Steps 4–6 (BaselineRunner.Run + Runner.RunCandidates + Scorer.Rank).
//	D-7 → Steps 7–8 (winner lookup + fast-forward + Cache.Store +
//	      EvtMergeCompleted emit). Pipeline complete after D-7 — Merge()
//	      returns (MergeOutcome, nil) on the success path; no placeholder
//	      tail remains (per CLAUDE.md doctrine §"no stubs / código completo").
//
// Per Plan 6 master plan §"Cross-phase interface vs struct collisions",
// the lowercase narrow consumer interfaces (cacheClient, anomalyClient)
// live HERE — engine.go is the only consumer in this package, so the
// interface definitions sit beside the struct that consumes them.
// Concrete impls (merge.Cache, merge.AnomalyDetector) satisfy
// structurally without an explicit `var _ cacheClient = ...` at the
// impl site.
//
// Boundary note (inv-zen-031 / inv-zen-104): nothing in this file
// imports internal/store. Phase D talks to Plan 5 only via the
// EventEmitter interface declared in events.go.

package merge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

type MergeEngine interface {
	Merge(ctx context.Context, req MergeRequest) (MergeOutcome, error)
}

type cacheClient interface {
	Lookup(req MergeRequest) (MergeOutcome, bool)
	Store(req MergeRequest, outcome MergeOutcome)
}

type anomalyClient interface {
	OnEvent(ctx context.Context, evt Event) error
}

type Deps struct {
	Pool     WorktreePool
	Emitter  EventEmitter
	Clock    AnomalyClock
	Baseline BaselineRunner
	Runner   Runner
	Scorer   Scorer
	Cache    cacheClient
	Anomaly  anomalyClient
	Git      GitClient
	Config   EngineConfig

	BlastRadius BlastRadiusScorer
}

type EngineConfig struct {
	Scoring       ScoringConfig
	EngineVersion string
	PoolCapacity  int
}

// realEngine is the production MergeEngine impl. Three durable fields:
//
//   - deps     — collaborator wiring (immutable after NewEngine).
//   - gen      — per-engine generation counter (atomic.Int64-backed)
//     producing strictly monotonic event GenerationIDs across
//     all Merge() invocations — Plan 5 amendment.proposer
//     relies on the monotonic property when reconstructing
//     causal chains across replay (Q9 D).
//   - targetMu — sync.Map[string]*sync.Mutex providing
//     per-TargetBranch serialization. Two concurrent
//     Merge() calls against the same TargetBranch must
//     run in declared order (spec §4.6 + Q8 D); concurrent
//     calls against DIFFERENT targets MUST run in parallel
//     (no global lock). sync.Map's LoadOrStore is the
//     canonical lock-free init path.
type realEngine struct {
	deps     Deps
	gen      *GenerationCounter
	targetMu sync.Map
}

func NewEngine(deps Deps) (MergeEngine, error) {
	if deps.Pool == nil {
		return nil, fmt.Errorf("merge.NewEngine: Pool nil")
	}
	if deps.Emitter == nil {
		return nil, fmt.Errorf("merge.NewEngine: Emitter nil")
	}
	if deps.Baseline == nil {
		return nil, fmt.Errorf("merge.NewEngine: Baseline nil")
	}
	if deps.Runner == nil {
		return nil, fmt.Errorf("merge.NewEngine: Runner nil")
	}
	if deps.Scorer == nil {
		return nil, fmt.Errorf("merge.NewEngine: Scorer nil")
	}
	if deps.Cache == nil {
		return nil, fmt.Errorf("merge.NewEngine: Cache nil")
	}
	if deps.Git == nil {
		return nil, fmt.Errorf("merge.NewEngine: Git nil")
	}
	if deps.Clock == nil {
		deps.Clock = realClock{}
	}

	return &realEngine{
		deps: deps,
		gen:  &GenerationCounter{},
	}, nil
}

func (e *realEngine) targetMutex(target string) *sync.Mutex {
	mu, _ := e.targetMu.LoadOrStore(target, &sync.Mutex{})
	return mu.(*sync.Mutex)
}

func (e *realEngine) Merge(ctx context.Context, req MergeRequest) (MergeOutcome, error) {

	if err := Validate(ctx, e.deps.Git, e.deps.Config.PoolCapacity, req); err != nil {
		if isCtxCancelErr(err) {
			e.emitMergeFailed(ctx, req, "ctx_cancelled", err.Error())
			return MergeOutcome{Reverted: true}, err
		}
		e.emitMergeFailed(ctx, req, "invalid_request", err.Error())
		return MergeOutcome{}, err
	}

	if req.EngineVersion == "" {
		req.EngineVersion = e.deps.Config.EngineVersion
	}
	requestHash := CacheKey(req)
	gen := e.gen.Next()

	if cached, ok := e.deps.Cache.Lookup(req); ok {
		hitPayload, _ := json.Marshal(MergeCacheHitPayload{
			RequestHash:       requestHash,
			OriginalOutcomeID: cached.Winner.HeadSHA,
		})

		_ = e.deps.Emitter.Append(ctx, Event{
			Type:         EvtMergeCacheHit,
			GenerationID: gen,
			RequestHash:  requestHash,
			Payload:      hitPayload,
			Timestamp:    e.deps.Clock.Now(),
		})
		return cached, nil
	}

	mu := e.targetMutex(req.TargetBranch)
	mu.Lock()
	defer mu.Unlock()

	startedPayload, _ := json.Marshal(MergeStartedWithModePayload{
		RequestHash:    requestHash,
		GenerationID:   gen,
		Mode:           req.Mode.String(),
		TriggerEventID: req.TriggerEventID,
	})
	_ = e.deps.Emitter.Append(ctx, Event{
		Type:         EvtMergeStartedWithMode,
		GenerationID: gen,
		RequestHash:  requestHash,
		Payload:      startedPayload,
		Timestamp:    e.deps.Clock.Now(),
	})

	if ctxErr := ctx.Err(); ctxErr != nil {
		e.emitMergeFailed(ctx, req, "ctx_cancelled", ctxErr.Error())
		return MergeOutcome{Reverted: true}, ctxErr
	}

	passingSet, baseErr := e.deps.Baseline.Run(ctx, req.BaseSHA, req.Mode, req.TestSuite)
	if baseErr != nil {
		if isCtxCancelErr(baseErr) || ctx.Err() != nil {
			e.emitMergeFailed(ctx, req, "ctx_cancelled", baseErr.Error())
			return MergeOutcome{Reverted: true}, baseErr
		}
		e.emitMergeFailed(ctx, req, "baseline_failed", baseErr.Error())
		return MergeOutcome{}, baseErr
	}

	if ctxErr := ctx.Err(); ctxErr != nil {
		e.emitMergeFailed(ctx, req, "ctx_cancelled", ctxErr.Error())
		return MergeOutcome{Reverted: true}, ctxErr
	}

	outcomes, runErr := e.deps.Runner.RunCandidates(ctx, req.Candidates, req.BaseSHA, passingSet, req.Mode, req.TestSuite)
	if runErr != nil {
		if isCtxCancelErr(runErr) || ctx.Err() != nil {
			e.emitMergeFailed(ctx, req, "ctx_cancelled", runErr.Error())
			return MergeOutcome{Reverted: true}, runErr
		}
		e.emitMergeFailed(ctx, req, "all_candidates_failed", runErr.Error())
		return MergeOutcome{}, runErr
	}

	if ctxErr := ctx.Err(); ctxErr != nil {
		e.emitMergeFailed(ctx, req, "ctx_cancelled", ctxErr.Error())
		return MergeOutcome{Reverted: true}, ctxErr
	}

	if e.deps.BlastRadius != nil {
		for i := range outcomes {
			_, files := candidateChangedFiles(outcomes[i].Candidate)
			v, bErr := e.deps.BlastRadius.BlastRadius(ctx, "", nil, files)
			if bErr != nil {
				continue
			}
			outcomes[i].BlastRadius = v.Score
		}
	}

	votes := req.ReviewerVotes
	if votes == nil {
		votes = map[string]int{}
	}
	scoringRes, scErr := e.deps.Scorer.Rank(ctx, outcomes, votes, e.deps.Config.Scoring)
	if scErr != nil {

		if isCtxCancelErr(scErr) || ctx.Err() != nil {
			e.emitMergeFailed(ctx, req, "ctx_cancelled", scErr.Error())
			return MergeOutcome{Reverted: true}, scErr
		}
		e.emitMergeFailed(ctx, req, "all_candidates_failed", scErr.Error())
		return MergeOutcome{}, scErr
	}
	scoringPayload := MarshalScoringComplete(scoringRes, e.deps.Config.Scoring)
	_ = e.deps.Emitter.Append(ctx, Event{
		Type:         EvtScoringComplete,
		GenerationID: gen,
		RequestHash:  requestHash,
		Payload:      scoringPayload,
		Timestamp:    e.deps.Clock.Now(),
	})

	if ctxErr := ctx.Err(); ctxErr != nil {
		e.emitMergeFailed(ctx, req, "ctx_cancelled", ctxErr.Error())
		return MergeOutcome{Reverted: true}, ctxErr
	}

	winner, ok := lookupCandidate(req.Candidates, scoringRes.WinnerID)
	if !ok {
		e.emitMergeFailed(ctx, req, "integration_failed", "winner_id not in candidates: "+scoringRes.WinnerID)
		return MergeOutcome{}, fmt.Errorf("merge.realEngine.Merge: winner_id %q not in candidates", scoringRes.WinnerID)
	}

	var preFFTargetSHA string
	if stdout, _, gitErr := e.deps.Git.Run(ctx, "", "", "rev-parse", "refs/heads/"+req.TargetBranch); gitErr == nil {
		preFFTargetSHA = strings.TrimSpace(stdout)
	}

	if _, _, err := e.deps.Git.Run(ctx, "", "", "update-ref", "refs/heads/"+req.TargetBranch, winner.HeadSHA); err != nil {
		if isCtxCancelErr(err) || ctx.Err() != nil {
			rollbackErr := e.rollbackFastForward(req.TargetBranch, preFFTargetSHA)
			detail := "ctx_cancelled during update-ref: " + err.Error()
			if rollbackErr != nil {
				detail += "; rollback failed: " + rollbackErr.Error()
			}
			e.emitMergeFailed(ctx, req, "ctx_cancelled", detail)
			return MergeOutcome{Reverted: true}, err
		}
		e.emitMergeFailed(ctx, req, "integration_failed", "update-ref: "+err.Error())
		return MergeOutcome{}, fmt.Errorf("merge.realEngine.Merge: update-ref %s: %w", winner.HeadSHA, err)
	}

	// C-1 fix (spec §3.4) — POST-FF ctx-cancel: the FF mutation already
	// landed on refs/heads/<target>. If the caller cancelled the ctx
	// mid-flight (e.g., Plan 5 cost_gating @ 90%+ per spec §3.3), we
	// MUST roll back the FF to honor the spec contract. Best-effort
	// rollback: any rollback error surfaces in the EvtMergeFailed.Detail
	// field but does NOT change the Reverted=true return — the caller's
	// ctx is dead and the operator-facing surface is "we tried".
	if ctxErr := ctx.Err(); ctxErr != nil {
		rollbackErr := e.rollbackFastForward(req.TargetBranch, preFFTargetSHA)
		detail := "ctx_cancelled post-fast-forward"
		if rollbackErr != nil {
			detail += "; rollback failed: " + rollbackErr.Error()
		}
		e.emitMergeFailed(ctx, req, "ctx_cancelled", detail)
		return MergeOutcome{Reverted: true}, ctxErr
	}

	out := MergeOutcome{
		Winner:          winner,
		IntegrationSHA:  winner.HeadSHA,
		TestsPassed:     true,
		ReviewerSummary: summarizeVotes(req.ReviewerVotes),
		AllScores:       scoringRes.AllScores,
		Reverted:        false,
	}
	e.deps.Cache.Store(req, out)

	completedPayload, _ := json.Marshal(MergeCompletedPayload{
		WinnerCandidateID: winner.HeadSHA,
		IntegrationSHA:    winner.HeadSHA,
		RequestHash:       requestHash,
		Outcome:           out,
	})
	_ = e.deps.Emitter.Append(ctx, Event{
		Type:         EvtMergeCompleted,
		GenerationID: gen,
		RequestHash:  requestHash,
		Payload:      completedPayload,
		Timestamp:    e.deps.Clock.Now(),
	})

	return out, nil
}

func (e *realEngine) emitMergeFailed(ctx context.Context, req MergeRequest, reason, detail string) {
	payload, _ := json.Marshal(MergeFailedPayload{
		RequestHash: CacheKey(req),
		Reason:      reason,
		Detail:      detail,
	})
	_ = e.deps.Emitter.Append(ctx, Event{
		Type:         EvtMergeFailed,
		GenerationID: e.gen.Current(),
		Payload:      payload,
		Timestamp:    e.deps.Clock.Now(),
	})
}

func lookupCandidate(candidates []MergeCandidate, headSHA string) (MergeCandidate, bool) {
	for _, c := range candidates {
		if c.HeadSHA == headSHA {
			return c, true
		}
	}
	return MergeCandidate{}, false
}

func isCtxCancelErr(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// rollbackFastForward attempts to revert refs/heads/<target> to its
// pre-FF SHA after a post-FF ctx-cancel forced rollback (spec §3.4).
// If preSHA == "", the target ref didn't exist pre-FF — rollback deletes
// the ref via `git update-ref -d`. Otherwise rollback resets the ref to
// preSHA via `git update-ref refs/heads/<target> <preSHA>`.
//
// Best-effort: rollback errors surface to the caller (Merge() forwards
// them to EvtMergeFailed.Detail) but do NOT change the Reverted=true
// return — by the time rollback runs, the operator-facing contract is
// "we tried", and a failed rollback is an observability + operator-action
// signal rather than a recoverable error.
//
// IMPORTANT rollback uses context.Background() because the caller's
// ctx is dead by definition (this helper only runs after the caller
// cancelled). Threading the cancelled ctx into the rollback subprocess
// would refuse the operation in production (real Git honors ctx) — the
// detached Background() context lets the rollback succeed despite the
// caller-level cancellation. Same pattern as anomaly_test's emit-on-
// cancelled-ctx path.
func (e *realEngine) rollbackFastForward(target, preSHA string) error {
	if preSHA == "" {
		_, _, err := e.deps.Git.Run(context.Background(), "", "", "update-ref", "-d", "refs/heads/"+target)
		return err
	}
	_, _, err := e.deps.Git.Run(context.Background(), "", "", "update-ref", "refs/heads/"+target, preSHA)
	return err
}

func summarizeVotes(votes map[string]int) string {
	if len(votes) == 0 {
		return ""
	}
	keys := make([]string, 0, len(votes))
	for k := range votes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var buf []byte
	for i, k := range keys {
		if i > 0 {
			buf = append(buf, ", "...)
		}
		buf = append(buf, fmt.Sprintf("%s:%+d", k, votes[k])...)
	}
	return string(buf)
}

func candidateChangedFiles(cand MergeCandidate) (changedSymbols []string, changedFiles []string) {
	if len(cand.Patch) == 0 {
		return nil, nil
	}
	seen := make(map[string]bool)
	lines := splitLines(cand.Patch)
	for _, line := range lines {

		if len(line) < 4 || line[0] != '+' || line[1] != '+' || line[2] != '+' || line[3] != ' ' {
			continue
		}
		path := line[4:]

		if len(path) >= 2 && path[0] == 'b' && path[1] == '/' {
			path = path[2:]
		}

		if path == "/dev/null" {
			continue
		}
		if path != "" && !seen[path] {
			seen[path] = true
			changedFiles = append(changedFiles, path)
		}
	}
	return nil, changedFiles
}

func splitLines(b []byte) []string {
	if len(b) == 0 {
		return nil
	}
	s := string(b)

	if len(s) > 0 && s[len(s)-1] == '\n' {
		s = s[:len(s)-1]
	}
	lines := strings.Split(s, "\n")
	return lines
}

var _ MergeEngine = (*realEngine)(nil)
