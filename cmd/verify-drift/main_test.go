package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/safetynet"
)

func TestVerifyDriftBuilds(t *testing.T) {
	t.Parallel()
	cmd := exec.Command("go", "build", "-o", os.DevNull, "github.com/cbip-solutions/hades-system/cmd/verify-drift")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %v\n%s", err, out)
	}
}

func TestStderrEmit_Emit(t *testing.T) {
	t.Parallel()
	if err := (stderrEmit{}).Emit(context.Background(), safetynet.Event{}); err != nil {
		t.Errorf("Emit returned %v want nil", err)
	}
}

func TestGitSource_Recent_RealRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	mustGit := func(args ...string) {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = dir
		c.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=zen", "GIT_AUTHOR_EMAIL=zen@local",
			"GIT_COMMITTER_NAME=zen", "GIT_COMMITTER_EMAIL=zen@local",
		)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	mustGit("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit("add", "README")
	mustGit("commit", "-q", "-m", "feat(x): initial")
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("ok2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit("add", "README")
	mustGit("commit", "-q", "-m", "fix(y): tweak\n\ndetail body")

	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })

	got, err := (gitSource{}).Recent(context.Background(), 5)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len=%d want 2: %+v", len(got), got)
	}
	if got[0].Subject != "fix(y): tweak" {
		t.Errorf("HEAD subject = %q", got[0].Subject)
	}
	if !strings.Contains(got[0].Body, "detail body") {
		t.Errorf("HEAD body missing 'detail body': %q", got[0].Body)
	}
	if got[1].Subject != "feat(x): initial" {
		t.Errorf("HEAD~1 subject = %q", got[1].Subject)
	}
}

func TestGitSource_Recent_NotARepo(t *testing.T) {
	dir := t.TempDir()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
	if _, err := (gitSource{}).Recent(context.Background(), 1); err == nil {
		t.Fatal("want error in non-repo")
	}
}
