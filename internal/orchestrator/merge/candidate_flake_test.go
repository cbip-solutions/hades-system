package merge_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/merge"
)

type scriptedExecutor struct {
	mu      sync.Mutex
	calls   []fakeTestCall
	scripts []merge.RunResult
	errs    []error
}

func (s *scriptedExecutor) Run(_ context.Context, dir string, cmd, env []string) (merge.RunResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, fakeTestCall{WorkingDir: dir, Cmd: cmd, Env: env})
	if len(s.scripts) == 0 {
		return merge.RunResult{}, nil
	}
	r := s.scripts[0]
	s.scripts = s.scripts[1:]
	var err error
	if len(s.errs) > 0 {
		err = s.errs[0]
		s.errs = s.errs[1:]
	}
	return r, err
}

func (s *scriptedExecutor) Calls() []fakeTestCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]fakeTestCall, len(s.calls))
	copy(out, s.calls)
	return out
}

func TestCandidateFlakeRerunRecoversFailingTest(t *testing.T) {
	exec := &scriptedExecutor{
		scripts: []merge.RunResult{
			{Stdout: "test_a\ntest_b\n", ExitCode: 0},
			{Stdout: "test_a\n", ExitCode: 1, Stderr: "FAIL"},
			{Stdout: "test_b\n", ExitCode: 0},
		},
	}
	pool := &fakeWorktreePool{leaseDir: "/tmp/wt"}
	em := &recordingEmitter{}
	fg := merge.NewFakeGit(merge.FakeOutput{}, merge.FakeOutput{})
	r := makeCandidateRunner(t, pool, exec, em, fg)

	c := makeCand("feat-A", "h1", []byte("patch\n"))
	baseline := merge.PassingSet{"test_a", "test_b"}
	suite := merge.TestSuite{Smoke: []string{"smoke"}, Full: []string{"full"}}
	out, err := r.Run(context.Background(), c, "abc", baseline, merge.ModeNormal, suite)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.FlakeCount != 1 {
		t.Errorf("FlakeCount = %d want 1", out.FlakeCount)
	}
	if out.HardRejected {
		t.Error("HardRejected = true after flake recovered baseline_breaker")
	}
	if !out.PassingSet.Has("test_b") {
		t.Error("PassingSet missing test_b after flake-rerun recovery")
	}
	if out.Reason != "" {
		t.Errorf("Reason = %q want empty (baseline_breaker should clear post-flake)", out.Reason)
	}
}

func TestCandidateFlakeRerunRespectsBudget(t *testing.T) {
	exec := &scriptedExecutor{
		scripts: []merge.RunResult{
			{Stdout: "test_a\n", ExitCode: 1, Stderr: "FAIL"},
		},
	}
	pool := &fakeWorktreePool{leaseDir: "/tmp/wt"}
	em := &recordingEmitter{}
	fg := merge.NewFakeGit(merge.FakeOutput{}, merge.FakeOutput{})
	r := makeCandidateRunner(t, pool, exec, em, fg)

	c := makeCand("feat-A", "h1", []byte("p\n"))
	baseline := merge.PassingSet{"test_a"}
	suite := merge.TestSuite{Smoke: []string{"smoke"}, Full: []string{"full"}}
	out, _ := r.Run(context.Background(), c, "abc", baseline, merge.ModeDegraded80, suite)
	if out.FlakeCount != 0 {
		t.Errorf("FlakeCount = %d want 0 (Degraded80 budget=0)", out.FlakeCount)
	}
	if len(exec.Calls()) > 1 {
		t.Errorf("Degraded80 invoked %d test calls; expected 1 (no rerun)", len(exec.Calls()))
	}
}

func TestCandidateFlakeRerunPersistentFailure(t *testing.T) {
	exec := &scriptedExecutor{
		scripts: []merge.RunResult{
			{Stdout: "test_a\ntest_b\n", ExitCode: 0},
			{Stdout: "test_a\n", ExitCode: 1, Stderr: "FAIL"},
			{Stdout: "", ExitCode: 1, Stderr: "FAIL"},
			{Stdout: "", ExitCode: 1, Stderr: "FAIL"},
		},
	}
	pool := &fakeWorktreePool{leaseDir: "/tmp/wt"}
	em := &recordingEmitter{}
	fg := merge.NewFakeGit(merge.FakeOutput{}, merge.FakeOutput{})
	r := makeCandidateRunner(t, pool, exec, em, fg)

	c := makeCand("feat-A", "h1", []byte("p\n"))
	baseline := merge.PassingSet{"test_a", "test_b"}
	suite := merge.TestSuite{Smoke: []string{"smoke"}, Full: []string{"full"}}
	out, _ := r.Run(context.Background(), c, "abc", baseline, merge.ModeNormal, suite)
	if out.FlakeCount != 0 {
		t.Errorf("FlakeCount = %d want 0 (persistent failure)", out.FlakeCount)
	}
	if !out.HardRejected {
		t.Error("HardRejected = false; expected true (test_b never passes)")
	}
	if out.TestFailCount != 1 {
		t.Errorf("TestFailCount = %d want 1 (test_b still missing post-flake)", out.TestFailCount)
	}
}

