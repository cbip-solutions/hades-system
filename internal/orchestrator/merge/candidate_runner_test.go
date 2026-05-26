package merge_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/merge"
)

func makeCandidateRunner(t *testing.T, pool merge.WorktreePool, exec merge.TestExecutor, em merge.EventEmitter, git merge.GitClient) merge.CandidateRunner {
	t.Helper()
	r, err := merge.NewCandidateRunner(merge.CandidateDeps{
		Pool: pool, Executor: exec, Emitter: em, Git: git,
	}, merge.CandidateConfig{
		Timeout:        30 * time.Second,
		StderrCapBytes: 512,
	})
	if err != nil {
		t.Fatalf("NewCandidateRunner: %v", err)
	}
	return r
}

func makeCand(branch, headSHA string, patch []byte) merge.MergeCandidate {
	return merge.MergeCandidate{
		Branch:       branch,
		HeadSHA:      headSHA,
		Patch:        patch,
		ReviewerVote: 1,
		SubmittedAt:  time.Now(),
	}
}

func TestCandidateRunHappyPath(t *testing.T) {
	pool := &fakeWorktreePool{leaseDir: "/tmp/wt-1"}

	exec := &fakeTestExecutor{
		stdout:   "test_a\ntest_b\ntest_c\n",
		exitCode: 0,
	}
	em := &recordingEmitter{}
	fg := merge.NewFakeGit(merge.FakeOutput{}, merge.FakeOutput{})
	r := makeCandidateRunner(t, pool, exec, em, fg)

	c := makeCand("feat-A", "h1", []byte("--- a/x\n+++ b/x\n@@ -1 +1,2 @@\n a\n+b\n"))
	baseline := merge.PassingSet{"test_a", "test_b", "test_c"}
	suite := merge.TestSuite{
		Smoke: []string{"go", "test", "-tags=smoke"},
		Full:  []string{"go", "test", "./..."},
	}
	out, err := r.Run(context.Background(), c, "deadbeef", baseline, merge.ModeNormal, suite)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.HardRejected {
		t.Error("HardRejected = true on full passing set")
	}
	if out.TestPassCount < 1 {
		t.Errorf("TestPassCount = %d want >=1", out.TestPassCount)
	}
	if out.Candidate.HeadSHA != "h1" {
		t.Errorf("Candidate.HeadSHA = %s want h1", out.Candidate.HeadSHA)
	}
	if out.PatchSizeLines == 0 {
		t.Error("PatchSizeLines = 0 on non-empty patch")
	}
	if out.Duration <= 0 {
		t.Error("Duration = 0; should be measured")
	}
}

func TestCandidateRunHardRejectsBaselineBreaker(t *testing.T) {
	pool := &fakeWorktreePool{leaseDir: "/tmp/wt-1"}

	exec := &fakeTestExecutor{stdout: "test_a\ntest_c\n", exitCode: 0}
	em := &recordingEmitter{}
	fg := merge.NewFakeGit(merge.FakeOutput{}, merge.FakeOutput{})
	r := makeCandidateRunner(t, pool, exec, em, fg)

	c := makeCand("feat-A", "h1", []byte("--- a/x\n+++ b/x\n+changes\n"))
	baseline := merge.PassingSet{"test_a", "test_b", "test_c"}
	suite := merge.TestSuite{
		Smoke: []string{"smoke"},
		Full:  []string{"full"},
	}
	out, err := r.Run(context.Background(), c, "deadbeef", baseline, merge.ModeNormal, suite)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !out.HardRejected {
		t.Error("HardRejected = false; expected true (baseline-breaker)")
	}
	if out.Reason == "" {
		t.Error("Reason empty on HardRejected outcome")
	}
}

func TestCandidateRunPatchRejectedEmitsCandidateFailed(t *testing.T) {
	pool := &fakeWorktreePool{leaseDir: "/tmp/wt-1"}
	exec := &fakeTestExecutor{}
	em := &recordingEmitter{}

	fg := merge.NewFakeGit(
		merge.FakeOutput{},
		merge.FakeOutput{Stderr: "fatal: corrupt patch", Err: errors.New("exit 1")},
	)
	r := makeCandidateRunner(t, pool, exec, em, fg)
	c := makeCand("feat-A", "h1", []byte("garbage"))
	baseline := merge.PassingSet{"x"}
	suite := merge.TestSuite{Smoke: []string{"smoke"}, Full: []string{"full"}}
	out, err := r.Run(context.Background(), c, "abc", baseline, merge.ModeNormal, suite)
	if err != nil {
		t.Fatalf("Run: %v (PatchRejected is non-error outcome path)", err)
	}
	if !out.HardRejected {

		t.Error("HardRejected = false on PatchRejected; expected true")
	}
	snap := em.Snapshot()
	last := snap[len(snap)-1]
	if last.Type != merge.EvtCandidateFailed {
		t.Errorf("last event = %v want EvtCandidateFailed", last.Type)
	}
	var p merge.CandidateFailedPayload
	if err := json.Unmarshal(last.Payload, &p); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if p.FailureType != merge.CandidateFailurePatchRejected.String() {
		t.Errorf("FailureType = %s want PatchRejected", p.FailureType)
	}
}

