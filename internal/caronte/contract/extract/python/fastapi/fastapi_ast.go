//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

package fastapi

import (
	"context"
	"fmt"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/python"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

var fastapiHTTPMethods = map[string]bool{
	"get":     true,
	"post":    true,
	"put":     true,
	"delete":  true,
	"patch":   true,
	"head":    true,
	"options": true,
	"trace":   true,
}

type routerBinding struct {
	prefix string
}

type includeEdge struct {
	parent string
	child  string
	prefix string
}

func (e *Extractor) endpointsFromAST(ctx context.Context, file string, src []byte, repo string) (eps []store.APIEndpoint, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("fastapi: ast extraction panic on %s: %v", file, r)
		}
	}()

	parser := sitter.NewParser()
	parser.SetLanguage(python.GetLanguage())
	tree, parseErr := parser.ParseCtx(ctx, nil, src)
	if parseErr != nil {
		return nil, fmt.Errorf("fastapi: parse %s: %w", file, parseErr)
	}
	defer tree.Close()
	root := tree.RootNode()
	if root == nil {
		return nil, nil
	}

	bindings := make(map[string]*routerBinding)
	var edges []includeEdge
	collectRoutersAndIncludes(root, src, bindings, &edges)

	composed := composeRouterPrefixes(bindings, edges)

	now := extractedAtFn()
	walkDecorators(root, src, func(routerVar, method, decoratorPath, funcName string, pos uint32) {
		if !fastapiHTTPMethods[strings.ToLower(method)] {
			return
		}
		prefix := composed[routerVar]
		fullPath := canonicalisePath(prefix + decoratorPath)
		ep := store.APIEndpoint{
			EndpointID:    fmt.Sprintf("%s:%s:%s", repo, strings.ToUpper(method), fullPath),
			Repo:          repo,
			Kind:          "http",
			Method:        strings.ToUpper(method),
			PathTemplate:  fullPath,
			HandlerNodeID: pyNodeID(file, funcName),
			ExtractedAt:   now,
			ExtractorID:   ExtractorID,
		}
		eps = append(eps, ep)
	})
	return eps, nil
}

func composeRouterPrefixes(bindings map[string]*routerBinding, edges []includeEdge) map[string]string {
	// Build a child→edge index. A router may have at most one inbound edge
	// in well-formed code; on multiple inbounds, the first wins (warning is
	// surfaced via the audit emitter in ).
	inbound := make(map[string]includeEdge)
	for _, edge := range edges {
		if _, exists := inbound[edge.child]; !exists {
			inbound[edge.child] = edge
		}
	}
	composed := make(map[string]string, len(bindings))
	for routerVar := range bindings {
		composed[routerVar] = resolveRouterPrefix(routerVar, bindings, inbound, map[string]bool{})
	}
	return composed
}

func resolveRouterPrefix(routerVar string, bindings map[string]*routerBinding, inbound map[string]includeEdge, visited map[string]bool) string {
	if visited[routerVar] {
		return ""
	}
	visited[routerVar] = true
	var ownPrefix string
	if b, ok := bindings[routerVar]; ok {
		ownPrefix = b.prefix
	}
	edge, hasInbound := inbound[routerVar]
	if !hasInbound {
		return ownPrefix
	}

	parentComposed := resolveRouterPrefix(edge.parent, bindings, inbound, visited)
	return parentComposed + edge.prefix + ownPrefix
}

func collectRoutersAndIncludes(node *sitter.Node, src []byte, bindings map[string]*routerBinding, edges *[]includeEdge) {
	if node == nil {
		return
	}

	switch node.Type() {
	case "assignment":
		collectRouterBinding(node, src, bindings)
	case "call":
		collectIncludeEdge(node, src, edges)
	}

	for i := 0; i < int(node.ChildCount()); i++ {
		collectRoutersAndIncludes(node.Child(i), src, bindings, edges)
	}
}

func collectRouterBinding(assign *sitter.Node, src []byte, bindings map[string]*routerBinding) {
	if assign.ChildCount() < 3 {
		return
	}
	lhs := assign.Child(0)
	rhs := assign.Child(2)
	if lhs == nil || rhs == nil || lhs.Type() != "identifier" || rhs.Type() != "call" {
		return
	}
	fn := rhs.ChildByFieldName("function")
	if fn == nil || !isAPIRouterCall(fn, src) {
		return
	}

	prefix := extractKeywordArg(rhs.ChildByFieldName("arguments"), src, "prefix")
	bindings[lhs.Content(src)] = &routerBinding{prefix: prefix}
}

func isAPIRouterCall(fn *sitter.Node, src []byte) bool {
	switch fn.Type() {
	case "identifier":
		return fn.Content(src) == "APIRouter"
	case "attribute":

		attr := fn.ChildByFieldName("attribute")
		if attr == nil {
			return false
		}
		return attr.Content(src) == "APIRouter"
	}
	return false
}

