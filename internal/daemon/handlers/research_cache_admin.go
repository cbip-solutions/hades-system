// SPDX-License-Identifier: MIT
// Package handlers — research_cache_admin.go.
//
// Operator-facing admin endpoints layered on top of the research
// cache primitives. These are NOT used by the research MCP (it uses
// /v1/research/cache/{get,set}); they exist solely so `zen research
// cache {list, clear, stats}` and `zen research show <hash>` can surface
// the cache to operators.
//
// GET /v1/research/cache/list — recent entries (limit/offset)
// POST /v1/research/cache/clear — drop entries older than X seconds
// GET /v1/research/cache/stats — aggregate size + age stats
// GET /v1/research/cache/show?hash=<sha> — raw row including JSON body
//
// invariant: Unix socket only.
// invariant: never imports internal/store directly; ResearchCacheAdminCtx
// is the bridge.
package handlers

import (
	"encoding/json"
	"net/http"
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

type ResearchCacheAdminCtx interface {
	ResearchCacheList(limit, offset int) ([]ResearchCacheEntry, error)

	ResearchCacheClear(cutoffUnix int64) (int64, error)

	ResearchCacheStats() (ResearchCacheStats, error)

	ResearchCacheShow(hash string) (ResearchCacheShow, bool, error)
}

func ResearchCacheList(s ResearchCacheAdminCtx) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit := 100
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
				limit = n
			}
		}
		offset := 0
		if v := r.URL.Query().Get("offset"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n >= 0 {
				offset = n
			}
		}
		items, err := s.ResearchCacheList(limit, offset)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if items == nil {
			items = []ResearchCacheEntry{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items)})
	}
}

type researchCacheClearReq struct {
	OlderThanSeconds int64 `json:"older_than_seconds"`
}

func ResearchCacheClear(s ResearchCacheAdminCtx) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var req researchCacheClearReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		if req.OlderThanSeconds <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "older_than_seconds must be > 0",
			})
			return
		}
		cutoff := time.Now().Unix() - req.OlderThanSeconds
		n, err := s.ResearchCacheClear(cutoff)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"deleted": n})
	}
}

func ResearchCacheStatsHandler(s ResearchCacheAdminCtx) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		stats, err := s.ResearchCacheStats()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, stats)
	}
}

func ResearchCacheShowHandler(s ResearchCacheAdminCtx) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hash := r.URL.Query().Get("hash")
		if hash == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "hash required"})
			return
		}
		show, hit, err := s.ResearchCacheShow(hash)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if !hit {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		writeJSON(w, http.StatusOK, show)
	}
}
