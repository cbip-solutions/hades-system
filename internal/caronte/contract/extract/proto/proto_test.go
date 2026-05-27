// go:build cgo
//go:build cgo
// +build cgo

package proto

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/caronte/contract/extract"
)

func TestPackageScaffolded(t *testing.T) {
	_ = t
}

func TestExtractorRegistersOnInit(t *testing.T) {
	got := extract.Default().Resolve("any.proto", []byte("syntax = \"proto3\";\npackage x;\n"))
	found := false
	for _, e := range got {
		if e.Language() == extract.LangProto {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("Default().Resolve(*.proto) returned no LangProto extractor; init() did not register")
	}
}

func TestDetectProto(t *testing.T) {
	e := New()
	if !e.Detect("foo/bar.proto", []byte("syntax = \"proto3\";\n")) {
		t.Error("Detect(*.proto) = false; want true")
	}
	if e.Detect("foo/bar.go", []byte("syntax = \"proto3\";\n")) {
		t.Error("Detect(*.go) = true; want false (extension gate)")
	}
	if e.Detect("README.md", []byte("# docs\n")) {
		t.Error("Detect(.md) = true; want false (extension gate)")
	}
}

func TestEndpointsSimpleService(t *testing.T) {
	src := readFixture(t, "service_simple.proto")
	e := New()
	eps, err := e.EndpointsFromBytes(context.Background(), "users.proto", src, "auth-svc")
	if err != nil {
		t.Fatalf("EndpointsFromBytes: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1 (one rpc, no http annotation); got %+v", len(eps), eps)
	}
	if eps[0].Kind != "grpc" {
		t.Errorf("Kind = %q; want grpc", eps[0].Kind)
	}
	if eps[0].ProtoService != "UserService" {
		t.Errorf("ProtoService = %q; want UserService", eps[0].ProtoService)
	}
	if eps[0].ProtoRPC != "GetUser" {
		t.Errorf("ProtoRPC = %q; want GetUser", eps[0].ProtoRPC)
	}
	if eps[0].Repo != "auth-svc" {
		t.Errorf("Repo = %q; want auth-svc", eps[0].Repo)
	}
	if eps[0].ExtractorID != "proto-v1" {
		t.Errorf("ExtractorID = %q; want proto-v1", eps[0].ExtractorID)
	}
	if eps[0].HandlerNodeID != "auth.v1.UserService.GetUser" {
		t.Errorf("HandlerNodeID = %q; want auth.v1.UserService.GetUser", eps[0].HandlerNodeID)
	}
}

func TestEndpointsHttpTranscoding(t *testing.T) {
	src := readFixture(t, "service_with_http.proto")
	e := New()
	eps, err := e.EndpointsFromBytes(context.Background(), "users.proto", src, "auth-svc")
	if err != nil {
		t.Fatalf("EndpointsFromBytes: %v", err)
	}
	if len(eps) != 2 {
		t.Fatalf("eps len = %d; want 2 (grpc + http sibling); got %+v", len(eps), eps)
	}
	var grpcIdx, httpIdx = -1, -1
	for i := range eps {
		switch eps[i].Kind {
		case "grpc":
			grpcIdx = i
		case "http":
			httpIdx = i
		}
	}
	if grpcIdx < 0 || httpIdx < 0 {
		t.Fatalf("missing kind; got %+v", eps)
	}
	if eps[grpcIdx].HandlerNodeID != eps[httpIdx].HandlerNodeID {
		t.Errorf("sibling HandlerNodeID mismatch: grpc=%q http=%q",
			eps[grpcIdx].HandlerNodeID, eps[httpIdx].HandlerNodeID)
	}
	if eps[httpIdx].Method != "GET" {
		t.Errorf("http.Method = %q; want GET", eps[httpIdx].Method)
	}
	if eps[httpIdx].PathTemplate != "/v1/users/{param}" {
		t.Errorf("http.PathTemplate = %q; want /v1/users/{param}", eps[httpIdx].PathTemplate)
	}
}

// TestStubArtifactsGoGrpcPb asserts a *_grpc.pb.go file yields one
// StubReference per server-interface method. The StubReference is the
// highest-confidence link tier in §6 — the linker matches (ProtoPackage,
// ServiceName, RpcName) to a target-repo APIEndpoint exactly.
//
// The fixture (stub_go_grpc.pb.go.fixture) defines UserServiceServer (3 rpcs:
// GetUser, UpdateUser, DeleteUser) + AdminServiceServer (2 rpcs: Promote,
// Revoke), so the expected exact set is 5 rpcs. The `mustEmbedUnimplemented*`
// method on UserServiceServer is a forward-compatibility stub that protoc-gen-
// go-grpc emits inside every server interface — it is NOT a real rpc and
// MUST be filtered out by the regex / unexported-name check (Go convention:
// rpcs are exported / PascalCase). Without that filter the phantom
// `mustEmbedUnimplementedUserServiceServer` row would appear and pollute the
// linker's StubReference set with a non-existent endpoint.
func TestStubArtifactsGoGrpcPb(t *testing.T) {
	src := readFixture(t, "stub_go_grpc.pb.go.fixture")
	e := New()
	refs := e.StubArtifacts("auth/v1/auth_grpc.pb.go", src)
	got := map[string]bool{}
	for _, r := range refs {
		if r.ProtoPackage == "" || r.ServiceName == "" || r.RpcName == "" {
			t.Errorf("empty field in %+v", r)
		}
		got[r.RpcName] = true
	}
	want := map[string]bool{
		"GetUser":    true,
		"UpdateUser": true,
		"DeleteUser": true,
		"Promote":    true,
		"Revoke":     true,
	}
	if len(refs) != len(want) {
		t.Errorf("len(refs) = %d; want %d; got %+v", len(refs), len(want), refs)
	}
	for name := range want {
		if !got[name] {
			t.Errorf("missing rpc %q from extracted set: %v", name, got)
		}
	}
	for name := range got {
		if !want[name] {
			t.Errorf("phantom rpc %q in extracted set; want only %v", name, keys(want))
		}
	}
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestStubArtifactsPythonPb2Grpc(t *testing.T) {
	src := readFixture(t, "stub_python_pb2_grpc.py.fixture")
	e := New()
	refs := e.StubArtifacts("auth/v1/auth_pb2_grpc.py", src)
	if len(refs) < 1 {
		t.Fatalf("StubArtifacts py: len = %d; want >=1", len(refs))
	}
	for _, r := range refs {
		if r.ProtoPackage == "" || r.ServiceName == "" || r.RpcName == "" {
			t.Errorf("empty field in %+v", r)
		}
	}
}

func TestStubArtifactsJSGrpcWebPb(t *testing.T) {
	src := readFixture(t, "stub_jsweb_grpc_web_pb.js.fixture")
	e := New()
	refs := e.StubArtifacts("auth/v1/auth_grpc_web_pb.js", src)
	if len(refs) < 1 {
		t.Fatalf("StubArtifacts js: len = %d; want >=1", len(refs))
	}
	for _, r := range refs {
		if r.ProtoPackage == "" || r.ServiceName == "" || r.RpcName == "" {
			t.Errorf("empty field in %+v", r)
		}
	}
}

func TestStubArtifactsNonStubReturnsNil(t *testing.T) {
	e := New()
	if got := e.StubArtifacts("internal/foo/bar.go", []byte("package foo\n")); got != nil {
		t.Errorf("non-stub .go: got %v; want nil", got)
	}
	if got := e.StubArtifacts("server/main.py", []byte("def main(): pass\n")); got != nil {
		t.Errorf("non-stub .py: got %v; want nil", got)
	}
}

func TestEndpointsMultiRpc(t *testing.T) {
	src := readFixture(t, "service_multi_rpc.proto")
	e := New()
	eps, err := e.EndpointsFromBytes(context.Background(), "x.proto", src, "x")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 6 {
		t.Fatalf("eps len = %d; want 6; got %+v", len(eps), eps)
	}
	for _, ep := range eps {
		if ep.ProtoService != "AuthService" {
			t.Errorf("ProtoService = %q; want AuthService", ep.ProtoService)
		}
	}
}

func TestEndpointsEmptyService(t *testing.T) {
	src := readFixture(t, "service_empty.proto")
	e := New()
	eps, err := e.EndpointsFromBytes(context.Background(), "x.proto", src, "x")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 0 {
		t.Errorf("eps len = %d; want 0; got %+v", len(eps), eps)
	}
}

func TestEndpointsCommentsOnly(t *testing.T) {
	src := readFixture(t, "service_comments_only.proto")
	e := New()
	eps, err := e.EndpointsFromBytes(context.Background(), "x.proto", src, "x")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 0 {
		t.Errorf("eps len = %d; want 0; got %+v", len(eps), eps)
	}
}

func TestEndpointsNestedPackage(t *testing.T) {
	src := readFixture(t, "service_nested_pkg.proto")
	e := New()
	eps, err := e.EndpointsFromBytes(context.Background(), "x.proto", src, "deep-svc")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1; got %+v", len(eps), eps)
	}
	want := "com.example.deeply.nested.auth.v1.UserService.GetUser"
	if eps[0].HandlerNodeID != want {
		t.Errorf("HandlerNodeID = %q; want %q", eps[0].HandlerNodeID, want)
	}
}

func TestEndpointsStreaming(t *testing.T) {
	src := readFixture(t, "service_streaming.proto")
	e := New()
	eps, err := e.EndpointsFromBytes(context.Background(), "x.proto", src, "x")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) == 0 {
		t.Fatal("streaming rpc dropped; want extracted as ordinary endpoint")
	}
}

