// SPDX-License-Identifier: MIT
// Package merge — Phase D Task D-1.
//
// runner.go ships the Runner type's PUBLIC SURFACE — interface + Deps +
// Config + sentinel + DefaultStragglerKillGracePeriod constant + the
// NewRunner constructor. The goroutine fanout body lands in D-2 (per-
// candidate goroutines + sibling isolation + all-fail aggregation) and
// the straggler-kill supervisor lands in D-3 (inv-zen-108 SIGTERM →
// 30s grace → SIGKILL escalation).
//
// This is an explicit multi-task TDD progression: D-1 ships the surface
// so engine.go (D-4..D-7) can reference the types while D-2/D-3 fill
// the body in the same Phase D.
//
// Layout note: Runner is the per-candidate fanout coordinator that
// engine.go consumes via the lowercase narrow runnerClient interface
// (master plan §"Cross-phase interface vs struct collisions"). Phase D
// engine constructs from this concrete; tests inject fakes.

package merge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

var ErrAllCandidatesFailed = errors.New("merge: all candidates failed")

const DefaultStragglerKillGracePeriod = 30 * time.Second

// Runner is the per-candidate goroutine fanout coordinator. engine.go
// (Phase D pipeline Step 5) calls RunCandidates with the validated
// MergeCandidate slice and the BaselineRunner-derived passingSet; the
// runner spawns one goroutine per candidate, collects outcomes via a
// per-candidate result channel, and returns the slice in input order
// so consumers can correlate by index.
//
// Sibling isolation contract: a panic or err in one candidate goroutine
// MUST NOT cancel sibling candidates. Each goroutine recovers its own
// panic and returns a HardRejected outcome; the parent ctx is the only
// cancellation source that propagates across siblings (operator Ctrl-C
// or engine-level deadline).
//
// All-fail aggregation: if every returned CandidateOutcome has
// HardRejected==true, RunCandidates wraps ErrAllCandidatesFailed via
// %w. Partial-fail returns the per-candidate slice with failed entries
// flagged via HardRejected — scoring (Phase C) filters those before
// ranking.
type Runner interface {
	RunCandidates(ctx context.Context, candidates []MergeCandidate, baseSHA string, passingSet PassingSet, mode Mode, suite TestSuite) ([]CandidateOutcome, error)
}

type RunnerDeps struct {
	Candidate CandidateRunner
	Emitter   EventEmitter
	GenCtr    *GenerationCounter
	Clock     AnomalyClock
}

type RunnerConfig struct {
	StragglerKillGracePeriod time.Duration
}

type runner struct {
	deps  RunnerDeps
	cfg   RunnerConfig
	clock AnomalyClock
}

func NewRunner(deps RunnerDeps, cfg RunnerConfig) (Runner, error) {
	if deps.Candidate == nil {
		return nil, fmt.Errorf("merge.NewRunner: Candidate nil")
	}
	if deps.Emitter == nil {
		return nil, fmt.Errorf("merge.NewRunner: Emitter nil")
	}
	if cfg.StragglerKillGracePeriod < 0 {
		return nil, fmt.Errorf("merge.NewRunner: StragglerKillGracePeriod negative: %v", cfg.StragglerKillGracePeriod)
	}
	if cfg.StragglerKillGracePeriod == 0 {
		cfg.StragglerKillGracePeriod = DefaultStragglerKillGracePeriod
	}
	clk := deps.Clock
	if clk == nil {
		clk = realClock{}
	}
	return &runner{deps: deps, cfg: cfg, clock: clk}, nil
}

