package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/daemon/handlers"
)

type fakePlan20RESTCtx struct {
	h   http.Handler
	fed *fakeFederationRESTStore
}

func (f *fakePlan20RESTCtx) MCPGateway() http.Handler { return f.h }

func (f *fakePlan20RESTCtx) ContractFederationREST() handlers.ContractFederationRESTStore {
	return f.fed
}

type fakeFederationRESTStore struct {
	validateResp handlers.ContractValidateRESTResponse

	workspaces []handlers.WorkspaceRESTRow
	members    map[string][]handlers.WorkspaceMemberRESTRow
	policies   map[string]string

	registered []handlers.WorkspaceRESTRow
	added      []handlers.WorkspaceMemberRESTRow
	removed    []string
}

func (f *fakeFederationRESTStore) ValidateContractManifest(_ context.Context, repo, workspaceID string) (handlers.ContractValidateRESTResponse, error) {
	if repo == "" {
		return handlers.ContractValidateRESTResponse{}, nil
	}
	resp := f.validateResp
	if resp.SchemaVersion == 0 {
		resp.SchemaVersion = 1
	}
	return resp, nil
}

func (f *fakeFederationRESTStore) RegisterWorkspace(_ context.Context, row handlers.WorkspaceRESTRow) error {
	f.registered = append(f.registered, row)
	f.workspaces = append(f.workspaces, row)
	return nil
}

func (f *fakeFederationRESTStore) ListWorkspaces(_ context.Context) ([]handlers.WorkspaceRESTRow, error) {
	return f.workspaces, nil
}

func (f *fakeFederationRESTStore) ListWorkspaceMembers(_ context.Context, workspaceID string) ([]handlers.WorkspaceMemberRESTRow, error) {
	return f.members[workspaceID], nil
}

func (f *fakeFederationRESTStore) AddWorkspaceMember(_ context.Context, row handlers.WorkspaceMemberRESTRow) error {
	f.added = append(f.added, row)
	if f.members == nil {
		f.members = map[string][]handlers.WorkspaceMemberRESTRow{}
	}
	f.members[row.WorkspaceID] = append(f.members[row.WorkspaceID], row)
	return nil
}

func (f *fakeFederationRESTStore) RemoveWorkspace(_ context.Context, workspaceID string) (int64, error) {
	f.removed = append(f.removed, workspaceID)
	return 1, nil
}

func (f *fakeFederationRESTStore) GetWorkspacePolicy(_ context.Context, workspaceID string) (string, error) {
	return f.policies[workspaceID], nil
}

func (f *fakeFederationRESTStore) SetWorkspacePolicy(_ context.Context, workspaceID, policy string) error {
	if f.policies == nil {
		f.policies = map[string]string{}
	}
	f.policies[workspaceID] = policy
	return nil
}

func TestContractRESTHappyPath(t *testing.T) {
	gw := fakeGw(t,
		func(toolName string, args map[string]any, _ http.Header) {
			if toolName != "mcp_zen-swarm_caronte_get_contract" {
				t.Errorf("tool = %q; want mcp_zen-swarm_caronte_get_contract", toolName)
			}
			if args["endpoint"] != "endpoint-1" {
				t.Errorf("args.endpoint = %v; want endpoint-1", args["endpoint"])
			}
		},
		func(_ string) (any, *jsonrpcErrorPayload) {
			return map[string]any{
				"endpoint_id":     "endpoint-1",
				"repo":            "repo-a",
				"kind":            "http",
				"method":          "GET",
				"path_template":   "/users/{id}",
				"handler_node_id": "node-1",
				"extracted_at":    1700000000,
				"extractor_id":    "oasdiff",
			}, nil
		})
	h := handlers.ContractREST(&fakeGwCtx{h: gw})
	body, _ := json.Marshal(handlers.ContractRESTRequest{Endpoint: "endpoint-1"})
	r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/contract", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200", rec.Code)
	}
	var resp handlers.ContractRESTResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.EndpointID != "endpoint-1" || resp.Method != "GET" {
		t.Errorf("resp = %+v", resp)
	}
}