func TestCandidateRunGitCheckoutFailureSurfacesGitTransient(t *testing.T) {
	pool := &fakeWorktreePool{leaseDir: "/tmp/wt-1"}
	exec := &fakeTestExecutor{}
	em := &recordingEmitter{}
	fg := merge.NewFakeGit(merge.FakeOutput{Stderr: "fatal: bad ref", Err: errors.New("exit 128")})
	r := makeCandidateRunner(t, pool, exec, em, fg)
	c := makeCand("feat-A", "h1", []byte("patch"))
	baseline := merge.PassingSet{"x"}
	suite := merge.TestSuite{Smoke: []string{"s"}, Full: []string{"f"}}
	out, _ := r.Run(context.Background(), c, "abc", baseline, merge.ModeNormal, suite)
	if !out.HardRejected {
		t.Error("HardRejected = false on GitTransient")
	}
	snap := em.Snapshot()
	last := snap[len(snap)-1]
	var p merge.CandidateFailedPayload
	_ = json.Unmarshal(last.Payload, &p)
	if p.FailureType != merge.CandidateFailureGitTransient.String() {
		t.Errorf("FailureType = %s want GitTransient", p.FailureType)
	}
}

func TestCandidateRunReleasesWorktreeOnAllPaths(t *testing.T) {
	cases := []struct {
		name  string
		exec  *fakeTestExecutor
		gitFx []merge.FakeOutput
	}{
		{"happy", &fakeTestExecutor{stdout: "test_a\n", exitCode: 0}, []merge.FakeOutput{{}, {}}},
		{"executor_err", &fakeTestExecutor{err: errors.New("spawn")}, []merge.FakeOutput{{}, {}}},
		{"smoke_fail_no_full", &fakeTestExecutor{exitCode: 1, stderr: "FAIL"}, []merge.FakeOutput{{}, {}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pool := &fakeWorktreePool{leaseDir: "/tmp/wt"}
			em := &recordingEmitter{}
			fg := merge.NewFakeGit(tc.gitFx...)
			r := makeCandidateRunner(t, pool, tc.exec, em, fg)
			c := makeCand("feat-A", "h1", []byte("patch\n"))
			baseline := merge.PassingSet{"test_a"}
			suite := merge.TestSuite{Smoke: []string{"s"}, Full: []string{"f"}}
			_, _ = r.Run(context.Background(), c, "abc", baseline, merge.ModeNormal, suite)
			if l, rel := pool.Stats(); l != rel {
				t.Errorf("[%s] worktree leak: lease=%d release=%d", tc.name, l, rel)
			}
		})
	}
}

func TestCandidateRunSmokeFailureSkipsFull(t *testing.T) {
	pool := &fakeWorktreePool{leaseDir: "/tmp/wt-1"}
	exec := &fakeTestExecutor{exitCode: 1, stderr: "FAIL: smoke"}
	em := &recordingEmitter{}
	fg := merge.NewFakeGit(merge.FakeOutput{}, merge.FakeOutput{})
	r := makeCandidateRunner(t, pool, exec, em, fg)
	c := makeCand("feat-A", "h1", []byte("patch\n"))
	baseline := merge.PassingSet{"test_a"}
	suite := merge.TestSuite{Smoke: []string{"smoke"}, Full: []string{"full"}}
	_, _ = r.Run(context.Background(), c, "abc", baseline, merge.ModeNormal, suite)
	calls := exec.Calls()
	if len(calls) != 1 {
		t.Errorf("calls = %d want 1 (smoke fail must skip full)", len(calls))
	}
}

func TestCandidateRunHonorsContextCancel(t *testing.T) {
	pool := &fakeWorktreePool{leaseDir: "/tmp/wt-1"}
	exec := &fakeTestExecutor{stdout: "test_a", delay: 100 * time.Millisecond}
	em := &recordingEmitter{}
	fg := merge.NewFakeGit(merge.FakeOutput{}, merge.FakeOutput{})
	r := makeCandidateRunner(t, pool, exec, em, fg)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c := makeCand("feat-A", "h1", []byte("patch\n"))
	baseline := merge.PassingSet{"x"}
	suite := merge.TestSuite{Smoke: []string{"s"}, Full: []string{"f"}}
	_, _ = r.Run(ctx, c, "abc", baseline, merge.ModeNormal, suite)

	if l, rel := pool.Stats(); l != rel {
		t.Errorf("ctx-cancel leak: lease=%d release=%d", l, rel)
	}
}

