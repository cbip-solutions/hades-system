package loretrailer_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	lt "github.com/cbip-solutions/hades-system/internal/doctrine/lint/analyzers/loretrailer"
)

type commitSpec struct {
	body  string
	files map[string]string
}

func initRepoWithCommits(t *testing.T, commits []commitSpec) string {
	t.Helper()
	dir := t.TempDir()
	gitOK := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	gitOK("init", "-q", "-b", "main")
	gitOK("config", "user.email", "t@example.com")
	gitOK("config", "user.name", "t")
	gitOK("config", "commit.gpgsign", "false")
	for _, c := range commits {
		for path, content := range c.files {
			full := filepath.Join(dir, path)
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
				t.Fatalf("write %s: %v", path, err)
			}
			gitOK("add", path)
		}
		gitOK("commit", "-q", "-m", c.body)
	}
	return dir
}

func TestEnforcementDisabledByDefault(t *testing.T) {
	dir := initRepoWithCommits(t, []commitSpec{
		{body: "feat(core): touch a hub with no lore", files: map[string]string{"internal/core/hub.go": "package core\n"}},
	})
	diags, err := lt.RunWithGitDir(dir, lt.Options{
		Enabled:       false,
		HighRiskFiles: []string{"internal/core/*.go"},
		BaseRef:       "",
		Depth:         10,
	})
	if err != nil {
		t.Fatalf("RunWithGitDir: %v", err)
	}
	if len(diags) != 0 {
		t.Errorf("enabled=false reported %d diagnostics; want 0 (adoption-gated default OFF)", len(diags))
	}
}

func TestEnforcementFlagsMissingConstraintOnHighRiskFile(t *testing.T) {
	dir := initRepoWithCommits(t, []commitSpec{
		{body: "feat(core): modify hub no lore", files: map[string]string{"internal/core/hub.go": "package core\n"}},
	})
	diags, err := lt.RunWithGitDir(dir, lt.Options{
		Enabled:       true,
		HighRiskFiles: []string{"internal/core/*.go"},
		Depth:         10,
	})
	if err != nil {
		t.Fatalf("RunWithGitDir: %v", err)
	}
	if len(diags) != 1 {
		t.Fatalf("got %d diagnostics; want 1", len(diags))
	}
	if diags[0].Message == "" || !strings.Contains(diags[0].Message, "Lore-Constraint") {
		t.Errorf("diagnostic message %q does not mention Lore-Constraint", diags[0].Message)
	}
}

func TestEnforcementPassesWhenConstraintPresent(t *testing.T) {
	dir := initRepoWithCommits(t, []commitSpec{
		{body: "feat(core): modify hub\n\nLore-Constraint: hub stays lock-free\n", files: map[string]string{"internal/core/hub.go": "package core\n"}},
	})
	diags, err := lt.RunWithGitDir(dir, lt.Options{Enabled: true, HighRiskFiles: []string{"internal/core/*.go"}, Depth: 10})
	if err != nil {
		t.Fatalf("RunWithGitDir: %v", err)
	}
	if len(diags) != 0 {
		t.Errorf("got %d diagnostics; want 0 (constraint present)", len(diags))
	}
}

func TestEnforcementIgnoresLowRiskFile(t *testing.T) {
	dir := initRepoWithCommits(t, []commitSpec{
		{body: "docs(x): tweak readme", files: map[string]string{"README.md": "# x\n"}},
	})
	diags, err := lt.RunWithGitDir(dir, lt.Options{Enabled: true, HighRiskFiles: []string{"internal/core/*.go"}, Depth: 10})
	if err != nil {
		t.Fatalf("RunWithGitDir: %v", err)
	}
	if len(diags) != 0 {
		t.Errorf("got %d diagnostics; want 0 (low-risk file)", len(diags))
	}
}

func TestEnforcementEmptyHighRiskSetIsNoOp(t *testing.T) {
	dir := initRepoWithCommits(t, []commitSpec{
		{body: "feat(core): hub no lore", files: map[string]string{"internal/core/hub.go": "package core\n"}},
	})
	diags, err := lt.RunWithGitDir(dir, lt.Options{Enabled: true, HighRiskFiles: nil, Depth: 10})
	if err != nil {
		t.Fatalf("RunWithGitDir: %v", err)
	}
	if len(diags) != 0 {
		t.Errorf("got %d diagnostics; want 0 (empty high-risk set)", len(diags))
	}
}

