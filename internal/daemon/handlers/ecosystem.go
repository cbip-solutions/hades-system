// SPDX-License-Identifier: MIT
// Package handlers — ecosystem.go.
//
// EcosystemHandler interface + 8 HTTP handlers for the HADES design ecosystem
// docs operator surface. Referenced server-side by:
//
// POST /v1/ecosystem/pin — pin a version
// GET /v1/ecosystem/prune-preview — preview cascade counts
// DELETE /v1/ecosystem/version — hard-remove a version
// POST /v1/ecosystem/ingest-delta — schedule delta ingest
// POST /v1/ecosystem/sweep/fingerprints — re-verify chunk fingerprints
// POST /v1/ecosystem/sweep/change-nodes — verify Change-node graph
// POST /v1/ecosystem/sweep/rebuild-symbol-index — rebuild symbol index
// POST /v1/ecosystem/sweep/cas-gc — CAS garbage-collect
// GET /v1/ecosystem/new-versions/{eco} — detect new upstream versions
//
// Routes registered in registerRoutes() consult the EcosystemHandler()
// accessor on *daemon.Server. When nil (production wiring deferred until a
// later stage composes the *internal/research/ecosystem.Dispatcher +
// verifier + symbol_index façade and calls SetEcosystemHandler), every
// path returns 503 Service Unavailable so the operator/cron worker sees
// "feature not configured" rather than a silent 404 from an unmounted
// route. Mirrors the HADES design KnowledgeIndex pattern + the
// HandoffEmitter / DayGenerator nil-safety contracts.
//
// Status-code mapping (mirrors knowledge_p7 + day_p7 patterns):
//
// 503 — EcosystemHandler() unavailable (later-stage bootstrap will
// register the façade at boot; tests inject fakes via
// SetEcosystemHandler).
// 400 — invalid JSON body, missing required field, malformed path param.
// 404 — (ecosystem, version) tuple does not exist (Pin / Prune /
// PrunePreview); unknown ecosystem name (NewVersions).
// 409 — version is pinned (Prune cannot proceed); version is already
// pinned (Pin is idempotent no-op; we surface 409 per the client
// contract so the CLI classifyDocsError can map to recoverable).
// 500 — opaque backend error (sql I/O, integrity sweep failure, etc).
// 200/204 — success; bodies documented per route below.
//
// Wire contract: request/response shapes here are the daemon-side mirror
// of internal/client/ecosystem_docs_ops.go. Any drift between this file
// and the client side breaks the round-trip. The handler decodes the
// JSON request body into a request struct, validates, dispatches to the
// EcosystemHandler interface, and encodes the response.
//
// invariant boundary: this handler MAY import
// internal/research/ecosystem for the wire-side ecosystem name constants
// (`Ecosystem`, `AllEcosystems`) — the boundary applies to
// `internal/store`, not `internal/research`. The EcosystemHandler
// interface is structural so the handler→adapter relationship stays at
// the interface layer; production wires a concrete dispatcher adapter
// later, tests substitute a fake without dragging in the real
// dispatcher's CGO dependency tree.
//
// Why one shared ecosystem.go (vs splitting into 8 files): each handler
// has only ~40 lines of unique logic (decode body + delegate to seam +
// encode response). Keeping them co-located with the interface + the
// accessor + the request/response types makes the wire contract reviewable
// in a single read. The 8 individual handlers are exported separately so
// each route can be wired in server.go without ambiguity.
package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/cbip-solutions/hades-system/internal/daemon/handlers/ecospec"
)

const ecosystemHandlerTimeout = 30 * time.Second

const sweepHandlerTimeout = 2 * time.Hour

var validEcosystems = []string{"go", "python", "typescript", "rust"}

func isValidEcosystem(eco string) bool {
	return slices.Contains(validEcosystems, eco)
}

var (
	ErrEcosystemPinAlreadyPinned = ecospec.ErrEcosystemPinAlreadyPinned
	ErrEcosystemVersionPinned    = ecospec.ErrEcosystemVersionPinned
	ErrEcosystemVersionNotFound  = ecospec.ErrEcosystemVersionNotFound
)

type EcosystemPrunePreviewResult = ecospec.EcosystemPrunePreviewResult

type EcosystemHandler = ecospec.EcosystemHandler

type ecosystemHandlerAccessor interface {
	EcosystemHandler() EcosystemHandler
}

func resolveEcosystemHandler(s any) EcosystemHandler {
	acc, ok := s.(ecosystemHandlerAccessor)
	if !ok {
		return nil
	}
	return acc.EcosystemHandler()
}

func ecosystemUnavailable(w http.ResponseWriter) {
	http.Error(w, "ecosystem handler not configured", http.StatusServiceUnavailable)
}

type EcosystemPinRequest struct {
	Ecosystem string `json:"ecosystem"`
	Version   string `json:"version"`
}

type EcosystemPruneRequest struct {
	Ecosystem string `json:"ecosystem"`
	Version   string `json:"version"`
}

type EcosystemPrunePreviewResponse struct {
	Ecosystem      string `json:"ecosystem"`
	Version        string `json:"version"`
	ChunkCount     int    `json:"chunk_count"`
	ChunkFP32Count int    `json:"chunk_fp32_count"`
	SymbolCount    int    `json:"symbol_count"`
	ChangeCount    int    `json:"change_count"`
	FTS5Count      int    `json:"fts5_count"`
	Pinned         bool   `json:"pinned"`
}

type EcosystemSimpleRequest struct {
	Ecosystem string `json:"ecosystem"`
}

type EcosystemNewVersionsResponse struct {
	Versions []string `json:"versions"`
}

func decodeEcosystemAndVersion(r *http.Request) (eco, ver string, err error) {
	var req struct {
		Ecosystem string `json:"ecosystem"`
		Version   string `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return "", "", fmt.Errorf("invalid JSON body: %w", err)
	}
	eco = strings.TrimSpace(req.Ecosystem)
	ver = strings.TrimSpace(req.Version)
	if eco == "" {
		return "", "", errors.New("ecosystem is required")
	}
	if !isValidEcosystem(eco) {
		return "", "", fmt.Errorf("unknown ecosystem %q; must be one of: %s",
			eco, strings.Join(validEcosystems, ", "))
	}
	if ver == "" {
		return "", "", errors.New("version is required")
	}
	return eco, ver, nil
}

func decodeEcosystemOnly(r *http.Request) (string, error) {
	var req EcosystemSimpleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return "", fmt.Errorf("invalid JSON body: %w", err)
	}
	eco := strings.TrimSpace(req.Ecosystem)
	if eco == "" {
		return "", errors.New("ecosystem is required")
	}
	if !isValidEcosystem(eco) {
		return "", fmt.Errorf("unknown ecosystem %q; must be one of: %s",
			eco, strings.Join(validEcosystems, ", "))
	}
	return eco, nil
}

func mapEcosystemError(err error) (int, string) {
	switch {
	case errors.Is(err, ErrEcosystemVersionNotFound):
		return http.StatusNotFound, err.Error()
	case errors.Is(err, ErrEcosystemPinAlreadyPinned):
		return http.StatusConflict, err.Error()
	case errors.Is(err, ErrEcosystemVersionPinned):
		return http.StatusConflict, err.Error()
	default:
		return http.StatusInternalServerError, err.Error()
	}
}
