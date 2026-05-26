//go:build cgo

// tests/compliance/inv_zen_265_unresolved_surface_test.go
//
// inv-zen-265 (unresolvable client calls surfaced as `unresolved` rows; never
// silently dropped or false-linked; Plan 20 Phase F):
//
//	When an api_calls row has a base_url_ref with no matching caronte.yaml
//	manifest entry AND no artifact-tier hit, the linker MUST record an
//	`unresolved` row + emit a Plan 14 Tessera audit row (under the default
//	PolicySurface), and MUST NOT insert a contract_links row. The schema
//	CHECK on contract_links.confidence refuses any value outside the four
//	tier enum (exact_proto_import|spec_artifact|static_path|fuzzy_path), so
//	even a forged INSERT with confidence='unresolved' is refused.
package compliance

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/caronte/contract/link"
	"github.com/cbip-solutions/hades-system/internal/caronte/contract/yaml"
	"github.com/cbip-solutions/hades-system/internal/caronte/store"
	"github.com/cbip-solutions/hades-system/internal/caronte/store/federation"
)

type fed244AuditEmitter struct {
	events []struct {
		t       federation.EventType
		payload []byte
	}
}

func (f *fed244AuditEmitter) Emit(_ context.Context, t federation.EventType, payload []byte) error {
	f.events = append(f.events, struct {
		t       federation.EventType
		payload []byte
	}{t: t, payload: append([]byte(nil), payload...)})
	return nil
}

type fed244SrcStore struct {
	calls     []store.APICall
	endpoints []store.APIEndpoint
}

