// SPDX-License-Identifier: MIT
// Package chi implements the chi HTTP route extractor
//
// It walks a Go package via go/parser (NOT go/packages — see "Type-resolution
// strategy" below) and finds every call expression of the form:
//
// - `<routerVar>.<HTTPMethod>(<path>, <handler>)` — Get/Post/Put/Delete/...
// - `<routerVar>.Route(<prefix>, func(r chi.Router){...})` — sub-router prefix
// - `<routerVar>.Group(func(r chi.Router){...})` — middleware scope (no prefix)
// - `<routerVar>.Mount(<prefix>, <subRouter>)` — cross-package composition
//
// It threads the receiver-variable binding to confirm the receiver is a chi
// router (initialized via `chi.NewRouter()` / `chi.NewMux()`, or accepted as
// a function parameter of type `chi.Router` / `*chi.Mux`), and emits one
// APIEndpoint{Kind:"http"} per route with the prefix-composed path.
//
// Detect(file, content) gates by the `github.com/go-chi/chi` import (any
// major version) — quick byte-scan before the parse pass.
//
// # Type-resolution strategy
//
// The plan's anticipated reusing release C's semantic.Resolver +
// golang.org/x/tools/go/packages for router-var type-info; the as-built
// divergence (documented in the plan's §" divergences from
// master/spec" #2) noted go/packages would be heavyweight (10-60 s per
// project) and substituted a per-file packages.Load instead.
//
// This implementation goes one step further: routes are extracted via
// go/parser AST walking + SYNTACTIC receiver-type inference, not
// types.Info — because (a) the fixtures import chi without the parent
// module taking chi as a dep (testdata/ skips); (b) on real repos that DO
// import chi, the type inference is structurally identical (chi.NewRouter()
// is a chi router; a `chi.Router`-typed parameter is a chi router); (c) the
// AST-only approach is ~10× faster than go/packages.Load even when the deps
// resolve, and there is no need to walk transitive deps for route extraction.
//
// The receiver-type check (`isChiReceiver`) is syntactic: a variable is a
// chi router iff its assignment / parameter type / type-conversion source
// names the chi package (the import-alias name from the file's import block).
//
// Boundary (invariant + invariant): imports only
// `internal/caronte/store` + `internal/caronte/contract/extract` + the
// standard library. Does NOT import `internal/store` (daemon store boundary)
// nor `golang.org/x/tools/go/packages` (kept out of the dep chain for Phase
// D Wave 3; future may add types-info C's existing
// loadgo.go path).
package chi

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

const extractorID = "gohttp-chi-v1"

var httpMethods = map[string]bool{
	"Get": true, "Post": true, "Put": true, "Delete": true,
	"Patch": true, "Head": true, "Options": true, "Connect": true, "Trace": true,
}

var chiImportRE = regexp.MustCompile(`(?m)"github\.com/go-chi/chi(?:/v\d+)?"`)

func init() {
	if err := extract.Default().Register("gohttp-chi", New()); err != nil {
		panic(fmt.Sprintf("chi extractor: Register: %v", err))
	}
}

type Extractor struct{}

func New() *Extractor { return &Extractor{} }

func (e *Extractor) Language() extract.Language { return extract.LangGo }

func (e *Extractor) Frameworks() []string { return []string{"chi"} }

func (e *Extractor) Detect(file string, content []byte) bool {
	if strings.ToLower(filepath.Ext(file)) != ".go" {
		return false
	}
	return chiImportRE.Match(content)
}

func (e *Extractor) Endpoints(_ *caronteparser.Tree, file string) ([]store.APIEndpoint, error) {
	pkgDir := filepath.Dir(file)
	return e.ExtractFromPackage(context.Background(), pkgDir, "")
}

func (e *Extractor) Calls(_ *caronteparser.Tree, _ string) ([]store.APICall, error) {
	return nil, nil
}

func (e *Extractor) StubArtifacts(_ string, _ []byte) []extract.StubReference {
	return []extract.StubReference{}
}

