package nostub

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestIsTestFileNilFile(t *testing.T) {
	fset := token.NewFileSet()
	if isTestFile(fset, token.NoPos) {
		t.Error("isTestFile(token.NoPos) = true; want false (defensive nil-file branch)")
	}
}

func TestIsTestFilePositive(t *testing.T) {
	fset := token.NewFileSet()
	tf := fset.AddFile("/some/path/foo_test.go", -1, 100)
	pos := tf.Pos(0)
	if !isTestFile(fset, pos) {
		t.Error("isTestFile for foo_test.go = false; want true")
	}
}

func TestIsTestFileNonTest(t *testing.T) {
	fset := token.NewFileSet()
	tf := fset.AddFile("/some/path/foo.go", -1, 100)
	pos := tf.Pos(0)
	if isTestFile(fset, pos) {
		t.Error("isTestFile for foo.go = true; want false")
	}
}

func TestIsNoOpSignaturePosNeg(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want bool
	}{
		{
			name: "zero_params_zero_returns",
			src:  `package p; func (s S) F() {}`,
			want: true,
		},
		{
			name: "with_params",
			src:  `package p; func (s S) F(x int) {}`,
			want: false,
		},
		{
			name: "with_returns",
			src:  `package p; func (s S) F() error { return nil }`,
			want: false,
		},
		{
			name: "params_and_returns",
			src:  `package p; func (s S) F(x int) error { return nil }`,
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, "x.go", tc.src, 0)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			fd := f.Decls[0].(*ast.FuncDecl)
			got := isNoOpSignature(fd)
			if got != tc.want {
				t.Errorf("isNoOpSignature = %v; want %v (src=%q)", got, tc.want, tc.src)
			}
		})
	}
}

func TestCommentsForFuncNilFallback(t *testing.T) {

	fd := &ast.FuncDecl{
		Name: ast.NewIdent("F"),
		Type: &ast.FuncType{},
		Body: &ast.BlockStmt{},
	}

	pass := &analysis.Pass{
		Fset:  token.NewFileSet(),
		Files: nil,
	}
	got := commentsForFunc(pass, fd)
	if got != nil {
		t.Errorf("commentsForFunc with empty Files = %v; want nil (defensive fallback)", got)
	}
}

