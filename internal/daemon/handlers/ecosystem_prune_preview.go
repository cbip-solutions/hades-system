// SPDX-License-Identifier: MIT
// Package handlers — ecosystem_prune_preview.go.
//
// EcosystemPrunePreview implements GET /v1/ecosystem/prune-preview.
//
// Returns the row counts that WOULD be cascade-deleted by a corresponding
// DELETE /v1/ecosystem/version on the same (ecosystem, version) tuple.
// No state is mutated; safe to call repeatedly. Pinned versions still
// produce counts but the Pinned flag is set so the CLI can refuse to
// proceed without rendering a confused "0 rows" view.
//
// Query parameters (NOT body — GET):
//
// ecosystem (required) — one of go, python, typescript, rust
// version (required) — semver string (e.g., "1.22.0")
//
// Status codes:
//
// 200 OK — JSON body with row counts + pinned flag.
// 400 Bad Request — missing/invalid query param.
// 404 Not Found — unknown (ecosystem, version) tuple.
// 500 Internal Server Error — opaque seam failure.
// 503 Service Unavailable — EcosystemHandler unavailable.
//
// Wire mirror: matches client.EcosystemPrunePreview(ctx, eco, ver) →
// GET /v1/ecosystem/prune-preview?ecosystem=<X>&version=<Y> →
// 200 + EcosystemPrunePreviewResponse body.
package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

func EcosystemPrunePreview(s any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h := resolveEcosystemHandler(s)
		if h == nil {
			ecosystemUnavailable(w)
			return
		}
		eco, ver, err := readEcosystemAndVersionFromQuery(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), ecosystemHandlerTimeout)
		defer cancel()
		preview, err := h.PrunePreview(ctx, eco, ver)
		if err != nil {
			code, msg := mapEcosystemError(err)
			http.Error(w, msg, code)
			return
		}
		writeJSON(w, http.StatusOK, EcosystemPrunePreviewResponse{
			Ecosystem:      preview.Ecosystem,
			Version:        preview.Version,
			ChunkCount:     preview.ChunkCount,
			ChunkFP32Count: preview.ChunkFP32Count,
			SymbolCount:    preview.SymbolCount,
			ChangeCount:    preview.ChangeCount,
			FTS5Count:      preview.FTS5Count,
			Pinned:         preview.Pinned,
		})
	}
}

// readEcosystemAndVersionFromQuery extracts ecosystem + version from
// URL query params and validates both. Symmetric counterpart to
// decodeEcosystemAndVersion (which reads JSON body); GET endpoints
// MUST use query params per HTTP semantics, so a separate helper.
func readEcosystemAndVersionFromQuery(r *http.Request) (eco, ver string, err error) {
	eco = strings.TrimSpace(r.URL.Query().Get("ecosystem"))
	ver = strings.TrimSpace(r.URL.Query().Get("version"))
	if eco == "" {
		return "", "", errors.New("ecosystem query param is required")
	}
	if !isValidEcosystem(eco) {
		return "", "", fmt.Errorf("unknown ecosystem %q; must be one of: %s",
			eco, strings.Join(validEcosystems, ", "))
	}
	if ver == "" {
		return "", "", errors.New("version query param is required")
	}
	return eco, ver, nil
}
