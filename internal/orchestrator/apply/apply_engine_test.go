package apply_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/apply"
)

func gitInit(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=apply-test",
			"GIT_AUTHOR_EMAIL=apply-test@example.com",
			"GIT_COMMITTER_NAME=apply-test",
			"GIT_COMMITTER_EMAIL=apply-test@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "hello.txt")
	run("commit", "-q", "-m", "initial")
	run("checkout", "-q", "-b", "worker/W1")
	return dir, "worker/W1"
}

type recordingEmitter struct {
	mu sync.Mutex
	ev []apply.Event
}

func (r *recordingEmitter) Append(_ context.Context, e apply.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ev = append(r.ev, e)
	return nil
}

func (r *recordingEmitter) snapshot() []apply.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]apply.Event, len(r.ev))
	copy(out, r.ev)
	return out
}

func TestApplyEngine_ApplyFix_HappyPathClean(t *testing.T) {
	dir, branch := gitInit(t)
	em := &recordingEmitter{}
	eng := apply.New(apply.Config{RepoDir: dir, Emitter: em, Timeout: 5 * time.Second})

	fp := apply.FixPrompt{
		ID: "fix-1",
		Patch: "diff --git a/hello.txt b/hello.txt\n" +
			"--- a/hello.txt\n+++ b/hello.txt\n" +
			"@@ -1 +1 @@\n-hello\n+hola\n",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := eng.ApplyFix(ctx, branch, fp)
	if err != nil {
		t.Fatalf("ApplyFix returned error: %v", err)
	}
	if !res.TestsPassed {
		t.Fatalf("expected TestsPassed=true (no TestCmd → tests skipped pass-through), got false (stderr=%q)", res.TestStderr)
	}
	if res.CommitSHA == "" {
		t.Fatal("expected non-empty CommitSHA")
	}
	if want := []string{"hello.txt"}; !equalStrings(res.FilesTouched, want) {
		t.Fatalf("FilesTouched: got %v want %v", res.FilesTouched, want)
	}
	if res.Reverted {
		t.Fatal("expected Reverted=false on happy path")
	}

	if b, _ := os.ReadFile(filepath.Join(dir, "hello.txt")); string(b) != "hola\n" {
		t.Fatalf("hello.txt content: got %q want %q", string(b), "hola\n")
	}

	evs := em.snapshot()
	if len(evs) != 2 {
		t.Fatalf("emitted %d events, want 2: %+v", len(evs), evs)
	}
	if evs[0].Type != apply.EventApplyAttempted {
		t.Fatalf("ev[0].Type = %v; want ApplyAttempted", evs[0].Type)
	}
	if evs[1].Type != apply.EventApplySucceeded {
		t.Fatalf("ev[1].Type = %v; want ApplySucceeded", evs[1].Type)
	}
	if evs[1].FixID != "fix-1" {
		t.Fatalf("ev[1].FixID = %q; want fix-1", evs[1].FixID)
	}
	if evs[1].Branch != branch {
		t.Fatalf("ev[1].Branch = %q; want %q", evs[1].Branch, branch)
	}
}

func TestApplyEngine_New_RejectsEmptyRepoDir(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on empty Config.RepoDir")
		}
	}()
	_ = apply.New(apply.Config{Emitter: &recordingEmitter{}})
}

func TestApplyEngine_New_RejectsNilEmitter(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on nil Config.Emitter")
		}
	}()
	_ = apply.New(apply.Config{RepoDir: t.TempDir()})
}

