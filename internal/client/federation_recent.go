// SPDX-License-Identifier: MIT
// Package client — federation_recent.go.
//
// client extensions for the F7 TUI Contract Federation
// sub-panel: FederationRecentBreakingChanges + FederationRecentDispatches.
// Both proxy through the daemon's narrow ContractFederationForDaemon
// + ContractCoordinatorForDaemon interfaces ( wiring; data
// originates from the WorkspaceFederationDB +
// OrchestratorCoordinator).
//
// # Routes
//
// POST /v1/mcpgateway/federation/recent-breaking-changes
// POST /v1/mcpgateway/federation/recent-dispatches
//
// inv-hades-088 single-egress preserved: every round-trip proxies through
// the daemon. inv-hades-129 enforced: this file uses ONLY c.postJSON —
// never net/http directly.
package client

import "context"

type FederationRecentBreakingChangesRequest struct {
	WorkspaceID string `json:"workspace_id,omitempty"`
	Limit       int    `json:"limit,omitempty"`
}

type FederationBreakingChangeEntry struct {
	ChangeID      string `json:"change_id"`
	WorkspaceID   string `json:"workspace_id"`
	EndpointID    string `json:"endpoint_id"`
	EndpointRepo  string `json:"endpoint_repo"`
	Kind          string `json:"kind"`
	Severity      string `json:"severity,omitempty"`
	DetectedAt    int64  `json:"detected_at"`
	DetectorID    string `json:"detector_id,omitempty"`
	LoreAuthor    string `json:"lore_author,omitempty"`
	LoreCommitSHA string `json:"lore_commit_sha,omitempty"`
}

type FederationRecentBreakingChangesResponse struct {
	WorkspaceID string                          `json:"workspace_id"`
	Changes     []FederationBreakingChangeEntry `json:"changes"`
}

func (c *Client) FederationRecentBreakingChanges(ctx context.Context, req FederationRecentBreakingChangesRequest) (*FederationRecentBreakingChangesResponse, error) {
	var resp FederationRecentBreakingChangesResponse
	if err := c.postJSON(ctx, "/v1/mcpgateway/federation/recent-breaking-changes", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

type FederationRecentDispatchesRequest struct {
	Limit int `json:"limit,omitempty"`
}

type FederationDispatchDecisionEntry struct {
	ChangeID        string   `json:"change_id"`
	Mode            string   `json:"mode"`
	DispatchedRepos []string `json:"dispatched_repos"`
	AuditID         string   `json:"audit_id"`
	DecidedAt       int64    `json:"decided_at"`
}

type FederationRecentDispatchesResponse struct {
	Decisions []FederationDispatchDecisionEntry `json:"decisions"`
}

func (c *Client) FederationRecentDispatches(ctx context.Context, req FederationRecentDispatchesRequest) (*FederationRecentDispatchesResponse, error) {
	var resp FederationRecentDispatchesResponse
	if err := c.postJSON(ctx, "/v1/mcpgateway/federation/recent-dispatches", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
