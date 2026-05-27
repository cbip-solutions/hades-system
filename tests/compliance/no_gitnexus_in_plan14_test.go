// tests/compliance/no_gitnexus_in_plan14_test.go
//
// Per ADR-0007 (vendor mode): project-RAG is orthogonal to ecosystem-RAG.
// project-level code intelligence; owns ecosystem docs only.
//
// Checked packages:
// - internal/research/ecosystem/...
// - cmd/zen-docs-cron/... (cron worker)
// - internal/cli/ files: knowledge_remote.go, memory_*.go, specs_*.go, docs_*.go,
// doctor_ecosystem.go
// - internal/mcp/research/ecosystem_docs.go (the single MCP binding
// rewired in ; the rest of internal/mcp/research/ is the
// parent MCP which owns the long-lived `gitnexus mcp` child subprocess
// and is OUT OF SCOPE for this boundary — the contract is that
// the ecosystem_docs.go binding stays gitnexus-agnostic so swap
// readiness per ADR-0080 is preserved).
//
// Invariant enforcement:
// - invariant: gitnexus boundary preserved.
// This test is the CANONICAL enforcement point for invariant in the
// to clarify N4 doc drift: this file IS the invariant enforcer; no
// other compliance test duplicates the check).
// - ADR-0007 (vendor mode): gitnexus orthogonal to ecosystem-RAG.
// - ADR-0080: if gitnexus is removed/replaced
// per ADR-0082 conditional trigger, remains operational without
// amendment because the substrate is gitnexus-agnostic.
//
// Implementation strategy:
// - AST parse each candidate.go file with go/parser.ParseFile and
// parser.ImportsOnly (fast: stops after the import block).
// - Inspect f.Imports and fail if any import path contains "gitnexus".
// - AST-level inspection (NOT source-text grep) so comments and string
// literals mentioning "gitnexus" do not trigger false positives. The
// ecosystem doc.go has a comment line explaining the orthogonality
// with gitnexus — that is informational, not an import, and must
// pass the gate cleanly.
package compliance

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNoGitNexusInPlan14EcosystemPackage(t *testing.T) {
	root := repoRoot(t)
	checkDirNoGitNexus(t, filepath.Join(root, "internal", "research", "ecosystem"))
}

func TestNoGitNexusInZenDocsCron(t *testing.T) {
	root := repoRoot(t)
	checkDirNoGitNexus(t, filepath.Join(root, "cmd", "zen-docs-cron"))
}

// TestNoGitNexusInPlan14CLIFiles verifies that CLI namespace fills
// (knowledge_remote.go, memory_*.go, specs_*.go, docs_*.go, doctor_ecosystem.go)
// do not import gitnexus.
func TestNoGitNexusInPlan14CLIFiles(t *testing.T) {
	root := repoRoot(t)
	cliDir := filepath.Join(root, "internal", "cli")

	if _, err := os.Stat(cliDir); os.IsNotExist(err) {
		t.Skip("internal/cli/ not found — Phase F not yet landed")
	}

	plan14CLIPrefixes := []string{
		"knowledge_remote",
		"memory_",
		"specs_",
		"docs_",
		"doctor_ecosystem",
	}

	entries, err := os.ReadDir(cliDir)
	if err != nil {
		t.Fatalf("cannot read internal/cli/: %v", err)
	}

	checked := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		isPlan14 := false
		for _, prefix := range plan14CLIPrefixes {
			if strings.HasPrefix(entry.Name(), prefix) {
				isPlan14 = true
				break
			}
		}
		if !isPlan14 {
			continue
		}
		path := filepath.Join(cliDir, entry.Name())
		checked++
		checkFileNoGitNexus(t, path)
	}

	if checked == 0 {
		t.Log("no Plan 14 CLI files found in internal/cli/ — Phase F not yet landed; OK")
	} else {
		t.Logf("checked %d Plan 14 CLI files for gitnexus boundary", checked)
	}
}

func TestNoGitNexusInEcosystemMCPExtension(t *testing.T) {
	root := repoRoot(t)
	ecosysDocsFile := filepath.Join(root, "internal", "mcp", "research", "ecosystem_docs.go")
	if _, err := os.Stat(ecosysDocsFile); os.IsNotExist(err) {
		t.Skip("internal/mcp/research/ecosystem_docs.go not found — Phase F not yet landed")
	}
	checkFileNoGitNexus(t, ecosysDocsFile)
}

func checkDirNoGitNexus(t *testing.T, dir string) {
	t.Helper()
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Logf("directory %s not found — skipping (phase not yet landed)", dir)
		t.Skip("directory not found")
	}

	scanned := 0
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		scanned++
		checkFileNoGitNexus(t, path)
		return nil
	})
	if err != nil {
		t.Fatalf("walk error on %s: %v", dir, err)
	}
	t.Logf("checked %d .go files in %s for gitnexus boundary", scanned, dir)
}

// checkFileNoGitNexus AST-parses path and fails if any import contains "gitnexus".
//
// AST-level inspection (not source-text grep) ensures that comments and string
// literals mentioning "gitnexus" do not trigger false positives — only actual
// import paths are evaluated. The ImportsOnly mode is the fastest parser mode
// for this check; it stops after the import block.
func checkFileNoGitNexus(t *testing.T, path string) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
	if err != nil {
		t.Errorf("cannot parse %s: %v", path, err)
		return
	}
	for _, imp := range f.Imports {
		importPath := strings.Trim(imp.Path.Value, `"`)
		if strings.Contains(importPath, "gitnexus") {
			t.Errorf("inv-zen-201 (ADR-0007): %s imports gitnexus: %q\n"+
				"Plan 14 is orthogonal to gitnexus project-RAG; ecosystem package MUST NOT import gitnexus.",
				path, importPath)
		}
	}
}
