// SPDX-License-Identifier: MIT
// Package handlers — notifications_post.go.
//
// POST /v1/notifications/post — accepts notification events from the
// out-of-process bypass sidecar (cbip-solutions/zen-bypass-tier1) and
// writes them to the schema-v9 notifications table.
//
// Architectural intent:
// the daemon owns SQLite. The sidecar maintains NO SQLite state.
// Notifications previously dispatched via in-process callback shapes
// (OnTierSwitch / OnRefreshPermanentFail / OnCertPinFailure /
// OnAnomalyThreshold living on the bypass Client + Validator) now
// HTTP-post to this endpoint instead. The endpoint's request shape
// matches the wire-schema documented in the release spec §B-8:
//
// {"severity": "info"|"warn"|"error",
// "source": "bypass-sidecar",
// "ts": "RFC3339",
// "payload": {...}}
//
// invariant boundary: this handler imports internal/store solely for
// the Notification value type passed to NotificationsInserter — it
// never touches the concrete *store.Store. The daemon adapter that
// satisfies NotificationsInserter is the single bridge.
package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/cbip-solutions/hades-system/internal/store"
)

type NotificationsInserter interface {
	InsertBypassNotification(ctx context.Context, n store.Notification) (int64, error)
}

type NotificationsPostCtx interface {
	NotificationsInserter() NotificationsInserter
}

type notificationPostBody struct {
	Severity string         `json:"severity"`
	Source   string         `json:"source"`
	TS       string         `json:"ts"`
	Payload  map[string]any `json:"payload,omitempty"`
}

const (
	maxNotificationSource  = 64
	maxNotificationPayload = 8192
)

func canonicalSeverity(wire string) (string, bool) {
	switch strings.ToUpper(strings.TrimSpace(wire)) {
	case "INFO":
		return "INFO", true
	case "WARN":
		return "WARN", true
	case "ERROR", "CRITICAL":
		return "CRITICAL", true
	}
	return "", false
}

func extractTitleAndBody(severity, source string, payload map[string]any) (title, body string) {
	if payload == nil {
		return severity + " " + source, ""
	}
	if t, ok := payload["title"].(string); ok && t != "" {
		title = t
	} else {
		title = severity + " " + source
	}
	if b, ok := payload["body"].(string); ok && b != "" {
		body = b
		return title, body
	}

	residual := make(map[string]any, len(payload))
	for k, v := range payload {
		if k == "title" || k == "body" {
			continue
		}
		residual[k] = v
	}
	if len(residual) == 0 {
		return title, ""
	}
	raw, err := json.Marshal(residual)
	if err != nil {

		return title, ""
	}
	return title, string(raw)
}

func NotificationsPost(s NotificationsPostCtx) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var body notificationPostBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			RenderError(r.Context(), w, http.StatusBadRequest,
				"bad_json", "request body is not valid JSON")
			return
		}

		severity, ok := canonicalSeverity(body.Severity)
		if !ok {
			RenderError(r.Context(), w, http.StatusBadRequest,
				"bad_severity",
				"severity must be one of: info, warn, error (or INFO, WARN, CRITICAL)")
			return
		}

		src := strings.TrimSpace(body.Source)
		if src == "" || len(src) > maxNotificationSource {
			RenderError(r.Context(), w, http.StatusBadRequest,
				"bad_source",
				"source must be a non-empty string ≤ 64 chars")
			return
		}

		ts, err := time.Parse(time.RFC3339, body.TS)
		if err != nil || ts.IsZero() {
			RenderError(r.Context(), w, http.StatusBadRequest,
				"bad_timestamp",
				"ts must be a non-zero RFC3339 timestamp")
			return
		}

		if body.Payload != nil {
			raw, mErr := json.Marshal(body.Payload)
			if mErr == nil && len(raw) > maxNotificationPayload {
				RenderError(r.Context(), w, http.StatusBadRequest,
					"payload_too_large",
					"payload exceeds 8192 bytes when JSON-encoded")
				return
			}
		}

		inserter := s.NotificationsInserter()
		if inserter == nil {

			RenderError(r.Context(), w, http.StatusServiceUnavailable,
				"store_unavailable",
				"notifications store not yet wired (daemon boot in progress)")
			return
		}

		title, bodyText := extractTitleAndBody(severity, src, body.Payload)

		_, err = inserter.InsertBypassNotification(r.Context(), store.Notification{
			Severity: severity,
			Title:    title,
			Body:     bodyText,
			Source:   src,
			TS:       ts.UTC(),
		})
		if err != nil {
			// Upstream SQL error text MUST NOT be surfaced — it can leak
			// disk paths + errno strings. Stable opaque code is enough
			// for the sidecar's retry/log decision.
			RenderError(r.Context(), w, http.StatusInternalServerError,
				"insert_failed",
				"notifications insert failed")
			return
		}

		RenderJSON(r.Context(), w, http.StatusOK, map[string]any{
			"inserted": true,
		})
	}
}
