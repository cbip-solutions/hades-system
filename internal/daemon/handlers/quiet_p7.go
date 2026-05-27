// SPDX-License-Identifier: MIT
// Package handlers — quiet_p7.go.
//
// Three routes for the release quiet-hours operator surface:
//
// GET /v1/quiet — render quiet config + active pause
// POST /v1/quiet/urgent-pause — set UrgentPauseUntil for a duration
// POST /v1/quiet/cancel — clear active urgent-pause
//
// The persistent quiet-hours config (~/.config/zen-swarm/notifications.toml)
// is operator-edited; this surface ONLY exposes the read view + the
// runtime UrgentPauseUntil mutator (the file-as-source-of-truth pattern
// per spec §6.5). The CLI's RunQuietList renders the config, and
// RunQuietPause / RunQuietCancel manage the in-memory pause window.
//
// Status-code mapping (mirrors the inbox_p7 + schedule_p7 patterns):
//
// 503 — QuietStore() not yet wired (cmd/zen-swarm-ctld registers
// the store at boot; tests inject fakes via SetQuietStore).
// 400 — invalid JSON / missing required fields (until on pause).
// 422 — validation rejected the input (zero / past until).
// 500 — opaque backend error.
// 200 — success; bodies documented per route below.
//
// invariant boundary: this handler imports internal/inbox value types
// only (QuietConfig / QuietHours). No internal/store imports — the
// QuietStore interface is structural and the daemon-side accessor
// returns it as the same interface, keeping the boundary at the
// interface layer.
//
// CLI surface (handled in internal/cli/quiet.go):
//
// zen quiet [--list] # default: list
// zen quiet --urgent-pause <duration>
// zen quiet --cancel
package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/cbip-solutions/hades-system/internal/inbox"
)

type QuietStore interface {
	Get(ctx context.Context) (inbox.QuietConfig, error)
	SetUrgentPause(ctx context.Context, until time.Time) error
	CancelUrgentPause(ctx context.Context) error
}

type quietStoreAccessor interface {
	QuietStore() QuietStore
}

func resolveQuietStore(s any) QuietStore {
	acc, ok := s.(quietStoreAccessor)
	if !ok {
		return nil
	}
	return acc.QuietStore()
}

func quietUnavailable(w http.ResponseWriter) {
	http.Error(w, "quiet store not configured", http.StatusServiceUnavailable)
}

const quietHandlerTimeout = 5 * time.Second

type QuietHoursWire struct {
	StartSec        int64 `json:"start_sec"`
	EndSec          int64 `json:"end_sec"`
	WeekendExtended bool  `json:"weekend_extended"`
	UrgentBypass    bool  `json:"urgent_bypass"`
}

type QuietGetResponse struct {
	Default          QuietHoursWire            `json:"default"`
	PerProject       map[string]QuietHoursWire `json:"per_project"`
	UrgentPauseUntil *time.Time                `json:"urgent_pause_until,omitempty"`
}

type QuietPauseRequest struct {
	Until time.Time `json:"until"`
}

type QuietPauseResponse struct {
	OK    bool      `json:"ok"`
	Until time.Time `json:"until"`
}

type QuietCancelResponse struct {
	OK bool `json:"ok"`
}

func quietHoursToWire(q inbox.QuietHours) QuietHoursWire {
	return QuietHoursWire{
		StartSec:        int64(q.Start.Seconds()),
		EndSec:          int64(q.End.Seconds()),
		WeekendExtended: q.WeekendExtended,
		UrgentBypass:    q.UrgentBypass,
	}
}

func QuietGetHandler(s any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		store := resolveQuietStore(s)
		if store == nil {
			quietUnavailable(w)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), quietHandlerTimeout)
		defer cancel()
		cfg, err := store.Get(ctx)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		resp := QuietGetResponse{
			Default:    quietHoursToWire(cfg.Default),
			PerProject: make(map[string]QuietHoursWire, len(cfg.PerProject)),
		}
		for projectID, hours := range cfg.PerProject {
			resp.PerProject[projectID] = quietHoursToWire(hours)
		}
		if cfg.UrgentPauseUntil != nil {
			t := *cfg.UrgentPauseUntil
			resp.UrgentPauseUntil = &t
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func QuietUrgentPauseHandler(s any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		store := resolveQuietStore(s)
		if store == nil {
			quietUnavailable(w)
			return
		}
		var req QuietPauseRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf("invalid JSON body: %v", err), http.StatusBadRequest)
			return
		}
		if req.Until.IsZero() {
			http.Error(w, "until is required (RFC3339 UTC)", http.StatusUnprocessableEntity)
			return
		}
		now := time.Now().UTC()
		until := req.Until.UTC()
		if !until.After(now) {
			http.Error(w, "until must be in the future", http.StatusUnprocessableEntity)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), quietHandlerTimeout)
		defer cancel()
		if err := store.SetUrgentPause(ctx, until); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, QuietPauseResponse{OK: true, Until: until})
	}
}

func QuietCancelHandler(s any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		store := resolveQuietStore(s)
		if store == nil {
			quietUnavailable(w)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), quietHandlerTimeout)
		defer cancel()
		if err := store.CancelUrgentPause(ctx); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, QuietCancelResponse{OK: true})
	}
}
