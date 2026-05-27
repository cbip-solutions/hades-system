// SPDX-License-Identifier: MIT
// Package client — knowledge.go.
//
// Three methods + supporting wire types for the daemon's
// /v1/knowledge/* surface backing the operator-facing
// `hades knowledge` CLI:
//
// KnowledgeQuery POST /v1/knowledge/query — hybrid FTS5 + structured filter
// KnowledgeReindex POST /v1/knowledge/reindex — cold rebuild dispatch
// KnowledgeStats GET /v1/knowledge/stats — index statistics
//
// Field names + JSON tags align with the daemon-side handler in
// internal/daemon/handlers/knowledge.go. Times use Unix seconds at the
// wire boundary (rather than RFC3339) for the stats payload — the
// LastIndexedUnix is a single scalar, kept compact for shell pipelines
// (`jq.last_indexed_unix`). The query payload's LastModified is RFC3339
// per Go's encoding/json default for time.Time.
//
// Wire shapes are decoupled from internal/knowledge.Result at the client
// layer (the typed FileType field there is a domain enum); the CLI re-
// hydrates rows back into a domain-friendly form for rendering — keeping
// the wire boundary at strings, the domain layer at typed enums.
//
// inv-hades-129 boundary: this file imports stdlib + context + encoding/json
// only — never net/http directly (the *Client transport handles it).
// `--remote` and `--audit-chain` are intercepted at the CLI BEFORE this
// layer runs, so neither flag ever reaches a wire payload (per spec §1
// Q17 + the G-12/G-13 sentinel-anchor contract).
package client

import (
	"context"
	"time"
)

type KnowledgeQueryRequest struct {
	FreeText     string   `json:"free_text,omitempty"`
	ProjectAlias []string `json:"project_alias,omitempty"`
	Type         []string `json:"type,omitempty"`
	SinceSeconds int64    `json:"since_seconds,omitempty"`
	Limit        int      `json:"limit,omitempty"`
	CodeSymbol   string   `json:"code_symbol,omitempty"`
	Realtime     bool     `json:"realtime,omitempty"`
	CrossProject bool     `json:"cross_project,omitempty"`
}

type KnowledgeQueryResponse struct {
	Rows []KnowledgeResultRow `json:"rows"`
}

type KnowledgeResultRow struct {
	FilePath     string    `json:"file_path"`
	ProjectID    string    `json:"project_id"`
	ProjectAlias string    `json:"project_alias"`
	FileType     string    `json:"file_type"`
	Title        string    `json:"title"`
	LastModified time.Time `json:"last_modified"`
	Score        float64   `json:"score"`
	Snippet      string    `json:"snippet"`
}

type KnowledgeReindexRequest struct {
	Full         bool   `json:"full,omitempty"`
	ProjectAlias string `json:"project_alias,omitempty"`
}

type KnowledgeReindexResponse struct {
	OK      bool `json:"ok"`
	Indexed int  `json:"indexed"`
	Errors  int  `json:"errors,omitempty"`
}

type KnowledgeStatsResponse struct {
	TotalDocs       int            `json:"total_docs"`
	ByType          map[string]int `json:"by_type"`
	LastIndexedUnix int64          `json:"last_indexed_unix"`
}

func (c *Client) KnowledgeQuery(ctx context.Context, req KnowledgeQueryRequest) ([]KnowledgeResultRow, error) {
	var resp KnowledgeQueryResponse
	if err := c.postJSON(ctx, "/v1/knowledge/query", req, &resp); err != nil {
		return nil, err
	}
	if resp.Rows == nil {
		return []KnowledgeResultRow{}, nil
	}
	return resp.Rows, nil
}

func (c *Client) KnowledgeReindex(ctx context.Context, req KnowledgeReindexRequest) (*KnowledgeReindexResponse, error) {
	var resp KnowledgeReindexResponse
	if err := c.postJSON(ctx, "/v1/knowledge/reindex", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) KnowledgeStats(ctx context.Context) (*KnowledgeStatsResponse, error) {
	var resp KnowledgeStatsResponse
	if err := c.getJSON(ctx, "/v1/knowledge/stats", &resp); err != nil {
		return nil, err
	}
	if resp.ByType == nil {
		resp.ByType = map[string]int{}
	}
	return &resp, nil
}

type KnowledgePromoteRequest struct {
	ID          string `json:"id"`
	GlobalScope bool   `json:"global_scope"`
}

type KnowledgePromoteResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Scope  string `json:"scope"`
}

func (c *Client) KnowledgePromote(ctx context.Context, req KnowledgePromoteRequest) (*KnowledgePromoteResponse, error) {
	var resp KnowledgePromoteResponse
	if err := c.postJSON(ctx, "/v1/knowledge/promote", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

type KnowledgeSyncRequest struct {
	ProjectAlias string `json:"project_alias,omitempty"`
	Verify       bool   `json:"verify,omitempty"`
}

type KnowledgeSyncResponse struct {
	RowsIndexed int   `json:"rows_indexed"`
	DurationMs  int64 `json:"duration_ms"`
	VerifyDelta int   `json:"verify_delta,omitempty"`
}

func (c *Client) KnowledgeSync(ctx context.Context, req KnowledgeSyncRequest) (*KnowledgeSyncResponse, error) {
	var resp KnowledgeSyncResponse
	if err := c.postJSON(ctx, "/v1/knowledge/sync", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

type KnowledgeRestoreRequest struct {
	ProjectAlias string `json:"project_alias"`
	Timestamp    string `json:"timestamp,omitempty"`
	DryRun       bool   `json:"dry_run,omitempty"`
}

type KnowledgeRestoreResponse struct {
	ProjectAlias string `json:"project_alias"`
	SnapshotID   string `json:"snapshot_id"`
	RowsRestored int    `json:"rows_restored"`
	DurationMs   int64  `json:"duration_ms"`
}

func (c *Client) KnowledgeRestore(ctx context.Context, req KnowledgeRestoreRequest) (*KnowledgeRestoreResponse, error) {
	var resp KnowledgeRestoreResponse
	if err := c.postJSON(ctx, "/v1/knowledge/restore", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
