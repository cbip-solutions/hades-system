// SPDX-License-Identifier: MIT
// Package gin implements the gin HTTP route extractor
//
// It walks a Go package via go/parser AST + syntactic receiver-type
// inference (same strategy as the chi extractor — see internal/caronte/
// contract/extract/gohttp/chi/chi.go's "Type-resolution strategy" comment).
// The gin-specific twist: chi's Group/Route use INLINE CLOSURES that bind
// the receiver as a function parameter, while gin's Group RETURNS A NEW
// VALUE (`g := r.Group("/v1")`) that becomes the active prefix-carrying
// receiver. This requires a varPrefix tracking pass that maps each
// *gin.RouterGroup variable to its cumulative path prefix.
//
// Detect(file, content) gates by the `github.com/gin-gonic/gin` import.
//
// Receiver types recognised:
// - *gin.Engine (the top-level router constructed via gin.Default() /
// gin.New())
// - *gin.RouterGroup (the value returned by.Group(prefix,...))
// - gin.IRoutes (the interface both Engine and RouterGroup implement)
//
// Composition `g := r.Group("/v1")` pushes "/v1" onto g's prefix; a
// subsequent `g.GET("/users", h)` is emitted with `/v1/users`.
//
// Boundary: imports only
// `internal/caronte/store` + `internal/caronte/contract/extract` + std
// library. Does NOT import `internal/store` (daemon store) nor
// `golang.org/x/tools/go/packages`.
package gin

import (
	"bytes"
	"context"
	"fmt"
	"go/ast"
	goparser "go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cbip-solutions/hades-system/internal/caronte/contract/extract"
	caronteparser "github.com/cbip-solutions/hades-system/internal/caronte/parser"
	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

const extractorID = "gohttp-gin-v1"

var httpMethods = map[string]bool{
	"GET": true, "POST": true, "PUT": true, "DELETE": true,
	"PATCH": true, "HEAD": true, "OPTIONS": true,
}

var ginImportRE = regexp.MustCompile(`(?m)"github\.com/gin-gonic/gin"`)

func init() {
	if err := extract.Default().Register("gohttp-gin", New()); err != nil {
		panic(fmt.Sprintf("gin extractor: Register: %v", err))
	}
}

type Extractor struct{}

func New() *Extractor { return &Extractor{} }

func (e *Extractor) Language() extract.Language { return extract.LangGo }

func (e *Extractor) Frameworks() []string { return []string{"gin"} }

func (e *Extractor) Detect(file string, content []byte) bool {
	if strings.ToLower(filepath.Ext(file)) != ".go" {
		return false
	}
	return ginImportRE.Match(content)
}

func (e *Extractor) Endpoints(_ *caronteparser.Tree, file string) ([]store.APIEndpoint, error) {
	return e.ExtractFromPackage(context.Background(), filepath.Dir(file), "")
}

func (e *Extractor) Calls(_ *caronteparser.Tree, _ string) ([]store.APICall, error) {
	return nil, nil
}

func (e *Extractor) StubArtifacts(_ string, _ []byte) []extract.StubReference {
	return []extract.StubReference{}
}

func (e *Extractor) ExtractFromPackage(_ context.Context, pkgDir, repo string) ([]store.APIEndpoint, error) {
	entries, err := os.ReadDir(pkgDir)
	if err != nil {
		return nil, fmt.Errorf("gin extractor: readdir %q: %w", pkgDir, err)
	}
	fset := token.NewFileSet()
	var out []store.APIEndpoint
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		name := ent.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(pkgDir, name)
		src, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("gin extractor: read %q: %w", path, err)
		}
		f, err := goparser.ParseFile(fset, path, src, goparser.SkipObjectResolution)
		if err != nil {
			return nil, fmt.Errorf("gin extractor: parse %q: %w", path, err)
		}
		w := &ginWalker{
			pkg:     pkgDir,
			repo:    repo,
			file:    f,
			path:    path,
			fset:    fset,
			ginName: ginImportAlias(f),
		}
		if w.ginName == "" {
			continue
		}
		w.collectBindings()
		w.walkFile()
		out = append(out, w.endpoints...)
	}
	return out, nil
}

