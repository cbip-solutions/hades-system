package echo

import (
	"context"
	"go/ast"
	goparser "go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/caronte/contract/extract"
	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

func TestPackageScaffolded(t *testing.T) { _ = t }

func readFixturePkg(t *testing.T, name string) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("fixture pkg %s: %v", name, err)
	}
	return p
}

func fmtEps(eps []store.APIEndpoint) string {
	out := ""
	for _, ep := range eps {
		out += ep.Method + " " + ep.PathTemplate + ", "
	}
	return out
}

func TestExtractorRegistersOnInit(t *testing.T) {
	got := extract.Default().Resolve("s.go", []byte("package main\nimport _ \"github.com/labstack/echo/v4\"\n"))
	found := false
	for _, e := range got {
		for _, f := range e.Frameworks() {
			if f == "echo" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("Default().Resolve did not yield an echo extractor; init() did not register")
	}
}

func TestDetectEcho(t *testing.T) {
	e := New()
	if !e.Detect("s.go", []byte("package main\nimport \"github.com/labstack/echo/v4\"\n")) {
		t.Error("Detect(echo v4) = false; want true")
	}
	if !e.Detect("s.go", []byte("package main\nimport \"github.com/labstack/echo\"\n")) {
		t.Error("Detect(echo v1) = false; want true")
	}
	if e.Detect("s.go", []byte("package main\nimport \"github.com/gin-gonic/gin\"\n")) {
		t.Error("Detect(gin) = true; want false")
	}
	if e.Detect("README.md", []byte("# docs\n")) {
		t.Error("Detect(.md) = true; want false (extension gate)")
	}
}

func TestEndpointsSimpleRoute(t *testing.T) {
	pkgDir := readFixturePkg(t, "simple")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1", len(eps))
	}
	if eps[0].Method != "GET" || eps[0].PathTemplate != "/health" {
		t.Errorf("eps[0] = %+v; want GET /health", eps[0])
	}
}

func TestExtractGroupChain(t *testing.T) {
	pkgDir := readFixturePkg(t, "group_simple")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 1 || eps[0].PathTemplate != "/v1/users" {
		t.Errorf("got %+v; want one /v1/users", eps)
	}
}

func TestExtractGroupVar(t *testing.T) {
	pkgDir := readFixturePkg(t, "group_var")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 1 || eps[0].PathTemplate != "/v1/users" {
		t.Errorf("got %+v; want one /v1/users", eps)
	}
}

func TestExtractNestedGroup(t *testing.T) {
	pkgDir := readFixturePkg(t, "nested_group")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 1 || eps[0].PathTemplate != "/v1/admin/stats" {
		t.Errorf("got %+v; want /v1/admin/stats", eps)
	}
}

func TestExtractMiddlewareChain(t *testing.T) {
	pkgDir := readFixturePkg(t, "middleware_chain")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 1 || eps[0].PathTemplate != "/v1/me" {
		t.Errorf("got %+v; want /v1/me", eps)
	}
}

func TestExtractDynamicRouteNormalizes(t *testing.T) {
	pkgDir := readFixturePkg(t, "dynamic_route")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if eps[0].PathTemplate != "/users/{param}" {
		t.Errorf("got %+v; want /users/{param}", eps)
	}
}

func TestExtractGroupReceiverAliased(t *testing.T) {
	pkgDir := readFixturePkg(t, "group_receiver_aliased")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 1 {
		t.Errorf("eps len = %d; want 1", len(eps))
	}
}

func TestExtractReverseNotARoute(t *testing.T) {
	pkgDir := readFixturePkg(t, "reverse_router")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1; got %s", len(eps), fmtEps(eps))
	}
}

func TestExtractEmptyEcho(t *testing.T) {
	pkgDir := readFixturePkg(t, "empty_echo")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 0 {
		t.Errorf("eps len = %d; want 0", len(eps))
	}
}

func TestExtractHandlerSelector(t *testing.T) {
	pkgDir := readFixturePkg(t, "handler_selector")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1", len(eps))
	}
	if !strings.Contains(eps[0].HandlerNodeID, "s.Health") {
		t.Errorf("HandlerNodeID = %q; want it to mention s.Health", eps[0].HandlerNodeID)
	}
}

func TestExtractHandlerAsClosure(t *testing.T) {
	pkgDir := readFixturePkg(t, "handler_as_closure")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1", len(eps))
	}
	if !strings.Contains(eps[0].HandlerNodeID, ".anon") {
		t.Errorf("HandlerNodeID = %q; want .anon", eps[0].HandlerNodeID)
	}
}

