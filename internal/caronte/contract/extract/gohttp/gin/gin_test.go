package gin

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
	got := extract.Default().Resolve("server.go", []byte("package main\nimport _ \"github.com/gin-gonic/gin\"\n"))
	found := false
	for _, e := range got {
		for _, f := range e.Frameworks() {
			if f == "gin" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("Default().Resolve did not yield a gin extractor; init() did not register")
	}
}

func TestDetectGin(t *testing.T) {
	e := New()
	if !e.Detect("s.go", []byte("package main\nimport \"github.com/gin-gonic/gin\"\nfunc main(){}\n")) {
		t.Error("Detect(gin file) = false; want true")
	}
	if e.Detect("s.go", []byte("package main\nimport \"github.com/go-chi/chi/v5\"\nfunc main(){}\n")) {
		t.Error("Detect(chi file) = true; want false")
	}
	if e.Detect("README.md", []byte("# docs\n")) {
		t.Error("Detect(.md) = true; want false")
	}
}

func TestEndpointsSimpleRoute(t *testing.T) {
	pkgDir := readFixturePkg(t, "simple")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1; got %+v", len(eps), eps)
	}
	if eps[0].Method != "GET" || eps[0].PathTemplate != "/health" {
		t.Errorf("eps[0] = %+v; want GET /health", eps[0])
	}
	if eps[0].ExtractorID != "gohttp-gin-v1" {
		t.Errorf("ExtractorID = %q; want gohttp-gin-v1", eps[0].ExtractorID)
	}
}

func TestExtractGroupChain(t *testing.T) {
	pkgDir := readFixturePkg(t, "group_simple")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1; got %+v", len(eps), eps)
	}
	if eps[0].PathTemplate != "/v1/users" {
		t.Errorf("PathTemplate = %q; want /v1/users", eps[0].PathTemplate)
	}
}

func TestExtractGroupVar(t *testing.T) {
	pkgDir := readFixturePkg(t, "group_var")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1; got %+v", len(eps), eps)
	}
	if eps[0].PathTemplate != "/v1/users" {
		t.Errorf("PathTemplate = %q; want /v1/users (group-var binding)", eps[0].PathTemplate)
	}
}

func TestExtractNestedGroup(t *testing.T) {
	pkgDir := readFixturePkg(t, "nested_group")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1; got %+v", len(eps), eps)
	}
	if eps[0].PathTemplate != "/v1/admin/stats" {
		t.Errorf("PathTemplate = %q; want /v1/admin/stats", eps[0].PathTemplate)
	}
}

func TestExtractGroupThreeDeep(t *testing.T) {
	pkgDir := readFixturePkg(t, "group_three_deep")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1; got %+v", len(eps), eps)
	}
	if eps[0].PathTemplate != "/a/b/c/x" {
		t.Errorf("PathTemplate = %q; want /a/b/c/x", eps[0].PathTemplate)
	}
}

func TestExtractMiddlewareInGroup(t *testing.T) {
	pkgDir := readFixturePkg(t, "middleware_in_group")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1; got %+v", len(eps), eps)
	}
	if eps[0].PathTemplate != "/v1/me" {
		t.Errorf("PathTemplate = %q; want /v1/me (middleware in Group)", eps[0].PathTemplate)
	}
}

func TestExtractDynamicRouteNormalizes(t *testing.T) {
	pkgDir := readFixturePkg(t, "dynamic_route")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1; got %+v", len(eps), eps)
	}
	if eps[0].PathTemplate != "/users/{param}" {
		t.Errorf("PathTemplate = %q; want /users/{param} (normalized)", eps[0].PathTemplate)
	}
}

func TestExtractHandlerAsMethodValue(t *testing.T) {
	pkgDir := readFixturePkg(t, "handler_as_method_value")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1; got %+v", len(eps), eps)
	}
	if !strings.Contains(eps[0].HandlerNodeID, "svc") {
		t.Errorf("HandlerNodeID = %q; want it to mention svc.<Method>", eps[0].HandlerNodeID)
	}
}

func TestExtractEmptyEngineReturnsZero(t *testing.T) {
	pkgDir := readFixturePkg(t, "empty_engine")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 0 {
		t.Errorf("eps len = %d; want 0", len(eps))
	}
}

func TestExtractIRoutesTypedReceiver(t *testing.T) {
	pkgDir := readFixturePkg(t, "irroutes_typed_receiver")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1; got %+v", len(eps), eps)
	}
}

