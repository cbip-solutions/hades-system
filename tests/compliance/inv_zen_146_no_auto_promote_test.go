// tests/compliance/inv_zen_146_no_auto_promote_test.go
//
// Compliance gate for invariant: Promote and Unpromote are OPERATOR-GATED
// actions. The reason argument MUST NOT be an empty string literal at any
// callsite inside internal/. Passing an empty string bypasses the runtime
// guard (ErrPromoteReasonRequired) and breaks the audit trail.
//
// AST walk strategy:
//
// Walk every non-test.go file under internal/ recursively. For each
// function call expression where the selector name is "Promote" or
// "Unpromote", inspect the last positional argument:
//
// - If it is an *ast.BasicLit of kind token.STRING whose Value is `""`,
// fail immediately: this is a literal empty-reason callsite.
// - All other argument shapes (variables, constants, non-empty literals,
// composite expressions) are accepted: the runtime guard
// (ErrPromoteReasonRequired) is the second line of defence for those.
//
// Why AST not grep: grep would false-positive on variable declarations that
// happen to contain `Promote("` in a comment or string constant. The AST
// walk is structurally precise and eliminates that class of false-positive.
//
// Why last argument: the Promote signature is
//
// Promote(ctx, noteID, projectID, operatorID, reason string)
//
// and Unpromote is
//
// Unpromote(ctx, noteID, operatorID, reason string)
//
// In both cases `reason` is the last positional argument. The test checks
// len(call.Args) >= 2 (ctx + at least one real arg) before indexing.
//
// This test does NOT check that the reason is non-empty at runtime — that
// is the job of the ErrPromoteReasonRequired sentinel and its unit tests.
// This test catches the static case where a developer hard-codes `""`.
//
// invariant: Promote/Unpromote reason required.
//
// Plan-file requested a runtime Promote(reason="") rejection test
// (Step 1) here in compliance/. That assertion is fully covered at
// the unit level by internal/knowledge/aggregator/promote_test.go's
// TestPromoteEmptyReasonReturnsErrPromoteReasonRequired and
// TestPromoteRequiresReasonSentinelReachable. Adding a duplicate in
// compliance/ would import internal/knowledge/aggregator which
// links mattn/go-sqlite3; the compliance package already links
// github.com/ncruces/go-sqlite3/driver (via inv_zen_073_test.go),
// and registering both drivers in one binary panics with "Register
// called twice for driver sqlite3" (documented in
// inv_zen_148_research_dispatch_metadata_privacy_test.go header).
// The AST-callsite scan below + the aggregator unit test together
// provide complete invariant coverage (Step 2 + Step 1
// respectively). No extension needed.
package compliance

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInvZen146NoAutoPromoteCallsite(t *testing.T) {
	root := repoRoot(t)
	internalDir := filepath.Join(root, "internal")

	scanned := 0
	violations := 0

	err := filepath.WalkDir(internalDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {

			if d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".go") {
			return nil
		}

		if strings.HasSuffix(name, "_test.go") {
			return nil
		}
		scanned++
		violations += checkPromoteCallsites(t, path)
		return nil
	})
	if err != nil {
		t.Fatalf("inv-zen-146: WalkDir internal/: %v", err)
	}

	if scanned == 0 {
		t.Fatal("inv-zen-146: sentinel failure — 0 non-test Go files found under internal/; " +
			"directory layout may have changed")
	}
	if violations > 0 {
		t.Logf("inv-zen-146: %d violation(s) found across %d files", violations, scanned)
	}
}

func checkPromoteCallsites(t *testing.T, path string) int {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {

		t.Logf("inv-zen-146: parse %s: %v (skipping file)", path, err)
		return 0
	}

	violations := 0
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		var selName string
		switch fun := call.Fun.(type) {
		case *ast.SelectorExpr:
			selName = fun.Sel.Name
		case *ast.Ident:
			selName = fun.Name
		default:
			return true
		}

		if selName != "Promote" && selName != "Unpromote" {
			return true
		}

		if len(call.Args) < 2 {
			return true
		}

		lastArg := call.Args[len(call.Args)-1]
		lit, isLit := lastArg.(*ast.BasicLit)
		if !isLit {

			return true
		}
		if lit.Kind == token.STRING && lit.Value == `""` {
			pos := fset.Position(call.Pos())
			t.Errorf("inv-zen-146 violated: %s:%d — %s called with empty string literal reason; "+
				"reason MUST be non-empty per inv-zen-146 (runtime guard: ErrPromoteReasonRequired)",
				pos.Filename, pos.Line, selName)
			violations++
		}
		return true
	})
	return violations
}
