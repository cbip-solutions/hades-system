// SPDX-License-Identifier: MIT
// Package handlers — mcpgateway_rest_plan20.go.
//
// REST sub-route adapters for the federation surface:
//
// POST /v1/mcpgateway/contract → caronte get_contract
// POST /v1/mcpgateway/contract/validate → caronte.yaml validator
// POST /v1/mcpgateway/contract/why → caronte get_why_breaking_change
// POST /v1/mcpgateway/workspace/init → federation register_workspace
// POST /v1/mcpgateway/workspace/list → federation list_workspaces
// POST /v1/mcpgateway/workspace/members → federation list_workspace_members
// POST /v1/mcpgateway/workspace/link → federation add_member
// POST /v1/mcpgateway/workspace/remove → federation remove_workspace
// POST /v1/mcpgateway/workspace/policy/get → federation get_workspace_policy
// POST /v1/mcpgateway/workspace/policy/set → federation set_workspace_policy
// POST /v1/mcpgateway/federation/health → caronte federation_health
// POST /v1/mcpgateway/api-impact → caronte get_breaking_changes + consumers fan-out
//
// The contract read routes + federation-health + api-impact routes dispatch
// to the caronte engine tools via callGatewayRaw. Contract validation and
// all 7 workspace lifecycle routes are direct daemon-federation calls so
// workspace register/list/members/link/remove/policy state is read from the
// same substrate that persists the mutations and emits audit leaves.
//
// wiring registers each route on the daemon mux. may
// refactor the lifecycle routes once the engine method bodies grow real
// composite logic.
//
// invariant boundary: this file imports stdlib only; the daemon package
// adapts concrete federation dependencies behind local interfaces.
// invariant single-egress: external callers still enter through the daemon;
// read routes proxy through the MCP gateway and lifecycle routes mutate the
// daemon-owned federation store directly.

package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

type ContractFederationRESTStore interface {
	ValidateContractManifest(ctx context.Context, repo, workspaceID string) (ContractValidateRESTResponse, error)
	RegisterWorkspace(ctx context.Context, row WorkspaceRESTRow) error
	ListWorkspaces(ctx context.Context) ([]WorkspaceRESTRow, error)
	ListWorkspaceMembers(ctx context.Context, workspaceID string) ([]WorkspaceMemberRESTRow, error)
	AddWorkspaceMember(ctx context.Context, row WorkspaceMemberRESTRow) error
	RemoveWorkspace(ctx context.Context, workspaceID string) (int64, error)
	GetWorkspacePolicy(ctx context.Context, workspaceID string) (string, error)
	SetWorkspacePolicy(ctx context.Context, workspaceID, policy string) error
}

type contractFederationRESTCtx interface {
	ContractFederationREST() ContractFederationRESTStore
}

func contractFederationStore(ctx MCPGatewayCtx) ContractFederationRESTStore {
	if ctx == nil {
		return nil
	}
	provider, ok := ctx.(contractFederationRESTCtx)
	if !ok {
		return nil
	}
	return provider.ContractFederationREST()
}

