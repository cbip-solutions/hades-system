package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func initTempGitRepoWithCommits(t *testing.T, n int) (string, []string) {
	t.Helper()
	dir := t.TempDir()
	for _, c := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "test"},
		{"config", "commit.gpgsign", "false"},
	} {
		cmd := exec.Command("git", c...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", c, err, out)
		}
	}
	hashes := make([]string, n)
	for i := 0; i < n; i++ {
		fname := filepath.Join(dir, "f.txt")
		if err := os.WriteFile(fname, []byte(strings.Repeat("x", i+1)), 0o600); err != nil {
			t.Fatal(err)
		}
		for _, args := range [][]string{
			{"add", "f.txt"},
			{"commit", "-q", "-m", "step"},
		} {
			cmd := exec.Command("git", args...)
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v\n%s", args, err, out)
			}
		}

		cmd := exec.Command("git", "rev-parse", "HEAD")
		cmd.Dir = dir
		out, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		hashes[i] = strings.TrimSpace(string(out))
	}
	return dir, hashes
}

func TestComputeMergeBaseLinearHistory(t *testing.T) {
	dir, hashes := initTempGitRepoWithCommits(t, 3)
	got, err := computeMergeBase(dir, "HEAD~1", "HEAD")
	if err != nil {
		t.Fatalf("computeMergeBase: %v", err)
	}
	want := hashes[1]
	if got != want {
		t.Errorf("merge-base = %q, want %q", got, want)
	}
}

func TestComputeMergeBaseInvalidRef(t *testing.T) {
	dir, _ := initTempGitRepoWithCommits(t, 1)
	_, err := computeMergeBase(dir, "no-such-ref", "HEAD")
	if err == nil {
		t.Error("computeMergeBase(invalid) returned nil error")
	}
}

func TestComputeMergeBaseNonRepo(t *testing.T) {
	dir := t.TempDir()
	_, err := computeMergeBase(dir, "HEAD", "HEAD")
	if err == nil {
		t.Error("computeMergeBase(non-repo) returned nil error")
	}
}
