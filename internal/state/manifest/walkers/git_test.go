package walkers

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func newFixtureGitRepo(t *testing.T, tags []string, branches []string) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--initial-branch=main", ".")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "test")
	run("commit", "--allow-empty", "-m", "initial")
	for _, tag := range tags {
		run("tag", tag)
	}
	for _, b := range branches {
		run("branch", b)
	}
	return dir
}

func TestGitWalker_ReleasedTags(t *testing.T) {
	repo := newFixtureGitRepo(t,
		[]string{"v0.1.0", "v0.2.2", "v0.3.0", "not-a-version"},
		nil,
	)
	w := NewGitWalker(repo)
	result, err := w.Walk(context.Background())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if got := strings.Join(result.Released, ","); got != "v0.1.0,v0.2.2,v0.3.0" {
		t.Errorf("Released: got %q, want sorted v0.1.0,v0.2.2,v0.3.0", got)
	}
}

func TestGitWalker_InProgressBranches(t *testing.T) {
	repo := newFixtureGitRepo(t, nil,
		[]string{"plan-7-execute", "plan-9-execute", "feature/x"})
	w := NewGitWalker(repo)
	result, err := w.Walk(context.Background())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if got := strings.Join(result.InProgress, ","); got != "plan-7,plan-9" {
		t.Errorf("InProgress: got %q, want plan-7,plan-9", got)
	}
}

func TestGitWalker_GitUnreachable_ReportsMissing(t *testing.T) {
	w := NewGitWalker("/nonexistent/repo")
	result, err := w.Walk(context.Background())
	if err != nil {

		t.Fatalf("Walk: %v (expected nil)", err)
	}
	if !contains(result.MissingSources, "git") {
		t.Errorf("MissingSources: got %v, want to contain 'git'", result.MissingSources)
	}
	if len(result.Released) != 0 || len(result.InProgress) != 0 {
		t.Errorf("expected empty lists on missing git, got %+v", result)
	}
}

func TestGitWalker_BrainstormPendingFromCoordinationDoc(t *testing.T) {
	repo := newFixtureGitRepo(t, nil, nil)
	docPath := filepath.Join(repo, "docs", "operations", "parallel-execution-coordination.md")
	if err := writeFile(docPath, []byte(`# Coordination

| Plan | Status |
|---|---|
| plan-10 | brainstorm-pending |
| plan-11 | brainstorm-pending |
| plan-12 | brainstorm-pending |
`)); err != nil {
		t.Fatal(err)
	}
	w := NewGitWalker(repo)
	result, err := w.Walk(context.Background())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if got := strings.Join(result.BrainstormPending, ","); got != "plan-10,plan-11,plan-12" {
		t.Errorf("BrainstormPending: got %q", got)
	}
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
