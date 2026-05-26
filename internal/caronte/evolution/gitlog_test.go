package evolution

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func initSimpleGitRepo(t *testing.T, dir string, n int) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "test")
	run("config", "commit.gpgsign", "false")
	run("config", "gc.auto", "0")
	run("config", "maintenance.auto", "false")
	run("config", "core.fsmonitor", "false")
	for i := 0; i < n; i++ {
		f := filepath.Join(dir, "file.go")

		content := "package p // rev " + strconv.Itoa(i) + "\n"
		if err := os.WriteFile(f, []byte(content), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		run("add", ".")
		run("commit", "-q", "-m", "commit")
	}
}

func TestParseLogSingleCommit(t *testing.T) {

	rec := "abc123" + unitSep + "alice@example.com" + unitSep + "1700000000" + unitSep +
		"a.go\nb.go\n" + recSep
	commits, err := parseLog(rec)
	if err != nil {
		t.Fatalf("parseLog: %v", err)
	}
	if len(commits) != 1 {
		t.Fatalf("len(commits) = %d; want 1", len(commits))
	}
	c := commits[0]
	if c.SHA != "abc123" {
		t.Errorf("SHA = %q; want abc123", c.SHA)
	}
	if c.AuthorEmail != "alice@example.com" {
		t.Errorf("AuthorEmail = %q; want alice@example.com", c.AuthorEmail)
	}
	if c.UnixTime != 1700000000 {
		t.Errorf("UnixTime = %d; want 1700000000", c.UnixTime)
	}
	if len(c.Files) != 2 || c.Files[0] != "a.go" || c.Files[1] != "b.go" {
		t.Errorf("Files = %v; want [a.go b.go]", c.Files)
	}
}

func TestParseLogMultipleCommitsAndBlankFiles(t *testing.T) {
	out := strings.Join([]string{
		"sha1" + unitSep + "a@x.com" + unitSep + "100" + unitSep + "x.go\n\ny.go\n",
		"sha2" + unitSep + "b@x.com" + unitSep + "200" + unitSep + "",
	}, recSep) + recSep
	commits, err := parseLog(out)
	if err != nil {
		t.Fatalf("parseLog: %v", err)
	}
	if len(commits) != 2 {
		t.Fatalf("len(commits) = %d; want 2", len(commits))
	}
	if len(commits[0].Files) != 2 || commits[0].Files[0] != "x.go" || commits[0].Files[1] != "y.go" {
		t.Errorf("commit0 Files = %v; want [x.go y.go] (blank line dropped)", commits[0].Files)
	}
	if len(commits[1].Files) != 0 {
		t.Errorf("commit1 Files = %v; want [] (no files)", commits[1].Files)
	}
}

func TestParseLogEmptyOutput(t *testing.T) {
	commits, err := parseLog("")
	if err != nil {
		t.Fatalf("parseLog(\"\"): %v", err)
	}
	if len(commits) != 0 {
		t.Errorf("len(commits) = %d; want 0", len(commits))
	}
}

func TestParseLogMalformedRecordSkipped(t *testing.T) {
	out := "only-one-field" + recSep +
		"sha2" + unitSep + "b@x.com" + unitSep + "200" + unitSep + "z.go\n" + recSep
	commits, err := parseLog(out)
	if err != nil {
		t.Fatalf("parseLog: %v", err)
	}
	if len(commits) != 1 || commits[0].SHA != "sha2" {
		t.Errorf("commits = %+v; want only the well-formed sha2 record", commits)
	}
}

func TestParseLogBadTimestampSkipped(t *testing.T) {
	out := "sha1" + unitSep + "a@x.com" + unitSep + "notanumber" + unitSep + "a.go\n" + recSep +
		"sha2" + unitSep + "b@x.com" + unitSep + "999" + unitSep + "b.go\n" + recSep
	commits, err := parseLog(out)
	if err != nil {
		t.Fatalf("parseLog: %v", err)
	}
	if len(commits) != 1 || commits[0].SHA != "sha2" {
		t.Errorf("commits = %+v; want only well-formed sha2 (bad-timestamp sha1 skipped)", commits)
	}
}