func collectIncludeEdge(call *sitter.Node, src []byte, edges *[]includeEdge) {
	fn := call.ChildByFieldName("function")
	if fn == nil || fn.Type() != "attribute" {
		return
	}
	method := fn.ChildByFieldName("attribute")
	if method == nil || method.Content(src) != "include_router" {
		return
	}
	parentNode := fn.ChildByFieldName("object")
	if parentNode == nil || parentNode.Type() != "identifier" {
		return
	}
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return
	}

	childVar := firstPositionalIdentifier(args, src)
	if childVar == "" {
		return
	}
	prefix := extractKeywordArg(args, src, "prefix")
	*edges = append(*edges, includeEdge{
		parent: parentNode.Content(src),
		child:  childVar,
		prefix: prefix,
	})
}

func firstPositionalIdentifier(argList *sitter.Node, src []byte) string {
	for i := 0; i < int(argList.ChildCount()); i++ {
		child := argList.Child(i)
		if child == nil {
			continue
		}
		switch child.Type() {
		case "(", ")", ",", "keyword_argument":
			continue
		case "identifier":
			return child.Content(src)
		default:

			return ""
		}
	}
	return ""
}

func extractKeywordArg(argList *sitter.Node, src []byte, name string) string {
	if argList == nil {
		return ""
	}
	for i := 0; i < int(argList.ChildCount()); i++ {
		child := argList.Child(i)
		if child == nil || child.Type() != "keyword_argument" {
			continue
		}
		nameNode := child.ChildByFieldName("name")
		if nameNode == nil || nameNode.Content(src) != name {
			continue
		}
		valueNode := child.ChildByFieldName("value")
		if valueNode == nil {
			return ""
		}
		return stringLiteralContent(valueNode, src)
	}
	return ""
}

func stringLiteralContent(node *sitter.Node, src []byte) string {
	if node == nil {
		return ""
	}
	if node.Type() != "string" {
		return ""
	}
	var buf strings.Builder
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		if child.Type() == "string_content" {
			buf.WriteString(child.Content(src))
		}
	}
	return buf.String()
}

func walkDecorators(node *sitter.Node, src []byte, onDecorator func(routerVar, method, decoratorPath, funcName string, pos uint32)) {
	if node == nil {
		return
	}
	if node.Type() == "decorated_definition" {
		def := node.ChildByFieldName("definition")
		if def == nil {

			for i := int(node.ChildCount()) - 1; i >= 0; i-- {
				c := node.Child(i)
				if c == nil {
					continue
				}
				if c.Type() == "function_definition" || c.Type() == "async_function_definition" {
					def = c
					break
				}
			}
		}
		funcName := ""
		if def != nil {
			nameNode := def.ChildByFieldName("name")
			if nameNode != nil {
				funcName = nameNode.Content(src)
			}
		}

		for i := 0; i < int(node.ChildCount()); i++ {
			child := node.Child(i)
			if child == nil || child.Type() != "decorator" {
				continue
			}
			routerVar, method, path := parseDecorator(child, src)
			if method == "" {
				continue
			}
			onDecorator(routerVar, method, path, funcName, child.StartByte())
		}
	}
	for i := 0; i < int(node.ChildCount()); i++ {
		walkDecorators(node.Child(i), src, onDecorator)
	}
}

func parseDecorator(decorator *sitter.Node, src []byte) (routerVar, method, path string) {

	for i := 0; i < int(decorator.ChildCount()); i++ {
		child := decorator.Child(i)
		if child == nil || child.Type() == "@" {
			continue
		}

		if child.Type() != "call" {
			return "", "", ""
		}
		fn := child.ChildByFieldName("function")
		if fn == nil || fn.Type() != "attribute" {
			return "", "", ""
		}
		obj := fn.ChildByFieldName("object")
		attr := fn.ChildByFieldName("attribute")
		if obj == nil || attr == nil {
			return "", "", ""
		}
		if obj.Type() != "identifier" {
			return "", "", ""
		}
		routerVar = obj.Content(src)
		method = attr.Content(src)
		args := child.ChildByFieldName("arguments")
		if args != nil {
			path = firstPositionalString(args, src)
		}
		return routerVar, method, path
	}
	return "", "", ""
}

func firstPositionalString(argList *sitter.Node, src []byte) string {
	for i := 0; i < int(argList.ChildCount()); i++ {
		child := argList.Child(i)
		if child == nil {
			continue
		}
		switch child.Type() {
		case "(", ")", ",", "keyword_argument":
			continue
		case "string":
			return stringLiteralContent(child, src)
		default:
			return ""
		}
	}
	return ""
}
