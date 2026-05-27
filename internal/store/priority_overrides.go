// SPDX-License-Identifier: MIT
package store

//
// Schema lives in migrations/060_priority_overrides.sql ( ships the
// migration alongside the CRUD; the master plan §"Migration numbering
// coordination" reserves 060 for the priority_overrides + tmux_session_state
// joint pair, with adding tmux_session_state in a follow-up
// migration). uses these tables; extends.
//
// invariant boundary: internal/quota MUST NOT import internal/store. The
// only consumer of these helpers is internal/daemon/quotaadapter — that
// package's import list is the single legitimate co-location of
// internal/quota and internal/store anywhere in the codebase, enforced
// by the invariant compliance test.
//
// invariant audit hook: every priority_overrides mutation MUST emit a
// row in the events table inside the SAME transaction. The helpers below
// expose tx-scoped variants (UpsertPriorityOverrideTx /
// DeletePriorityOverrideTx / InsertEventTx) so the adapter can compose
// the multi-statement atomic write. A failure mid-transaction rolls back
// BOTH the priority_overrides change AND the audit event row —
// hash-chain integrity depends on this atomicity.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type PriorityOverrideRow struct {
	ProjectAlias string
	Multiplier   float64
	ExpiresAt    time.Time
	Reason       string
	CreatedAt    time.Time
}

func (s *Store) UpsertPriorityOverrideTx(ctx context.Context, tx *sql.Tx, row PriorityOverrideRow) (replaced bool, err error) {
	if tx == nil {
		return false, errors.New("store: UpsertPriorityOverrideTx: tx is nil")
	}
	if row.ProjectAlias == "" {
		return false, errors.New("store: UpsertPriorityOverrideTx: project_alias is empty")
	}
	if row.Multiplier <= 0 {
		return false, fmt.Errorf("store: UpsertPriorityOverrideTx: multiplier %v must be > 0", row.Multiplier)
	}
	var priorCount int
	err = tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM priority_overrides WHERE project_alias = ?`,
		row.ProjectAlias).Scan(&priorCount)
	if err != nil {
		return false, fmt.Errorf("store: probe priority_overrides: %w", err)
	}
	replaced = priorCount > 0
	_, err = tx.ExecContext(ctx, `
		INSERT INTO priority_overrides
			(project_alias, multiplier, expires_at, reason, created_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(project_alias) DO UPDATE SET
			multiplier = excluded.multiplier,
			expires_at = excluded.expires_at,
			reason = excluded.reason,
			created_at = excluded.created_at`,
		row.ProjectAlias, row.Multiplier, row.ExpiresAt, row.Reason, row.CreatedAt)
	if err != nil {
		return false, fmt.Errorf("store: upsert priority_overrides: %w", err)
	}
	return replaced, nil
}

func (s *Store) GetPriorityOverride(ctx context.Context, alias string) (*PriorityOverrideRow, error) {
	if alias == "" {
		return nil, errors.New("store: GetPriorityOverride: alias is empty")
	}
	var row PriorityOverrideRow
	err := s.db.QueryRowContext(ctx, `
		SELECT project_alias, multiplier, expires_at, reason, created_at
		FROM priority_overrides
		WHERE project_alias = ?`, alias).Scan(
		&row.ProjectAlias, &row.Multiplier, &row.ExpiresAt, &row.Reason, &row.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: get priority_override: %w", err)
	}
	return &row, nil
}

func (s *Store) DeletePriorityOverrideTx(ctx context.Context, tx *sql.Tx, alias string) error {
	if tx == nil {
		return errors.New("store: DeletePriorityOverrideTx: tx is nil")
	}
	if alias == "" {
		return errors.New("store: DeletePriorityOverrideTx: alias is empty")
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM priority_overrides WHERE project_alias = ?`, alias)
	if err != nil {
		return fmt.Errorf("store: delete priority_override: %w", err)
	}
	return nil
}

