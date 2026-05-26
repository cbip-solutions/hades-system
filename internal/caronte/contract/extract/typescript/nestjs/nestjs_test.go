//go:build cgo
// +build cgo

package nestjs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/caronte/contract/extract"
	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

func TestExtractorRegistersOnInit(t *testing.T) {
	src := []byte(`import { Controller, Get } from '@nestjs/common';
@Controller('users')
export class UsersController {
  @Get(':id')
  findOne() {}
}
`)
	got := extract.Default().Resolve("users.controller.ts", src)
	found := false
	for _, e := range got {
		if e.Language() == extract.LangTypeScript {
			for _, fw := range e.Frameworks() {
				if fw == "nestjs" {
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatal("Default().Resolve(nestjs controller) returned no LangTypeScript/nestjs extractor; init() did not register")
	}
}

func TestDetectNestJS(t *testing.T) {
	e := New()
	cases := []struct {
		name, file, src string
		want            bool
	}{
		{
			"controller with Get decorator",
			"users.controller.ts",
			"import { Controller, Get } from '@nestjs/common';\n@Controller('users')\nclass C { @Get() find() {} }",
			true,
		},
		{
			"controller with Post decorator",
			"items.controller.ts",
			"import { Controller, Post } from '@nestjs/common';\n@Controller('items')\nclass C { @Post() create() {} }",
			true,
		},
		{
			"tsx variant",
			"weird.controller.tsx",
			"import { Controller, Get } from '@nestjs/common';\n@Controller('weird')\nclass C { @Get() f() {} }",
			true,
		},
		{
			"no nestjs import",
			"util.ts",
			"@Get() doSomething() {}",
			false,
		},
		{
			"nestjs import but no decorator",
			"empty.ts",
			"import { Module } from '@nestjs/common';\nexport class Foo {}",
			false,
		},
		{
			"go file",
			"app.go",
			"package main\n",
			false,
		},
		{
			"non-ts ext",
			"x.py",
			"@Controller('x')",
			false,
		},
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

type methodPath struct {
	method string
	path   string
}

func epSet(eps []store.APIEndpoint) map[methodPath]int {
	out := make(map[methodPath]int, len(eps))
	for _, ep := range eps {
		out[methodPath{ep.Method, ep.PathTemplate}]++
	}
	return out
}

func TestEndpointsAST_SimpleController(t *testing.T) {
	src := readFixture(t, "simple_controller.ts")
	e := New()
	eps, err := e.EndpointsFromBytes(context.Background(), "users.controller.ts", src, "api-svc", "")
	if err != nil {
		t.Fatalf("EndpointsFromBytes: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1", len(eps))
	}
	if eps[0].Method != "GET" {
		t.Errorf("Method = %q; want GET", eps[0].Method)
	}
	if eps[0].PathTemplate != "/users/{id}" {
		t.Errorf("PathTemplate = %q; want /users/{id}", eps[0].PathTemplate)
	}
	if eps[0].ExtractorID != ExtractorID {
		t.Errorf("ExtractorID = %q; want %q", eps[0].ExtractorID, ExtractorID)
	}
	if eps[0].HandlerNodeID == "" {
		t.Error("HandlerNodeID empty")
	}
}

func TestEndpointsMultiMethodController(t *testing.T) {
	src := readFixture(t, "multi_method_controller.ts")
	e := New()
	eps, err := e.EndpointsFromBytes(context.Background(), "items.controller.ts", src, "svc", "")
	if err != nil {
		t.Fatalf("EndpointsFromBytes: %v", err)
	}
	if len(eps) != 3 {
		t.Fatalf("eps len = %d; want 3", len(eps))
	}
	got := epSet(eps)
	want := map[methodPath]int{
		{"GET", "/items"}:         1,
		{"POST", "/items"}:        1,
		{"DELETE", "/items/{id}"}: 1,
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("missing %v: got=%v", k, got)
		}
	}
}

func TestEndpointsNestedPath(t *testing.T) {
	src := readFixture(t, "nested_path_controller.ts")
	e := New()
	eps, err := e.EndpointsFromBytes(context.Background(), "userposts.controller.ts", src, "svc", "")
	if err != nil {
		t.Fatalf("EndpointsFromBytes: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1", len(eps))
	}
	want := "/v1/users/{userId}/posts/{postId}/comments/{commentId}"
	if eps[0].PathTemplate != want {
		t.Errorf("PathTemplate = %q; want %s", eps[0].PathTemplate, want)
	}
}

func TestEndpointsApiOperation(t *testing.T) {
	src := readFixture(t, "with_api_operation.ts")
	e := New()
	eps, err := e.EndpointsFromBytes(context.Background(), "invoices.controller.ts", src, "svc", "")
	if err != nil {
		t.Fatalf("EndpointsFromBytes: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1", len(eps))
	}
	if eps[0].HandlerNodeID != "getInvoiceById" {
		t.Errorf("HandlerNodeID = %q; want getInvoiceById", eps[0].HandlerNodeID)
	}
}

// TestEndpointsApiTagsNotInHandlerNodeID is the sister-test for the I-2
// review fix: the implementation comment in endpointsFromAST claims
// "classTags is intentionally unused in this phase (Phase F wires the
// doc surface)". Pin that claim: a controller decorated with
// `@ApiTags('billing')` MUST NOT leak the tag value into HandlerNodeID.
// If a future refactor accidentally appends tags to the handler id, this
// gate catches it.
//
// Bite-check: temporarily append classTags to handler id in
// endpointsFromAST → TestEndpointsApiTagsNotInHandlerNodeID FAILS
// because "billing" appears in HandlerNodeID. Restore → PASS.
func TestEndpointsApiTagsNotInHandlerNodeID(t *testing.T) {
	src := readFixture(t, "api_tags_no_operation.ts")
	e := New()
	eps, err := e.EndpointsFromBytes(context.Background(), "reports.controller.ts", src, "svc", "")
	if err != nil {
		t.Fatalf("EndpointsFromBytes: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1", len(eps))
	}
	if eps[0].HandlerNodeID == "" {
		t.Fatal("HandlerNodeID empty; expected <module>.<class>.<method>")
	}
	// The tag value MUST NOT appear anywhere in HandlerNodeID — the I-2
	// contract is "classTags collected for Phase F, not surfaced here".
	if strings.Contains(eps[0].HandlerNodeID, "billing") {
		t.Errorf("HandlerNodeID = %q contains tag 'billing'; classTags must NOT leak into HandlerNodeID (I-2 contract)", eps[0].HandlerNodeID)
	}

	want := "reports.controller.ReportsController.summary"
	if eps[0].HandlerNodeID != want {
		t.Errorf("HandlerNodeID = %q; want %q", eps[0].HandlerNodeID, want)
	}
}

func TestEndpointsAllDecorator(t *testing.T) {
	src := readFixture(t, "catch_all_controller.ts")
	e := New()
	eps, err := e.EndpointsFromBytes(context.Background(), "proxy.controller.ts", src, "svc", "")
	if err != nil {
		t.Fatalf("EndpointsFromBytes: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1", len(eps))
	}
	if eps[0].Method != "*" {
		t.Errorf("Method = %q; want * (catch-all)", eps[0].Method)
	}
	if eps[0].PathTemplate != "/proxy" {
		t.Errorf("PathTemplate = %q; want /proxy", eps[0].PathTemplate)
	}
}

func TestEndpointsModuleWithControllers(t *testing.T) {
	src := readFixture(t, "module_with_controllers.ts")
	e := New()
	if e.Detect("app.module.ts", src) {
		t.Error("Detect should be false for @Module-only file")
	}
	eps, err := e.EndpointsFromBytes(context.Background(), "app.module.ts", src, "svc", "")
	if err != nil {
		t.Fatalf("EndpointsFromBytes: %v", err)
	}
	if len(eps) != 0 {
		t.Errorf("eps len = %d; want 0", len(eps))
	}
}

func TestEndpointsControllerAndModule(t *testing.T) {
	e := New()
	ctrlSrc := readFixture(t, "controller_and_module/users.controller.ts")
	modSrc := readFixture(t, "controller_and_module/users.module.ts")

	ctrlEps, err := e.EndpointsFromBytes(context.Background(), "users.controller.ts", ctrlSrc, "svc", "")
	if err != nil || len(ctrlEps) != 1 {
		t.Fatalf("controller endpoints = %d (err=%v); want 1", len(ctrlEps), err)
	}
	modEps, err := e.EndpointsFromBytes(context.Background(), "users.module.ts", modSrc, "svc", "")
	if err != nil || len(modEps) != 0 {
		t.Errorf("module endpoints = %d (err=%v); want 0", len(modEps), err)
	}
}

// TestExtractNonExportedController is the sister-test for the I-5 review
// fix: ALL 14 NestJS fixture controllers use `export class`, so the
// walker's double-emission guard (commit 07004d42) is only exercised
// against the export-wrapped shape. A regression that re-introduced
// double-emission by ALSO processing export_statement children would
// produce len(eps) == 2 for export-wrapped classes (caught by existing
// fixtures) BUT len(eps) == 1 for non-export classes (passes silently).
// This fixture + test covers the symmetric non-export side: a bare
// `class FooController { ... }` MUST emit exactly 1 endpoint and the
// walker MUST process it via the class_declaration path (not via any
// export-statement walker that would otherwise miss it entirely).
//
// Bite-check: temporarily restored the buggy walker that processed
// BOTH class_declaration AND export_statement (children → process the
// inner class_declaration again) — existing fixtures with `export class`
// emit 2 endpoints (existing tests fail with len == 2); this fixture
// without the export wrapper still emits 1 (this test PASSES the count
// assertion BUT the existing tests fail — combined surface bites
// symmetrically).
func TestExtractNonExportedController(t *testing.T) {
	src := readFixture(t, "non_exported_controller.ts")
	e := New()
	eps, err := e.EndpointsFromBytes(context.Background(), "private.controller.ts", src, "svc", "")
	if err != nil {
		t.Fatalf("EndpointsFromBytes: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1 (non-exported class must emit exactly once)", len(eps))
	}
	if eps[0].Method != "GET" {
		t.Errorf("Method = %q; want GET", eps[0].Method)
	}
	if eps[0].PathTemplate != "/private/hello" {
		t.Errorf("PathTemplate = %q; want /private/hello", eps[0].PathTemplate)
	}

	want := "private.controller.PrivateController.hello"
	if eps[0].HandlerNodeID != want {
		t.Errorf("HandlerNodeID = %q; want %q (non-export class still gets module+class+method composition)", eps[0].HandlerNodeID, want)
	}
}

func TestEndpointsArtifactSwaggerJSON(t *testing.T) {
	repo := readFixtureRepo(t, "repo_with_swagger_json")
	src := readFixture(t, "repo_with_swagger_json/app.module.ts")
	e := New()
	eps, err := e.EndpointsFromBytes(context.Background(), "app.module.ts", src, "svc", repo)
	if err != nil {
		t.Fatalf("EndpointsFromBytes: %v", err)
	}
	if len(eps) != 2 {
		t.Fatalf("eps len = %d; want 2 (artifact-derived)", len(eps))
	}
	for _, ep := range eps {
		if ep.ContractArtifact == "" {
			t.Errorf("ContractArtifact empty for %v", ep)
		}
	}

	handlers := map[string]bool{}
	for _, ep := range eps {
		handlers[ep.HandlerNodeID] = true
	}
	if !handlers["listUsers"] {
		t.Error("missing listUsers HandlerNodeID")
	}
	if !handlers["GET:/admin/health"] {
		t.Error("missing synthetic GET:/admin/health HandlerNodeID")
	}
}

func TestEndpointsArtifactSwaggerYAML(t *testing.T) {
	repo := readFixtureRepo(t, "repo_with_swagger_yaml")
	src := readFixture(t, "repo_with_swagger_yaml/app.module.ts")
	e := New()
	eps, err := e.EndpointsFromBytes(context.Background(), "app.module.ts", src, "svc", repo)
	if err != nil {
		t.Fatalf("EndpointsFromBytes: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1", len(eps))
	}
	if eps[0].HandlerNodeID != "listItems" {
		t.Errorf("HandlerNodeID = %q; want listItems", eps[0].HandlerNodeID)
	}
}

func TestEndpointsArtifactPlusAST(t *testing.T) {
	repo := readFixtureRepo(t, "repo_with_swagger_plus_ast")
	src := readFixture(t, "repo_with_swagger_plus_ast/users.controller.ts")
	e := New()
	eps, err := e.EndpointsFromBytes(context.Background(), "users.controller.ts", src, "svc", repo)
	if err != nil {
		t.Fatalf("EndpointsFromBytes: %v", err)
	}

	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1 (artifact-preferred)", len(eps))
	}
	if eps[0].ContractArtifact == "" {
		t.Errorf("ContractArtifact empty")
	}
}

func TestEndpointsBrokenSwaggerFallback(t *testing.T) {
	repo := readFixtureRepo(t, "repo_with_broken_swagger")
	src := readFixture(t, "repo_with_broken_swagger/users.controller.ts")
	e := New()
	eps, err := e.EndpointsFromBytes(context.Background(), "users.controller.ts", src, "svc", repo)
	if err != nil {
		t.Fatalf("EndpointsFromBytes: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1 (AST fallback)", len(eps))
	}
	if eps[0].ContractArtifact != "" {
		t.Errorf("ContractArtifact = %q; want empty (AST path)", eps[0].ContractArtifact)
	}
}

func TestEndpointsEmptyFile(t *testing.T) {
	src := readFixture(t, "empty.ts")
	e := New()
	if e.Detect("empty.ts", src) {
		t.Error("Detect should be false")
	}
	eps, err := e.EndpointsFromBytes(context.Background(), "empty.ts", src, "svc", "")
	if err != nil {
		t.Fatalf("EndpointsFromBytes: %v", err)
	}
	if len(eps) != 0 {
		t.Errorf("eps len = %d; want 0", len(eps))
	}
}

func TestEndpointsOnlyApiTags(t *testing.T) {
	src := readFixture(t, "only_api_tags.ts")
	e := New()
	if e.Detect("only.ts", src) {
		t.Error("Detect should be false (no @nestjs/common + no @Controller decorator)")
	}
}

func TestEndpointsTSXController(t *testing.T) {
	src := readFixture(t, "tsx_controller.controller.tsx")
	e := New()
	eps, err := e.EndpointsFromBytes(context.Background(), "x.controller.tsx", src, "svc", "")
	if err != nil {
		t.Fatalf("EndpointsFromBytes: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1", len(eps))
	}
	if eps[0].PathTemplate != "/jsx" {
		t.Errorf("PathTemplate = %q; want /jsx", eps[0].PathTemplate)
	}
}

func TestInterfaceShims(t *testing.T) {
	e := New()
	if eps, err := e.Endpoints(nil, "x.ts"); err != nil || eps != nil {
		t.Errorf("Endpoints returned (%v, %v); want (nil, nil)", eps, err)
	}
	if calls, err := e.Calls(nil, "x.ts"); err != nil || calls != nil {
		t.Errorf("Calls returned (%v, %v); want (nil, nil)", calls, err)
	}
	if stubs := e.StubArtifacts("x.ts", []byte("anything")); len(stubs) != 0 {
		t.Errorf("StubArtifacts returned %v; want empty (NestJS has no gRPC; registry contract: empty slice not nil)", stubs)
	}
}

func TestCanonicalisePath(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"/users/:id", "/users/{id}"},
		{"/v1/users/:userId/posts/:postId", "/v1/users/{userId}/posts/{postId}"},
		{"/proxy/", "/proxy"},
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

func TestTSNodeID(t *testing.T) {
	cases := []struct {
		file, owner, name, want string
	}{
		{"users.controller.ts", "UsersController", "findOne", "users.controller.UsersController.findOne"},
		{"pkg/svc.ts", "C", "f", "pkg.svc.C.f"},
		{"no_ext", "Owner", "x", "no_ext.Owner.x"},
	}
	for _, c := range cases {
		if got := tsNodeID(c.file, c.owner, c.name); got != c.want {
			t.Errorf("tsNodeID(%q,%q,%q) = %q; want %q", c.file, c.owner, c.name, got, c.want)
		}
	}
}

func TestArtifactToEndpointsEmpty(t *testing.T) {
	doc := &swaggerDoc{}
	got := artifactToEndpoints(doc, "/tmp/swagger.json", "svc")
	if got == nil {
		t.Error("artifactToEndpoints({}) returned nil; want empty non-nil slice")
	}
	if len(got) != 0 {
		t.Errorf("len = %d; want 0", len(got))
	}
}

func TestDecodeSwaggerUnsupportedExt(t *testing.T) {
	if _, err := decodeSwagger([]byte("{}"), ".toml"); err == nil {
		t.Error("decodeSwagger(.toml) returned nil err; want unsupported-extension")
	}
}