func TestEndpointsImports(t *testing.T) {
	src := readFixture(t, "service_imports.proto")
	e := New()
	eps, err := e.EndpointsFromBytes(context.Background(), "x.proto", src, "x")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1; got %+v", len(eps), eps)
	}
}

func TestEndpointsHttpPatternVariations(t *testing.T) {
	src := readFixture(t, "service_http_pattern_variations.proto")
	e := New()
	eps, err := e.EndpointsFromBytes(context.Background(), "x.proto", src, "x")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	wantTotal := 11
	if len(eps) != wantTotal {
		t.Fatalf("eps len = %d; want %d; got %+v", len(eps), wantTotal, eps)
	}
	seenMethods := map[string]bool{}
	for _, ep := range eps {
		if ep.Kind == "http" {
			seenMethods[ep.Method] = true
		}
	}
	for _, m := range []string{"GET", "POST", "PUT", "DELETE", "PATCH"} {
		if !seenMethods[m] {
			t.Errorf("HTTP method %q missing from sibling rows: %v", m, seenMethods)
		}
	}
}

func TestEndpointsContractArtifactPopulated(t *testing.T) {
	src := readFixture(t, "service_simple.proto")
	e := New()
	eps, err := e.EndpointsFromBytes(context.Background(), "users.proto", src, "auth-svc")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1", len(eps))
	}
	if eps[0].ContractArtifact != "users.proto" {
		t.Errorf("ContractArtifact = %q; want users.proto", eps[0].ContractArtifact)
	}
}