func (f *fed244SrcStore) ListAPICallsByRepo(_ context.Context, repo string) ([]store.APICall, error) {
	out := []store.APICall{}
	for _, c := range f.calls {
		if c.Repo == repo {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fed244SrcStore) ListAPIEndpointsByRepo(_ context.Context, repo string) ([]store.APIEndpoint, error) {
	out := []store.APIEndpoint{}
	for _, e := range f.endpoints {
		if e.Repo == repo {
			out = append(out, e)
		}
	}
	return out, nil
}

func (f *fed244SrcStore) GetAPICall(_ context.Context, callID string) (store.APICall, error) {
	for _, c := range f.calls {
		if c.CallID == callID {
			return c, nil
		}
	}
	return store.APICall{}, errors.New("no such call")
}

func (f *fed244SrcStore) GetNode(_ context.Context, _ string) (store.Node, error) {
	return store.Node{}, errors.New("no node")
}

type fed244Workspace struct {
	ls federation.LinkStore
}

func (w *fed244Workspace) CrossRepoLink(ctx context.Context, link store.ContractLink) error {
	return w.ls.Append(ctx, federation.LinkRow{
		CallID:       link.CallID,
		CallRepo:     link.CallRepo,
		EndpointID:   link.EndpointID,
		EndpointRepo: link.EndpointRepo,
		Confidence:   link.Confidence,
		WorkspaceID:  link.WorkspaceID,
		ResolvedAt:   link.ResolvedAt,
		LinkMethod:   link.LinkMethod,
	})
}

func TestInvZen265_UnresolvedSurfacesNoFalseLink(t *testing.T) {
	ctx := context.Background()
	fedPath := filepath.Join(t.TempDir(), "zen-swarm", "workspace.db")
	fdb, err := federation.Open(ctx, fedPath)
	if err != nil {
		t.Fatalf("federation.Open: %v", err)
	}
	t.Cleanup(func() { _ = fdb.Close() })
	if err := fdb.RegisterWorkspace(ctx, federation.WorkspaceRow{
		WorkspaceID: "ws-244", OwningProject: "client-app",
		PolicyLocked: false, CreatedAt: 1, SchemaVersion: 1,
	}); err != nil {
		t.Fatalf("RegisterWorkspace: %v", err)
	}

	us := fdb.UnresolvedStore()
	if err := us.Insert(ctx, federation.UnresolvedRow{
		WorkspaceID: "ws-244",
		CallID:      "c-244-1",
		CallRepo:    "client-app",
		BaseURLRef:  "UNKNOWN_URL",
		Reason:      "no manifest entry / no path match",
		RecordedAt:  1,
	}); err != nil {
		t.Fatalf("UnresolvedStore.Insert: %v", err)
	}

	rows, err := fdb.ListUnresolvedByWorkspace(ctx, "ws-244", 0)
	if err != nil {
		t.Fatalf("ListUnresolvedByWorkspace: %v", err)
	}
	if len(rows) != 1 || rows[0].CallID != "c-244-1" {
		t.Errorf("rows = %+v; want 1 row with CallID=c-244-1", rows)
	}

	links, err := fdb.ListByCall(ctx, "ws-244", "c-244-1", "client-app")
	if err != nil {
		t.Fatalf("ListByCall: %v", err)
	}
	if len(links) != 0 {
		t.Errorf("contract_links has %d rows; want 0 (inv-zen-265 no false-link)", len(links))
	}
}

func TestInvZen265_DefaultPolicyIsSurface(t *testing.T) {

	m, err := yaml.Load(
		"../../internal/caronte/contract/yaml/fixtures/happy/caronte.yaml",
		[]string{"client-app", "auth-svc", "billing-svc", "shipping-svc"})
	if err != nil {
		t.Fatalf("yaml.Load(happy): %v", err)
	}
	if m.UnresolvedPolicy != yaml.PolicySurface {
		t.Errorf("UnresolvedPolicy = %q; want surface (doctrine-default)", m.UnresolvedPolicy)
	}
}

func TestInvZen265_SchemaCheckRefusesForgedConfidence(t *testing.T) {
	ctx := context.Background()
	fedPath := filepath.Join(t.TempDir(), "zen-swarm", "workspace.db")
	fdb, err := federation.Open(ctx, fedPath)
	if err != nil {
		t.Fatalf("federation.Open: %v", err)
	}
	t.Cleanup(func() { _ = fdb.Close() })
	if err := fdb.RegisterWorkspace(ctx, federation.WorkspaceRow{
		WorkspaceID: "ws-244-forged", OwningProject: "proj-a",
		PolicyLocked: false, CreatedAt: 1, SchemaVersion: 1,
	}); err != nil {
		t.Fatalf("RegisterWorkspace: %v", err)
	}
	// Forged INSERT via the LinkStore.Append path: confidence='unresolved'
	// is NOT in the schema CHECK enum and the SQL layer MUST reject.
	err = fdb.LinkStore().Append(ctx, federation.LinkRow{
		CallID: "c-forged", CallRepo: "proj-a",
		EndpointID: "ep-forged", EndpointRepo: "proj-b",
		Confidence:  "unresolved",
		WorkspaceID: "ws-244-forged",
		ResolvedAt:  1,
		LinkMethod:  "static",
	})
	if err == nil {
		t.Errorf("LinkStore.Append(confidence='unresolved') = nil; want CHECK constraint refusal")
	}
}

func TestInvZen265_LinkerSurfacePathProducesUnresolvedRow(t *testing.T) {
	ctx := context.Background()
	fedPath := filepath.Join(t.TempDir(), "zen-swarm", "workspace.db")
	fdb, err := federation.Open(ctx, fedPath)
	if err != nil {
		t.Fatalf("federation.Open: %v", err)
	}
	t.Cleanup(func() { _ = fdb.Close() })
	if err := fdb.RegisterWorkspace(ctx, federation.WorkspaceRow{
		WorkspaceID: "ws-244-e2e", OwningProject: "client-app",
		PolicyLocked: false, CreatedAt: 1, SchemaVersion: 1,
	}); err != nil {
		t.Fatalf("RegisterWorkspace: %v", err)
	}

	srcStore := &fed244SrcStore{
		calls: []store.APICall{{
			CallID: "c-244-e2e", Repo: "client-app", CallerNodeID: "n1",
			TargetMethod: "GET", TargetPathTemplate: "/users/{id}",
			BaseURLRef: "UNKNOWN_URL", Confidence: "static_path",
			ExtractedAt: 1, ExtractorID: "gohttp",
		}},
	}
	manifest := &yaml.Manifest{
		SchemaVersion:    1,
		Services:         []yaml.Service{{BaseURLEnv: "KNOWN_URL", TargetRepo: "auth-svc"}},
		UnresolvedPolicy: yaml.PolicySurface,
	}
	manifests := map[string]*yaml.Manifest{"client-app": manifest}
	audit := &fed244AuditEmitter{}
	ws := &fed244Workspace{ls: fdb.LinkStore()}

	linker := link.NewLinker(ws, fdb.UnresolvedStore(), audit, manifests, nil, "ws-244-e2e",
		link244Deps{src: srcStore, fed: fdb})

	res, err := linker.LinkProject(ctx, "p1", "client-app")
	if err != nil {
		t.Fatalf("LinkProject: %v", err)
	}
	if res.LinksPersisted != 0 {
		t.Errorf("LinksPersisted = %d; want 0 (inv-zen-265)", res.LinksPersisted)
	}
	if res.UnresolvedRows != 1 {
		t.Errorf("UnresolvedRows = %d; want 1", res.UnresolvedRows)
	}

	rows, err := fdb.ListUnresolvedByWorkspace(ctx, "ws-244-e2e", 0)
	if err != nil {
		t.Fatalf("ListUnresolvedByWorkspace: %v", err)
	}
	if len(rows) != 1 || rows[0].CallID != "c-244-e2e" {
		t.Errorf("rows = %+v; want 1 row CallID=c-244-e2e", rows)
	}
	// Audit chain transit: EvtUnresolvedCall MUST be emitted.
	foundUnresolved := false
	for _, e := range audit.events {
		if e.t == federation.EvtUnresolvedCall {
			foundUnresolved = true
		}
	}
	if !foundUnresolved {
		t.Errorf("audit events = %+v; want one EvtUnresolvedCall (inv-zen-265 audit transit)", audit.events)
	}

	links, err := fdb.ListByCall(ctx, "ws-244-e2e", "c-244-e2e", "client-app")
	if err != nil {
		t.Fatalf("ListByCall: %v", err)
	}
	if len(links) != 0 {
		t.Errorf("contract_links has %d rows; want 0 (inv-zen-265 no false-link)", len(links))
	}
}

type link244Deps struct {
	src *fed244SrcStore
	fed *federation.WorkspaceFederationDB
}

func (d link244Deps) OpenProjectStore(_ context.Context, _ string) (link.ProjectStorePort, error) {
	return d.src, nil
}

func (d link244Deps) FederationDB() link.FederationReadPort {
	return link244FedRead{fed: d.fed}
}

type link244FedRead struct {
	fed *federation.WorkspaceFederationDB
}

func (f link244FedRead) ListContractLinks(ctx context.Context, workspaceID string, limit int) ([]federation.LinkRow, error) {
	return f.fed.ListContractLinks(ctx, workspaceID, limit)
}
