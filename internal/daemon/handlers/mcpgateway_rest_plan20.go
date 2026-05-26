// SPDX-License-Identifier: MIT
// Package handlers — mcpgateway_rest_plan20.go (Plan 20 Phase I Task I-15).
//
// REST sub-route adapters for the Plan-20 federation surface:
//
//	POST /v1/mcpgateway/contract                         → caronte get_contract
//	POST /v1/mcpgateway/contract/validate                → caronte.yaml validator (Phase F linker)
//	POST /v1/mcpgateway/contract/why                     → caronte get_why_breaking_change
//	POST /v1/mcpgateway/workspace/init                   → federation register_workspace
//	POST /v1/mcpgateway/workspace/list                   → federation list_workspaces
//	POST /v1/mcpgateway/workspace/members                → federation list_workspace_members
//	POST /v1/mcpgateway/workspace/link                   → federation add_member
//	POST /v1/mcpgateway/workspace/remove                 → federation remove_workspace
//	POST /v1/mcpgateway/workspace/policy/get             → federation get_workspace_policy
//	POST /v1/mcpgateway/workspace/policy/set             → federation set_workspace_policy
//	POST /v1/mcpgateway/federation/health                → caronte federation_health
//	POST /v1/mcpgateway/api-impact                       → caronte contract_diff + consumers fan-out
//
// The 4 contract-* + federation-health + api-impact routes are read paths
// that dispatch to the caronte engine tools via callGatewayRaw. The 7
// workspace routes are write/lifecycle paths that the daemon's federation
// substrate handles; for the engine read paths (workspace list/members),
// the daemon would dispatch through caronte tools — but the lifecycle
// surfaces (init/link/remove/policy/set) are direct federation-store calls
// + Plan-14 audit emission.
//
// Phase J wiring registers each route on the daemon mux. Phase H may
// refactor the lifecycle routes once the engine method bodies grow real
// composite logic.
//
// inv-zen-031 boundary: this file imports stdlib only; types are local
// anonymous decode targets parsed from the JSON the gateway returns.
// inv-zen-088 single-egress: every route proxies through the gateway.

package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
)

type ContractRESTRequest struct {
	Endpoint    string `json:"endpoint"`
	WorkspaceID string `json:"workspace_id,omitempty"`
}

type ContractRESTResponse struct {
	EndpointID       string `json:"endpoint_id"`
	Repo             string `json:"repo"`
	Kind             string `json:"kind"`
	Method           string `json:"method,omitempty"`
	PathTemplate     string `json:"path_template,omitempty"`
	ProtoService     string `json:"proto_service,omitempty"`
	ProtoRPC         string `json:"proto_rpc,omitempty"`
	Topic            string `json:"topic,omitempty"`
	GraphQLType      string `json:"graphql_type,omitempty"`
	GraphQLField     string `json:"graphql_field,omitempty"`
	HandlerNodeID    string `json:"handler_node_id"`
	ContractArtifact string `json:"contract_artifact,omitempty"`
	ExtractedAt      int64  `json:"extracted_at"`
	ExtractorID      string `json:"extractor_id"`
}

func ContractREST(ctx MCPGatewayCtx) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		gw := ctx.MCPGateway()
		if gw == nil {
			http.Error(w, "mcpgateway not configured", http.StatusServiceUnavailable)
			return
		}
		var req ContractRESTRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(req.Endpoint) == "" {
			http.Error(w, "endpoint required", http.StatusBadRequest)
			return
		}
		args := map[string]any{"endpoint": req.Endpoint}
		if req.WorkspaceID != "" {
			args["workspace"] = req.WorkspaceID
		}
		raw, status, err := callGatewayRaw(r.Context(), gw, "mcp_zen-swarm_caronte_get_contract", args, r.Header)
		if err != nil {
			http.Error(w, err.Error(), status)
			return
		}
		var out ContractRESTResponse
		if err := json.Unmarshal(raw, &out); err != nil {
			http.Error(w, "decode contract payload: "+err.Error(), http.StatusBadGateway)
			return
		}
		writeJSON(w, http.StatusOK, out)
	}
}