func TestCommentsForFuncMatchesFile(t *testing.T) {
	fset := token.NewFileSet()
	src := `package p
// pkg-level comment
func F() {
	// body comment
}
`
	f, err := parser.ParseFile(fset, "x.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	fd := f.Decls[0].(*ast.FuncDecl)
	pass := &analysis.Pass{
		Fset:  fset,
		Files: []*ast.File{f},
	}
	got := commentsForFunc(pass, fd)
	if got == nil {
		t.Fatal("commentsForFunc returned nil for in-pass file")
	}
	if len(got) == 0 {
		t.Error("commentsForFunc returned empty slice; want non-empty")
	}
}

func TestIsInterfaceMethodNonMethod(t *testing.T) {
	src := `package p; func F() {}`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", src, 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	fd := f.Decls[0].(*ast.FuncDecl)
	pass := &analysis.Pass{Fset: fset, Files: []*ast.File{f}}
	if isInterfaceMethod(pass, fd) {
		t.Error("isInterfaceMethod for free func = true; want false")
	}
}

func TestIsInterfaceMethodEmptyRecvList(t *testing.T) {
	fd := &ast.FuncDecl{
		Recv: &ast.FieldList{List: []*ast.Field{}},
		Name: ast.NewIdent("F"),
		Type: &ast.FuncType{},
	}
	fset := token.NewFileSet()
	pass := &analysis.Pass{Fset: fset}
	if isInterfaceMethod(pass, fd) {
		t.Error("isInterfaceMethod with empty Recv.List = true; want false (defensive)")
	}
}

func TestIsInterfaceMethodNonIdentReceiver(t *testing.T) {
	fd := &ast.FuncDecl{
		Recv: &ast.FieldList{
			List: []*ast.Field{{Type: &ast.SelectorExpr{X: ast.NewIdent("pkg"), Sel: ast.NewIdent("T")}}},
		},
		Name: ast.NewIdent("F"),
		Type: &ast.FuncType{},
	}
	fset := token.NewFileSet()
	pass := &analysis.Pass{Fset: fset}
	if isInterfaceMethod(pass, fd) {
		t.Error("isInterfaceMethod with SelectorExpr receiver = true; want false")
	}
}

func TestIsInterfaceMethodPointerNonIdent(t *testing.T) {
	fd := &ast.FuncDecl{
		Recv: &ast.FieldList{
			List: []*ast.Field{{Type: &ast.StarExpr{
				X: &ast.SelectorExpr{X: ast.NewIdent("pkg"), Sel: ast.NewIdent("T")},
			}}},
		},
		Name: ast.NewIdent("F"),
		Type: &ast.FuncType{},
	}
	fset := token.NewFileSet()
	pass := &analysis.Pass{Fset: fset}
	if isInterfaceMethod(pass, fd) {
		t.Error("isInterfaceMethod with *pkg.T receiver = true; want false")
	}
}

func TestIsInterfaceMethodIndexExprReceiver(t *testing.T) {
	fd := &ast.FuncDecl{
		Recv: &ast.FieldList{
			List: []*ast.Field{{Type: &ast.IndexExpr{
				X:     ast.NewIdent("S"),
				Index: ast.NewIdent("T"),
			}}},
		},
		Name: ast.NewIdent("F"),
		Type: &ast.FuncType{},
	}
	fset := token.NewFileSet()
	pass := &analysis.Pass{Fset: fset}

	if isInterfaceMethod(pass, fd) {
		t.Error("isInterfaceMethod with generic IndexExpr (no TypesInfo) = true; want false")
	}
}

func TestIsInterfaceMethodIndexListExprReceiver(t *testing.T) {
	fd := &ast.FuncDecl{
		Recv: &ast.FieldList{
			List: []*ast.Field{{Type: &ast.IndexListExpr{
				X:       ast.NewIdent("S"),
				Indices: []ast.Expr{ast.NewIdent("T"), ast.NewIdent("U")},
			}}},
		},
		Name: ast.NewIdent("F"),
		Type: &ast.FuncType{},
	}
	fset := token.NewFileSet()
	pass := &analysis.Pass{Fset: fset}
	if isInterfaceMethod(pass, fd) {
		t.Error("isInterfaceMethod with generic IndexListExpr (no TypesInfo) = true; want false")
	}
}

func TestStubPanicRegexCases(t *testing.T) {
	matches := []string{
		`"not implemented"`,
		`"not implemented yet"`,
		`"NOT IMPLEMENTED"`,
		`"not_implemented"`,
		`"not impl"`,
		`"not impl yet"`,
		`"TODO"`,
		`"unimplemented"`,
		`"Unimplemented"`,
	}
	for _, m := range matches {
		if !stubPanicRegex.MatchString(m) {
			t.Errorf("stubPanicRegex did NOT match %q (should)", m)
		}
	}
	nonMatches := []string{
		`"invalid x: must be >= 0"`,
		`"unreachable"`,
		`"some runtime error"`,
		`""`,
	}
	for _, m := range nonMatches {
		if stubPanicRegex.MatchString(m) {
			t.Errorf("stubPanicRegex matched %q (should NOT)", m)
		}
	}
}

func TestErrNotImplPlanRegexCases(t *testing.T) {
	cases := map[string]string{
		"ErrNotImplementedPlan8":   "8",
		"ErrNotImplementedPlan99":  "99",
		"ErrNotImplementedPlan0":   "0",
		"ErrNotImplementedPlan100": "100",
	}
	for name, wantN := range cases {
		m := errNotImplPlanRegex.FindStringSubmatch(name)
		if m == nil {
			t.Errorf("errNotImplPlanRegex did NOT match %q", name)
			continue
		}
		if m[1] != wantN {
			t.Errorf("errNotImplPlanRegex %q: got N=%q; want %q", name, m[1], wantN)
		}
	}
	nonMatches := []string{
		"ErrNotImplemented",
		"ErrNotImplementedPlanX",
		"ErrSomethingElsePlan8",
		"ErrNotImplementedPlan8Beta",
	}
	for _, name := range nonMatches {
		if m := errNotImplPlanRegex.FindStringSubmatch(name); m != nil {
			t.Errorf("errNotImplPlanRegex matched %q (should NOT) → %v", name, m)
		}
	}
}

func TestStubTodoCommentRegexCases(t *testing.T) {
	matches := []string{
		`// TODO implement later`,
		`// TODO: implement later`,
		`// TODO implement`,
		`// FIXME: implement later`,
		`// XXX implement later`,
		`// todo implement later`,
		`// TODO  implement  later`,
	}
	for _, c := range matches {
		if !stubTodoCommentRegex.MatchString(c) {
			t.Errorf("stubTodoCommentRegex did NOT match %q (should)", c)
		}
	}
	nonMatches := []string{
		`// TODO add metric here later`,
		`// TODO some unrelated note`,
		`// just a comment`,
		`// implement this`,
		// IMPORTANT #3:
		// the strict regex MUST NOT match comments with trailing
		// content such as analysistest // want annotations on the
		// same line, or arbitrary trailing junk. Pre-fix, the
		// relaxed (\s*//.*)? group allowed these to match,
		// undermining the strict end-of-line anchor.
		"// TODO implement later // want `nostub-todo`",
		"// TODO: implement later // arbitrary trailing text",
		"// FIXME: implement later  trailing junk",
	}
	for _, c := range nonMatches {
		if stubTodoCommentRegex.MatchString(c) {
			t.Errorf("stubTodoCommentRegex matched %q (should NOT)", c)
		}
	}
}
