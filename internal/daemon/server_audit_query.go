// SPDX-License-Identifier: MIT
// Package daemon — server_audit_query.go (Plan 4 Phase N Task N-5).
//
// Operator audit-query methods on *Server backing handlers.AuditQueryCtx.
// The MCP-side audit_events_raw write path (server_phase_g_defaults.go's
// AuditEmit) is unchanged; this file adds read aggregations.
//
// handler + Doctrine field extracted from payload_json on all read paths.
package daemon

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/cbip-solutions/hades-system/internal/daemon/handlers"
)

const doctrineCapaFirewall = "capa-firewall"

// recognisedDoctrines is the set of doctrine names a positively-
// extracted payload may carry. Anything outside this set is treated as
// fail-closed (capa-firewall). Kept in sync with internal/doctrine
// builtin labels (`max-scope`, `default`, `capa-firewall` — spec §3.1).
//
// inv-zen-172: this set is the trust boundary between extracted-and-
// accepted vs. fail-closed. New doctrine names introduced in future
// plans MUST be added here AND in the visibility-matrix doc in
// internal/daemon/handlers/audit_event.go.
var recognisedDoctrines = map[string]struct{}{
	"max-scope":     {},
	"default":       {},
	"capa-firewall": {},
}

func (s *Server) AuditEvents(typePrefix, projectID string, sinceUnix int64, limit int) ([]handlers.AuditEventRow, error) {
	if s.store == nil {
		return []handlers.AuditEventRow{}, nil
	}

	clauses := []string{}
	args := []any{}
	if typePrefix != "" {
		clauses = append(clauses, "type LIKE ?")
		args = append(args, typePrefix+"%")
	}
	if projectID != "" {
		clauses = append(clauses, "project_id = ?")
		args = append(args, projectID)
	}
	if sinceUnix > 0 {
		clauses = append(clauses, "emitted_at >= ?")
		args = append(args, sinceUnix)
	}
	where := ""
	if len(clauses) > 0 {
		where = " WHERE " + strings.Join(clauses, " AND ")
	}
	args = append(args, limit)
	query := `SELECT id, project_id, type, payload_json, emitted_at FROM audit_events_raw` +
		where + ` ORDER BY emitted_at DESC LIMIT ?`
	rows, err := s.store.DB().Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]handlers.AuditEventRow, 0, limit)
	for rows.Next() {
		var r handlers.AuditEventRow
		if err := rows.Scan(&r.ID, &r.ProjectID, &r.Type, &r.PayloadRaw, &r.EmittedAt); err != nil {
			return nil, err
		}

		r.Doctrine = extractDoctrineFromPayload(r.PayloadRaw)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Server) AuditEventByID(id string) (handlers.AuditEventRow, error) {
	if s.store == nil {
		return handlers.AuditEventRow{}, handlers.ErrAuditEventNotFound
	}
	const q = `SELECT id, project_id, type, payload_json, emitted_at
	           FROM audit_events_raw WHERE id = ?`
	row := s.store.DB().QueryRow(q, id)
	var r handlers.AuditEventRow
	if err := row.Scan(&r.ID, &r.ProjectID, &r.Type, &r.PayloadRaw, &r.EmittedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return handlers.AuditEventRow{}, handlers.ErrAuditEventNotFound
		}
		return handlers.AuditEventRow{}, err
	}
	r.Doctrine = extractDoctrineFromPayload(r.PayloadRaw)
	return r, nil
}

func extractDoctrineFromPayload(payload string) string {
	if payload == "" {
		return doctrineCapaFirewall
	}
	var p struct {
		Doctrine *string `json:"doctrine"`
	}
	if err := json.Unmarshal([]byte(payload), &p); err != nil {

		return doctrineCapaFirewall
	}
	if p.Doctrine == nil {

		return doctrineCapaFirewall
	}
	d := *p.Doctrine
	if _, ok := recognisedDoctrines[d]; !ok {

		return doctrineCapaFirewall
	}
	return d
}

func (s *Server) AuditTypes() ([]handlers.AuditTypeRow, error) {
	if s.store == nil {
		return []handlers.AuditTypeRow{}, nil
	}
	cutoff := time.Now().Add(-30 * 24 * time.Hour).Unix()
	rows, err := s.store.DB().Query(
		`SELECT type, COUNT(*) FROM audit_events_raw
		 WHERE emitted_at >= ?
		 GROUP BY type
		 ORDER BY 2 DESC`,
		cutoff,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []handlers.AuditTypeRow{}
	for rows.Next() {
		var r handlers.AuditTypeRow
		if err := rows.Scan(&r.Type, &r.Count); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
