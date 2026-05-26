package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/client"
)

func newPlan20TestClient(t *testing.T) (*client.Client, func()) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/mcpgateway/contract":
			_ = json.NewEncoder(w).Encode(client.ContractResponse{EndpointID: "e1"})
		case "/v1/mcpgateway/contract/validate":
			_ = json.NewEncoder(w).Encode(client.ContractValidateResponse{Valid: true})
		case "/v1/mcpgateway/contract/why":
			_ = json.NewEncoder(w).Encode(client.ContractWhyResponse{ChangeID: "chg-1"})
		case "/v1/mcpgateway/workspace/init":
			_ = json.NewEncoder(w).Encode(client.WorkspaceInitResponse{WorkspaceID: "ws-1"})
		case "/v1/mcpgateway/workspace/list":
			_ = json.NewEncoder(w).Encode(client.WorkspaceListResponse{})
		case "/v1/mcpgateway/workspace/members":
			_ = json.NewEncoder(w).Encode(client.WorkspaceMembersResponse{})
		case "/v1/mcpgateway/workspace/link":
			_ = json.NewEncoder(w).Encode(client.WorkspaceLinkResponse{WorkspaceID: "ws-1", ProjectID: "p1"})
		case "/v1/mcpgateway/workspace/remove":
			_ = json.NewEncoder(w).Encode(client.WorkspaceRemoveResponse{WorkspaceID: "ws-1"})
		case "/v1/mcpgateway/workspace/policy/get":
			_ = json.NewEncoder(w).Encode(client.WorkspacePolicyGetResponse{WorkspaceID: "ws-1"})
		case "/v1/mcpgateway/workspace/policy/set":
			_ = json.NewEncoder(w).Encode(client.WorkspacePolicySetResponse{WorkspaceID: "ws-1", NewPolicy: "locked"})
		case "/v1/mcpgateway/federation/health":
			_ = json.NewEncoder(w).Encode(client.FederationHealthResponse{Reachable: true})
		case "/v1/mcpgateway/api-impact":
			_ = json.NewEncoder(w).Encode(client.APIImpactResponse{DiffRef: "x"})
		case "/v1/audit/emit":
			_ = json.NewEncoder(w).Encode(client.AuditEmitResp{Accepted: true})
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	return client.NewWithBaseURL(srv.URL), srv.Close
}

func TestProductionContractClient(t *testing.T) {
	c, cleanup := newPlan20TestClient(t)
	defer cleanup()
	wrap := &productionContractClient{c: c}
	if _, err := wrap.Contract(context.Background(), client.ContractRequest{Endpoint: "e1"}); err != nil {
		t.Errorf("Contract: %v", err)
	}
	if _, err := wrap.ContractValidate(context.Background(), client.ContractValidateRequest{Repo: "/r"}); err != nil {
		t.Errorf("ContractValidate: %v", err)
	}
	if _, err := wrap.ContractWhy(context.Background(), client.ContractWhyRequest{ChangeID: "chg-1"}); err != nil {
		t.Errorf("ContractWhy: %v", err)
	}
}

func TestProductionWorkspaceClient(t *testing.T) {
	c, cleanup := newPlan20TestClient(t)
	defer cleanup()
	wrap := &productionWorkspaceClient{c: c}
	if _, err := wrap.WorkspaceInit(context.Background(), client.WorkspaceInitRequest{WorkspaceID: "ws-1"}); err != nil {
		t.Errorf("WorkspaceInit: %v", err)
	}
	if _, err := wrap.WorkspaceList(context.Background(), client.WorkspaceListRequest{}); err != nil {
		t.Errorf("WorkspaceList: %v", err)
	}
	if _, err := wrap.WorkspaceMembers(context.Background(), client.WorkspaceMembersRequest{WorkspaceID: "ws-1"}); err != nil {
		t.Errorf("WorkspaceMembers: %v", err)
	}
	if _, err := wrap.WorkspaceLink(context.Background(), client.WorkspaceLinkRequest{WorkspaceID: "ws-1", ProjectID: "p1"}); err != nil {
		t.Errorf("WorkspaceLink: %v", err)
	}
	if _, err := wrap.WorkspaceRemove(context.Background(), client.WorkspaceRemoveRequest{WorkspaceID: "ws-1"}); err != nil {
		t.Errorf("WorkspaceRemove: %v", err)
	}
	if _, err := wrap.WorkspacePolicyGet(context.Background(), client.WorkspacePolicyGetRequest{WorkspaceID: "ws-1"}); err != nil {
		t.Errorf("WorkspacePolicyGet: %v", err)
	}
	if _, err := wrap.WorkspacePolicySet(context.Background(), client.WorkspacePolicySetRequest{WorkspaceID: "ws-1", NewPolicy: "locked"}); err != nil {
		t.Errorf("WorkspacePolicySet: %v", err)
	}
	if err := wrap.EmitAudit(context.Background(), "policy_change_requested", map[string]any{"a": 1}); err != nil {
		t.Errorf("EmitAudit: %v", err)
	}
}

