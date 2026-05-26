package scheduler_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/scheduler"
)

type fakePoller struct {
	sha string
	err error
}

func (f *fakePoller) HeadSHA(ctx context.Context, repoURL, branch string) (string, error) {
	return f.sha, f.err
}

func TestGitPollOnce_NewSHATriggers(t *testing.T) {
	s := &scheduler.Schedule{
		TriggerType: scheduler.TriggerGitPoll,
		TriggerConfig: scheduler.TriggerConfig{
			RepoURL:     "https://github.com/o/r",
			Branch:      "main",
			LastSeenSHA: "0000000000000000000000000000000000000001",
		},
	}
	p := &fakePoller{sha: "0000000000000000000000000000000000000002"}
	got, err := scheduler.GitPollOnce(context.Background(), s, p)
	if err != nil {
		t.Fatalf("GitPollOnce: %v", err)
	}
	if !got {
		t.Errorf("GitPollOnce(new SHA) = false, want true")
	}
	if s.TriggerConfig.LastSeenSHA != "0000000000000000000000000000000000000002" {
		t.Errorf("cursor not advanced: %q", s.TriggerConfig.LastSeenSHA)
	}
}

func TestGitPollOnce_SameSHANoTrigger(t *testing.T) {
	s := &scheduler.Schedule{
		TriggerType: scheduler.TriggerGitPoll,
		TriggerConfig: scheduler.TriggerConfig{
			RepoURL:     "https://github.com/o/r",
			Branch:      "main",
			LastSeenSHA: "0000000000000000000000000000000000000003",
		},
	}
	p := &fakePoller{sha: "0000000000000000000000000000000000000003"}
	got, err := scheduler.GitPollOnce(context.Background(), s, p)
	if err != nil {
		t.Fatalf("GitPollOnce: %v", err)
	}
	if got {
		t.Errorf("GitPollOnce(same SHA) = true, want false")
	}
	if s.TriggerConfig.LastSeenSHA != "0000000000000000000000000000000000000003" {
		t.Errorf("cursor mutated unexpectedly: %q", s.TriggerConfig.LastSeenSHA)
	}
}

func TestGitPollOnce_FirstPollAdvancesCursor(t *testing.T) {
	s := &scheduler.Schedule{
		TriggerType: scheduler.TriggerGitPoll,
		TriggerConfig: scheduler.TriggerConfig{
			RepoURL:     "https://github.com/o/r",
			Branch:      "main",
			LastSeenSHA: "",
		},
	}
	p := &fakePoller{sha: "0000000000000000000000000000000000000004"}
	got, err := scheduler.GitPollOnce(context.Background(), s, p)
	if err != nil {
		t.Fatalf("GitPollOnce: %v", err)
	}
	if got {
		t.Errorf("first poll should not trigger; got true")
	}
	if s.TriggerConfig.LastSeenSHA != "0000000000000000000000000000000000000004" {
		t.Errorf("first poll did not advance cursor: %q", s.TriggerConfig.LastSeenSHA)
	}
}

func TestGitPollOnce_NonGitPollTriggerRejected(t *testing.T) {
	s := &scheduler.Schedule{TriggerType: scheduler.TriggerCron}
	_, err := scheduler.GitPollOnce(context.Background(), s, &fakePoller{})
	if !errors.Is(err, scheduler.ErrInvalidSchedule) {
		t.Errorf("GitPollOnce(non-git-poll) = %v, want ErrInvalidSchedule", err)
	}
}

func TestGitPollOnce_NilSchedule(t *testing.T) {
	_, err := scheduler.GitPollOnce(context.Background(), nil, &fakePoller{})
	if !errors.Is(err, scheduler.ErrInvalidSchedule) {
		t.Errorf("GitPollOnce(nil schedule) = %v, want ErrInvalidSchedule", err)
	}
}

func TestGitPollOnce_NilPoller(t *testing.T) {
	s := &scheduler.Schedule{
		TriggerType: scheduler.TriggerGitPoll,
		TriggerConfig: scheduler.TriggerConfig{
			RepoURL: "https://github.com/o/r", Branch: "main",
		},
	}
	_, err := scheduler.GitPollOnce(context.Background(), s, nil)
	if !errors.Is(err, scheduler.ErrInvalidSchedule) {
		t.Errorf("GitPollOnce(nil poller) = %v, want ErrInvalidSchedule", err)
	}
}