func (s *Store) ListPriorityOverrides(ctx context.Context) ([]PriorityOverrideRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT project_alias, multiplier, expires_at, reason, created_at
		FROM priority_overrides
		ORDER BY created_at DESC, project_alias ASC`)
	if err != nil {
		return nil, fmt.Errorf("store: list priority_overrides: %w", err)
	}
	defer rows.Close()
	out := make([]PriorityOverrideRow, 0)
	for rows.Next() {
		var r PriorityOverrideRow
		if err := rows.Scan(&r.ProjectAlias, &r.Multiplier, &r.ExpiresAt, &r.Reason, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("store: scan priority_override: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate priority_overrides: %w", err)
	}
	return out, nil
}

func (s *Store) InsertEventTx(ctx context.Context, tx *sql.Tx, kind, payload string) error {
	if tx == nil {
		return errors.New("store: InsertEventTx: tx is nil")
	}
	if kind == "" {
		return errors.New("store: InsertEventTx: kind is empty")
	}
	_, err := tx.ExecContext(ctx,
		`INSERT INTO events (ts, project, session_id, swarm_id, task_id, type, payload_json)
		 VALUES (?, '', '', '', '', ?, ?)`,
		time.Now().UTC().Unix(), kind, payload)
	if err != nil {
		return fmt.Errorf("store: insert event %q: %w", kind, err)
	}
	return nil
}

func (s *Store) ListEventsByKind(ctx context.Context, kind string) ([]EventRow, error) {
	if kind == "" {
		return nil, errors.New("store: ListEventsByKind: kind is empty")
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, ts, project, session_id, swarm_id, task_id, type, payload_json
		 FROM events
		 WHERE type = ?
		 ORDER BY id ASC`, kind)
	if err != nil {
		return nil, fmt.Errorf("store: list events by kind: %w", err)
	}
	defer rows.Close()
	out := make([]EventRow, 0)
	for rows.Next() {
		var ev EventRow
		var project, sessionID, swarmID, taskID, payloadJSON sqlNullString
		if err := rows.Scan(
			&ev.ID, &ev.TS, &project, &sessionID, &swarmID, &taskID, &ev.Type, &payloadJSON,
		); err != nil {
			return nil, fmt.Errorf("store: scan event: %w", err)
		}
		ev.Project = project.Get()
		ev.SessionID = sessionID.Get()
		ev.SwarmID = swarmID.Get()
		ev.TaskID = taskID.Get()
		ev.PayloadJSON = payloadJSON.Get()
		out = append(out, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate events: %w", err)
	}
	return out, nil
}

// BeginTx exposes a transaction so adapters can compose multi-statement
// atomic writes without having to reach through DB(). The returned tx
// uses default isolation (DEFERRED in SQLite).
//
// Callers MUST ensure either Commit or Rollback runs — leaking a tx
// holds the SQLite write lock until garbage collection. The conventional
// pattern is `defer tx.Rollback()` immediately after BeginTx, with
// `tx.Commit()` setting a `committed = true` flag the deferred Rollback
// inspects.
func (s *Store) BeginTx(ctx context.Context) (*sql.Tx, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("store: begin tx: %w", err)
	}
	return tx, nil
}

// ExecRaw runs a raw SQL statement and returns the result. Used by tests
// for setup / teardown actions that don't fit the typed CRUD surface
// (e.g., dropping the events table to simulate audit-failure scenarios
// in the quotaadapter rollback test).
//
// Production code paths MUST NOT use ExecRaw — every production write
// goes through a typed Store method so the schema stays auditable
// against the migrations directory.
func (s *Store) ExecRaw(ctx context.Context, sqlText string, args ...any) (sql.Result, error) {
	if sqlText == "" {
		return nil, errors.New("store: ExecRaw: sql is empty")
	}
	res, err := s.db.ExecContext(ctx, sqlText, args...)
	if err != nil {
		return nil, fmt.Errorf("store: exec raw: %w", err)
	}
	return res, nil
}
