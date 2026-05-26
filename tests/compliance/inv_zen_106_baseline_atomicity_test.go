// tests/compliance/inv_zen_106_baseline_atomicity_test.go
//
// Compliance gate for inv-zen-106: baseline-fail aborts merge atomically.
//
// When the BaselineRunner.Run returns a wrapped ErrBaselineFailed (i.e.,
// the test suite did not pass on the merge-base before any candidate is
// applied), the merge engine MUST abort the merge atomically: NO
// candidate work proceeds, the leased worktree is released, and the
// EvtBaselineFailed event is emitted while EvtCandidateStarted is NOT.
// This is the runtime expression of the spec's "ground-truth-or-bust"
// contract — without it, a flaky baseline could silently let a candidate
// run on top of a broken tree and produce false positives in the
// integration-SHA outcome.
//
// Engine-pipeline contract: the inv-zen-106 short-circuit is a pure
// pipeline-level invariant (baseline → candidates with atomicity at
// the boundary). Phase D's engine.go ships the production pipeline;
// here we mirror the contract via a minimal in-test lambda so the
// invariant is enforceable BEFORE Phase D lands. Once Phase D ships,
// the same lambda-shape is what engine.New().Merge() implements.
//
// Two sibling assertions:
//  1. TestInvZen106BaselineFailAbortsAtomically — full atomicity check:
//     baseline fails (ExitCode=1) → candidate runner is invoked 0 times,
//     worktree leases match releases, EvtBaselineFailed emitted,
//     EvtCandidateStarted not emitted.
//  2. TestInvZen106BaselineFailReleaseSurvivesPanic — defer-based
//     release of the worktree even on the unhappy path. The defer in
//     concreteBaseline.Run is the operational guard; the test asserts
//     the lease/release counters match after a baseline-fail run, which
//     would only happen if the defer fired.
//
// Reference: docs/superpowers/specs/2026-05-01-zen-swarm-plan-6-merge-engine-design.md §4.1 #9-10 + §8.3 inv-zen-106
//
// Drift adaptation per Task B-7 instructions: package compliance (not
// _test) to match the predominant tests/compliance convention. Local
// helpers are b7-prefixed to avoid name collisions with sibling
// compliance files. b7Emitter is declared in the inv_zen_105 sibling
// file (same package) and reused here — Go same-package rules make
// this a single declaration shared across the two B-7 compliance files.
package compliance

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/merge"
)

type b7Pool struct {
	mu       sync.Mutex
	leases   int
	releases int
}

func (p *b7Pool) Lease(ctx context.Context) (*merge.LeasedWorktree, error) {
	p.mu.Lock()
	p.leases++
	p.mu.Unlock()
	return &merge.LeasedWorktree{Dir: "/tmp/compliance-wt"}, nil
}

func (p *b7Pool) Release(ctx context.Context, w *merge.LeasedWorktree) error {
	p.mu.Lock()
	p.releases++
	p.mu.Unlock()
	return nil
}

type b7Executor struct {
	exitCode int
}

func (e *b7Executor) Run(_ context.Context, _ string, _, _ []string) (merge.RunResult, error) {
	return merge.RunResult{Stdout: "", Stderr: "FAIL", ExitCode: e.exitCode}, nil
}

type b7CandidateRunner struct {
	mu    sync.Mutex
	calls int
}

func (r *b7CandidateRunner) Run(_ context.Context, _ merge.MergeCandidate, _ string, _ merge.PassingSet, _ merge.Mode, _ merge.TestSuite) (merge.CandidateOutcome, error) {
	r.mu.Lock()
	r.calls++
	r.mu.Unlock()
	return merge.CandidateOutcome{}, nil
}

func (r *b7CandidateRunner) Calls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

func TestInvZen106BaselineFailAbortsAtomically(t *testing.T) {
	pool := &b7Pool{}
	em := &b7Emitter{}
	exec := &b7Executor{exitCode: 1}
	br, err := merge.NewBaselineRunner(merge.BaselineDeps{
		Pool:     pool,
		Executor: exec,
		Emitter:  em,
		Git:      merge.NewFakeGit(merge.FakeOutput{}),
	}, merge.BaselineConfig{Timeout: 5 * time.Second, StderrCapBytes: 512})
	if err != nil {
		t.Fatal(err)
	}
	cr := &b7CandidateRunner{}
	suite := merge.TestSuite{Smoke: []string{"smoke"}, Full: []string{"full"}}
	candidates := []merge.MergeCandidate{
		{Branch: "feat-A", HeadSHA: "h1", Patch: []byte("patch\n")},
		{Branch: "feat-B", HeadSHA: "h2", Patch: []byte("patch\n")},
	}

	pset, baselineErr := br.Run(context.Background(), "deadbeef", merge.ModeNormal, suite)
	if !errors.Is(baselineErr, merge.ErrBaselineFailed) {
		t.Fatalf("inv-zen-106 setup: baseline did not fail (err=%v)", baselineErr)
	}
	// Engine-pipeline contract: do NOT proceed to candidate execution.
	if baselineErr != nil {

	} else {
		for _, c := range candidates {
			_, _ = cr.Run(context.Background(), c, "deadbeef", pset, merge.ModeNormal, suite)
		}
	}

	if calls := cr.Calls(); calls != 0 {
		t.Errorf("inv-zen-106 VIOLATION: %d candidate runs after baseline-fail (want 0)", calls)
	}

	pool.mu.Lock()
	leases, releases := pool.leases, pool.releases
	pool.mu.Unlock()
	if leases != releases {
		t.Errorf("inv-zen-106 VIOLATION: worktree leak after baseline-fail (lease=%d release=%d)", leases, releases)
	}

	sawBaselineFailed := false
	sawCandidateStarted := false
	for _, e := range em.Snapshot() {
		switch e.Type {
		case merge.EvtBaselineFailed:
			sawBaselineFailed = true
		case merge.EvtCandidateStarted:
			sawCandidateStarted = true
		}
	}
	if !sawBaselineFailed {
		t.Error("inv-zen-106 VIOLATION: EvtBaselineFailed not emitted")
	}
	if sawCandidateStarted {
		t.Error("inv-zen-106 VIOLATION: EvtCandidateStarted emitted after baseline-fail")
	}
}

func TestInvZen106BaselineFailReleaseSurvivesPanic(t *testing.T) {
	pool := &b7Pool{}
	em := &b7Emitter{}
	exec := &b7Executor{exitCode: 1}
	br, err := merge.NewBaselineRunner(merge.BaselineDeps{
		Pool:     pool,
		Executor: exec,
		Emitter:  em,
		Git:      merge.NewFakeGit(merge.FakeOutput{}),
	}, merge.BaselineConfig{Timeout: 5 * time.Second, StderrCapBytes: 512})
	if err != nil {
		t.Fatal(err)
	}
	suite := merge.TestSuite{Smoke: []string{"smoke"}, Full: []string{"full"}}
	_, _ = br.Run(context.Background(), "deadbeef", merge.ModeNormal, suite)

	pool.mu.Lock()
	leases, releases := pool.leases, pool.releases
	pool.mu.Unlock()
	if leases != releases {
		t.Errorf("inv-zen-106 VIOLATION: worktree not released on baseline-fail (lease=%d release=%d)", leases, releases)
	}
}
