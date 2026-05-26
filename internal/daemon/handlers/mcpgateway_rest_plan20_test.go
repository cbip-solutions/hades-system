package handlers_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/daemon/handlers"
)

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

func TestContractValidateRESTReturns503(t *testing.T) {
	h := handlers.ContractValidateREST(&fakeGwCtx{h: http.NotFoundHandler()})
	body, _ := json.Marshal(handlers.ContractValidateRESTRequest{Repo: "/tmp/repo"})
	r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/contract/validate", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d; want 503 (validator not wired in Phase I)", rec.Code)
	}
	resp, _ := io.ReadAll(rec.Body)
	if !bytes.Contains(resp, []byte("not wired")) {
		t.Errorf("body = %q; want not-wired hint", resp)
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

func TestAPIImpactRESTReturns503(t *testing.T) {
	h := handlers.APIImpactREST(&fakeGwCtx{h: http.NotFoundHandler()})
	body, _ := json.Marshal(handlers.APIImpactRESTRequest{DiffRef: "HEAD~3..HEAD"})
	r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/api-impact", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d; want 503 (api-impact not wired in Phase I)", rec.Code)
	}
}

func TestWorkspaceLifecycleRoutesReturn503(t *testing.T) {
	cases := []struct {
		name    string
		handler func(handlers.MCPGatewayCtx) http.HandlerFunc
		path    string
	}{
		{"init", handlers.WorkspaceInitREST, "/v1/mcpgateway/workspace/init"},
		{"list", handlers.WorkspaceListREST, "/v1/mcpgateway/workspace/list"},
		{"members", handlers.WorkspaceMembersREST, "/v1/mcpgateway/workspace/members"},
		{"link", handlers.WorkspaceLinkREST, "/v1/mcpgateway/workspace/link"},
		{"remove", handlers.WorkspaceRemoveREST, "/v1/mcpgateway/workspace/remove"},
		{"policy_get", handlers.WorkspacePolicyGetREST, "/v1/mcpgateway/workspace/policy/get"},
		{"policy_set", handlers.WorkspacePolicySetREST, "/v1/mcpgateway/workspace/policy/set"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := c.handler(&fakeGwCtx{h: http.NotFoundHandler()})
			r := httptest.NewRequest(http.MethodPost, c.path, bytes.NewReader([]byte("{}")))
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, r)
			if rec.Code != http.StatusServiceUnavailable {
				t.Errorf("status = %d; want 503", rec.Code)
			}
			body, _ := io.ReadAll(rec.Body)
			if !bytes.Contains(body, []byte("not wired")) {
				t.Errorf("body = %q; want not-wired hint", body)
			}
		})
	}
}
