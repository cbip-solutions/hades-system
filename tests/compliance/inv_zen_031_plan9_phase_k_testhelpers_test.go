// tests/compliance/inv_zen_031_plan9_phase_k_testhelpers_test.go
//
// inv-zen-031 boundary extension for Plan 9 Phase K-17:
//
//	tests/testhelpers/  MUST NOT be imported by any internal/* package.
//
// Why this boundary matters
// =========================
//
// `tests/testhelpers/` hosts in-memory mocks (MockTesseraAdapter,
// MockS3, MockWitness), fixture seeders (SampleEvent, SampleProject),
// fault-injection harnesses (tamperinject, crash_injector), and a
// mock research MCP client. These exist EXCLUSIVELY to support test
// binaries — they have non-production semantics (deterministic time
// pins, simulated corruption, in-memory storage that vanishes at
// process exit). If any production internal/* file imports
// testhelpers, the production binary would carry test-only code +
// test-only behaviour into operator-facing surfaces.
//
// inv-zen-031 originally pinned: bypass/providers/dispatcher/
// orchestrator packages NEVER import internal/store directly.
// internal/* code NEVER imports tests/testhelpers/. The two boundary
// rules share the same enforcement style (AST walk + import check)
// and the same purpose (production isolation from leaked dependencies).
//
// Implementation
// ==============
//
// Walks the entire internal/ tree, parsing every non-test (.go but
// NOT _test.go) file via go/parser.ParseFile(ImportsOnly), checks
// import paths for any string containing "tests/testhelpers", and
// fails with the offending file list.
//
// Exclusions: _test.go files are skipped. They are not part of the
// production binary and may legitimately use testhelpers (though most
// won't, since per-package _test.go usually imports specific narrow
// sub-mocks).
//
// Phase J test (inv_zen_031_plan9_phase_j_test.go), Plan 7 Phase B
// test (inv_zen_122_inv_zen_031_plan7_packages_test.go).
package compliance

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

func TestInvZen031Plan9PhaseKTesthelpersBoundary(t *testing.T) {
	root := repoRoot(t)
	pkgDir := filepath.Join(root, "internal")

	fset := token.NewFileSet()
	type offense struct {
		file       string
		importPath string
	}
	var offenders []offense

	walk := func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, parseErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if parseErr != nil {

			return nil
		}
		for _, imp := range f.Imports {

			pathLit := strings.Trim(imp.Path.Value, "\"`")
			if strings.Contains(pathLit, "/tests/testhelpers") {
				offenders = append(offenders, offense{
					file:       path,
					importPath: pathLit,
				})
			}
		}
		return nil
	}

	if err := filepath.WalkDir(pkgDir, walk); err != nil {
		t.Fatalf("WalkDir %s: %v", pkgDir, err)
	}

	if len(offenders) > 0 {
		t.Errorf("inv-zen-031 boundary violation: %d internal/* file(s) import tests/testhelpers/:", len(offenders))
		for _, o := range offenders {
			rel, err := filepath.Rel(root, o.file)
			if err != nil {
				rel = o.file
			}
			t.Errorf("  %s imports %q", rel, o.importPath)
		}
		t.Errorf("\nProduction internal/* code must not depend on test fixtures.")
		t.Errorf("If a helper is needed in production, hoist it to a non-test package.")
	}
}
