// tests/timeaccel/merge_timeaccel_test.go (Plan 6 Phase E Task E-4).
//
// Time-accelerated tier (//go:build timeaccel) for the Plan 6 merge engine.
// Validates timing-sensitive contracts that the default unit-test pass
// cannot bound deterministically without sleep budgets:
//
//  1. Per-stage timeouts (BaselineConfig.Timeout / CandidateConfig.Timeout
//     surface ErrBaselineFailed / wrap context.DeadlineExceeded paths).
//  2. Flake-rerun budget exhaustion under the canonical Mode taxonomy
//     (ModeNormal=2 → recovers; ModeDegraded80=0 → no rerun).
//  3. inv-zen-108 SIGKILL grace under a hanging candidate (ctx 50ms +
//     grace 30ms → EvtMergeStragglerKilled emitted).
//
// Wiring:
//
//   - tCExec implements merge.TestExecutor with a `scripts []RunResult`
//     stream + an optional per-call `delay`. The flake-rerun cases use
//     scripts to drive the smoke / full / rerun cadence; the timeout +
//     inv-zen-108 cases use `delay` to push beyond the configured budget.
//   - tCEmitter implements merge.EventEmitter with a mutex-guarded slice
//     and Snapshot() defensive copy (mirror of the b7Emitter / replay
//     emitter pattern used in adversarial / replay / compliance tiers).
//   - tCPool implements merge.WorktreePool by returning a stub
//     LeasedWorktree on every Lease and a no-op Release; the production
//     path (Phase D engine) is not exercised here — these tests target
//     the per-candidate runner + per-stage runner contracts directly.
//
// Plan deviation notes:
//
//   - inv-zen-108 (TestTimeaccel_InvZen108StragglerEmittedAfterGrace):
//     the plan template uses a ctx-honoring `tCExec.Run` (`select { case
//     <-ctx.Done(): return; case <-time.After(delay): ... }`). Under the
//     production runner's defer LIFO (close(done) before ccancel), a ctx-
//     honoring exec returns at the ctx-fire boundary; close(done) then
//     runs BEFORE the supervisor's first-select observes cctx.Done —
//     making the grace-fire branch racy (Go's select picks `done` OR
//     `cctx.Done` pseudo-randomly). The same race adaptation was applied
//     in D-8 (`tests/compliance/inv_zen_108_straggler_grace_test.go`) and
//     in the D-3 internal `runner_straggler_test.go`. We mirror that
//     deviation here: `tCExec.Run` honors ctx for the flake-rerun + base-
//     line-timeout cases (so cancellation is observable), and the
//     dedicated `hangingExec` helper does a non-ctx-honoring time.Sleep
//     for the inv-zen-108 case so the supervisor sees a still-running
//     candidate when the grace timer fires.
//
//go:build timeaccel
// +build timeaccel

package timeaccel_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/merge"
)

type tCExec struct {
	mu      sync.Mutex
	calls   int
	scripts []merge.RunResult
	delay   time.Duration
}

func (e *tCExec) Run(ctx context.Context, _ string, _, _ []string) (merge.RunResult, error) {
	e.mu.Lock()
	idx := e.calls
	e.calls++
	delay := e.delay
	e.mu.Unlock()
	if delay > 0 {
		select {
		case <-ctx.Done():
			return merge.RunResult{Stderr: "ctx", ExitCode: 1}, ctx.Err()
		case <-time.After(delay):
		}
	}
	if idx < len(e.scripts) {
		return e.scripts[idx], nil
	}
	return merge.RunResult{}, nil
}

type hangingExec struct {
	hangFor time.Duration
}

func (h *hangingExec) Run(_ context.Context, _ string, _, _ []string) (merge.RunResult, error) {

	time.Sleep(h.hangFor)
	return merge.RunResult{}, nil
}

type tCEmitter struct {
	mu sync.Mutex
	ev []merge.Event
}

func (e *tCEmitter) Append(_ context.Context, ev merge.Event) error {
	e.mu.Lock()
	e.ev = append(e.ev, ev)
	e.mu.Unlock()
	return nil
}

func (e *tCEmitter) Snapshot() []merge.Event {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]merge.Event{}, e.ev...)
}

type tCPool struct{}

func (tCPool) Lease(_ context.Context) (*merge.LeasedWorktree, error) {
	return &merge.LeasedWorktree{Dir: "/tmp/wt"}, nil
}

func (tCPool) Release(_ context.Context, _ *merge.LeasedWorktree) error { return nil }

func TestTimeaccel_FlakeRerunBudgetNormalMode(t *testing.T) {
	exec := &tCExec{
		scripts: []merge.RunResult{
			{Stdout: "test_a\ntest_b\n", ExitCode: 0},
			{Stdout: "test_a\n", ExitCode: 1, Stderr: "FAIL: test_b\n"},
			{Stdout: "test_b\n", ExitCode: 0},
		},
	}
	em := &tCEmitter{}
	cr, err := merge.NewCandidateRunner(merge.CandidateDeps{
		Pool:     tCPool{},
		Executor: exec,
		Emitter:  em,
		Git:      merge.NewFakeGit(merge.FakeOutput{}, merge.FakeOutput{}),
	}, merge.CandidateConfig{Timeout: 30 * time.Second, StderrCapBytes: 512})
	if err != nil {
		t.Fatalf("NewCandidateRunner: %v", err)
	}

	c := merge.MergeCandidate{Branch: "feat-A", HeadSHA: "h1", Patch: []byte("p\n")}
	baseline := merge.PassingSet{"test_a", "test_b"}
	suite := merge.TestSuite{Smoke: []string{"smoke"}, Full: []string{"full"}}

	out, runErr := cr.Run(context.Background(), c, "abc", baseline, merge.ModeNormal, suite)
	if runErr != nil {
		t.Fatalf("CandidateRunner.Run: %v", runErr)
	}
	if out.FlakeCount != 1 {
		t.Errorf("FlakeCount = %d, want 1 (ModeNormal budget=2 → recovers on first rerun)", out.FlakeCount)
	}
	if out.HardRejected {
		t.Errorf("HardRejected = true; want false (post-flake passing set covers baseline)")
	}
}

