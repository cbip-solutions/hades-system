// SPDX-License-Identifier: MIT
// Package stdlib implements the Go stdlib net/http route extractor for Plan
// 20, covering Go 1.22's "METHOD HOST/path/{param}" pattern syntax
// + the pre-1.22 legacy catch-all "/path" form. Both `http.HandleFunc /
// http.Handle` (against the default ServeMux) and `mux.HandleFunc /
// mux.Handle` (against an explicit *http.ServeMux) are extracted.
//
// Detect(file, content): the file imports `net/http` AND does NOT import a
// higher-level router (chi/gin/echo) — co-resident files with chi/gin/echo
// imports are owned by those extractors; the stdlib extractor only catches
// the residual.
//
// Pattern parsing (parseGo122Pattern): the 1.22 grammar is "[METHOD ][HOST]
// /path[{wildcard}|{wildcard...}]". We decompose left-to-right:
// - "GET /health" → ("GET", "/health")
// - "POST example.com/x/{id}" → ("POST", "/x/{id}") (host stripped)
// - "/legacy" → ("", "/legacy") (catch-all)
// - "/{$}" → ("", "/{$}")
//
// Boundary (inv-hades-230 + inv-hades-271): no internal/store import; no
// golang.org/x/tools/go/packages.
package stdlib

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

const extractorID = "gohttp-stdlib-v1"

var (
	netHTTPImportRE = regexp.MustCompile(`(?m)"net/http"`)
	chiImportRE     = regexp.MustCompile(`(?m)"github\.com/go-chi/chi(?:/v\d+)?"`)
	ginImportRE     = regexp.MustCompile(`(?m)"github\.com/gin-gonic/gin"`)
	echoImportRE    = regexp.MustCompile(`(?m)"github\.com/labstack/echo(?:/v\d+)?"`)
)

func init() {
	if err := extract.Default().Register("gohttp-stdlib", New()); err != nil {
		panic(fmt.Sprintf("stdlib extractor: Register: %v", err))
	}
}

type Extractor struct{}

func New() *Extractor { return &Extractor{} }

func (e *Extractor) Language() extract.Language { return extract.LangGo }

func (e *Extractor) Frameworks() []string { return []string{"stdlib"} }

func (e *Extractor) Detect(file string, content []byte) bool {
	if strings.ToLower(filepath.Ext(file)) != ".go" {
		return false
	}
	if !netHTTPImportRE.Match(content) {
		return false
	}
	if chiImportRE.Match(content) || ginImportRE.Match(content) || echoImportRE.Match(content) {
		return false
	}
	return true
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
		return nil, fmt.Errorf("stdlib extractor: readdir %q: %w", pkgDir, err)
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
			return nil, fmt.Errorf("stdlib extractor: read %q: %w", path, err)
		}
		f, err := goparser.ParseFile(fset, path, src, goparser.SkipObjectResolution)
		if err != nil {
			return nil, fmt.Errorf("stdlib extractor: parse %q: %w", path, err)
		}

		if !fileImportsNetHTTP(f) || fileImportsHigherLevel(f) {
			continue
		}
		httpName := httpImportAlias(f)
		w := &stdlibWalker{
			pkg:      pkgDir,
			repo:     repo,
			file:     f,
			path:     path,
			fset:     fset,
			httpName: httpName,
		}
		w.collectServeMuxBindings()
		w.walkFile()
		out = append(out, w.endpoints...)
	}
	return out, nil
}

func fileImportsNetHTTP(f *ast.File) bool {
	for _, imp := range f.Imports {
		if imp.Path == nil {
			continue
		}
		if imp.Path.Value == `"net/http"` {
			return true
		}
	}
	return false
}

func fileImportsHigherLevel(f *ast.File) bool {
	for _, imp := range f.Imports {
		if imp.Path == nil {
			continue
		}
		v := imp.Path.Value
		if chiImportRE.MatchString(v) || ginImportRE.MatchString(v) || echoImportRE.MatchString(v) {
			return true
		}
	}
	return false
}