func TestCandidateFlakeRerunEmitsFlakeRerunStarted(t *testing.T) {
	exec := &scriptedExecutor{
		scripts: []merge.RunResult{
			{Stdout: "test_a\ntest_b\n", ExitCode: 0},
			{Stdout: "test_a\n", ExitCode: 1, Stderr: "FAIL"},
			{Stdout: "test_b\n", ExitCode: 0},
		},
	}
	pool := &fakeWorktreePool{leaseDir: "/tmp/wt"}
	em := &recordingEmitter{}
	fg := merge.NewFakeGit(merge.FakeOutput{}, merge.FakeOutput{})
	r := makeCandidateRunner(t, pool, exec, em, fg)

	c := makeCand("feat-A", "h1", []byte("p\n"))
	baseline := merge.PassingSet{"test_a", "test_b"}
	suite := merge.TestSuite{Smoke: []string{"smoke"}, Full: []string{"full"}}
	_, _ = r.Run(context.Background(), c, "abc", baseline, merge.ModeNormal, suite)

	saw := false
	for _, e := range em.Snapshot() {
		if e.Type != merge.EvtFlakeRerunStarted {
			continue
		}
		saw = true
		var p merge.FlakeRerunStartedPayload
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			t.Fatalf("decode FlakeRerunStartedPayload: %v", err)
		}
		if p.RetryN < 1 {
			t.Errorf("RetryN = %d want >=1", p.RetryN)
		}
		if p.TestID != "test_b" {
			t.Errorf("TestID = %q want test_b", p.TestID)
		}
		if p.CandidateID != "h1" {
			t.Errorf("CandidateID = %q want h1", p.CandidateID)
		}
	}
	if !saw {
		t.Error("EvtFlakeRerunStarted not emitted")
	}
}

func TestCandidateFlakeRerunEnvCarriesFailedTestList(t *testing.T) {
	exec := &scriptedExecutor{
		scripts: []merge.RunResult{
			{Stdout: "test_a\ntest_b\n", ExitCode: 0},
			{Stdout: "test_a\n", ExitCode: 1, Stderr: "FAIL"},
			{Stdout: "test_b\n", ExitCode: 0},
		},
	}
	pool := &fakeWorktreePool{leaseDir: "/tmp/wt"}
	em := &recordingEmitter{}
	fg := merge.NewFakeGit(merge.FakeOutput{}, merge.FakeOutput{})
	r := makeCandidateRunner(t, pool, exec, em, fg)

	c := makeCand("feat-A", "h1", []byte("p\n"))
	baseline := merge.PassingSet{"test_a", "test_b"}
	suite := merge.TestSuite{Smoke: []string{"smoke"}, Full: []string{"full"}}
	_, _ = r.Run(context.Background(), c, "abc", baseline, merge.ModeNormal, suite)

	calls := exec.Calls()
	if len(calls) < 3 {
		t.Fatalf("calls = %d want >=3", len(calls))
	}
	rerunCall := calls[2]
	saw := false
	for _, e := range rerunCall.Env {
		if strings.HasPrefix(e, "ZEN_MERGE_RERUN_TESTS=") {
			saw = true
			if !strings.Contains(e, "test_b") {
				t.Errorf("ZEN_MERGE_RERUN_TESTS does not include test_b: %s", e)
			}
		}
	}
	if !saw {
		t.Error("rerun env missing ZEN_MERGE_RERUN_TESTS")
	}
}

func TestCandidateFlakeRerunMultipleFailedTestsMixedOutcomes(t *testing.T) {
	exec := &scriptedExecutor{
		scripts: []merge.RunResult{
			{Stdout: "test_a\ntest_b\ntest_c\n", ExitCode: 0},
			{Stdout: "test_a\n", ExitCode: 1, Stderr: "FAIL"},
			{Stdout: "test_b\n", ExitCode: 1, Stderr: "FAIL"},
			{Stdout: "", ExitCode: 1, Stderr: "FAIL"},
		},
	}
	pool := &fakeWorktreePool{leaseDir: "/tmp/wt"}
	em := &recordingEmitter{}
	fg := merge.NewFakeGit(merge.FakeOutput{}, merge.FakeOutput{})
	r := makeCandidateRunner(t, pool, exec, em, fg)

	c := makeCand("feat-A", "h1", []byte("p\n"))
	baseline := merge.PassingSet{"test_a", "test_b", "test_c"}
	suite := merge.TestSuite{Smoke: []string{"smoke"}, Full: []string{"full"}}
	out, err := r.Run(context.Background(), c, "abc", baseline, merge.ModeNormal, suite)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.FlakeCount != 1 {
		t.Errorf("FlakeCount = %d want 1 (only test_b recovered)", out.FlakeCount)
	}
	if !out.HardRejected {
		t.Error("HardRejected = false; want true (test_c never recovered)")
	}
	if out.TestFailCount != 1 {
		t.Errorf("TestFailCount = %d want 1 (test_c still missing)", out.TestFailCount)
	}
	if !out.PassingSet.Has("test_b") {
		t.Error("PassingSet missing test_b post-flake")
	}
	if out.PassingSet.Has("test_c") {
		t.Error("PassingSet contains test_c (should have remained absent)")
	}
}

