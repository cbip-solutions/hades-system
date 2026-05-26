package chi

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

func TestExtractorRegistersOnInit(t *testing.T) {
	got := extract.Default().Resolve("any.go", []byte("package x\nimport _ \"github.com/go-chi/chi/v5\"\n"))
	foundChi := false
	for _, e := range got {
		for _, f := range e.Frameworks() {
			if f == "chi" {
				foundChi = true
				break
			}
		}
	}
	if !foundChi {
		t.Fatal("Default().Resolve did not yield a chi extractor; init() did not register")
	}
}

func TestDetectChi(t *testing.T) {
	e := New()
	if !e.Detect("server.go", []byte("package main\nimport \"github.com/go-chi/chi/v5\"\nfunc main(){}\n")) {
		t.Error("Detect(chi v5 file) = false; want true")
	}
	if !e.Detect("server.go", []byte("package main\nimport \"github.com/go-chi/chi\"\nfunc main(){}\n")) {
		t.Error("Detect(chi v1 file) = false; want true")
	}
	if e.Detect("server.go", []byte("package main\nimport \"github.com/gin-gonic/gin\"\nfunc main(){}\n")) {
		t.Error("Detect(gin file) = true; want false (not chi)")
	}
	if e.Detect("README.md", []byte("# docs\n")) {
		t.Error("Detect(.md) = true; want false (extension gate)")
	}
}

func TestEndpointsSimpleRoute(t *testing.T) {
	pkgDir := readFixturePkg(t, "simple")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo-svc")
	if err != nil {
		t.Fatalf("ExtractFromPackage: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1; got %+v", len(eps), eps)
	}
	got := eps[0]
	if got.Kind != "http" {
		t.Errorf("Kind = %q; want http", got.Kind)
	}
	if got.Method != "GET" {
		t.Errorf("Method = %q; want GET", got.Method)
	}
	if got.PathTemplate != "/health" {
		t.Errorf("PathTemplate = %q; want /health", got.PathTemplate)
	}
	if got.Repo != "demo-svc" {
		t.Errorf("Repo = %q; want demo-svc", got.Repo)
	}
	if got.ExtractorID != "gohttp-chi-v1" {
		t.Errorf("ExtractorID = %q; want gohttp-chi-v1", got.ExtractorID)
	}
}

func TestExtractNestedRoute(t *testing.T) {
	pkgDir := readFixturePkg(t, "nested_route")
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

func TestExtractNestedRouteDeep(t *testing.T) {
	pkgDir := readFixturePkg(t, "nested_route_deep")
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

func TestExtractGroupNoPrefix(t *testing.T) {
	pkgDir := readFixturePkg(t, "group_no_prefix")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1 (Use is not a route); got %+v", len(eps), eps)
	}
	if eps[0].PathTemplate != "/me" {
		t.Errorf("PathTemplate = %q; want /me", eps[0].PathTemplate)
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

func TestExtractReceiverAliasedParam(t *testing.T) {
	pkgDir := readFixturePkg(t, "receiver_aliased_param")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1 (param-typed receiver); got %+v", len(eps), eps)
	}
}

func TestExtractEmptyRouterReturnsZero(t *testing.T) {
	pkgDir := readFixturePkg(t, "empty_router")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 0 {
		t.Errorf("eps len = %d; want 0 (no route calls)", len(eps))
	}
}

// TestExtractNonChiCoExistence — file imports BOTH chi AND net/http; only
// chi-receiver routes are extracted by THIS extractor. The fixture has
// `r.Get("/chi", chiHandler)` + `http.HandleFunc("/legacy", legacyHandler)`,
// so chi MUST emit exactly 1 row pointing at /chi and the foreign /legacy
// stdlib route MUST be absent. The ExtractorID check is a basic property
// but the load-bearing assertion is the exact count + foreign-path absence
// (review I-4: the prior assertion was trivially true because the same
// extractor populates ExtractorID).
func TestExtractNonChiCoExistence(t *testing.T) {
	pkgDir := readFixturePkg(t, "non_chi_co_existence")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("expected exactly 1 chi route (/chi), got %d: %s", len(eps), fmtEps(eps))
	}
	if eps[0].PathTemplate != "/chi" {
		t.Errorf("PathTemplate = %q; want /chi", eps[0].PathTemplate)
	}
	for _, ep := range eps {
		if ep.PathTemplate == "/legacy" {
			t.Errorf("chi extractor emitted foreign stdlib route %q (boundary leak)", ep.PathTemplate)
		}
		if ep.ExtractorID != extractorID {
			t.Errorf("ep.ExtractorID = %q; want %q (foreign extractor leaked in)", ep.ExtractorID, extractorID)
		}
	}
}

func TestExtractMountSubrouterNoOuterDoubleCount(t *testing.T) {
	pkgDir := readFixturePkg(t, "mount_subrouter")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	for _, ep := range eps {
		if strings.HasPrefix(ep.PathTemplate, "/admin/") {
			t.Errorf("outer-package Mount double-counted: %+v", ep)
		}
	}
}

func TestExtractMultiMethod(t *testing.T) {
	pkgDir := readFixturePkg(t, "multi_method")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 2 {
		t.Fatalf("eps len = %d; want 2 (Get + Post); got %+v", len(eps), eps)
	}
	seen := map[string]bool{}
	for _, ep := range eps {
		seen[ep.Method] = true
	}
	if !seen["GET"] || !seen["POST"] {
		t.Errorf("methods = %v; want GET + POST", seen)
	}
}

func TestExtractDynamicHandlerInline(t *testing.T) {
	pkgDir := readFixturePkg(t, "anon_handler")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1", len(eps))
	}
	if eps[0].HandlerNodeID == "" {
		t.Error("HandlerNodeID empty; want <pkg>.<file>:<line>.anon")
	}
}

