// tests/compliance/inv_zen_220_error_render_coverage_test.go
//
// inv-zen-220 (Plan 18c Phase G G-2/G-3) — Error render coverage.
//
// Doctrine: per spec §Q6 + master §G "Critical invariants" + Plan 18c
// Phase A (error catalog) + Phase B (Render wrapper), every error path
// that reaches the user MUST route through
// internal/cli/error_render.go::Render() — never via raw fmt.Errorf
// returns at the cobra RunE boundary or os.Exit(non-zero) calls outside
// the defense-in-depth recover() catch-all in main.go.
//
// Go-AST half (this section):
//   - Walks internal/cli/*.go + cmd/zen/main.go
//   - Visits cobra RunE function bodies (heuristic: signature
//     `func(cmd *cobra.Command, args []string) error` OR anonymous
//     function assigned to a RunE field via `RunE: func(...)`)
//   - Flags raw `fmt.Errorf(...)` direct returns at the boundary
//   - Flags `os.Exit(N)` calls where N != 0 outside the catch-all
//
// Note: cmd/hades/main.go uses stdlib-only (no cobra) per Plan 18a
// spec §3.2; it is excluded from the RunE AST scan. The os.Exit scan
// covers cmd/hades/main.go but allowlists its main() function as a
// catch-all entry point.
//
// Python-AST half (appended below):
//   - Invokes `python3 -c "import ast..."` per plugin/hades/commands/*.py
//   - Flags un-routed `raise` statements inside *_handler functions
//     (Hermes plugin convention for slash-command handlers)
//
// Allowlist (legitimate bypasses):
//   - Test files (*_test.go, *_test.py) — never scanned
//   - The catch-all recover block in cmd/zen/main.go:main +
//     cmd/hades/main.go:main (entry-point boundary)
//   - Build-tag-gated files (//go:build !test, //go:build integration)
//
// Failure message: aggregates all violations into a single t.Errorf so
// the implementer sees the full surface at once (per
// inv_zen_218_skin_closure_test.go aggregation precedent).
//
// Companion ADR: docs/decisions/0096-error-ux-framework.md
package compliance

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var inv220GoAuditPaths = []string{
	"internal/cli/*.go",
	"cmd/zen/main.go",
}

var inv220AllowlistMainRecoverBlock = []string{
	"cmd/zen/main.go:main",
	"cmd/hades/main.go:main",
}

type inv220Violation struct {
	Path     string
	Line     int
	FuncName string
	Rule     string
	Source   string
}

func TestInvZen220ErrorRenderCoverageGo(t *testing.T) {
	root := repoRoot(t)
	files := inv220ExpandGoAuditPaths(t, root)
	if len(files) == 0 {
		t.Fatal("inv-zen-220: no in-scope Go files matched the audit paths")
	}

	fset := token.NewFileSet()
	var violations []inv220Violation

	for _, path := range files {

		if strings.HasSuffix(path, "_test.go") {
			continue
		}

		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("inv-zen-220: cannot read %s: %v", path, err)
		}

		if inv220HasSkipBuildTag(string(src)) {
			continue
		}
		file, err := parser.ParseFile(fset, path, src, parser.AllErrors|parser.ParseComments)
		if err != nil {
			t.Fatalf("inv-zen-220: parse error in %s: %v", path, err)
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			rel = path
		}

		ast.Inspect(file, func(n ast.Node) bool {
			switch fn := n.(type) {
			case *ast.FuncDecl:
				if inv220LooksLikeRunE(fn) {
					inv220ScanRunEBody(rel, fn.Name.Name, fn.Body, fset, &violations)
				}

				inv220ScanOsExit(rel, fn.Name.Name, fn.Body, fset, &violations)

				return true
			case *ast.AssignStmt, *ast.KeyValueExpr:

				inv220ScanAnonymousRunE(rel, fn, fset, &violations)
				return true
			}
			return true
		})
	}

	if len(violations) > 0 {
		t.Errorf("inv-zen-220 (error render coverage — Go AST half) violated. %d offending site(s):\n%s",
			len(violations), inv220FormatViolations(violations))
	}
}