func TestCandidateRunPoolLeaseFailureNoEmit(t *testing.T) {
	pool := &fakeWorktreePool{leaseErr: errors.New("pool exhausted")}
	exec := &fakeTestExecutor{}
	em := &recordingEmitter{}
	fg := merge.NewFakeGit()
	r := makeCandidateRunner(t, pool, exec, em, fg)
	c := makeCand("feat-A", "h1", []byte("p\n"))
	suite := merge.TestSuite{Smoke: []string{"s"}, Full: []string{"f"}}
	_, err := r.Run(context.Background(), c, "abc", merge.PassingSet{"x"}, merge.ModeNormal, suite)
	if err == nil {
		t.Fatal("expected error on pool exhaustion")
	}

	for _, e := range em.Snapshot() {
		if e.Type == merge.EvtCandidateStarted {
			t.Errorf("EvtCandidateStarted emitted on pool failure")
		}
	}
}

func TestNewCandidateRunnerRejectsMissingDeps(t *testing.T) {
	cases := []merge.CandidateDeps{
		{},
		{Pool: &fakeWorktreePool{}},
		{Pool: &fakeWorktreePool{}, Executor: &fakeTestExecutor{}},
	}
	for i, deps := range cases {
		_, err := merge.NewCandidateRunner(deps, merge.CandidateConfig{Timeout: time.Second})
		if err == nil {
			t.Errorf("case %d: NewCandidateRunner accepted incomplete deps", i)
		}
	}
}

func TestNewCandidateRunnerRejectsNilGit(t *testing.T) {
	pool := &fakeWorktreePool{}
	exec := &fakeTestExecutor{}
	em := &recordingEmitter{}
	r, err := merge.NewCandidateRunner(merge.CandidateDeps{
		Pool: pool, Executor: exec, Emitter: em, Git: nil,
	}, merge.CandidateConfig{Timeout: time.Second, StderrCapBytes: 512})
	if err == nil {
		t.Fatal("expected error for nil Git")
	}
	if r != nil {
		t.Errorf("runner = %v want nil", r)
	}
}

func TestNewCandidateRunnerRejectsNegativeStderrCap(t *testing.T) {
	pool := &fakeWorktreePool{}
	exec := &fakeTestExecutor{}
	em := &recordingEmitter{}
	git := merge.NewFakeGit()
	r, err := merge.NewCandidateRunner(merge.CandidateDeps{
		Pool: pool, Executor: exec, Emitter: em, Git: git,
	}, merge.CandidateConfig{Timeout: time.Second, StderrCapBytes: -1})
	if err == nil {
		t.Fatal("expected error for negative StderrCapBytes")
	}
	if r != nil {
		t.Errorf("runner = %v want nil", r)
	}
}

type fullExecutor struct {
	smoke   merge.RunResult
	full    merge.RunResult
	fullErr error
	calls   int
}

func (f *fullExecutor) Run(ctx context.Context, workingDir string, cmd []string, env []string) (merge.RunResult, error) {
	f.calls++
	if f.calls == 1 {
		return f.smoke, nil
	}
	return f.full, f.fullErr
}

func TestCandidateRunFullExecutorErrorEmitsCandidateFailed(t *testing.T) {
	pool := &fakeWorktreePool{leaseDir: "/tmp/wt-1"}
	exec := &fullExecutor{
		smoke:   merge.RunResult{Stdout: "test_a\n", ExitCode: 0},
		full:    merge.RunResult{Stderr: "spawn err", ExitCode: -1},
		fullErr: errors.New("spawn full"),
	}
	em := &recordingEmitter{}
	fg := merge.NewFakeGit(merge.FakeOutput{}, merge.FakeOutput{})
	r := makeCandidateRunner(t, pool, exec, em, fg)
	c := makeCand("feat-A", "h1", []byte("patch\n"))
	baseline := merge.PassingSet{"test_a"}
	suite := merge.TestSuite{Smoke: []string{"s"}, Full: []string{"f"}}
	out, err := r.Run(context.Background(), c, "abc", baseline, merge.ModeNormal, suite)
	if err != nil {
		t.Fatalf("Run: %v (full executor error is non-error outcome path)", err)
	}
	if !out.HardRejected {
		t.Error("HardRejected = false; want true on full executor error")
	}
	if out.Reason == "" || !strings.Contains(out.Reason, "full_executor_error") {
		t.Errorf("Reason = %q want prefix full_executor_error", out.Reason)
	}
	snap := em.Snapshot()
	last := snap[len(snap)-1]
	if last.Type != merge.EvtCandidateFailed {
		t.Errorf("last event = %v want EvtCandidateFailed", last.Type)
	}
}

