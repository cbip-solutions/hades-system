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

type fakeWorktreePool struct {
	mu       sync.Mutex
	leases   int
	releases int
	leaseErr error
	leaseDir string
}

func (f *fakeWorktreePool) Lease(ctx context.Context) (*merge.LeasedWorktree, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.leaseErr != nil {
		return nil, f.leaseErr
	}
	f.leases++
	return &merge.LeasedWorktree{Dir: f.leaseDir}, nil
}

func (f *fakeWorktreePool) Release(ctx context.Context, w *merge.LeasedWorktree) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.releases++
	return nil
}

func (f *fakeWorktreePool) Stats() (leases, releases int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.leases, f.releases
}

type fakeTestExecutor struct {
	mu       sync.Mutex
	calls    []fakeTestCall
	stdout   string
	stderr   string
	exitCode int
	err      error
	delay    time.Duration
}

type fakeTestCall struct {
	WorkingDir string
	Cmd        []string
	Env        []string
}

func (f *fakeTestExecutor) Run(ctx context.Context, workingDir string, cmd []string, env []string) (merge.RunResult, error) {
	f.mu.Lock()
	f.calls = append(f.calls, fakeTestCall{WorkingDir: workingDir, Cmd: cmd, Env: env})
	delay := f.delay
	f.mu.Unlock()
	if delay > 0 {
		select {
		case <-ctx.Done():
			return merge.RunResult{Stderr: "ctx", ExitCode: 1}, ctx.Err()
		case <-time.After(delay):
		}
	}
	return merge.RunResult{
		Stdout:   f.stdout,
		Stderr:   f.stderr,
		ExitCode: f.exitCode,
	}, f.err
}

func (f *fakeTestExecutor) Calls() []fakeTestCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]fakeTestCall, len(f.calls))
	copy(out, f.calls)
	return out
}

func makeBaseline(t *testing.T, pool merge.WorktreePool, exec merge.TestExecutor, em merge.EventEmitter) merge.BaselineRunner {
	t.Helper()
	r, err := merge.NewBaselineRunner(merge.BaselineDeps{
		Pool:     pool,
		Executor: exec,
		Emitter:  em,
		Git:      merge.NewFakeGit(),
	}, merge.BaselineConfig{
		Timeout:        30 * time.Second,
		StderrCapBytes: 512,
	})
	if err != nil {
		t.Fatalf("NewBaselineRunner: %v", err)
	}
	return r
}

func TestBaselineRunHappyPath(t *testing.T) {
	pool := &fakeWorktreePool{leaseDir: "/tmp/wt-1"}
	exec := &fakeTestExecutor{
		stdout:   "test_alpha\ntest_beta\ntest_gamma\n",
		exitCode: 0,
	}
	em := &recordingEmitter{}
	r := makeBaseline(t, pool, exec, em)

	suite := merge.TestSuite{Full: []string{"go", "test", "./..."}, Smoke: []string{"go", "test", "-tags=smoke"}}
	got, err := r.Run(context.Background(), "deadbeefcafef00d", merge.ModeNormal, suite)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("PassingSet len = %d want 3", len(got))
	}
	if !got.Has("test_alpha") {
		t.Error("missing test_alpha")
	}
	if l, r := pool.Stats(); l != 1 || r != 1 {
		t.Errorf("pool stats = leases=%d releases=%d want 1/1", l, r)
	}
	snap := em.Snapshot()
	if len(snap) < 2 {
		t.Fatalf("emitter snapshot = %d want >=2", len(snap))
	}
	if snap[0].Type != merge.EvtBaselineStarted {
		t.Errorf("first event = %v want EvtBaselineStarted", snap[0].Type)
	}
	last := snap[len(snap)-1]
	if last.Type != merge.EvtBaselineComplete {
		t.Errorf("last event = %v want EvtBaselineComplete", last.Type)
	}
	var p merge.BaselineCompletePayload
	if err := json.Unmarshal(last.Payload, &p); err != nil {
		t.Fatalf("decode complete payload: %v", err)
	}
	if p.PassingSetHash == "" {
		t.Error("BaselineCompletePayload.PassingSetHash empty")
	}
}

