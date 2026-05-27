// SPDX-License-Identifier: MIT
// Package client — research_p9.go.
//
// 4 typed wrappers for the release research endpoints declared in
// internal/daemon/handlers/research_p9.go. Wire types mirror the handler
// declarations; duplication is intentional (client compiles standalone without
// importing internal/daemon — release N convention).
//
// GET /v1/research/history — ResearchHistory
// GET /v1/research/cache/stats — ResearchCacheStatsP9 (P9 suffix avoids
// conflict with release N ResearchCacheStatsCall)
// POST /v1/research/cache/invalidate — ResearchCacheInvalidate
// GET /v1/research/cache/list — ResearchCacheListP9 (P9 suffix avoids
// conflict with release N ResearchCacheList)
//
// inv-hades-031: this file imports stdlib only (context, net/url, strconv).
// No internal/daemon, internal/store, or internal/research imports.
//
// Method-name rationale (P9 suffix):
//
// ResearchCacheStatsCall on *Client. Go does not support method overloading;
// different parameter signatures alone do not resolve the collision. The P9
// suffix is the same pattern used in knowledge_p9.go (KnowledgeQueryP9 etc.)
// and audit_p9.go for analogous collisions.
package client

import (
	"context"
	"net/url"
	"strconv"
)

type ResearchHistoryFilter struct {
	Filter    string
	Since     int64
	ProjectID string
}

type ResearchHistoryEntry struct {
	Query         string `json:"query"`
	DispatchedAt  int64  `json:"dispatched_at_unix"`
	FindingsCount int    `json:"findings_count"`
	Source        string `json:"source,omitempty"`
}

type ResearchCacheStatsP9 struct {
	TotalEntries           int   `json:"total_entries"`
	TotalBytes             int64 `json:"total_bytes"`
	OldestUnix             int64 `json:"oldest_unix,omitempty"`
	NewestUnix             int64 `json:"newest_unix,omitempty"`
	ExpiredCount           int   `json:"expired_count,omitempty"`
	FreshnessLagSeconds    int   `json:"freshness_lag_seconds,omitempty"`
	RevalidationQueueDepth int   `json:"revalidation_queue_depth,omitempty"`
	StuckQueriesCount      int   `json:"stuck_queries_count,omitempty"`
}

type ResearchCacheEntryP9 struct {
	Hash        string `json:"hash"`
	BytesSize   int64  `json:"bytes_size,omitempty"`
	CreatedAt   int64  `json:"created_at,omitempty"`
	TTLUnix     int64  `json:"ttl_unix,omitempty"`
	SourceURL   string `json:"source_url,omitempty"`
	ContentHash string `json:"content_hash,omitempty"`
}

func (c *Client) ResearchHistory(ctx context.Context, filter ResearchHistoryFilter) ([]ResearchHistoryEntry, error) {
	q := url.Values{}
	if filter.Filter != "" {
		q.Set("filter", filter.Filter)
	}
	if filter.Since > 0 {
		q.Set("since", strconv.FormatInt(filter.Since, 10))
	}
	if filter.ProjectID != "" {
		q.Set("project_id", filter.ProjectID)
	}
	path := "/v1/research/history"
	if e := q.Encode(); e != "" {
		path += "?" + e
	}
	var out struct {
		Items []ResearchHistoryEntry `json:"items"`
		Count int                    `json:"count"`
	}
	if err := c.getJSON(ctx, path, &out); err != nil {
		return nil, err
	}
	if out.Items == nil {
		out.Items = []ResearchHistoryEntry{}
	}
	return out.Items, nil
}

func (c *Client) ResearchCacheStatsP9(ctx context.Context, projectID string) (ResearchCacheStatsP9, error) {
	q := url.Values{}
	if projectID != "" {
		q.Set("project_id", projectID)
	}
	path := "/v1/research/cache/stats"
	if e := q.Encode(); e != "" {
		path += "?" + e
	}
	var out ResearchCacheStatsP9
	if err := c.getJSON(ctx, path, &out); err != nil {
		return ResearchCacheStatsP9{}, err
	}
	return out, nil
}

func (c *Client) ResearchCacheInvalidate(ctx context.Context, query string) (int, error) {
	body := map[string]any{"query": query}
	var out struct {
		Invalidated int `json:"invalidated"`
	}
	if err := c.postJSON(ctx, "/v1/research/cache/invalidate", body, &out); err != nil {
		return 0, err
	}
	return out.Invalidated, nil
}

func (c *Client) ResearchCacheListP9(ctx context.Context, projectID, sourcePrefix string) ([]ResearchCacheEntryP9, error) {
	q := url.Values{}
	if projectID != "" {
		q.Set("project_id", projectID)
	}
	if sourcePrefix != "" {
		q.Set("source", sourcePrefix)
	}
	path := "/v1/research/cache/list"
	if e := q.Encode(); e != "" {
		path += "?" + e
	}
	var out struct {
		Items []ResearchCacheEntryP9 `json:"items"`
		Count int                    `json:"count"`
	}
	if err := c.getJSON(ctx, path, &out); err != nil {
		return nil, err
	}
	if out.Items == nil {
		out.Items = []ResearchCacheEntryP9{}
	}
	return out.Items, nil
}