func decodeJSONBody(r *http.Request, dst any) error {
	err := json.NewDecoder(r.Body).Decode(dst)
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

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
	Repo        string `json:"repo"`
	WorkspaceID string `json:"workspace_id,omitempty"`
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
		var req ContractValidateRESTRequest
		if err := decodeJSONBody(r, &req); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		repo := strings.TrimSpace(req.Repo)
		if repo == "" {
			http.Error(w, "repo required", http.StatusBadRequest)
			return
		}
		store := contractFederationStore(ctx)
		if store == nil {
			http.Error(w, "contract federation not configured", http.StatusServiceUnavailable)
			return
		}
		out, err := store.ValidateContractManifest(r.Context(), repo, strings.TrimSpace(req.WorkspaceID))
		if err != nil {
			http.Error(w, "contract validate: "+err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, out)
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

// --- workspace lifecycle (7 routes) ---
//
// The workspace lifecycle routes write directly to the daemon's federation
// substrate. They intentionally do not
// proxy through the MCP gateway because they mutate the daemon-owned
// workspace ledger and, for policy changes, the store emits the audit
// leaf through its federation audit emitter.

type WorkspaceRESTRow struct {
	WorkspaceID   string `json:"workspace_id"`
	OwningProject string `json:"owning_project"`
	PolicyLocked  bool   `json:"policy_locked"`
	CreatedAt     int64  `json:"created_at"`
	SchemaVersion int    `json:"schema_version"`
}

type WorkspaceMemberRESTRow struct {
	WorkspaceID  string `json:"workspace_id"`
	ProjectID    string `json:"project_id"`
	RegisteredAt int64  `json:"registered_at"`
}

type WorkspaceInitRESTRequest struct {
	WorkspaceID   string   `json:"workspace_id"`
	OwningProject string   `json:"owning_project"`
	Members       []string `json:"members,omitempty"`
	PolicyLocked  bool     `json:"policy_locked"`
}

type WorkspaceInitRESTResponse struct {
	WorkspaceID   string `json:"workspace_id"`
	CreatedAt     int64  `json:"created_at"`
	SchemaVersion int    `json:"schema_version"`
}

type WorkspaceListRESTRequest struct{}

type WorkspaceListRESTResponse struct {
	Workspaces []WorkspaceRESTRow `json:"workspaces"`
}

type WorkspaceMembersRESTRequest struct {
	WorkspaceID string `json:"workspace_id"`
}

type WorkspaceMembersRESTResponse struct {
	Members []WorkspaceMemberRESTRow `json:"members"`
}

type WorkspaceLinkRESTRequest struct {
	WorkspaceID string `json:"workspace_id"`
	ProjectID   string `json:"project_id"`
}

type WorkspaceLinkRESTResponse struct {
	WorkspaceID  string `json:"workspace_id"`
	ProjectID    string `json:"project_id"`
	RegisteredAt int64  `json:"registered_at"`
}

type WorkspaceRemoveRESTRequest struct {
	WorkspaceID string `json:"workspace_id"`
}

type WorkspaceRemoveRESTResponse struct {
	WorkspaceID  string `json:"workspace_id"`
	RowsAffected int64  `json:"rows_affected"`
}

type WorkspacePolicyGetRESTRequest struct {
	WorkspaceID string `json:"workspace_id"`
}

type WorkspacePolicyGetRESTResponse struct {
	WorkspaceID string `json:"workspace_id"`
	Policy      string `json:"policy"`
}

type WorkspacePolicySetRESTRequest struct {
	WorkspaceID string `json:"workspace_id"`
	NewPolicy   string `json:"new_policy"`
}

type WorkspacePolicySetRESTResponse struct {
	WorkspaceID string `json:"workspace_id"`
	NewPolicy   string `json:"new_policy"`
}

func WorkspaceInitREST(ctx MCPGatewayCtx) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		store := contractFederationStore(ctx)
		if store == nil {
			http.Error(w, "contract federation not configured", http.StatusServiceUnavailable)
			return
		}
		var req WorkspaceInitRESTRequest
		if err := decodeJSONBody(r, &req); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		workspaceID := strings.TrimSpace(req.WorkspaceID)
		owner := strings.TrimSpace(req.OwningProject)
		if workspaceID == "" {
			http.Error(w, "workspace_id required", http.StatusBadRequest)
			return
		}
		if owner == "" {
			http.Error(w, "owning_project required", http.StatusBadRequest)
			return
		}
		createdAt := time.Now().Unix()
		row := WorkspaceRESTRow{
			WorkspaceID:   workspaceID,
			OwningProject: owner,
			PolicyLocked:  req.PolicyLocked,
			CreatedAt:     createdAt,
			SchemaVersion: 1,
		}
		if err := store.RegisterWorkspace(r.Context(), row); err != nil {
			http.Error(w, "workspace init: "+err.Error(), http.StatusInternalServerError)
			return
		}
		seen := map[string]bool{owner: true}
		members := []string{owner}
		for _, raw := range req.Members {
			member := strings.TrimSpace(raw)
			if member == "" || seen[member] {
				continue
			}
			seen[member] = true
			members = append(members, member)
		}
		for _, member := range members {
			if err := store.AddWorkspaceMember(r.Context(), WorkspaceMemberRESTRow{
				WorkspaceID: workspaceID, ProjectID: member, RegisteredAt: createdAt,
			}); err != nil {
				_, _ = store.RemoveWorkspace(r.Context(), workspaceID)
				http.Error(w, "workspace init member: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}
		writeJSON(w, http.StatusOK, WorkspaceInitRESTResponse{
			WorkspaceID: workspaceID, CreatedAt: createdAt, SchemaVersion: row.SchemaVersion,
		})
	}
}

func WorkspaceListREST(ctx MCPGatewayCtx) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		store := contractFederationStore(ctx)
		if store == nil {
			http.Error(w, "contract federation not configured", http.StatusServiceUnavailable)
			return
		}
		var req WorkspaceListRESTRequest
		if err := decodeJSONBody(r, &req); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		rows, err := store.ListWorkspaces(r.Context())
		if err != nil {
			http.Error(w, "workspace list: "+err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, WorkspaceListRESTResponse{Workspaces: rows})
	}
}

func WorkspaceMembersREST(ctx MCPGatewayCtx) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		store := contractFederationStore(ctx)
		if store == nil {
			http.Error(w, "contract federation not configured", http.StatusServiceUnavailable)
			return
		}
		var req WorkspaceMembersRESTRequest
		if err := decodeJSONBody(r, &req); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		workspaceID := strings.TrimSpace(req.WorkspaceID)
		if workspaceID == "" {
			http.Error(w, "workspace_id required", http.StatusBadRequest)
			return
		}
		rows, err := store.ListWorkspaceMembers(r.Context(), workspaceID)
		if err != nil {
			http.Error(w, "workspace members: "+err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, WorkspaceMembersRESTResponse{Members: rows})
	}
}

func WorkspaceLinkREST(ctx MCPGatewayCtx) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		store := contractFederationStore(ctx)
		if store == nil {
			http.Error(w, "contract federation not configured", http.StatusServiceUnavailable)
			return
		}
		var req WorkspaceLinkRESTRequest
		if err := decodeJSONBody(r, &req); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		workspaceID := strings.TrimSpace(req.WorkspaceID)
		projectID := strings.TrimSpace(req.ProjectID)
		if workspaceID == "" {
			http.Error(w, "workspace_id required", http.StatusBadRequest)
			return
		}
		if projectID == "" {
			http.Error(w, "project_id required", http.StatusBadRequest)
			return
		}
		registeredAt := time.Now().Unix()
		row := WorkspaceMemberRESTRow{WorkspaceID: workspaceID, ProjectID: projectID, RegisteredAt: registeredAt}
		if err := store.AddWorkspaceMember(r.Context(), row); err != nil {
			http.Error(w, "workspace link: "+err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, WorkspaceLinkRESTResponse{
			WorkspaceID: workspaceID, ProjectID: projectID, RegisteredAt: registeredAt,
		})
	}
}

