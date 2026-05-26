//go:build cgo
// +build cgo

package fastapi

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/caronte/contract/extract"
	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

func TestExtractorRegistersOnInit(t *testing.T) {
	src := []byte("from fastapi import FastAPI\napp = FastAPI()\n")
	got := extract.Default().Resolve("app.py", src)
	found := false
	for _, e := range got {
		if e.Language() == extract.LangPython {
			for _, fw := range e.Frameworks() {
				if fw == "fastapi" {
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatal("Default().Resolve(*.py with fastapi import) returned no LangPython/fastapi extractor; init() did not register")
	}
}

func TestDetectFastAPI(t *testing.T) {
	e := New()
	cases := []struct {
		name, file string
		src        string
		want       bool
	}{
		{"py with from-import", "app.py", "from fastapi import FastAPI\napp = FastAPI()\n", true},
		{"py with plain import", "app.py", "import fastapi\napp = fastapi.FastAPI()\n", true},
		{"py with import alias", "app.py", "import fastapi as f\napp = f.FastAPI()\n", true},
		{"py without import", "app.py", "x = 1\n", false},
		{"go file with fastapi text", "app.go", "package main\nimport \"fastapi\"\n", false},
		{"empty file", "empty.py", "", false},
	}
	for _, c := range cases {
		if got := e.Detect(c.file, []byte(c.src)); got != c.want {
			t.Errorf("Detect(%s) = %v; want %v", c.name, got, c.want)
		}
	}
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("fixtures", name))
	if err != nil {
		t.Fatalf("readFixture(%s): %v", name, err)
	}
	return src
}

func readFixtureRepo(t *testing.T, name string) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("fixtures", name))
	if err != nil {
		t.Fatalf("readFixtureRepo(%s): %v", name, err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("readFixtureRepo(%s) stat: %v", name, err)
	}
	return p
}

func TestEndpointsFromBytesSingleDecorator(t *testing.T) {
	src := readFixture(t, "single_decorator.py")
	e := New()
	eps, err := e.EndpointsFromBytes(context.Background(), "app.py", src, "auth-svc", "")
	if err != nil {
		t.Fatalf("EndpointsFromBytes: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1", len(eps))
	}
	if eps[0].Kind != "http" {
		t.Errorf("Kind = %q; want http", eps[0].Kind)
	}
	if eps[0].Method != "GET" {
		t.Errorf("Method = %q; want GET", eps[0].Method)
	}
	if eps[0].PathTemplate != "/users/{id}" {
		t.Errorf("PathTemplate = %q; want /users/{id}", eps[0].PathTemplate)
	}
	if eps[0].Repo != "auth-svc" {
		t.Errorf("Repo = %q; want auth-svc", eps[0].Repo)
	}
	if eps[0].ExtractorID != ExtractorID {
		t.Errorf("ExtractorID = %q; want %q", eps[0].ExtractorID, ExtractorID)
	}
	if eps[0].HandlerNodeID == "" {
		t.Error("HandlerNodeID empty; want <module>.<func>")
	}
}

type methodPath struct {
	method string
	path   string
}

func apiEndpointSet(eps []store.APIEndpoint) map[methodPath]int {
	out := make(map[methodPath]int, len(eps))
	for _, ep := range eps {
		out[methodPath{method: ep.Method, path: ep.PathTemplate}]++
	}
	return out
}

func TestEndpointsAPIRouterPrefix(t *testing.T) {
	src := readFixture(t, "router_with_prefix.py")
	e := New()
	eps, err := e.EndpointsFromBytes(context.Background(), "router.py", src, "svc", "")
	if err != nil {
		t.Fatalf("EndpointsFromBytes: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1", len(eps))
	}
	if eps[0].Method != "GET" || eps[0].PathTemplate != "/v1/items" {
		t.Errorf("(method,path) = (%s,%s); want (GET,/v1/items)", eps[0].Method, eps[0].PathTemplate)
	}
}

func TestEndpointsIncludeRouterPrefix(t *testing.T) {
	src := readFixture(t, "include_router_prefix.py")
	e := New()
	eps, err := e.EndpointsFromBytes(context.Background(), "app.py", src, "svc", "")
	if err != nil {
		t.Fatalf("EndpointsFromBytes: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1", len(eps))
	}
	want := "/api/v1/items/{item_id}"
	if eps[0].Method != "GET" || eps[0].PathTemplate != want {
		t.Errorf("(method,path) = (%s,%s); want (GET,%s)", eps[0].Method, eps[0].PathTemplate, want)
	}
}

func TestEndpointsMultiMethod(t *testing.T) {
	src := readFixture(t, "multi_method.py")
	e := New()
	eps, err := e.EndpointsFromBytes(context.Background(), "app.py", src, "svc", "")
	if err != nil {
		t.Fatalf("EndpointsFromBytes: %v", err)
	}
	if len(eps) != 5 {
		t.Fatalf("eps len = %d; want 5", len(eps))
	}
	got := apiEndpointSet(eps)
	want := map[methodPath]int{
		{"GET", "/users/{id}"}:    1,
		{"POST", "/users"}:        1,
		{"PUT", "/users/{id}"}:    1,
		{"DELETE", "/users/{id}"}: 1,
		{"PATCH", "/users/{id}"}:  1,
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("missing/wrong-count for %v: got %d, want %d", k, got[k], v)
		}
	}
}

