// SPDX-License-Identifier: MIT
// Package handlers — audit_query.go (Plan 4 Phase N Task N-5).
//
// Operator-facing audit query endpoints layered on the audit_events_raw
// table from Phase G. The MCP-side write path (POST /v1/audit/emit) is
// unchanged; this file adds:
//
//	GET /v1/audit/events  — recent events filtered by type/project/since
//	GET /v1/audit/types   — distinct event types (catalog)
//
// The CLI's `zen audit verdicts` filters events by type prefix
// (audit_review.*) on the events route; the catalog supports
// `zen audit criteria list` where criteria templates are surfaced
// via doctrine state (audit.criteria.* keys).
//
// inv-zen-001: Unix socket only.
// inv-zen-031: never imports internal/store directly; AuditQueryCtx is the bridge.
package handlers

import (
	"net/http"
	"strconv"
)

type AuditEventRow struct {
	ID         string `json:"id"`
	ProjectID  string `json:"project_id"`
	Type       string `json:"type"`
	Doctrine   string `json:"doctrine"`
	PayloadRaw string `json:"payload_json"`
	EmittedAt  int64  `json:"emitted_at"`
}

type AuditTypeRow struct {
	Type  string `json:"type"`
	Count int    `json:"count"`
}

type AuditQueryCtx interface {
	AuditEvents(typePrefix, projectID string, sinceUnix int64, limit int) ([]AuditEventRow, error)

	AuditTypes() ([]AuditTypeRow, error)
	// AuditEventByID returns a single row by id, or
	// (zero-value, ErrAuditEventNotFound) when absent. Plan 11 D-5
	// (zen://audit URL handler). The Doctrine field MUST be populated
	// (extracted from payload_json) so the handler's doctrine privacy
	// filter can run without re-parsing.
	AuditEventByID(id string) (AuditEventRow, error)
}

func AuditEvents(s AuditQueryCtx) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		typePrefix := r.URL.Query().Get("type")
		projectID := r.URL.Query().Get("project")
		var since int64
		if v := r.URL.Query().Get("since"); v != "" {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 0 {
				since = n
			}
		}
		limit := 100
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
				limit = n
			}
		}
		rows, err := s.AuditEvents(typePrefix, projectID, since, limit)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if rows == nil {
			rows = []AuditEventRow{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": rows, "count": len(rows)})
	}
}

func AuditTypes(s AuditQueryCtx) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := s.AuditTypes()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if rows == nil {
			rows = []AuditTypeRow{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": rows, "count": len(rows)})
	}
}
