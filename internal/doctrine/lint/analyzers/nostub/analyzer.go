// SPDX-License-Identifier: MIT
// Package nostub implements noStubAnalyzer for zen-swarm Plan 8 (spec §1 Q4 B).
//
// Detects production-code stubs that violate CLAUDE.md project doctrine
// "No stubs, código completo":
//
//  1. nostub-panic       : panic("not implemented") and case-insensitive variants
//  2. nostub-errnotimpl  : return errors.ErrNotImplementedPlanN where N <= released-plan
//  3. nostub-todo        : // TODO implement later as first body comment
//  4. nostub-empty-method: empty body on concrete-type method with non-trivial signature
//
// Configurable via flags:
//
//	-nostub.released-plan=8   # ErrNotImplementedPlanN where N <= this is reported
package nostub

import (
	"flag"
	"go/ast"
	"go/token"
	"go/types"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

var stubPanicRegex = regexp.MustCompile(`(?i)^"(not[ _]impl(emented)?( yet)?|todo|unimplemented)"$`)

var errNotImplPlanRegex = regexp.MustCompile(`^ErrNotImplementedPlan([0-9]+)$`)

var stubTodoCommentRegex = regexp.MustCompile(`(?i)^//\s*(TODO|FIXME|XXX)[:\s]+\s*implement(\s+later)?\s*$`)

var (
	releasedPlanFlag = 8
	flagSetOnce      = newFlagSet()
)

func newFlagSet() flag.FlagSet {
	fs := flag.NewFlagSet("nostub", flag.ExitOnError)
	fs.IntVar(&releasedPlanFlag, "released-plan", 8,
		"ErrNotImplementedPlanN where N <= this value is reported as a stub")
	return *fs
}

var Analyzer = &analysis.Analyzer{
	Name: "nostub",
	Doc: "Detects production-code stubs (panic-not-implemented, ErrNotImplementedPlanN, " +
		"// TODO implement later, empty method bodies on concrete types). Enforces " +
		"CLAUDE.md \"No stubs, código completo\" doctrine. Subsumes manual stub-grep.",
	Flags:    flagSetOnce,
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

func run(pass *analysis.Pass) (any, error) {

	if strings.HasSuffix(pass.Pkg.Path(), "_test") {
		return nil, nil
	}

	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)

	insp.Preorder([]ast.Node{(*ast.CallExpr)(nil)}, func(n ast.Node) {
		ce := n.(*ast.CallExpr)
		if isTestFile(pass.Fset, ce.Pos()) {
			return
		}
		ident, ok := ce.Fun.(*ast.Ident)
		if !ok || ident.Name != "panic" {
			return
		}
		if len(ce.Args) == 0 {
			return
		}
		lit, ok := ce.Args[0].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return
		}
		if stubPanicRegex.MatchString(lit.Value) {
			pass.Reportf(ce.Pos(),
				"nostub-panic: %s is a stub marker per CLAUDE.md \"No stubs, código completo\" doctrine; replace with real implementation",
				lit.Value)
		}
	})

	insp.Preorder([]ast.Node{(*ast.ReturnStmt)(nil)}, func(n ast.Node) {
		rs := n.(*ast.ReturnStmt)
		if isTestFile(pass.Fset, rs.Pos()) {
			return
		}
		for _, expr := range rs.Results {
			sel, ok := expr.(*ast.SelectorExpr)
			if !ok {
				continue
			}
			m := errNotImplPlanRegex.FindStringSubmatch(sel.Sel.Name)
			if m == nil {
				continue
			}
			planN, err := strconv.Atoi(m[1])
			if err != nil {
				continue
			}
			if planN <= releasedPlanFlag {
				pass.Reportf(sel.Pos(),
					"nostub-errnotimpl: %s is a stub for an already-released plan (released=%d); "+
						"plan %d code MUST be implemented, not stub-returned",
					sel.Sel.Name, releasedPlanFlag, planN)
			}
		}
	})

	insp.Preorder([]ast.Node{(*ast.FuncDecl)(nil)}, func(n ast.Node) {
		fd := n.(*ast.FuncDecl)
		if fd.Body == nil || isTestFile(pass.Fset, fd.Pos()) {
			return
		}

		bodyStart := fd.Body.Lbrace
		var firstStmtPos token.Pos
		if len(fd.Body.List) > 0 {
			firstStmtPos = fd.Body.List[0].Pos()
		} else {
			firstStmtPos = fd.Body.Rbrace
		}
		fileComments := commentsForFunc(pass, fd)
		for _, cg := range fileComments {
			if cg.Pos() < bodyStart || cg.Pos() > firstStmtPos {
				continue
			}
			for _, c := range cg.List {
				if stubTodoCommentRegex.MatchString(c.Text) {
					pass.Reportf(c.Pos(),
						"nostub-todo: %q at start of %s body is a stub marker; "+
							"replace with real implementation per CLAUDE.md \"No stubs\" doctrine",
						c.Text, fd.Name.Name)
				}
			}
		}
	})

	insp.Preorder([]ast.Node{(*ast.FuncDecl)(nil)}, func(n ast.Node) {
		fd := n.(*ast.FuncDecl)
		if fd.Recv == nil || fd.Body == nil || isTestFile(pass.Fset, fd.Pos()) {
			return
		}

		if len(fd.Body.List) > 0 {
			return
		}
		// Resolve receiver type — skip if it's an interface (interface methods
		// MUST have empty bodies; that's the language convention).
		if isInterfaceMethod(pass, fd) {
			return
		}

		if isNoOpSignature(fd) {
			return
		}
		pass.Reportf(fd.Pos(),
			"nostub-empty-method: method %s on concrete type has empty body but non-trivial signature; "+
				"either implement OR add documented no-op rationale (zero params + zero returns) "+
				"per CLAUDE.md \"No stubs\" doctrine",
			fd.Name.Name)
	})

	return nil, nil
}