func TestEndpointsStackedDecorators(t *testing.T) {
	src := readFixture(t, "stacked_decorators.py")
	e := New()
	eps, err := e.EndpointsFromBytes(context.Background(), "app.py", src, "svc", "")
	if err != nil {
		t.Fatalf("EndpointsFromBytes: %v", err)
	}
	if len(eps) != 2 {
		t.Fatalf("eps len = %d; want 2", len(eps))
	}

	if eps[0].HandlerNodeID != eps[1].HandlerNodeID {
		t.Errorf("stacked decorators got distinct handlers: %q vs %q", eps[0].HandlerNodeID, eps[1].HandlerNodeID)
	}
	methods := map[string]bool{eps[0].Method: true, eps[1].Method: true}
	if !methods["GET"] || !methods["HEAD"] {
		t.Errorf("missing GET/HEAD: methods = %v", methods)
	}
}

func TestEndpointsPathConverters(t *testing.T) {
	src := readFixture(t, "path_converters.py")
	e := New()
	eps, err := e.EndpointsFromBytes(context.Background(), "app.py", src, "svc", "")
	if err != nil {
		t.Fatalf("EndpointsFromBytes: %v", err)
	}
	if len(eps) != 2 {
		t.Fatalf("eps len = %d; want 2", len(eps))
	}
	paths := map[string]bool{}
	for _, ep := range eps {
		paths[ep.PathTemplate] = true
	}
	if !paths["/items/{item_id}"] || !paths["/files/{file_path}"] {
		t.Errorf("expected canonicalised paths /items/{item_id} + /files/{file_path}; got %v", paths)
	}
}

func TestEndpointsFalsePositive(t *testing.T) {
	src := readFixture(t, "false_positive.py")
	e := New()
	if e.Detect("false_positive.py", src) {
		t.Error("Detect should be false (no fastapi import)")
	}
	eps, err := e.EndpointsFromBytes(context.Background(), "false_positive.py", src, "svc", "")
	if err != nil {
		t.Fatalf("EndpointsFromBytes: %v", err)
	}
	if len(eps) != 0 {
		t.Errorf("eps len = %d; want 0 (no decorators)", len(eps))
	}
}