func ginImportAlias(f *ast.File) string {
	for _, imp := range f.Imports {
		if imp.Path == nil {
			continue
		}
		if !ginImportRE.MatchString(imp.Path.Value) {
			continue
		}
		if imp.Name != nil && imp.Name.Name != "" {
			return imp.Name.Name
		}
		return "gin"
	}
	return ""
}

type ginWalker struct {
	pkg     string
	repo    string
	file    *ast.File
	path    string
	fset    *token.FileSet
	ginName string

	ginRouterVars map[string]string

	endpoints []store.APIEndpoint
}

func (w *ginWalker) collectBindings() {
	w.ginRouterVars = map[string]string{}

	w.collectInitialBindings()

	for i := 0; i < 8; i++ {
		before := len(w.ginRouterVars)
		w.collectGroupBindings()
		if len(w.ginRouterVars) == before {
			break
		}
	}
}

// collectInitialBindings scans for gin.Default()/New() + parameter-typed
// receivers — bindings that do not depend on other ginRouterVars entries.
func (w *ginWalker) collectInitialBindings() {
	ast.Inspect(w.file, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.AssignStmt:
			if len(x.Lhs) == 1 && len(x.Rhs) == 1 {
				if id, ok := x.Lhs[0].(*ast.Ident); ok {
					if w.exprProducesGinEngine(x.Rhs[0]) {
						w.ginRouterVars[id.Name] = ""
					}
				}
			}
		case *ast.GenDecl:
			if x.Tok != token.VAR {
				return true
			}
			for _, spec := range x.Specs {
				vspec, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				if vspec.Type != nil && w.typeExprIsGinReceiver(vspec.Type) {
					for _, name := range vspec.Names {
						w.ginRouterVars[name.Name] = ""
					}
				}
				if vspec.Type == nil {
					for i, name := range vspec.Names {
						if i < len(vspec.Values) && w.exprProducesGinEngine(vspec.Values[i]) {
							w.ginRouterVars[name.Name] = ""
						}
					}
				}
			}
		case *ast.FuncDecl:
			if x.Recv != nil {
				for _, field := range x.Recv.List {
					if w.typeExprIsGinReceiver(field.Type) {
						for _, name := range field.Names {
							w.ginRouterVars[name.Name] = ""
						}
					}
				}
			}
			if x.Type != nil && x.Type.Params != nil {
				for _, field := range x.Type.Params.List {
					if w.typeExprIsGinReceiver(field.Type) {
						for _, name := range field.Names {
							w.ginRouterVars[name.Name] = ""
						}
					}
				}
			}
		case *ast.FuncLit:
			if x.Type != nil && x.Type.Params != nil {
				for _, field := range x.Type.Params.List {
					if w.typeExprIsGinReceiver(field.Type) {
						for _, name := range field.Names {
							w.ginRouterVars[name.Name] = ""
						}
					}
				}
			}
		}
		return true
	})
}

func (w *ginWalker) collectGroupBindings() {
	ast.Inspect(w.file, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		if len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
			return true
		}
		lhsIdent, ok := assign.Lhs[0].(*ast.Ident)
		if !ok {
			return true
		}
		ce, ok := assign.Rhs[0].(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := ce.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if sel.Sel.Name != "Group" {
			return true
		}
		recvIdent, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		parentPrefix, parentKnown := w.ginRouterVars[recvIdent.Name]
		if !parentKnown {
			return true
		}
		if len(ce.Args) < 1 {
			return true
		}
		prefix, ok := stringLitOf(ce.Args[0])
		if !ok {
			return true
		}
		w.ginRouterVars[lhsIdent.Name] = parentPrefix + prefix
		return true
	})
}

func (w *ginWalker) exprProducesGinEngine(e ast.Expr) bool {
	ce, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := ce.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	xIdent, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	if xIdent.Name != w.ginName {
		return false
	}
	return sel.Sel.Name == "Default" || sel.Sel.Name == "New"
}

