// SPDX-License-Identifier: MIT
// Package handlers — research_p9.go.
//
// 4 NEW operator-facing research endpoints surfacing substrate
// (research findings cache per design choice A) over /v1/research/*. The cache
// stats + cache list paths COLLIDE with HADES design N's admin handlers
// (research_cache_admin.go); replaces the mux registration
// because the HADES design wire shape is a strict superset (additive fields
// only; older clients deserialise without breakage).
//
// invariant: handlers consume ResearchStoreP9 interface only — no direct
// import of internal/research/cache or internal/store.
// invariant: no fresh LLM dispatch from these handlers; cache reads only.
// wires *daemon.Server to satisfy ResearchStoreP9 via the
// production research/cache.Store; during development the 503 makes
// intent explicit.
//
// GET /v1/research/history — HADES design eventlog research.* filter
// GET /v1/research/cache/stats — extends HADES design N stats (additive)
// POST /v1/research/cache/invalidate — force-stale by query match
// GET /v1/research/cache/list — extends HADES design N list (additive)
//
// Wire-compatibility: HADES design stats + list shapes are JSON strict supersets
// of HADES design N — older clients deserialize cleanly (extra fields ignored).
// The HADES design N handler functions become dead code post-H-10 wiring; left
// in place for review-friendly diff (CHANGELOG v0.9.0 documents).
//
// Boundary invariants:
//
// invariant: handler never imports internal/store directly.
// invariant: research dispatch goes through dispatcheradapter;
// H-4 reads cache only, never dispatches fresh research.
package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
)

type ResearchHistoryFilterP9 struct {
	Filter    string `json:"filter,omitempty"`
	Since     int64  `json:"since,omitempty"`
	ProjectID string `json:"project_id,omitempty"`
	Limit     int    `json:"limit,omitempty"`
}

type ResearchHistoryEntryP9 struct {
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

type ResearchStoreP9 interface {
	History(ctx context.Context, filter ResearchHistoryFilterP9) ([]ResearchHistoryEntryP9, error)

	CacheStats(ctx context.Context, projectID string) (ResearchCacheStatsP9, error)

	CacheInvalidate(ctx context.Context, query string) (int, error)

	CacheList(ctx context.Context, projectID, sourcePrefix string) ([]ResearchCacheEntryP9, error)
}

func researchP9Unavailable(w http.ResponseWriter) {
	writeJSON(w, http.StatusServiceUnavailable, map[string]string{
		"error": "feature not configured",
		"code":  "plan9_research_unavailable",
	})
}

func ResearchP9History(s ResearchStoreP9) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s == nil {
			researchP9Unavailable(w)
			return
		}
		q := r.URL.Query()
		filter := ResearchHistoryFilterP9{
			Filter:    q.Get("filter"),
			ProjectID: q.Get("project_id"),
		}
		if v := q.Get("since"); v != "" {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 0 {
				filter.Since = n
			}
		}
		filter.Limit = 100
		if v := q.Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
				filter.Limit = n
			}
		}
		rows, err := s.History(r.Context(), filter)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if rows == nil {
			rows = []ResearchHistoryEntryP9{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": rows, "count": len(rows)})
	}
}

func ResearchP9CacheStats(s ResearchStoreP9) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s == nil {
			researchP9Unavailable(w)
			return
		}
		projectID := r.URL.Query().Get("project_id")
		stats, err := s.CacheStats(r.Context(), projectID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, stats)
	}
}

func ResearchP9CacheInvalidate(s ResearchStoreP9) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s == nil {
			researchP9Unavailable(w)
			return
		}
		defer r.Body.Close()
		var req struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		if req.Query == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "query required"})
			return
		}
		n, err := s.CacheInvalidate(r.Context(), req.Query)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"invalidated": n})
	}
}

func ResearchP9CacheList(s ResearchStoreP9) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s == nil {
			researchP9Unavailable(w)
			return
		}
		projectID := r.URL.Query().Get("project_id")
		sourcePrefix := r.URL.Query().Get("source")
		rows, err := s.CacheList(r.Context(), projectID, sourcePrefix)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if rows == nil {
			rows = []ResearchCacheEntryP9{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": rows, "count": len(rows)})
	}
}
