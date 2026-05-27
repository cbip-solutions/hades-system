// SPDX-License-Identifier: MIT
// Package client — knowledge_p9.go.
//
// 5 methods on *Client wrapping the endpoints surfaced at
// /v1/knowledge/{query,promote,unpromote,list,rebuild}. Wire types mirror
// internal/daemon/handlers/knowledge_p9.go; the "P9" suffix is dropped on
// the type names at the client side per the convention established in
// audit_p9.go (client types are unsuffixed; handler types keep the suffix
// to coexist with legacy types inside the daemon package).
//
// Method names carry the P9 suffix to disambiguate from the
// KnowledgeQuery/KnowledgeReindex/KnowledgeStats methods in knowledge.go
// which back the legacy /v1/knowledge/{query,reindex,stats} POST routes.
// The H-2 routes use GET for query/list and POST for the mutating
// endpoints — the HTTP method alone would not resolve the Go method name
// collision, hence the explicit P9 suffix.
//
// invariant boundary: this file imports stdlib only (context, net/url,
// strconv). No internal/daemon, internal/store, or internal/knowledge
// imports. The *Client transport handles net/http.
package client

import (
	"context"
	"net/url"
	"strconv"
)

type KnowledgeQueryReq struct {
	Q          string
	Scope      string
	ProjectID  string
	PinnedOnly bool
	AuditChain bool
	Limit      int
}

type KnowledgeResult struct {
	NoteID           string  `json:"note_id"`
	ProjectID        string  `json:"project_id,omitempty"`
	Path             string  `json:"path,omitempty"`
	Snippet          string  `json:"snippet,omitempty"`
	Score            float64 `json:"score"`
	AuditChainAnchor string  `json:"audit_chain_anchor,omitempty"`
	ChainProof       string  `json:"audit_chain_proof,omitempty"`
}

type KnowledgeNote struct {
	NoteID    string `json:"note_id"`
	ProjectID string `json:"project_id,omitempty"`
	Path      string `json:"path,omitempty"`
	Pinned    bool   `json:"pinned"`
	UpdatedAt int64  `json:"updated_at_unix,omitempty"`
}

type KnowledgeRebuildResp struct {
	JobID        string `json:"job_id"`
	StartedAt    int64  `json:"started_at_unix,omitempty"`
	RebuiltCount int    `json:"rebuilt_count,omitempty"`
}

func (c *Client) KnowledgeQueryP9(ctx context.Context, req KnowledgeQueryReq) ([]KnowledgeResult, error) {
	q := url.Values{}
	q.Set("q", req.Q)
	if req.Scope != "" {
		q.Set("scope", req.Scope)
	}
	if req.ProjectID != "" {
		q.Set("project_id", req.ProjectID)
	}
	if req.PinnedOnly {
		q.Set("pinned_only", "true")
	}
	if req.AuditChain {
		q.Set("audit_chain", "true")
	}
	if req.Limit > 0 {
		q.Set("limit", strconv.Itoa(req.Limit))
	}
	var out struct {
		Items []KnowledgeResult `json:"items"`
		Count int               `json:"count"`
	}
	if err := c.getJSON(ctx, "/v1/knowledge/query?"+q.Encode(), &out); err != nil {
		return nil, err
	}
	if out.Items == nil {
		out.Items = []KnowledgeResult{}
	}
	return out.Items, nil
}

func (c *Client) KnowledgePromoteP9(ctx context.Context, noteID, reason string) error {
	return c.KnowledgePromoteProjectP9(ctx, noteID, "", reason)
}

func (c *Client) KnowledgePromoteProjectP9(ctx context.Context, noteID, projectID, reason string) error {
	body := map[string]any{"note_id": noteID, "reason": reason}
	if projectID != "" {
		body["project_id"] = projectID
	}
	return c.postJSON(ctx, "/v1/knowledge/promote", body, nil)
}

func (c *Client) KnowledgeUnpromoteP9(ctx context.Context, noteID, reason string) error {
	return c.KnowledgeUnpromoteProjectP9(ctx, noteID, "", reason)
}

func (c *Client) KnowledgeUnpromoteProjectP9(ctx context.Context, noteID, projectID, reason string) error {
	body := map[string]any{"note_id": noteID, "reason": reason}
	if projectID != "" {
		body["project_id"] = projectID
	}
	return c.postJSON(ctx, "/v1/knowledge/unpromote", body, nil)
}

func (c *Client) KnowledgeListP9(ctx context.Context, projectID string, pinnedOnly bool) ([]KnowledgeNote, error) {
	q := url.Values{}
	if projectID != "" {
		q.Set("project_id", projectID)
	}
	if pinnedOnly {
		q.Set("pinned_only", "true")
	}
	path := "/v1/knowledge/list"
	if e := q.Encode(); e != "" {
		path += "?" + e
	}
	var out struct {
		Items []KnowledgeNote `json:"items"`
		Count int             `json:"count"`
	}
	if err := c.getJSON(ctx, path, &out); err != nil {
		return nil, err
	}
	if out.Items == nil {
		out.Items = []KnowledgeNote{}
	}
	return out.Items, nil
}

func (c *Client) KnowledgeRebuildP9(ctx context.Context, projectID string) (KnowledgeRebuildResp, error) {
	body := map[string]any{"project_id": projectID}
	var out KnowledgeRebuildResp
	if err := c.postJSON(ctx, "/v1/knowledge/rebuild", body, &out); err != nil {
		return KnowledgeRebuildResp{}, err
	}
	return out, nil
}