func TestBaselineRunSelectsFullForNormalMode(t *testing.T) {
	pool := &fakeWorktreePool{leaseDir: "/tmp/wt-1"}
	exec := &fakeTestExecutor{stdout: "test_a\n", exitCode: 0}
	em := &recordingEmitter{}
	r := makeBaseline(t, pool, exec, em)
	suite := merge.TestSuite{
		Full:  []string{"go", "test", "./..."},
		Smoke: []string{"go", "test", "-tags=smoke"},
	}
	if _, err := r.Run(context.Background(), "abc", merge.ModeNormal, suite); err != nil {
		t.Fatal(err)
	}
	calls := exec.Calls()
	if len(calls) != 1 {
		t.Fatalf("calls = %d want 1", len(calls))
	}
	if got, want := strings.Join(calls[0].Cmd, " "), "go test ./..."; got != want {
		t.Errorf("Normal mode cmd = %q want %q", got, want)
	}
}

func TestBaselineRunSelectsSmokeForDegraded80(t *testing.T) {
	pool := &fakeWorktreePool{leaseDir: "/tmp/wt-1"}
	exec := &fakeTestExecutor{stdout: "test_a\n", exitCode: 0}
	em := &recordingEmitter{}
	r := makeBaseline(t, pool, exec, em)
	suite := merge.TestSuite{
		Full:  []string{"go", "test", "./..."},
		Smoke: []string{"go", "test", "-tags=smoke"},
	}
	if _, err := r.Run(context.Background(), "abc", merge.ModeDegraded80, suite); err != nil {
		t.Fatal(err)
	}
	calls := exec.Calls()
	if got, want := strings.Join(calls[0].Cmd, " "), "go test -tags=smoke"; got != want {
		t.Errorf("Degraded80 mode cmd = %q want %q", got, want)
	}
}

func TestBaselineRunFailsOnNonZeroExit(t *testing.T) {
	pool := &fakeWorktreePool{leaseDir: "/tmp/wt-1"}
	exec := &fakeTestExecutor{
		stdout:   "test_a\n",
		stderr:   "FAIL: test_b\n",
		exitCode: 1,
	}
	em := &recordingEmitter{}
	r := makeBaseline(t, pool, exec, em)
	suite := merge.TestSuite{Full: []string{"go", "test", "./..."}, Smoke: []string{"go", "test", "-tags=smoke"}}
	_, err := r.Run(context.Background(), "abc", merge.ModeNormal, suite)
	if !errors.Is(err, merge.ErrBaselineFailed) {
		t.Fatalf("err = %v want wraps ErrBaselineFailed", err)
	}
	if l, rel := pool.Stats(); l != 1 || rel != 1 {
		t.Errorf("worktree leak: lease=%d release=%d", l, rel)
	}
	snap := em.Snapshot()
	last := snap[len(snap)-1]
	if last.Type != merge.EvtBaselineFailed {
		t.Errorf("last = %v want EvtBaselineFailed", last.Type)
	}
	var p merge.BaselineFailedPayload
	if err := json.Unmarshal(last.Payload, &p); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if p.Reason == "" {
		t.Error("Reason empty on failure")
	}
}

func TestBaselineRunFailsOnExecutorError(t *testing.T) {
	pool := &fakeWorktreePool{leaseDir: "/tmp/wt-1"}
	exec := &fakeTestExecutor{err: errors.New("exec spawn failure")}
	em := &recordingEmitter{}
	r := makeBaseline(t, pool, exec, em)
	suite := merge.TestSuite{Full: []string{"go", "test", "./..."}, Smoke: []string{"go", "test"}}
	_, err := r.Run(context.Background(), "abc", merge.ModeNormal, suite)
	if !errors.Is(err, merge.ErrBaselineFailed) {
		t.Fatalf("err = %v want wraps ErrBaselineFailed", err)
	}
}

func TestBaselineRunFailsOnPoolLeaseError(t *testing.T) {
	pool := &fakeWorktreePool{leaseErr: errors.New("pool exhausted")}
	exec := &fakeTestExecutor{}
	em := &recordingEmitter{}
	r := makeBaseline(t, pool, exec, em)
	suite := merge.TestSuite{Full: []string{"x"}, Smoke: []string{"y"}}
	_, err := r.Run(context.Background(), "abc", merge.ModeNormal, suite)
	if err == nil {
		t.Fatal("expected error on pool lease failure")
	}
	if l, _ := pool.Stats(); l != 0 {
		t.Errorf("leases = %d want 0 (lease failed)", l)
	}
}