func TestEnforcementBaseRefScansOnlyBranchLocalCommits(t *testing.T) {

	dir := initRepoWithCommits(t, []commitSpec{
		{body: "feat(core): main touches hub — no lore — must be ignored under base-ref", files: map[string]string{"internal/core/hub.go": "package core\n"}},
	})

	gitOK := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	gitOK("checkout", "-q", "-b", "feature")
	if err := os.WriteFile(filepath.Join(dir, "internal/core/hub.go"), []byte("package core\n// v2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitOK("add", "internal/core/hub.go")

	gitOK("commit", "-q", "-m", "feat(core): branch clean\n\nLore-Constraint: hub stays lock-free\n")

	diags, err := lt.RunWithGitDir(dir, lt.Options{
		Enabled:       true,
		HighRiskFiles: []string{"internal/core/*.go"},
		BaseRef:       "main",
		Depth:         10,
	})
	if err != nil {
		t.Fatalf("RunWithGitDir: %v", err)
	}
	if len(diags) != 0 {
		t.Errorf("got %d diagnostics; want 0 (main commit ignored; branch commit has constraint)", len(diags))
	}
}

func TestEnforcementBaseRefFlagsViolationOnBranch(t *testing.T) {
	dir := initRepoWithCommits(t, []commitSpec{
		{body: "feat(core): main seed", files: map[string]string{"README.md": "# r\n"}},
	})
	gitOK := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	gitOK("checkout", "-q", "-b", "feature")
	if err := os.MkdirAll(filepath.Join(dir, "internal/core"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "internal/core/hub.go"), []byte("package core\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitOK("add", "internal/core/hub.go")
	gitOK("commit", "-q", "-m", "feat(core): branch touches hub with no lore")

	diags, err := lt.RunWithGitDir(dir, lt.Options{
		Enabled:       true,
		HighRiskFiles: []string{"internal/core/*.go"},
		BaseRef:       "main",
		Depth:         10,
	})
	if err != nil {
		t.Fatalf("RunWithGitDir: %v", err)
	}
	if len(diags) != 1 {
		t.Fatalf("got %d diagnostics; want 1 (branch-local violation)", len(diags))
	}
}

func TestEnforcementDoubleGlobStar(t *testing.T) {
	dir := initRepoWithCommits(t, []commitSpec{
		{body: "feat(core): deep file no lore", files: map[string]string{"internal/core/sub/deep.go": "package sub\n"}},
	})
	diags, err := lt.RunWithGitDir(dir, lt.Options{
		Enabled:       true,
		HighRiskFiles: []string{"internal/core/**"},
		Depth:         10,
	})
	if err != nil {
		t.Fatalf("RunWithGitDir: %v", err)
	}
	if len(diags) != 1 {
		t.Errorf("got %d diagnostics; want 1 (/** glob must match subdir)", len(diags))
	}
}

func TestAnalyzerName(t *testing.T) {
	if got := lt.Analyzer.Name; got != "loretrailer" {
		t.Errorf("Analyzer.Name = %q; want %q", got, "loretrailer")
	}
}

func TestAnalyzerDoc(t *testing.T) {
	doc := lt.Analyzer.Doc
	if doc == "" {
		t.Fatal("Analyzer.Doc is empty")
	}
	for _, want := range []string{"Lore-Constraint", "inv-zen-238", "enabled"} {
		if !strings.Contains(doc, want) {
			t.Errorf("Analyzer.Doc does not mention %q; got: %s", want, doc)
		}
	}
}

func TestAnalyzerHasEnabledFlag(t *testing.T) {
	f := lt.Analyzer.Flags.Lookup("enabled")
	if f == nil {
		t.Fatal("Analyzer.Flags does not expose -enabled flag")
	}
}

func TestAnalyzerHasHighRiskFilesFlag(t *testing.T) {
	f := lt.Analyzer.Flags.Lookup("high-risk-files")
	if f == nil {
		t.Fatal("Analyzer.Flags does not expose -high-risk-files flag")
	}
}

func TestRunWithGitDirResetOnce(t *testing.T) {
	dir := initRepoWithCommits(t, []commitSpec{
		{body: "feat(core): no lore", files: map[string]string{"internal/core/hub.go": "package core\n"}},
	})
	opts := lt.Options{Enabled: true, HighRiskFiles: []string{"internal/core/*.go"}, Depth: 10}
	lt.ResetOnceForTest()
	d1, err := lt.RunWithGitDir(dir, opts)
	if err != nil {
		t.Fatalf("RunWithGitDir #1: %v", err)
	}
	lt.ResetOnceForTest()
	d2, err := lt.RunWithGitDir(dir, opts)
	if err != nil {
		t.Fatalf("RunWithGitDir #2: %v", err)
	}
	if len(d1) != len(d2) {
		t.Errorf("second call (after ResetOnceForTest) returned %d diags; first returned %d; must be equal", len(d2), len(d1))
	}
}
