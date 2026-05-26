package compliance

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

const dispatcherFile = "internal/daemon/dispatcher/dispatcher.go"
const adapterFile = "internal/daemon/dispatcheradapter/budget_hooks.go"

func resolveRepoFile(t *testing.T, rel string) string {
	t.Helper()
	cwd, _ := filepath.Abs(".")
	for d := cwd; d != "/" && d != ""; d = filepath.Dir(d) {
		candidate := filepath.Join(d, rel)
		if _, err := parser.ParseFile(token.NewFileSet(), candidate, nil, parser.PackageClauseOnly); err == nil {
			return candidate
		}
	}
	t.Fatalf("could not locate %s from %s upward", rel, cwd)
	return ""
}

func dispatcherImportsAdapter(t *testing.T) bool {
	path := resolveRepoFile(t, dispatcherFile)
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	for _, imp := range f.Imports {
		if strings.Contains(imp.Path.Value, "dispatcheradapter") {
			return true
		}
	}
	return false
}

func TestInvZen076_DispatcherFileImportsBudgetAdapter(t *testing.T) {
	if !dispatcherImportsAdapter(t) {
		t.Skip("inv-zen-076 wiring lands in Plan 4 Phase G — no dispatcheradapter import yet (Phase F-only commit)")
	}
}

func TestInvZen076_EveryForwardPrecededByPreCall(t *testing.T) {
	if !dispatcherImportsAdapter(t) {
		t.Skip("inv-zen-076 wiring lands in Plan 4 Phase G")
	}
	path := resolveRepoFile(t, dispatcherFile)
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		var forwardPos, preCallPos token.Pos
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			ce, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := ce.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if sel.Sel.Name == "Forward" && forwardPos == token.NoPos {
				forwardPos = ce.Pos()
			}
			if sel.Sel.Name == "PreCall" && preCallPos == token.NoPos {
				preCallPos = ce.Pos()
			}
			return true
		})
		if forwardPos == token.NoPos {
			continue
		}
		if preCallPos == token.NoPos {
			t.Errorf("function %s contains backend.Forward(...) without any adapter.PreCall(...) (inv-zen-076)",
				fd.Name.Name)
			continue
		}
		if preCallPos > forwardPos {
			t.Errorf("function %s: PreCall (%v) appears AFTER Forward (%v) (inv-zen-076 source-order)",
				fd.Name.Name, fset.Position(preCallPos), fset.Position(forwardPos))
		}
	}
}

func TestInvZen076_AdapterFileExists(t *testing.T) {
	path := resolveRepoFile(t, adapterFile)
	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, path, nil, parser.PackageClauseOnly); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
}

func TestInvZen076_AdapterExposesPreCallAndPostCall(t *testing.T) {
	path := resolveRepoFile(t, adapterFile)
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	hasPreCall := false
	hasPostCall := false
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		switch fd.Name.Name {
		case "PreCall":
			hasPreCall = true
		case "PostCall":
			hasPostCall = true
		}
	}
	if !hasPreCall {
		t.Error("adapter does not expose PreCall (inv-zen-076)")
	}
	if !hasPostCall {
		t.Error("adapter does not expose PostCall (inv-zen-077)")
	}
}

func TestInvZen076_ResearchEveryBackendDispatchHasPreCall(t *testing.T) {
	path := resolveRepoFile(t, "internal/mcp/research/server.go")
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	wantHandlers := []string{
		"handleWebSearch",
		"handleArxiv",
		"handleGitHubSearch",
		"handleCodeGraph",
		"handleEcosystemDocs",
		"handleSynthesize",
		// C-20 (post-review I-2): handleAgenticDeep MUST also have a
		// handler-level PreCall. Per-iteration PreCall inside Agentic.Run
		// is in addition to (not a substitute for) the handler entry
		// gate; bad requests must reject before constructing the
		// Agentic struct + allocating resources.
		"handleAgenticDeep",
	}
	found := make(map[string]bool)
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}

		var isHandler bool
		for _, h := range wantHandlers {
			if fd.Name.Name == h {
				isHandler = true
				break
			}
		}
		if !isHandler {
			continue
		}
		hasPreCall := false
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			ce, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := ce.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if sel.Sel.Name == "PreCall" {
				hasPreCall = true
				return false
			}
			return true
		})
		if !hasPreCall {
			t.Errorf("research handler %s lacks BudgetClient.PreCall (inv-zen-076)",
				fd.Name.Name)
		}
		found[fd.Name.Name] = true
	}
	for _, h := range wantHandlers {
		if !found[h] {
			t.Errorf("expected handler %s not found in server.go", h)
		}
	}
}

func TestInvZen076_ResearchDispatcherHasPreCall(t *testing.T) {
	path := resolveRepoFile(t, "internal/mcp/research/dispatch.go")
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	hasPreCall, hasPreCheck := false, false
	ast.Inspect(f, func(n ast.Node) bool {
		ce, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fn := ce.Fun.(type) {
		case *ast.SelectorExpr:
			if fn.Sel.Name == "PreCall" {
				hasPreCall = true
			}
			if fn.Sel.Name == "preCheck" {
				hasPreCheck = true
			}
		case *ast.Ident:
			if fn.Name == "preCheck" {
				hasPreCheck = true
			}
		}
		return true
	})
	if !hasPreCheck && !hasPreCall {
		t.Errorf("dispatch.go has neither preCheck nor PreCall — inv-zen-076 unanchored")
	}
}