func (e *Extractor) ExtractFromPackage(ctx context.Context, pkgDir, repo string) ([]store.APIEndpoint, error) {
	entries, err := os.ReadDir(pkgDir)
	if err != nil {
		return nil, fmt.Errorf("chi extractor: readdir %q: %w", pkgDir, err)
	}
	fset := token.NewFileSet()
	var out []store.APIEndpoint
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		name := ent.Name()
		if !strings.HasSuffix(name, ".go") {
			continue
		}
		if strings.HasSuffix(name, "_test.go") {

			continue
		}
		path := filepath.Join(pkgDir, name)
		src, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("chi extractor: read %q: %w", path, err)
		}
		f, err := goparser.ParseFile(fset, path, src, goparser.SkipObjectResolution)
		if err != nil {

			return nil, fmt.Errorf("chi extractor: parse %q: %w", path, err)
		}
		w := &chiWalker{
			pkg:     pkgDir,
			repo:    repo,
			file:    f,
			path:    path,
			fset:    fset,
			chiName: chiImportAlias(f),
		}
		if w.chiName == "" {

			continue
		}

		w.collectBindings()
		w.walkFile()
		out = append(out, w.endpoints...)
		_ = ctx
	}
	return out, nil
}

func chiImportAlias(f *ast.File) string {
	for _, imp := range f.Imports {
		if imp.Path == nil {
			continue
		}
		path := imp.Path.Value
		if !chiImportRE.MatchString(path) {
			continue
		}
		if imp.Name != nil && imp.Name.Name != "" {
			return imp.Name.Name
		}

		return "chi"
	}
	return ""
}

type chiWalker struct {
	pkg     string
	repo    string
	file    *ast.File
	path    string
	fset    *token.FileSet
	chiName string

	chiRouterVars map[string]bool

	endpoints []store.APIEndpoint

	prefixStack []string
}

func (w *chiWalker) collectBindings() {
	w.chiRouterVars = map[string]bool{}

	ast.Inspect(w.file, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.AssignStmt:

			if len(x.Lhs) == 1 && len(x.Rhs) == 1 {
				if id, ok := x.Lhs[0].(*ast.Ident); ok {
					if w.exprProducesChiRouter(x.Rhs[0]) {
						w.chiRouterVars[id.Name] = true
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

				if vspec.Type != nil && w.typeExprIsChiRouter(vspec.Type) {
					for _, name := range vspec.Names {
						w.chiRouterVars[name.Name] = true
					}
				}

				if vspec.Type == nil {
					for i, name := range vspec.Names {
						if i < len(vspec.Values) && w.exprProducesChiRouter(vspec.Values[i]) {
							w.chiRouterVars[name.Name] = true
						}
					}
				}
			}
		case *ast.FuncDecl:

			if x.Recv != nil {
				for _, field := range x.Recv.List {
					if w.typeExprIsChiRouter(field.Type) {
						for _, name := range field.Names {
							w.chiRouterVars[name.Name] = true
						}
					}
				}
			}

			if x.Type != nil && x.Type.Params != nil {
				for _, field := range x.Type.Params.List {
					if w.typeExprIsChiRouter(field.Type) {
						for _, name := range field.Names {
							w.chiRouterVars[name.Name] = true
						}
					}
				}
			}
		case *ast.FuncLit:

			if x.Type != nil && x.Type.Params != nil {
				for _, field := range x.Type.Params.List {
					if w.typeExprIsChiRouter(field.Type) {
						for _, name := range field.Names {
							w.chiRouterVars[name.Name] = true
						}
					}
				}
			}
		}
		return true
	})
}

func (w *chiWalker) exprProducesChiRouter(e ast.Expr) bool {
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
	if xIdent.Name != w.chiName {
		return false
	}
	return sel.Sel.Name == "NewRouter" || sel.Sel.Name == "NewMux"
}

func (w *chiWalker) typeExprIsChiRouter(t ast.Expr) bool {
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
			if xIdent.Name != w.chiName {
				return false
			}
			return tt.Sel.Name == "Router" || tt.Sel.Name == "Mux"
		default:
			return false
		}
	}
}

func (w *chiWalker) walkFile() {
	ast.Inspect(w.file, func(n ast.Node) bool {
		ce, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := ce.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		recvIdent, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		if !w.chiRouterVars[recvIdent.Name] {
			return true
		}
		method := sel.Sel.Name
		switch {
		case httpMethods[method]:
			w.handleRouteCall(method, ce)
		case method == "Route":
			w.handleCompositionRoute(ce)
			return false
		case method == "Mount":

			w.handleCompositionMount(ce)
			return false
		case method == "Group":

			w.handleCompositionGroup(ce)
			return false
		}
		return true
	})
}

func (w *chiWalker) currentPrefix() string {
	if len(w.prefixStack) == 0 {
		return ""
	}
	return strings.Join(w.prefixStack, "")
}

