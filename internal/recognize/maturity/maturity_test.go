package maturity

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func initGitRepo(t *testing.T, dir string, commits int) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	runGit := func(args ...string) {
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
	runGit("init", "-q")
	runGit("config", "user.email", "test@example.com")
	runGit("config", "user.name", "test")
	runGit("config", "commit.gpgsign", "false")
	for i := 0; i < commits; i++ {
		f := filepath.Join(dir, "f.txt")
		if err := os.WriteFile(f, []byte("v"+itoa(i)+"\n"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		runGit("add", ".")
		runGit("commit", "-q", "-m", "commit "+itoa(i))
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var d []byte
	for i > 0 {
		d = append([]byte{byte('0' + i%10)}, d...)
		i /= 10
	}
	return string(d)
}

func TestProbe_CommitCount(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir, 3)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m, err := Probe(ctx, dir)
	if err != nil {
		t.Fatalf("Probe err: %v", err)
	}
	if m.CommitCount != 3 {
		t.Errorf("CommitCount = %d; want 3", m.CommitCount)
	}
	if m.LastCommitISO8601 == "" {
		t.Error("LastCommitISO8601 empty; want non-empty")
	}
}

func TestProbe_NoGitRepo(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m, err := Probe(ctx, dir)
	if err != nil {
		t.Fatalf("Probe err: %v", err)
	}
	if m.CommitCount != -1 {
		t.Errorf("CommitCount = %d; want -1 for non-git dir", m.CommitCount)
	}
	if m.LastCommitISO8601 != "" {
		t.Errorf("LastCommitISO8601 = %q; want \"\"", m.LastCommitISO8601)
	}
}

func TestProbe_CIDetected_GitHubActions(t *testing.T) {
	dir := t.TempDir()
	ciDir := filepath.Join(dir, ".github", "workflows")
	if err := os.MkdirAll(ciDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ciDir, "ci.yml"), []byte("name: CI\non: push\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m, _ := Probe(ctx, dir)
	if !m.HasCI {
		t.Error("HasCI = false; want true")
	}
	if m.CIPlatform != "github-actions" {
		t.Errorf("CIPlatform = %q; want github-actions", m.CIPlatform)
	}
}

func TestProbe_CIDetected_GitLab(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".gitlab-ci.yml"), []byte("stages: [test]\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m, _ := Probe(ctx, dir)
	if !m.HasCI || m.CIPlatform != "gitlab" {
		t.Errorf("Probe = %+v; want HasCI=true CIPlatform=gitlab", m)
	}
}

func TestProbe_CIDetected_CircleCI(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".circleci"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".circleci", "config.yml"), []byte("version: 2.1\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m, _ := Probe(ctx, dir)
	if !m.HasCI || m.CIPlatform != "circleci" {
		t.Errorf("Probe = %+v; want HasCI=true CIPlatform=circleci", m)
	}
}

func TestProbe_NoCI(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m, _ := Probe(ctx, dir)
	if m.HasCI {
		t.Error("HasCI = true; want false")
	}
	if m.CIPlatform != "" {
		t.Errorf("CIPlatform = %q; want \"\"", m.CIPlatform)
	}
}

func TestProbe_ContextCanceled(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir, 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m, err := Probe(ctx, dir)

	if err == nil && m.CommitCount > 0 {
		t.Errorf("expected ctx-cancelled signal; got m=%+v err=nil", m)
	}
}

func TestProbe_AzurePipelines(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "azure-pipelines.yml"), []byte("trigger: main\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m, _ := Probe(ctx, dir)
	if m.CIPlatform != "azure-pipelines" {
		t.Errorf("CIPlatform = %q; want azure-pipelines", m.CIPlatform)
	}
}

func TestProbe_Jenkins(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Jenkinsfile"), []byte("pipeline {}\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m, _ := Probe(ctx, dir)
	if m.CIPlatform != "jenkins" {
		t.Errorf("CIPlatform = %q; want jenkins", m.CIPlatform)
	}
}
