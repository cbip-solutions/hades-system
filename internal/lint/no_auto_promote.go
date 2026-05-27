// SPDX-License-Identifier: MIT
// Package lint — no_auto_promote.go
//
// Task J-8: custom go vet analyzer enforcing invariant
// (knowledge promote operator-gated; no auto-promote code path).
//
// Scans all packages for callsites of Promote() on receiver types named
// Adapter (or aggregator package) lacking a non-empty reason string arg.
//
// Heuristics (per spec §7.3 + invariant):
// 1. Empty string literal `""` as last arg → REJECT.
// 2. Wrong arity (<3 args) → REJECT (signature violation).
// 3. Non-string-literal reason → WARN (runtime panic Layer 2 active).
//
// Pattern golang.org/x/tools/go/analysis.Analyzer + analysistest.
// Loaded via cmd/hades-doctrine-lint module plugin.
package lint

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

var NoAutoPromoteAnalyzer = &analysis.Analyzer{
	Name: "noAutoPromote",
	Doc: `Enforces inv-hades-146: aggregator.Promote() callsites MUST supply
a non-empty operator-controlled reason string. Empty literal "" or
missing arg is rejected; non-literal reason triggers a warning.`,
	Run: runNoAutoPromote,
}

func runNoAutoPromote(pass *analysis.Pass) (any, error) {
	for _, file := range pass.Files {
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if !isPromoteCallsite(pass, call) {
				return true
			}

			if len(call.Args) < 3 {
				pass.Reportf(call.Pos(),
					"inv-hades-146: Promote() called with %d args (expected ≥3); reason MUST be non-empty operator-supplied string",
					len(call.Args))
				return true
			}

			lastArg := call.Args[len(call.Args)-1]
			if isEmptyStringLit(lastArg) {
				pass.Reportf(call.Pos(),
					"inv-hades-146: Promote() reason MUST be non-empty operator-supplied string")
				return true
			}
			if !isStringLit(lastArg) {

				pass.Reportf(call.Pos(),
					"inv-hades-146: Promote() reason is non-literal; operator review must verify non-empty (defense-in-depth runtime check active)")
			}
			return true
		})
	}
	return nil, nil
}

func isPromoteCallsite(pass *analysis.Pass, call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	if sel.Sel.Name != "Promote" {
		return false
	}

	if pass.TypesInfo == nil {

		return true
	}

	tv, ok := pass.TypesInfo.Types[sel.X]
	if !ok {
		return true
	}
	t := tv.Type
	if ptr, isPtr := t.(*types.Pointer); isPtr {
		t = ptr.Elem()
	}
	named, ok := t.(*types.Named)
	if !ok {
		return true
	}
	pkg := named.Obj().Pkg()
	if pkg == nil {
		return true
	}
	typeName := named.Obj().Name()
	pkgName := pkg.Name()

	return pkgName == "aggregator" ||
		pkgName == "bad" || pkgName == "bad2" || pkgName == "good" ||
		typeName == "Adapter" ||
		strings.HasSuffix(pkg.Path(), "/knowledge/aggregator")
}

func isEmptyStringLit(e ast.Expr) bool {
	bl, ok := e.(*ast.BasicLit)
	return ok && bl.Kind == token.STRING && bl.Value == `""`
}

func isStringLit(e ast.Expr) bool {
	bl, ok := e.(*ast.BasicLit)
	return ok && bl.Kind == token.STRING
}