func TestProductionFederationClient(t *testing.T) {
	c, cleanup := newPlan20TestClient(t)
	defer cleanup()
	wrap := &productionFederationClient{c: c}
	if _, err := wrap.FederationHealth(context.Background(), client.FederationHealthRequest{}); err != nil {
		t.Errorf("FederationHealth: %v", err)
	}
	if _, err := wrap.APIImpact(context.Background(), client.APIImpactRequest{DiffRef: "x"}); err != nil {
		t.Errorf("APIImpact: %v", err)
	}
}

func TestNewContractCmdProdConstructs(t *testing.T) {
	cmd := NewContractCmdProd()
	if cmd.Use != "contract <endpoint>" {
		t.Errorf("Use = %q", cmd.Use)
	}
	if len(cmd.Commands()) < 2 {
		t.Errorf("expected at least 2 sub-commands (validate + why); got %d", len(cmd.Commands()))
	}
}

func TestNewWorkspaceCmdProdConstructs(t *testing.T) {
	cmd := NewWorkspaceCmdProd()

	if len(cmd.Commands()) < 6 {
		t.Errorf("expected at least 6 sub-commands; got %d", len(cmd.Commands()))
	}
}

func TestNewFederationCmdProdConstructs(t *testing.T) {
	cmd := NewFederationCmdProd()
	if len(cmd.Commands()) < 1 {
		t.Errorf("expected at least 1 sub-command (health); got %d", len(cmd.Commands()))
	}
}

func TestNewAPIImpactCmdProdConstructs(t *testing.T) {
	cmd := NewAPIImpactCmdProd()
	if !strings.HasPrefix(cmd.Use, "api-impact") {
		t.Errorf("Use = %q; want api-impact prefix", cmd.Use)
	}
}

func TestNewWorkspaceSubCommandConstructors(t *testing.T) {
	if c := NewWorkspaceInitCmdProd(); c == nil {
		t.Error("NewWorkspaceInitCmdProd nil")
	}
	if c := NewWorkspaceListCmdProd(); c == nil {
		t.Error("NewWorkspaceListCmdProd nil")
	}
	if c := NewWorkspaceMembersCmdProd(); c == nil {
		t.Error("NewWorkspaceMembersCmdProd nil")
	}
	if c := NewWorkspaceLinkCmdProd(); c == nil {
		t.Error("NewWorkspaceLinkCmdProd nil")
	}
	if c := NewWorkspaceRemoveCmdProd(); c == nil {
		t.Error("NewWorkspaceRemoveCmdProd nil")
	}
	if c := NewWorkspacePolicyGetCmdProd(); c == nil {
		t.Error("NewWorkspacePolicyGetCmdProd nil")
	}
	if c := NewWorkspacePolicySetCmdProd(); c == nil {
		t.Error("NewWorkspacePolicySetCmdProd nil")
	}
	if c := NewContractValidateCmdProd(); c == nil {
		t.Error("NewContractValidateCmdProd nil")
	}
	if c := NewContractWhyCmdProd(); c == nil {
		t.Error("NewContractWhyCmdProd nil")
	}
	if c := NewFederationHealthCmdProd(); c == nil {
		t.Error("NewFederationHealthCmdProd nil")
	}
}
