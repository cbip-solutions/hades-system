package semantic

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"testing"

	"golang.org/x/tools/go/packages"
)

func typeCheckSrc(t *testing.T, importPath, src string) (*types.Package, *types.Info) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "src.go", src, 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	info := &types.Info{
		Defs:  map[*ast.Ident]types.Object{},
		Uses:  map[*ast.Ident]types.Object{},
		Types: map[ast.Expr]types.TypeAndValue{},
	}
	conf := types.Config{Importer: importer.Default()}
	pkg, err := conf.Check(importPath, fset, []*ast.File{f}, info)
	if err != nil {
		t.Fatalf("type-check: %v", err)
	}
	return pkg, info
}

func lookupObj(t *testing.T, pkg *types.Package, name string) types.Object {
	t.Helper()
	scope := pkg.Scope()
	if obj := scope.Lookup(name); obj != nil {
		return obj
	}
	t.Fatalf("object %q not found in package scope", name)
	return nil
}

const testModulePrefix = "example.com/m"

func TestCanonicalNodeIDFunction(t *testing.T) {
	pkg, _ := typeCheckSrc(t, "example.com/m/pkg/x", `package x
func Alpha() {}
`)
	obj := lookupObj(t, pkg, "Alpha")
	got := canonicalNodeID(obj, testModulePrefix)
	want := "pkg/x.Alpha"
	if got != want {
		t.Errorf("canonicalNodeID(func) = %q; want %q (Phase B dir-relative form)", got, want)
	}
}

func TestCanonicalNodeIDMethod(t *testing.T) {
	pkg, _ := typeCheckSrc(t, "example.com/m/pkg/x", `package x
type T struct{}
func (t T) Beta() {}
func (t *T) Gamma() {}
`)
	tObj := lookupObj(t, pkg, "T").Type()

	msVal := types.NewMethodSet(tObj)
	var betaID string
	for i := 0; i < msVal.Len(); i++ {
		if msVal.At(i).Obj().Name() == "Beta" {
			betaID = canonicalNodeID(msVal.At(i).Obj(), testModulePrefix)
		}
	}
	if betaID != "pkg/x.T.Beta" {
		t.Errorf("canonicalNodeID(value method) = %q; want pkg/x.T.Beta (dir-relative)", betaID)
	}

	msPtr := types.NewMethodSet(types.NewPointer(tObj))
	var gammaID string
	for i := 0; i < msPtr.Len(); i++ {
		if msPtr.At(i).Obj().Name() == "Gamma" {
			gammaID = canonicalNodeID(msPtr.At(i).Obj(), testModulePrefix)
		}
	}
	if gammaID != "pkg/x.T.Gamma" {
		t.Errorf("canonicalNodeID(pointer method) = %q; want pkg/x.T.Gamma (dir-relative)", gammaID)
	}
}

func TestCanonicalNodeIDNamedType(t *testing.T) {
	pkg, _ := typeCheckSrc(t, "example.com/m/pkg/x", `package x
type Reader interface{ Read() error }
`)
	obj := lookupObj(t, pkg, "Reader")
	if got := canonicalNodeID(obj, testModulePrefix); got != "pkg/x.Reader" {
		t.Errorf("canonicalNodeID(named type) = %q; want pkg/x.Reader (dir-relative)", got)
	}
}

func TestCanonicalNodeIDMainModuleRoot(t *testing.T) {
	pkg, _ := typeCheckSrc(t, "example.com/m", `package m
func Root() {}
type Box struct{}
func (b Box) Open() {}
`)
	if got := canonicalNodeID(lookupObj(t, pkg, "Root"), testModulePrefix); got != "Root" {
		t.Errorf("canonicalNodeID(root func) = %q; want bare \"Root\" (no leading dot)", got)
	}
	msPtr := types.NewMethodSet(types.NewPointer(lookupObj(t, pkg, "Box").Type()))
	var openID string
	for i := 0; i < msPtr.Len(); i++ {
		if msPtr.At(i).Obj().Name() == "Open" {
			openID = canonicalNodeID(msPtr.At(i).Obj(), testModulePrefix)
		}
	}
	if openID != "Box.Open" {
		t.Errorf("canonicalNodeID(root method) = %q; want \"Box.Open\" (dir prefix empty, receiver kept)", openID)
	}
}

