//go:build timeaccel

// tests/compliance/inv_zen_108_straggler_grace_test.go
//
// Compliance gate for inv-zen-108: straggler-kill SIGTERM → grace →
// SIGKILL escalation timing.
//
// implements the per-candidate watchdog goroutine that owns the
// grace-window arm/disarm lifecycle. When a candidate's per-candidate
// context fires (deadline, cancel, or parent-ctx propagation), the
// supervisor arms a time.AfterFunc(grace, emit) callback that fires the
// EvtMergeStragglerKilled event with MergeStragglerKilledPayload
// carrying the candidate id, "SIGKILL" canonical signal name, and the
// configured grace period in milliseconds. The contract:
//
//   - The grace window MUST be respected: if the candidate exits BEFORE
//     grace elapses, the supervisor stops the timer and emits no event.
//   - Once grace elapses with the candidate still running, the
//     supervisor MUST emit EvtMergeStragglerKilled within bounded
//     wall-clock latency (no leaks, no permanent hangs).
//
// This compliance gate is the runtime expression of that contract: drive
// a hanging candidate (`ignoreCtxHang`-style: a plain time.Sleep that
// does NOT honour ctx.Done()) through the production runner with a
// 50ms ctx timeout + 30ms grace, and assert EvtMergeStragglerKilled is
// emitted within wall-clock < 5s ceiling. The 5s slack is forensic, not
// performance: the canonical timing is ctx(50ms) + grace(30ms) +
// scheduler jitter (~10s of ms ceiling), so 5s catches catastrophic
// regressions (e.g., a refactor that swapped time.AfterFunc for a
// blocking time.Sleep), not normal jitter.
//
// Build tag: //go:build timeaccel
// This test is timing-sensitive (ctx 50ms + grace 30ms + emit slack).
// Per task instructions and the project's test-tier convention, the
// timeaccel build tag opts the test into the timing-acceleration suite
// (default-skipped in plain `go test`; included via `go test -tags=timeaccel`).
//
// Plan deviation (deterministic hangingRunner): the plan suggested a
// ctx-honouring `select { case <-ctx.Done(): return; case <-time.After(d): ... }`
// runner with a 200ms hang. Under the production runner's defer LIFO
// (close(done) before ccancel), a ctx-honouring runner returns at the
// 50ms ctx-fire boundary; close(done) then runs BEFORE the supervisor's
// first-select observes cctx.Done — making the grace-fire branch racy
// (Go's select picks done OR cctx.Done pseudo-randomly). The
// deterministic alternative used here mirrors the D-3 internal test
// (`ignoreCtxHang: true`): a non-ctx-honouring `time.Sleep(200ms)` that
// emulates a real subprocess that ignores SIGTERM. Under this pattern,
// done stays open for the entire 200ms; the supervisor sees cctx.Done()
// at t=50ms (parent ctx fired), arms AfterFunc(30ms), and emits at
// t≈80ms — well before the candidate's Sleep completes. This is the
// production scenario inv-zen-108 actually guards against.
//
// Reference: docs/superpowers/specs/2026-05-01-zen-swarm-plan-6-merge-engine-design.md §8.3 inv-zen-108
//
// Drift adaptation per task instructions: package compliance (not
// _test) to match the predominant tests/compliance convention. Local
// fakes are c108-prefixed to avoid name collisions with sibling files.
// b7Emitter (declared in inv_zen_105_replay_determinism_test.go) is
// reused — Go same-package rules make this a single shared declaration.
package compliance

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/merge"
)

type complianceHangingRunner struct {
	mu      sync.Mutex
	calls   int
	hangFor time.Duration
}

func (r *complianceHangingRunner) Run(_ context.Context, c merge.MergeCandidate, _ string, _ merge.PassingSet, _ merge.Mode, _ merge.TestSuite) (merge.CandidateOutcome, error) {
	r.mu.Lock()
	r.calls++
	r.mu.Unlock()

	time.Sleep(r.hangFor)

	return merge.CandidateOutcome{Candidate: c, TestPassCount: 0}, nil
}

func (r *complianceHangingRunner) Calls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

func TestInvZen108StragglerGracefulKill(t *testing.T) {
	const (
		ctxTimeout    = 50 * time.Millisecond
		grace         = 30 * time.Millisecond
		candidateHang = 200 * time.Millisecond
		wallClockMax  = 5 * time.Second
	)

	cr := &complianceHangingRunner{hangFor: candidateHang}
	em := &b7Emitter{}
	gc := &merge.GenerationCounter{}

	r, err := merge.NewRunner(merge.RunnerDeps{
		Candidate: cr,
		Emitter:   em,
		GenCtr:    gc,
	}, merge.RunnerConfig{StragglerKillGracePeriod: grace})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	cands := []merge.MergeCandidate{{Branch: "feat-A", HeadSHA: "h108"}}
	suite := merge.TestSuite{Smoke: []string{"smoke"}, Full: []string{"full"}}

	ctx, cancel := context.WithTimeout(context.Background(), ctxTimeout)
	defer cancel()

	rcDone := make(chan struct{})
	start := time.Now()
	go func() {
		defer close(rcDone)
		_, _ = r.RunCandidates(ctx, cands, "abc", merge.PassingSet{}, merge.ModeNormal, suite)
	}()

	deadline := time.Now().Add(wallClockMax)
	var stragglerEvent merge.Event
	sawStraggler := false
poll:
	for time.Now().Before(deadline) {
		for _, ev := range em.Snapshot() {
			if ev.Type == merge.EvtMergeStragglerKilled {
				stragglerEvent = ev
				sawStraggler = true
				break poll
			}
		}
		time.Sleep(5 * time.Millisecond)
	}

	elapsed := time.Since(start)
	if !sawStraggler {

		<-rcDone
		t.Fatalf("EvtMergeStragglerKilled NOT emitted within %v wall-clock ceiling (elapsed=%v)",
			wallClockMax, elapsed)
	}

	if elapsed >= wallClockMax {
		t.Errorf("EvtMergeStragglerKilled emitted but wall-clock elapsed=%v >= %v ceiling", elapsed, wallClockMax)
	}

	var payload merge.MergeStragglerKilledPayload
	if err := json.Unmarshal(stragglerEvent.Payload, &payload); err != nil {
		t.Fatalf("MergeStragglerKilledPayload unmarshal: %v", err)
	}
	if payload.CandidateID != "h108" {
		t.Errorf("payload.CandidateID = %q, want %q", payload.CandidateID, "h108")
	}
	if payload.Signal != "SIGKILL" {
		t.Errorf("payload.Signal = %q, want %q (canonical render per runner.go:328)", payload.Signal, "SIGKILL")
	}
	if payload.GraceMs != grace.Milliseconds() {
		t.Errorf("payload.GraceMs = %d, want %d (inv-zen-111 audit-trail correlate)",
			payload.GraceMs, grace.Milliseconds())
	}

	stragglerCount := 0
	for _, ev := range em.Snapshot() {
		if ev.Type == merge.EvtMergeStragglerKilled {
			stragglerCount++
		}
	}
	if stragglerCount != 1 {
		t.Errorf("EvtMergeStragglerKilled count = %d, want exactly 1 (no double-fires)", stragglerCount)
	}

	<-rcDone
}
