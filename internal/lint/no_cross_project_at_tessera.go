// SPDX-License-Identifier: MIT
// Package lint — no_cross_project_at_tessera.go
//
// Task J-9: custom go vet analyzer enforcing invariant
// (per-project Tessera tile-log isolation; *Adapter exported methods
// MUST key tile reads by a.projectID).
//
// Conservative heuristic: scan internal/audit/tessera/ exported methods
// on *Adapter; for each method, check if any string-typed parameter name
// signals an external projectID (e.g., "otherProjectID", "targetProjectID",
// any name containing "projectid" or "project_id" that is NOT the receiver-
// field accessor). If found, REJECT.
//
// Compile-check complementary to runtime defense: returns
// tessera.ErrCrossProject when tile keys do not match a.projectID.
//
// Pattern golang.org/x/tools/go/analysis.Analyzer + analysistest.
// Loaded via cmd/hades-doctrine-lint module plugin.
package lint

import (
	"go/ast"
	"strings"

	"golang.org/x/tools/go/analysis"
)

var NoCrossProjectAtTesseraAnalyzer = &analysis.Analyzer{
	Name: "noCrossProjectAtTessera",
	Doc: `Enforces inv-hades-144: *Adapter exported methods in internal/audit/tessera
MUST key tile reads by constructor-bound a.projectID. Methods accepting an
external projectID parameter (otherProjectID, targetProjectID, etc.) are
forbidden — they enable cross-project blast radius.`,
	Run: runNoCrossProjectAtTessera,
}

func runNoCrossProjectAtTessera(pass *analysis.Pass) (any, error) {
	pkgPath := pass.Pkg.Path()
	if !isTesseraScope(pkgPath) {
		return nil, nil
	}

	for _, file := range pass.Files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if fn.Recv == nil {
				continue
			}
			if !isExportedAdapterMethod(fn) {
				continue
			}
			if fn.Type.Params == nil {
				continue
			}
			for _, param := range fn.Type.Params.List {
				if isExternalProjectIDParam(param) {
					pass.Reportf(fn.Pos(),
						"inv-hades-144: *Adapter exported method reads tiles NOT keyed by a.projectID; param %s shadows receiver project_id",
						joinParamNames(param))
					break
				}
			}
		}
	}
	return nil, nil
}

func isTesseraScope(pkgPath string) bool {
	if strings.Contains(pkgPath, "internal/audit/tessera") {
		return true
	}

	return strings.HasSuffix(pkgPath, "/projectid_keyed") ||
		strings.HasSuffix(pkgPath, "/cross_project") ||
		pkgPath == "projectid_keyed" ||
		pkgPath == "cross_project"
}

func isExportedAdapterMethod(fn *ast.FuncDecl) bool {
	if !ast.IsExported(fn.Name.Name) {
		return false
	}
	for _, field := range fn.Recv.List {
		switch t := field.Type.(type) {
		case *ast.StarExpr:
			if id, ok := t.X.(*ast.Ident); ok && id.Name == "Adapter" {
				return true
			}
		case *ast.Ident:
			if t.Name == "Adapter" {
				return true
			}
		}
	}
	return false
}

func isExternalProjectIDParam(field *ast.Field) bool {

	typeIdent, ok := field.Type.(*ast.Ident)
	if !ok || typeIdent.Name != "string" {
		return false
	}
	for _, name := range field.Names {
		lower := strings.ToLower(name.Name)
		if strings.Contains(lower, "projectid") ||
			strings.Contains(lower, "project_id") ||
			strings.Contains(lower, "otherproject") ||
			strings.Contains(lower, "targetproject") {
			return true
		}
	}
	return false
}

func joinParamNames(field *ast.Field) string {
	names := make([]string, 0, len(field.Names))
	for _, n := range field.Names {
		names = append(names, n.Name)
	}
	return strings.Join(names, ", ")
}
