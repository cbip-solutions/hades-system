package stdlib

import (
	"context"
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
	got := extract.Default().Resolve("s.go", []byte("package main\nimport \"net/http\"\nfunc main(){}\n"))
	found := false
	for _, e := range got {
		for _, f := range e.Frameworks() {
			if f == "stdlib" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("Default().Resolve did not yield a stdlib extractor; init() did not register")
	}
}

func TestDetectStdlib(t *testing.T) {
	e := New()
	if !e.Detect("s.go", []byte("package main\nimport \"net/http\"\nfunc main(){}\n")) {
		t.Error("Detect(net/http file) = false; want true")
	}
	if e.Detect("s.go", []byte("package main\nimport \"github.com/go-chi/chi/v5\"\nfunc main(){}\n")) {
		t.Error("Detect(chi file) = true; want false (chi-imported = higher-level ownership)")
	}
	if e.Detect("s.go", []byte("package main\nimport (\n\"net/http\"\n\"github.com/gin-gonic/gin\"\n)\nfunc main(){}\n")) {
		t.Error("Detect(net/http + gin) = true; want false")
	}
	if e.Detect("README.md", []byte("# docs\n")) {
		t.Error("Detect(.md) = true; want false")
	}
}

func TestEndpointsModernPattern(t *testing.T) {
	pkgDir := readFixturePkg(t, "modern_pattern")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1; got %s", len(eps), fmtEps(eps))
	}
	if eps[0].Method != "GET" || eps[0].PathTemplate != "/health" {
		t.Errorf("eps[0] = %+v; want GET /health", eps[0])
	}
	if eps[0].ExtractorID != "gohttp-stdlib-v1" {
		t.Errorf("ExtractorID = %q; want gohttp-stdlib-v1", eps[0].ExtractorID)
	}
}

func TestEndpointsLegacyCatchAll(t *testing.T) {
	pkgDir := readFixturePkg(t, "legacy_catchall")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1", len(eps))
	}
	if eps[0].Method != "" {
		t.Errorf("Method = %q; want empty (catch-all)", eps[0].Method)
	}
	if eps[0].PathTemplate != "/legacy" {
		t.Errorf("PathTemplate = %q; want /legacy", eps[0].PathTemplate)
	}
}

func TestExtractDefaultServeMux(t *testing.T) {
	pkgDir := readFixturePkg(t, "default_servemux")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1", len(eps))
	}
}

func TestExtractMuxHandlefunc(t *testing.T) {
	pkgDir := readFixturePkg(t, "mux_handlefunc")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 1 || eps[0].Method != "POST" || eps[0].PathTemplate != "/api/login" {
		t.Errorf("got %+v; want POST /api/login", eps)
	}
}

func TestExtractMuxHandle(t *testing.T) {
	pkgDir := readFixturePkg(t, "mux_handle")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 1 || eps[0].Method != "GET" {
		t.Errorf("got %+v; want GET /static/...", eps)
	}
}

func TestExtractPathWithHost(t *testing.T) {
	pkgDir := readFixturePkg(t, "path_with_host")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 1 || eps[0].Method != "GET" || eps[0].PathTemplate != "/x" {
		t.Errorf("got %+v; want GET /x (host stripped)", eps)
	}
}

func TestExtractMethodTypedPrefix(t *testing.T) {
	pkgDir := readFixturePkg(t, "method_typed_prefix")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 1 || eps[0].Method != "DELETE" || eps[0].PathTemplate != "/resource/{param}" {
		t.Errorf("got %+v; want DELETE /resource/{param}", eps)
	}
}

func TestExtractDynamicPath(t *testing.T) {
	pkgDir := readFixturePkg(t, "dynamic_path")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 1 || eps[0].PathTemplate != "/users/{param}/posts/{param}" {
		t.Errorf("got %+v; want /users/{param}/posts/{param}", eps)
	}
}

func TestExtractMiddlewareWrapHandler(t *testing.T) {
	pkgDir := readFixturePkg(t, "middleware_wrap_handler")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1", len(eps))
	}
	if eps[0].PathTemplate != "/x" {
		t.Errorf("PathTemplate = %q; want /x", eps[0].PathTemplate)
	}
}

func TestExtractCoExistenceWithChiSkipsFile(t *testing.T) {

	pkgDir := readFixturePkg(t, "co_existence_with_chi")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 0 {
		t.Errorf("eps len = %d; want 0 (file owned by chi)", len(eps))
	}
}

func TestParseGo122PatternShapes(t *testing.T) {
	cases := []struct {
		in         string
		wantMethod string
		wantPath   string
	}{
		{"GET /health", "GET", "/health"},
		{"POST example.com/x/{id}", "POST", "/x/{id}"},
		{"/legacy", "", "/legacy"},
		{"/{$}", "", "/{$}"},
		{"DELETE /a/{b}/c/{d...}", "DELETE", "/a/{b}/c/{d...}"},
		{"", "", ""},
		{"GET", "", "GET"},
		{"get /x", "", "/x"},
		{"  /spaced", "", "/spaced"},
	}
	for _, c := range cases {
		m, p := parseGo122Pattern(c.in)
		if m != c.wantMethod || p != c.wantPath {
			t.Errorf("parseGo122Pattern(%q) = (%q, %q); want (%q, %q)",
				c.in, m, p, c.wantMethod, c.wantPath)
		}
	}
}

func TestC4Endpoints(t *testing.T) {
	pkgDir := readFixturePkg(t, "modern_pattern")
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
	if got := New().Frameworks(); !reflect.DeepEqual(got, []string{"stdlib"}) {
		t.Errorf("Frameworks() = %v; want [stdlib]", got)
	}
}

func TestLanguageLabel(t *testing.T) {
	if New().Language() != extract.LangGo {
		t.Error("Language() != LangGo")
	}
}

func TestStubArtifactsEmptyForStdlib(t *testing.T) {
	if got := New().StubArtifacts("any.go", nil); len(got) != 0 {
		t.Errorf("StubArtifacts = %v; want empty (stdlib has no stub; registry contract: empty slice not nil)", got)
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

func TestExtractAliasedHTTPImport(t *testing.T) {
	pkgDir := readFixturePkg(t, "aliased_http_import")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 1 || eps[0].Method != "GET" || eps[0].PathTemplate != "/health" {
		t.Errorf("got %+v; want GET /health (aliased import)", eps)
	}
}

func TestExtractMuxVarSpec(t *testing.T) {
	pkgDir := readFixturePkg(t, "mux_var_spec")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 2 {
		t.Fatalf("eps len = %d; want 2 (typed + typeless spec)", len(eps))
	}
}

func TestExtractMuxParam(t *testing.T) {
	pkgDir := readFixturePkg(t, "mux_param")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1 (param-typed mux)", len(eps))
	}
}

func TestExtractNoNetHTTPSkipsFile(t *testing.T) {
	pkgDir := readFixturePkg(t, "no_nethttp")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 0 {
		t.Errorf("eps len = %d; want 0 (no net/http import)", len(eps))
	}
}

func TestExtractDynamicPattern(t *testing.T) {
	pkgDir := readFixturePkg(t, "dynamic_pattern")
	eps, err := New().ExtractFromPackage(context.Background(), pkgDir, "demo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1 (only the static one); got %s", len(eps), fmtEps(eps))
	}
	if eps[0].PathTemplate != "/static" {
		t.Errorf("PathTemplate = %q; want /static", eps[0].PathTemplate)
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
		t.Errorf("HandlerNodeID = %q; want .anon", eps[0].HandlerNodeID)
	}
}

var _ = fmtEps