func TestTimeaccel_FlakeRerunBudgetDegraded80(t *testing.T) {
	exec := &tCExec{
		scripts: []merge.RunResult{
			{Stdout: "test_a\n", ExitCode: 1, Stderr: "FAIL"},
		},
	}
	em := &tCEmitter{}
	cr, err := merge.NewCandidateRunner(merge.CandidateDeps{
		Pool:     tCPool{},
		Executor: exec,
		Emitter:  em,
		Git:      merge.NewFakeGit(merge.FakeOutput{}, merge.FakeOutput{}),
	}, merge.CandidateConfig{Timeout: 30 * time.Second, StderrCapBytes: 512})
	if err != nil {
		t.Fatalf("NewCandidateRunner: %v", err)
	}

	c := merge.MergeCandidate{Branch: "feat-A", HeadSHA: "h1", Patch: []byte("p\n")}
	baseline := merge.PassingSet{"test_a"}
	suite := merge.TestSuite{Smoke: []string{"smoke"}, Full: []string{"full"}}

	out, _ := cr.Run(context.Background(), c, "abc", baseline, merge.ModeDegraded80, suite)
	if out.FlakeCount != 0 {
		t.Errorf("FlakeCount = %d, want 0 (ModeDegraded80 budget=0 → no rerun)", out.FlakeCount)
	}
	if !out.HardRejected {
		t.Errorf("HardRejected = false; want true (smoke_failed → BaselineBreaker)")
	}
}

func TestTimeaccel_BaselineTimeoutSurfacesErrBaselineFailed(t *testing.T) {
	exec := &tCExec{
		scripts: []merge.RunResult{{Stdout: "test_a\n", ExitCode: 0}},
		delay:   100 * time.Millisecond,
	}
	em := &tCEmitter{}
	br, err := merge.NewBaselineRunner(merge.BaselineDeps{
		Pool:     tCPool{},
		Executor: exec,
		Emitter:  em,
		Git:      merge.NewFakeGit(merge.FakeOutput{}),
	}, merge.BaselineConfig{Timeout: 20 * time.Millisecond, StderrCapBytes: 512})
	if err != nil {
		t.Fatalf("NewBaselineRunner: %v", err)
	}

	suite := merge.TestSuite{Smoke: []string{"s"}, Full: []string{"f"}}
	_, runErr := br.Run(context.Background(), "abc", merge.ModeNormal, suite)
	if runErr == nil {
		t.Fatal("expected non-nil error on baseline timeout; got nil")
	}

	if !errorIs(runErr, merge.ErrBaselineFailed) {
		t.Errorf("error %v does not wrap ErrBaselineFailed", runErr)
	}
}

func TestTimeaccel_InvZen108StragglerEmittedAfterGrace(t *testing.T) {
	hanging := &hangingExec{hangFor: 200 * time.Millisecond}
	em := &tCEmitter{}
	cr, err := merge.NewCandidateRunner(merge.CandidateDeps{
		Pool:     tCPool{},
		Executor: hanging,
		Emitter:  em,
		Git:      merge.NewFakeGit(merge.FakeOutput{}, merge.FakeOutput{}),
	}, merge.CandidateConfig{Timeout: 30 * time.Millisecond, StderrCapBytes: 512})
	if err != nil {
		t.Fatalf("NewCandidateRunner: %v", err)
	}

	gc := &merge.GenerationCounter{}
	r, err := merge.NewRunner(merge.RunnerDeps{
		Candidate: cr,
		Emitter:   em,
		GenCtr:    gc,
	}, merge.RunnerConfig{StragglerKillGracePeriod: 30 * time.Millisecond})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	cands := []merge.MergeCandidate{{Branch: "a", HeadSHA: "h1", Patch: []byte("p\n")}}
	suite := merge.TestSuite{Smoke: []string{"s"}, Full: []string{"f"}}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, _ = r.RunCandidates(ctx, cands, "abc", merge.PassingSet{"test_a"}, merge.ModeNormal, suite)

	time.Sleep(250 * time.Millisecond)

	saw := false
	for _, e := range em.Snapshot() {
		if e.Type == merge.EvtMergeStragglerKilled {
			saw = true
			break
		}
	}
	if !saw {
		t.Errorf("EvtMergeStragglerKilled not emitted (expected after ctx 50ms + grace 30ms)")
	}
}

func errorIs(err, target error) bool {
	for ; err != nil; err = unwrap(err) {
		if err == target {
			return true
		}
	}
	return false
}

func unwrap(err error) error {
	type wrapper interface{ Unwrap() error }
	if w, ok := err.(wrapper); ok {
		return w.Unwrap()
	}
	return nil
}
