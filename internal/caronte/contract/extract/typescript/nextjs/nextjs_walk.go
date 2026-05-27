// go:build cgo
//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

package nextjs

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/typescript/tsx"
	"github.com/smacker/go-tree-sitter/typescript/typescript"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

var httpMethodExports = map[string]bool{
	"GET":     true,
	"POST":    true,
	"PUT":     true,
	"DELETE":  true,
	"PATCH":   true,
	"OPTIONS": true,
	"HEAD":    true,
}

var extractedAtFn = func() int64 { return time.Now().Unix() }

func (e *Extractor) ExtractFromPackage(ctx context.Context, pkgRoot, repo string) ([]store.APIEndpoint, []store.APICall, error) {
	var (
		endpoints []store.APIEndpoint
		calls     []store.APICall
	)

	appRouteFiles, pagesRouteFiles, middlewareFiles, err := discoverNextFiles(pkgRoot)
	if err != nil {
		return nil, nil, err
	}

	now := extractedAtFn()
	for _, abs := range appRouteFiles {
		eps, err := extractAppRouterFile(ctx, abs, pkgRoot, repo, now)
		if err != nil {

			continue
		}
		endpoints = append(endpoints, eps...)
	}

	for _, abs := range middlewareFiles {
		call, ok := extractMiddlewareCall(abs, pkgRoot, repo, now)
		if ok {
			calls = append(calls, call)
		}
	}

	for _, abs := range pagesRouteFiles {
		eps, err := extractPagesRouterFile(ctx, abs, pkgRoot, repo, now)
		if err != nil {
			continue
		}
		endpoints = append(endpoints, eps...)
	}

	return endpoints, calls, nil
}

func discoverNextFiles(pkgRoot string) (appRoutes, pagesRoutes, middlewares []string, err error) {
	root := os.DirFS(pkgRoot)
	err = fs.WalkDir(root, ".", func(rel string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		ext := filepath.Ext(rel)
		if !isJSorTSExt(ext) {
			return nil
		}
		clean := filepath.ToSlash(rel)
		base := filepath.Base(clean)
		abs := filepath.Join(pkgRoot, rel)
		switch {
		case isStem(base, "middleware"):
			middlewares = append(middlewares, abs)
		case isStem(base, "route") && hasPrefixSegment(clean, "app"):
			appRoutes = append(appRoutes, abs)
		case hasPrefixSegments(clean, "pages", "api"):
			pagesRoutes = append(pagesRoutes, abs)
		}
		return nil
	})
	return appRoutes, pagesRoutes, middlewares, err
}

func extractAppRouterFile(ctx context.Context, abs, pkgRoot, repo string, now int64) ([]store.APIEndpoint, error) {
	src, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(pkgRoot, abs)
	if err != nil {
		return nil, err
	}
	relSlash := filepath.ToSlash(rel)
	pathTemplate := appRouterPath(relSlash)
	methods := findMethodExports(ctx, src, abs)
	if len(methods) == 0 {
		return nil, nil
	}
	eps := make([]store.APIEndpoint, 0, len(methods))
	for _, method := range methods {
		eps = append(eps, store.APIEndpoint{
			EndpointID:    fmt.Sprintf("%s:%s:%s", repo, method, pathTemplate),
			Repo:          repo,
			Kind:          "http",
			Method:        method,
			PathTemplate:  pathTemplate,
			HandlerNodeID: tsNodeID(relSlash, method),
			ExtractedAt:   now,
			ExtractorID:   ExtractorID,
		})
	}
	return eps, nil
}