func TestBaselineRunHonorsContextCancel(t *testing.T) {
	pool := &fakeWorktreePool{leaseDir: "/tmp/wt-1"}
	exec := &fakeTestExecutor{stdout: "test_a", delay: 100 * time.Millisecond}
	em := &recordingEmitter{}
	r := makeBaseline(t, pool, exec, em)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	suite := merge.TestSuite{Full: []string{"x"}, Smoke: []string{"y"}}
	_, err := r.Run(ctx, "abc", merge.ModeNormal, suite)
	if err == nil {
		t.Fatal("expected error on pre-cancelled ctx")
	}

	if !errors.Is(err, merge.ErrBaselineFailed) {
		t.Errorf("err = %v want errors.Is(err, ErrBaselineFailed)", err)
	}
}

func TestBaselineRunReleasesWorktreeOnFailure(t *testing.T) {

	pool := &fakeWorktreePool{leaseDir: "/tmp/wt-1"}
	exec := &fakeTestExecutor{exitCode: 2, stderr: "panic"}
	em := &recordingEmitter{}
	r := makeBaseline(t, pool, exec, em)
	suite := merge.TestSuite{Full: []string{"x"}, Smoke: []string{"y"}}
	_, _ = r.Run(context.Background(), "abc", merge.ModeNormal, suite)
	if l, rel := pool.Stats(); l != rel {
		t.Errorf("worktree leak on failure: lease=%d release=%d", l, rel)
	}
}

func TestBaselineRunStderrTruncatedTo512Bytes(t *testing.T) {
	pool := &fakeWorktreePool{leaseDir: "/tmp/wt-1"}
	bigStderr := strings.Repeat("X", 2000)
	exec := &fakeTestExecutor{exitCode: 1, stderr: bigStderr}
	em := &recordingEmitter{}
	r := makeBaseline(t, pool, exec, em)
	suite := merge.TestSuite{Full: []string{"x"}, Smoke: []string{"y"}}
	_, _ = r.Run(context.Background(), "abc", merge.ModeNormal, suite)
	snap := em.Snapshot()
	last := snap[len(snap)-1]
	var p merge.BaselineFailedPayload
	if err := json.Unmarshal(last.Payload, &p); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(p.Stderr) > 512 {
		t.Errorf("stderr len = %d want <= 512", len(p.Stderr))
	}
}

func TestNewBaselineRunnerRejectsNilDeps(t *testing.T) {
	pool := &fakeWorktreePool{leaseDir: "/tmp/wt"}
	exec := &fakeTestExecutor{}
	em := &recordingEmitter{}
	git := merge.NewFakeGit()
	cfg := merge.BaselineConfig{Timeout: 30 * time.Second, StderrCapBytes: 512}

	cases := []struct {
		name string
		deps merge.BaselineDeps
	}{
		{"nil Pool", merge.BaselineDeps{Executor: exec, Emitter: em, Git: git}},
		{"nil Executor", merge.BaselineDeps{Pool: pool, Emitter: em, Git: git}},
		{"nil Emitter", merge.BaselineDeps{Pool: pool, Executor: exec, Git: git}},
		{"nil Git", merge.BaselineDeps{Pool: pool, Executor: exec, Emitter: em}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, err := merge.NewBaselineRunner(c.deps, cfg)
			if err == nil {
				t.Fatalf("NewBaselineRunner(%s): err = nil want non-nil", c.name)
			}
			if r != nil {
				t.Errorf("NewBaselineRunner(%s): runner = %v want nil", c.name, r)
			}
		})
	}
}

func TestNewBaselineRunnerRejectsNegativeStderrCap(t *testing.T) {
	pool := &fakeWorktreePool{leaseDir: "/tmp/wt"}
	exec := &fakeTestExecutor{}
	em := &recordingEmitter{}
	git := merge.NewFakeGit()
	r, err := merge.NewBaselineRunner(merge.BaselineDeps{
		Pool: pool, Executor: exec, Emitter: em, Git: git,
	}, merge.BaselineConfig{Timeout: 30 * time.Second, StderrCapBytes: -1})
	if err == nil {
		t.Fatal("NewBaselineRunner with negative StderrCapBytes: err = nil want non-nil")
	}
	if r != nil {
		t.Errorf("runner = %v want nil", r)
	}
}

