package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/daemon/handlers"
)

type fakeContractFederation struct{}

func (f *fakeContractFederation) ValidateContractManifest(_ context.Context, _, _ string) (ContractManifestValidation, error) {
	return ContractManifestValidation{}, nil
}
func (f *fakeContractFederation) RegisterWorkspace(_ context.Context, _ Workspace) error {
	return nil
}
func (f *fakeContractFederation) ListWorkspaces(_ context.Context) ([]Workspace, error) {
	return nil, nil
}
func (f *fakeContractFederation) GetWorkspace(_ context.Context, _ string) (Workspace, error) {
	return Workspace{}, nil
}
func (f *fakeContractFederation) ListRecentBreakingChanges(_ context.Context, _ string, _ int) ([]BreakingChange, error) {
	return nil, nil
}
func (f *fakeContractFederation) ListWorkspaceMembers(_ context.Context, _ string) ([]Member, error) {
	return nil, nil
}
func (f *fakeContractFederation) AddWorkspaceMember(_ context.Context, _ Member) error {
	return nil
}
func (f *fakeContractFederation) RemoveWorkspace(_ context.Context, _ string) (int64, error) {
	return 0, nil
}
func (f *fakeContractFederation) GetWorkspacePolicy(_ context.Context, _ string) (string, error) {
	return "", nil
}
func (f *fakeContractFederation) SetWorkspacePolicy(_ context.Context, _, _ string) error {
	return nil
}
func (f *fakeContractFederation) GetBreakingChangeWithConsumers(_ context.Context, _ string) (BreakingChange, []BreakingChangeConsumer, error) {
	return BreakingChange{}, nil, nil
}
func (f *fakeContractFederation) Close() error { return nil }

type plan20RESTFederation struct {
	fakeContractFederation
	validateRepo      string
	validateWorkspace string
	workspaces        map[string]Workspace
	members           map[string][]Member
	policies          map[string]string
}

func newPlan20RESTFederation() *plan20RESTFederation {
	return &plan20RESTFederation{
		workspaces: map[string]Workspace{},
		members:    map[string][]Member{},
		policies:   map[string]string{},
	}
}

func (f *plan20RESTFederation) ValidateContractManifest(_ context.Context, repo, workspaceID string) (ContractManifestValidation, error) {
	f.validateRepo = repo
	f.validateWorkspace = workspaceID
	return ContractManifestValidation{
		Valid:         true,
		SchemaVersion: 1,
		Services: []ContractManifestService{
			{BaseURLRef: "${BACKEND_URL}", TargetRepo: "backend"},
		},
	}, nil
}

func (f *plan20RESTFederation) RegisterWorkspace(_ context.Context, row Workspace) error {
	f.workspaces[row.WorkspaceID] = row
	return nil
}

func (f *plan20RESTFederation) ListWorkspaces(_ context.Context) ([]Workspace, error) {
	rows := make([]Workspace, 0, len(f.workspaces))
	for _, row := range f.workspaces {
		rows = append(rows, row)
	}
	return rows, nil
}

func (f *plan20RESTFederation) ListWorkspaceMembers(_ context.Context, workspaceID string) ([]Member, error) {
	return append([]Member(nil), f.members[workspaceID]...), nil
}

func (f *plan20RESTFederation) AddWorkspaceMember(_ context.Context, row Member) error {
	f.members[row.WorkspaceID] = append(f.members[row.WorkspaceID], row)
	return nil
}

func (f *plan20RESTFederation) RemoveWorkspace(_ context.Context, workspaceID string) (int64, error) {
	_, existed := f.workspaces[workspaceID]
	delete(f.workspaces, workspaceID)
	delete(f.members, workspaceID)
	delete(f.policies, workspaceID)
	if !existed {
		return 0, nil
	}
	return 1, nil
}

func (f *plan20RESTFederation) GetWorkspacePolicy(_ context.Context, workspaceID string) (string, error) {
	return f.policies[workspaceID], nil
}

func (f *plan20RESTFederation) SetWorkspacePolicy(_ context.Context, workspaceID, policy string) error {
	f.policies[workspaceID] = policy
	return nil
}

type fakeContractCoordinator struct{}

func (f *fakeContractCoordinator) RecentDispatches(_ context.Context, _ int) ([]DispatchDecision, error) {
	return nil, nil
}

func TestContractFederationNilByDefault(t *testing.T) {
	s := newTestServer(t)
	if s.ContractFederation() != nil {
		t.Error("ContractFederation() non-nil before SetContractFederation")
	}
}

