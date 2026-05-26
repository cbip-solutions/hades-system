//go:build cgo

package link

import (
	"context"
	"errors"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/caronte/contract/extract"
	"github.com/cbip-solutions/hades-system/internal/caronte/contract/yaml"
	"github.com/cbip-solutions/hades-system/internal/caronte/store"
	"github.com/cbip-solutions/hades-system/internal/caronte/store/federation"
)

type fakeProjectStore struct {
	calls     []store.APICall
	endpoints []store.APIEndpoint
	nodes     map[string]store.Node
}

func (f *fakeProjectStore) ListAPICallsByRepo(_ context.Context, repo string) ([]store.APICall, error) {
	out := []store.APICall{}
	for _, c := range f.calls {
		if c.Repo == repo {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakeProjectStore) ListAPIEndpointsByRepo(_ context.Context, repo string) ([]store.APIEndpoint, error) {
	out := []store.APIEndpoint{}
	for _, e := range f.endpoints {
		if e.Repo == repo {
			out = append(out, e)
		}
	}
	return out, nil
}

func (f *fakeProjectStore) GetAPICall(_ context.Context, callID string) (store.APICall, error) {
	for _, c := range f.calls {
		if c.CallID == callID {
			return c, nil
		}
	}
	return store.APICall{}, errors.New("no such call")
}

func (f *fakeProjectStore) GetNode(_ context.Context, nodeID string) (store.Node, error) {
	if n, ok := f.nodes[nodeID]; ok {
		return n, nil
	}
	return store.Node{}, errors.New("no such node")
}

type fakeWorkspace struct {
	persisted []store.ContractLink
	err       error
}

func (w *fakeWorkspace) CrossRepoLink(_ context.Context, link store.ContractLink) error {
	if w.err != nil {
		return w.err
	}
	w.persisted = append(w.persisted, link)
	return nil
}

type fakeFedRead struct {
	links []federation.LinkRow
	err   error
}

func (f *fakeFedRead) ListContractLinks(_ context.Context, _ string, _ int) ([]federation.LinkRow, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.links, nil
}

type fakeDeps struct {
	stores  map[string]*fakeProjectStore
	fed     *fakeFedRead
	openErr error
	opened  []string
}

func (d *fakeDeps) OpenProjectStore(_ context.Context, repo string) (ProjectStorePort, error) {
	d.opened = append(d.opened, repo)
	if d.openErr != nil {
		return nil, d.openErr
	}
	s, ok := d.stores[repo]
	if !ok {
		return nil, errors.New("no fake store for repo " + repo)
	}
	return s, nil
}

func (d *fakeDeps) FederationDB() FederationReadPort { return d.fed }

func TestLinkProjectArtifactTierWinsOverManifest(t *testing.T) {
	// Source-repo "client-app" has one gRPC api_call with TargetProto =
	// "acme.v1.OrderService/PlaceOrder". Server-repo "order-svc" has a
	// matching gRPC api_endpoint. caronte.yaml also points client-app →
	// order-svc via env. The artifact tier MUST win.
	srcStore := &fakeProjectStore{
		calls: []store.APICall{{
			CallID:       "c1",
			Repo:         "client-app",
			CallerNodeID: "n1",
			TargetProto:  "acme.v1.OrderService/PlaceOrder",
			BaseURLRef:   "ORDER_SVC_URL",
			Confidence:   "exact_proto_import",
			ExtractedAt:  1,
			ExtractorID:  "proto",
		}},
	}
	tgtStore := &fakeProjectStore{
		endpoints: []store.APIEndpoint{{
			EndpointID:    "ep-place",
			Repo:          "order-svc",
			Kind:          "grpc",
			ProtoService:  "acme.v1.OrderService",
			ProtoRPC:      "PlaceOrder",
			HandlerNodeID: "h1",
			ExtractedAt:   1,
			ExtractorID:   "proto",
		}},
	}
	deps := &fakeDeps{
		stores: map[string]*fakeProjectStore{
			"client-app": srcStore,
			"order-svc":  tgtStore,
		},
	}
	ws := &fakeWorkspace{}
	audit := &fakeAuditEmitter{}
	us := &fakeUnresolvedStore{}
	manifests := map[string]*yaml.Manifest{
		"client-app": {
			SchemaVersion: 1,
			Services: []yaml.Service{
				{BaseURLEnv: "ORDER_SVC_URL", TargetRepo: "order-svc"},
			},
			UnresolvedPolicy: yaml.PolicySurface,
		},
	}
	stubs := map[string][]extract.StubReference{
		"client-app": {{
			Repo: "order-svc", ProtoPackage: "acme.v1",
			ServiceName: "OrderService", RpcName: "PlaceOrder",
		}},
	}
	linker := NewLinker(ws, us, audit, manifests, stubs, "ws-1", deps)
	res, err := linker.LinkProject(context.Background(), "p-1", "client-app")
	if err != nil {
		t.Fatalf("LinkProject: %v", err)
	}
	if res.LinksPersisted != 1 {
		t.Errorf("LinksPersisted = %d; want 1", res.LinksPersisted)
	}
	if got := res.TierCounts[ConfExactProtoImport]; got != 1 {
		t.Errorf("TierCounts[exact_proto_import] = %d; want 1", got)
	}
	if got := res.TierCounts[ConfStaticPath]; got != 0 {
		t.Errorf("TierCounts[static_path] = %d; want 0 (artifact tier wins)", got)
	}
	if len(ws.persisted) != 1 {
		t.Fatalf("ws.persisted = %d; want 1", len(ws.persisted))
	}
	got := ws.persisted[0]
	if got.LinkMethod != "artifact" || got.Confidence != "exact_proto_import" {
		t.Errorf("link drift: method=%s conf=%s; want artifact/exact_proto_import", got.LinkMethod, got.Confidence)
	}
	if got.ResolvedAt == 0 {
		t.Errorf("ResolvedAt = 0; want non-zero (time.Now().UnixNano())")
	}
	if len(audit.events) != 1 || audit.events[0].t != federation.EvtCrossRepoLink {
		t.Errorf("audit drift: %+v; want 1 EvtCrossRepoLink", audit.events)
	}
}

func TestLinkProjectStaticPathTier(t *testing.T) {
	srcStore := &fakeProjectStore{
		calls: []store.APICall{{
			CallID: "c-http", Repo: "client-app", CallerNodeID: "n1",
			TargetMethod: "GET", TargetPathTemplate: "/users/{id}",
			BaseURLRef: "AUTH_SVC_URL", Confidence: "static_path",
			ExtractedAt: 1, ExtractorID: "gohttp",
		}},
	}
	tgtStore := &fakeProjectStore{
		endpoints: []store.APIEndpoint{{
			EndpointID: "ep-getuser", Repo: "auth-svc",
			Kind: "http", Method: "GET", PathTemplate: "/users/{id}",
			HandlerNodeID: "h1", ExtractedAt: 1, ExtractorID: "gohttp",
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
	if res.LinksPersisted != 1 || res.TierCounts[ConfStaticPath] != 1 {
		t.Errorf("LinksPersisted=%d TierCounts=%+v; want static_path=1", res.LinksPersisted, res.TierCounts)
	}
	if len(ws.persisted) == 1 {
		if ws.persisted[0].LinkMethod != "static" || ws.persisted[0].Confidence != "static_path" {
			t.Errorf("persist drift: %+v", ws.persisted[0])
		}
	}
}

func TestLinkProjectFuzzyPathTier(t *testing.T) {
	srcStore := &fakeProjectStore{
		calls: []store.APICall{{
			CallID: "c-fuzzy", Repo: "client-app", CallerNodeID: "n1",
			TargetMethod: "GET", TargetPathTemplate: "/users/{id}",
			BaseURLRef: "AUTH_SVC_URL", Confidence: "fuzzy_path",
			ExtractedAt: 1, ExtractorID: "gohttp",
		}},
	}
	tgtStore := &fakeProjectStore{
		endpoints: []store.APIEndpoint{{
			EndpointID: "ep-getuser", Repo: "auth-svc",
			Kind: "http", Method: "GET", PathTemplate: "/users/{user_id}",
			HandlerNodeID: "h1", ExtractedAt: 1, ExtractorID: "gohttp",
		}},
	}
	deps := &fakeDeps{stores: map[string]*fakeProjectStore{"client-app": srcStore, "auth-svc": tgtStore}}
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
	if res.LinksPersisted != 1 || res.TierCounts[ConfFuzzyPath] != 1 {
		t.Errorf("LinksPersisted=%d TierCounts=%+v; want fuzzy_path=1", res.LinksPersisted, res.TierCounts)
	}
	if len(ws.persisted) == 1 && ws.persisted[0].LinkMethod != "fuzzy" {
		t.Errorf("LinkMethod = %s; want fuzzy", ws.persisted[0].LinkMethod)
	}
}

func TestLinkProjectUnresolvedSurfaces(t *testing.T) {
	srcStore := &fakeProjectStore{
		calls: []store.APICall{{
			CallID: "c-unres", Repo: "client-app", CallerNodeID: "n1",
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
			UnresolvedPolicy: yaml.PolicySurface,
		},
	}
	linker := NewLinker(ws, us, audit, manifests, nil, "ws-1", deps)
	res, err := linker.LinkProject(context.Background(), "p1", "client-app")
	if err != nil {
		t.Fatalf("LinkProject: %v", err)
	}
	if res.LinksPersisted != 0 {
		t.Errorf("LinksPersisted = %d; want 0 (inv-zen-265 no false-link)", res.LinksPersisted)
	}
	if res.UnresolvedRows != 1 {
		t.Errorf("UnresolvedRows = %d; want 1 (PolicySurface)", res.UnresolvedRows)
	}
	if len(us.inserted) != 1 {
		t.Errorf("us.inserted = %d; want 1", len(us.inserted))
	}
	if len(audit.events) != 1 || audit.events[0].t != federation.EvtUnresolvedCall {
		t.Errorf("audit drift: %+v; want 1 EvtUnresolvedCall", audit.events)
	}
}

func TestLinkProjectAuditPerInsert(t *testing.T) {
	srcStore := &fakeProjectStore{
		calls: []store.APICall{
			{
				CallID: "c1", Repo: "client-app", CallerNodeID: "n1",
				TargetMethod: "GET", TargetPathTemplate: "/a",
				BaseURLRef: "AUTH_URL", Confidence: "static_path",
				ExtractedAt: 1, ExtractorID: "gohttp",
			},
			{
				CallID: "c2", Repo: "client-app", CallerNodeID: "n2",
				TargetMethod: "GET", TargetPathTemplate: "/b",
				BaseURLRef: "AUTH_URL", Confidence: "static_path",
				ExtractedAt: 1, ExtractorID: "gohttp",
			},
		},
	}
	tgtStore := &fakeProjectStore{
		endpoints: []store.APIEndpoint{
			{EndpointID: "ep-a", Repo: "auth-svc", Kind: "http", Method: "GET", PathTemplate: "/a", HandlerNodeID: "h1", ExtractedAt: 1, ExtractorID: "gohttp"},
			{EndpointID: "ep-b", Repo: "auth-svc", Kind: "http", Method: "GET", PathTemplate: "/b", HandlerNodeID: "h2", ExtractedAt: 1, ExtractorID: "gohttp"},
		},
	}
	deps := &fakeDeps{stores: map[string]*fakeProjectStore{"client-app": srcStore, "auth-svc": tgtStore}}
	ws := &fakeWorkspace{}
	audit := &fakeAuditEmitter{}
	us := &fakeUnresolvedStore{}
	manifests := map[string]*yaml.Manifest{
		"client-app": {
			SchemaVersion:    1,
			Services:         []yaml.Service{{BaseURLEnv: "AUTH_URL", TargetRepo: "auth-svc"}},
			UnresolvedPolicy: yaml.PolicySurface,
		},
	}
	linker := NewLinker(ws, us, audit, manifests, nil, "ws-1", deps)
	res, err := linker.LinkProject(context.Background(), "p1", "client-app")
	if err != nil {
		t.Fatalf("LinkProject: %v", err)
	}
	if res.LinksPersisted != 2 {
		t.Errorf("LinksPersisted = %d; want 2", res.LinksPersisted)
	}
	if len(audit.events) != 2 {
		t.Errorf("audit events = %d; want 2 (1 per persist; inv-zen-269)", len(audit.events))
	}
	for _, e := range audit.events {
		if e.t != federation.EvtCrossRepoLink {
			t.Errorf("audit event type = %q; want EvtCrossRepoLink", e.t)
		}
	}
}

func TestLinkProjectCapaFirewallTransit(t *testing.T) {
	srcStore := &fakeProjectStore{
		calls: []store.APICall{{
			CallID: "c", Repo: "client-app", CallerNodeID: "n",
			TargetMethod: "GET", TargetPathTemplate: "/a",
			BaseURLRef: "AUTH_URL", Confidence: "static_path",
			ExtractedAt: 1, ExtractorID: "gohttp",
		}},
	}
	tgtStore := &fakeProjectStore{
		endpoints: []store.APIEndpoint{{
			EndpointID: "ep-a", Repo: "auth-svc", Kind: "http", Method: "GET", PathTemplate: "/a",
			HandlerNodeID: "h1", ExtractedAt: 1, ExtractorID: "gohttp",
		}},
	}
	deps := &fakeDeps{stores: map[string]*fakeProjectStore{"client-app": srcStore, "auth-svc": tgtStore}}
	denied := errors.New("ErrCrossProjectDenied")
	ws := &fakeWorkspace{err: denied}
	audit := &fakeAuditEmitter{}
	us := &fakeUnresolvedStore{}
	manifests := map[string]*yaml.Manifest{
		"client-app": {
			SchemaVersion:    1,
			Services:         []yaml.Service{{BaseURLEnv: "AUTH_URL", TargetRepo: "auth-svc"}},
			UnresolvedPolicy: yaml.PolicySurface,
		},
	}
	linker := NewLinker(ws, us, audit, manifests, nil, "ws-1", deps)
	res, err := linker.LinkProject(context.Background(), "p1", "client-app")
	if err != nil {
		t.Fatalf("LinkProject (top-level): %v", err)
	}
	if res.LinksPersisted != 0 {
		t.Errorf("LinksPersisted = %d; want 0 (denial surfaced)", res.LinksPersisted)
	}
	if len(res.Errors) != 1 || !errors.Is(res.Errors[0], denied) {
		t.Errorf("Errors = %v; want one wrapping ErrCrossProjectDenied", res.Errors)
	}
	if len(audit.events) != 0 {
		t.Errorf("audit events = %d; want 0 (no INSERT, no audit)", len(audit.events))
	}
}

func TestLinkProjectForgedConfidenceTierIsRefused(t *testing.T) {

	cases := []struct {
		method LinkMethod
		conf   Confidence
		name   string
	}{
		{LinkArtifact, ConfStaticPath, "artifact-with-static-conf"},
		{LinkStatic, ConfExactProtoImport, "static-with-artifact-conf"},
		{LinkFuzzy, ConfStaticPath, "fuzzy-with-static-conf"},
		{LinkCaronteYAML, ConfStaticPath, "bare-caronte_yaml"},
		{LinkMethod("invented"), ConfStaticPath, "unknown-method"},
	}
	for _, c := range cases {
		err := checkTierConsistency(c.method, c.conf)
		if !errors.Is(err, ErrConfidenceTierDowngrade) {
			t.Errorf("[%s] = %v; want ErrConfidenceTierDowngrade", c.name, err)
		}
	}

	for _, c := range []struct {
		method LinkMethod
		conf   Confidence
		name   string
	}{
		{LinkArtifact, ConfExactProtoImport, "artifact-proto"},
		{LinkArtifact, ConfSpecArtifact, "artifact-spec"},
		{LinkStatic, ConfStaticPath, "static"},
		{LinkFuzzy, ConfFuzzyPath, "fuzzy"},
	} {
		if err := checkTierConsistency(c.method, c.conf); err != nil {
			t.Errorf("[%s] = %v; want nil", c.name, err)
		}
	}
}