func TestBaselineRunFailsOnGitCheckoutError(t *testing.T) {
	pool := &fakeWorktreePool{leaseDir: "/tmp/wt-1"}
	exec := &fakeTestExecutor{}
	em := &recordingEmitter{}

	gitFake := merge.NewFakeGit(merge.FakeOutput{Err: errors.New("checkout exploded")})
	r, err := merge.NewBaselineRunner(merge.BaselineDeps{
		Pool: pool, Executor: exec, Emitter: em, Git: gitFake,
	}, merge.BaselineConfig{Timeout: 30 * time.Second, StderrCapBytes: 512})
	if err != nil {
		t.Fatal(err)
	}
	suite := merge.TestSuite{Full: []string{"go", "test"}, Smoke: []string{"go", "test"}}
	_, runErr := r.Run(context.Background(), "deadbeef", merge.ModeNormal, suite)
	if !errors.Is(runErr, merge.ErrBaselineFailed) {
		t.Fatalf("err = %v want wraps ErrBaselineFailed", runErr)
	}

	if l, rel := pool.Stats(); l != 1 || rel != 1 {
		t.Errorf("worktree leak: lease=%d release=%d want 1/1", l, rel)
	}
	snap := em.Snapshot()
	if len(snap) == 0 {
		t.Fatal("expected at least one emitted event (BaselineFailed)")
	}
	last := snap[len(snap)-1]
	if last.Type != merge.EvtBaselineFailed {
		t.Errorf("last event = %v want EvtBaselineFailed", last.Type)
	}
	var p merge.BaselineFailedPayload
	if err := json.Unmarshal(last.Payload, &p); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if p.Reason != "git_checkout_failed" {
		t.Errorf("Reason = %q want %q", p.Reason, "git_checkout_failed")
	}
	if p.ExitCode != -1 {
		t.Errorf("ExitCode = %d want -1", p.ExitCode)
	}
}

func TestBaselineRunFailsOnEmptyCommandForFullTier(t *testing.T) {

	pool := &fakeWorktreePool{leaseDir: "/tmp/wt-1"}
	exec := &fakeTestExecutor{}
	em := &recordingEmitter{}
	r := makeBaseline(t, pool, exec, em)
	suite := merge.TestSuite{Full: nil, Smoke: []string{"go", "test"}}
	_, err := r.Run(context.Background(), "abc", merge.ModeNormal, suite)
	if !errors.Is(err, merge.ErrBaselineFailed) {
		t.Fatalf("err = %v want wraps ErrBaselineFailed", err)
	}

	if calls := exec.Calls(); len(calls) != 0 {
		t.Errorf("calls = %d want 0 (executor must not be invoked on empty command)", len(calls))
	}
	snap := em.Snapshot()
	if len(snap) == 0 {
		t.Fatal("expected EvtBaselineFailed emission")
	}
	last := snap[len(snap)-1]
	if last.Type != merge.EvtBaselineFailed {
		t.Errorf("last event = %v want EvtBaselineFailed", last.Type)
	}
	var p merge.BaselineFailedPayload
	if err := json.Unmarshal(last.Payload, &p); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if p.Reason != "empty_command" {
		t.Errorf("Reason = %q want %q", p.Reason, "empty_command")
	}
}

func TestBaselineRunSelectsSmokeFailFastForEmergencyOnlyMode(t *testing.T) {

	pool := &fakeWorktreePool{leaseDir: "/tmp/wt-1"}
	exec := &fakeTestExecutor{stdout: "test_a\n", exitCode: 0}
	em := &recordingEmitter{}
	r := makeBaseline(t, pool, exec, em)
	suite := merge.TestSuite{
		Full:  []string{"go", "test", "./..."},
		Smoke: []string{"go", "test", "-tags=smoke"},
	}
	if _, err := r.Run(context.Background(), "abc", merge.ModeEmergencyOnly, suite); err != nil {
		t.Fatal(err)
	}
	calls := exec.Calls()
	if len(calls) != 1 {
		t.Fatalf("calls = %d want 1", len(calls))
	}
	if got, want := strings.Join(calls[0].Cmd, " "), "go test -tags=smoke"; got != want {
		t.Errorf("EmergencyOnly cmd = %q want %q", got, want)
	}
	sawEnv := false
	for _, e := range calls[0].Env {
		if e == "ZEN_MERGE_FAIL_FAST=1" {
			sawEnv = true
			break
		}
	}
	if !sawEnv {
		t.Errorf("EmergencyOnly env missing ZEN_MERGE_FAIL_FAST=1; got %v", calls[0].Env)
	}

	snap := em.Snapshot()
	if snap[0].Type != merge.EvtBaselineStarted {
		t.Fatalf("first = %v want EvtBaselineStarted", snap[0].Type)
	}
	var sp merge.BaselineStartedPayload
	if err := json.Unmarshal(snap[0].Payload, &sp); err != nil {
		t.Fatalf("decode start payload: %v", err)
	}
	if sp.Tier != "SmokeFailFast" {
		t.Errorf("Tier = %q want SmokeFailFast", sp.Tier)
	}
	if sp.Mode != "EmergencyOnly" {
		t.Errorf("Mode = %q want EmergencyOnly", sp.Mode)
	}
}