func TestApplyEngine_ApplyFix_PropagatesContextCancellation(t *testing.T) {
	dir, branch := gitInit(t)
	eng := apply.New(apply.Config{RepoDir: dir, Emitter: &recordingEmitter{}, Timeout: time.Hour})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := eng.ApplyFix(ctx, branch, apply.FixPrompt{ID: "fix-cancel", Patch: "ignored"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v; want context.Canceled", err)
	}
}

func TestApplyEngine_ApplyFix_HappyPathWithGreenTestCmd(t *testing.T) {
	dir, branch := gitInit(t)
	em := &recordingEmitter{}
	eng := apply.New(apply.Config{RepoDir: dir, Emitter: em, Timeout: 5 * time.Second})

	fp := apply.FixPrompt{
		ID: "fix-test-green",
		Patch: "diff --git a/hello.txt b/hello.txt\n" +
			"--- a/hello.txt\n+++ b/hello.txt\n" +
			"@@ -1 +1 @@\n-hello\n+hola\n",
		TestCmd: []string{"sh", "-c", "exit 0"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := eng.ApplyFix(ctx, branch, fp)
	if err != nil {
		t.Fatalf("ApplyFix returned error: %v", err)
	}
	if !res.TestsPassed || res.Reverted {
		t.Fatalf("expected TestsPassed=true,Reverted=false; got %+v", res)
	}
	evs := em.snapshot()
	if len(evs) != 2 || evs[1].Type != apply.EventApplySucceeded {
		t.Fatalf("event sequence: %+v", evs)
	}
}

func TestApplyEngine_ApplyFix_RevertsOnTestFailure(t *testing.T) {
	dir, branch := gitInit(t)
	em := &recordingEmitter{}
	eng := apply.New(apply.Config{RepoDir: dir, Emitter: em, Timeout: 5 * time.Second})

	priorSHA := headSHA(t, dir)

	fp := apply.FixPrompt{
		ID: "fix-bad",
		Patch: "diff --git a/hello.txt b/hello.txt\n" +
			"--- a/hello.txt\n+++ b/hello.txt\n" +
			"@@ -1 +1 @@\n-hello\n+broken\n",
		TestCmd: []string{"sh", "-c", "echo 'regression detected' >&2; exit 1"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := eng.ApplyFix(ctx, branch, fp)
	if err == nil {
		t.Fatal("expected error from ApplyFix when tests fail")
	}
	if !errors.Is(err, apply.ErrTestsFailed) {
		t.Fatalf("err = %v; want wraps ErrTestsFailed", err)
	}
	if !res.Reverted {
		t.Fatalf("expected Reverted=true; got %+v", res)
	}
	if !strings.Contains(res.TestStderr, "regression detected") {
		t.Fatalf("TestStderr did not capture child stderr: %q", res.TestStderr)
	}
	if got := headSHA(t, dir); got != priorSHA {
		t.Fatalf("HEAD moved despite revert: prior=%s now=%s", priorSHA, got)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "hello.txt")); string(b) != "hello\n" {
		t.Fatalf("hello.txt not reverted: %q", string(b))
	}
	evs := em.snapshot()
	if len(evs) != 2 || evs[1].Type != apply.EventApplyReverted {
		t.Fatalf("event sequence: %+v", evs)
	}
	if !strings.Contains(evs[1].Stderr, "regression detected") {
		t.Fatalf("ev[1].Stderr did not propagate test stderr: %q", evs[1].Stderr)
	}
}

func TestApplyEngine_ApplyFix_RejectsMalformedPatch(t *testing.T) {
	dir, branch := gitInit(t)
	em := &recordingEmitter{}
	eng := apply.New(apply.Config{RepoDir: dir, Emitter: em, Timeout: 3 * time.Second})

	fp := apply.FixPrompt{ID: "fix-malformed", Patch: "this is not a unified diff\n"}
	_, err := eng.ApplyFix(context.Background(), branch, fp)
	if err == nil {
		t.Fatal("expected error on malformed patch")
	}
	if !errors.Is(err, apply.ErrPatchRejected) {
		t.Fatalf("err = %v; want wraps ErrPatchRejected", err)
	}

	evs := em.snapshot()
	if len(evs) != 1 || evs[0].Type != apply.EventApplyAttempted {
		t.Fatalf("expected only ApplyAttempted; got %+v", evs)
	}
}

func TestApplyEngine_ApplyFix_RejectsDirtyWorkingTree(t *testing.T) {
	dir, branch := gitInit(t)
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	em := &recordingEmitter{}
	eng := apply.New(apply.Config{RepoDir: dir, Emitter: em, Timeout: 3 * time.Second})
	_, err := eng.ApplyFix(context.Background(), branch, apply.FixPrompt{ID: "fix-dirty", Patch: "ignored"})
	if !errors.Is(err, apply.ErrWorkingTreeDirty) {
		t.Fatalf("err = %v; want wraps ErrWorkingTreeDirty", err)
	}

	if evs := em.snapshot(); len(evs) != 0 {
		t.Fatalf("expected no events on dirty-tree refusal; got %+v", evs)
	}
}

func TestApplyEngine_ApplyFix_CheckoutFailsOnUnknownBranch(t *testing.T) {
	dir, _ := gitInit(t)
	em := &recordingEmitter{}
	eng := apply.New(apply.Config{RepoDir: dir, Emitter: em, Timeout: 3 * time.Second})
	_, err := eng.ApplyFix(context.Background(), "worker/does-not-exist", apply.FixPrompt{ID: "fix-noref", Patch: ""})
	if !errors.Is(err, apply.ErrCheckoutFailed) {
		t.Fatalf("err = %v; want wraps ErrCheckoutFailed", err)
	}
	if evs := em.snapshot(); len(evs) != 0 {
		t.Fatalf("expected no events on checkout-fail; got %+v", evs)
	}
}

func TestApplyEngine_ApplyFix_StatusFailsOnNonRepo(t *testing.T) {

	dir := t.TempDir()
	em := &recordingEmitter{}
	eng := apply.New(apply.Config{RepoDir: dir, Emitter: em, Timeout: 3 * time.Second})
	_, err := eng.ApplyFix(context.Background(), "main", apply.FixPrompt{ID: "fix-nonrepo", Patch: ""})
	if !errors.Is(err, apply.ErrWorkingTreeDirty) {
		t.Fatalf("err = %v; want wraps ErrWorkingTreeDirty (status err arm)", err)
	}
}

func TestApplyEngine_ApplyFix_CommitFailsOnEmptyPatch(t *testing.T) {

	dir, branch := gitInit(t)
	em := &recordingEmitter{}
	eng := apply.New(apply.Config{RepoDir: dir, Emitter: em, Timeout: 3 * time.Second})

	fp := apply.FixPrompt{ID: "fix-empty", Patch: ""}
	_, err := eng.ApplyFix(context.Background(), branch, fp)
	if err == nil {
		t.Fatal("expected error on empty/no-op patch")
	}

	if !errors.Is(err, apply.ErrPatchRejected) && !errors.Is(err, apply.ErrCommitFailed) {
		t.Fatalf("err = %v; want wraps ErrPatchRejected or ErrCommitFailed", err)
	}
}

func TestApplyEngine_ApplyFix_RevertFailureSurfacesErrRevertFailed(t *testing.T) {

	dir, branch := gitInit(t)
	em := &recordingEmitter{}
	eng := apply.New(apply.Config{RepoDir: dir, Emitter: em, Timeout: 5 * time.Second})

	fp := apply.FixPrompt{
		ID: "fix-revert-fail",
		Patch: "diff --git a/hello.txt b/hello.txt\n" +
			"--- a/hello.txt\n+++ b/hello.txt\n" +
			"@@ -1 +1 @@\n-hello\n+hola\n",

		TestCmd: []string{"sh", "-c", "rm -rf .git && exit 1"},
	}

	_, err := eng.ApplyFix(context.Background(), branch, fp)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, apply.ErrRevertFailed) {
		t.Fatalf("err = %v; want wraps ErrRevertFailed", err)
	}

	evs := em.snapshot()
	if len(evs) != 2 || evs[1].Type != apply.EventApplyReverted {
		t.Fatalf("event sequence on revert failure: %+v", evs)
	}
}

func TestApplyEngine_ApplyFix_HonoursTimeout(t *testing.T) {
	dir, branch := gitInit(t)
	em := &recordingEmitter{}
	eng := apply.New(apply.Config{RepoDir: dir, Emitter: em, Timeout: 100 * time.Millisecond})

	fp := apply.FixPrompt{
		ID: "fix-slow-test",
		Patch: "diff --git a/hello.txt b/hello.txt\n" +
			"--- a/hello.txt\n+++ b/hello.txt\n" +
			"@@ -1 +1 @@\n-hello\n+hola\n",
		TestCmd: []string{"sh", "-c", "sleep 5"},
	}
	_, err := eng.ApplyFix(context.Background(), branch, fp)
	if err == nil {
		t.Fatal("expected error from timeout-bounded ApplyFix")
	}
}

func TestApplyEngine_GitEnvScrubsInheritedGitVars(t *testing.T) {

	t.Setenv("GIT_AUTHOR_NAME", "should-not-leak")
	t.Setenv("GIT_HOSTILE", "should-not-leak")
	dir, branch := gitInit(t)
	em := &recordingEmitter{}
	eng := apply.New(apply.Config{RepoDir: dir, Emitter: em, Timeout: 5 * time.Second})
	fp := apply.FixPrompt{
		ID: "fix-env-scrub",
		Patch: "diff --git a/hello.txt b/hello.txt\n" +
			"--- a/hello.txt\n+++ b/hello.txt\n" +
			"@@ -1 +1 @@\n-hello\n+hola\n",
	}
	res, err := eng.ApplyFix(context.Background(), branch, fp)
	if err != nil {
		t.Fatalf("ApplyFix: %v", err)
	}

	cmd := exec.Command("git", "log", "-1", "--pretty=%an", res.CommitSHA)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git log: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != "zen-apply" {
		t.Fatalf("commit author = %q; expected zen-apply (env scrub failed)", got)
	}
}

func headSHA(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func TestIsTestRun_TrueUnderGoTest(t *testing.T) {
	if !apply.IsTestRun() {
		t.Fatal("apply.IsTestRun() == false under `go test`; expected true")
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
