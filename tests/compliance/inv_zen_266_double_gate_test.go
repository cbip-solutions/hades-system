package compliance

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

func TestInvZen266DoubleGateAST(t *testing.T) {
	root := repoRoot(t)
	path := filepath.Join(root, "internal/caronte/coordinated/orchestrator.go")
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.AllErrors)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	var dispatchFn *ast.FuncDecl
	ast.Inspect(file, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "Dispatch" {
			return true
		}
		if fn.Recv == nil || len(fn.Recv.List) == 0 {
			return true
		}

		star, ok := fn.Recv.List[0].Type.(*ast.StarExpr)
		if !ok {
			return true
		}
		ident, ok := star.X.(*ast.Ident)
		if !ok || ident.Name != "OrchestratorCoordinator" {
			return true
		}
		dispatchFn = fn
		return false
	})
	if dispatchFn == nil {
		t.Fatalf("inv-zen-266: Dispatch method on *OrchestratorCoordinator not found in %s", path)
	}

	var authorizePos, oraclePos token.Pos
	ast.Inspect(dispatchFn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		switch sel.Sel.Name {
		case "AuthorizeProjects":
			if !authorizePos.IsValid() {
				authorizePos = call.Pos()
			}
		case "Decision":
			if !oraclePos.IsValid() {
				oraclePos = call.Pos()
			}
		}
		return true
	})

	if !authorizePos.IsValid() {
		t.Errorf("inv-zen-266: Dispatch body MUST call Workspace.AuthorizeProjects (capa-firewall side of double-gate); not found")
	}
	if !oraclePos.IsValid() {
		t.Errorf("inv-zen-266: Dispatch body MUST call Autonomy.Decision (oracle side of double-gate); not found")
	}
	if authorizePos.IsValid() && oraclePos.IsValid() && authorizePos >= oraclePos {
		authLoc := fset.Position(authorizePos)
		oracleLoc := fset.Position(oraclePos)
		t.Errorf(
			"inv-zen-266: AuthorizeProjects MUST be called BEFORE Autonomy.Decision (capa-firewall first per §8.4); got AuthorizeProjects at %s vs Autonomy.Decision at %s",
			authLoc, oracleLoc,
		)
	}
}