func TestCandidateFlakeRerunExecutorErrorAbortsLoop(t *testing.T) {
	exec := &scriptedExecutor{
		scripts: []merge.RunResult{
			{Stdout: "test_a\ntest_b\n", ExitCode: 0},
			{Stdout: "test_a\n", ExitCode: 1, Stderr: "FAIL"},
			{Stdout: "", ExitCode: -1, Stderr: "spawn"},
		},
		errs: []error{nil, nil, errors.New("spawn rerun")},
	}
	pool := &fakeWorktreePool{leaseDir: "/tmp/wt"}
	em := &recordingEmitter{}
	fg := merge.NewFakeGit(merge.FakeOutput{}, merge.FakeOutput{})
	r := makeCandidateRunner(t, pool, exec, em, fg)

	c := makeCand("feat-A", "h1", []byte("p\n"))
	baseline := merge.PassingSet{"test_a", "test_b"}
	suite := merge.TestSuite{Smoke: []string{"smoke"}, Full: []string{"full"}}
	out, err := r.Run(context.Background(), c, "abc", baseline, merge.ModeNormal, suite)
	if err != nil {
		t.Fatalf("Run: %v (rerun executor error must NOT surface as Run error)", err)
	}
	if out.FlakeCount != 0 {
		t.Errorf("FlakeCount = %d want 0 (rerun aborted on executor error)", out.FlakeCount)
	}

	calls := exec.Calls()
	if len(calls) != 3 {
		t.Errorf("calls = %d want 3 (smoke + full + 1 rerun before abort)", len(calls))
	}

	if !out.HardRejected {
		t.Error("HardRejected = false; want true (rerun aborted, baseline still broken)")
	}
}

func TestCandidateFlakeRerunCtxCancelMidLoop(t *testing.T) {

	exec := &ctxAwareExecutor{
		scripts: []merge.RunResult{
			{Stdout: "test_a\ntest_b\ntest_c\n", ExitCode: 0},
			{Stdout: "test_a\n", ExitCode: 1, Stderr: "FAIL b,c"},
			{Stdout: "test_b\n", ExitCode: 0},
		},
	}
	pool := &fakeWorktreePool{leaseDir: "/tmp/wt"}
	em := &recordingEmitter{}
	fg := merge.NewFakeGit(merge.FakeOutput{}, merge.FakeOutput{})
	r := makeCandidateRunner(t, pool, exec, em, fg)

	ctx, cancel := context.WithCancel(context.Background())
	exec.cancelAfter = 3
	exec.cancelFn = cancel

	c := makeCand("feat-A", "h1", []byte("p\n"))
	baseline := merge.PassingSet{"test_a", "test_b", "test_c"}
	suite := merge.TestSuite{Smoke: []string{"smoke"}, Full: []string{"full"}}
	out, err := r.Run(ctx, c, "abc", baseline, merge.ModeNormal, suite)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if out.FlakeCount != 1 {
		t.Errorf("FlakeCount = %d want 1 (only pre-cancel rerun should count)", out.FlakeCount)
	}

	rerunStartedCount := 0
	for _, ev := range em.Snapshot() {
		if ev.Type == merge.EvtFlakeRerunStarted {
			rerunStartedCount++
		}
	}
	if rerunStartedCount != 3 {
		t.Errorf("EvtFlakeRerunStarted emits = %d; want 3 (retry1=2 + retry2=1, emit-before-execute)", rerunStartedCount)
	}

	calls := exec.Calls()
	if len(calls) != 4 {
		t.Errorf("calls = %d want 4 (smoke + full + 2 reruns including cancelled one)", len(calls))
	}

	if !out.HardRejected {
		t.Error("HardRejected = false; want true (rerun aborted, test_c still missing)")
	}
}

type ctxAwareExecutor struct {
	mu          sync.Mutex
	calls       []fakeTestCall
	scripts     []merge.RunResult
	cancelAfter int
	cancelFn    context.CancelFunc
}

func (s *ctxAwareExecutor) Run(ctx context.Context, dir string, cmd, env []string) (merge.RunResult, error) {
	s.mu.Lock()
	s.calls = append(s.calls, fakeTestCall{WorkingDir: dir, Cmd: cmd, Env: env})
	idx := len(s.calls)
	s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return merge.RunResult{}, err
	}

	s.mu.Lock()
	var result merge.RunResult
	if idx-1 < len(s.scripts) {
		result = s.scripts[idx-1]
	}
	cancelTrigger := s.cancelAfter > 0 && idx == s.cancelAfter
	s.mu.Unlock()

	if cancelTrigger && s.cancelFn != nil {
		s.cancelFn()
	}
	return result, nil
}

func (s *ctxAwareExecutor) Calls() []fakeTestCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]fakeTestCall, len(s.calls))
	copy(out, s.calls)
	return out
}

var _ = time.Second