func isTestFile(fset *token.FileSet, pos token.Pos) bool {
	f := fset.File(pos)
	if f == nil {
		return false
	}
	return strings.HasSuffix(f.Name(), "_test.go")
}

func commentsForFunc(pass *analysis.Pass, fd *ast.FuncDecl) []*ast.CommentGroup {
	for _, f := range pass.Files {
		if f.Pos() <= fd.Pos() && fd.End() <= f.End() {
			return f.Comments
		}
	}
	return nil
}

// isInterfaceMethod returns true if fd is a method on an interface type.
// Interface methods MUST have empty bodies in Go, so they are exempt from
// the empty-method-on-concrete-type analyzer rule.
func isInterfaceMethod(pass *analysis.Pass, fd *ast.FuncDecl) bool {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return false
	}
	recvField := fd.Recv.List[0]
	recvType := recvField.Type

	if star, ok := recvType.(*ast.StarExpr); ok {
		recvType = star.X
	}

	if idx, ok := recvType.(*ast.IndexExpr); ok {
		recvType = idx.X
	}
	if idx, ok := recvType.(*ast.IndexListExpr); ok {
		recvType = idx.X
	}
	ident, ok := recvType.(*ast.Ident)
	if !ok {
		return false
	}
	if pass.TypesInfo == nil {

		return false
	}
	obj := pass.TypesInfo.Uses[ident]
	if obj == nil {
		obj = pass.TypesInfo.Defs[ident]
	}
	if obj == nil {
		return false
	}
	tn, ok := obj.(*types.TypeName)
	if !ok {
		return false
	}
	_, isIface := tn.Type().Underlying().(*types.Interface)
	return isIface
}

func isNoOpSignature(fd *ast.FuncDecl) bool {
	if fd.Type.Params != nil && len(fd.Type.Params.List) > 0 {
		return false
	}
	if fd.Type.Results != nil && len(fd.Type.Results.List) > 0 {
		return false
	}
	return true
}
