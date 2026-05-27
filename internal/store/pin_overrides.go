// SPDX-License-Identifier: MIT
// pin_overrides.go — CRUD for the pin_overrides table.
//
// The pin_overrides table stores operator-set tier pins at three scope levels:
// session, project, and global. At most one active pin exists per (scope, scope_id)
// pair — the UNIQUE constraint is enforced at the SQL layer (inv-hades-063).
//
// Scope vocabulary (CHECK-constrained at SQL):
// - "session" — pin applies to a single Claude Code session
// - "project" — pin applies across all sessions for a project
// - "global" — pin applies to all projects; ScopeID must be ""
//
// ScopeID is "" for the global scope. Using NULL would allow multiple global
// rows because SQLite treats NULLs as distinct in UNIQUE constraints; empty
// string enforces the single-global-pin invariant.
//
// ExpiresAt is nil when the pin has no TTL (permanent until explicitly unset).
// Non-nil ExpiresAt is stored as INTEGER unix epoch seconds; the orchestrator's
// 5-min sweep calls PurgeExpiredPins to remove stale rows.
//
// inv-hades-063: this table stores PIN INTENT only. The cap check in
// tier_resolver.Select overrides a pin when the pinned tier has
// reached its budget cap. There is no SQL coupling to cost_ledger; the
// invariant is enforced by the capOverridesPin sentinel + integration tests.
//
// Import boundary: stdlib only (database/sql, errors, fmt, time).
// No github.com/ncruces imports — pin ops do not need typed SQLite errors
// because we do not need to distinguish constraint types; all insert errors
// bubble up as-is (the UPSERT pattern means UNIQUE conflicts do not arise
// under normal use — they are resolved in-place by ON CONFLICT DO UPDATE).

package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type PinRow struct {
	ID        int64
	Scope     string
	ScopeID   string
	Tier      string
	Provider  string
	SetAt     time.Time
	ExpiresAt *time.Time
	Reason    string
}

func (s *Store) InsertPin(p PinRow) error {
	_, err := s.db.Exec(
		`INSERT INTO pin_overrides
		    (scope, scope_id, tier, provider, set_at, expires_at, reason)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(scope, scope_id) DO UPDATE SET
		    tier       = excluded.tier,
		    provider   = excluded.provider,
		    set_at     = excluded.set_at,
		    expires_at = excluded.expires_at,
		    reason     = excluded.reason`,
		p.Scope,
		p.ScopeID,
		p.Tier,
		p.Provider,
		p.SetAt.UTC().Unix(),
		nullableInt64(p.ExpiresAt),
		p.Reason,
	)
	if err != nil {
		return fmt.Errorf("insert pin_overrides: %w", err)
	}
	return nil
}

func (s *Store) QueryPin(scope, scopeID string) (*PinRow, error) {
	row := s.db.QueryRow(
		`SELECT id, scope, scope_id, tier, provider, set_at, expires_at, reason
		 FROM pin_overrides WHERE scope = ? AND scope_id = ?`,
		scope, scopeID,
	)
	p, err := scanPinRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query pin_overrides: %w", err)
	}
	return p, nil
}

func (s *Store) DeletePin(scope, scopeID string) error {
	_, err := s.db.Exec(
		`DELETE FROM pin_overrides WHERE scope = ? AND scope_id = ?`,
		scope, scopeID,
	)
	if err != nil {
		return fmt.Errorf("delete pin_overrides: %w", err)
	}
	return nil
}

func (s *Store) ListAllPins() ([]PinRow, error) {
	rows, err := s.db.Query(
		`SELECT id, scope, scope_id, tier, provider, set_at, expires_at, reason
		 FROM pin_overrides ORDER BY set_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list pin_overrides: %w", err)
	}
	defer rows.Close()

	var out []PinRow
	for rows.Next() {
		p, err := scanPinRow(rows)
		if err != nil {
			return nil, fmt.Errorf("scan pin_overrides row: %w", err)
		}
		out = append(out, *p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pin_overrides rows: %w", err)
	}
	return out, nil
}

func (s *Store) PurgeExpiredPins(now time.Time) (int, error) {
	res, err := s.db.Exec(
		`DELETE FROM pin_overrides
		 WHERE expires_at IS NOT NULL AND expires_at < ?`,
		now.UTC().Unix(),
	)
	if err != nil {
		return 0, fmt.Errorf("purge expired pin_overrides: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("pin_overrides purge rows affected: %w", err)
	}
	return int(n), nil
}

type pinScanner interface {
	Scan(dest ...interface{}) error
}

func scanPinRow(rs pinScanner) (*PinRow, error) {
	var (
		p          PinRow
		setAtSec   int64
		expiresSec sql.NullInt64
	)
	if err := rs.Scan(
		&p.ID, &p.Scope, &p.ScopeID, &p.Tier, &p.Provider,
		&setAtSec, &expiresSec, &p.Reason,
	); err != nil {
		return nil, err
	}
	p.SetAt = time.Unix(setAtSec, 0).UTC()
	if expiresSec.Valid {
		t := time.Unix(expiresSec.Int64, 0).UTC()
		p.ExpiresAt = &t
	}
	return &p, nil
}

func nullableInt64(t *time.Time) interface{} {
	if t == nil {
		return nil
	}
	return t.UTC().Unix()
}