// RunCandidates is the public fanout entry point. Spawns one goroutine per
// candidate, recovers per-goroutine panics into HardRejected outcomes (so
// sibling failures DO NOT abort sibling work), and aggregates an all-fail
// terminal event + sentinel-wrapped error when no survivors remain.
//
// Contract details (Q6 C + Q8 D):
//
//   - Empty input: returns (nil, error). Caller bug; phase D engine.go
//     pre-validates 1..5 candidates per spec §4.1, this is defense-in-depth.
//   - outcomes preserve input order (outcomes[i] aligns with candidates[i])
//     so Phase C scoring can correlate by index against MergeRequest.Candidates.
//   - Per-candidate context.WithCancel(ctx) gives the D-3 supervisor a
//     handle to cancel a single candidate without disturbing siblings.
//     Parent ctx cancellation still propagates (operator Ctrl-C / engine
//     deadline) — that is the intentional cross-sibling cancellation path.
//   - Per-candidate `done` channel is closed once Run returns; D-3's
//     straggler supervisor consumes it as the stop signal so it does not
//     fire SIGKILL escalation against a goroutine that completed normally.
//   - On goroutine-level panic: outcomes[idx] is set to HardRejected with
//     Reason="runner_panic: <recovered>". The recover keeps siblings alive
//     (inv-zen-005 sibling isolation contract).
//   - On Candidate.Run returning err: outcomes[idx].HardRejected = true and
//     Reason = "runner_err: " + err.Error() (only if Reason was empty —
//     respect any reason the runner already populated for diagnostics).
//   - All-fail aggregation: when no outcome has HardRejected==false,
//     emit EvtMergeAllCandidatesFailed (best-effort; emit-error is
//     observability, never returned), then wrap ErrAllCandidatesFailed via
//     %w with "%d/%d candidates failed" tail so subscribers see both the
//     sentinel root AND the contextual detail via errors.Unwrap.
func (r *runner) RunCandidates(ctx context.Context, candidates []MergeCandidate, baseSHA string, passingSet PassingSet, mode Mode, suite TestSuite) ([]CandidateOutcome, error) {
	if len(candidates) == 0 {
		return nil, fmt.Errorf("merge.runner.RunCandidates: empty candidate slice")
	}

	outcomes := make([]CandidateOutcome, len(candidates))
	var wg sync.WaitGroup

	for i, c := range candidates {
		wg.Add(1)
		go func(idx int, cand MergeCandidate) {
			defer wg.Done()
			// Sibling isolation (inv-zen-005 / Q8 D): a panic in this
			// goroutine MUST surface as a HardRejected outcome rather
			// than crashing the runner or aborting siblings.
			defer func() {
				if rec := recover(); rec != nil {
					outcomes[idx] = CandidateOutcome{
						Candidate:    cand,
						HardRejected: true,
						Reason:       fmt.Sprintf("runner_panic: %v", rec),
					}
				}
			}()

			cctx, ccancel := context.WithCancel(ctx)
			defer ccancel()

			done := make(chan struct{})
			defer close(done)
			r.armStragglerSupervisor(cctx, cand, done)

			out, err := r.deps.Candidate.Run(cctx, cand, baseSHA, passingSet, mode, suite)

			if err != nil {

				if out.Candidate.HeadSHA == "" {
					out.Candidate = cand
				}
				out.HardRejected = true
				if out.Reason == "" {
					out.Reason = "runner_err: " + err.Error()
				}
			}
			outcomes[idx] = out
		}(i, c)
	}

	wg.Wait()

	survivors := 0
	failures := make([]CandidateFailure, 0)
	for _, o := range outcomes {
		if !o.HardRejected {
			survivors++
			continue
		}

		failures = append(failures, CandidateFailure{
			CandidateID: o.Candidate.HeadSHA,
			TypeStr:     "Crash",
			Reason:      o.Reason,
			Stderr:      o.Stderr,
		})
	}

	if survivors == 0 {

		payload, _ := json.Marshal(MergeAllCandidatesFailedPayload{
			CandidateFailures: failures,
		})
		_ = r.deps.Emitter.Append(ctx, Event{
			Type:         EvtMergeAllCandidatesFailed,
			GenerationID: r.genID(),
			Payload:      payload,
			Timestamp:    r.clock.Now(),
		})
		return outcomes, fmt.Errorf("%w: %d/%d candidates failed", ErrAllCandidatesFailed, len(candidates), len(candidates))
	}
	return outcomes, nil
}

func (r *runner) genID() int64 {
	if r.deps.GenCtr == nil {
		return 0
	}
	return r.deps.GenCtr.Current()
}

func (r *runner) armStragglerSupervisor(cctx context.Context, cand MergeCandidate, done <-chan struct{}) {
	go func() {
		select {
		case <-done:

			return
		case <-cctx.Done():

		}
		grace := r.cfg.StragglerKillGracePeriod
		timer := time.AfterFunc(grace, func() {
			payload, _ := json.Marshal(MergeStragglerKilledPayload{
				CandidateID: cand.HeadSHA,
				Signal:      "SIGKILL",
				GraceMs:     grace.Milliseconds(),
			})

			_ = r.deps.Emitter.Append(context.Background(), Event{
				Type:         EvtMergeStragglerKilled,
				GenerationID: r.genID(),
				Payload:      payload,
				Timestamp:    r.clock.Now(),
			})
		})

		select {
		case <-done:
			timer.Stop()
		case <-time.After(grace + 10*time.Millisecond):
			timer.Stop()
		}
	}()
}

var _ Runner = (*runner)(nil)
