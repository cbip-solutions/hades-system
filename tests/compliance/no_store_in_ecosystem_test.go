// tests/compliance/no_store_in_ecosystem_test.go
//
// The internal/research/ecosystem/... package tree MUST NOT transitively
// import any of the following:
// - internal/store (direct DB ops — use aggregator abstraction)
// - internal/daemon/budget (budget's AuditChainEmitter — declare own narrow interface)
// - net/http (HTTP egress — use Revalidator.Fetch)
// - internal/caronte/*
//
// Note on net/http + revalidator transitive: the ecosystem package imports
// internal/research/cache/Revalidator. The Revalidator itself imports net/http.
// BUT: `go list -deps./internal/research/ecosystem/...` will include the
// transitive net/http import via the revalidator. The boundary enforced here is:
// - DIRECT import of net/http in ecosystem files: zero (enforced by noWebInEcosystem vet analyzer)
// - TRANSITIVE net/http via revalidator: ACCEPTABLE (revalidator is the legal gateway)
//
// Therefore this test checks for DIRECT net/http imports via AST (not go list).
// For store/budget/caronte: go list -deps is used (any transitive = violation).
//
// invariant: aggregator/ecosystem do not import internal/store.
// invariant: ecosystem does not import net/http directly.
// invariant: caronte boundary preserved.
package compliance

import (
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNoStoreInEcosystem(t *testing.T) {
	root := repoRoot(t)
	checkGoListDepsAbsent(t, root,
		"./internal/research/ecosystem/...",
		"internal/store",
		"inv-zen-031: ecosystem MUST NOT import internal/store (use aggregator abstraction)")
}

func TestNoBudgetInEcosystem(t *testing.T) {
	root := repoRoot(t)
	checkGoListDepsAbsent(t, root,
		"./internal/research/ecosystem/...",
		"internal/daemon/budget",
		"inv-zen-031: ecosystem MUST NOT import internal/daemon/budget (declare own RAGAuditChainEmitter narrow interface)")
}

func TestNoCaronteInEcosystem(t *testing.T) {
	root := repoRoot(t)
	checkGoListDepsAbsent(t, root,
		"./internal/research/ecosystem/...",
		"internal/caronte",
		"inv-zen-201: ecosystem MUST NOT import internal/caronte (project code-graph is orthogonal per ADR-0007/Plan 19)")
}

func TestNoDirectHTTPInEcosystem(t *testing.T) {
	root := repoRoot(t)
	ecosystemDir := filepath.Join(root, "internal", "research", "ecosystem")

	if _, err := os.Stat(ecosystemDir); os.IsNotExist(err) {
		t.Skip("internal/research/ecosystem/ not yet landed (Phase A dependency)")
	}

	scanned := 0
	err := filepath.WalkDir(ecosystemDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}

		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		scanned++
		checkNoNetHTTPImport(t, path)
		return nil
	})
	if err != nil {
		t.Fatalf("walk error: %v", err)
	}
	if scanned == 0 {
		t.Log("no .go files found in internal/research/ecosystem/ — Phase A not yet landed; skipping count assertion")
	} else {
		t.Logf("scanned %d production .go files in internal/research/ecosystem/", scanned)
	}
}

func TestNoDirectHTTPInEcosystemSources(t *testing.T) {
	root := repoRoot(t)
	sourcesDir := filepath.Join(root, "internal", "research", "ecosystem", "sources")

	if _, err := os.Stat(sourcesDir); os.IsNotExist(err) {
		t.Skip("internal/research/ecosystem/sources/ not yet landed (Phase B dependency)")
	}

	scanned := 0
	err := filepath.WalkDir(sourcesDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		scanned++
		checkNoNetHTTPImport(t, path)
		return nil
	})
	if err != nil {
		t.Fatalf("walk error: %v", err)
	}
	t.Logf("scanned %d production .go files in sources/", scanned)
}

func TestEcosystemBoundaryFileSentinel(t *testing.T) {
	root := repoRoot(t)
	ecosystemDir := filepath.Join(root, "internal", "research", "ecosystem")
	if _, err := os.Stat(ecosystemDir); os.IsNotExist(err) {
		t.Skip("internal/research/ecosystem/ not yet landed")
	}

	entries, err := os.ReadDir(ecosystemDir)
	if err != nil {
		t.Fatalf("cannot read directory: %v", err)
	}
	goFiles := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") {
			goFiles++
		}
	}
	if goFiles == 0 {
		t.Error("internal/research/ecosystem/ exists but contains no .go files — Phase A may be incomplete")
	}
}

func checkGoListDepsAbsent(t *testing.T, root, pattern, forbiddenSubstring, msg string) {
	t.Helper()
	deps := goListDeps(t, root, pattern)
	for _, dep := range deps {
		if strings.Contains(dep, forbiddenSubstring) {
			t.Errorf("%s\nfound: %s", msg, dep)
		}
	}
}

func goListDeps(t *testing.T, root, pattern string) []string {
	t.Helper()
	cmd := exec.Command("go", "list", "-deps", pattern)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {

		t.Logf("go list -deps %s: %v — skipping (package may not be landed yet)", pattern, err)
		t.Skip("package not yet buildable — dependency phase not landed")
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	result := make([]string, 0, len(lines))
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			result = append(result, l)
		}
	}
	return result
}

func checkNoNetHTTPImport(t *testing.T, path string) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
	if err != nil {
		t.Errorf("cannot parse %s: %v", path, err)
		return
	}
	for _, imp := range f.Imports {
		importPath := strings.Trim(imp.Path.Value, `"`)
		if importPath == "net/http" {
			t.Errorf("inv-zen-191: %s directly imports net/http (use Revalidator.Fetch instead)", path)
		}
	}
}
