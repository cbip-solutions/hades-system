// SPDX-License-Identifier: MIT
// Package store — audit.go
//
// CRUD wrappers for the HADES design audit triple:
// - bypass_audit (existing v1 table, extended in v4 with conversation_id)
// - bypass_audit_bodies (v5, encrypted bodies, invariant)
// - bypass_audit_pins (v6, retention-exempt registry, design choice D)
//
// The bypass package never imports this file directly — it goes through
// the in-package adapter at tier1-sidecar/audit_integration_adapter.go
// so the bypass-side types stay decoupled from the SQL row types.
//
// The legacy stub in bypass_audit.go (InsertBypassAudit / ListBypassAudit /
// BypassSuccessRate) returns ErrNotImplementedPlan2 and is preserved for
// callers that haven't migrated yet; they can be removed in
package store

import (
	"database/sql"
	"fmt"
	"strings"
)

type BypassAuditFullRow struct {
	ID             int64
	TS             int64
	RequestHash    string
	ResponseHash   string
	Success        bool
	LatencyMs      int64
	ErrorCode      string
	ErrorPattern   string
	TierUsed       string
	ConversationID string
}

type BypassAuditPin struct {
	ConversationID string
	PinnedAt       int64
	Reason         string
}

func (s *Store) InsertBypassAuditFull(row BypassAuditFullRow) (int64, error) {
	successInt := 0
	if row.Success {
		successInt = 1
	}
	res, err := s.db.Exec(
		`INSERT INTO bypass_audit (
			ts, request_hash, response_hash, success, latency_ms,
			error_code, error_pattern, tier_used, conversation_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		row.TS, row.RequestHash, row.ResponseHash, successInt, row.LatencyMs,
		nullableString(row.ErrorCode), nullableString(row.ErrorPattern),
		row.TierUsed, nullableString(row.ConversationID),
	)
	if err != nil {
		return 0, fmt.Errorf("insert bypass_audit: %w", err)
	}
	return res.LastInsertId()
}

func (s *Store) InsertBypassAuditBody(auditID int64, reqBody, resBody []byte, keyVersion int) error {
	_, err := s.db.Exec(
		`INSERT INTO bypass_audit_bodies (audit_id, request_body, response_body, encrypted_at, key_version)
		 VALUES (?, ?, ?, strftime('%s','now'), ?)`,
		auditID, reqBody, resBody, keyVersion,
	)
	if err != nil {
		return fmt.Errorf("insert bypass_audit_bodies: %w", err)
	}
	return nil
}

func (s *Store) GetBypassAuditBody(auditID int64) (req, res []byte, keyVersion int, err error) {
	err = s.db.QueryRow(
		`SELECT request_body, response_body, key_version
		 FROM bypass_audit_bodies WHERE audit_id = ?`, auditID,
	).Scan(&req, &res, &keyVersion)
	return
}

func (s *Store) ListBypassAuditByConversation(conversationID string) ([]BypassAuditFullRow, error) {
	rows, err := s.db.Query(
		`SELECT id, ts, request_hash, response_hash, success, latency_ms,
		        COALESCE(error_code, ''), COALESCE(error_pattern, ''), tier_used,
		        COALESCE(conversation_id, '')
		 FROM bypass_audit WHERE conversation_id = ?
		 ORDER BY ts DESC`, conversationID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BypassAuditFullRow
	for rows.Next() {
		var r BypassAuditFullRow
		var successInt int
		if err := rows.Scan(
			&r.ID, &r.TS, &r.RequestHash, &r.ResponseHash, &successInt,
			&r.LatencyMs, &r.ErrorCode, &r.ErrorPattern, &r.TierUsed, &r.ConversationID,
		); err != nil {
			return nil, err
		}
		r.Success = successInt == 1
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) CountBypassAuditOlderThan(cutoffTS int64, exempt []string) (int, error) {
	q, args := purgeQuery("SELECT COUNT(*)", "", cutoffTS, exempt)
	var n int
	if err := s.db.QueryRow(q, args...).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

func (s *Store) BypassResponseCountInWindow(sinceTS, untilTS int64) (int64, error) {
	const q = `SELECT COUNT(*) FROM bypass_audit
		WHERE ts >= ? AND ts <= ? AND success = 1`
	var n int64
	if err := s.db.QueryRow(q, sinceTS, untilTS).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

func (s *Store) SizeBypassAuditBodiesOlderThan(cutoffTS int64, exempt []string) (int64, error) {

	q, args := purgeQuery("SELECT id", "", cutoffTS, exempt)
	wrapped := `SELECT COALESCE(SUM(LENGTH(b.request_body) + LENGTH(b.response_body)), 0)
	            FROM bypass_audit_bodies b WHERE b.audit_id IN (` + q + `)`
	var size sql.NullInt64
	if err := s.db.QueryRow(wrapped, args...).Scan(&size); err != nil {
		return 0, err
	}
	return size.Int64, nil
}

func (s *Store) PurgeBypassAuditOlderThan(cutoffTS int64, exempt []string) (int, error) {

	inner, args := purgeQuery("SELECT id", "", cutoffTS, exempt)
	q := `DELETE FROM bypass_audit WHERE id IN (` + inner + `)`
	res, err := s.db.Exec(q, args...)
	if err != nil {
		return 0, fmt.Errorf("purge bypass_audit: %w", err)
	}
	n, err := res.RowsAffected()
	return int(n), err
}

func purgeQuery(prefix, alias string, cutoffTS int64, exempt []string) (string, []interface{}) {
	var b strings.Builder
	b.WriteString(prefix)
	b.WriteString(" FROM bypass_audit ")
	if alias != "" {
		b.WriteString(alias + " ")
	}
	b.WriteString("WHERE ts < ?")
	args := []interface{}{cutoffTS}
	if len(exempt) > 0 {
		b.WriteString(" AND (conversation_id IS NULL OR conversation_id NOT IN (")
		for i, c := range exempt {
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString("?")
			args = append(args, c)
		}
		b.WriteString("))")
	}
	return b.String(), args
}

func (s *Store) UpsertBypassAuditPin(conversationID string, pinnedAt int64, reason string) error {
	_, err := s.db.Exec(
		`INSERT INTO bypass_audit_pins (conversation_id, pinned_at, reason) VALUES (?, ?, ?)
		 ON CONFLICT(conversation_id) DO UPDATE
		   SET pinned_at = excluded.pinned_at, reason = excluded.reason`,
		conversationID, pinnedAt, nullableString(reason),
	)
	return err
}

func (s *Store) DeleteBypassAuditPin(conversationID string) error {
	_, err := s.db.Exec(`DELETE FROM bypass_audit_pins WHERE conversation_id = ?`, conversationID)
	return err
}

func (s *Store) IsConversationPinned(conversationID string) (bool, error) {
	var n int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM bypass_audit_pins WHERE conversation_id = ?`, conversationID,
	).Scan(&n)
	return n > 0, err
}

func (s *Store) ListBypassAuditPins() ([]BypassAuditPin, error) {
	rows, err := s.db.Query(
		`SELECT conversation_id, pinned_at, COALESCE(reason, '')
		 FROM bypass_audit_pins ORDER BY pinned_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BypassAuditPin
	for rows.Next() {
		var p BypassAuditPin
		if err := rows.Scan(&p.ConversationID, &p.PinnedAt, &p.Reason); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) PinnedConversationIDs() ([]string, error) {
	pins, err := s.ListBypassAuditPins()
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(pins))
	for _, p := range pins {
		out = append(out, p.ConversationID)
	}
	return out, nil
}

func nullableString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func nullableBytes(b []byte) interface{} {
	if b == nil {
		return nil
	}
	return b
}
