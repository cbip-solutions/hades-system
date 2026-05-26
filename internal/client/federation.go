// SPDX-License-Identifier: MIT
// Package client — federation.go (Plan 20 Phase I).
//
// Thin pass-throughs for the daemon's Plan 20 federation REST sub-routes:
// /v1/mcpgateway/federation/health and /v1/mcpgateway/api-impact. The
// daemon-side adapters translate each into a JSON-RPC tools/call against the
// caronte federation_health / contract_diff engine ops.
//
// inv-zen-088 single-egress preserved: every round-trip proxies through the
// daemon. inv-zen-129 enforced: this file uses ONLY c.postJSON — never
// net/http directly.
package client

import "context"

type FederationHealthRequest struct {
	WorkspaceID string `json:"workspace_id,omitempty"`
}

type FederationHealthResponse struct {
	WorkspaceID               string  `json:"workspace_id"`
	Reachable                 bool    `json:"reachable"`
	GateLatencyP95Ms          float64 `json:"gate_latency_p95_ms"`
	IndexingCurrencyMaxAgeSec int64   `json:"indexing_currency_max_age_sec"`
	UnresolvedCount           int     `json:"unresolved_count"`
	ContractLinksCount        int     `json:"contract_links_count"`
	BreakingChangesOpenCount  int     `json:"breaking_changes_open_count"`
	LastAuditChainTip         string  `json:"last_audit_chain_tip,omitempty"`
}

func (c *Client) FederationHealth(ctx context.Context, req FederationHealthRequest) (*FederationHealthResponse, error) {
	var resp FederationHealthResponse
	if err := c.postJSON(ctx, "/v1/mcpgateway/federation/health", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

type APIImpactRequest struct {
	DiffRef     string `json:"diff_ref"`
	WorkspaceID string `json:"workspace_id,omitempty"`
}

type APIImpactConsumer struct {
	Repo     string `json:"repo"`
	CallID   string `json:"call_id"`
	Severity string `json:"severity"`
}

type APIImpactResponse struct {
	DiffRef       string              `json:"diff_ref"`
	WorkspaceID   string              `json:"workspace_id,omitempty"`
	AffectedCount int                 `json:"affected_count"`
	Consumers     []APIImpactConsumer `json:"consumers"`
}

func (c *Client) APIImpact(ctx context.Context, req APIImpactRequest) (*APIImpactResponse, error) {
	var resp APIImpactResponse
	if err := c.postJSON(ctx, "/v1/mcpgateway/api-impact", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
