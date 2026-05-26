//go:build integration

package grpcstub

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/caronte/contract/extract"
	"github.com/cbip-solutions/hades-system/internal/caronte/contract/extract/proto"
	"github.com/cbip-solutions/hades-system/internal/caronte/contract/link"
	caronte_store "github.com/cbip-solutions/hades-system/internal/caronte/store"
)

func TestGRPCStubImportLinksAtExactProtoImport(t *testing.T) {
	disableKeychain(t)
	fixturesDir := requireFixtures(t)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	protoExtractor := proto.New()

	if got := protoExtractor.Language(); got != extract.LangProto {
		t.Errorf("proto extractor Language = %q; want %q", got, extract.LangProto)
	}
	if got := protoExtractor.Frameworks(); len(got) != 1 || got[0] != "proto" {
		t.Errorf("proto extractor Frameworks = %+v; want [proto]", got)
	}

	protoPath := filepath.Join(fixturesDir, "server_repo", "proto", "greeter.proto")
	protoSrc, err := os.ReadFile(protoPath)
	if err != nil {
		t.Fatalf("read .proto: %v", err)
	}
	const serverRepo = "server_repo"
	endpoints, err := protoExtractor.EndpointsFromBytes(ctx, protoPath, protoSrc, serverRepo)
	if err != nil {
		t.Fatalf("proto.EndpointsFromBytes: %v", err)
	}
	var helloEndpoint, hasHelloMatch = findGreeterHello(endpoints)
	if !hasHelloMatch {
		t.Fatalf("api_endpoints missing greeter.v1.Greeter/Hello; got %+v", endpoints)
	}
	if helloEndpoint.ExtractorID == "" {
		t.Errorf("Hello endpoint ExtractorID empty; expected populated (per Phase D C-4 contract)")
	}
	if helloEndpoint.HandlerNodeID != "greeter.v1.Greeter.Hello" {
		t.Errorf("HandlerNodeID = %q; want greeter.v1.Greeter.Hello (canonical fully-qualified per composeHandlerNodeID)", helloEndpoint.HandlerNodeID)
	}

	stubPath := filepath.Join(fixturesDir, "client_repo", "stub", "greeter_grpc.pb.go")
	stubSrc, err := os.ReadFile(stubPath)
	if err != nil {
		t.Fatalf("read stub: %v", err)
	}
	stubs := protoExtractor.StubArtifacts(stubPath, stubSrc)
	if len(stubs) == 0 {
		t.Fatalf("proto.StubArtifacts returned 0 references for hand-written stub; expected >=1 StubReference{ProtoPackage:greeter.v1, ServiceName:Greeter, RpcName:Hello}")
	}
	var matched bool
	var matchedStub extract.StubReference
	for _, s := range stubs {

		if s.ProtoPackage == "greeter.v1" && s.ServiceName == "Greeter" && s.RpcName == "Hello" {
			matched = true
			matchedStub = s
			break
		}
	}
	if !matched {
		t.Fatalf("StubArtifacts did not return the expected StubReference{greeter.v1, Greeter, Hello}; got %+v", stubs)
	}

	wantKey := "greeter.v1.Greeter/Hello"
	if got := link.GRPCKey(matchedStub.ProtoPackage, matchedStub.ServiceName, matchedStub.RpcName); got != wantKey {
		t.Errorf("GRPCKey(%q,%q,%q) = %q; want %q (the canonical proto fully-qualified form linker.tryProtoArtifact matches on)",
			matchedStub.ProtoPackage, matchedStub.ServiceName, matchedStub.RpcName, got, wantKey)
	}

	if link.LinkArtifact != "artifact" {
		t.Errorf("link.LinkArtifact = %q; want artifact (C-5 frozen)", link.LinkArtifact)
	}
	if link.ConfExactProtoImport != "exact_proto_import" {
		t.Errorf("link.ConfExactProtoImport = %q; want exact_proto_import (C-5 frozen)", link.ConfExactProtoImport)
	}

	plainGoSrc := []byte("package foo\n\nfunc Bar() {}\n")
	if got := protoExtractor.StubArtifacts("internal/foo/bar.go", plainGoSrc); got != nil {
		t.Errorf("StubArtifacts(non-stub .go) = %+v; want nil", got)
	}
}

func findGreeterHello(endpoints []caronte_store.APIEndpoint) (caronte_store.APIEndpoint, bool) {
	for _, ep := range endpoints {
		if ep.Kind == "grpc" && ep.ProtoService == "Greeter" && ep.ProtoRPC == "Hello" {
			return ep, true
		}
	}
	return caronte_store.APIEndpoint{}, false
}
