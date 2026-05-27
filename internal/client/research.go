// SPDX-License-Identifier: MIT
// Package client — research.go.
//
// Typed wrappers for /v1/research/cache/* endpoints.
//
// The plan-doc additionally describes /v1/research/dispatch and
// /v1/research/agentic-deep streaming endpoints; these are scheduled
// for (research MCP wiring) and intentionally NOT exposed by
// the daemon today. adapts: ship the cache admin surface as
// the operator-reachable research subset, deliver real CLI commands
// with real round-trips. Dispatch/agentic-deep land in a follow-up
// phase WITHOUT changing this client wrapper (additive only).
package client

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

type ResearchCacheEntry struct {
	Hash      string `json:"hash"`
	BytesSize int64  `json:"bytes_size"`
	CreatedAt int64  `json:"created_at"`
	TTLUnix   int64  `json:"ttl_unix"`
}

type ResearchCacheStats struct {
	TotalEntries int   `json:"total_entries"`
	TotalBytes   int64 `json:"total_bytes"`
	OldestUnix   int64 `json:"oldest_unix"`
	NewestUnix   int64 `json:"newest_unix"`
	ExpiredCount int   `json:"expired_count"`
}

type ResearchCacheShow struct {
	Hash         string `json:"hash"`
	ResponseJSON string `json:"response_json"`
	BytesSize    int64  `json:"bytes_size"`
	CreatedAt    int64  `json:"created_at"`
	TTLUnix      int64  `json:"ttl_unix"`
	Expired      bool   `json:"expired"`
}

type ResearchCacheGetResp struct {
	Hit          bool   `json:"hit"`
	ResponseJSON string `json:"response_json,omitempty"`
	TTLUnix      int64  `json:"ttl_unix,omitempty"`
}

type ResearchCacheSetReq struct {
	Hash         string `json:"hash"`
	ResponseJSON string `json:"response_json"`
	TTLSeconds   int64  `json:"ttl_seconds,omitempty"`
}

type ResearchCacheSetResp struct {
	Stored  bool  `json:"stored"`
	TTLUnix int64 `json:"ttl_unix"`
}

func (c *Client) ResearchCacheGet(ctx context.Context, hash string) (*ResearchCacheGetResp, error) {
	if hash == "" {
		return nil, fmt.Errorf("hash required")
	}
	q := url.Values{"hash": []string{hash}}
	var out ResearchCacheGetResp
	err := c.getJSON(ctx, "/v1/research/cache/get?"+q.Encode(), &out)
	if err != nil {

		if errIsNotFound(err) {
			return &ResearchCacheGetResp{Hit: false}, nil
		}
		return nil, err
	}
	return &out, nil
}

func (c *Client) ResearchCacheSet(ctx context.Context, req ResearchCacheSetReq) (*ResearchCacheSetResp, error) {
	var out ResearchCacheSetResp
	if err := c.postJSON(ctx, "/v1/research/cache/set", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ResearchCacheList(ctx context.Context, limit, offset int) ([]ResearchCacheEntry, error) {
	q := url.Values{}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	if offset > 0 {
		q.Set("offset", strconv.Itoa(offset))
	}
	var out struct {
		Items []ResearchCacheEntry `json:"items"`
		Count int                  `json:"count"`
	}
	path := "/v1/research/cache/list"
	if e := q.Encode(); e != "" {
		path += "?" + e
	}
	if err := c.getJSON(ctx, path, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

func (c *Client) ResearchCacheClear(ctx context.Context, olderThan time.Duration) (int64, error) {
	if olderThan <= 0 {
		return 0, fmt.Errorf("olderThan must be > 0")
	}
	body := map[string]int64{"older_than_seconds": int64(olderThan.Seconds())}
	var out struct {
		Deleted int64 `json:"deleted"`
	}
	if err := c.postJSON(ctx, "/v1/research/cache/clear", body, &out); err != nil {
		return 0, err
	}
	return out.Deleted, nil
}

func (c *Client) ResearchCacheStatsCall(ctx context.Context) (*ResearchCacheStats, error) {
	var out ResearchCacheStats
	if err := c.getJSON(ctx, "/v1/research/cache/stats", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ResearchCacheShow(ctx context.Context, hash string) (*ResearchCacheShow, bool, error) {
	if hash == "" {
		return nil, false, fmt.Errorf("hash required")
	}
	q := url.Values{"hash": []string{hash}}
	var out ResearchCacheShow
	err := c.getJSON(ctx, "/v1/research/cache/show?"+q.Encode(), &out)
	if err != nil {
		if errIsNotFound(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return &out, true, nil
}

func errIsNotFound(err error) bool {
	if err == nil {
		return false
	}
	if IsHTTPStatus(err, 404) {
		return true
	}
	s := err.Error()
	for i := 0; i+5 < len(s); i++ {
		if s[i] == ':' && s[i+1] == ' ' && s[i+2] == '4' && s[i+3] == '0' && s[i+4] == '4' {
			return true
		}
	}
	return false
}

type ResearchSource struct {
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description" yaml:"description"`
	Source      string `json:"source" yaml:"source"`
}

// researchSourceDescriptions maps a source name to its operator-visible
// description. New sources MUST be registered here so they surface in
// `hades research sources` with a meaningful description.
var researchSourceDescriptions = map[string]string{
	"web_search":     "Generic web search (doctrine-configurable backend)",
	"arxiv":          "arXiv academic paper search",
	"github_search":  "GitHub code + repo search",
	"code_graph":     "Cross-project code knowledge graph (caronte)",
	"ecosystem_docs": "Pinned ecosystem documentation (Go, Python, ...)",
}

func ResearchSourcesFromList(sources []string) []ResearchSource {
	out := make([]ResearchSource, 0, len(sources))
	for _, n := range sources {
		desc := researchSourceDescriptions[n]
		if desc == "" {
			desc = fmt.Sprintf("%s research source", n)
		}
		out = append(out, ResearchSource{
			Name:        n,
			Description: desc,
			Source:      "doctrine",
		})
	}
	return out
}

func ResearchSourcesDefault() []ResearchSource {
	return ResearchSourcesFromList([]string{"web_search", "arxiv", "github_search"})
}

func (c *Client) ResearchSourcesResolve(ctx context.Context) ([]ResearchSource, error) {
	state, err := c.DoctrineStateCall(ctx)
	if err != nil {
		return ResearchSourcesDefault(), nil
	}
	if pool := extractResearchSources(state); len(pool) > 0 {
		return ResearchSourcesFromList(pool), nil
	}
	return ResearchSourcesDefault(), nil
}

func (c *Client) ResearchSourcesResolveFromState(state DoctrineState) ([]ResearchSource, bool) {
	if pool := extractResearchSources(state); len(pool) > 0 {
		return ResearchSourcesFromList(pool), true
	}
	return nil, false
}

func extractResearchSources(state DoctrineState) []string {
	if pool := poolFromKeys(state, "research", "sources"); pool != nil {
		return pool
	}
	if pool := poolFromKeys(state, "Research", "Sources"); pool != nil {
		return pool
	}
	return nil
}