func TestValidateGitArgRejectsFlagInjection(t *testing.T) {
	bad := []string{"--output=/etc/passwd", "-x", "--since"}
	for _, a := range bad {
		if err := validateGitArg(a); err == nil {
			t.Errorf("validateGitArg(%q) = nil; want rejection", a)
		}
	}
	good := []string{"HEAD", "2026-01-01T00:00:00Z", "a.go", "main", "v1.2.3"}
	for _, a := range good {
		if err := validateGitArg(a); err != nil {
			t.Errorf("validateGitArg(%q) = %v; want nil", a, err)
		}
	}
}

func TestValidateGitArgRejectsEmpty(t *testing.T) {
	if err := validateGitArg(""); err == nil {
		t.Error("validateGitArg(\"\") = nil; want rejection for empty arg")
	}
}

type fakeRunner struct {
	log   string
	count int
	err   error
}

func (f fakeRunner) Log(_ context.Context, _ string, _ ...string) (string, error) {
	return f.log, f.err
}
func (f fakeRunner) RevListCount(_ context.Context, _ string) (int, error) {
	return f.count, f.err
}

func TestGitRunnerSeam(t *testing.T) {
	var r GitRunner = fakeRunner{log: "", count: 7}
	n, err := r.RevListCount(context.Background(), "/repo")
	if err != nil {
		t.Fatalf("RevListCount: %v", err)
	}
	if n != 7 {
		t.Errorf("RevListCount = %d; want 7", n)
	}
}

func TestOsGitRunnerErrorWrapping(t *testing.T) {
	r := NewOSGitRunner()

	_, err := r.RevListCount(context.Background(), t.TempDir())
	if err == nil {
		t.Error("RevListCount on non-repo = nil error; want a wrapped git error")
	}
	if !errors.Is(err, ErrGit) {
		t.Errorf("error = %v; want wrapped ErrGit", err)
	}
}

func TestOsGitRunnerLogArgGuard(t *testing.T) {
	r := NewOSGitRunner()

	_, err := r.Log(context.Background(), t.TempDir(), "")
	if err == nil {
		t.Error("Log with empty interpolated arg = nil; want ErrGit rejection")
	}
	if !errors.Is(err, ErrGit) {
		t.Errorf("error = %v; want wrapped ErrGit", err)
	}
}

func TestFakeRunnerPropagatesError(t *testing.T) {
	sentinel := errors.New("test-error")
	r := fakeRunner{err: sentinel}
	_, logErr := r.Log(context.Background(), "/repo")
	if !errors.Is(logErr, sentinel) {
		t.Errorf("Log err = %v; want sentinel", logErr)
	}
	_, cntErr := r.RevListCount(context.Background(), "/repo")
	if !errors.Is(cntErr, sentinel) {
		t.Errorf("RevListCount err = %v; want sentinel", cntErr)
	}
}

func TestOsGitRunnerRevListCountSuccess(t *testing.T) {
	dir := t.TempDir()
	initSimpleGitRepo(t, dir, 3)
	r := NewOSGitRunner()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	n, err := r.RevListCount(ctx, dir)
	if err != nil {
		t.Fatalf("RevListCount on real repo: %v", err)
	}
	if n != 3 {
		t.Errorf("RevListCount = %d; want 3", n)
	}
}

func TestOsGitRunnerLogSuccess(t *testing.T) {
	dir := t.TempDir()
	initSimpleGitRepo(t, dir, 2)
	r := NewOSGitRunner()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	format := "--pretty=format:" + recSep + "%H" + unitSep + "%ae" + unitSep + "%ct" + unitSep
	out, err := r.Log(ctx, dir, format, "--name-only", "--no-merges")
	if err != nil {
		t.Fatalf("Log on real repo: %v", err)
	}
	commits, perr := parseLog(out)
	if perr != nil {
		t.Fatalf("parseLog: %v", perr)
	}
	if len(commits) != 2 {
		t.Errorf("len(commits) = %d; want 2", len(commits))
	}
}

func TestOsGitRunnerLogNonRepoDirErrors(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	r := NewOSGitRunner()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := r.Log(ctx, t.TempDir(), "--name-only", "--no-merges")
	if err == nil {
		t.Error("Log on non-repo = nil error; want wrapped ErrGit")
	}
	if !errors.Is(err, ErrGit) {
		t.Errorf("error = %v; want wrapped ErrGit", err)
	}
}