func TestEndpointsArtifactPreferred(t *testing.T) {
	repo := readFixtureRepo(t, "repo_with_artifact")
	src := readFixture(t, "repo_with_artifact/app.py")
	e := New()
	eps, err := e.EndpointsFromBytes(context.Background(), "app.py", src, "svc", repo)
	if err != nil {
		t.Fatalf("EndpointsFromBytes: %v", err)
	}
	if len(eps) != 2 {
		t.Fatalf("eps len = %d; want 2 (artifact-derived)", len(eps))
	}
	for _, ep := range eps {
		if ep.ContractArtifact == "" {
			t.Errorf("endpoint %v has empty ContractArtifact; expected artifact path", ep)
		}
	}
	got := apiEndpointSet(eps)
	want := map[methodPath]int{
		{"GET", "/users"}:        1,
		{"GET", "/admin/health"}: 1,
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("artifact missing %v: got %d, want %d", k, got[k], v)
		}
	}

	for _, ep := range eps {
		switch ep.PathTemplate {
		case "/users":
			if ep.HandlerNodeID != "listUsers" {
				t.Errorf("/users HandlerNodeID = %q; want listUsers", ep.HandlerNodeID)
			}
		case "/admin/health":
			if ep.HandlerNodeID != "adminHealth" {
				t.Errorf("/admin/health HandlerNodeID = %q; want adminHealth", ep.HandlerNodeID)
			}
		}
	}
}

func TestEndpointsArtifactYAML(t *testing.T) {
	repo := readFixtureRepo(t, "repo_with_yaml_artifact")
	src := readFixture(t, "repo_with_yaml_artifact/app.py")
	e := New()
	eps, err := e.EndpointsFromBytes(context.Background(), "app.py", src, "svc", repo)
	if err != nil {
		t.Fatalf("EndpointsFromBytes: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1 (yaml artifact)", len(eps))
	}
	if eps[0].PathTemplate != "/v1/health" || eps[0].Method != "GET" {
		t.Errorf("(method,path) = (%s,%s); want (GET,/v1/health)", eps[0].Method, eps[0].PathTemplate)
	}
	if eps[0].HandlerNodeID != "healthCheck" {
		t.Errorf("HandlerNodeID = %q; want healthCheck", eps[0].HandlerNodeID)
	}
	if eps[0].ContractArtifact == "" {
		t.Errorf("ContractArtifact empty; expected the artifact path")
	}
}

func TestEndpointsArtifactBrokenFallsBackToAST(t *testing.T) {
	repo := readFixtureRepo(t, "repo_with_broken_artifact")
	src := readFixture(t, "repo_with_broken_artifact/app.py")
	e := New()
	eps, err := e.EndpointsFromBytes(context.Background(), "app.py", src, "svc", repo)
	if err != nil {
		t.Fatalf("EndpointsFromBytes: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1 (AST fallback)", len(eps))
	}
	if eps[0].ContractArtifact != "" {
		t.Errorf("ContractArtifact = %q; want empty (AST path)", eps[0].ContractArtifact)
	}
	if eps[0].Method != "GET" || eps[0].PathTemplate != "/ok" {
		t.Errorf("(method,path) = (%s,%s); want (GET,/ok)", eps[0].Method, eps[0].PathTemplate)
	}
}

func TestEndpointsEmptyFile(t *testing.T) {
	src := readFixture(t, "empty.py")
	e := New()
	if e.Detect("empty.py", src) {
		t.Error("Detect should be false (no fastapi import)")
	}
	eps, err := e.EndpointsFromBytes(context.Background(), "empty.py", src, "svc", "")
	if err != nil {
		t.Fatalf("EndpointsFromBytes: %v", err)
	}
	if len(eps) != 0 {
		t.Errorf("eps len = %d; want 0", len(eps))
	}
}

func TestEndpointsNoDecorators(t *testing.T) {
	src := readFixture(t, "no_decorators.py")
	e := New()
	if !e.Detect("no_decorators.py", src) {
		t.Error("Detect should be true (fastapi import present)")
	}
	eps, err := e.EndpointsFromBytes(context.Background(), "no_decorators.py", src, "svc", "")
	if err != nil {
		t.Fatalf("EndpointsFromBytes: %v", err)
	}
	if len(eps) != 0 {
		t.Errorf("eps len = %d; want 0", len(eps))
	}
}

