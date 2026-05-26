package cli

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func findCheck(rs []CheckResult, name string) *CheckResult {
	for i := range rs {
		if rs[i].Name == name {
			return &rs[i]
		}
	}
	return nil
}

func TestRunMergeChecks_HappyPath(t *testing.T) {
	c := &fakeMergeClient{cacheStatus: &MergeCacheStatusResult{
		Size:        47,
		HitRatePct:  23.5,
		LastRebuilt: "2026-05-05T10:00:00Z",
	}}
	results := runMergeChecks(context.Background(), c)
	if len(results) != 4 {
		t.Fatalf("expected 4 checks, got %d", len(results))
	}
	for _, name := range []string{
		"merge.daemon_up",
		"merge.git_version",
		"merge.eventlog_writable",
		"merge.cache_health",
	} {
		r := findCheck(results, name)
		if r == nil {
			t.Errorf("missing check %q in results", name)
			continue
		}
		if r.Status != "ok" {
			t.Errorf("happy path: check %q status=%q (want ok); detail=%q", name, r.Status, r.Detail)
		}
	}

	cache := findCheck(results, "merge.cache_health")
	if cache == nil {
		t.Fatal("merge.cache_health missing")
	}
	for _, want := range []string{"size=47", "hit_rate=23.50%", "2026-05-05"} {
		if !strings.Contains(cache.Detail, want) {
			t.Errorf("cache_health missing %q in detail: %q", want, cache.Detail)
		}
	}
}

func TestRunMergeChecks_DaemonUnreachable(t *testing.T) {
	c := &fakeMergeClient{cacheErr: errors.New("dial unix: no such file")}
	results := runMergeChecks(context.Background(), c)
	if len(results) != 4 {
		t.Fatalf("expected 4 checks, got %d", len(results))
	}
	failures := 0
	for _, r := range results {
		if r.Status == "fail" {
			failures++
		}
	}
	if failures < 3 {
		t.Errorf("expected ≥3 failures (daemon_up + eventlog_writable + cache_health); got %d", failures)
	}
	if du := findCheck(results, "merge.daemon_up"); du == nil || du.Status != "fail" {
		t.Errorf("daemon_up should be fail; got %+v", du)
	} else if !strings.Contains(du.Detail, "dial unix") {
		t.Errorf("daemon error should surface in detail: %q", du.Detail)
	} else if du.Hint == "" {
		t.Error("daemon_up failure should carry an operator Hint")
	}

	if gv := findCheck(results, "merge.git_version"); gv == nil {
		t.Error("git_version missing even when daemon down")
	}

	for _, name := range []string{"merge.eventlog_writable", "merge.cache_health"} {
		r := findCheck(results, name)
		if r == nil {
			t.Errorf("%s missing", name)
			continue
		}
		if r.Status != "fail" {
			t.Errorf("%s should be fail when daemon unreachable; got %q", name, r.Status)
		}
		if r.Hint == "" {
			t.Errorf("%s failure should carry a Hint", name)
		}
	}
}

func TestRunMergeChecks_GitVersion(t *testing.T) {
	c := &fakeMergeClient{cacheStatus: &MergeCacheStatusResult{}}
	results := runMergeChecks(context.Background(), c)
	gv := findCheck(results, "merge.git_version")
	if gv == nil {
		t.Fatal("git_version row missing")
	}
	if gv.Status != "ok" {
		t.Errorf("git_version should be ok on CI host; got %q (detail=%q)", gv.Status, gv.Detail)
	}
	if !strings.Contains(gv.Detail, "2.40") {
		t.Errorf("git_version detail missing '2.40' sentinel: %q", gv.Detail)
	}
}

func TestRunMergeChecks_SlowDaemon(t *testing.T) {
	c := &slowMergeClient{
		fakeMergeClient: fakeMergeClient{cacheStatus: &MergeCacheStatusResult{
			Size: 1, HitRatePct: 10.0, LastRebuilt: "2026-05-05T00:00:00Z",
		}},

		delay: 150 * time.Millisecond,
	}
	results := runMergeChecks(context.Background(), c)
	du := findCheck(results, "merge.daemon_up")
	if du == nil {
		t.Fatal("daemon_up missing")
	}
	if du.Status != "warn" {
		t.Errorf("slow daemon should render status=warn; got %q", du.Status)
	}
	if !strings.Contains(du.Detail, "slow") {
		t.Errorf("slow daemon detail should mention 'slow'; got %q", du.Detail)
	}
	if du.Hint == "" {
		t.Error("warn rows should carry a Hint")
	}
}

