// go:build cgo
package link

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/caronte/contract/extract"
	"github.com/cbip-solutions/hades-system/internal/caronte/contract/yaml"
	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

func TestSameHTTPMethodCaseFold(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"GET", "get", true},
		{"PoSt", "POST", true},
		{"GET", "POST", false},
		{"GETT", "GET", false},
		{"", "", true},
		{"DELETE", "delete", true},
	}
	for _, c := range cases {
		if got := sameHTTPMethod(c.a, c.b); got != c.want {
			t.Errorf("sameHTTPMethod(%q,%q) = %v; want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestConfidenceForMethodFullCoverage(t *testing.T) {
	if _, err := confidenceForMethod(LinkArtifact, "unknown_source"); err == nil {
		t.Errorf("artifact+unknown_source = nil; want ErrConfidenceTierDowngrade")
	}
	if _, err := confidenceForMethod(LinkMethod("invented"), ""); err == nil {
		t.Errorf("unknown method = nil; want ErrConfidenceTierDowngrade")
	}

}

func TestLinkProjectOpenStoreError(t *testing.T) {
	deps := &fakeDeps{
		stores:  map[string]*fakeProjectStore{},
		openErr: errors.New("disk full"),
	}
	l := NewLinker(&fakeWorkspace{}, &fakeUnresolvedStore{}, &fakeAuditEmitter{}, nil, nil, "ws-1", deps)
	_, err := l.LinkProject(context.Background(), "p1", "client-app")
	if err == nil {
		t.Errorf("LinkProject(open-fail) = nil; want error")
	}
}

func TestLinkProjectNilDeps(t *testing.T) {
	l := &Linker{workspaceID: "ws-1"}
	_, err := l.LinkProject(context.Background(), "p1", "r")
	if err == nil {
		t.Errorf("LinkProject(nil deps) = nil; want error")
	}
}

func TestTryProtoArtifactNoStubsReturnsNil(t *testing.T) {
	l := &Linker{extractStubs: map[string][]extract.StubReference{}}
	hit := l.tryProtoArtifact(context.Background(), "client-app", store.APICall{TargetProto: "x/y"})
	if hit != nil {
		t.Errorf("hit = %+v; want nil (no stubs)", hit)
	}
}

func TestTryProtoArtifactStubMissesEndpoint(t *testing.T) {
	srcStubs := map[string][]extract.StubReference{
		"client-app": {{Repo: "order-svc", ProtoPackage: "acme.v1", ServiceName: "OrderService", RpcName: "PlaceOrder"}},
	}
	deps := &fakeDeps{
		stores: map[string]*fakeProjectStore{
			"client-app": {},
			"order-svc":  {endpoints: nil},
		},
	}
	l := &Linker{deps: deps, extractStubs: srcStubs}
	hit := l.tryProtoArtifact(context.Background(), "client-app",
		store.APICall{TargetProto: "acme.v1.OrderService/PlaceOrder"})
	if hit != nil {
		t.Errorf("hit = %+v; want nil (stub key matched but endpoint missing)", hit)
	}
}

func TestLinkProjectSpecArtifactTier(t *testing.T) {
	srcStore := &fakeProjectStore{
		calls: []store.APICall{{
			CallID: "c-spec", Repo: "client-app", CallerNodeID: "n1",
			TargetMethod: "GET", TargetPathTemplate: "/users/{id}",
			BaseURLRef: "AUTH_SVC_URL", Confidence: "spec_artifact",
			ExtractedAt: 1, ExtractorID: "gohttp",
		}},
	}
	tgtStore := &fakeProjectStore{
		endpoints: []store.APIEndpoint{{
			EndpointID: "ep-getuser", Repo: "auth-svc",
			Kind: "http", Method: "GET", PathTemplate: "/users/{id}",
			ContractArtifact: "openapi/users.yaml",
			HandlerNodeID:    "h1",
			ExtractedAt:      1, ExtractorID: "gohttp",
		}},
	}
	deps := &fakeDeps{
		stores: map[string]*fakeProjectStore{"client-app": srcStore, "auth-svc": tgtStore},
	}
	ws := &fakeWorkspace{}
	audit := &fakeAuditEmitter{}
	us := &fakeUnresolvedStore{}
	manifests := map[string]*yaml.Manifest{
		"client-app": {
			SchemaVersion:    1,
			Services:         []yaml.Service{{BaseURLEnv: "AUTH_SVC_URL", TargetRepo: "auth-svc"}},
			UnresolvedPolicy: yaml.PolicySurface,
		},
	}
	linker := NewLinker(ws, us, audit, manifests, nil, "ws-1", deps)
	res, err := linker.LinkProject(context.Background(), "p1", "client-app")
	if err != nil {
		t.Fatalf("LinkProject: %v", err)
	}
	if res.LinksPersisted != 1 {
		t.Fatalf("LinksPersisted = %d; want 1 (spec_artifact tier hit)", res.LinksPersisted)
	}
	if got := res.TierCounts[ConfSpecArtifact]; got != 1 {
		t.Errorf("TierCounts[spec_artifact] = %d; want 1", got)
	}
	if got := res.TierCounts[ConfStaticPath]; got != 0 {
		t.Errorf("TierCounts[static_path] = %d; want 0 (spec_artifact tier wins over static)", got)
	}
	if len(ws.persisted) != 1 {
		t.Fatalf("ws.persisted = %d; want 1", len(ws.persisted))
	}
	got := ws.persisted[0]
	if got.LinkMethod != "artifact" || got.Confidence != "spec_artifact" {
		t.Errorf("link drift: method=%s conf=%s; want artifact/spec_artifact", got.LinkMethod, got.Confidence)
	}
}

func TestLinkProjectPolicyFailPropagatesError(t *testing.T) {
	srcStore := &fakeProjectStore{
		calls: []store.APICall{{
			CallID: "c-fail", Repo: "client-app", CallerNodeID: "n1",
			TargetMethod: "GET", TargetPathTemplate: "/users/{id}",
			BaseURLRef: "UNKNOWN_URL", Confidence: "static_path",
			ExtractedAt: 1, ExtractorID: "gohttp",
		}},
	}
	deps := &fakeDeps{stores: map[string]*fakeProjectStore{"client-app": srcStore}}
	ws := &fakeWorkspace{}
	audit := &fakeAuditEmitter{}
	us := &fakeUnresolvedStore{}
	manifests := map[string]*yaml.Manifest{
		"client-app": {
			SchemaVersion:    1,
			Services:         []yaml.Service{{BaseURLEnv: "KNOWN_URL", TargetRepo: "auth-svc"}},
			UnresolvedPolicy: yaml.PolicyFail,
		},
	}
	linker := NewLinker(ws, us, audit, manifests, nil, "ws-1", deps)
	res, err := linker.LinkProject(context.Background(), "p1", "client-app")
	if err != nil {
		t.Fatalf("LinkProject (top-level): %v", err)
	}
	if res.LinksPersisted != 0 {
		t.Errorf("LinksPersisted = %d; want 0 (PolicyFail)", res.LinksPersisted)
	}
	if len(res.Errors) != 1 {
		t.Fatalf("res.Errors = %v; want 1 wrapped error from PolicyFail", res.Errors)
	}
	if !errors.Is(res.Errors[0], ErrNoManifestEntry) {
		t.Errorf("res.Errors[0] = %v; want wraps ErrNoManifestEntry", res.Errors[0])
	}
	if !strings.Contains(res.Errors[0].Error(), "c-fail") {
		t.Errorf("res.Errors[0] = %v; want contains call_id (c-fail)", res.Errors[0])
	}

	if res.UnresolvedRows != 1 {
		t.Errorf("res.UnresolvedRows = %d; want 1 (counter increments unconditionally)", res.UnresolvedRows)
	}
	if len(us.inserted) != 0 {
		t.Errorf("us.inserted = %d; want 0 (PolicyFail returns before Insert)", len(us.inserted))
	}
}

func TestLinkProjectPolicySilentIncrementsCounter(t *testing.T) {
	srcStore := &fakeProjectStore{
		calls: []store.APICall{{
			CallID: "c-silent", Repo: "client-app", CallerNodeID: "n1",
			TargetMethod: "GET", TargetPathTemplate: "/users/{id}",
			BaseURLRef: "UNKNOWN_URL", Confidence: "static_path",
			ExtractedAt: 1, ExtractorID: "gohttp",
		}},
	}
	deps := &fakeDeps{stores: map[string]*fakeProjectStore{"client-app": srcStore}}
	ws := &fakeWorkspace{}
	audit := &fakeAuditEmitter{}
	us := &fakeUnresolvedStore{}
	manifests := map[string]*yaml.Manifest{
		"client-app": {
			SchemaVersion:    1,
			Services:         []yaml.Service{{BaseURLEnv: "KNOWN_URL", TargetRepo: "auth-svc"}},
			UnresolvedPolicy: yaml.PolicySilent,
		},
	}
	linker := NewLinker(ws, us, audit, manifests, nil, "ws-1", deps)
	res, err := linker.LinkProject(context.Background(), "p1", "client-app")
	if err != nil {
		t.Fatalf("LinkProject: %v", err)
	}
	if res.LinksPersisted != 0 {
		t.Errorf("LinksPersisted = %d; want 0 (PolicySilent)", res.LinksPersisted)
	}
	if res.SilentDrops != 1 {
		t.Errorf("res.SilentDrops = %d; want 1 (PolicySilent increments orchestrator counter)", res.SilentDrops)
	}
	if len(res.Errors) != 0 {
		t.Errorf("res.Errors = %v; want empty (PolicySilent drops quietly, no error)", res.Errors)
	}
	if len(us.inserted) != 0 {
		t.Errorf("us.inserted = %d; want 0 (PolicySilent never inserts)", len(us.inserted))
	}
	if len(audit.events) != 0 {
		t.Errorf("audit.events = %d; want 0 (PolicySilent never audits)", len(audit.events))
	}

	if res.UnresolvedRows != 1 {
		t.Errorf("res.UnresolvedRows = %d; want 1 (counter increments unconditionally)", res.UnresolvedRows)
	}
}

func TestTrySpecArtifactPathsNonHTTPSkipped(t *testing.T) {
	srcStore := &fakeProjectStore{}
	tgtStore := &fakeProjectStore{
		endpoints: []store.APIEndpoint{

			{EndpointID: "ep-1", Repo: "auth-svc", Kind: "http", Method: "GET", PathTemplate: "/users/{id}", ContractArtifact: "", HandlerNodeID: "h1", ExtractedAt: 1, ExtractorID: "x"},

			{EndpointID: "ep-2", Repo: "auth-svc", Kind: "grpc", ContractArtifact: "x.openapi", HandlerNodeID: "h2", ExtractedAt: 1, ExtractorID: "x"},
		},
	}
	deps := &fakeDeps{stores: map[string]*fakeProjectStore{"client-app": srcStore, "auth-svc": tgtStore}}
	manifest := &yaml.Manifest{
		SchemaVersion:    1,
		Services:         []yaml.Service{{BaseURLEnv: "AUTH_URL", TargetRepo: "auth-svc"}},
		UnresolvedPolicy: yaml.PolicySurface,
	}
	l := &Linker{deps: deps, manifests: map[string]*yaml.Manifest{"client-app": manifest}}
	hit := l.trySpecArtifact(context.Background(), "client-app", store.APICall{
		TargetMethod: "GET", TargetPathTemplate: "/users/{id}", BaseURLRef: "AUTH_URL",
	}, manifest)
	if hit != nil {
		t.Errorf("hit = %+v; want nil (no spec artifact present)", hit)
	}
}

func TestListEndpointsDepsNilReturnsEmpty(t *testing.T) {
	l := &Linker{}
	eps := l.listEndpoints(context.Background(), "any")
	if eps != nil {
		t.Errorf("eps = %v; want nil (nil deps)", eps)
	}
}

func TestListEndpointsOpenErrorReturnsNil(t *testing.T) {
	deps := &fakeDeps{openErr: errors.New("down")}
	l := &Linker{deps: deps}
	if eps := l.listEndpoints(context.Background(), "x"); eps != nil {
		t.Errorf("eps = %v; want nil (open error)", eps)
	}
}

func TestTryStaticPathNonHTTPSkipped(t *testing.T) {
	deps := &fakeDeps{stores: map[string]*fakeProjectStore{"auth-svc": {endpoints: []store.APIEndpoint{
		{EndpointID: "g", Repo: "auth-svc", Kind: "grpc", HandlerNodeID: "h", ExtractedAt: 1, ExtractorID: "x"},
	}}}}
	l := &Linker{deps: deps}
	hit := l.tryStaticPath(context.Background(), "client-app", "auth-svc", store.APICall{
		TargetMethod: "GET", TargetPathTemplate: "/x",
	})
	if hit != nil {
		t.Errorf("hit = %+v; want nil (non-http skipped)", hit)
	}
}

func TestTryStaticPathMethodMismatchSkipped(t *testing.T) {
	deps := &fakeDeps{stores: map[string]*fakeProjectStore{"auth-svc": {endpoints: []store.APIEndpoint{
		{EndpointID: "ep", Repo: "auth-svc", Kind: "http", Method: "POST", PathTemplate: "/x", HandlerNodeID: "h", ExtractedAt: 1, ExtractorID: "x"},
	}}}}
	l := &Linker{deps: deps}
	hit := l.tryStaticPath(context.Background(), "client-app", "auth-svc", store.APICall{
		TargetMethod: "GET", TargetPathTemplate: "/x",
	})
	if hit != nil {
		t.Errorf("hit = %+v; want nil (method mismatch)", hit)
	}
}

func TestTryStaticPathPathLiteralMismatchSkipped(t *testing.T) {
	deps := &fakeDeps{stores: map[string]*fakeProjectStore{"auth-svc": {endpoints: []store.APIEndpoint{
		{EndpointID: "ep", Repo: "auth-svc", Kind: "http", Method: "GET", PathTemplate: "/users/{user_id}", HandlerNodeID: "h", ExtractedAt: 1, ExtractorID: "x"},
	}}}}
	l := &Linker{deps: deps}
	hit := l.tryStaticPath(context.Background(), "client-app", "auth-svc", store.APICall{
		TargetMethod: "GET", TargetPathTemplate: "/users/{id}",
	})
	if hit != nil {
		t.Errorf("hit = %+v; want nil (parameter-name mismatch in static tier)", hit)
	}
}

func TestTryFuzzyPathNonHTTPSkipped(t *testing.T) {
	deps := &fakeDeps{stores: map[string]*fakeProjectStore{"auth-svc": {endpoints: []store.APIEndpoint{
		{EndpointID: "g", Repo: "auth-svc", Kind: "grpc", HandlerNodeID: "h", ExtractedAt: 1, ExtractorID: "x"},
	}}}}
	l := &Linker{deps: deps}
	hit := l.tryFuzzyPath(context.Background(), "client-app", "auth-svc", store.APICall{
		TargetMethod: "GET", TargetPathTemplate: "/x",
	})
	if hit != nil {
		t.Errorf("hit = %+v; want nil (non-http skipped in fuzzy)", hit)
	}
}

func TestSurfaceInsertErrorIsWrapped(t *testing.T) {
	us := &fakeUnresolvedStore{err: errors.New("disk full")}
	s := &unresolvedSurfacer{store: us, audit: &fakeAuditEmitter{}, workspaceID: "ws"}
	err := s.Surface(context.Background(), store.APICall{CallID: "c"}, yaml.PolicySurface, "no")
	if err == nil {
		t.Errorf("Surface(insert error) = nil; want non-nil")
	}
}

func TestFindGRPCEndpointMatchesShortName(t *testing.T) {
	tgtStore := &fakeProjectStore{
		endpoints: []store.APIEndpoint{{
			EndpointID: "ep-1", Repo: "order-svc", Kind: "grpc",
			ProtoService:  "OrderService",
			ProtoRPC:      "PlaceOrder",
			HandlerNodeID: "h", ExtractedAt: 1, ExtractorID: "x",
		}},
	}
	deps := &fakeDeps{stores: map[string]*fakeProjectStore{"order-svc": tgtStore}}
	l := &Linker{deps: deps}
	ep := l.findGRPCEndpoint(context.Background(), "order-svc", "acme.v1", "OrderService", "PlaceOrder")
	if ep == nil {
		t.Errorf("ep = nil; want match on short-name fallback")
	}
}