func extractPagesRouterFile(ctx context.Context, abs, pkgRoot, repo string, now int64) ([]store.APIEndpoint, error) {
	src, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(pkgRoot, abs)
	if err != nil {
		return nil, err
	}
	relSlash := filepath.ToSlash(rel)
	pathTemplate := pagesRouterPath(relSlash)
	methods := findReqMethodDispatch(ctx, src, abs)
	if len(methods) == 0 {
		methods = []string{"*"}
	}
	eps := make([]store.APIEndpoint, 0, len(methods))
	for _, method := range methods {
		eps = append(eps, store.APIEndpoint{
			EndpointID:    fmt.Sprintf("%s:%s:%s", repo, method, pathTemplate),
			Repo:          repo,
			Kind:          "http",
			Method:        method,
			PathTemplate:  pathTemplate,
			HandlerNodeID: tsNodeID(relSlash, "default"),
			ExtractedAt:   now,
			ExtractorID:   ExtractorID,
		})
	}
	return eps, nil
}

func extractMiddlewareCall(abs, pkgRoot, repo string, now int64) (store.APICall, bool) {
	rel, err := filepath.Rel(pkgRoot, abs)
	if err != nil {
		return store.APICall{}, false
	}
	relSlash := filepath.ToSlash(rel)
	callerID := tsNodeID(relSlash, "middleware")
	return store.APICall{
		CallID:       fmt.Sprintf("%s:%s:middleware", repo, callerID),
		Repo:         repo,
		CallerNodeID: callerID,
		Confidence:   "static_path",
		ExtractedAt:  now,
		ExtractorID:  ExtractorID,
	}, true
}

func appRouterPath(rel string) string {

	dir := filepath.ToSlash(filepath.Dir(rel))

	if dir == "app" {
		return "/"
	}
	dir = strings.TrimPrefix(dir, "app/")
	segments := strings.Split(dir, "/")
	out := make([]string, 0, len(segments)+1)
	for _, seg := range segments {
		if seg == "" {
			continue
		}

		if strings.HasPrefix(seg, "(") && strings.HasSuffix(seg, ")") {
			continue
		}
		out = append(out, convertSegment(seg))
	}
	return "/" + strings.Join(out, "/")
}

func pagesRouterPath(rel string) string {
	ext := filepath.Ext(rel)
	noExt := strings.TrimSuffix(filepath.ToSlash(rel), ext)
	noExt = strings.TrimPrefix(noExt, "pages/")
	if noExt == "" {
		return "/"
	}
	segments := strings.Split(noExt, "/")
	out := make([]string, 0, len(segments)+1)
	for _, seg := range segments {
		if seg == "" || seg == "index" {
			continue
		}
		out = append(out, convertSegment(seg))
	}
	if len(out) == 0 {
		return "/"
	}
	return "/" + strings.Join(out, "/")
}

func convertSegment(seg string) string {

	if strings.HasPrefix(seg, "[[...") && strings.HasSuffix(seg, "]]") {
		inner := seg[len("[[...") : len(seg)-len("]]")]
		return "{" + inner + "?...}"
	}

	if strings.HasPrefix(seg, "[...") && strings.HasSuffix(seg, "]") {
		inner := seg[len("[...") : len(seg)-1]
		return "{" + inner + "...}"
	}

	if strings.HasPrefix(seg, "[") && strings.HasSuffix(seg, "]") {
		inner := seg[1 : len(seg)-1]
		return "{" + inner + "}"
	}
	return seg
}

func tsNodeID(file, name string) string {
	mod := tsModulePath(file)
	if mod == "" {
		return name
	}
	return mod + "." + name
}

func tsModulePath(file string) string {
	slash := strings.ReplaceAll(file, "\\", "/")
	dot := strings.LastIndex(slash, ".")
	var noExt string
	if dot < 0 {
		noExt = slash
	} else {
		noExt = slash[:dot]
	}
	return strings.ReplaceAll(noExt, "/", ".")
}

func pickGrammar(file string) *sitter.Language {
	switch strings.ToLower(filepath.Ext(file)) {
	case ".tsx", ".jsx":
		return tsx.GetLanguage()
	}
	return typescript.GetLanguage()
}

