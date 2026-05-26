// SPDX-License-Identifier: MIT
// Package client — knowledge_aggregator.go (Plan 9 Phase D-12).
//
// Five typed methods wrapping the five Plan 9 daemon aggregator routes:
//
//	AggQuery     POST /v1/knowledge/aggregator/query
//	AggPromote   POST /v1/knowledge/aggregator/promote
//	AggUnpromote POST /v1/knowledge/aggregator/unpromote
//	AggList      GET  /v1/knowledge/aggregator/list
//	AggRebuild   POST /v1/knowledge/aggregator/rebuild
//
// These are DISTINCT from the Plan 7 knowledge client methods
// (KnowledgeQuery/KnowledgeReindex/KnowledgeStats in knowledge.go) which
// back /v1/knowledge/{query,reindex,stats}. Both surfaces coexist; Plan 9
// adds the aggregator-specific routes.
//
// Wire types mirror handlers/knowledge_aggregator.go exactly (single
// source of truth on field names + JSON tags). The client does NOT import
// the aggregator package — it decodes into its own local types (AggXxxReq /
// AggXxxResp) to keep the client package free of CGO dependencies.
//
// inv-zen-129: this file imports stdlib only (context + time); the *Client
// transport handles net/http. No aggregator or internal/store imports.
package client

import (
	"context"
	"fmt"
	"net/url"
	"time"
)

type AggQueryRequest struct {
	Text      string `json:"text"`
	Scope     string `json:"scope,omitempty"`
	ProjectID string `json:"project_id,omitempty"`
	Limit     int    `json:"limit,omitempty"`
}

type AggQueryResponse struct {
	Results []AggQueryResultRow `json:"results"`
}

type AggQueryResultRow struct {
	NoteID           string  `json:"note_id"`
	Title            string  `json:"title"`
	ProjectID        string  `json:"project_id"`
	Score            float64 `json:"score"`
	Snippet          string  `json:"snippet,omitempty"`
	AuditChainAnchor string  `json:"audit_chain_anchor,omitempty"`
	Source           string  `json:"source"`
}

type AggPromoteRequest struct {
	NoteID     string `json:"note_id"`
	ProjectID  string `json:"project_id"`
	OperatorID string `json:"operator_id"`
	Reason     string `json:"reason"`
}

type AggPromoteResponse struct {
	NoteID           string    `json:"note_id"`
	AuditChainAnchor string    `json:"audit_chain_anchor"`
	PromotedAt       time.Time `json:"promoted_at"`
	Idempotent       bool      `json:"idempotent,omitempty"`
}

type AggUnpromoteRequest struct {
	NoteID     string `json:"note_id"`
	ProjectID  string `json:"project_id,omitempty"`
	OperatorID string `json:"operator_id"`
	Reason     string `json:"reason"`
}

type AggUnpromoteResponse struct {
	NoteID       string    `json:"note_id"`
	UnpromotedAt time.Time `json:"unpromoted_at"`
	Idempotent   bool      `json:"idempotent,omitempty"`
}

type AggListResponse struct {
	Notes []AggPinNote `json:"notes"`
}

type AggPinNote struct {
	NoteID           string    `json:"note_id"`
	ProjectID        string    `json:"project_id"`
	Title            string    `json:"title"`
	Content          string    `json:"content"`
	FrontmatterJSON  string    `json:"frontmatter_json"`
	PromotedAt       time.Time `json:"promoted_at"`
	PromotedBy       string    `json:"promoted_by"`
	PromoteReason    string    `json:"promote_reason"`
	AuditChainAnchor string    `json:"audit_chain_anchor"`
}

type AggRebuildResponse struct {
	Status    string `json:"status"`
	ProjectID string `json:"project_id,omitempty"`
}

func (c *Client) AggQuery(ctx context.Context, req AggQueryRequest) ([]AggQueryResultRow, error) {
	var resp AggQueryResponse
	if err := c.postJSON(ctx, "/v1/knowledge/aggregator/query", req, &resp); err != nil {
		return nil, err
	}
	if resp.Results == nil {
		return []AggQueryResultRow{}, nil
	}
	return resp.Results, nil
}

func (c *Client) AggPromote(ctx context.Context, req AggPromoteRequest) (*AggPromoteResponse, error) {
	var resp AggPromoteResponse
	if err := c.postJSON(ctx, "/v1/knowledge/aggregator/promote", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) AggUnpromote(ctx context.Context, req AggUnpromoteRequest) (*AggUnpromoteResponse, error) {
	var resp AggUnpromoteResponse
	if err := c.postJSON(ctx, "/v1/knowledge/aggregator/unpromote", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) AggList(ctx context.Context, projectID string, _ bool) ([]AggPinNote, error) {
	path := "/v1/knowledge/aggregator/list"
	if projectID != "" {
		path = fmt.Sprintf("%s?%s", path, url.Values{"project_id": {projectID}}.Encode())
	}
	var resp AggListResponse
	if err := c.getJSON(ctx, path, &resp); err != nil {
		return nil, err
	}
	if resp.Notes == nil {
		return []AggPinNote{}, nil
	}
	return resp.Notes, nil
}

func (c *Client) AggRebuild(ctx context.Context, projectID string) error {
	req := struct {
		ProjectID string `json:"project_id,omitempty"`
	}{ProjectID: projectID}
	var resp AggRebuildResponse
	if err := c.postJSON(ctx, "/v1/knowledge/aggregator/rebuild", req, &resp); err != nil {
		return err
	}
	return nil
}