func TestContractRESTMissingEndpoint(t *testing.T) {
	h := handlers.ContractREST(&fakeGwCtx{h: http.NotFoundHandler()})
	body, _ := json.Marshal(handlers.ContractRESTRequest{})
	r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/contract", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d; want 400", rec.Code)
	}
}

func TestContractRESTGatewayUnconfigured(t *testing.T) {
	h := handlers.ContractREST(&fakeGwCtx{h: nil})
	body, _ := json.Marshal(handlers.ContractRESTRequest{Endpoint: "x"})
	r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/contract", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d; want 503", rec.Code)
	}
}

func TestContractRESTMethodNotAllowed(t *testing.T) {
	h := handlers.ContractREST(&fakeGwCtx{h: http.NotFoundHandler()})
	r := httptest.NewRequest(http.MethodGet, "/v1/mcpgateway/contract", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d; want 405", rec.Code)
	}
}

func TestContractWhyRESTHappyPath(t *testing.T) {
	gw := fakeGw(t,
		func(toolName string, args map[string]any, _ http.Header) {
			if toolName != "mcp_zen-swarm_caronte_get_why_breaking_change" {
				t.Errorf("tool = %q", toolName)
			}
			if args["change"] != "chg-1" {
				t.Errorf("args.change = %v", args["change"])
			}
		},
		func(_ string) (any, *jsonrpcErrorPayload) {
			return map[string]any{
				"change_id":       "chg-1",
				"workspace_id":    "ws-1",
				"endpoint_id":     "endpoint-1",
				"endpoint_repo":   "repo-a",
				"lore_author":     "alice@example.com",
				"lore_commit_sha": "abc1234",
				"lore_adr_refs":   []string{"ADR-0114"},
				"detected_at":     1700000000,
			}, nil
		})
	h := handlers.ContractWhyREST(&fakeGwCtx{h: gw})
	body, _ := json.Marshal(handlers.ContractWhyRESTRequest{ChangeID: "chg-1"})
	r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/contract/why", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var resp handlers.ContractWhyRESTResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.LoreAuthor != "alice@example.com" {
		t.Errorf("LoreAuthor = %q", resp.LoreAuthor)
	}
}

func TestContractValidateRESTUsesManifestValidator(t *testing.T) {
	fed := &fakeFederationRESTStore{validateResp: handlers.ContractValidateRESTResponse{
		Valid:         true,
		SchemaVersion: 1,
		Services: []handlers.ContractValidateRESTService{
			{BaseURLRef: "${BACKEND_URL}", TargetRepo: "backend"},
		},
	}}
	h := handlers.ContractValidateREST(&fakePlan20RESTCtx{h: http.NotFoundHandler(), fed: fed})
	body, _ := json.Marshal(handlers.ContractValidateRESTRequest{Repo: "/tmp/repo"})
	r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/contract/validate", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp handlers.ContractValidateRESTResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Valid || resp.SchemaVersion != 1 {
		t.Fatalf("resp = %+v; want valid schema v1", resp)
	}
	if len(resp.Services) != 1 || resp.Services[0].TargetRepo != "backend" {
		t.Errorf("services = %+v", resp.Services)
	}
}

func TestFederationHealthRESTHappyPath(t *testing.T) {
	gw := fakeGw(t,
		func(toolName string, args map[string]any, _ http.Header) {
			if toolName != "mcp_zen-swarm_caronte_federation_health" {
				t.Errorf("tool = %q", toolName)
			}
			if args["workspace"] != "ws-1" {
				t.Errorf("args.workspace = %v", args["workspace"])
			}
		},
		func(_ string) (any, *jsonrpcErrorPayload) {
			return map[string]any{
				"workspace_id":         "ws-1",
				"reachable":            true,
				"gate_latency_p95_ms":  1.2,
				"contract_links_count": 5,
			}, nil
		})
	h := handlers.FederationHealthREST(&fakeGwCtx{h: gw})
	body, _ := json.Marshal(handlers.FederationHealthRESTRequest{WorkspaceID: "ws-1"})
	r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/federation/health", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var resp handlers.FederationHealthRESTResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Reachable || resp.GateLatencyP95Ms != 1.2 {
		t.Errorf("resp = %+v", resp)
	}
}