func (w *ginWalker) typeExprIsGinReceiver(t ast.Expr) bool {
	for {
		switch tt := t.(type) {
		case *ast.StarExpr:
			t = tt.X
			continue
		case *ast.SelectorExpr:
			xIdent, ok := tt.X.(*ast.Ident)
			if !ok {
				return false
			}
			if xIdent.Name != w.ginName {
				return false
			}
			return tt.Sel.Name == "Engine" || tt.Sel.Name == "RouterGroup" || tt.Sel.Name == "IRoutes"
		default:
			return false
		}
	}
}

func (w *ginWalker) walkFile() {
	ast.Inspect(w.file, func(n ast.Node) bool {
		ce, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := ce.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		method := sel.Sel.Name
		if !httpMethods[method] {
			return true
		}
		recvIdent, ok := sel.X.(*ast.Ident)
		if ok {
			prefix, known := w.ginRouterVars[recvIdent.Name]
			if !known {
				return true
			}
			w.handleRouteCall(method, ce, prefix)
			return true
		}

		if chainPrefix, ok := w.resolveChainPrefix(sel.X); ok {
			w.handleRouteCall(method, ce, chainPrefix)
		}
		return true
	})
}

func (w *ginWalker) resolveChainPrefix(e ast.Expr) (string, bool) {
	switch x := e.(type) {
	case *ast.Ident:
		p, ok := w.ginRouterVars[x.Name]
		return p, ok
	case *ast.CallExpr:
		sel, ok := x.Fun.(*ast.SelectorExpr)
		if !ok {
			return "", false
		}
		if sel.Sel.Name != "Group" {
			return "", false
		}
		parentPrefix, ok := w.resolveChainPrefix(sel.X)
		if !ok {
			return "", false
		}
		if len(x.Args) < 1 {
			return parentPrefix, true
		}
		prefix, ok := stringLitOf(x.Args[0])
		if !ok {
			return parentPrefix, true
		}
		return parentPrefix + prefix, true
	}
	return "", false
}

func (w *ginWalker) handleRouteCall(method string, ce *ast.CallExpr, prefix string) {
	if len(ce.Args) < 1 {
		return
	}
	pathLit, ok := stringLitOf(ce.Args[0])
	if !ok {
		return
	}
	fullPath := prefix + pathLit
	handlerNodeID := w.handlerNodeID(ce.Args)
	normalized := normalizePathTemplate(fullPath)
	w.endpoints = append(w.endpoints, store.APIEndpoint{
		EndpointID:    fmt.Sprintf("%s:http:%s %s", w.repo, method, normalized),
		Repo:          w.repo,
		Kind:          "http",
		Method:        method,
		PathTemplate:  normalized,
		HandlerNodeID: handlerNodeID,
		ExtractedAt:   nowSeconds(),
		ExtractorID:   extractorID,
	})
}

func (w *ginWalker) handlerNodeID(args []ast.Expr) string {
	if len(args) < 2 {
		return ""
	}
	h := args[len(args)-1]
	switch x := h.(type) {
	case *ast.Ident:
		return fmt.Sprintf("%s.%s", w.pkg, x.Name)
	case *ast.SelectorExpr:
		if id, ok := x.X.(*ast.Ident); ok {
			return fmt.Sprintf("%s.%s.%s", w.pkg, id.Name, x.Sel.Name)
		}
	case *ast.FuncLit:
		pos := w.fset.Position(x.Pos())
		return fmt.Sprintf("%s.%s:%d.anon", w.pkg, filepath.Base(pos.Filename), pos.Line)
	}
	return ""
}

func stringLitOf(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING || len(lit.Value) < 2 {
		return "", false
	}
	q := lit.Value[0]
	if q != '"' && q != '`' {
		return "", false
	}
	if !bytes.HasSuffix([]byte(lit.Value), []byte{q}) {
		return "", false
	}
	return lit.Value[1 : len(lit.Value)-1], true
}

var nowSeconds = func() int64 { return realNowSeconds() }

func normalizePathTemplate(p string) string {
	return paramSyntaxRE.ReplaceAllString(p, "{param}")
}

var paramSyntaxRE = regexp.MustCompile(`\{[^}]*\}|:[A-Za-z_][A-Za-z0-9_]*|\[[^\]]+\]|\$\{[^}]*\}|<[^>]+>`)
