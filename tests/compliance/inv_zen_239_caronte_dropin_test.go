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

func TestInvZen239DropInAnchorPresent(t *testing.T) {
	root := repoRoot(t)
	src := filepath.Join(root, "internal", "caronte", "engine.go")
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read engine.go: %v", err)
	}
	if !strings.Contains(string(data), "research.GitnexusClient = (*Engine)(nil)") {
		t.Errorf("inv-zen-239: engine.go missing drop-in anchor `var _ research.GitnexusClient = (*Engine)(nil)`")
	}
}

func TestInvZen239BootstrapRequired(t *testing.T) {
	root := repoRoot(t)
	src := filepath.Join(root, "cmd", "zen-swarm-ctld", "main.go")
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, src, nil, 0)
	if err != nil {
		t.Fatalf("parse main.go: %v", err)
	}
	var sawConstruct, sawExit bool
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fn := call.Fun.(type) {
		case *ast.Ident:
			if fn.Name == "buildCaronteEngine" {
				sawConstruct = true
			}
		case *ast.SelectorExpr:
			if fn.Sel.Name == "NewEngine" {
				if x, ok := fn.X.(*ast.Ident); ok && x.Name == "caronte" {
					sawConstruct = true
				}
			}
			if fn.Sel.Name == "Exit" {
				if x, ok := fn.X.(*ast.Ident); ok && x.Name == "os" {
					sawExit = true
				}
			}
		}
		return true
	})
	if !sawConstruct {
		t.Error("inv-zen-239: main.go does not construct the caronte engine (buildCaronteEngine/caronte.NewEngine)")
	}
	if !sawExit {
		t.Error("inv-zen-239: main.go has no os.Exit (bootstrap-required path missing)")
	}
}

func TestInvZen239GitnexusChildClientRemovedFromBoot(t *testing.T) {
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "cmd", "zen-swarm-ctld", "main.go"))
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	if strings.Contains(string(data), "NewGitnexusChildClient") {
		t.Error("inv-zen-239: main.go still calls NewGitnexusChildClient; the caronte drop-in must replace the gitnexus subprocess boot")
	}
}