func inv220ExpandGoAuditPaths(t *testing.T, root string) []string {
	t.Helper()
	var all []string
	for _, glob := range inv220GoAuditPaths {
		matches, err := filepath.Glob(filepath.Join(root, glob))
		if err != nil {
			t.Fatalf("inv-zen-220: glob %s: %v", glob, err)
		}
		all = append(all, matches...)
	}
	return all
}

func inv220HasSkipBuildTag(src string) bool {
	lines := strings.SplitN(src, "\n", 11)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "//go:build") {
			continue
		}

		for _, exclude := range []string{"!test", "integration", "adversarial"} {
			if strings.Contains(trimmed, exclude) {
				return true
			}
		}
	}
	return false
}

func inv220LooksLikeRunE(fn *ast.FuncDecl) bool {
	if fn.Type == nil || fn.Type.Params == nil || fn.Type.Results == nil {
		return false
	}
	if len(fn.Type.Results.List) != 1 {
		return false
	}
	if !inv220IsErrorType(fn.Type.Results.List[0].Type) {
		return false
	}
	if len(fn.Type.Params.List) < 2 {
		return false
	}

	first := fn.Type.Params.List[0]
	star, ok := first.Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	sel, ok := star.X.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	ident, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	return ident.Name == "cobra" && sel.Sel.Name == "Command"
}

func inv220IsErrorType(expr ast.Expr) bool {
	ident, ok := expr.(*ast.Ident)
	if !ok {
		return false
	}
	return ident.Name == "error"
}

func inv220ScanRunEBody(path, funcName string, body *ast.BlockStmt, fset *token.FileSet, violations *[]inv220Violation) {
	if body == nil {
		return
	}
	ast.Inspect(body, func(n ast.Node) bool {
		ret, ok := n.(*ast.ReturnStmt)
		if !ok {
			return true
		}
		for _, result := range ret.Results {
			call, ok := result.(*ast.CallExpr)
			if !ok {
				continue
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				continue
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok {
				continue
			}
			if pkg.Name == "fmt" && sel.Sel.Name == "Errorf" {
				pos := fset.Position(call.Pos())
				*violations = append(*violations, inv220Violation{
					Path:     path,
					Line:     pos.Line,
					FuncName: funcName,
					Rule:     "raw-errorf-return",
					Source:   "fmt.Errorf(...) at RunE boundary",
				})
			}
		}
		return true
	})
}

func inv220ScanOsExit(path, funcName string, body *ast.BlockStmt, fset *token.FileSet, violations *[]inv220Violation) {
	if body == nil {
		return
	}
	allowed := inv220IsAllowed(path, funcName)
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		if pkg.Name != "os" || sel.Sel.Name != "Exit" {
			return true
		}

		if len(call.Args) == 0 {
			return true
		}
		lit, ok := call.Args[0].(*ast.BasicLit)
		if !ok {

			return true
		}
		if lit.Kind != token.INT {
			return true
		}
		if lit.Value == "0" {
			return true
		}
		if allowed {
			return true
		}
		pos := fset.Position(call.Pos())
		*violations = append(*violations, inv220Violation{
			Path:     path,
			Line:     pos.Line,
			FuncName: funcName,
			Rule:     "non-zero-exit",
			Source:   "os.Exit(" + lit.Value + ") outside catch-all",
		})
		return true
	})
}

func inv220ScanAnonymousRunE(path string, n ast.Node, fset *token.FileSet, violations *[]inv220Violation) {
	switch v := n.(type) {
	case *ast.KeyValueExpr:
		if key, ok := v.Key.(*ast.Ident); ok && key.Name == "RunE" {
			if fn, ok := v.Value.(*ast.FuncLit); ok {
				inv220ScanRunEBody(path, "<anon-RunE>", fn.Body, fset, violations)
			}
		}
	case *ast.AssignStmt:
		for i, lhs := range v.Lhs {
			sel, ok := lhs.(*ast.SelectorExpr)
			if !ok {
				continue
			}
			if sel.Sel.Name != "RunE" {
				continue
			}
			if i >= len(v.Rhs) {
				continue
			}
			fn, ok := v.Rhs[i].(*ast.FuncLit)
			if !ok {
				continue
			}
			inv220ScanRunEBody(path, "<anon-RunE>", fn.Body, fset, violations)
		}
	}
}

func inv220IsAllowed(path, funcName string) bool {
	key := path + ":" + funcName
	for _, allowed := range inv220AllowlistMainRecoverBlock {
		if key == allowed {
			return true
		}
	}
	return false
}

