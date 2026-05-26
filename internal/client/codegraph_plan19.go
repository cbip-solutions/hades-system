// SPDX-License-Identifier: MIT
// Package client — codegraph_plan19.go (Plan 19 Phase K).
//
// Thin pass-throughs for the daemon's Plan 19 caronte REST sub-routes:
// /v1/mcpgateway/{why,risk,cochange,impl}. The daemon side (Phase K Task
// K-5, handlers/mcpgateway_rest.go) translates each into a JSON-RPC
// tools/call against the now-native Caronte engine (Phase J backed the
// caronte segment with the engine). CLI is operator-side; LLM traffic is
// not involved (these are structural queries, not generation).
//
// inv-zen-088 single-egress preserved: every round-trip proxies through
// the daemon. inv-zen-129 enforced: this file uses ONLY c.postJSON /
// c.postJSONH — never net/http directly.
//
// ProjectAlias as the canonical X-Zen-Project-ID header so the daemon
// mcpgateway alias resolver picks it up per MCP protocol convention.
// Body still includes ProjectAlias (Phase A body-fallback compat).
package client

import "context"

type WhyRequest struct {
	Symbol       string `json:"symbol"`
	ProjectAlias string `json:"project_alias,omitempty"`
}

type WhyLinkedADR struct {
	ADRID      string  `json:"adr_id"`
	ADRTitle   string  `json:"adr_title"`
	LinkKind   string  `json:"link_kind"`
	Confidence float64 `json:"confidence"`
	Stale      bool    `json:"stale"`
}

type WhySemanticPassage struct {
	SourceID   string  `json:"source_id"`
	SourceKind string  `json:"source_kind"`
	Text       string  `json:"text"`
	Score      float64 `json:"score"`
}

type WhyLoreEntry struct {
	CommitSHA   string `json:"commit_sha"`
	TrailerKind string `json:"trailer_kind"`
	Body        string `json:"body"`
	AuthoredAt  int64  `json:"authored_at"`
}

type WhyResponse struct {
	Subject          string               `json:"subject"`
	LinkedADRs       []WhyLinkedADR       `json:"linked_adrs"`
	SemanticPassages []WhySemanticPassage `json:"semantic_passages"`
	LoreTrailers     []WhyLoreEntry       `json:"lore_trailers"`
	Degraded         bool                 `json:"degraded"`
}

func (c *Client) Why(ctx context.Context, req WhyRequest) (*WhyResponse, error) {
	var resp WhyResponse
	if err := c.postJSONH(ctx, "/v1/mcpgateway/why", projectAliasHeaders(req.ProjectAlias), req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

type RiskRequest struct {
	ChangedSymbols []string `json:"changed_symbols,omitempty"`
	ChangedFiles   []string `json:"changed_files,omitempty"`
	ProjectAlias   string   `json:"project_alias,omitempty"`
}

type RiskResponse struct {
	Level       string   `json:"level"`
	Score       float64  `json:"score"`
	Cone        float64  `json:"cone"`
	Coreness    float64  `json:"coreness"`
	Churn       float64  `json:"churn"`
	Coupling    float64  `json:"coupling"`
	TopAffected []string `json:"top_affected"`
}

func (c *Client) Risk(ctx context.Context, req RiskRequest) (*RiskResponse, error) {
	var resp RiskResponse
	if err := c.postJSONH(ctx, "/v1/mcpgateway/risk", projectAliasHeaders(req.ProjectAlias), req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

type CoChangeRequest struct {
	File         string `json:"file"`
	ProjectAlias string `json:"project_alias,omitempty"`
}

type CoChangePeerDTO struct {
	Path            string  `json:"path"`
	CouplingPercent float64 `json:"coupling_percent"`
	SharedRevs      int     `json:"shared_revs"`
	WindowDays      int     `json:"window_days"`
}

type CoChangeResponse struct {
	File  string            `json:"file"`
	Peers []CoChangePeerDTO `json:"peers"`
}

func (c *Client) CoChange(ctx context.Context, req CoChangeRequest) (*CoChangeResponse, error) {
	var resp CoChangeResponse
	if err := c.postJSONH(ctx, "/v1/mcpgateway/cochange", projectAliasHeaders(req.ProjectAlias), req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

type ImplRequest struct {
	Interface    string `json:"interface"`
	ProjectAlias string `json:"project_alias,omitempty"`
}

type ImplDTO struct {
	InterfaceID string `json:"interface_id"`
	ImplID      string `json:"impl_id"`
	Confidence  string `json:"confidence"`
	Reachable   bool   `json:"reachable"`
}

type ImplResponse struct {
	Interface       string    `json:"interface"`
	Implementations []ImplDTO `json:"implementations"`
}

func (c *Client) Impl(ctx context.Context, req ImplRequest) (*ImplResponse, error) {
	var resp ImplResponse
	if err := c.postJSONH(ctx, "/v1/mcpgateway/impl", projectAliasHeaders(req.ProjectAlias), req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

type CaronteHealthRequest struct {
	ProjectAlias string `json:"project_alias,omitempty"`
}

type CaronteHealthResponse struct {
	ProjectID    string   `json:"project_id"`
	NodeCount    int      `json:"node_count"`
	EdgeCount    int      `json:"edge_count"`
	PackageCount int      `json:"package_count"`
	CyclicSCCs   int      `json:"cyclic_sccs"`
	Languages    []string `json:"languages,omitempty"`
	Degraded     bool     `json:"degraded"`
	ResolveMode  string   `json:"resolve_mode,omitempty"`
	LastIndexed  int64    `json:"last_indexed"`
}

func (c *Client) CaronteHealth(ctx context.Context, req CaronteHealthRequest) (*CaronteHealthResponse, error) {
	var resp CaronteHealthResponse
	if err := c.postJSONH(ctx, "/v1/mcpgateway/health", projectAliasHeaders(req.ProjectAlias), req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