// TestExtractNonGinCoExistence — imports gin + net/http; only gin-receiver
// routes from THIS extractor. The fixture has `r.GET("/gin", ginHandler)`
// + `http.HandleFunc("/legacy", legacyHandler)`, so gin MUST emit exactly 1
// row pointing at /gin and the foreign /legacy stdlib route MUST be absent
// (review I-4: prior assertion only checked ExtractorID which is trivially
// true since this extractor populates that field).
func TestExtractNonGinCoExistence(t *testing.T) {
	pkgDir := readFixturePkg(t, "non_gin_co_existence")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("expected exactly 1 gin route (/gin), got %d: %s", len(eps), fmtEps(eps))
	}
	if eps[0].PathTemplate != "/gin" {
		t.Errorf("PathTemplate = %q; want /gin", eps[0].PathTemplate)
	}
	for _, ep := range eps {
		if ep.PathTemplate == "/legacy" {
			t.Errorf("gin extractor emitted foreign stdlib route %q (boundary leak)", ep.PathTemplate)
		}
		if ep.ExtractorID != extractorID {
			t.Errorf("foreign extractor leaked: %+v", ep)
		}
	}
}

func TestFrameworksLabel(t *testing.T) {
	if got := New().Frameworks(); !reflect.DeepEqual(got, []string{"gin"}) {
		t.Errorf("Frameworks() = %v; want [gin]", got)
	}
}

func TestLanguageLabel(t *testing.T) {
	if New().Language() != extract.LangGo {
		t.Error("Language() != LangGo")
	}
}

func TestStubArtifactsEmptyForGin(t *testing.T) {
	if got := New().StubArtifacts("any.go", nil); len(got) != 0 {
		t.Errorf("StubArtifacts = %v; want empty (gin has no stub; registry contract: empty slice not nil)", got)
	}
}

func TestC4Endpoints(t *testing.T) {
	pkgDir := readFixturePkg(t, "simple")
	file := pkgDir + "/main.go"
	eps, err := New().Endpoints(nil, file)
	if err != nil || len(eps) != 1 {
		t.Errorf("Endpoints: err=%v len=%d; want 1 row + nil err", err, len(eps))
	}
	calls, err := New().Calls(nil, file)
	if err != nil || len(calls) != 0 {
		t.Errorf("Calls: err=%v len=%d; want 0 rows + nil err", err, len(calls))
	}
}

func TestExtractNonExistentPkgDir(t *testing.T) {
	_, err := New().ExtractFromPackage(context.Background(), "/nonexistent/abs/path", "demo")
	if err == nil {
		t.Error("expected wrapped readdir error")
	}
}

func TestExtractNoGinImport(t *testing.T) {
	pkgDir := readFixturePkg(t, "no_gin_import")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 0 {
		t.Errorf("eps len = %d; want 0", len(eps))
	}
}

func TestExtractAliasedGinImport(t *testing.T) {
	pkgDir := readFixturePkg(t, "aliased_gin_import")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1 (aliased import)", len(eps))
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
	gotProd := false
	for _, ep := range eps {
		if ep.PathTemplate == "/prod" {
			gotProd = true
		}
	}
	if !gotProd {
		t.Errorf("missing /prod; got: %s", fmtEps(eps))
	}
}

func TestExtractGroupChainDeep(t *testing.T) {
	pkgDir := readFixturePkg(t, "group_chain_deep")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1; got %+v", len(eps), eps)
	}
	if eps[0].PathTemplate != "/a/b/c" {
		t.Errorf("PathTemplate = %q; want /a/b/c (chain)", eps[0].PathTemplate)
	}
}

func TestExtractTypelessVarBinding(t *testing.T) {
	pkgDir := readFixturePkg(t, "var_typeless_binding")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1; got %+v", len(eps), eps)
	}
}

func TestExtractTypedVarSpec(t *testing.T) {
	pkgDir := readFixturePkg(t, "var_typed_spec")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1 (typed var spec); got %+v", len(eps), eps)
	}
}

func TestExtractAnonHandler(t *testing.T) {
	pkgDir := readFixturePkg(t, "anon_handler")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1", len(eps))
	}
	if !strings.Contains(eps[0].HandlerNodeID, ".anon") {
		t.Errorf("HandlerNodeID = %q; want it to contain .anon", eps[0].HandlerNodeID)
	}
}

func TestExtractAllVerbs(t *testing.T) {
	pkgDir := readFixturePkg(t, "all_verbs")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	verbs := map[string]bool{}
	for _, ep := range eps {
		verbs[ep.Method] = true
	}
	for _, m := range []string{"GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS"} {
		if !verbs[m] {
			t.Errorf("verb %q missing from extracted set: %v", m, verbs)
		}
	}
}

func TestExtractDynamicPath(t *testing.T) {
	pkgDir := readFixturePkg(t, "dynamic_path")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	for _, ep := range eps {
		if !strings.HasPrefix(ep.PathTemplate, "/static/") {
			t.Errorf("unexpected dynamic path leaked: %s", fmtEps(eps))
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
		{`'x'`, false, ""},
	}
	for _, c := range cases {
		got, ok := stringLitOf(mustLit(t, c.src))
		if ok != c.wantOK || got != c.wantVal {
			t.Errorf("stringLitOf(%q) = (%q, %v); want (%q, %v)", c.src, got, ok, c.wantVal, c.wantOK)
		}
	}
}

var _ = fmtEps
