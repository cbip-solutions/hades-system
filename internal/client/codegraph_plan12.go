// SPDX-License-Identifier: MIT
// Package client — codegraph_plan12.go (Plan 12 Phase C Task C-4).
//
// Extensions to the codegraph + augment + audit wrappers introduced in
//
// Public surface added here:
//
//	CodegraphFile     synthesizes per-file structural payload from three
//	                  mcpgateway round-trips (codegraph + context + impact).
//	                  Drives the F7 Code Graph panel's Refetch() Cmd.
//	AugmentCache      thin alias over AugmentSummary mapping the rich
//	                  daily-stats payload into the cache-stats display
//	                  schema the F3 Cost panel expects.
//	AuditEventByID    resolves zen://audit/<id> via GET /v1/audit/event/{id}.
//	                  Used by the F4 Audit panel "inspect by ID" key and
//	                  the citation envelope click-through.
//
// Wire-shape decoupling: response Render*Text helpers stay flat-string;
// no domain types leak across the boundary.
//
// inv-zen-088 single-egress preserved: all round-trips proxy through the
// daemon. inv-zen-129 enforced: this file uses only c.getJSON / c.postJSON
// / c.urlFor — never net/http directly.
package client

import (
	"context"
	"strings"
	"time"
)

type CodegraphFileResponse struct {
	Symbols []CodegraphFileSymbol `json:"symbols"`

	Callers []CodegraphFileCaller `json:"callers"`

	CommunityID string `json:"community_id"`

	CommunitySummary string `json:"community_summary"`

	Commits7d int `json:"commits_7d"`

	Authors []string `json:"authors"`

	BlastRadiusScore float64 `json:"blast_radius_score"`

	LastIndexedRFC3339 string `json:"last_indexed_rfc3339"`

	Coreness int `json:"coreness"`

	SCCID  int  `json:"scc_id"`
	Cyclic bool `json:"cyclic"`

	CoChangePeers []CodegraphCoChangePeer `json:"co_change_peers"`
}

type CodegraphFileSymbol struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	Line int    `json:"line"`
}

type CodegraphFileCaller struct {
	File     string `json:"file"`
	Symbol   string `json:"symbol"`
	Count30d int    `json:"count_30d"`
}

type CodegraphCoChangePeer struct {
	Path            string  `json:"path"`
	CouplingPercent float64 `json:"coupling_percent"`
	SharedRevs      int     `json:"shared_revs"`
}

func (c *Client) CodegraphFile(ctx context.Context, filePath string) (*CodegraphFileResponse, error) {

	qResp, err := c.CodegraphQuery(ctx, CodegraphQueryRequest{Query: filePath, Limit: 100})
	if err != nil {
		return nil, err
	}
	out := &CodegraphFileResponse{
		Symbols:       make([]CodegraphFileSymbol, 0, len(qResp.Hits)),
		Callers:       make([]CodegraphFileCaller, 0),
		Authors:       []string{},
		CoChangePeers: []CodegraphCoChangePeer{},
	}
	for _, h := range qResp.Hits {
		if h.File != "" && h.File != filePath {
			continue
		}
		out.Symbols = append(out.Symbols, CodegraphFileSymbol{Name: h.Symbol, Kind: h.Kind, Line: h.Line})
	}

	focal := filePath
	if len(out.Symbols) > 0 {
		focal = out.Symbols[0].Name
	}
	ctxResp, err := c.Context360(ctx, Context360Request{Symbol: focal})
	if err != nil {
		return nil, err
	}
	for _, caller := range ctxResp.Callers {
		out.Callers = append(out.Callers, CodegraphFileCaller{File: caller, Symbol: focal})
	}
	out.CommunityID = ctxResp.Community
	out.Coreness = ctxResp.Coreness
	out.SCCID = ctxResp.SCCID
	out.Cyclic = ctxResp.Cyclic

	impactResp, err := c.Impact(ctx, ImpactRequest{Symbol: focal})
	if err != nil {
		return nil, err
	}
	out.BlastRadiusScore = blastRadiusScoreFromEnum(impactResp.BlastRadius, impactResp.Score)

	if ccResp, ccErr := c.CoChange(ctx, CoChangeRequest{File: filePath}); ccErr == nil {
		for _, pr := range ccResp.Peers {
			out.CoChangePeers = append(out.CoChangePeers, CodegraphCoChangePeer{
				Path:            pr.Path,
				CouplingPercent: pr.CouplingPercent,
				SharedRevs:      pr.SharedRevs,
			})
		}
	}

	if hResp, hErr := c.CaronteHealth(ctx, CaronteHealthRequest{}); hErr == nil && hResp.LastIndexed > 0 {
		out.LastIndexedRFC3339 = time.Unix(hResp.LastIndexed, 0).UTC().Format(time.RFC3339)
	}

	return out, nil
}

func blastRadiusScoreFromEnum(enum string, score int) float64 {
	switch strings.ToLower(enum) {
	case "low":
		return 0.25
	case "medium":
		return 0.55
	case "high":
		return 0.85
	}

	switch {
	case score <= 0:
		return 0
	case score >= 100:
		return 1
	default:
		return float64(score) / 100.0
	}
}

type AugmentCacheResponse struct {
	HitRate        float64 `json:"hit_rate"`
	TotalQueries   int64   `json:"total_queries"`
	BytesCached    int64   `json:"bytes_cached"`
	LastEvictedRFC string  `json:"last_evicted_rfc3339,omitempty"`
}

func (c *Client) AugmentCache(ctx context.Context) (*AugmentCacheResponse, error) {
	summary, err := c.AugmentSummary(ctx, "")
	if err != nil {
		return nil, err
	}
	return &AugmentCacheResponse{
		HitRate:        summary.CacheHitRate,
		TotalQueries:   int64(summary.KGQueriesFired),
		BytesCached:    int64(summary.TokensConsumed) * 4,
		LastEvictedRFC: summary.LastIndexedRFC3339,
	}, nil
}

func (c *Client) AuditEventByID(ctx context.Context, id string) (*AuditEvent, error) {

	if id == "" || strings.ContainsAny(id, "/?#") {
		return nil, &HTTPError{
			Method:  "GET",
			Path:    "/v1/audit/event/" + id,
			Status:  400,
			RawBody: []byte("invalid event id"),
		}
	}
	path := "/v1/audit/event/" + id

	var wrapper struct {
		Row AuditEvent `json:"row"`
	}
	if err := c.getJSON(ctx, path, &wrapper); err != nil {
		return nil, err
	}
	return &wrapper.Row, nil
}