func findMethodExports(ctx context.Context, src []byte, file string) []string {
	defer recoverParse(file)
	parser := sitter.NewParser()
	parser.SetLanguage(pickGrammar(file))
	tree, err := parser.ParseCtx(ctx, nil, src)
	if err != nil || tree == nil {
		return nil
	}
	defer tree.Close()
	root := tree.RootNode()
	if root == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	walkNode(root, func(node *sitter.Node) {
		if node.Type() != "export_statement" {
			return
		}
		names := extractExportedNames(node, src)
		for _, name := range names {
			if !httpMethodExports[name] {
				continue
			}
			if seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, name)
		}
	})
	return out
}

func extractExportedNames(node *sitter.Node, src []byte) []string {
	var names []string
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		switch child.Type() {
		case "function_declaration":
			if name := child.ChildByFieldName("name"); name != nil {
				names = append(names, name.Content(src))
			}
		case "lexical_declaration":

			for j := 0; j < int(child.ChildCount()); j++ {
				vd := child.Child(j)
				if vd == nil || vd.Type() != "variable_declarator" {
					continue
				}
				if name := vd.ChildByFieldName("name"); name != nil {
					names = append(names, name.Content(src))
				}
			}
		case "export_clause":

			for j := 0; j < int(child.ChildCount()); j++ {
				spec := child.Child(j)
				if spec == nil || spec.Type() != "export_specifier" {
					continue
				}
				alias := spec.ChildByFieldName("alias")
				if alias != nil {
					names = append(names, alias.Content(src))
					continue
				}
				if name := spec.ChildByFieldName("name"); name != nil {
					names = append(names, name.Content(src))
				}
			}
		}
	}
	return names
}

func findReqMethodDispatch(ctx context.Context, src []byte, file string) []string {
	defer recoverParse(file)
	parser := sitter.NewParser()
	parser.SetLanguage(pickGrammar(file))
	tree, err := parser.ParseCtx(ctx, nil, src)
	if err != nil || tree == nil {
		return nil
	}
	defer tree.Close()
	root := tree.RootNode()
	if root == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	walkNode(root, func(node *sitter.Node) {
		if node.Type() != "binary_expression" {
			return
		}
		method, ok := reqMethodComparisonLiteral(node, src)
		if !ok {
			return
		}
		upper := strings.ToUpper(method)
		if !httpMethodExports[upper] {
			return
		}
		if seen[upper] {
			return
		}
		seen[upper] = true
		out = append(out, upper)
	})
	return out
}

func reqMethodComparisonLiteral(node *sitter.Node, src []byte) (string, bool) {
	if node.ChildCount() < 3 {
		return "", false
	}
	left := node.Child(0)
	op := node.Child(1)
	right := node.Child(2)
	if left == nil || op == nil || right == nil {
		return "", false
	}
	opStr := op.Content(src)
	if opStr != "===" && opStr != "==" {
		return "", false
	}

	if !isReqDotMethod(left, src) && !isReqDotMethod(right, src) {
		return "", false
	}

	lit := right
	if isReqDotMethod(right, src) {
		lit = left
	}
	if lit.Type() != "string" {
		return "", false
	}
	val := stringLiteralContent(lit, src)
	if val == "" {
		return "", false
	}
	return val, true
}

func isReqDotMethod(node *sitter.Node, src []byte) bool {
	if node.Type() != "member_expression" {
		return false
	}
	obj := node.ChildByFieldName("object")
	prop := node.ChildByFieldName("property")
	if obj == nil || prop == nil {
		return false
	}
	return obj.Content(src) == "req" && prop.Content(src) == "method"
}

func stringLiteralContent(node *sitter.Node, src []byte) string {
	if node == nil || node.Type() != "string" {
		return ""
	}
	var buf strings.Builder
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		if child.Type() == "string_fragment" {
			buf.WriteString(child.Content(src))
		}
	}
	return buf.String()
}

func walkNode(node *sitter.Node, visit func(*sitter.Node)) {
	if node == nil {
		return
	}
	visit(node)
	for i := 0; i < int(node.ChildCount()); i++ {
		walkNode(node.Child(i), visit)
	}
}

func recoverParse(file string) {
	if r := recover(); r != nil {

		_ = file
		_ = r
	}
}