type ContractValidateRESTRequest struct {
	Repo string `json:"repo"`
}

type ContractValidateRESTService struct {
	BaseURLRef string `json:"base_url_ref"`
	TargetRepo string `json:"target_repo"`
}

type ContractValidateRESTError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Path    string `json:"path,omitempty"`
}

type ContractValidateRESTResponse struct {
	Valid         bool                          `json:"valid"`
	SchemaVersion int                           `json:"schema_version"`
	Services      []ContractValidateRESTService `json:"services,omitempty"`
	Errors        []ContractValidateRESTError   `json:"errors,omitempty"`
}

func ContractValidateREST(ctx MCPGatewayCtx) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		gw := ctx.MCPGateway()
		if gw == nil {
			http.Error(w, "mcpgateway not configured", http.StatusServiceUnavailable)
			return
		}
		var req ContractValidateRESTRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(req.Repo) == "" {
			http.Error(w, "repo required", http.StatusBadRequest)
			return
		}

		http.Error(w, "contract validator not wired (phase j scope)", http.StatusServiceUnavailable)
	}
}

type ContractWhyRESTRequest struct {
	ChangeID string `json:"change_id"`
}

type ContractWhyRESTResponse struct {
	ChangeID          string   `json:"change_id"`
	WorkspaceID       string   `json:"workspace_id"`
	EndpointID        string   `json:"endpoint_id"`
	EndpointRepo      string   `json:"endpoint_repo"`
	LoreAuthor        string   `json:"lore_author,omitempty"`
	LoreCommitSHA     string   `json:"lore_commit_sha,omitempty"`
	LoreADRRefs       []string `json:"lore_adr_refs,omitempty"`
	LoreSupersedes    []string `json:"lore_supersedes,omitempty"`
	CommitSubject     string   `json:"commit_subject,omitempty"`
	CommitBodyExcerpt string   `json:"commit_body_excerpt,omitempty"`
	DetectedAt        int64    `json:"detected_at"`
}

func ContractWhyREST(ctx MCPGatewayCtx) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		gw := ctx.MCPGateway()
		if gw == nil {
			http.Error(w, "mcpgateway not configured", http.StatusServiceUnavailable)
			return
		}
		var req ContractWhyRESTRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(req.ChangeID) == "" {
			http.Error(w, "change_id required", http.StatusBadRequest)
			return
		}
		args := map[string]any{"change": req.ChangeID}
		raw, status, err := callGatewayRaw(r.Context(), gw, "mcp_zen-swarm_caronte_get_why_breaking_change", args, r.Header)
		if err != nil {
			http.Error(w, err.Error(), status)
			return
		}
		var out ContractWhyRESTResponse
		if err := json.Unmarshal(raw, &out); err != nil {
			http.Error(w, "decode why payload: "+err.Error(), http.StatusBadGateway)
			return
		}
		writeJSON(w, http.StatusOK, out)
	}
}

type WorkspaceInitRESTRequest struct {
	WorkspaceID   string   `json:"workspace_id"`
	OwningProject string   `json:"owning_project"`
	Members       []string `json:"members,omitempty"`
	PolicyLocked  bool     `json:"policy_locked"`
}

func WorkspaceInitREST(ctx MCPGatewayCtx) http.HandlerFunc {
	return workspaceLifecycleREST(ctx, "workspace_init")
}

func WorkspaceListREST(ctx MCPGatewayCtx) http.HandlerFunc {
	return workspaceLifecycleREST(ctx, "workspace_list")
}

func WorkspaceMembersREST(ctx MCPGatewayCtx) http.HandlerFunc {
	return workspaceLifecycleREST(ctx, "workspace_members")
}

