// tests/compliance/inv_zen_271_boundary_no_internal_store_test.go
//
// Compliance gate for inv-zen-271 (Plan 20 boundary mirror of inv-zen-031 /
// inv-zen-230): the Plan-20 federation package (and the later Plan-20
// packages contract/{extract,link,break} + coordinated owned by Phases
// C-H) MUST NOT import internal/store. The single sanctioned bridge is
// internal/caronte/store (Plan 19's boundary, for the FROZEN value types
// ContractLink + WorkspacePolicy + the sentinel errors); cross-project
// federation does NOT go through the daemon-shared internal/store at all.
//
// Phase A ships the gate scoped to internal/caronte/store/federation only;
// later phases (C/D/E/F/G/H/L) extend the scan scope additively as their
// packages land. Phase L roll-up consolidates the full scope.
//
// inv-zen-271 (Plan 20 Phase A).
package compliance

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInvZen271BoundaryNoInternalStore(t *testing.T) {
	root := repoRoot(t)
	federationRoot := filepath.Join(root, "internal", "caronte", "store", "federation")

	scanned := 0
	_ = filepath.WalkDir(federationRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		scanned++
		checkFederationNoStoreImport(t, path)
		return nil
	})

	if scanned == 0 {
		t.Fatal("inv-zen-271: sentinel failure — 0 Go files scanned under " +
			"internal/caronte/store/federation/; layout changed or package deleted")
	}
}

func checkFederationNoStoreImport(t *testing.T, path string) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
	if err != nil {
		t.Errorf("inv-zen-271: parse %s: %v", path, err)
		return
	}
	for _, imp := range f.Imports {
		if imp.Path == nil {
			continue
		}
		importPath := strings.Trim(imp.Path.Value, `"`)

		const forbidden = "github.com/cbip-solutions/hades-system/internal/store"
		if importPath == forbidden || strings.HasPrefix(importPath, forbidden+"/") {
			t.Errorf("inv-zen-271 violated: %s imports %q — internal/caronte/store/federation MUST NOT "+
				"import internal/store; the only sanctioned cross-pkg import is internal/caronte/store "+
				"(Plan 19 boundary, for ContractLink + WorkspacePolicy value types)",
				path, importPath)
		}
	}
}

func TestInvZen271SentinelInvokedFromOpen(t *testing.T) {
	root := repoRoot(t)
	src, err := os.ReadFile(filepath.Join(root, "internal", "caronte", "store", "federation", "db.go"))
	if err != nil {
		t.Fatalf("inv-zen-271: read db.go: %v", err)
	}
	body := string(src)
	if !strings.Contains(body, "func federationBoundarySentinel()") {
		t.Error("inv-zen-271: federationBoundarySentinel declaration missing from db.go")
	}

	if strings.Count(body, "federationBoundarySentinel") < 2 {
		t.Error("inv-zen-271: federationBoundarySentinel declared but never invoked in db.go")
	}
}