func TestFrameworksLabel(t *testing.T) {
	e := New()
	if got := e.Frameworks(); !reflect.DeepEqual(got, []string{"chi"}) {
		t.Errorf("Frameworks() = %v; want [chi]", got)
	}
}

func TestLanguageLabel(t *testing.T) {
	if New().Language() != extract.LangGo {
		t.Error("Language() != LangGo")
	}
}

func TestStubArtifactsEmptyForChi(t *testing.T) {
	if got := New().StubArtifacts("any.go", []byte("package x\n")); len(got) != 0 {
		t.Errorf("StubArtifacts = %v; want empty (chi has no stub; registry contract: empty slice not nil)", got)
	}
}

func TestC4EndpointsAndCalls(t *testing.T) {
	pkgDir := readFixturePkg(t, "simple")

	file := pkgDir + "/main.go"
	e := New()
	eps, err := e.Endpoints(nil, file)
	if err != nil {
		t.Fatalf("Endpoints: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("Endpoints len = %d; want 1", len(eps))
	}
	calls, err := e.Calls(nil, file)
	if err != nil {
		t.Errorf("Calls: %v", err)
	}
	if len(calls) != 0 {
		t.Errorf("Calls len = %d; want 0", len(calls))
	}
}

func TestExtractHandlerSelector(t *testing.T) {
	pkgDir := readFixturePkg(t, "handler_selector")
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

func TestExtractVarSpecBinding(t *testing.T) {
	pkgDir := readFixturePkg(t, "var_spec_binding")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1 (var-spec); got %+v", len(eps), eps)
	}
}

// TestExtractMountInlineFuncLit — Mount("/area", func(r chi.Router){...})
// inline-FuncLit form. chi.Mount's real signature takes a chi.Routes /
// http.Handler, so this shape is non-standard, but the extractor's walker
// tolerates it (the prefix is pushed; the inner func body is walked).
//
// The fixture (testdata/mount_inline_funclit/main.go) has exactly ONE
// route — `r.Get("/inner", inner)` inside the inline-FuncLit body —
// so the extractor MUST emit exactly 1 row with PathTemplate /area/inner.
// The handleCompositionMount inline-FuncLit branch (chi.go: handles the
// case where the Mount's second arg is *ast.FuncLit) is the load-bearing
// support; pinning the count guards against regression of that branch
// (review I-5: prior assertion used "may emit 0 or 1" hedge that masked
// any regression to 0).
func TestExtractMountInlineFuncLit(t *testing.T) {
	pkgDir := readFixturePkg(t, "mount_inline_funclit")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("expected exactly 1 inline-FuncLit-Mount route, got %d: %s", len(eps), fmtEps(eps))
	}
	if eps[0].PathTemplate != "/area/inner" {
		t.Errorf("PathTemplate = %q; want /area/inner", eps[0].PathTemplate)
	}
}