func WorkspaceLinkREST(ctx MCPGatewayCtx) http.HandlerFunc {
	return workspaceLifecycleREST(ctx, "workspace_link")
}

func WorkspaceRemoveREST(ctx MCPGatewayCtx) http.HandlerFunc {
	return workspaceLifecycleREST(ctx, "workspace_remove")
}

func WorkspacePolicyGetREST(ctx MCPGatewayCtx) http.HandlerFunc {
	return workspaceLifecycleREST(ctx, "workspace_policy_get")
}

func WorkspacePolicySetREST(ctx MCPGatewayCtx) http.HandlerFunc {
	return workspaceLifecycleREST(ctx, "workspace_policy_set")
}

func workspaceLifecycleREST(ctx MCPGatewayCtx, op string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		gw := ctx.MCPGateway()
		if gw == nil {
			http.Error(w, "mcpgateway not configured", http.StatusServiceUnavailable)
			return
		}

		var raw json.RawMessage
		_ = json.NewDecoder(r.Body).Decode(&raw)
		http.Error(w, "federation lifecycle not wired ("+op+", phase j scope)", http.StatusServiceUnavailable)
	}
}

type FederationHealthRESTRequest struct {
	WorkspaceID string `json:"workspace_id,omitempty"`
}

type FederationHealthRESTResponse struct {
	WorkspaceID               string  `json:"workspace_id"`
	Reachable                 bool    `json:"reachable"`
	GateLatencyP95Ms          float64 `json:"gate_latency_p95_ms"`
	IndexingCurrencyMaxAgeSec int64   `json:"indexing_currency_max_age_sec"`
	UnresolvedCount           int     `json:"unresolved_count"`
	ContractLinksCount        int     `json:"contract_links_count"`
	BreakingChangesOpenCount  int     `json:"breaking_changes_open_count"`
	LastAuditChainTip         string  `json:"last_audit_chain_tip,omitempty"`
}

func FederationHealthREST(ctx MCPGatewayCtx) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		gw := ctx.MCPGateway()
		if gw == nil {
			http.Error(w, "mcpgateway not configured", http.StatusServiceUnavailable)
			return
		}
		var req FederationHealthRESTRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		args := map[string]any{}
		if req.WorkspaceID != "" {
			args["workspace"] = req.WorkspaceID
		}
		raw, status, err := callGatewayRaw(r.Context(), gw, "mcp_zen-swarm_caronte_federation_health", args, r.Header)
		if err != nil {
			http.Error(w, err.Error(), status)
			return
		}
		var out FederationHealthRESTResponse
		if err := json.Unmarshal(raw, &out); err != nil {
			http.Error(w, "decode federation_health payload: "+err.Error(), http.StatusBadGateway)
			return
		}
		writeJSON(w, http.StatusOK, out)
	}
}

type APIImpactRESTRequest struct {
	DiffRef     string `json:"diff_ref"`
	WorkspaceID string `json:"workspace_id,omitempty"`
}

type APIImpactRESTConsumer struct {
	Repo     string `json:"repo"`
	CallID   string `json:"call_id"`
	Severity string `json:"severity"`
}

type APIImpactRESTResponse struct {
	DiffRef       string                  `json:"diff_ref"`
	WorkspaceID   string                  `json:"workspace_id,omitempty"`
	AffectedCount int                     `json:"affected_count"`
	Consumers     []APIImpactRESTConsumer `json:"consumers"`
}

func APIImpactREST(ctx MCPGatewayCtx) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		gw := ctx.MCPGateway()
		if gw == nil {
			http.Error(w, "mcpgateway not configured", http.StatusServiceUnavailable)
			return
		}
		var req APIImpactRESTRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(req.DiffRef) == "" {
			http.Error(w, "diff_ref required", http.StatusBadRequest)
			return
		}
		http.Error(w, "api-impact not wired (phase j scope)", http.StatusServiceUnavailable)
	}
}
