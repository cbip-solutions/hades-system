// SPDX-License-Identifier: MIT
// Package lint — no_web_in_aggregator.go
//
// Phase J Task J-7: custom go vet analyzer enforcing inv-zen-129 +
// inv-zen-152 at compile time.
//
// inv-zen-129 (Plan 7 commitment): aggregator NEVER queries web.
// inv-zen-152 (Plan 9 commitment):  research cache stores findings;
//
//	never dispatches against ecosystem corpus.
//
// Allowlist `internal/research/cache/revalidator.go` may call
// http.Head/Do for HEAD revalidation per Q8 A.
// `internal/research/cache/revalidator_fetch.go` for the Fetch +
// FetchPOST URL-fetch primitives consumed by Plan 14 ingester sources.
//
// Pattern golang.org/x/tools/go/analysis.Analyzer + analysistest.
// Loaded via cmd/zen-doctrine-lint module plugin (Plan 8 Q4 B).
package lint

import (
	"go/ast"
	"go/types"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/analysis"
)

var NoWebInAggregatorAnalyzer = &analysis.Analyzer{
	Name: "noWebInAggregator",
	Doc: `Enforces inv-zen-129 + inv-zen-152: aggregator and research cache
NEVER query web (Plan 14 territory). Allowlist: revalidator.go HEAD
revalidation per Q8 A.`,
	Run: runNoWebInAggregator,
}

func isForbiddenPkg(pkgPath string) (tag string, forbidden bool) {

	if strings.Contains(pkgPath, "internal/knowledge/aggregator") {
		return "inv-zen-129", true
	}
	if strings.Contains(pkgPath, "internal/research/cache") {
		return "inv-zen-152", true
	}

	switch {
	case strings.HasSuffix(pkgPath, "/aggregator_violation") ||
		strings.HasSuffix(pkgPath, "/aggregator_clean") ||
		pkgPath == "aggregator_violation" ||
		pkgPath == "aggregator_clean":
		return "inv-zen-129", true
	case strings.HasSuffix(pkgPath, "/cache_violation") ||
		strings.HasSuffix(pkgPath, "/cache_revalidator") ||
		pkgPath == "cache_violation" ||
		pkgPath == "cache_revalidator":
		return "inv-zen-152", true
	}
	return "", false
}

func allowlistedFile(name string) bool {
	base := filepath.Base(name)
	return base == "revalidator.go" || base == "revalidator_fetch.go"
}

func runNoWebInAggregator(pass *analysis.Pass) (any, error) {
	invariantTag, forbidden := isForbiddenPkg(pass.Pkg.Path())
	if !forbidden {
		return nil, nil
	}

	for _, file := range pass.Files {
		fileName := pass.Fset.File(file.Pos()).Name()
		if allowlistedFile(fileName) {

			continue
		}

		for _, imp := range file.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			if path == "net/http" {
				if invariantTag == "inv-zen-129" {
					pass.Reportf(imp.Pos(), "%s: net/http import in aggregator package forbidden", invariantTag)
				} else {
					pass.Reportf(imp.Pos(), "%s: net/http import in cache package outside revalidator.go forbidden", invariantTag)
				}
			}
		}

		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if !isHTTPPkgIdent(pass, sel.X) {
				return true
			}
			methodName := sel.Sel.Name
			if isForbiddenHTTPMethod(methodName) {
				if invariantTag == "inv-zen-129" {
					pass.Reportf(call.Pos(), "%s: aggregator NEVER queries web - Plan 14 territory", invariantTag)
				} else {
					pass.Reportf(call.Pos(), "%s: research cache NEVER dispatches against ecosystem corpus - Plan 14 territory", invariantTag)
				}
			}
			return true
		})
	}
	return nil, nil
}

func isHTTPPkgIdent(pass *analysis.Pass, expr ast.Expr) bool {
	ident, ok := expr.(*ast.Ident)
	if !ok {
		return false
	}
	if pass.TypesInfo != nil {
		obj := pass.TypesInfo.ObjectOf(ident)
		if obj == nil {
			return false
		}
		pkgName, ok := obj.(*types.PkgName)
		if !ok {
			return false
		}
		return pkgName.Imported().Path() == "net/http"
	}

	return ident.Name == "http"
}

func isForbiddenHTTPMethod(name string) bool {
	switch name {
	case "Get", "Post", "PostForm", "Head", "Do", "NewRequest":
		return true
	}
	return false
}
