//go:build cgo

// SPDX-License-Identifier: MIT

package federation

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type UnresolvedRow struct {
	WorkspaceID string
	CallID      string
	CallRepo    string
	BaseURLRef  string
	Reason      string
	RecordedAt  int64
}

type UnresolvedStore interface {
	Insert(ctx context.Context, row UnresolvedRow) error
}

type unresolvedStoreImpl struct {
	parent *WorkspaceFederationDB
}

func (w *WorkspaceFederationDB) UnresolvedStore() UnresolvedStore {
	return &unresolvedStoreImpl{parent: w}
}

// Insert upserts one unresolved_calls row. PK = (workspace_id, call_id,
// call_repo) ⇒ a re-linker pass on the same gap overwrites; rows do not
// stack. The insert transits the same per-DB sql handle the LinkStore
// uses; the caller coordinates the policy gating.
func (u *unresolvedStoreImpl) Insert(ctx context.Context, row UnresolvedRow) error {
	if u.parent == nil || u.parent.db == nil {
		return ErrEmptyDB
	}
	const q = `INSERT INTO unresolved_calls
	    (workspace_id, call_id, call_repo, base_url_ref, reason, recorded_at)
	    VALUES (?, ?, ?, ?, ?, ?)
	    ON CONFLICT(workspace_id, call_id, call_repo) DO UPDATE SET
	        base_url_ref = excluded.base_url_ref,
	        reason       = excluded.reason,
	        recorded_at  = excluded.recorded_at`
	if _, err := u.parent.db.ExecContext(ctx, q,
		row.WorkspaceID, row.CallID, row.CallRepo,
		row.BaseURLRef, row.Reason, row.RecordedAt,
	); err != nil {
		return fmt.Errorf("caronte/store/federation: UnresolvedStore.Insert(%s/%s): %w",
			row.WorkspaceID, row.CallID, err)
	}
	return nil
}

func (w *WorkspaceFederationDB) GetUnresolved(ctx context.Context, workspaceID, callID, callRepo string) (UnresolvedRow, error) {
	if w.db == nil {
		return UnresolvedRow{}, ErrEmptyDB
	}
	const q = `SELECT workspace_id, call_id, call_repo, base_url_ref, reason, recorded_at
	           FROM unresolved_calls
	           WHERE workspace_id = ? AND call_id = ? AND call_repo = ?`
	var r UnresolvedRow
	err := w.db.QueryRowContext(ctx, q, workspaceID, callID, callRepo).Scan(
		&r.WorkspaceID, &r.CallID, &r.CallRepo,
		&r.BaseURLRef, &r.Reason, &r.RecordedAt,
	)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return UnresolvedRow{}, ErrNotFound
	case err != nil:
		return UnresolvedRow{}, fmt.Errorf("caronte/store/federation: GetUnresolved(%s/%s/%s): %w", workspaceID, callID, callRepo, err)
	}
	return r, nil
}

func (w *WorkspaceFederationDB) ListUnresolvedByWorkspace(ctx context.Context, workspaceID string, limit int) ([]UnresolvedRow, error) {
	if w.db == nil {
		return nil, ErrEmptyDB
	}
	if limit <= 0 {
		limit = 1000
	}
	const q = `SELECT workspace_id, call_id, call_repo, base_url_ref, reason, recorded_at
	           FROM unresolved_calls
	           WHERE workspace_id = ?
	           ORDER BY call_repo ASC, call_id ASC
	           LIMIT ?`
	rows, err := w.db.QueryContext(ctx, q, workspaceID, limit)
	if err != nil {
		return nil, fmt.Errorf("caronte/store/federation: ListUnresolvedByWorkspace(%s): %w", workspaceID, err)
	}
	defer rows.Close()
	out := make([]UnresolvedRow, 0, 4)
	for rows.Next() {
		var r UnresolvedRow
		if err := rows.Scan(
			&r.WorkspaceID, &r.CallID, &r.CallRepo,
			&r.BaseURLRef, &r.Reason, &r.RecordedAt,
		); err != nil {
			return nil, fmt.Errorf("caronte/store/federation: ListUnresolvedByWorkspace scan: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("caronte/store/federation: ListUnresolvedByWorkspace iterate: %w", err)
	}
	return out, nil
}