func TestSetContractFederationRoundTrip(t *testing.T) {
	s := newTestServer(t)
	f := &fakeContractFederation{}
	s.SetContractFederation(f)
	got := s.ContractFederation()
	if got == nil {
		t.Fatal("ContractFederation() nil after SetContractFederation")
	}
	if got != f {
		t.Errorf("ContractFederation() returned %v; want %v", got, f)
	}
}

func TestSetContractFederationNilSafe(t *testing.T) {
	s := newTestServer(t)
	f := &fakeContractFederation{}
	s.SetContractFederation(f)
	if s.ContractFederation() == nil {
		t.Fatal("ContractFederation() nil after first Set")
	}
	s.SetContractFederation(nil)
	if s.ContractFederation() != nil {
		t.Error("ContractFederation() non-nil after SetContractFederation(nil)")
	}
}

func TestContractFederationRESTAdapterNilSafe(t *testing.T) {
	s := newTestServer(t)
	if s.ContractFederationREST() != nil {
		t.Error("ContractFederationREST() non-nil before SetContractFederation")
	}
	s.SetContractFederation(&fakeContractFederation{})
	if s.ContractFederationREST() == nil {
		t.Error("ContractFederationREST() nil after SetContractFederation")
	}
	s.SetContractFederation(nil)
	if s.ContractFederationREST() != nil {
		t.Error("ContractFederationREST() non-nil after SetContractFederation(nil)")
	}
}

func TestPlan20RESTRoutesUseServerFederationSeam(t *testing.T) {
	s := newTestServer(t)
	fed := newPlan20RESTFederation()
	s.SetContractFederation(fed)

	post := func(path string, body any) []byte {
		t.Helper()
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal %s: %v", path, err)
		}
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d; want 200; body=%s", path, rec.Code, rec.Body.String())
		}
		return rec.Body.Bytes()
	}

	var validate handlers.ContractValidateRESTResponse
	if err := json.Unmarshal(post("/v1/mcpgateway/contract/validate", handlers.ContractValidateRESTRequest{
		Repo: "/tmp/client", WorkspaceID: "ws-1",
	}), &validate); err != nil {
		t.Fatalf("decode validate: %v", err)
	}
	if !validate.Valid || fed.validateRepo != "/tmp/client" || fed.validateWorkspace != "ws-1" {
		t.Fatalf("validate = %+v repo=%q workspace=%q; want real federation seam call", validate, fed.validateRepo, fed.validateWorkspace)
	}

	var initResp handlers.WorkspaceInitRESTResponse
	if err := json.Unmarshal(post("/v1/mcpgateway/workspace/init", handlers.WorkspaceInitRESTRequest{
		WorkspaceID: "ws-1", OwningProject: "client", Members: []string{"backend"}, PolicyLocked: true,
	}), &initResp); err != nil {
		t.Fatalf("decode init: %v", err)
	}
	if initResp.WorkspaceID != "ws-1" || initResp.SchemaVersion != 1 {
		t.Fatalf("initResp = %+v; want workspace ws-1 schema v1", initResp)
	}

	var listResp handlers.WorkspaceListRESTResponse
	if err := json.Unmarshal(post("/v1/mcpgateway/workspace/list", handlers.WorkspaceListRESTRequest{}), &listResp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listResp.Workspaces) != 1 || listResp.Workspaces[0].SchemaVersion != 1 {
		t.Fatalf("listResp = %+v; want one schema-versioned workspace", listResp)
	}

	var membersResp handlers.WorkspaceMembersRESTResponse
	if err := json.Unmarshal(post("/v1/mcpgateway/workspace/members", handlers.WorkspaceMembersRESTRequest{
		WorkspaceID: "ws-1",
	}), &membersResp); err != nil {
		t.Fatalf("decode members: %v", err)
	}
	if len(membersResp.Members) != 2 {
		t.Fatalf("members = %+v; want owner + explicit member", membersResp.Members)
	}

	var linkResp handlers.WorkspaceLinkRESTResponse
	if err := json.Unmarshal(post("/v1/mcpgateway/workspace/link", handlers.WorkspaceLinkRESTRequest{
		WorkspaceID: "ws-1", ProjectID: "cli",
	}), &linkResp); err != nil {
		t.Fatalf("decode link: %v", err)
	}
	if linkResp.ProjectID != "cli" {
		t.Fatalf("linkResp = %+v; want cli linked", linkResp)
	}

	var policySetResp handlers.WorkspacePolicySetRESTResponse
	if err := json.Unmarshal(post("/v1/mcpgateway/workspace/policy/set", handlers.WorkspacePolicySetRESTRequest{
		WorkspaceID: "ws-1", NewPolicy: "permissive",
	}), &policySetResp); err != nil {
		t.Fatalf("decode policy set: %v", err)
	}
	if policySetResp.NewPolicy != "permissive" {
		t.Fatalf("policySetResp = %+v; want permissive", policySetResp)
	}

	var policyGetResp handlers.WorkspacePolicyGetRESTResponse
	if err := json.Unmarshal(post("/v1/mcpgateway/workspace/policy/get", handlers.WorkspacePolicyGetRESTRequest{
		WorkspaceID: "ws-1",
	}), &policyGetResp); err != nil {
		t.Fatalf("decode policy get: %v", err)
	}
	if policyGetResp.Policy != "permissive" {
		t.Fatalf("policyGetResp = %+v; want permissive", policyGetResp)
	}

	var removeResp handlers.WorkspaceRemoveRESTResponse
	if err := json.Unmarshal(post("/v1/mcpgateway/workspace/remove", handlers.WorkspaceRemoveRESTRequest{
		WorkspaceID: "ws-1",
	}), &removeResp); err != nil {
		t.Fatalf("decode remove: %v", err)
	}
	if removeResp.RowsAffected != 1 || len(fed.workspaces) != 0 || len(fed.members["ws-1"]) != 0 {
		t.Fatalf("removeResp = %+v workspaces=%+v members=%+v; want cascade through seam", removeResp, fed.workspaces, fed.members)
	}
}