func TestFederationHealthRESTDaemonWide(t *testing.T) {
	gw := fakeGw(t,
		func(_ string, args map[string]any, _ http.Header) {

			if _, has := args["workspace"]; has {
				t.Errorf("args has workspace = %v; want absent (daemon-wide)", args["workspace"])
			}
		},
		func(_ string) (any, *jsonrpcErrorPayload) {
			return map[string]any{"workspace_id": "", "reachable": true}, nil
		})
	h := handlers.FederationHealthREST(&fakeGwCtx{h: gw})
	body, _ := json.Marshal(handlers.FederationHealthRESTRequest{})
	r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/federation/health", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestAPIImpactRESTUsesBreakingChangesFanout(t *testing.T) {
	gw := fakeGw(t,
		func(toolName string, args map[string]any, _ http.Header) {
			if toolName != "mcp_zen-swarm_caronte_get_breaking_changes" {
				t.Errorf("tool = %q; want mcp_zen-swarm_caronte_get_breaking_changes", toolName)
			}
			if args["workspace"] != "ws-1" {
				t.Errorf("args.workspace = %v; want ws-1", args["workspace"])
			}
		},
		func(_ string) (any, *jsonrpcErrorPayload) {
			return []map[string]any{
				{
					"change_id":     "chg-1",
					"workspace_id":  "ws-1",
					"endpoint_id":   "endpoint-1",
					"endpoint_repo": "repo-api",
					"kind":          "removed_endpoint",
					"detector_id":   "oasdiff",
					"detected_at":   int64(1700000000),
					"consumers": []map[string]any{
						{"repo": "repo-web", "call_id": "call-1"},
						{"repo": "repo-cli", "call_id": "call-2"},
					},
				},
			}, nil
		})
	h := handlers.APIImpactREST(&fakeGwCtx{h: gw})
	body, _ := json.Marshal(handlers.APIImpactRESTRequest{DiffRef: "HEAD~3..HEAD", WorkspaceID: "ws-1"})
	r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/api-impact", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp handlers.APIImpactRESTResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.DiffRef != "HEAD~3..HEAD" || resp.WorkspaceID != "ws-1" {
		t.Errorf("resp identity = (%q,%q); want (HEAD~3..HEAD,ws-1)", resp.DiffRef, resp.WorkspaceID)
	}
	if resp.AffectedCount != 2 {
		t.Fatalf("AffectedCount = %d; want 2; resp=%+v", resp.AffectedCount, resp)
	}
	got := []string{resp.Consumers[0].Repo + "/" + resp.Consumers[0].CallID, resp.Consumers[1].Repo + "/" + resp.Consumers[1].CallID}
	want := []string{"repo-web/call-1", "repo-cli/call-2"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("consumer[%d] = %q; want %q", i, got[i], want[i])
		}
	}
	for i, consumer := range resp.Consumers {
		if consumer.Severity != "BREAKING" {
			t.Errorf("consumer[%d].Severity = %q; want BREAKING", i, consumer.Severity)
		}
	}
}

