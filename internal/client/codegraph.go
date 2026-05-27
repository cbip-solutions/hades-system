// SPDX-License-Identifier: MIT
// Package client — codegraph.go
//
// /v1/mcpgateway/{codegraph,impact,context,wiki} routes. Daemon side
// ships in (mcpgateway) — these routes proxy to the in-daemon
// Caronte engine. CLI is operator-side; LLM
// traffic isn't involved (these are KG queries, not generation).
//
// ProjectAlias as the canonical `X-HADES-Project-ID` header so the daemon
// mcpgateway alias resolver picks it up per the MCP protocol convention.
// The body still includes ProjectAlias ( body-fallback path
// keeps operator-pinned behavior). CaronteProbe migrated from legacy
// GET /v1/caronte/probe to POST /v1/mcpgateway JSON-RPC tools/call.
package client

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
)

type CodegraphQueryRequest struct {
	Query        string `json:"query"`
	ProjectAlias string `json:"project_alias,omitempty"`
	Limit        int    `json:"limit,omitempty"`
}

type CodegraphHit struct {
	Symbol     string `json:"symbol"`
	File       string `json:"file"`
	Line       int    `json:"line"`
	Kind       string `json:"kind,omitempty"`
	Confidence int    `json:"confidence,omitempty"`
}

type CodegraphQueryResponse struct {
	Hits []CodegraphHit `json:"hits"`
}

func projectAliasHeaders(alias string) map[string]string {
	if alias == "" {
		return nil
	}
	return map[string]string{"X-HADES-Project-ID": alias}
}