func WorkspaceRemoveREST(ctx MCPGatewayCtx) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		store := contractFederationStore(ctx)
		if store == nil {
			http.Error(w, "contract federation not configured", http.StatusServiceUnavailable)
			return
		}
		var req WorkspaceRemoveRESTRequest
		if err := decodeJSONBody(r, &req); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		workspaceID := strings.TrimSpace(req.WorkspaceID)
		if workspaceID == "" {
			http.Error(w, "workspace_id required", http.StatusBadRequest)
			return
		}
		n, err := store.RemoveWorkspace(r.Context(), workspaceID)
		if err != nil {
			http.Error(w, "workspace remove: "+err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, WorkspaceRemoveRESTResponse{WorkspaceID: workspaceID, RowsAffected: n})
	}
}

func WorkspacePolicyGetREST(ctx MCPGatewayCtx) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		store := contractFederationStore(ctx)
		if store == nil {
			http.Error(w, "contract federation not configured", http.StatusServiceUnavailable)
			return
		}
		var req WorkspacePolicyGetRESTRequest
		if err := decodeJSONBody(r, &req); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		workspaceID := strings.TrimSpace(req.WorkspaceID)
		if workspaceID == "" {
			http.Error(w, "workspace_id required", http.StatusBadRequest)
			return
		}
		policy, err := store.GetWorkspacePolicy(r.Context(), workspaceID)
		if err != nil {
			http.Error(w, "workspace policy get: "+err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, WorkspacePolicyGetRESTResponse{WorkspaceID: workspaceID, Policy: policy})
	}
}