func TestCandidateRunSmokeOnlyModeSkipsFullExecutorCall(t *testing.T) {
	for _, mode := range []merge.Mode{merge.ModeDegraded80, merge.ModeEmergencyOnly} {
		t.Run(mode.String(), func(t *testing.T) {
			pool := &fakeWorktreePool{leaseDir: "/tmp/wt-1"}
			exec := &fakeTestExecutor{stdout: "test_a\n", exitCode: 0}
			em := &recordingEmitter{}
			fg := merge.NewFakeGit(merge.FakeOutput{}, merge.FakeOutput{})
			r := makeCandidateRunner(t, pool, exec, em, fg)
			c := makeCand("feat-A", "h1", []byte("patch\n"))
			baseline := merge.PassingSet{"test_a"}
			suite := merge.TestSuite{Smoke: []string{"s"}, Full: []string{"f"}}
			out, err := r.Run(context.Background(), c, "abc", baseline, mode, suite)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			calls := exec.Calls()
			if len(calls) != 1 {
				t.Errorf("calls = %d want 1 (smoke-only mode skips full)", len(calls))
			}
			if out.HardRejected {
				t.Error("HardRejected = true on smoke-only success")
			}
		})
	}
}

func TestCandidateRunCountsBaselineMissingTestFails(t *testing.T) {
	pool := &fakeWorktreePool{leaseDir: "/tmp/wt-1"}

	exec := &fullExecutor{
		smoke: merge.RunResult{Stdout: "test_a\ntest_b\n", ExitCode: 0},
		full:  merge.RunResult{Stdout: "test_a\n", ExitCode: 1},
	}
	em := &recordingEmitter{}
	fg := merge.NewFakeGit(merge.FakeOutput{}, merge.FakeOutput{})
	r := makeCandidateRunner(t, pool, exec, em, fg)
	c := makeCand("feat-A", "h1", []byte("patch\n"))
	baseline := merge.PassingSet{"test_a", "test_b", "test_c"}
	suite := merge.TestSuite{Smoke: []string{"s"}, Full: []string{"f"}}
	out, err := r.Run(context.Background(), c, "abc", baseline, merge.ModeNormal, suite)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if out.TestFailCount != 2 {
		t.Errorf("TestFailCount = %d want 2", out.TestFailCount)
	}

	if !out.HardRejected {
		t.Error("HardRejected = false; want true (baseline breaker)")
	}
}

func TestCandidateRunStderrTruncatedTo512Bytes(t *testing.T) {
	pool := &fakeWorktreePool{leaseDir: "/tmp/wt-1"}
	bigStderr := make([]byte, 2000)
	for i := range bigStderr {
		bigStderr[i] = 'X'
	}
	exec := &fakeTestExecutor{exitCode: 1, stderr: string(bigStderr)}
	em := &recordingEmitter{}
	fg := merge.NewFakeGit(merge.FakeOutput{}, merge.FakeOutput{})
	r := makeCandidateRunner(t, pool, exec, em, fg)
	c := makeCand("feat-A", "h1", []byte("patch\n"))
	baseline := merge.PassingSet{"test_a"}
	suite := merge.TestSuite{Smoke: []string{"s"}, Full: []string{"f"}}
	out, _ := r.Run(context.Background(), c, "abc", baseline, merge.ModeNormal, suite)
	if len(out.Stderr) > 512 {
		t.Errorf("Stderr len = %d want <= 512", len(out.Stderr))
	}
	snap := em.Snapshot()
	last := snap[len(snap)-1]
	var p merge.CandidateFailedPayload
	if err := json.Unmarshal(last.Payload, &p); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(p.Stderr) > 512 {
		t.Errorf("payload Stderr len = %d want <= 512", len(p.Stderr))
	}
}