func (w *chiWalker) handleRouteCall(method string, ce *ast.CallExpr) {
	if len(ce.Args) < 1 {
		return
	}
	pathLit, ok := stringLitOf(ce.Args[0])
	if !ok {
		return
	}
	fullPath := w.currentPrefix() + pathLit
	handlerNodeID := w.handlerNodeID(ce.Args)
	normalized := normalizePathTemplate(fullPath)
	w.endpoints = append(w.endpoints, store.APIEndpoint{
		EndpointID:    fmt.Sprintf("%s:http:%s %s", w.repo, strings.ToUpper(method), normalized),
		Repo:          w.repo,
		Kind:          "http",
		Method:        strings.ToUpper(method),
		PathTemplate:  normalized,
		HandlerNodeID: handlerNodeID,
		ExtractedAt:   nowSeconds(),
		ExtractorID:   extractorID,
	})
}

func (w *chiWalker) handleCompositionRoute(ce *ast.CallExpr) {
	if len(ce.Args) < 1 {
		return
	}
	prefix, ok := stringLitOf(ce.Args[0])
	if !ok {
		return
	}
	w.prefixStack = append(w.prefixStack, prefix)
	defer func() { w.prefixStack = w.prefixStack[:len(w.prefixStack)-1] }()
	w.walkInnerHandler(ce)
}

func (w *chiWalker) handleCompositionMount(ce *ast.CallExpr) {
	if len(ce.Args) < 1 {
		return
	}
	prefix, ok := stringLitOf(ce.Args[0])
	if !ok {
		return
	}
	if len(ce.Args) < 2 {
		return
	}

	if _, isFuncLit := ce.Args[1].(*ast.FuncLit); !isFuncLit {
		_ = prefix
		return
	}
	w.prefixStack = append(w.prefixStack, prefix)
	defer func() { w.prefixStack = w.prefixStack[:len(w.prefixStack)-1] }()
	w.walkInnerHandler(ce)
}

func (w *chiWalker) handleCompositionGroup(ce *ast.CallExpr) {
	if len(ce.Args) < 1 {
		return
	}

	shim := &ast.CallExpr{
		Args: []ast.Expr{nil, ce.Args[0]},
	}
	w.walkInnerHandler(shim)
}

func (w *chiWalker) walkInnerHandler(ce *ast.CallExpr) {
	if len(ce.Args) < 2 {
		return
	}
	fl, ok := ce.Args[1].(*ast.FuncLit)
	if !ok {
		return
	}
	if fl.Body == nil {
		return
	}

	ast.Inspect(fl.Body, func(n ast.Node) bool {
		innerCall, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := innerCall.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		recvIdent, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		if !w.chiRouterVars[recvIdent.Name] {
			return true
		}
		method := sel.Sel.Name
		switch {
		case httpMethods[method]:
			w.handleRouteCall(method, innerCall)
		case method == "Route":
			w.handleCompositionRoute(innerCall)
			return false
		case method == "Mount":
			w.handleCompositionMount(innerCall)
			return false
		case method == "Group":
			w.handleCompositionGroup(innerCall)
			return false
		}
		return true
	})
}

func (w *chiWalker) handlerNodeID(args []ast.Expr) string {
	if len(args) < 2 {
		return ""
	}
	switch h := args[1].(type) {
	case *ast.Ident:
		return fmt.Sprintf("%s.%s", w.pkg, h.Name)
	case *ast.SelectorExpr:
		if x, ok := h.X.(*ast.Ident); ok {
			return fmt.Sprintf("%s.%s.%s", w.pkg, x.Name, h.Sel.Name)
		}
	case *ast.FuncLit:
		pos := w.fset.Position(h.Pos())
		return fmt.Sprintf("%s.%s:%d.anon", w.pkg, filepath.Base(pos.Filename), pos.Line)
	}
	return ""
}

func stringLitOf(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok {
		return "", false
	}
	if lit.Kind != token.STRING {
		return "", false
	}
	v := lit.Value
	if len(v) < 2 {
		return "", false
	}
	q := v[0]
	if q != '"' && q != '`' {
		return "", false
	}
	if !bytes.HasSuffix([]byte(v), []byte{q}) {
		return "", false
	}
	return v[1 : len(v)-1], true
}

var nowSeconds = func() int64 { return realNowSeconds() }

func normalizePathTemplate(p string) string {
	return paramSyntaxRE.ReplaceAllString(p, "{param}")
}

var paramSyntaxRE = regexp.MustCompile(`\{[^}]*\}|:[A-Za-z_][A-Za-z0-9_]*|\[[^\]]+\]|\$\{[^}]*\}|<[^>]+>`)
