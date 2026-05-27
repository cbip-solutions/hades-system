// SPDX-License-Identifier: MIT
// Package echo implements the labstack/echo HTTP route extractor for Plan
// 20
//
// Strategy identical to the gin extractor (go/parser AST + syntactic
// receiver-type inference + value-returning Group binding); only the
// receiver type set + import path differ:
// - *echo.Echo (top-level router from echo.New())
// - *echo.Group (value returned by e.Group(prefix,...))
//
// echo's `Reverse(...)` is NOT a route registration — it's a reverse-URL
// lookup helper; the extractor never emits a row for Reverse calls (the
// httpMethods set excludes it).
//
// Boundary (inv-hades-230 + inv-hades-271): no internal/store import; no
// golang.org/x/tools/go/packages.
package echo

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

const extractorID = "gohttp-echo-v1"

var httpMethods = map[string]bool{
	"GET": true, "POST": true, "PUT": true, "DELETE": true,
	"PATCH": true, "HEAD": true, "OPTIONS": true, "CONNECT": true, "TRACE": true,
}

var echoImportRE = regexp.MustCompile(`(?m)"github\.com/labstack/echo(?:/v\d+)?"`)

func init() {
	if err := extract.Default().Register("gohttp-echo", New()); err != nil {
		panic(fmt.Sprintf("echo extractor: Register: %v", err))
	}
}

type Extractor struct{}

func New() *Extractor { return &Extractor{} }

func (e *Extractor) Language() extract.Language { return extract.LangGo }

func (e *Extractor) Frameworks() []string { return []string{"echo"} }

func (e *Extractor) Detect(file string, content []byte) bool {
	if strings.ToLower(filepath.Ext(file)) != ".go" {
		return false
	}
	return echoImportRE.Match(content)
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
		return nil, fmt.Errorf("echo extractor: readdir %q: %w", pkgDir, err)
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
			return nil, fmt.Errorf("echo extractor: read %q: %w", path, err)
		}
		f, err := goparser.ParseFile(fset, path, src, goparser.SkipObjectResolution)
		if err != nil {
			return nil, fmt.Errorf("echo extractor: parse %q: %w", path, err)
		}
		w := &echoWalker{
			pkg:      pkgDir,
			repo:     repo,
			file:     f,
			path:     path,
			fset:     fset,
			echoName: echoImportAlias(f),
		}
		if w.echoName == "" {
			continue
		}
		w.collectBindings()
		w.walkFile()
		out = append(out, w.endpoints...)
	}
	return out, nil
}

func echoImportAlias(f *ast.File) string {
	for _, imp := range f.Imports {
		if imp.Path == nil {
			continue
		}
		if !echoImportRE.MatchString(imp.Path.Value) {
			continue
		}
		if imp.Name != nil && imp.Name.Name != "" {
			return imp.Name.Name
		}
		return "echo"
	}
	return ""
}

type echoWalker struct {
	pkg      string
	repo     string
	file     *ast.File
	path     string
	fset     *token.FileSet
	echoName string

	echoRouterVars map[string]string
	endpoints      []store.APIEndpoint
}

func (w *echoWalker) collectBindings() {
	w.echoRouterVars = map[string]string{}
	w.collectInitialBindings()
	for i := 0; i < 8; i++ {
		before := len(w.echoRouterVars)
		w.collectGroupBindings()
		if len(w.echoRouterVars) == before {
			break
		}
	}
}

func (w *echoWalker) collectInitialBindings() {
	ast.Inspect(w.file, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.AssignStmt:
			if len(x.Lhs) == 1 && len(x.Rhs) == 1 {
				if id, ok := x.Lhs[0].(*ast.Ident); ok {
					if w.exprProducesEchoRouter(x.Rhs[0]) {
						w.echoRouterVars[id.Name] = ""
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
				if vspec.Type != nil && w.typeExprIsEchoReceiver(vspec.Type) {
					for _, name := range vspec.Names {
						w.echoRouterVars[name.Name] = ""
					}
				}
				if vspec.Type == nil {
					for i, name := range vspec.Names {
						if i < len(vspec.Values) && w.exprProducesEchoRouter(vspec.Values[i]) {
							w.echoRouterVars[name.Name] = ""
						}
					}
				}
			}
		case *ast.FuncDecl:
			if x.Recv != nil {
				for _, field := range x.Recv.List {
					if w.typeExprIsEchoReceiver(field.Type) {
						for _, name := range field.Names {
							w.echoRouterVars[name.Name] = ""
						}
					}
				}
			}
			if x.Type != nil && x.Type.Params != nil {
				for _, field := range x.Type.Params.List {
					if w.typeExprIsEchoReceiver(field.Type) {
						for _, name := range field.Names {
							w.echoRouterVars[name.Name] = ""
						}
					}
				}
			}
		case *ast.FuncLit:
			if x.Type != nil && x.Type.Params != nil {
				for _, field := range x.Type.Params.List {
					if w.typeExprIsEchoReceiver(field.Type) {
						for _, name := range field.Names {
							w.echoRouterVars[name.Name] = ""
						}
					}
				}
			}
		}
		return true
	})
}

func (w *echoWalker) collectGroupBindings() {
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
		parentPrefix, parentKnown := w.echoRouterVars[recvIdent.Name]
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
		w.echoRouterVars[lhsIdent.Name] = parentPrefix + prefix
		return true
	})
}

func (w *echoWalker) exprProducesEchoRouter(e ast.Expr) bool {
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
	if xIdent.Name != w.echoName {
		return false
	}
	return sel.Sel.Name == "New"
}

func (w *echoWalker) typeExprIsEchoReceiver(t ast.Expr) bool {
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
			if xIdent.Name != w.echoName {
				return false
			}
			return tt.Sel.Name == "Echo" || tt.Sel.Name == "Group"
		default:
			return false
		}
	}
}

func (w *echoWalker) walkFile() {
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
			prefix, known := w.echoRouterVars[recvIdent.Name]
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

func (w *echoWalker) resolveChainPrefix(e ast.Expr) (string, bool) {
	switch x := e.(type) {
	case *ast.Ident:
		p, ok := w.echoRouterVars[x.Name]
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

func (w *echoWalker) handleRouteCall(method string, ce *ast.CallExpr, prefix string) {
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

func (w *echoWalker) handlerNodeID(args []ast.Expr) string {
	if len(args) < 2 {
		return ""
	}
	h := args[1]
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