func TestEndpointsNestedRouters(t *testing.T) {
	src := readFixture(t, "nested_routers.py")
	e := New()
	eps, err := e.EndpointsFromBytes(context.Background(), "nested.py", src, "svc", "")
	if err != nil {
		t.Fatalf("EndpointsFromBytes: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1", len(eps))
	}
	want := "/api/v1/users/{id}"
	if eps[0].PathTemplate != want {
		t.Errorf("PathTemplate = %q; want %s", eps[0].PathTemplate, want)
	}
}

func TestInterfaceShims(t *testing.T) {
	e := New()
	if calls, err := e.Calls(nil, "app.py"); err != nil || calls != nil {
		t.Errorf("Calls returned (%v, %v); want (nil, nil) (FastAPI is server-side)", calls, err)
	}
	if stubs := e.StubArtifacts("app.py", []byte("anything")); len(stubs) != 0 {
		t.Errorf("StubArtifacts returned %v; want empty (FastAPI has no gRPC stubs; registry contract: empty slice not nil)", stubs)
	}
	if eps, err := e.Endpoints(nil, "app.py"); err != nil || eps != nil {
		t.Errorf("Endpoints(nil, ...) returned (%v, %v); want (nil, nil) (nil-tree fast path)", eps, err)
	}
}

func TestPyModulePath(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"pkg/util/helpers.py", "pkg.util.helpers"},
		{"pkg/util/__init__.py", "pkg.util"},
		{"__init__.py", ""},
		{"single.py", "single"},
		{"no_extension", "no_extension"},

		{"pkg.bar/no_extension", "pkg"},

		{"pkg.bar/helpers.py", "pkg.bar.helpers"},
	}
	for _, c := range cases {
		if got := pyModulePath(c.in); got != c.want {
			t.Errorf("pyModulePath(%q) = %q; want %q", c.in, got, c.want)
		}
	}
}

func TestPyNodeID(t *testing.T) {
	cases := []struct {
		file, fn, want string
	}{
		{"app.py", "get_user", "app.get_user"},
		{"pkg/svc.py", "list", "pkg.svc.list"},
		{"__init__.py", "boot", "boot"},
	}
	for _, c := range cases {
		if got := pyNodeID(c.file, c.fn); got != c.want {
			t.Errorf("pyNodeID(%q, %q) = %q; want %q", c.file, c.fn, got, c.want)
		}
	}
}

func TestCanonicalisePath(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"/users/{id}", "/users/{id}"},
		{"/items/{id:int}", "/items/{id}"},
		{"/files/{p:path}", "/files/{p}"},
		{"/v1/", "/v1"},
		{"/", "/"},
		{"", ""},
		{"//api//users", "/api/users"},
	}
	for _, c := range cases {
		if got := canonicalisePath(c.in); got != c.want {
			t.Errorf("canonicalisePath(%q) = %q; want %q", c.in, got, c.want)
		}
	}
}

func TestArtifactToEndpointsEmpty(t *testing.T) {
	doc := &openAPIDoc{}
	got := artifactToEndpoints(doc, "/tmp/openapi.json", "svc")
	if got == nil {
		t.Error("artifactToEndpoints({}) returned nil; want empty non-nil slice")
	}
	if len(got) != 0 {
		t.Errorf("len = %d; want 0", len(got))
	}
}

func TestArtifactToEndpointsSyntheticHandlerID(t *testing.T) {
	doc := &openAPIDoc{
		Paths: map[string]map[string]openAPIOperation{
			"/users": {
				"get": {OperationID: "", Summary: ""},
			},
		},
	}
	got := artifactToEndpoints(doc, "/tmp/openapi.json", "svc")
	if len(got) != 1 {
		t.Fatalf("len = %d; want 1", len(got))
	}
	want := "GET:/users"
	if got[0].HandlerNodeID != want {
		t.Errorf("HandlerNodeID = %q; want %q", got[0].HandlerNodeID, want)
	}
}

