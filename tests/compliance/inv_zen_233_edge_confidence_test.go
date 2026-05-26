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

func TestInvZen233_UpsertEdgeGatesConfidence(t *testing.T) {
	root := repoRoot(t)

	src := filepath.Join(root, "internal", "caronte", "store", "edges.go")
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, src, nil, 0)
	if err != nil {
		t.Fatalf("parse edges.go: %v", err)
	}
	var found, gatesValid bool
	ast.Inspect(f, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "UpsertEdge" {
			return true
		}
		found = true
		ast.Inspect(fn, func(m ast.Node) bool {
			if sel, ok := m.(*ast.SelectorExpr); ok && sel.Sel.Name == "Valid" {
				gatesValid = true
			}
			return true
		})
		return false
	})
	if !found {
		t.Fatal("inv-zen-233: store.UpsertEdge not found in internal/caronte/store/edges.go")
	}
	if !gatesValid {
		t.Error("inv-zen-233: UpsertEdge does not call Confidence.Valid() — the edge-confidence gate is missing")
	}
}

func TestInvZen233_AllConfidencesValid(t *testing.T) {

	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "internal", "caronte", "store", "types.go"))
	if err != nil {
		t.Fatalf("read types.go: %v", err)
	}
	body := string(raw)
	for _, tier := range []string{"exact_static", "exact_vta", "exact_cha", "scip_impl", "heuristic_name", "llm_hint"} {
		if !strings.Contains(body, tier) {
			t.Errorf("inv-zen-233: C-3 tier %q absent from types.go frozen set", tier)
		}
	}
}