func TestEndpointsExtractedAtPopulated(t *testing.T) {
	src := readFixture(t, "service_simple.proto")
	e := New()
	eps, err := e.EndpointsFromBytes(context.Background(), "users.proto", src, "auth-svc")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if eps[0].ExtractedAt == 0 {
		t.Error("ExtractedAt = 0; want a unix-seconds timestamp")
	}
}

func TestEndpointsViaC4Interface(t *testing.T) {
	src := readFixture(t, "service_simple.proto")
	e := New()
	tree, err := e.parseTree(context.Background(), src)
	if err != nil {
		t.Fatalf("parseTree: %v", err)
	}
	defer tree.Close()
	eps, err := e.Endpoints(tree, "users.proto")
	if err != nil {
		t.Fatalf("Endpoints: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("eps len = %d; want 1", len(eps))
	}
	if eps[0].ProtoService != "UserService" {
		t.Errorf("ProtoService = %q; want UserService", eps[0].ProtoService)
	}
}

func TestCallsAlwaysEmpty(t *testing.T) {
	src := readFixture(t, "service_simple.proto")
	e := New()
	tree, err := e.parseTree(context.Background(), src)
	if err != nil {
		t.Fatalf("parseTree: %v", err)
	}
	defer tree.Close()
	calls, err := e.Calls(tree, "users.proto")
	if err != nil {
		t.Errorf("Calls returned err: %v", err)
	}
	if len(calls) != 0 {
		t.Errorf("Calls returned %d rows; want 0 (server-side schema)", len(calls))
	}
}

func TestFrameworksLabel(t *testing.T) {
	e := New()
	got := e.Frameworks()
	if !reflect.DeepEqual(got, []string{"proto"}) {
		t.Errorf("Frameworks() = %v; want [proto]", got)
	}
}

func TestLanguageLabel(t *testing.T) {
	e := New()
	if e.Language() != extract.LangProto {
		t.Errorf("Language() = %q; want %q", e.Language(), extract.LangProto)
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
