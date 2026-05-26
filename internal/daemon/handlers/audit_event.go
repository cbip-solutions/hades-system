// Copyright 2026 zen-swarm contributors. SPDX-License-Identifier: MIT

package handlers

import (
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/cbip-solutions/hades-system/internal/citation"
)

var auditEventIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)

type SessionDoctrineFunc func(r *http.Request) string

var ErrAuditEventNotFound = errors.New("audit event not found")

func AuditEventByIDHandler(ctx AuditQueryCtx, sessDoctrine SessionDoctrineFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		doctrine := sessDoctrine(r)
		if doctrine == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "session required"})
			return
		}

		id := strings.TrimPrefix(r.URL.Path, "/v1/audit/event/")
		id = strings.TrimSuffix(id, "/")
		if id == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "event id required"})
			return
		}
		if !auditEventIDPattern.MatchString(id) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid event id format"})
			return
		}

		row, err := ctx.AuditEventByID(id)
		if err != nil {
			if errors.Is(err, ErrAuditEventNotFound) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "event not found"})
				return
			}
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		if !DoctrineVisible(row.Doctrine, doctrine) {
			// Return 403 — the operator's session can't see this row
			// even though it exists. Do NOT leak row.ProjectID / payload
			// in the response body (cross-doctrine leak path).
			writeJSON(w, http.StatusForbidden, map[string]string{
				"error": "doctrine not authorised to view this event",
			})
			return
		}

		auditRow := citation.AuditEventRow{
			ID:        row.ID,
			ProjectID: row.ProjectID,
			Type:      row.Type,
			Doctrine:  row.Doctrine,
			Payload:   row.PayloadRaw,
			EmittedAt: row.EmittedAt,
		}
		env, err := citation.EnvelopeFromAuditEvent(auditRow)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": "envelope construction failed: " + err.Error(),
			})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"envelope": env,
			"row":      row,
		})
	}
}

func DoctrineVisible(rowDoctrine, sessionDoctrine string) bool {
	switch sessionDoctrine {
	case "capa-firewall":

		return rowDoctrine == "capa-firewall"
	case "max-scope", "default":

		return rowDoctrine != "capa-firewall"
	default:

		return false
	}
}
