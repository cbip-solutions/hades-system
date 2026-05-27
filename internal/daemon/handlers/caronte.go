// SPDX-License-Identifier: MIT
// Package handlers — caronte.go.
//
// POST /v1/caronte/reindex — operator-triggered reindex of a project's
// Caronte code-graph. The handler resolves the X-HADES-Project-ID header
// (alias OR canonical id_sha256) through the ProjectsAliasResolver,
// then delegates to the engine's IndexProject method. The engine is
// in-daemon; this route
// avoids the gateway round-trip used by /v1/mcpgateway/* — operators
// can `hades caronte reindex` against a daemon without a running Hermes
// session.
//
// inv-hades-031 boundary: this handler does NOT import internal/caronte
// or internal/daemon/mcpgateway. The engine + alias resolver are
// consumed through narrow handler-local interfaces
// (CaronteEngineForReindex + ProjectsAliasResolverForReindex). The
// *daemon.Server in cmd/hades-ctld wires concrete adapters that
// thin-translate to those interfaces (the existing CaronteEngine /
// mcpgateway.ProjectsAliasResolver instances).
//
// inv-hades-277 alias resolution: the handler MUST translate alias →
// canonical id_sha256 BEFORE invoking IndexProject (the engine never
// sees aliases — its surface keys on canonical id_sha256 always).
package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

type CaronteReindexReport struct {
	ProjectID      string         `json:"project_id"`
	NodesCreated   int            `json:"nodes_created"`
	EdgesCreated   int            `json:"edges_created"`
	FilesIndexed   int            `json:"files_indexed"`
	LanguageCounts map[string]int `json:"language_counts"`
	DurationMillis int64          `json:"duration_ms"`
	StartedAt      time.Time      `json:"started_at"`
	Completed      bool           `json:"completed"`
}

type CaronteEngineForReindex interface {
	IndexProject(ctx context.Context, projectID string) (CaronteReindexReport, error)
}

// ProjectsAliasResolverForReindex is the alias-resolver seam the handler
// uses. Mirrors mcpgateway.ProjectsAliasResolver so the
// production wiring is a thin pass-through; declared locally so the
// handler package does not import internal/daemon/mcpgateway directly.
//
// Resolve MUST return canonical id_sha256 (64-char lowercase hex) on
// success or ErrCaronteAliasNotFound when no active row matches the
// alias-or-id.
type ProjectsAliasResolverForReindex interface {
	Resolve(ctx context.Context, idOrAlias string) (string, error)
}

// ErrCaronteAliasNotFound is the handler-local mirror of
// mcpgateway.ErrAliasNotFound. The production resolver adapter MUST
// return this sentinel (not the mcpgateway sentinel directly) so the
// handler can errors.Is-match without importing mcpgateway.
//
// (CLI router migration) may consolidate the sentinel into a
// shared package; until then the value-type mirror is the right scope.
var ErrCaronteAliasNotFound = errors.New("handlers/caronte: project alias not found")

// CaronteReindexCtx is the joint Ctx satisfied by *daemon.Server in
// production. Both accessors MUST be non-nil at boot; either being nil
// surfaces as 503 from the handler (a wiring bug, NOT a per-request
// failure).
type CaronteReindexCtx interface {
	CaronteEngineForReindex() CaronteEngineForReindex

	AliasResolverForReindex() ProjectsAliasResolverForReindex
}

func CaronteReindex(s CaronteReindexCtx) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		engine := s.CaronteEngineForReindex()
		if engine == nil {
			http.Error(w, "caronte engine not configured", http.StatusServiceUnavailable)
			return
		}
		resolver := s.AliasResolverForReindex()
		if resolver == nil {
			http.Error(w, "alias resolver not configured", http.StatusServiceUnavailable)
			return
		}
		raw := r.Header.Get("X-HADES-Project-ID")
		if raw == "" {
			http.Error(w, "X-HADES-Project-ID header required", http.StatusBadRequest)
			return
		}
		canonicalID, err := resolver.Resolve(r.Context(), raw)
		if err != nil {
			if errors.Is(err, ErrCaronteAliasNotFound) {
				http.Error(w, fmt.Sprintf("project %q not found", raw), http.StatusNotFound)
				return
			}
			http.Error(w, "alias resolution failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		rep, indexErr := engine.IndexProject(r.Context(), canonicalID)
		if indexErr != nil {
			http.Error(w, "reindex failed: "+indexErr.Error(), http.StatusInternalServerError)
			return
		}

		if rep.LanguageCounts == nil {
			rep.LanguageCounts = map[string]int{}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rep)
	}
}
