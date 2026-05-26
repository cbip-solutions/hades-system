// SPDX-License-Identifier: MIT
// Package handlers — research_cache.go (Plan 4 Phase G Task G-1).
//
// GET  /v1/research/cache/get?hash=<sha256>  — cache lookup by pre-computed hash.
// POST /v1/research/cache/set                — persist a cache entry with TTL.
//
// Cache hash = sha256(query + sources_used + iteration_index), computed by the
// research MCP (Phase I) before the outbound call. The daemon stores and retrieves
// the opaque response_json blob. TTL is 7 days by default; doctrine-tunable via
// ResearchCacheCtx.ResearchCacheTTL() (Phase A wires the actual doctrine loader;
// the interface defaults to 7 days when no loader is present).
//
// # Eviction
//
// The daemon spawns a background eviction goroutine in Server.Start() that
// runs every hour and deletes expired rows
// (DELETE FROM research_cache WHERE ttl_unix < unixepoch()). Without it,
// the schema/054_research_cache.sql + handler-level TTL check would hide
// expired rows from reads but never delete them, growing the table
// unboundedly (post-review C-3 fix). The goroutine is stopped on
// daemon shutdown via Stop().
//
// inv-zen-001: Unix socket only — enforced at server.go listener level.
// inv-zen-031: This file never imports internal/store directly.
package handlers

import (
	"encoding/json"
	"net/http"
	"time"
)

type ResearchCacheCtx interface {
	ResearchCacheGet(hash string) (string, int64, bool, error)

	ResearchCacheSet(hash, responseJSON string, ttlUnix int64) error

	ResearchCacheTTL() time.Duration
}

func ResearchCacheGet(s ResearchCacheCtx) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hash := r.URL.Query().Get("hash")
		if hash == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "hash parameter required"})
			return
		}
		responseJSON, ttlUnix, hit, err := s.ResearchCacheGet(hash)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if !hit {
			writeJSON(w, http.StatusNotFound, map[string]any{"hit": false})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"hit":           true,
			"response_json": responseJSON,
			"ttl_unix":      ttlUnix,
		})
	}
}

type researchCacheSetRequest struct {
	Hash         string `json:"hash"`
	ResponseJSON string `json:"response_json"`

	TTLSeconds int64 `json:"ttl_seconds"`
}

func ResearchCacheSet(s ResearchCacheCtx) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var req researchCacheSetRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
			return
		}
		if req.Hash == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "hash required"})
			return
		}
		if req.ResponseJSON == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "response_json required"})
			return
		}
		var ttl time.Duration
		if req.TTLSeconds > 0 {
			ttl = time.Duration(req.TTLSeconds) * time.Second
		} else {
			ttl = s.ResearchCacheTTL()
		}
		ttlUnix := time.Now().Add(ttl).Unix()
		if err := s.ResearchCacheSet(req.Hash, req.ResponseJSON, ttlUnix); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"stored":   true,
			"ttl_unix": ttlUnix,
		})
	}
}
