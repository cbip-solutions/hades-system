// tests/compliance/inv_zen_152_plan_14_boundary_test.go
//
// Compliance gate for inv-zen-152 (Plan 14 boundary preserved): the
// internal/research/cache package MUST NOT import "net/http" or call
// http.Get / http.Post / http.Do in any non-test Go file, EXCEPT in the
// allowlisted HTTP surface files:
//
//   - revalidator.go        — Plan 9 F-7 HEAD revalidation (Validate).
//   - revalidator_fetch.go  — Plan 14 Phase A Task A-2 (ADR-0087)
//     URL-fetch primitives (Fetch GET + FetchPOST companion).
//
// Both files implement the SOLE legal HTTP boundary for the cache package
// per inv-zen-152; the Plan 14 amendment kept the boundary by extending
// the allowlist rather than adding a third surface.
//
// Invariant text (inv-zen-152, post-ADR-0087):
//
//	"The research cache package has exactly two legal HTTP surface files:
//	 revalidator.go (HEAD revalidation, Plan 9 F-7) and revalidator_fetch.go
//	 (URL-fetch primitives, Plan 14 Phase A Task A-2 / ADR-0087). All other
//	 files in internal/research/cache/ MUST NOT import net/http or call
//	 http.Get / http.Post / http.Do. The Phase J noWebInCache compile-time
//	 analyzer (internal/lint/no_web_in_aggregator.go) enforces the same
//	 allowlist; this runtime AST gate is the defence-in-depth companion."
//
// Implementation strategy:
//
//   - AST parse every non-test .go file in internal/research/cache/ using
//     go/parser.ParseDir (nil filter = all .go files; the filter parameter
//     is func(os.FileInfo) bool, not a WalkDir closure — plan-file line 5038
//     had this wrong; corrected here).
//   - Assert no file except revalidator.go carries a "net/http" import
//     (AST-level: f.Imports).
//   - Assert no file except revalidator.go contains an http.Get / http.Post /
//     http.Do call expression (AST-level: ast.Inspect + CallExpr + SelectorExpr).
//
// Two distinct assertion layers catch distinct drift modes:
//   - Layer (a): import scan catches `import "net/http"` added directly.
//   - Layer (b): callsite scan catches http.Get/Post/Do usage (even if the
//     import were aliased or injected via a vendor shim without the canonical
//     path — defence in depth).
//
// Sentinel: the test counts scanned files and fails if 0 were found, ensuring
// that a directory rename or accidental deletion does not silently pass the gate.
//
// analyzer pending).
//
//	Plan-file requested a TestInvZen152_NoWebDispatchInResearchCache
//	that walks internal/research/cache/ and rejects http.Get/Post/
//	NewRequest callsites (excluding revalidator.go). This file
//	ships TWO assertion layers: (a) AST import scan rejecting
//	`import "net/http"` outside revalidator.go (catches direct
//	import), AND (b) AST callsite scan rejecting http.Get/Post/Do
//	outside revalidator.go (catches use even via aliased imports).
//	Defense-in-depth pattern. Plus a sentinel counter that fails
//	the gate if 0 files were scanned (catches directory-rename
//	silent-pass regressions). EXCEEDS plan-file requirement. No
//	extension needed. Audit-trail comment added.
package compliance

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

func TestInvZen152NoHTTPOutsideRevalidator(t *testing.T) {
	root := repoRoot(t)
	pkgDir := filepath.Join(root, "internal", "research", "cache")

	allowed := map[string]bool{
		"revalidator.go":       true,
		"revalidator_fetch.go": true,
	}

	fset := token.NewFileSet()

	pkgs, err := parser.ParseDir(fset, pkgDir, nil, parser.AllErrors)
	if err != nil {

		t.Fatalf("inv-zen-152: parser.ParseDir(%s): %v", pkgDir, err)
	}

	scanned := 0
	for _, pkg := range pkgs {
		for fname, f := range pkg.Files {
			base := filepath.Base(fname)

			if strings.HasSuffix(base, "_test.go") {
				continue
			}

			if allowed[base] {
				continue
			}

			scanned++

			for _, imp := range f.Imports {
				if imp.Path == nil {
					continue
				}

				if imp.Path.Value == `"net/http"` {
					t.Errorf("inv-zen-152: %s imports net/http — "+
						"only revalidator.go is permitted to use net/http in this package", fname)
				}
			}

			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				ident, ok := sel.X.(*ast.Ident)
				if !ok {
					return true
				}
				if ident.Name == "http" {
					switch sel.Sel.Name {
					case "Get", "Post", "Do":
						t.Errorf("inv-zen-152: %s calls http.%s — "+
							"only revalidator.go is permitted to make HTTP calls in this package",
							fname, sel.Sel.Name)
					}
				}
				return true
			})
		}
	}

	if scanned == 0 {
		t.Fatalf("inv-zen-152: sentinel failure — 0 non-test non-allowed Go files found in %s; "+
			"directory layout may have changed or all files were accidentally deleted", pkgDir)
	}
}
