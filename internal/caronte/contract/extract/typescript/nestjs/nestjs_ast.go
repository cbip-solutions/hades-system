// go:build cgo
//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

package nestjs

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/typescript/tsx"
	"github.com/smacker/go-tree-sitter/typescript/typescript"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

var nestjsMethods = map[string]string{
	"Get":     "GET",
	"Post":    "POST",
	"Put":     "PUT",
	"Delete":  "DELETE",
	"Patch":   "PATCH",
	"Options": "OPTIONS",
	"Head":    "HEAD",
	"All":     "*",
}

var extractedAtFn = func() int64 { return time.Now().Unix() }

func pickGrammar(file string) *sitter.Language {
	if strings.ToLower(filepath.Ext(file)) == ".tsx" {
		return tsx.GetLanguage()
	}
	return typescript.GetLanguage()
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

func tsNodeID(file, owner, name string) string {
	mod := tsModulePath(file)
	parts := make([]string, 0, 3)
	if mod != "" {
		parts = append(parts, mod)
	}
	if owner != "" {
		parts = append(parts, owner)
	}
	parts = append(parts, name)
	return strings.Join(parts, ".")
}

func (e *Extractor) EndpointsFromBytes(ctx context.Context, file string, src []byte, repo, repoRoot string) ([]store.APIEndpoint, error) {
	if repoRoot != "" {
		eps, ok := e.endpointsFromArtifact(repoRoot, repo)
		if ok {
			return eps, nil
		}
	}
	return e.endpointsFromAST(ctx, file, src, repo)
}

func (e *Extractor) endpointsFromAST(ctx context.Context, file string, src []byte, repo string) (eps []store.APIEndpoint, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("nestjs: ast extraction panic on %s: %v", file, r)
		}
	}()
	p := sitter.NewParser()
	p.SetLanguage(pickGrammar(file))
	tree, parseErr := p.ParseCtx(ctx, nil, src)
	if parseErr != nil {
		return nil, fmt.Errorf("nestjs: parse %s: %w", file, parseErr)
	}
	defer tree.Close()
	root := tree.RootNode()
	if root == nil {
		return nil, nil
	}
	now := extractedAtFn()
	walkClasses(root, src, func(className, controllerPrefix, classTags string, methodDecls []methodDecl) {
		for _, m := range methodDecls {
			method, ok := nestjsMethods[m.decoratorName]
			if !ok {
				continue
			}
			fullPath := canonicalisePath(joinPath(controllerPrefix, m.path))
			handler := m.operationID
			if handler == "" {
				handler = tsNodeID(file, className, m.methodName)
			}
			ep := store.APIEndpoint{
				EndpointID:    fmt.Sprintf("%s:%s:%s", repo, method, fullPath),
				Repo:          repo,
				Kind:          "http",
				Method:        method,
				PathTemplate:  fullPath,
				HandlerNodeID: handler,
				ExtractedAt:   now,
				ExtractorID:   ExtractorID,
			}

			_ = classTags
			eps = append(eps, ep)
		}
	})
	return eps, nil
}

type methodDecl struct {
	decoratorName string
	methodName    string
	path          string
	operationID   string
}

func walkClasses(node *sitter.Node, src []byte, visit func(className, controllerPrefix, classTags string, decls []methodDecl)) {
	if node == nil {
		return
	}
	if node.Type() == "class_declaration" {
		processClass(node, src, visit)
	}
	for i := 0; i < int(node.ChildCount()); i++ {
		walkClasses(node.Child(i), src, visit)
	}
}

func processClass(cls *sitter.Node, src []byte, visit func(className, controllerPrefix, classTags string, decls []methodDecl)) {
	className := ""
	if name := cls.ChildByFieldName("name"); name != nil {
		className = name.Content(src)
	}
	controllerPrefix := ""
	classTags := ""
	controllerFound := false

	if parent := cls.Parent(); parent != nil && parent.Type() == "export_statement" {
		for i := 0; i < int(parent.ChildCount()); i++ {
			c := parent.Child(i)
			if c == nil || c.Type() != "decorator" {
				continue
			}
			name, arg := parseClassDecorator(c, src)
			switch name {
			case "Controller":
				controllerFound = true
				controllerPrefix = arg
			case "ApiTags":
				classTags = arg
			}
		}
	}

	if !controllerFound {
		for i := 0; i < int(cls.ChildCount()); i++ {
			c := cls.Child(i)
			if c == nil || c.Type() != "decorator" {
				continue
			}
			name, arg := parseClassDecorator(c, src)
			switch name {
			case "Controller":
				controllerFound = true
				controllerPrefix = arg
			case "ApiTags":
				classTags = arg
			}
		}
	}

	if !controllerFound {
		return
	}
	body := cls.ChildByFieldName("body")
	if body == nil {
		return
	}
	decls := collectMethodDecorators(body, src)
	visit(className, controllerPrefix, classTags, decls)
}