func WorkspacePolicySetREST(ctx MCPGatewayCtx) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		store := contractFederationStore(ctx)
		if store == nil {
			http.Error(w, "contract federation not configured", http.StatusServiceUnavailable)
			return
		}
		var req WorkspacePolicySetRESTRequest
		if err := decodeJSONBody(r, &req); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		workspaceID := strings.TrimSpace(req.WorkspaceID)
		policy := strings.TrimSpace(req.NewPolicy)
		if workspaceID == "" {
			http.Error(w, "workspace_id required", http.StatusBadRequest)
			return
		}
		if policy == "" {
			http.Error(w, "new_policy required", http.StatusBadRequest)
			return
		}
		if err := store.SetWorkspacePolicy(r.Context(), workspaceID, policy); err != nil {
			http.Error(w, "workspace policy set: "+err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, WorkspacePolicySetRESTResponse{WorkspaceID: workspaceID, NewPolicy: policy})
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

type apiImpactBreakingChange struct {
	ChangeID    string `json:"change_id"`
	WorkspaceID string `json:"workspace_id"`
	Kind        string `json:"kind"`
	Consumers   []struct {
		Repo   string `json:"repo"`
		CallID string `json:"call_id"`
	} `json:"consumers"`
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
		workspaceID := strings.TrimSpace(req.WorkspaceID)
		if workspaceID == "" {
			workspaceID = "default"
		}
		args := map[string]any{"workspace": workspaceID}
		raw, status, err := callGatewayRaw(r.Context(), gw, "mcp_zen-swarm_caronte_get_breaking_changes", args, r.Header)
		if err != nil {
			http.Error(w, err.Error(), status)
			return
		}
		var changes []apiImpactBreakingChange
		if err := json.Unmarshal(raw, &changes); err != nil {
			http.Error(w, "decode get_breaking_changes payload: "+err.Error(), http.StatusBadGateway)
			return
		}
		out := APIImpactRESTResponse{
			DiffRef:     req.DiffRef,
			WorkspaceID: workspaceID,
			Consumers:   apiImpactConsumers(req.DiffRef, changes),
		}
		out.AffectedCount = len(out.Consumers)
		writeJSON(w, http.StatusOK, out)
	}
}

func apiImpactConsumers(diffRef string, changes []apiImpactBreakingChange) []APIImpactRESTConsumer {
	selector := strings.TrimSpace(diffRef)
	wantChangeID, filterByChangeID := strings.CutPrefix(selector, "change:")
	if !filterByChangeID {
		for _, ch := range changes {
			if ch.ChangeID == selector {
				wantChangeID = selector
				filterByChangeID = true
				break
			}
		}
	}
	out := make([]APIImpactRESTConsumer, 0)
	index := map[string]int{}
	for _, ch := range changes {
		if filterByChangeID {
			if ch.ChangeID != wantChangeID {
				continue
			}
		}
		severity := apiImpactSeverity(ch.Kind)
		for _, c := range ch.Consumers {
			key := c.Repo + "\x00" + c.CallID
			if pos, ok := index[key]; ok {
				if apiImpactSeverityRank(severity) > apiImpactSeverityRank(out[pos].Severity) {
					out[pos].Severity = severity
				}
				continue
			}
			index[key] = len(out)
			out = append(out, APIImpactRESTConsumer{
				Repo:     c.Repo,
				CallID:   c.CallID,
				Severity: severity,
			})
		}
	}
	return out
}

func apiImpactSeverity(kind string) string {
	switch kind {
	case "param_added_optional", "deprecation_announced", "extension_added",
		"DIRECTIVE_USAGE_REMOVED", "ENUM_VALUE_SAME_NAME":
		return "DANGEROUS"
	default:
		return "BREAKING"
	}
}

func apiImpactSeverityRank(severity string) int {
	switch severity {
	case "BREAKING":
		return 3
	case "DANGEROUS":
		return 2
	default:
		return 1
	}
}
