package compliance

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/doctrine"
)

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func initRepoWithSchema(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "internal", "doctrine"), 0o755); err != nil {
		t.Fatal(err)
	}
	baseline := `package doctrine

type Schema struct {
	Research   ResearchAxis   ` + "`toml:\"research\"`" + `
	Subprocess SubprocessAxis ` + "`toml:\"subprocess\"`" + `
	Reviewer   ReviewerAxis   ` + "`toml:\"reviewer\"`" + `
	Budget     BudgetAxis     ` + "`toml:\"budget\"`" + `
}
type ResearchAxis struct{}
type SubprocessAxis struct{}
type ReviewerAxis struct{}
type BudgetAxis struct{}
`
	if err := os.WriteFile(filepath.Join(dir, "internal", "doctrine", "schema.go"), []byte(baseline), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "test")
	runGit(t, dir, "config", "commit.gpgsign", "false")
	runGit(t, dir, "add", "internal/doctrine/schema.go")
	runGit(t, dir, "commit", "-q", "-m", "feat(doctrine): baseline schema")
	return dir
}

func TestInvZen084AdditiveCommitOK(t *testing.T) {
	dir := initRepoWithSchema(t)
	additive := `package doctrine

type Schema struct {
	Research   ResearchAxis   ` + "`toml:\"research\"`" + `
	Subprocess SubprocessAxis ` + "`toml:\"subprocess\"`" + `
	Reviewer   ReviewerAxis   ` + "`toml:\"reviewer\"`" + `
	Budget     BudgetAxis     ` + "`toml:\"budget\"`" + `
	Apply      ApplyAxis      ` + "`toml:\"apply\"`" + `
}
type ResearchAxis struct{}
type SubprocessAxis struct{}
type ReviewerAxis struct{}
type BudgetAxis struct{}
type ApplyAxis struct{}
`
	if err := os.WriteFile(filepath.Join(dir, "internal", "doctrine", "schema.go"), []byte(additive), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "internal/doctrine/schema.go")
	runGit(t, dir, "commit", "-q", "-m", "feat(doctrine): add apply axis")
	res, err := doctrine.ValidateRange(dir, "HEAD~1", "HEAD")
	if err != nil {
		t.Fatalf("ValidateRange: %v", err)
	}
	if !res.OK {
		t.Errorf("OK = false, want true; violations=%v", res.Violations)
	}
}

func TestInvZen084RemovalRejected(t *testing.T) {
	dir := initRepoWithSchema(t)
	removed := `package doctrine

type Schema struct {
	Research   ResearchAxis   ` + "`toml:\"research\"`" + `
	Subprocess SubprocessAxis ` + "`toml:\"subprocess\"`" + `
	Reviewer   ReviewerAxis   ` + "`toml:\"reviewer\"`" + `
}
type ResearchAxis struct{}
type SubprocessAxis struct{}
type ReviewerAxis struct{}
`
	if err := os.WriteFile(filepath.Join(dir, "internal", "doctrine", "schema.go"), []byte(removed), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "internal/doctrine/schema.go")
	runGit(t, dir, "commit", "-q", "-m", "fix(doctrine): drop budget axis")
	res, err := doctrine.ValidateRange(dir, "HEAD~1", "HEAD")
	if err != nil {
		t.Fatalf("ValidateRange: %v", err)
	}
	if res.OK {
		t.Errorf("OK = true, want false (removal without ADR)")
	}
	if len(res.Violations) != 1 || !strings.Contains(res.Violations[0], "budget") {
		t.Errorf("violations = %v, want one entry mentioning 'budget'", res.Violations)
	}
}

func TestInvZen084RemovalWithADROK(t *testing.T) {
	dir := initRepoWithSchema(t)
	removed := `package doctrine

type Schema struct {
	Research   ResearchAxis   ` + "`toml:\"research\"`" + `
	Subprocess SubprocessAxis ` + "`toml:\"subprocess\"`" + `
	Reviewer   ReviewerAxis   ` + "`toml:\"reviewer\"`" + `
}
type ResearchAxis struct{}
type SubprocessAxis struct{}
type ReviewerAxis struct{}
`
	if err := os.WriteFile(filepath.Join(dir, "internal", "doctrine", "schema.go"), []byte(removed), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "internal/doctrine/schema.go")
	runGit(t, dir, "commit", "-q", "-m", `refactor(doctrine): drop budget axis

ADR: docs/decisions/0008-doctrine-schema-budget-removal.md`)
	res, err := doctrine.ValidateRange(dir, "HEAD~1", "HEAD")
	if err != nil {
		t.Fatalf("ValidateRange: %v", err)
	}
	if !res.OK {
		t.Errorf("OK = false, want true; violations=%v", res.Violations)
	}
}