func TestCanonicalNodeIDCmdPackage(t *testing.T) {
	pkg, _ := typeCheckSrc(t, "example.com/m/cmd/zen", `package main
func main() {}
`)
	if got := canonicalNodeID(lookupObj(t, pkg, "main"), testModulePrefix); got != "cmd/zen.main" {
		t.Errorf("canonicalNodeID(cmd main) = %q; want cmd/zen.main", got)
	}
}

func TestCanonicalNodeIDNotMisStrippedOnPrefixCollision(t *testing.T) {
	pkg, _ := typeCheckSrc(t, "example.com/mtools/x", `package x
func Helper() {}
`)
	got := canonicalNodeID(lookupObj(t, pkg, "Helper"), testModulePrefix)
	if got != "example.com/mtools/x.Helper" {
		t.Errorf("canonicalNodeID(prefix-collision) = %q; want the un-stripped full path (boundary not honoured)", got)
	}
}

func TestCanonicalNodeIDNilObject(t *testing.T) {
	if got := canonicalNodeID(nil, testModulePrefix); got != "" {
		t.Errorf("canonicalNodeID(nil) = %q; want empty", got)
	}
}

func TestCanonicalNodeIDUniverseScope(t *testing.T) {

	errObj := types.Universe.Lookup("error")
	if errObj == nil {
		t.Fatal("types.Universe.Lookup(\"error\") returned nil — stdlib changed?")
	}
	if got := canonicalNodeID(errObj, testModulePrefix); got != "" {
		t.Errorf("canonicalNodeID(universe error) = %q; want empty (no project node_id)", got)
	}

	lenObj := types.Universe.Lookup("len")
	if lenObj != nil {
		if got := canonicalNodeID(lenObj, testModulePrefix); got != "" {
			t.Errorf("canonicalNodeID(builtin len) = %q; want empty", got)
		}
	}
}

func TestCanonicalNodeIDVarConst(t *testing.T) {
	pkg, _ := typeCheckSrc(t, "example.com/m/pkg/x", `package x
var Count int
`)
	obj := lookupObj(t, pkg, "Count")

	got := canonicalNodeID(obj, testModulePrefix)
	if got != "pkg/x.Count" {
		t.Errorf("canonicalNodeID(var) = %q; want pkg/x.Count", got)
	}
}

func TestDirRelativePrefixEmptyModule(t *testing.T) {
	got := dirRelativePrefix("some/import/path", "")
	if got != "some/import/path" {
		t.Errorf("dirRelativePrefix(empty module) = %q; want verbatim input", got)
	}
}

func TestReceiverNamedTypeNameAnonymousInterface(t *testing.T) {

	iface := types.NewInterfaceType(nil, nil)
	if got := receiverNamedTypeName(iface); got != "" {
		t.Errorf("receiverNamedTypeName(anonymous iface) = %q; want empty", got)
	}
}

func TestModulePathOf(t *testing.T) {

	if got := modulePathOf(nil); got != "" {
		t.Errorf("modulePathOf(nil) = %q; want empty", got)
	}

	if got := modulePathOf([]*packages.Package{nil, nil}); got != "" {
		t.Errorf("modulePathOf([nil, nil]) = %q; want empty", got)
	}

	if got := modulePathOf([]*packages.Package{{}}); got != "" {
		t.Errorf("modulePathOf([no module]) = %q; want empty", got)
	}

	pkg := &packages.Package{Module: &packages.Module{Path: "github.com/example/m"}}
	if got := modulePathOf([]*packages.Package{pkg}); got != "github.com/example/m" {
		t.Errorf("modulePathOf([with module]) = %q; want github.com/example/m", got)
	}

	pkgNil := &packages.Package{}
	if got := modulePathOf([]*packages.Package{pkgNil, pkg}); got != "github.com/example/m" {
		t.Errorf("modulePathOf([nil module, with module]) = %q; want github.com/example/m", got)
	}
}
