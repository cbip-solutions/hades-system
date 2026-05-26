package merge_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/merge"
)

func gitInit(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=merge-test",
			"GIT_AUTHOR_EMAIL=merge-test@example.com",
			"GIT_COMMITTER_NAME=merge-test",
			"GIT_COMMITTER_EMAIL=merge-test@example.com",
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
	return dir
}

func TestRealGitRunHappyPath(t *testing.T) {
	dir := gitInit(t)
	g, err := merge.NewRealGit()
	if err != nil {
		t.Fatalf("NewRealGit: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stdout, stderr, err := g.Run(ctx, dir, "", "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("Run: %v stderr=%s", err, stderr)
	}
	if len(strings.TrimSpace(stdout)) != 40 {
		t.Errorf("rev-parse HEAD returned %q (want 40-char SHA)", strings.TrimSpace(stdout))
	}
}

func TestRealGitRunPropagatesContextCancel(t *testing.T) {
	dir := gitInit(t)
	g, err := merge.NewRealGit()
	if err != nil {
		t.Fatalf("NewRealGit: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err = g.Run(ctx, dir, "", "rev-parse", "HEAD")
	if err == nil {
		t.Fatal("expected error on pre-cancelled context")
	}
}

func TestRealGitRunCapturesStderr(t *testing.T) {
	dir := gitInit(t)
	g, err := merge.NewRealGit()
	if err != nil {
		t.Fatalf("NewRealGit: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, stderr, runErr := g.Run(ctx, dir, "", "rev-parse", "this-ref-does-not-exist")
	if runErr == nil {
		t.Fatal("expected error from rev-parse on bogus ref")
	}
	if stderr == "" {
		t.Error("expected non-empty stderr capture")
	}
}

func TestRealGitRunStdinForwarded(t *testing.T) {
	dir := gitInit(t)
	g, err := merge.NewRealGit()
	if err != nil {
		t.Fatalf("NewRealGit: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stdout, stderr, err := g.Run(ctx, dir, "world\n", "hash-object", "--stdin")
	if err != nil {
		t.Fatalf("Run hash-object: %v stderr=%s", err, stderr)
	}
	got := strings.TrimSpace(stdout)
	if len(got) != 40 {
		t.Errorf("hash-object returned %q (want 40-char SHA)", got)
	}
}

func TestParseGitVersionVariants(t *testing.T) {
	cases := []struct {
		in           string
		major, minor int
		ok           bool
	}{
		{"git version 2.45.2\n", 2, 45, true},
		{"git version 2.40.0", 2, 40, true},
		{"git version 2.50.0.windows.1\n", 2, 50, true},
		{"git version 1.9.5", 1, 9, true},
		{"git version 3.0.0-rc1\n", 3, 0, true},
		{"git version (Apple Git-152)", 0, 0, false},
		{"", 0, 0, false},
		{"not a version line", 0, 0, false},
	}
	for _, c := range cases {
		major, minor, ok := merge.ParseGitVersion(c.in)
		if ok != c.ok {
			t.Errorf("ParseGitVersion(%q) ok=%v want %v", c.in, ok, c.ok)
			continue
		}
		if !ok {
			continue
		}
		if major != c.major || minor != c.minor {
			t.Errorf("ParseGitVersion(%q) = (%d,%d) want (%d,%d)", c.in, major, minor, c.major, c.minor)
		}
	}
}

func TestGitEnvScrubsGitVarsButKeepsPATH(t *testing.T) {
	t.Setenv("GIT_DIR", "/tmp/should-be-stripped")
	t.Setenv("GIT_AUTHOR_NAME", "should-be-replaced")
	env := merge.GitEnv()
	gotPath := false
	gotAuthor := false
	for _, e := range env {
		if strings.HasPrefix(e, "PATH=") {
			gotPath = true
		}
		if strings.HasPrefix(e, "GIT_DIR=") {
			t.Errorf("GitEnv leaked GIT_DIR: %s", e)
		}
		if e == "GIT_AUTHOR_NAME=zen-merge" {
			gotAuthor = true
		}
		if e == "GIT_AUTHOR_NAME=should-be-replaced" {
			t.Errorf("GitEnv did not replace GIT_AUTHOR_NAME: %s", e)
		}
	}
	if !gotPath {
		t.Error("GitEnv missing PATH entry")
	}
	if !gotAuthor {
		t.Error("GitEnv missing canonical GIT_AUTHOR_NAME=zen-merge")
	}
}

func TestFakeGitRecordsCallsAndPopsOutputs(t *testing.T) {
	fg := merge.NewFakeGit(
		merge.FakeOutput{Stdout: "abc1234\n"},
		merge.FakeOutput{Stderr: "fatal: nope", Err: errors.New("exit 128")},
	)
	out, _, err := fg.Run(context.Background(), "/repo", "", "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if strings.TrimSpace(out) != "abc1234" {
		t.Errorf("first stdout = %q want abc1234", out)
	}
	_, stderr, err := fg.Run(context.Background(), "/repo", "", "rev-parse", "bogus")
	if err == nil {
		t.Fatal("second call expected error")
	}
	if !strings.Contains(stderr, "fatal: nope") {
		t.Errorf("second stderr = %q want contains 'fatal: nope'", stderr)
	}
	calls := fg.Calls()
	if len(calls) != 2 {
		t.Fatalf("Calls() len = %d want 2", len(calls))
	}
	if calls[0].Args[0] != "rev-parse" {
		t.Errorf("calls[0].Args[0] = %q want rev-parse", calls[0].Args[0])
	}
}

func TestFakeGitPushExtendsOutputs(t *testing.T) {
	fg := merge.NewFakeGit(merge.FakeOutput{Stdout: "first"})
	fg.Push(merge.FakeOutput{Stdout: "second"}, merge.FakeOutput{Stdout: "third"})
	for i, want := range []string{"first", "second", "third"} {
		out, _, err := fg.Run(context.Background(), "/r", "", "noop")
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		if out != want {
			t.Errorf("call %d: got %q want %q", i, out, want)
		}
	}
}

func TestFakeGitNoCannedOutputReturnsZero(t *testing.T) {
	fg := merge.NewFakeGit()
	out, stderr, err := fg.Run(context.Background(), "/r", "", "rev-parse", "HEAD")
	if err != nil {
		t.Errorf("err = %v want nil (zero outputs default)", err)
	}
	if out != "" || stderr != "" {
		t.Errorf("zero outputs: out=%q stderr=%q want both empty", out, stderr)
	}
}

func TestVersionCheckHappyPath(t *testing.T) {
	g, err := merge.NewRealGit()
	if err != nil {
		t.Skipf("git not on PATH (system-level): %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := merge.VersionCheck(ctx, g); err != nil {
		t.Fatalf("VersionCheck: %v (CI must have git ≥2.40)", err)
	}
}

func TestVersionCheckRejectsTooOld(t *testing.T) {
	fg := merge.NewFakeGit(merge.FakeOutput{Stdout: "git version 2.39.5\n"})
	err := merge.VersionCheck(context.Background(), fg)
	if !errors.Is(err, merge.ErrGitVersionTooOld) {
		t.Fatalf("err = %v want wraps ErrGitVersionTooOld", err)
	}
}

func TestVersionCheckRejectsUnparseable(t *testing.T) {
	fg := merge.NewFakeGit(merge.FakeOutput{Stdout: "garbage output\n"})
	err := merge.VersionCheck(context.Background(), fg)
	if !errors.Is(err, merge.ErrGitVersionTooOld) {
		t.Fatalf("err = %v want wraps ErrGitVersionTooOld (unparseable counts as too-old)", err)
	}
}

func TestVersionCheckRejectsAtBoundaryMinusOne(t *testing.T) {
	cases := []string{
		"git version 1.9.5\n",
		"git version 2.39.0\n",
		"git version 0.99.0\n",
	}
	for _, in := range cases {
		fg := merge.NewFakeGit(merge.FakeOutput{Stdout: in})
		err := merge.VersionCheck(context.Background(), fg)
		if !errors.Is(err, merge.ErrGitVersionTooOld) {
			t.Errorf("VersionCheck(%q): err=%v want wraps ErrGitVersionTooOld", in, err)
		}
	}
}

func TestVersionCheckAcceptsBoundaryAndAbove(t *testing.T) {
	cases := []string{
		"git version 2.40.0\n",
		"git version 2.45.2\n",
		"git version 3.0.0\n",
		"git version 2.50.0.windows.1\n",
	}
	for _, in := range cases {
		fg := merge.NewFakeGit(merge.FakeOutput{Stdout: in})
		err := merge.VersionCheck(context.Background(), fg)
		if err != nil {
			t.Errorf("VersionCheck(%q): err=%v want nil", in, err)
		}
	}
}

func TestVersionCheckPropagatesRunError(t *testing.T) {
	fg := merge.NewFakeGit(merge.FakeOutput{Err: errors.New("exec: not found")})
	err := merge.VersionCheck(context.Background(), fg)
	if err == nil {
		t.Fatal("expected error when underlying Run errors")
	}
}

func TestRevParseHappyPath(t *testing.T) {
	dir := gitInit(t)
	g, err := merge.NewRealGit()
	if err != nil {
		t.Skipf("git not on PATH: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sha, err := merge.RevParse(ctx, g, dir, "HEAD")
	if err != nil {
		t.Fatalf("RevParse: %v", err)
	}
	if len(sha) != 40 {
		t.Errorf("RevParse SHA len = %d want 40 (got %q)", len(sha), sha)
	}
}

func TestRevParseRejectsBogusRef(t *testing.T) {
	dir := gitInit(t)
	g, err := merge.NewRealGit()
	if err != nil {
		t.Skipf("git not on PATH: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = merge.RevParse(ctx, g, dir, "refs/heads/this-does-not-exist")
	if !errors.Is(err, merge.ErrTargetNotExist) {
		t.Fatalf("err = %v want wraps ErrTargetNotExist", err)
	}
}

func TestRevParseTrimsTrailingNewline(t *testing.T) {
	fg := merge.NewFakeGit(merge.FakeOutput{Stdout: "abcdef0123456789abcdef0123456789abcdef01\n"})
	sha, err := merge.RevParse(context.Background(), fg, "/r", "HEAD")
	if err != nil {
		t.Fatalf("RevParse: %v", err)
	}
	if sha != "abcdef0123456789abcdef0123456789abcdef01" {
		t.Errorf("RevParse trimmed = %q", sha)
	}
	if strings.HasSuffix(sha, "\n") {
		t.Error("RevParse left trailing newline")
	}
}

func TestMergeBaseHappyPathTwoBranches(t *testing.T) {
	dir := gitInit(t)
	g, err := merge.NewRealGit()
	if err != nil {
		t.Skipf("git not on PATH: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mainSHA, err := merge.RevParse(ctx, g, dir, "HEAD")
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}
	for _, branch := range []string{"feat-A", "feat-B"} {
		if _, _, err := g.Run(ctx, dir, "", "checkout", "-q", "-b", branch); err != nil {
			t.Fatalf("checkout -b %s: %v", branch, err)
		}
		fpath := filepath.Join(dir, branch+".txt")
		if err := os.WriteFile(fpath, []byte(branch+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, _, err := g.Run(ctx, dir, "", "add", "."); err != nil {
			t.Fatalf("add: %v", err)
		}
		if _, _, err := g.Run(ctx, dir, "", "commit", "-q", "-m", "feat: "+branch); err != nil {
			t.Fatalf("commit: %v", err)
		}
		if _, _, err := g.Run(ctx, dir, "", "checkout", "-q", "main"); err != nil {
			t.Fatalf("checkout main: %v", err)
		}
	}

	mb, err := merge.MergeBase(ctx, g, dir, "feat-A", "feat-B")
	if err != nil {
		t.Fatalf("MergeBase: %v", err)
	}
	if mb != mainSHA {
		t.Errorf("MergeBase = %s want %s (main HEAD)", mb, mainSHA)
	}
}

func TestMergeBaseRequiresAtLeastTwoHeads(t *testing.T) {
	g, err := merge.NewRealGit()
	if err != nil {
		t.Skipf("git not on PATH: %v", err)
	}
	_, err = merge.MergeBase(context.Background(), g, "/tmp/noop", "only-one")
	if err == nil {
		t.Fatal("expected error when fewer than 2 heads supplied")
	}
}

func TestMergeBaseEmptyOutput(t *testing.T) {
	fg := merge.NewFakeGit(merge.FakeOutput{Stdout: "\n"})
	_, err := merge.MergeBase(context.Background(), fg, "/r", "a", "b")
	if err == nil {
		t.Fatal("expected error on empty merge-base output")
	}
}

func TestMergeBasePropagatesError(t *testing.T) {
	fg := merge.NewFakeGit(merge.FakeOutput{Err: errors.New("exit 128"), Stderr: "fatal: bad object"})
	_, err := merge.MergeBase(context.Background(), fg, "/r", "a", "b")
	if err == nil {
		t.Fatal("expected error from underlying Run")
	}
	if !strings.Contains(err.Error(), "fatal: bad object") {
		t.Errorf("error message %q does not surface stderr", err.Error())
	}
}

func TestRevParseEmptyOutputWrapsErrTargetNotExist(t *testing.T) {
	fg := merge.NewFakeGit(merge.FakeOutput{Stdout: ""})
	_, err := merge.RevParse(context.Background(), fg, "/r", "HEAD")
	if !errors.Is(err, merge.ErrTargetNotExist) {
		t.Fatalf("err = %v want wraps ErrTargetNotExist on empty output", err)
	}
}

func TestNewRealGitReturnsErrGitNotFoundWhenPATHMissingGit(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	_, err := merge.NewRealGit()
	if err == nil {
		t.Fatal("expected error when git absent from PATH")
	}
	if !errors.Is(err, merge.ErrGitNotFound) {
		t.Fatalf("err = %v want wraps ErrGitNotFound", err)
	}
}

func TestParseGitVersionRejectsNonNumericMajorMinor(t *testing.T) {
	cases := []string{
		"git version a.40",
		"git version 2.b.0",
	}
	for _, in := range cases {
		_, _, ok := merge.ParseGitVersion(in)
		if ok {
			t.Errorf("ParseGitVersion(%q) ok=true want false", in)
		}
	}
}