func (c *Client) CodegraphQuery(ctx context.Context, req CodegraphQueryRequest) (*CodegraphQueryResponse, error) {
	var resp CodegraphQueryResponse
	if err := c.postJSONH(ctx, "/v1/mcpgateway/codegraph", projectAliasHeaders(req.ProjectAlias), req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

type ImpactRequest struct {
	Symbol       string `json:"symbol"`
	ProjectAlias string `json:"project_alias,omitempty"`
}

type ImpactResponse struct {
	Symbol        string   `json:"symbol"`
	BlastRadius   string   `json:"blast_radius"`
	Score         int      `json:"score"`
	AffectedFiles []string `json:"affected_files,omitempty"`
}

func (c *Client) Impact(ctx context.Context, req ImpactRequest) (*ImpactResponse, error) {
	var resp ImpactResponse
	if err := c.postJSONH(ctx, "/v1/mcpgateway/impact", projectAliasHeaders(req.ProjectAlias), req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

type Context360Request struct {
	Symbol       string `json:"symbol"`
	ProjectAlias string `json:"project_alias,omitempty"`
}

type Context360Response struct {
	Symbol    string   `json:"symbol"`
	Callers   []string `json:"callers,omitempty"`
	Callees   []string `json:"callees,omitempty"`
	Neighbors []string `json:"neighbors,omitempty"`
	Community string   `json:"community,omitempty"`

	Coreness int `json:"coreness,omitempty"`

	SCCID  int  `json:"scc_id,omitempty"`
	Cyclic bool `json:"cyclic,omitempty"`
}

func (c *Client) Context360(ctx context.Context, req Context360Request) (*Context360Response, error) {
	var resp Context360Response
	if err := c.postJSONH(ctx, "/v1/mcpgateway/context", projectAliasHeaders(req.ProjectAlias), req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

type WikiRequest struct {
	Module       string `json:"module,omitempty"`
	ProjectAlias string `json:"project_alias,omitempty"`
	Regenerate   bool   `json:"regenerate,omitempty"`
}

type WikiResponse struct {
	Module   string `json:"module,omitempty"`
	Markdown string `json:"markdown"`
}

func (c *Client) Wiki(ctx context.Context, req WikiRequest) (*WikiResponse, error) {
	var resp WikiResponse
	if err := c.postJSONH(ctx, "/v1/mcpgateway/wiki", projectAliasHeaders(req.ProjectAlias), req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

type MCPRestartResponse struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	DurationMs int64  `json:"duration_ms"`
}

func (c *Client) MCPRestart(ctx context.Context, name string) (*MCPRestartResponse, error) {
	req := struct {
		Name string `json:"name"`
	}{Name: name}
	var resp MCPRestartResponse
	if err := c.postJSON(ctx, "/v1/daemon/restart-mcp", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

type CaronteProbeResp struct {
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

const caronteProbeIndexFreshnessThreshold = 7 * 24 * time.Hour

type caronteHealthReportShape struct {
	ProjectID    string   `json:"ProjectID"`
	NodeCount    int      `json:"NodeCount"`
	EdgeCount    int      `json:"EdgeCount"`
	PackageCount int      `json:"PackageCount"`
	CyclicSCCs   int      `json:"CyclicSCCs"`
	Languages    []string `json:"Languages"`
	Degraded     bool     `json:"Degraded"`
	ResolveMode  string   `json:"ResolveMode"`
	LastIndexed  int64    `json:"LastIndexed"`
}

func (c *Client) CaronteProbe(ctx context.Context, check, projectAlias string) (*CaronteProbeResp, error) {

	args := map[string]any{}
	if projectAlias != "" {
		args["project_id"] = projectAlias
	}
	reqBody := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "mcp_hades-system_caronte_get_health",
			"arguments": args,
		},
	}
	var envelope struct {
		Result *caronteHealthReportShape `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := c.postJSONH(ctx, "/v1/mcpgateway", projectAliasHeaders(projectAlias), reqBody, &envelope); err != nil {
		return nil, err
	}
	if envelope.Error != nil {
		return nil, fmt.Errorf("caronte probe %q: %s", check, envelope.Error.Message)
	}
	if envelope.Result == nil {
		return nil, fmt.Errorf("caronte probe %q: empty result envelope", check)
	}
	return synthesizeCaronteProbeRow(check, envelope.Result), nil
}

func synthesizeCaronteProbeRow(check string, h *caronteHealthReportShape) *CaronteProbeResp {
	switch check {
	case "engine.healthy":
		if h.Degraded {
			detail := "engine reports degraded"
			if h.ResolveMode != "" {
				detail = "engine reports degraded (resolve mode: " + h.ResolveMode + ")"
			}
			return &CaronteProbeResp{Status: "fail", Detail: detail}
		}
		return &CaronteProbeResp{Status: "ok", Detail: "engine healthy"}
	case "index.freshness":
		if h.LastIndexed == 0 {
			return &CaronteProbeResp{Status: "warn", Detail: "never indexed"}
		}
		elapsed := time.Since(time.Unix(h.LastIndexed, 0))
		if elapsed > caronteProbeIndexFreshnessThreshold {
			return &CaronteProbeResp{
				Status: "warn",
				Detail: fmt.Sprintf("last indexed %s ago (threshold: %s)", elapsed.Round(time.Hour), caronteProbeIndexFreshnessThreshold),
			}
		}
		return &CaronteProbeResp{
			Status: "ok",
			Detail: fmt.Sprintf("last indexed %s ago", elapsed.Round(time.Hour)),
		}
	case "language.coverage":
		if len(h.Languages) == 0 {
			return &CaronteProbeResp{Status: "warn", Detail: "no language parser populated the index"}
		}
		return &CaronteProbeResp{
			Status: "ok",
			Detail: fmt.Sprintf("parsers active: %s", strings.Join(h.Languages, ", ")),
		}
	case "project-db.status":
		if h.ProjectID == "" {
			return &CaronteProbeResp{Status: "warn", Detail: "no project id resolved (daemon-default-project path)"}
		}
		if h.NodeCount == 0 {
			return &CaronteProbeResp{Status: "warn", Detail: "project db is empty (0 nodes)"}
		}
		return &CaronteProbeResp{
			Status: "ok",
			Detail: fmt.Sprintf("project %s: %d nodes / %d edges / %d packages", h.ProjectID, h.NodeCount, h.EdgeCount, h.PackageCount),
		}
	case "rerank.available":

		if h.Degraded {
			return &CaronteProbeResp{
				Status: "warn",
				Detail: "engine degraded; BGE reranker availability unverified. Install: scripts/download-bge-model.sh",
			}
		}
		return &CaronteProbeResp{Status: "ok", Detail: "engine non-degraded; reranker presumed operational"}
	default:
		return &CaronteProbeResp{
			Status: "warn",
			Detail: fmt.Sprintf("unknown check %q; valid: engine.healthy, index.freshness, language.coverage, project-db.status, rerank.available", check),
		}
	}
}

type CoordinationProbeResp struct {
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

func (c *Client) CoordinationProbe(ctx context.Context, check string) (*CoordinationProbeResp, error) {
	u := "/v1/coordination/probe?check=" + url.QueryEscape(check)
	var resp CoordinationProbeResp
	if err := c.getJSON(ctx, u, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
