// tests/compliance/inv_zen_238_lore_enforcement_test.go
//
// inv-zen-238 (Plan 19 Phase I) — Lore enforcement (doctrine-tunable).
//
// Doctrine: per spec §10 + §21, the loretrailer go-vet analyzer MUST, WHEN
// ENABLED (-loretrailer.enabled=true), flag a branch-local commit that touches
// a high-risk node without a Lore-Constraint: git-trailer; and MUST be a no-op
// WHEN DISABLED (the adoption-gated default). The enforcement must also be
// reachable — the analyzer is registered in the zen-doctrine-lint binary via
// Plan19RegisteredAnalyzers().
//
// Two halves:
//  1. Behavior: drive loretrailer.RunWithGitDir over a hermetic temp repo with
//     a high-risk commit missing a constraint — enabled→1 diag, disabled→0.
//  2. Registration: assert cmd/zen-doctrine-lint/plan19_extension.go declares
//     loretrailer.Analyzer in plan19Analyzers (AST scan — the binary is
//     package main, not importable from compliance, so we scan the source).
//
// Companion: spec §10 (get_why Lore source); ADR-0111 (Caronte architecture).
package compliance

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	loretrailer "github.com/cbip-solutions/hades-system/internal/doctrine/lint/analyzers/loretrailer"
)

func TestInvZen238EnforcesWhenEnabled(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
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
	if err := os.MkdirAll(filepath.Join(dir, "internal", "core"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "internal", "core", "hub.go"), []byte("package core\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitOK("add", "internal/core/hub.go")
	gitOK("commit", "-q", "-m", "feat(core): touch hub without lore")

	high := []string{"internal/core/*.go"}

	loretrailer.ResetOnceForTest()
	diags, err := loretrailer.RunWithGitDir(dir, loretrailer.Options{Enabled: true, HighRiskFiles: high, Depth: 5})
	if err != nil {
		t.Fatalf("RunWithGitDir(enabled): %v", err)
	}
	if len(diags) != 1 {
		t.Fatalf("inv-zen-238: enabled enforcement flagged %d commits; want 1", len(diags))
	}
	if !strings.Contains(diags[0].Message, "Lore-Constraint") {
		t.Errorf("inv-zen-238: diagnostic %q must mention Lore-Constraint", diags[0].Message)
	}

	off, err := loretrailer.RunWithGitDir(dir, loretrailer.Options{Enabled: false, HighRiskFiles: high, Depth: 5})
	if err != nil {
		t.Fatalf("RunWithGitDir(disabled): %v", err)
	}
	if len(off) != 0 {
		t.Errorf("inv-zen-238: disabled enforcement flagged %d commits; want 0 (doctrine-tunable, default OFF)", len(off))
	}
}

func TestInvZen238AnalyzerRegisteredInBinary(t *testing.T) {
	root := repoRoot(t) // tests/compliance/invariants_test.go helper — do NOT redeclare
	src := filepath.Join(root, "cmd", "zen-doctrine-lint", "plan19_extension.go")
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("inv-zen-238: read %s: %v", src, err)
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, src, data, 0)
	if err != nil {
		t.Fatalf("inv-zen-238: parse %s: %v", src, err)
	}

	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if ok && pkg.Name == "loretrailer" && sel.Sel.Name == "Analyzer" {
			found = true
		}
		return true
	})
	if !found {
		t.Error("inv-zen-238: plan19_extension.go does not register loretrailer.Analyzer — enforcement is unreachable")
	}

	if !strings.Contains(string(data), "func Plan19RegisteredAnalyzers()") {
		t.Error("inv-zen-238: plan19_extension.go missing Plan19RegisteredAnalyzers() export")
	}
}
