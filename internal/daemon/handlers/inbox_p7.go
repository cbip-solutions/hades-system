// SPDX-License-Identifier: MIT
// Package handlers — inbox_p7.go.
//
// Three routes for the inbox operator surface:
//
// POST /v1/inbox/list — list cached notifications (cross-project)
// POST /v1/inbox/ack — acknowledge a notification by id (sets AckedAt)
// POST /v1/inbox/snooze — snooze a notification until a future time
//
// These operate on the inbox_aggregator_cache substrate (migration 058
// + adapter) via internal/inbox.AggregatorCacheStore +
// internal/inbox.Store.Ack/Snooze. The InboxStore interface this
// handler consumes is the read+write surface the daemon-level cache
// adapter satisfies; per invariant the handler never imports
// internal/store directly — the inboxadapter is the single bridge.
//
// Status-code mapping (mirrors the projects_p7 + schedule_p7 patterns):
//
// 503 — InboxStore() not yet wired (cmd/zen-swarm-ctld registers
// the adapter at boot; tests inject fakes via SetInboxStore).
// 400 — invalid JSON / missing required fields (id, until).
// 404 — notification id not found (ack/snooze paths surface
// inbox.ErrNotFound from the backend).
// 422 — validation rejected the input (severity not in 4-tier enum,
// id ≤ 0, snooze until missing).
// 500 — opaque backend error (sql I/O, transactional failure).
// 200 — success; bodies documented per route below.
//
// invariant boundary: this handler imports internal/inbox value
// types only (Severity / CacheRow / ListFilter / sentinel errors). No
// internal/store imports — the InboxStore interface is structural and
// the daemon-side accessor returns it as the same interface, keeping
// the boundary at the interface layer (mirrors handlers.ScheduleStore
// gate pattern).
//
// CLI surface (handled in internal/cli/inbox.go):
//
// zen inbox [--severity X] [--unacked] [--project alias] [--since DUR] [--limit N]
// zen inbox ack <id>
// zen inbox snooze <id> --until <duration>
package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/cbip-solutions/hades-system/internal/inbox"
)

type InboxStore interface {
	Query(ctx context.Context, filter inbox.ListFilter) ([]inbox.CacheRow, error)
	Ack(ctx context.Context, id int64) error
	Snooze(ctx context.Context, id int64, until time.Time) error
}

type inboxStoreAccessor interface {
	InboxStore() InboxStore
}

func resolveInboxStore(s any) InboxStore {
	acc, ok := s.(inboxStoreAccessor)
	if !ok {
		return nil
	}
	return acc.InboxStore()
}

func inboxUnavailable(w http.ResponseWriter) {
	http.Error(w, "inbox store not configured", http.StatusServiceUnavailable)
}

const inboxHandlerTimeout = 5 * time.Second

type InboxListRequest struct {
	Severity     string `json:"severity,omitempty"`
	Project      string `json:"project,omitempty"`
	SinceUnix    int64  `json:"since_unix,omitempty"`
	Limit        int    `json:"limit,omitempty"`
	IncludeAcked bool   `json:"include_acked,omitempty"`
}

type InboxListResponse struct {
	Rows []InboxListRow `json:"rows"`
}

type InboxListRow struct {
	CacheID        int64      `json:"cache_id"`
	ProjectID      string     `json:"project_id"`
	ProjectAlias   string     `json:"project_alias"`
	NotificationID int64      `json:"notification_id"`
	Severity       string     `json:"severity"`
	EventType      string     `json:"event_type"`
	ContentHash    string     `json:"content_hash"`
	CreatedAt      time.Time  `json:"created_at"`
	AckedAt        *time.Time `json:"acked_at,omitempty"`
}

func InboxListHandler(s any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		store := resolveInboxStore(s)
		if store == nil {
			inboxUnavailable(w)
			return
		}
		var req InboxListRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf("invalid JSON body: %v", err), http.StatusBadRequest)
			return
		}
		filter := inbox.ListFilter{
			ProjectID:    req.Project,
			Limit:        req.Limit,
			IncludeAcked: req.IncludeAcked,
		}
		if strings.TrimSpace(req.Severity) != "" {
			sev, err := inbox.ParseSeverity(req.Severity)
			if err != nil {
				http.Error(w, err.Error(), http.StatusUnprocessableEntity)
				return
			}
			filter.Severity = &sev
		}
		if req.SinceUnix > 0 {
			t := time.Unix(req.SinceUnix, 0).UTC()
			filter.Since = &t
		}
		ctx, cancel := context.WithTimeout(r.Context(), inboxHandlerTimeout)
		defer cancel()
		rows, err := store.Query(ctx, filter)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		out := InboxListResponse{Rows: make([]InboxListRow, 0, len(rows))}
		for _, row := range rows {
			out.Rows = append(out.Rows, InboxListRow{
				CacheID:        row.CacheID,
				ProjectID:      row.ProjectID,
				ProjectAlias:   row.ProjectAlias,
				NotificationID: row.NotificationID,
				Severity:       string(row.Severity),
				EventType:      row.EventType,
				ContentHash:    row.ContentHash,
				CreatedAt:      row.CreatedAt,
				AckedAt:        row.AckedAt,
			})
		}
		writeJSON(w, http.StatusOK, out)
	}
}

type InboxAckRequest struct {
	ID int64 `json:"id"`
}

type InboxAckResponse struct {
	OK bool  `json:"ok"`
	ID int64 `json:"id"`
}

func InboxAckHandler(s any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		store := resolveInboxStore(s)
		if store == nil {
			inboxUnavailable(w)
			return
		}
		var req InboxAckRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf("invalid JSON body: %v", err), http.StatusBadRequest)
			return
		}
		if req.ID <= 0 {
			http.Error(w, "id must be positive", http.StatusUnprocessableEntity)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), inboxHandlerTimeout)
		defer cancel()
		if err := store.Ack(ctx, req.ID); err != nil {
			if errors.Is(err, inbox.ErrNotFound) {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, InboxAckResponse{OK: true, ID: req.ID})
	}
}

type InboxSnoozeRequest struct {
	ID    int64     `json:"id"`
	Until time.Time `json:"until"`
}

type InboxSnoozeResponse struct {
	OK    bool      `json:"ok"`
	ID    int64     `json:"id"`
	Until time.Time `json:"until"`
}

func InboxSnoozeHandler(s any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		store := resolveInboxStore(s)
		if store == nil {
			inboxUnavailable(w)
			return
		}
		var req InboxSnoozeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf("invalid JSON body: %v", err), http.StatusBadRequest)
			return
		}
		if req.ID <= 0 {
			http.Error(w, "id must be positive", http.StatusUnprocessableEntity)
			return
		}
		if req.Until.IsZero() {
			http.Error(w, "until is required (RFC3339 UTC)", http.StatusUnprocessableEntity)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), inboxHandlerTimeout)
		defer cancel()
		until := req.Until.UTC()
		if err := store.Snooze(ctx, req.ID, until); err != nil {
			if errors.Is(err, inbox.ErrNotFound) {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, InboxSnoozeResponse{OK: true, ID: req.ID, Until: until})
	}
}
