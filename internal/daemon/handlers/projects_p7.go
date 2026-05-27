// SPDX-License-Identifier: MIT
// Package handlers — projects_p7.go.
//
// Three POST routes for the release project lifecycle surface:
//
// POST /v1/projects/doctor — diagnose project identity (alias OR cwd)
// POST /v1/projects/archive — soft-delete (preserves path_history)
// POST /v1/projects/rm — hard-delete (cascades path_history)
//
// These operate on the projects_alias / path_history substrate (release
// migration 057) via projectctx.ProjectStore (the daemon-side adapter
// is internal/daemon/projectctxadapter.Adapter — invariant: this
// package never imports internal/store directly).
//
// File name uses the `_p7` suffix to avoid colliding with the legacy
// projects.go in the same package, which carries release stubs for the
// older /v1/projects/{id}, /v1/projects, agents-md, and sync routes
// (those operate on the original `projects` table — distinct schema).
//
// invariant boundary: the only projectctx-side import is the
// ProjectStore interface + value types (Alias, ProjectID, Project,
// PathHistoryEntry, MvDetection, Activate, FindProjectRoot,
// CanonicalPath). No internal/store, no projectctxadapter direct
// dependency — the store reaches us through the Server's
// ProjectStore() accessor.
package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/cbip-solutions/hades-system/internal/projectctx"
)

type projectStoreAccessor interface {
	ProjectStore() projectctx.ProjectStore
}

func resolveProjectStore(s any) projectctx.ProjectStore {
	acc, ok := s.(projectStoreAccessor)
	if !ok {
		return nil
	}
	return acc.ProjectStore()
}

func projectsP7Unavailable(w http.ResponseWriter) {
	http.Error(w, "project store not configured", http.StatusServiceUnavailable)
}

func ProjectDoctor(s any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		store := resolveProjectStore(s)
		if store == nil {
			projectsP7Unavailable(w)
			return
		}
		var req struct {
			Alias  string `json:"alias"`
			Cwd    string `json:"cwd"`
			Rebind bool   `json:"rebind"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if req.Alias == "" && req.Cwd == "" {
			http.Error(w, "alias or cwd required", http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
		defer cancel()
		if req.Cwd == "" {

			alias := projectctx.Alias(req.Alias)
			p, err := store.GetByAlias(ctx, alias)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if p == nil {
				http.Error(w, "alias not found", http.StatusNotFound)
				return
			}
			history, err := store.GetPathHistory(ctx, alias)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeDoctorOK(w, p, nil, history)
			return
		}

		canonical, err := projectctx.CanonicalPath(req.Cwd)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		alias := projectctx.Alias(req.Alias)
		if alias == "" {
			root, err := projectctx.FindProjectRoot(canonical)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			alias, err = projectctx.ResolveAlias(root)
			if err != nil {

				if isAliasResolutionError(err) {
					http.Error(w, err.Error(), http.StatusUnprocessableEntity)
					return
				}
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			canonical = root
		}
		res, err := projectctx.Activate(ctx, store, canonical, alias)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		var history []projectctx.PathHistoryEntry
		if res.Project != nil {
			h, herr := store.GetPathHistory(ctx, res.Project.Alias)
			if herr != nil {
				http.Error(w, herr.Error(), http.StatusInternalServerError)
				return
			}
			history = h
		}
		writeDoctorOK(w, res.Project, res.MvDetected, history)
	}
}

func isAliasResolutionError(err error) bool {
	return errors.Is(err, projectctx.ErrZenswarmTOMLMalformed) ||
		errors.Is(err, projectctx.ErrAliasInvalid) ||
		errors.Is(err, projectctx.ErrAliasEmpty) ||
		errors.Is(err, projectctx.ErrAliasInvalidChar) ||
		errors.Is(err, projectctx.ErrAliasReserved) ||
		errors.Is(err, projectctx.ErrAliasTooLong)
}

func writeDoctorOK(w http.ResponseWriter, p *projectctx.Project, mv *projectctx.MvDetection, history []projectctx.PathHistoryEntry) {
	type historyRow struct {
		Path      string `json:"path"`
		FirstSeen int64  `json:"first_seen"`
		LastSeen  int64  `json:"last_seen"`
	}
	type mvBody struct {
		OldPath    string `json:"old_path"`
		NewPath    string `json:"new_path"`
		OldIDShort string `json:"old_id_short"`
		NewIDShort string `json:"new_id_short"`
	}
	type body struct {
		Healthy       bool         `json:"healthy"`
		Alias         string       `json:"alias"`
		IDSha256      string       `json:"id_sha256"`
		CanonicalPath string       `json:"canonical_path"`
		PathHistory   []historyRow `json:"path_history"`
		MvDetected    *mvBody      `json:"mv_detected,omitempty"`
		Hint          string       `json:"hint,omitempty"`
	}
	resp := body{
		Healthy: mv == nil && p != nil,
	}
	if p != nil {
		resp.Alias = string(p.Alias)
		resp.IDSha256 = string(p.ID)
		resp.CanonicalPath = p.CanonicalPath
	}
	for _, h := range history {
		resp.PathHistory = append(resp.PathHistory, historyRow{
			Path:      h.Path,
			FirstSeen: h.FirstSeenAt.Unix(),
			LastSeen:  h.LastSeenAt.Unix(),
		})
	}
	if resp.PathHistory == nil {
		resp.PathHistory = []historyRow{}
	}
	if mv != nil {
		resp.MvDetected = &mvBody{
			OldPath:    mv.OldPath,
			NewPath:    mv.NewPath,
			OldIDShort: mv.OldID.Short(),
			NewIDShort: mv.NewID.Short(),
		}

		resp.Hint = "To rebind: zen project doctor " + string(mv.Alias) + " --rebind\n" +
			"To register as a new project: rename in zenswarm.toml [project] id"
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func ProjectArchive(s any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		store := resolveProjectStore(s)
		if store == nil {
			projectsP7Unavailable(w)
			return
		}
		var req struct {
			Alias string `json:"alias"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if req.Alias == "" {
			http.Error(w, "alias required", http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
		defer cancel()
		alias := projectctx.Alias(req.Alias)
		got, err := store.GetByAlias(ctx, alias)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if got == nil {
			http.Error(w, "alias not found", http.StatusNotFound)
			return
		}
		if err := store.Archive(ctx, alias); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}
}

func ProjectRm(s any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		store := resolveProjectStore(s)
		if store == nil {
			projectsP7Unavailable(w)
			return
		}
		var req struct {
			Alias string `json:"alias"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if req.Alias == "" {
			http.Error(w, "alias required", http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
		defer cancel()
		alias := projectctx.Alias(req.Alias)
		got, err := store.GetByAlias(ctx, alias)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if got == nil {
			http.Error(w, "alias not found", http.StatusNotFound)
			return
		}
		if err := store.Remove(ctx, alias); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}
}
