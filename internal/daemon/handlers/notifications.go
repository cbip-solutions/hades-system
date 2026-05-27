// SPDX-License-Identifier: MIT
// Package handlers — notifications.go.
//
// Real CRUD against the bypass-scoped notifications table (schema v9).
// table is single-dispatcher, simple ack workflow with 1h CRITICAL repeat.
package handlers

import (
	"context"
	"net/http"
	"strconv"

	"github.com/cbip-solutions/hades-system/internal/store"
)

type notificationsServer interface {
	Store() *store.Store
}

func resolveNotificationsServer(s any) notificationsServer {
	if srv, ok := s.(notificationsServer); ok {
		return srv
	}
	return nil
}

func NotificationsList(s any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		srv := resolveNotificationsServer(s)
		if srv == nil || srv.Store() == nil {
			http.Error(w, "store not configured", http.StatusServiceUnavailable)
			return
		}
		limit := 50
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				if n > 1000 {
					n = 1000
				}
				limit = n
			}
		}
		onlyUnacked := r.URL.Query().Get("unacked") == "1"
		ctx := r.Context()
		if ctx == nil {
			ctx = context.Background()
		}
		rows, err := srv.Store().ListBypassNotifications(ctx, limit, onlyUnacked)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		out := make([]map[string]any, 0, len(rows))
		for _, n := range rows {
			row := map[string]any{
				"id":           n.ID,
				"severity":     n.Severity,
				"title":        n.Title,
				"body":         n.Body,
				"source":       n.Source,
				"ts":           n.TS,
				"acknowledged": n.Acknowledged,
			}
			if n.AckTS != nil {
				row["ack_ts"] = *n.AckTS
			}
			if n.LastRepeated != nil {
				row["last_repeated"] = *n.LastRepeated
			}
			out = append(out, row)
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func NotificationsDismiss(s any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		srv := resolveNotificationsServer(s)
		if srv == nil || srv.Store() == nil {
			http.Error(w, "store not configured", http.StatusServiceUnavailable)
			return
		}
		idStr := r.PathValue("id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			http.Error(w, "bad id", http.StatusBadRequest)
			return
		}
		if err := srv.Store().AckBypassNotification(r.Context(), id); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func NotificationsHistory(s any) http.HandlerFunc {
	return NotificationsList(s)
}