func TestContractCoordinatorNilByDefault(t *testing.T) {
	s := newTestServer(t)
	if s.ContractCoordinator() != nil {
		t.Error("ContractCoordinator() non-nil before SetContractCoordinator")
	}
}

func TestSetContractCoordinatorRoundTrip(t *testing.T) {
	s := newTestServer(t)
	c := &fakeContractCoordinator{}
	s.SetContractCoordinator(c)
	got := s.ContractCoordinator()
	if got == nil {
		t.Fatal("ContractCoordinator() nil after SetContractCoordinator")
	}
	if got != c {
		t.Errorf("ContractCoordinator() returned %v; want %v", got, c)
	}
}

func TestSetContractCoordinatorNilSafe(t *testing.T) {
	s := newTestServer(t)
	c := &fakeContractCoordinator{}
	s.SetContractCoordinator(c)
	if s.ContractCoordinator() == nil {
		t.Fatal("ContractCoordinator() nil after first Set")
	}
	s.SetContractCoordinator(nil)
	if s.ContractCoordinator() != nil {
		t.Error("ContractCoordinator() non-nil after SetContractCoordinator(nil)")
	}
}

// TestContractFederationForDaemon_InterfaceContractStable pins the
// interface method set: drift here breaks the structural contract that
// the wiring file's compile-time anchor (var _ daemon.ContractFederationForDaemon
// = fedDB) depends on. The list mirrors J-0.7 ( as-shipped
// Wave-1 surface).
//
// Sister-test per [[feedback_sister_test_pattern]] — any method
// addition/removal on ContractFederationForDaemon MUST update this slice
// in lock-step so the assertion stays honest.
func TestContractFederationForDaemon_InterfaceContractStable(t *testing.T) {
	iface := reflect.TypeOf((*ContractFederationForDaemon)(nil)).Elem()
	wantMethods := []string{
		"AddWorkspaceMember",
		"Close",
		"GetBreakingChangeWithConsumers",
		"GetWorkspace",
		"GetWorkspacePolicy",
		"ListRecentBreakingChanges",
		"ListWorkspaceMembers",
		"ListWorkspaces",
		"RegisterWorkspace",
		"RemoveWorkspace",
		"SetWorkspacePolicy",
		"ValidateContractManifest",
	}
	if got := iface.NumMethod(); got != len(wantMethods) {
		t.Fatalf("ContractFederationForDaemon has %d methods, want %d (%v)",
			got, len(wantMethods), wantMethods)
	}
	for i, want := range wantMethods {
		if got := iface.Method(i).Name; got != want {
			t.Errorf("method[%d] = %q, want %q", i, got, want)
		}
	}
}

func TestContractCoordinatorForDaemon_InterfaceContractStable(t *testing.T) {
	iface := reflect.TypeOf((*ContractCoordinatorForDaemon)(nil)).Elem()
	wantMethods := []string{"RecentDispatches"}
	if got := iface.NumMethod(); got != len(wantMethods) {
		t.Fatalf("ContractCoordinatorForDaemon has %d methods, want %d (%v)",
			got, len(wantMethods), wantMethods)
	}
	for i, want := range wantMethods {
		if got := iface.Method(i).Name; got != want {
			t.Errorf("method[%d] = %q, want %q", i, got, want)
		}
	}
}