func TestWorkspaceLifecycleRoutesUseFederationStore(t *testing.T) {
	fed := &fakeFederationRESTStore{
		members:  map[string][]handlers.WorkspaceMemberRESTRow{},
		policies: map[string]string{"ws-1": "locked"},
	}
	ctx := &fakePlan20RESTCtx{h: http.NotFoundHandler(), fed: fed}

	post := func(h http.HandlerFunc, path string, body any) *httptest.ResponseRecorder {
		t.Helper()
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d; want 200; body=%s", path, rec.Code, rec.Body.String())
		}
		return rec
	}

	initRec := post(handlers.WorkspaceInitREST(ctx), "/v1/mcpgateway/workspace/init",
		handlers.WorkspaceInitRESTRequest{WorkspaceID: "ws-1", OwningProject: "proj-a", Members: []string{"proj-b"}, PolicyLocked: true})
	var initResp handlers.WorkspaceInitRESTResponse
	if err := json.Unmarshal(initRec.Body.Bytes(), &initResp); err != nil {
		t.Fatalf("decode init: %v", err)
	}
	if initResp.WorkspaceID != "ws-1" || initResp.SchemaVersion != 1 {
		t.Fatalf("initResp = %+v", initResp)
	}
	if len(fed.registered) != 1 || !fed.registered[0].PolicyLocked {
		t.Fatalf("registered = %+v; want one locked workspace", fed.registered)
	}
	if got := []string{fed.added[0].ProjectID, fed.added[1].ProjectID}; got[0] != "proj-a" || got[1] != "proj-b" {
		t.Fatalf("init members = %+v; want owner then explicit member", got)
	}

	post(handlers.WorkspaceLinkREST(ctx), "/v1/mcpgateway/workspace/link",
		handlers.WorkspaceLinkRESTRequest{WorkspaceID: "ws-1", ProjectID: "proj-c"})

	listRec := post(handlers.WorkspaceListREST(ctx), "/v1/mcpgateway/workspace/list", map[string]any{})
	var listResp handlers.WorkspaceListRESTResponse
	if err := json.Unmarshal(listRec.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listResp.Workspaces) != 1 || listResp.Workspaces[0].WorkspaceID != "ws-1" {
		t.Fatalf("listResp = %+v", listResp)
	}

	membersRec := post(handlers.WorkspaceMembersREST(ctx), "/v1/mcpgateway/workspace/members",
		handlers.WorkspaceMembersRESTRequest{WorkspaceID: "ws-1"})
	var membersResp handlers.WorkspaceMembersRESTResponse
	if err := json.Unmarshal(membersRec.Body.Bytes(), &membersResp); err != nil {
		t.Fatalf("decode members: %v", err)
	}
	if len(membersResp.Members) != 3 {
		t.Fatalf("members = %+v; want owner + explicit + linked", membersResp.Members)
	}

	policySetRec := post(handlers.WorkspacePolicySetREST(ctx), "/v1/mcpgateway/workspace/policy/set",
		handlers.WorkspacePolicySetRESTRequest{WorkspaceID: "ws-1", NewPolicy: "permissive"})
	var policySetResp handlers.WorkspacePolicySetRESTResponse
	if err := json.Unmarshal(policySetRec.Body.Bytes(), &policySetResp); err != nil {
		t.Fatalf("decode policy set: %v", err)
	}
	if policySetResp.NewPolicy != "permissive" || fed.policies["ws-1"] != "permissive" {
		t.Fatalf("policySetResp = %+v policies=%+v", policySetResp, fed.policies)
	}

	policyGetRec := post(handlers.WorkspacePolicyGetREST(ctx), "/v1/mcpgateway/workspace/policy/get",
		handlers.WorkspacePolicyGetRESTRequest{WorkspaceID: "ws-1"})
	var policyGetResp handlers.WorkspacePolicyGetRESTResponse
	if err := json.Unmarshal(policyGetRec.Body.Bytes(), &policyGetResp); err != nil {
		t.Fatalf("decode policy get: %v", err)
	}
	if policyGetResp.Policy != "permissive" {
		t.Fatalf("policyGetResp = %+v", policyGetResp)
	}

	removeRec := post(handlers.WorkspaceRemoveREST(ctx), "/v1/mcpgateway/workspace/remove",
		handlers.WorkspaceRemoveRESTRequest{WorkspaceID: "ws-1"})
	var removeResp handlers.WorkspaceRemoveRESTResponse
	if err := json.Unmarshal(removeRec.Body.Bytes(), &removeResp); err != nil {
		t.Fatalf("decode remove: %v", err)
	}
	if removeResp.RowsAffected != 1 || len(fed.removed) != 1 || fed.removed[0] != "ws-1" {
		t.Fatalf("removeResp = %+v removed=%+v", removeResp, fed.removed)
	}
}