func TestCandidateRunGenIDFromCounterWhenProvided(t *testing.T) {
	pool := &fakeWorktreePool{leaseDir: "/tmp/wt-1"}
	exec := &fakeTestExecutor{stdout: "test_a\n", exitCode: 0}
	em := &recordingEmitter{}
	gc := &merge.GenerationCounter{}
	wantGen := gc.Next()
	fg := merge.NewFakeGit(merge.FakeOutput{}, merge.FakeOutput{})
	r, err := merge.NewCandidateRunner(merge.CandidateDeps{
		Pool: pool, Executor: exec, Emitter: em, Git: fg, GenCtr: gc,
	}, merge.CandidateConfig{Timeout: 30 * time.Second, StderrCapBytes: 512})
	if err != nil {
		t.Fatal(err)
	}
	c := makeCand("feat-A", "h1", []byte("patch\n"))
	baseline := merge.PassingSet{"test_a"}
	suite := merge.TestSuite{Smoke: []string{"s"}, Full: []string{"f"}}
	if _, err := r.Run(context.Background(), c, "abc", baseline, merge.ModeNormal, suite); err != nil {
		t.Fatal(err)
	}
	snap := em.Snapshot()
	for _, e := range snap {
		if e.GenerationID != wantGen {
			t.Errorf("event %v GenerationID = %d want %d", e.Type, e.GenerationID, wantGen)
		}
	}
}

func TestCandidateRunTimeoutClassifiesAsTimeout(t *testing.T) {
	pool := &fakeWorktreePool{leaseDir: "/tmp/wt-1"}
	exec := &fakeTestExecutor{err: context.DeadlineExceeded}
	em := &recordingEmitter{}
	fg := merge.NewFakeGit(merge.FakeOutput{}, merge.FakeOutput{})
	r := makeCandidateRunner(t, pool, exec, em, fg)
	c := makeCand("feat-A", "h1", []byte("patch\n"))
	baseline := merge.PassingSet{"test_a"}
	suite := merge.TestSuite{Smoke: []string{"s"}, Full: []string{"f"}}
	out, err := r.Run(context.Background(), c, "abc", baseline, merge.ModeNormal, suite)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !out.HardRejected {
		t.Error("HardRejected = false on timeout")
	}
	snap := em.Snapshot()
	last := snap[len(snap)-1]
	var p merge.CandidateFailedPayload
	_ = json.Unmarshal(last.Payload, &p)
	if p.FailureType != merge.CandidateFailureTimeout.String() {
		t.Errorf("FailureType = %s want Timeout", p.FailureType)
	}
}

func TestCandidateRunSignalKilledClassifiesAsCrash(t *testing.T) {
	pool := &fakeWorktreePool{leaseDir: "/tmp/wt-1"}
	exec := &fakeTestExecutor{err: errors.New("signal: killed"), exitCode: -1}
	em := &recordingEmitter{}
	fg := merge.NewFakeGit(merge.FakeOutput{}, merge.FakeOutput{})
	r := makeCandidateRunner(t, pool, exec, em, fg)
	c := makeCand("feat-A", "h1", []byte("patch\n"))
	baseline := merge.PassingSet{"test_a"}
	suite := merge.TestSuite{Smoke: []string{"s"}, Full: []string{"f"}}
	out, err := r.Run(context.Background(), c, "abc", baseline, merge.ModeNormal, suite)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !out.HardRejected {
		t.Error("HardRejected = false on signal-killed")
	}
	snap := em.Snapshot()
	last := snap[len(snap)-1]
	var p merge.CandidateFailedPayload
	_ = json.Unmarshal(last.Payload, &p)
	if p.FailureType != merge.CandidateFailureCrash.String() {
		t.Errorf("FailureType = %s want Crash", p.FailureType)
	}
}

type timeoutErr struct{}

func (timeoutErr) Error() string { return "timeout via interface" }
func (timeoutErr) Timeout() bool { return true }

func TestCandidateRunTimeoutInterfaceClassifiesAsTimeout(t *testing.T) {
	pool := &fakeWorktreePool{leaseDir: "/tmp/wt-1"}
	exec := &fakeTestExecutor{err: timeoutErr{}}
	em := &recordingEmitter{}
	fg := merge.NewFakeGit(merge.FakeOutput{}, merge.FakeOutput{})
	r := makeCandidateRunner(t, pool, exec, em, fg)
	c := makeCand("feat-A", "h1", []byte("patch\n"))
	baseline := merge.PassingSet{"test_a"}
	suite := merge.TestSuite{Smoke: []string{"s"}, Full: []string{"f"}}
	_, err := r.Run(context.Background(), c, "abc", baseline, merge.ModeNormal, suite)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	snap := em.Snapshot()
	last := snap[len(snap)-1]
	var p merge.CandidateFailedPayload
	_ = json.Unmarshal(last.Payload, &p)
	if p.FailureType != merge.CandidateFailureTimeout.String() {
		t.Errorf("FailureType = %s want Timeout (via Timeout() interface)", p.FailureType)
	}
}