func httpImportAlias(f *ast.File) string {
	for _, imp := range f.Imports {
		if imp.Path == nil {
			continue
		}
		if imp.Path.Value != `"net/http"` {
			continue
		}
		if imp.Name != nil && imp.Name.Name != "" {
			return imp.Name.Name
		}
		return "http"
	}
	return "http"
}

type stdlibWalker struct {
	pkg      string
	repo     string
	file     *ast.File
	path     string
	fset     *token.FileSet
	httpName string

	muxVars map[string]bool

	endpoints []store.APIEndpoint
}

func (w *stdlibWalker) collectServeMuxBindings() {
	w.muxVars = map[string]bool{}
	ast.Inspect(w.file, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.AssignStmt:
			if len(x.Lhs) == 1 && len(x.Rhs) == 1 {
				if id, ok := x.Lhs[0].(*ast.Ident); ok {
					if w.exprProducesServeMux(x.Rhs[0]) {
						w.muxVars[id.Name] = true
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
				if vspec.Type != nil && w.typeExprIsServeMux(vspec.Type) {
					for _, name := range vspec.Names {
						w.muxVars[name.Name] = true
					}
				}
				if vspec.Type == nil {
					for i, name := range vspec.Names {
						if i < len(vspec.Values) && w.exprProducesServeMux(vspec.Values[i]) {
							w.muxVars[name.Name] = true
						}
					}
				}
			}
		case *ast.FuncDecl:
			if x.Type != nil && x.Type.Params != nil {
				for _, field := range x.Type.Params.List {
					if w.typeExprIsServeMux(field.Type) {
						for _, name := range field.Names {
							w.muxVars[name.Name] = true
						}
					}
				}
			}
		}
		return true
	})
}

func (w *stdlibWalker) exprProducesServeMux(e ast.Expr) bool {
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
	if xIdent.Name != w.httpName {
		return false
	}
	return sel.Sel.Name == "NewServeMux"
}

func (w *stdlibWalker) typeExprIsServeMux(t ast.Expr) bool {
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
			if xIdent.Name != w.httpName {
				return false
			}
			return tt.Sel.Name == "ServeMux"
		default:
			return false
		}
	}
}

func (w *stdlibWalker) walkFile() {
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
		if method != "HandleFunc" && method != "Handle" {
			return true
		}
		xIdent, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}

		if xIdent.Name != w.httpName && !w.muxVars[xIdent.Name] {
			return true
		}
		w.handleStdlibRegistration(ce)
		return true
	})
}

func (w *stdlibWalker) handleStdlibRegistration(ce *ast.CallExpr) {
	if len(ce.Args) < 1 {
		return
	}
	pat, ok := stringLitOf(ce.Args[0])
	if !ok {
		return
	}
	method, path := parseGo122Pattern(pat)
	handlerNodeID := w.handlerNodeID(ce.Args)
	normalized := normalizePathTemplate(path)
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

func parseGo122Pattern(p string) (method, path string) {
	if p == "" {
		return "", ""
	}

	if i := strings.IndexByte(p, ' '); i > 0 {
		head := p[:i]
		if isAllUpperASCII(head) {
			rest := strings.TrimLeft(p[i+1:], " ")
			if h := strings.IndexByte(rest, '/'); h > 0 {

				return head, rest[h:]
			}
			return head, rest
		}
	}

	if i := strings.IndexByte(p, '/'); i > 0 {
		return "", p[i:]
	}
	return "", p
}

func isAllUpperASCII(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 'A' || c > 'Z' {
			return false
		}
	}
	return true
}

func (w *stdlibWalker) handlerNodeID(args []ast.Expr) string {
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
	case *ast.CallExpr:

		if sel, ok := h.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "HandlerFunc" {
			if len(h.Args) == 1 {
				return w.handlerNodeID([]ast.Expr{nil, h.Args[0]})
			}
		}
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
