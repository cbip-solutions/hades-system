package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadQuarantine_FileMissing(t *testing.T) {
	t.Parallel()
	_, err := loadQuarantine("/tmp/this-quarantine-file-does-not-exist-zen-test")
	if err == nil {
		t.Fatalf("expected error for missing quarantine file")
	}
}

func TestLoadQuarantine_FileWithEntries(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "quarantine.txt")
	content := "# Plan 15 Phase G G-3 flake quarantine\n" +
		"# Last review: 2026-05-25T00:00:00Z\n" +
		"TestKnownFlake\n" +
		"TestAnotherFlake.*intermittent\n" +
		"# comment\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	lines, err := loadQuarantine(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(lines) != 2 {
		t.Errorf("expected 2 quarantine entries; got %d: %v", len(lines), lines)
	}
	if lines[0] != "TestKnownFlake" {
		t.Errorf("entry[0]: got %s", lines[0])
	}
}

func TestRun_HappyPath_TextOutput(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	srv, base := stubGHForRun(t, 50, 0)
	defer srv.Close()
	t.Setenv("GH_API_BASE_URL", base)
	t.Setenv("GITHUB_TOKEN", "")

	resetHTTPClientForTest()

	buf := &bytes.Buffer{}
	err := run(context.Background(), runOptions{
		owner:          "test",
		repo:           "repo",
		branch:         "main",
		windowSize:     50,
		emitJSON:       false,
		quarantinePath: "",
		stdout:         buf,
		stderr:         &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "PASS") {
		t.Errorf("output should mention PASS; got: %s", out)
	}
	if !strings.Contains(out, "30-CI-green") {
		t.Errorf("output should mention 30-CI-green; got: %s", out)
	}
	if !strings.Contains(out, "inv-zen-275") {
		t.Errorf("output should reference inv-zen-275; got: %s", out)
	}
}

func TestRun_FailPath_RealFailExceeded(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	srv, base := stubGHForRun(t, 47, 3)
	defer srv.Close()
	t.Setenv("GH_API_BASE_URL", base)

	resetHTTPClientForTest()

	buf := &bytes.Buffer{}
	err := run(context.Background(), runOptions{
		owner:      "test",
		repo:       "repo",
		branch:     "main",
		windowSize: 50,
		stdout:     buf,
		stderr:     &bytes.Buffer{},
	})
	if err == nil {
		t.Fatal("expected error on 3 real failures > 2 max; got nil")
	}
	if !strings.Contains(err.Error(), "FAIL") {
		t.Errorf("error should mention FAIL; got: %v", err)
	}
}

func TestRun_JSONOutput(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	srv, base := stubGHForRun(t, 50, 0)
	defer srv.Close()
	t.Setenv("GH_API_BASE_URL", base)

	resetHTTPClientForTest()

	buf := &bytes.Buffer{}
	err := run(context.Background(), runOptions{
		owner:      "test",
		repo:       "repo",
		branch:     "main",
		windowSize: 50,
		emitJSON:   true,
		stdout:     buf,
		stderr:     &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var report map[string]any
	if err := json.Unmarshal(buf.Bytes(), &report); err != nil {
		t.Fatalf("parse JSON output: %v\noutput: %s", err, buf.String())
	}
	if pass, _ := report["pass"].(bool); !pass {
		t.Errorf("JSON report.pass should be true; got: %v", report["pass"])
	}
	if _, ok := report["window"]; !ok {
		t.Errorf("JSON report should include window; got keys: %v", report)
	}
}

func TestRun_InsufficientSample(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	srv, base := stubGHForRun(t, 20, 0)
	defer srv.Close()
	t.Setenv("GH_API_BASE_URL", base)

	resetHTTPClientForTest()

	buf := &bytes.Buffer{}
	err := run(context.Background(), runOptions{
		owner:      "test",
		repo:       "repo",
		branch:     "main",
		windowSize: 20,
		stdout:     buf,
		stderr:     &bytes.Buffer{},
	})
	if err == nil {
		t.Fatal("expected error on insufficient sample; got nil")
	}
	if !strings.Contains(err.Error(), "sample") {
		t.Errorf("error should mention sample shortage; got: %v", err)
	}
}

func TestRun_GHAPIError(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/test/repo/commits", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	t.Setenv("GH_API_BASE_URL", srv.URL)

	resetHTTPClientForTest()

	buf := &bytes.Buffer{}
	err := run(context.Background(), runOptions{
		owner: "test", repo: "repo", branch: "main", windowSize: 5,
		stdout: buf, stderr: &bytes.Buffer{},
	})
	if err == nil {
		t.Fatal("expected GH API error; got nil")
	}
	if !strings.Contains(err.Error(), "fetch commits") {
		t.Errorf("error should describe GH API failure; got: %v", err)
	}
}

func TestRun_QuarantineFileMissingIsNonFatal(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	srv, base := stubGHForRun(t, 50, 0)
	defer srv.Close()
	t.Setenv("GH_API_BASE_URL", base)

	resetHTTPClientForTest()

	stderr := &bytes.Buffer{}
	stdout := &bytes.Buffer{}
	err := run(context.Background(), runOptions{
		owner: "test", repo: "repo", branch: "main", windowSize: 50,
		quarantinePath: "/tmp/nonexistent-quarantine-zen-test-file",
		stdout:         stdout, stderr: stderr,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(stderr.String(), "warn") {
		t.Errorf("stderr should warn about missing quarantine; got: %s", stderr.String())
	}
}

func TestRun_QuarantineWithEntries(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	srv, base := stubGHForRunWithReason(t, 47, 3, "TestKnownFlake intermittently fails")
	defer srv.Close()
	t.Setenv("GH_API_BASE_URL", base)

	resetHTTPClientForTest()

	qPath := filepath.Join(tmp, "quarantine.txt")

	content := "# Last review: " + time.Now().UTC().Format(time.RFC3339) + "\n" +
		"TestKnownFlake\n"
	if err := os.WriteFile(qPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write quarantine: %v", err)
	}

	buf := &bytes.Buffer{}
	err := run(context.Background(), runOptions{
		owner: "test", repo: "repo", branch: "main", windowSize: 50,
		quarantinePath: qPath,
		stdout:         buf, stderr: &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("run with flake-bucketed failures should pass; got: %v\n%s", err, buf.String())
	}
}

func TestLoadQuarantine_ThreeTokenFormExtractsFirstToken(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "quarantine.txt")

	content := "# Last review: 2026-05-25T00:00:00Z\n" +
		"TestSpecFormatFlaky 2026-05-20T00:00:00Z network-timeout\n" +
		"TestAnotherSpec  2026-05-22T00:00:00Z gha-runner-flake\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	lines, err := loadQuarantine(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("expected 2 entries; got %d: %v", len(lines), lines)
	}
	if lines[0] != "TestSpecFormatFlaky" {
		t.Errorf("entry[0]: got %q; want TestSpecFormatFlaky", lines[0])
	}
	if lines[1] != "TestAnotherSpec" {
		t.Errorf("entry[1]: got %q; want TestAnotherSpec", lines[1])
	}
}

func TestRun_StdoutStderrDefaults(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	srv, base := stubGHForRun(t, 50, 0)
	defer srv.Close()
	t.Setenv("GH_API_BASE_URL", base)

	resetHTTPClientForTest()

	err := run(context.Background(), runOptions{
		owner: "test", repo: "repo", branch: "main", windowSize: 50,
		stdout: nil, stderr: nil,
	})
	if err != nil {
		t.Fatalf("run with nil stdout/stderr: %v", err)
	}
}

func TestRun_TimeoutZeroDefault(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	srv, base := stubGHForRun(t, 50, 0)
	defer srv.Close()
	t.Setenv("GH_API_BASE_URL", base)

	resetHTTPClientForTest()

	err := run(context.Background(), runOptions{
		owner: "test", repo: "repo", branch: "main", windowSize: 50,
		timeout: 0,
		stdout:  &bytes.Buffer{}, stderr: &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("run with zero timeout: %v", err)
	}
}

func stubGHForRun(t *testing.T, n, nfail int) (*httptest.Server, string) {
	t.Helper()
	return stubGHForRunWithReason(t, n, nfail, "test regression")
}

func stubGHForRunWithReason(t *testing.T, n, nfail int, failureReason string) (*httptest.Server, string) {
	t.Helper()
	type ghCommit struct {
		SHA    string `json:"sha"`
		Commit struct {
			Committer struct {
				Date time.Time `json:"date"`
			} `json:"committer"`
		} `json:"commit"`
	}
	commits := make([]ghCommit, 0, n+nfail)
	now := time.Now().UTC()
	for i := 0; i < n+nfail; i++ {
		var c ghCommit
		c.SHA = paddedHex(i)
		c.Commit.Committer.Date = now.Add(-time.Duration(i) * time.Hour)
		commits = append(commits, c)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/test/repo/commits", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(commits)
	})
	for i := 0; i < n+nfail; i++ {
		sha := paddedHex(i)
		isFailure := i >= n
		mux.HandleFunc("/repos/test/repo/commits/"+sha+"/check-runs", func(w http.ResponseWriter, r *http.Request) {
			run := map[string]any{
				"status":     "completed",
				"conclusion": "success",
				"name":       "build",
				"html_url":   "https://example/run/" + sha,
			}
			if isFailure {
				run["conclusion"] = "failure"
				run["name"] = "tests"
				run["output"] = map[string]any{"summary": failureReason}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"check_runs": []map[string]any{run},
			})
		})
	}
	srv := httptest.NewServer(mux)
	return srv, srv.URL
}

func paddedHex(i int) string {
	out := []byte("0000000000000000000000000000000000000000")
	hex := []byte("0123456789abcdef")
	for pos := 39; pos >= 30 && i > 0; pos-- {
		out[pos] = hex[i&0xf]
		i >>= 4
	}
	return string(out)
}

func resetHTTPClientForTest() {

	ciReset()
}