func TestBaselineGenIDUsesCounterWhenProvided(t *testing.T) {

	pool := &fakeWorktreePool{leaseDir: "/tmp/wt-1"}
	exec := &fakeTestExecutor{stdout: "test_a\n", exitCode: 0}
	em := &recordingEmitter{}
	gc := &merge.GenerationCounter{}
	wantGen := gc.Next()
	r, err := merge.NewBaselineRunner(merge.BaselineDeps{
		Pool: pool, Executor: exec, Emitter: em,
		Git: merge.NewFakeGit(), GenCtr: gc,
	}, merge.BaselineConfig{Timeout: 30 * time.Second, StderrCapBytes: 512})
	if err != nil {
		t.Fatal(err)
	}
	suite := merge.TestSuite{Full: []string{"go", "test"}, Smoke: []string{"go", "test"}}
	if _, err := r.Run(context.Background(), "abc", merge.ModeNormal, suite); err != nil {
		t.Fatal(err)
	}
	snap := em.Snapshot()
	for _, e := range snap {
		if e.GenerationID != wantGen {
			t.Errorf("event %v GenerationID = %d want %d", e.Type, e.GenerationID, wantGen)
		}
	}
}

func TestBaselineParseTestIDsSkipsSigilLines(t *testing.T) {

	pool := &fakeWorktreePool{leaseDir: "/tmp/wt-1"}
	exec := &fakeTestExecutor{
		stdout: "=== run\n" +
			"--- PASS: test_alpha\n" +
			"# package banner\n" +
			"+++ trace\n" +
			"test_real_id\n" +
			"\n" +
			"another_real_id\n",
		exitCode: 0,
	}
	em := &recordingEmitter{}
	r := makeBaseline(t, pool, exec, em)
	suite := merge.TestSuite{Full: []string{"go", "test"}, Smoke: []string{"go", "test"}}
	got, err := r.Run(context.Background(), "abc", merge.ModeNormal, suite)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("PassingSet len = %d want 2 (sigil decorations skipped); got=%v", len(got), got)
	}
	if !got.Has("test_real_id") || !got.Has("another_real_id") {
		t.Errorf("missing real IDs; got = %v", got)
	}
	for _, sigilDecoration := range []string{"=== run", "--- PASS: test_alpha", "# package banner", "+++ trace"} {
		if got.Has(sigilDecoration) {
			t.Errorf("PassingSet contains sigil decoration %q (must be skipped)", sigilDecoration)
		}
	}
}

func TestBaselineCheckoutInvokedWithBaseSHA(t *testing.T) {
	pool := &fakeWorktreePool{leaseDir: "/tmp/wt-1"}
	exec := &fakeTestExecutor{stdout: "test_a\n", exitCode: 0}
	em := &recordingEmitter{}
	fakeGit := merge.NewFakeGit(merge.FakeOutput{Stdout: ""})
	r, err := merge.NewBaselineRunner(merge.BaselineDeps{
		Pool: pool, Executor: exec, Emitter: em, Git: fakeGit,
	}, merge.BaselineConfig{Timeout: 30 * time.Second, StderrCapBytes: 512})
	if err != nil {
		t.Fatal(err)
	}
	suite := merge.TestSuite{Full: []string{"go", "test"}, Smoke: []string{"go", "test"}}
	_, _ = r.Run(context.Background(), "deadbeef", merge.ModeNormal, suite)
	gitCalls := fakeGit.Calls()
	saw := false
	for _, c := range gitCalls {
		if len(c.Args) >= 2 && c.Args[0] == "checkout" && c.Args[1] == "deadbeef" {
			saw = true
			break
		}
	}
	if !saw {
		t.Errorf("git checkout deadbeef not invoked; got calls = %v", gitCalls)
	}
}