func TestDecodeOpenAPIUnsupportedExt(t *testing.T) {
	if _, err := decodeOpenAPI([]byte("{}"), ".toml"); err == nil {
		t.Error("decodeOpenAPI(.toml) returned nil err; want unsupported-extension")
	}
}

func TestEndpointsFastAPIModuleAttribute(t *testing.T) {
	src := readFixture(t, "fastapi_module_attribute.py")
	e := New()
	eps, err := e.EndpointsFromBytes(context.Background(), "app.py", src, "svc", "")
	if err != nil {
		t.Fatalf("EndpointsFromBytes: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1", len(eps))
	}
	if eps[0].PathTemplate != "/sys/status" || eps[0].Method != "GET" {
		t.Errorf("(method,path) = (%s,%s); want (GET,/sys/status)", eps[0].Method, eps[0].PathTemplate)
	}
}

func TestEndpointsIncludeComplexArg(t *testing.T) {
	src := readFixture(t, "include_complex_arg.py")
	e := New()
	eps, err := e.EndpointsFromBytes(context.Background(), "x.py", src, "svc", "")
	if err != nil {
		t.Fatalf("EndpointsFromBytes: %v", err)
	}

	if len(eps) != 0 {
		t.Errorf("eps len = %d; want 0", len(eps))
	}
}

func TestEndpointsRouterNoPrefix(t *testing.T) {
	src := readFixture(t, "router_no_prefix.py")
	e := New()
	eps, err := e.EndpointsFromBytes(context.Background(), "x.py", src, "svc", "")
	if err != nil {
		t.Fatalf("EndpointsFromBytes: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1", len(eps))
	}
	if eps[0].PathTemplate != "/items" {
		t.Errorf("PathTemplate = %q; want /items", eps[0].PathTemplate)
	}
}

func TestEndpointsNonCallDecorator(t *testing.T) {
	src := readFixture(t, "non_call_decorator.py")
	e := New()
	eps, err := e.EndpointsFromBytes(context.Background(), "x.py", src, "svc", "")
	if err != nil {
		t.Fatalf("EndpointsFromBytes: %v", err)
	}
	if len(eps) != 0 {
		t.Errorf("eps len = %d; want 0 (non-call decorators must NOT emit)", len(eps))
	}
}

func TestEndpointsArtifactNonHTTPKeys(t *testing.T) {
	repo := readFixtureRepo(t, "repo_with_non_http_keys")
	src := readFixture(t, "repo_with_non_http_keys/app.py")
	e := New()
	eps, err := e.EndpointsFromBytes(context.Background(), "app.py", src, "svc", repo)
	if err != nil {
		t.Fatalf("EndpointsFromBytes: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1 (non-HTTP keys filtered)", len(eps))
	}
	if eps[0].HandlerNodeID != "listItems" {
		t.Errorf("HandlerNodeID = %q; want listItems", eps[0].HandlerNodeID)
	}
}

func TestEndpointsAsyncHandler(t *testing.T) {
	src := readFixture(t, "async_handler.py")
	e := New()
	eps, err := e.EndpointsFromBytes(context.Background(), "app.py", src, "svc", "")
	if err != nil {
		t.Fatalf("EndpointsFromBytes: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1", len(eps))
	}
	if eps[0].HandlerNodeID != "app.get_async" {
		t.Errorf("HandlerNodeID = %q; want app.get_async", eps[0].HandlerNodeID)
	}
}

func TestEndpointsMalformedPython(t *testing.T) {
	src := []byte(`from fastapi import FastAPI

app = FastAPI()

@app.get("/ok")
def ok(): return "ok"

# Malformed below — tree-sitter recovers
@app.post(((((
`)
	e := New()
	eps, err := e.EndpointsFromBytes(context.Background(), "x.py", src, "svc", "")
	if err != nil {
		t.Fatalf("EndpointsFromBytes: %v", err)
	}

	found := false
	for _, ep := range eps {
		if ep.PathTemplate == "/ok" && ep.Method == "GET" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected /ok endpoint to survive malformed-region degradation; got eps=%v", eps)
	}
}