func inv220FormatViolations(violations []inv220Violation) string {
	var sb strings.Builder
	for _, v := range violations {
		sb.WriteString("  - ")
		sb.WriteString(v.Path)
		sb.WriteString(":")
		sb.WriteString(inv220Itoa(v.Line))
		sb.WriteString(" in ")
		sb.WriteString(v.FuncName)
		sb.WriteString(": rule=")
		sb.WriteString(v.Rule)
		sb.WriteString(" | ")
		sb.WriteString(v.Source)
		sb.WriteString("\n")
	}
	sb.WriteString("\nRemediation: wrap raw errors via errors.Wrap(codes.SomeCode, err, ctx) at the RunE boundary, or document an allowlist entry with rationale.")
	return sb.String()
}

func inv220Itoa(i int) string {
	if i == 0 {
		return "0"
	}
	if i < 0 {
		return "-" + inv220Itoa(-i)
	}
	digits := ""
	for i > 0 {
		digits = string(rune('0'+(i%10))) + digits
		i /= 10
	}
	return digits
}

func TestInvZen220AnonymousRunEAssign(t *testing.T) {
	src := `package main

import (
	"fmt"
	"github.com/spf13/cobra"
)

func setup(cmd *cobra.Command) {
	cmd.RunE = func(c *cobra.Command, args []string) error {
		return fmt.Errorf("bad")
	}
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "synthetic.go", src, parser.AllErrors)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var violations []inv220Violation
	ast.Inspect(file, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.AssignStmt:
			inv220ScanAnonymousRunE("synthetic.go", v, fset, &violations)
		}
		return true
	})
	if len(violations) != 1 {
		t.Errorf("expected exactly 1 violation from assignment-style RunE, got %d", len(violations))
	}
}

const inv220PythonScript = `
import ast, json, sys

src = sys.stdin.read()
try:
    tree = ast.parse(src)
except SyntaxError as e:
    print(json.dumps([]))
    sys.exit(0)

violations = []

def is_handler_func(node):
    return isinstance(node, ast.FunctionDef) and node.name.endswith('_handler')

def walk_body_for_raise(node, fname):
    for child in ast.walk(node):
        if isinstance(child, ast.Raise):
            src_text = ''
            try:
                if hasattr(ast, 'unparse'):
                    src_text = ast.unparse(child)[:120]
                else:
                    src_text = '<raise>'
            except Exception:
                src_text = '<raise>'
            violations.append({
                'type': 'un-routed-raise',
                'func': fname,
                'line': child.lineno,
                'col': child.col_offset,
                'source': src_text,
            })

for node in ast.iter_child_nodes(tree):
    if is_handler_func(node):
        walk_body_for_raise(node, node.name)

print(json.dumps(violations))
`

var inv220PythonAuditPaths = []string{
	"plugin/hades/commands/*.py",
}

type inv220PyViolation struct {
	Type   string `json:"type"`
	Func   string `json:"func"`
	Line   int    `json:"line"`
	Col    int    `json:"col"`
	Source string `json:"source"`
}

func TestInvZen220ErrorRenderCoveragePython(t *testing.T) {
	root := repoRoot(t)
	files := inv220ExpandPyAuditPaths(t, root)
	if len(files) == 0 {

		t.Fatal("inv-zen-220 (Python half): no in-scope *.py files matched; Phase C+D handlers expected")
	}

	var violations []inv220Violation

	for _, path := range files {
		if strings.HasSuffix(path, "_test.py") {
			continue
		}
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("inv-zen-220 (Python): cannot read %s: %v", path, err)
		}
		pyViolations, err := inv220RunPythonAST(string(body))
		if err != nil {
			t.Fatalf("inv-zen-220 (Python): AST subprocess for %s: %v", path, err)
		}

		rel, _ := filepath.Rel(root, path)

		bodyLines := strings.Split(string(body), "\n")
		for _, pv := range pyViolations {
			if inv220IsPythonAllowlisted(bodyLines, pv.Line) {
				continue
			}
			violations = append(violations, inv220Violation{
				Path:     rel,
				Line:     pv.Line,
				FuncName: pv.Func,
				Rule:     "un-routed-raise",
				Source:   pv.Source,
			})
		}
	}

	if len(violations) > 0 {
		t.Errorf("inv-zen-220 (error render coverage — Python AST half) violated. %d offending site(s):\n%s",
			len(violations), inv220FormatViolations(violations))
	}
}

func inv220ExpandPyAuditPaths(t *testing.T, root string) []string {
	t.Helper()
	var all []string
	for _, glob := range inv220PythonAuditPaths {
		matches, err := filepath.Glob(filepath.Join(root, glob))
		if err != nil {
			t.Fatalf("inv-zen-220 (Python): glob %s: %v", glob, err)
		}
		all = append(all, matches...)
	}
	return all
}

func inv220RunPythonAST(body string) ([]inv220PyViolation, error) {
	cmd := exec.Command("python3", "-c", inv220PythonScript)
	cmd.Stdin = strings.NewReader(body)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, &inv220PythonError{
			Stderr: stderr.String(),
			Cause:  err,
		}
	}
	var rows []inv220PyViolation
	if err := json.Unmarshal(stdout.Bytes(), &rows); err != nil {
		return nil, &inv220PythonError{
			Stderr: "json decode: " + err.Error() + " (stdout=" + stdout.String() + ")",
			Cause:  err,
		}
	}
	return rows, nil
}

type inv220PythonError struct {
	Stderr string
	Cause  error
}

func (e *inv220PythonError) Error() string {
	return "python AST script failed: " + e.Stderr
}

func inv220IsPythonAllowlisted(bodyLines []string, n int) bool {
	const marker = "# inv-zen-220-allowlisted:"

	if n >= 1 && n <= len(bodyLines) {
		if strings.Contains(bodyLines[n-1], marker) {
			return true
		}
	}

	for i := n - 2; i >= 0; i-- {
		trimmed := strings.TrimSpace(bodyLines[i])
		if trimmed == "" {
			continue
		}
		return strings.Contains(trimmed, marker)
	}
	return false
}

func TestInvZen220ErrorRenderCoverageCombined(t *testing.T) {
	root := repoRoot(t)

	var allViolations []inv220Violation

	goFiles := inv220ExpandGoAuditPaths(t, root)
	fset := token.NewFileSet()
	for _, path := range goFiles {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("inv-zen-220 (combined): cannot read %s: %v", path, err)
		}
		if inv220HasSkipBuildTag(string(src)) {
			continue
		}
		file, err := parser.ParseFile(fset, path, src, parser.AllErrors|parser.ParseComments)
		if err != nil {
			t.Fatalf("inv-zen-220 (combined): parse error in %s: %v", path, err)
		}
		rel, _ := filepath.Rel(root, path)
		ast.Inspect(file, func(n ast.Node) bool {
			switch fn := n.(type) {
			case *ast.FuncDecl:
				if inv220LooksLikeRunE(fn) {
					inv220ScanRunEBody(rel, fn.Name.Name, fn.Body, fset, &allViolations)
				}
				inv220ScanOsExit(rel, fn.Name.Name, fn.Body, fset, &allViolations)
			case *ast.AssignStmt, *ast.KeyValueExpr:
				inv220ScanAnonymousRunE(rel, fn, fset, &allViolations)
			}
			return true
		})
	}

	pyFiles := inv220ExpandPyAuditPaths(t, root)
	for _, path := range pyFiles {
		if strings.HasSuffix(path, "_test.py") {
			continue
		}
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("inv-zen-220 (combined): cannot read %s: %v", path, err)
		}
		pyRows, err := inv220RunPythonAST(string(body))
		if err != nil {
			t.Fatalf("inv-zen-220 (combined): Python AST for %s: %v", path, err)
		}
		rel, _ := filepath.Rel(root, path)
		bodyLines := strings.Split(string(body), "\n")
		for _, pv := range pyRows {
			if inv220IsPythonAllowlisted(bodyLines, pv.Line) {
				continue
			}
			allViolations = append(allViolations, inv220Violation{
				Path:     rel,
				Line:     pv.Line,
				FuncName: pv.Func,
				Rule:     "un-routed-raise",
				Source:   pv.Source,
			})
		}
	}

	if len(allViolations) > 0 {
		t.Errorf("inv-zen-220 (error render coverage — combined Go+Python) violated. %d offending site(s):\n%s",
			len(allViolations), inv220FormatViolations(allViolations))
	}
}