func TestRunMergeChecks_CacheRebuildError(t *testing.T) {
	c := &fakeMergeClient{cacheStatus: &MergeCacheStatusResult{
		Size:         0,
		HitRatePct:   0.0,
		LastRebuilt:  "2026-05-05T10:00:00Z",
		RebuildError: "eventlog scan: file truncated",
	}}
	results := runMergeChecks(context.Background(), c)
	ch := findCheck(results, "merge.cache_health")
	if ch == nil {
		t.Fatal("cache_health missing")
	}
	if ch.Status != "fail" {
		t.Errorf("rebuild_error should bump cache_health to fail; got %q", ch.Status)
	}
	if !strings.Contains(ch.Detail, "rebuild_error") {
		t.Errorf("rebuild_error label missing in detail: %q", ch.Detail)
	}
	if !strings.Contains(ch.Detail, "file truncated") {
		t.Errorf("underlying rebuild error text missing in detail: %q", ch.Detail)
	}
	if ch.Hint == "" {
		t.Error("rebuild_error fail should carry a remediation Hint")
	}

	if du := findCheck(results, "merge.daemon_up"); du == nil || du.Status != "ok" {
		t.Errorf("daemon_up should still be ok on rebuild_error; got %+v", du)
	}
	if ew := findCheck(results, "merge.eventlog_writable"); ew == nil || ew.Status != "ok" {
		t.Errorf("eventlog_writable should still be ok on rebuild_error; got %+v", ew)
	}
}

func TestRunMergeChecks_NilCacheStatus(t *testing.T) {
	c := &fakeMergeClient{cacheStatus: nil, cacheErr: nil}
	results := runMergeChecks(context.Background(), c)
	ch := findCheck(results, "merge.cache_health")
	if ch == nil {
		t.Fatal("cache_health missing")
	}
	if ch.Status != "fail" {
		t.Errorf("nil status should bump cache_health to fail; got %q", ch.Status)
	}
	if !strings.Contains(ch.Detail, "nil status") {
		t.Errorf("nil-status sentinel missing in detail: %q", ch.Detail)
	}
	if ch.Hint == "" {
		t.Error("nil-status fail should carry a Hint")
	}
}

func TestRunMergeChecks_GitNotFound(t *testing.T) {
	prev := os.Getenv("PATH")
	emptyDir := t.TempDir()
	if err := os.Setenv("PATH", emptyDir); err != nil {
		t.Fatalf("setenv PATH: %v", err)
	}
	t.Cleanup(func() { _ = os.Setenv("PATH", prev) })

	c := &fakeMergeClient{cacheStatus: &MergeCacheStatusResult{}}
	results := runMergeChecks(context.Background(), c)
	gv := findCheck(results, "merge.git_version")
	if gv == nil {
		t.Fatal("git_version row missing")
	}
	if gv.Status != "fail" {
		t.Errorf("git_version should be fail when git not on PATH; got %q (detail=%q)", gv.Status, gv.Detail)
	}
	if gv.Hint == "" {
		t.Error("git-not-found fail should carry a Hint")
	}
}

func TestRunMergeChecks_GitVersionTooOld(t *testing.T) {
	if runtimeIsWindows() {
		t.Skip("fake git script uses POSIX shebang; skip on Windows")
	}
	prev := os.Getenv("PATH")
	dir := t.TempDir()
	gitPath := dir + "/git"

	script := "#!/bin/sh\necho 'git version 2.20.0'\nexit 0\n"
	if err := os.WriteFile(gitPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake git: %v", err)
	}
	if err := os.Setenv("PATH", dir); err != nil {
		t.Fatalf("setenv PATH: %v", err)
	}
	t.Cleanup(func() { _ = os.Setenv("PATH", prev) })

	c := &fakeMergeClient{cacheStatus: &MergeCacheStatusResult{}}
	results := runMergeChecks(context.Background(), c)
	gv := findCheck(results, "merge.git_version")
	if gv == nil {
		t.Fatal("git_version row missing")
	}
	if gv.Status != "fail" {
		t.Errorf("git_version should be fail when git is too old; got %q (detail=%q)", gv.Status, gv.Detail)
	}
	if gv.Hint == "" {
		t.Error("git-too-old fail should carry a Hint")
	}
}

func runtimeIsWindows() bool { return os.PathSeparator == '\\' }

type slowMergeClient struct {
	fakeMergeClient
	delay time.Duration
}

func (s *slowMergeClient) CacheStatus(ctx context.Context) (*MergeCacheStatusResult, error) {
	timer := time.NewTimer(s.delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return s.fakeMergeClient.CacheStatus(ctx)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