// TestExtractNonEchoCoExistence — file imports echo + chi + net/http; only
// echo-receiver routes are extracted by THIS extractor. The fixture has
// `e.GET("/echo", h)` + `r.Get("/chi", chiH)` + `http.HandleFunc("/legacy",
// legacyH)`, so echo MUST emit exactly 1 row pointing at /echo and the
// foreign /chi + /legacy routes MUST be absent (review I-4: prior assertion
// only checked ExtractorID which is trivially true since this extractor
// populates that field).
func TestExtractNonEchoCoExistence(t *testing.T) {
	pkgDir := readFixturePkg(t, "non_echo_co_existence")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("expected exactly 1 echo route (/echo), got %d: %s", len(eps), fmtEps(eps))
	}
	if eps[0].PathTemplate != "/echo" {
		t.Errorf("PathTemplate = %q; want /echo", eps[0].PathTemplate)
	}
	for _, ep := range eps {
		if ep.PathTemplate == "/chi" || ep.PathTemplate == "/legacy" {
			t.Errorf("echo extractor emitted foreign route %q (boundary leak)", ep.PathTemplate)
		}
		if ep.ExtractorID != extractorID {
			t.Errorf("foreign extractor leaked: %+v", ep)
		}
	}
}

func TestExtractGroupChainDeep(t *testing.T) {
	pkgDir := readFixturePkg(t, "group_chain_deep")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 1 || eps[0].PathTemplate != "/a/b/c" {
		t.Errorf("got %+v; want /a/b/c", eps)
	}
}

func TestExtractAliasedEchoImport(t *testing.T) {
	pkgDir := readFixturePkg(t, "aliased_echo_import")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 1 {
		t.Errorf("eps len = %d; want 1 (aliased import)", len(eps))
	}
}

func TestExtractTypelessVarBinding(t *testing.T) {
	pkgDir := readFixturePkg(t, "var_typeless_binding")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1", len(eps))
	}
}

func TestExtractTypedVarSpec(t *testing.T) {
	pkgDir := readFixturePkg(t, "var_typed_spec")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1", len(eps))
	}
}

func TestExtractSkipsTestFiles(t *testing.T) {
	pkgDir := readFixturePkg(t, "with_test_file")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	for _, ep := range eps {
		if strings.HasPrefix(ep.PathTemplate, "/test-only") {
			t.Errorf("test-file leak: %+v", ep)
		}
	}
}

func TestExtractInvalidGoFile(t *testing.T) {
	src, err := os.ReadFile(filepath.Join(readFixturePkg(t, "invalid_syntax"), "main.go.bad"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), src, 0o644); err != nil {
		t.Fatalf("write tmp: %v", err)
	}
	_, err = New().ExtractFromPackage(context.Background(), dir, "demo")
	if err == nil {
		t.Error("expected wrapped parse error")
	}
}

func TestExtractNonExistentPkgDir(t *testing.T) {
	_, err := New().ExtractFromPackage(context.Background(), "/nonexistent/abs/path", "demo")
	if err == nil {
		t.Error("expected wrapped readdir error")
	}
}

func TestC4Endpoints(t *testing.T) {
	pkgDir := readFixturePkg(t, "simple")
	file := pkgDir + "/main.go"
	eps, err := New().Endpoints(nil, file)
	if err != nil || len(eps) != 1 {
		t.Errorf("Endpoints: err=%v len=%d", err, len(eps))
	}
	calls, err := New().Calls(nil, file)
	if err != nil || len(calls) != 0 {
		t.Errorf("Calls: err=%v len=%d", err, len(calls))
	}
}

func TestFrameworksLabel(t *testing.T) {
	if got := New().Frameworks(); !reflect.DeepEqual(got, []string{"echo"}) {
		t.Errorf("Frameworks() = %v; want [echo]", got)
	}
}

func TestLanguageLabel(t *testing.T) {
	if New().Language() != extract.LangGo {
		t.Error("Language() != LangGo")
	}
}

func TestStubArtifactsEmptyForEcho(t *testing.T) {
	if got := New().StubArtifacts("any.go", nil); len(got) != 0 {
		t.Errorf("StubArtifacts = %v; want empty (echo has no stub; registry contract: empty slice not nil)", got)
	}
}

func TestStringLitOfShapes(t *testing.T) {
	mustLit := func(t *testing.T, src string) ast.Expr {
		t.Helper()
		fset := token.NewFileSet()
		f, err := goparser.ParseFile(fset, "x.go", "package x\nvar _ = "+src, goparser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %q: %v", src, err)
		}
		return f.Decls[0].(*ast.GenDecl).Specs[0].(*ast.ValueSpec).Values[0]
	}
	cases := []struct {
		src     string
		wantOK  bool
		wantVal string
	}{
		{`"foo"`, true, "foo"},
		{"`raw`", true, "raw"},
		{`42`, false, ""},
	}
	for _, c := range cases {
		got, ok := stringLitOf(mustLit(t, c.src))
		if ok != c.wantOK || got != c.wantVal {
			t.Errorf("stringLitOf(%q) = (%q, %v); want (%q, %v)", c.src, got, ok, c.wantVal, c.wantOK)
		}
	}
}

var _ = fmtEps