func TestGitPollOnce_PollerErrorPropagated(t *testing.T) {
	sentinel := errors.New("gh: command not found")
	s := &scheduler.Schedule{
		TriggerType: scheduler.TriggerGitPoll,
		TriggerConfig: scheduler.TriggerConfig{
			RepoURL: "https://github.com/o/r", Branch: "main",
			LastSeenSHA: "0000000000000000000000000000000000000005",
		},
	}
	p := &fakePoller{err: sentinel}
	fired, err := scheduler.GitPollOnce(context.Background(), s, p)
	if err == nil {
		t.Fatalf("GitPollOnce: nil err, want %v", sentinel)
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("err = %v; want errors.Is sentinel", err)
	}
	if fired {
		t.Errorf("fired = true on poller error; want false")
	}
	if s.TriggerConfig.LastSeenSHA != "0000000000000000000000000000000000000005" {
		t.Errorf("cursor advanced on poller error: %q", s.TriggerConfig.LastSeenSHA)
	}
}

func TestGitPollOnce_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := &scheduler.Schedule{
		TriggerType: scheduler.TriggerGitPoll,
		TriggerConfig: scheduler.TriggerConfig{
			RepoURL: "https://github.com/o/r", Branch: "main",
		},
	}
	p := &ctxAwarePoller{}
	_, err := scheduler.GitPollOnce(ctx, s, p)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("GitPollOnce(cancelled ctx) err = %v; want errors.Is context.Canceled", err)
	}
}

type ctxAwarePoller struct{ sha string }

func (c *ctxAwarePoller) HeadSHA(ctx context.Context, repoURL, branch string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return c.sha, nil
}

func TestParseGitHubURL_HTTPS(t *testing.T) {
	cases := []struct {
		in        string
		wantOwner string
		wantRepo  string
	}{
		{"https://github.com/o/r", "o", "r"},
		{"https://github.com/o/r.git", "o", "r"},
		{"https://github.com/owner-with-dash/repo_with_underscore", "owner-with-dash", "repo_with_underscore"},
		{"https://github.com/owner/multi.dot.repo", "owner", "multi.dot.repo"},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			owner, repo, err := scheduler.ParseGitHubURL(c.in)
			if err != nil {
				t.Fatalf("ParseGitHubURL(%q) err = %v", c.in, err)
			}
			if owner != c.wantOwner || repo != c.wantRepo {
				t.Errorf("ParseGitHubURL(%q) = (%q, %q); want (%q, %q)",
					c.in, owner, repo, c.wantOwner, c.wantRepo)
			}
		})
	}
}

func TestParseGitHubURL_SSH(t *testing.T) {
	cases := []struct {
		in        string
		wantOwner string
		wantRepo  string
	}{
		{"git@github.com:o/r.git", "o", "r"},
		{"git@github.com:o/r", "o", "r"},
		{"git@github.com:owner-with-dash/repo_with_underscore.git", "owner-with-dash", "repo_with_underscore"},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			owner, repo, err := scheduler.ParseGitHubURL(c.in)
			if err != nil {
				t.Fatalf("ParseGitHubURL(%q) err = %v", c.in, err)
			}
			if owner != c.wantOwner || repo != c.wantRepo {
				t.Errorf("ParseGitHubURL(%q) = (%q, %q); want (%q, %q)",
					c.in, owner, repo, c.wantOwner, c.wantRepo)
			}
		})
	}
}

func TestParseGitHubURL_Invalid(t *testing.T) {
	cases := []string{
		"git@github.com:no-slash",
		"https://github.com/onlyowner",
		"https://github.com/",
		"http://[::1]:namedport",
	}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			_, _, err := scheduler.ParseGitHubURL(c)
			if err == nil {
				t.Errorf("ParseGitHubURL(%q) err = nil; want non-nil", c)
			}
		})
	}
}

func TestGhPoller_HeadSHA_HappyPath(t *testing.T) {
	tmp := t.TempDir()
	script := "#!/bin/sh\necho 'aabbccddeeff00112233445566778899aabbccdd'\nexit 0\n"
	if err := writeExecScript(tmp+"/gh", script); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}
	t.Setenv("PATH", tmp+":"+os.Getenv("PATH"))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	sha, err := scheduler.GhPoller{}.HeadSHA(ctx, "https://github.com/o/r", "main")
	if err != nil {
		t.Fatalf("HeadSHA: %v", err)
	}
	if sha != "aabbccddeeff00112233445566778899aabbccdd" {
		t.Errorf("sha = %q; want aabbccddeeff00112233445566778899aabbccdd", sha)
	}
}

func TestGhPoller_HeadSHA_DefaultBranch(t *testing.T) {
	tmp := t.TempDir()

	script := "#!/bin/sh\nfor a in \"$@\"; do echo \"ARG: $a\" >&2; done\necho '1111111111111111111111111111111111111111'\n"
	if err := writeExecScript(tmp+"/gh", script); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}
	t.Setenv("PATH", tmp+":"+os.Getenv("PATH"))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	sha, err := scheduler.GhPoller{}.HeadSHA(ctx, "https://github.com/o/r", "")
	if err != nil {
		t.Fatalf("HeadSHA: %v", err)
	}
	if sha != "1111111111111111111111111111111111111111" {
		t.Errorf("sha = %q; want 40-char hex", sha)
	}
}