// TestExtractIgnoresTestFiles — _test.go files MUST be skipped (not
// treated as deployable routes).
func TestExtractIgnoresTestFiles(t *testing.T) {
	pkgDir := readFixturePkg(t, "with_test_file")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	for _, ep := range eps {

		if strings.HasPrefix(ep.PathTemplate, "/test-only/") {
			t.Errorf("test-file route leaked: %+v", ep)
		}
	}

	foundProd := false
	for _, ep := range eps {
		if ep.PathTemplate == "/prod" {
			foundProd = true
		}
	}
	if !foundProd {
		t.Error("expected /prod route from main.go; got: " + fmtEps(eps))
	}
}

func TestExtractNonExistentPkgDir(t *testing.T) {
	_, err := New().ExtractFromPackage(context.Background(), "/nonexistent/abs/path/xyz", "demo")
	if err == nil {
		t.Error("ExtractFromPackage(bogus dir) returned nil error; want a wrapped readdir failure")
	}
}

func TestExtractInvalidGoFile(t *testing.T) {
	src, err := os.ReadFile(filepath.Join(readFixturePkg(t, "invalid_syntax"), "main.go.bad"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), src, 0o644); err != nil {
		t.Fatalf("write tmp fixture: %v", err)
	}
	_, err = New().ExtractFromPackage(context.Background(), dir, "demo")
	if err == nil {
		t.Error("ExtractFromPackage(invalid syntax) returned nil error; want a wrapped parse failure")
	}
}

func TestExtractNoChiImport(t *testing.T) {
	pkgDir := readFixturePkg(t, "no_chi_import")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 0 {
		t.Errorf("eps len = %d; want 0 (no chi import); got %+v", len(eps), eps)
	}
}

func fmtEps(eps []store.APIEndpoint) string {
	out := ""
	for _, ep := range eps {
		out += ep.Method + " " + ep.PathTemplate + ", "
	}
	return out
}

func TestExtractTypelessVarBinding(t *testing.T) {
	pkgDir := readFixturePkg(t, "var_typeless_binding")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1 (typeless var); got %+v", len(eps), eps)
	}
}

func TestExtractAliasedChiImport(t *testing.T) {
	pkgDir := readFixturePkg(t, "aliased_chi_import")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1 (aliased import); got %+v", len(eps), eps)
	}
	if eps[0].PathTemplate != "/health" {
		t.Errorf("PathTemplate = %q; want /health", eps[0].PathTemplate)
	}
}

func TestExtractDeepNestedInsideGroup(t *testing.T) {
	pkgDir := readFixturePkg(t, "deep_nested_inside_group")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	var inner, sub bool
	for _, ep := range eps {
		if ep.PathTemplate == "/v1/inner" && ep.Method == "GET" {
			inner = true
		}
		if ep.PathTemplate == "/v1/sub/x" && ep.Method == "POST" {
			sub = true
		}
	}
	if !inner {
		t.Errorf("missing /v1/inner; got: %s", fmtEps(eps))
	}
	if !sub {
		t.Errorf("missing /v1/sub/x; got: %s", fmtEps(eps))
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
		expr := mustLit(t, c.src)
		got, ok := stringLitOf(expr)
		if ok != c.wantOK {
			t.Errorf("stringLitOf(%q) ok = %v; want %v", c.src, ok, c.wantOK)
		}
		if got != c.wantVal {
			t.Errorf("stringLitOf(%q) val = %q; want %q", c.src, got, c.wantVal)
		}
	}
}

func TestExtractDynamicPath(t *testing.T) {
	pkgDir := readFixturePkg(t, "dynamic_path")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	gotHealth := false
	for _, ep := range eps {
		if ep.PathTemplate == "/static/health" {
			gotHealth = true
		}
	}
	if !gotHealth {
		t.Errorf("expected /static/health (literal-arg call); got: %s", fmtEps(eps))
	}
}

func TestExtractAllHTTPVerbs(t *testing.T) {
	pkgDir := readFixturePkg(t, "all_verbs")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	verbs := map[string]bool{}
	for _, ep := range eps {
		verbs[ep.Method] = true
	}
	want := []string{"GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS"}
	for _, m := range want {
		if !verbs[m] {
			t.Errorf("verb %q missing from extracted set: %v", m, verbs)
		}
	}
}