func parseClassDecorator(decorator *sitter.Node, src []byte) (name, arg string) {
	expr := decoratorExpression(decorator)
	if expr == nil {
		return "", ""
	}
	switch expr.Type() {
	case "identifier":
		return expr.Content(src), ""
	case "call_expression":
		fn := expr.ChildByFieldName("function")
		if fn == nil || fn.Type() != "identifier" {
			return "", ""
		}
		name = fn.Content(src)
		args := expr.ChildByFieldName("arguments")
		arg = firstPositionalString(args, src)
		return name, arg
	}
	return "", ""
}

func decoratorExpression(decorator *sitter.Node) *sitter.Node {
	for i := 0; i < int(decorator.ChildCount()); i++ {
		c := decorator.Child(i)
		if c == nil {
			continue
		}
		if c.Type() == "@" {
			continue
		}
		return c
	}
	return nil
}

func collectMethodDecorators(body *sitter.Node, src []byte) []methodDecl {
	var decls []methodDecl
	for i := 0; i < int(body.ChildCount()); i++ {
		c := body.Child(i)
		if c == nil {
			continue
		}
		if c.Type() != "method_definition" {
			continue
		}
		methodName := ""
		if n := c.ChildByFieldName("name"); n != nil {
			methodName = n.Content(src)
		}

		methodDecorators := trailingDecoratorRun(body, src, i)
		var opID string
		var httpDecorators []methodDecl
		for _, d := range methodDecorators {
			name, arg := parseClassDecorator(d, src)
			switch {
			case name == "ApiOperation":
				opID = extractApiOperationID(d, src)
			case nestjsMethods[name] != "":
				httpDecorators = append(httpDecorators, methodDecl{
					decoratorName: name,
					methodName:    methodName,
					path:          arg,
				})
			}
		}
		for _, md := range httpDecorators {
			md.operationID = opID
			decls = append(decls, md)
		}
	}
	return decls
}

func trailingDecoratorRun(body *sitter.Node, src []byte, methodIdx int) []*sitter.Node {
	var run []*sitter.Node
	for j := methodIdx - 1; j >= 0; j-- {
		c := body.Child(j)
		if c == nil {
			continue
		}
		if c.Type() != "decorator" {
			break
		}

		run = append([]*sitter.Node{c}, run...)
	}
	return run
}

func extractApiOperationID(decorator *sitter.Node, src []byte) string {
	expr := decoratorExpression(decorator)
	if expr == nil || expr.Type() != "call_expression" {
		return ""
	}
	args := expr.ChildByFieldName("arguments")
	if args == nil {
		return ""
	}

	var obj *sitter.Node
	for i := 0; i < int(args.ChildCount()); i++ {
		c := args.Child(i)
		if c == nil {
			continue
		}
		switch c.Type() {
		case "(", ")", ",":
			continue
		case "object":
			obj = c
		}
		if obj != nil {
			break
		}
	}
	if obj == nil {
		return ""
	}

	for i := 0; i < int(obj.ChildCount()); i++ {
		pair := obj.Child(i)
		if pair == nil || pair.Type() != "pair" {
			continue
		}
		key := pair.ChildByFieldName("key")
		value := pair.ChildByFieldName("value")
		if key == nil || value == nil {
			continue
		}
		keyName := key.Content(src)

		keyName = strings.Trim(keyName, `"'`)
		if keyName != "operationId" {
			continue
		}
		if value.Type() == "string" {
			return stringLiteralContent(value, src)
		}
	}
	return ""
}

func firstPositionalString(argList *sitter.Node, src []byte) string {
	if argList == nil {
		return ""
	}
	for i := 0; i < int(argList.ChildCount()); i++ {
		c := argList.Child(i)
		if c == nil {
			continue
		}
		switch c.Type() {
		case "(", ")", ",":
			continue
		case "string":
			return stringLiteralContent(c, src)
		}
	}
	return ""
}

func stringLiteralContent(node *sitter.Node, src []byte) string {
	if node == nil || node.Type() != "string" {
		return ""
	}
	var buf strings.Builder
	for i := 0; i < int(node.ChildCount()); i++ {
		c := node.Child(i)
		if c == nil {
			continue
		}
		if c.Type() == "string_fragment" {
			buf.WriteString(c.Content(src))
		}
	}
	return buf.String()
}

func joinPath(prefix, path string) string {
	if !strings.HasPrefix(prefix, "/") && prefix != "" {
		prefix = "/" + prefix
	}
	if path != "" && !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	out := prefix + path
	if out == "" {
		return "/"
	}
	return out
}

func canonicalisePath(p string) string {
	if p == "" {
		return ""
	}

	out := strings.Builder{}
	i := 0
	for i < len(p) {
		ch := p[i]
		if ch != ':' {
			out.WriteByte(ch)
			i++
			continue
		}

		j := i + 1
		for j < len(p) && p[j] != '/' {
			j++
		}
		out.WriteByte('{')
		out.WriteString(p[i+1 : j])
		out.WriteByte('}')
		i = j
	}
	canon := out.String()

	canon = strings.ReplaceAll(canon, "[", "{")
	canon = strings.ReplaceAll(canon, "]", "}")
	for strings.Contains(canon, "//") {
		canon = strings.ReplaceAll(canon, "//", "/")
	}
	if len(canon) > 1 && strings.HasSuffix(canon, "/") {
		canon = strings.TrimRight(canon, "/")
	}
	return canon
}