func TestGhPoller_HeadSHA_GhMissing(t *testing.T) {
	t.Setenv("PATH", "")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := scheduler.GhPoller{}.HeadSHA(ctx, "https://github.com/o/r", "main")
	if err == nil {
		t.Fatal("HeadSHA with empty PATH = nil err; want non-nil")
	}
	if !strings.Contains(err.Error(), "gh api") && !strings.Contains(err.Error(), "executable file not found") {
		t.Errorf("err %v missing 'gh api' / 'not found' hint", err)
	}
}

func TestGhPoller_HeadSHA_GhError(t *testing.T) {
	tmp := t.TempDir()
	script := "#!/bin/sh\necho 'gh: not authenticated' >&2\nexit 4\n"
	if err := writeExecScript(tmp+"/gh", script); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}
	t.Setenv("PATH", tmp+":"+os.Getenv("PATH"))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := scheduler.GhPoller{}.HeadSHA(ctx, "https://github.com/o/r", "main")
	if err == nil {
		t.Fatal("HeadSHA with failing fake gh = nil err; want non-nil")
	}
	if !strings.Contains(err.Error(), "gh api") {
		t.Errorf("err %v missing 'gh api' prefix", err)
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Errorf("err %v not unwrappable to *exec.ExitError", err)
	}
}

func TestGhPoller_HeadSHA_UnexpectedShape(t *testing.T) {
	tmp := t.TempDir()

	script := "#!/bin/sh\necho '{\"message\":\"Not Found\"}'\nexit 0\n"
	if err := writeExecScript(tmp+"/gh", script); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}
	t.Setenv("PATH", tmp+":"+os.Getenv("PATH"))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := scheduler.GhPoller{}.HeadSHA(ctx, "https://github.com/o/r", "main")
	if err == nil {
		t.Fatal("HeadSHA with non-SHA output = nil err; want non-nil")
	}
	if !strings.Contains(err.Error(), "unexpected SHA shape") {
		t.Errorf("err %v missing 'unexpected SHA shape' hint", err)
	}
}

func TestGhPoller_HeadSHA_LongUnexpectedShape(t *testing.T) {
	tmp := t.TempDir()

	long := strings.Repeat("z", 200)
	script := "#!/bin/sh\necho '" + long + "'\nexit 0\n"
	if err := writeExecScript(tmp+"/gh", script); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}
	t.Setenv("PATH", tmp+":"+os.Getenv("PATH"))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := scheduler.GhPoller{}.HeadSHA(ctx, "https://github.com/o/r", "main")
	if err == nil {
		t.Fatal("HeadSHA with long output = nil err; want non-nil")
	}
	if !strings.Contains(err.Error(), "unexpected SHA shape") {
		t.Errorf("err %v missing 'unexpected SHA shape' hint", err)
	}
	if !strings.Contains(err.Error(), "...") {
		t.Errorf("err %v missing truncation marker '...'", err)
	}
}

func TestGhPoller_HeadSHA_NonHexCorrectLength(t *testing.T) {
	tmp := t.TempDir()

	bogus := strings.Repeat("g", 40)
	script := "#!/bin/sh\necho '" + bogus + "'\nexit 0\n"
	if err := writeExecScript(tmp+"/gh", script); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}
	t.Setenv("PATH", tmp+":"+os.Getenv("PATH"))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := scheduler.GhPoller{}.HeadSHA(ctx, "https://github.com/o/r", "main")
	if err == nil {
		t.Fatal("HeadSHA with non-hex 40-char output = nil err; want non-nil")
	}
	if !strings.Contains(err.Error(), "unexpected SHA shape") {
		t.Errorf("err %v missing 'unexpected SHA shape' hint", err)
	}
}

func TestGhPoller_HeadSHA_SHA256(t *testing.T) {
	tmp := t.TempDir()

	script := "#!/bin/sh\necho 'aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899'\nexit 0\n"
	if err := writeExecScript(tmp+"/gh", script); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}
	t.Setenv("PATH", tmp+":"+os.Getenv("PATH"))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	sha, err := scheduler.GhPoller{}.HeadSHA(ctx, "https://github.com/o/r", "main")
	if err != nil {
		t.Fatalf("HeadSHA(sha256): %v", err)
	}
	if len(sha) != 64 {
		t.Errorf("sha len = %d; want 64", len(sha))
	}
}

func TestGhPoller_HeadSHA_BadURL(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := scheduler.GhPoller{}.HeadSHA(ctx, "not-a-github-url", "main")
	if err == nil {
		t.Fatal("HeadSHA(invalid url) = nil err; want non-nil")
	}
	if !strings.Contains(err.Error(), "gitpoll.parseURL") {
		t.Errorf("err %v missing 'gitpoll.parseURL' prefix", err)
	}
}

func writeExecScript(path, script string) error {
	return os.WriteFile(path, []byte(script), 0o700)
}
