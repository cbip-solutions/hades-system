// tests/compliance/inv_zen_230_caronte_no_store_import_test.go
//
// Compliance gate for inv-zen-230: internal/caronte (and ALL subpackages)
// plus internal/daemon/caronteadapter MUST NOT import internal/store. DB
// access is bridged via caronteadapter, the only package that opens
// caronte.db by path. Mirrors inv_zen_031_plan9_aggregator_test.go.
//
// Two checks:
//  1. AST import scan over internal/caronte/** + caronteadapter (incl.
//     _test.go) — zero imports containing "internal/store".
//  2. Runtime sentinel reachability — production source under
//     internal/caronte/store invokes caronteBoundarySentinel(), the
//     structural witness that the import set is intentionally narrow.
//
// inv-zen-230 (Plan 19 Phase A).
package compliance

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInvZen230CaronteNoStoreImport(t *testing.T) {
	root := repoRoot(t)
	caronteRoot := filepath.Join(root, "internal", "caronte")
	adapterDir := filepath.Join(root, "internal", "daemon", "caronteadapter")

	scanned := 0
	walk := func(dir string) {
		_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			scanned++
			checkCaronteNoStoreImport(t, path)
			return nil
		})
	}
	walk(caronteRoot)
	walk(adapterDir)

	if scanned == 0 {
		t.Fatal("inv-zen-230: sentinel failure — 0 Go files scanned under " +
			"internal/caronte + caronteadapter; layout changed or packages deleted")
	}
}

func checkCaronteNoStoreImport(t *testing.T, path string) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
	if err != nil {
		t.Errorf("inv-zen-230: parse %s: %v", path, err)
		return
	}
	for _, imp := range f.Imports {
		if imp.Path == nil {
			continue
		}
		importPath := strings.Trim(imp.Path.Value, `"`)
		if strings.Contains(importPath, "internal/store") {
			t.Errorf("inv-zen-230 violated: %s imports %q — internal/caronte must "+
				"NOT import internal/store; DB access goes via internal/daemon/caronteadapter",
				path, importPath)
		}
	}
}

func TestInvZen230SentinelInvokedFromOpen(t *testing.T) {
	root := repoRoot(t)
	src, err := os.ReadFile(filepath.Join(root, "internal", "caronte", "store", "store.go"))
	if err != nil {
		t.Fatalf("inv-zen-230: read store.go: %v", err)
	}
	body := string(src)
	if !strings.Contains(body, "func caronteBoundarySentinel()") {
		t.Error("inv-zen-230: caronteBoundarySentinel declaration missing from store.go")
	}

	if strings.Count(body, "caronteBoundarySentinel") < 2 {
		t.Error("inv-zen-230: caronteBoundarySentinel declared but never invoked in store.go")
	}
}
