//go:build cgo

// SPDX-License-Identifier: MIT

package federation

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

type WorkspaceRow struct {
	WorkspaceID   string
	OwningProject string
	PolicyLocked  bool
	CreatedAt     int64
	SchemaVersion int
}

func (w *WorkspaceFederationDB) RegisterWorkspace(ctx context.Context, row WorkspaceRow) error {
	if w.db == nil {
		return ErrEmptyDB
	}
	locked := 0
	if row.PolicyLocked {
		locked = 1
	}
	const q = `INSERT INTO caronte_workspaces
	    (workspace_id, owning_project, policy_locked, created_at, schema_version)
	    VALUES (?, ?, ?, ?, ?)`
	if _, err := w.db.ExecContext(ctx, q,
		row.WorkspaceID, row.OwningProject, locked, row.CreatedAt, row.SchemaVersion,
	); err != nil {
		return fmt.Errorf("caronte/store/federation: RegisterWorkspace(%s): %w", row.WorkspaceID, err)
	}
	return nil
}

func (w *WorkspaceFederationDB) GetWorkspace(ctx context.Context, workspaceID string) (WorkspaceRow, error) {
	if w.db == nil {
		return WorkspaceRow{}, ErrEmptyDB
	}
	const q = `SELECT workspace_id, owning_project, policy_locked, created_at, schema_version
	           FROM caronte_workspaces WHERE workspace_id = ?`
	var row WorkspaceRow
	var locked int
	err := w.db.QueryRowContext(ctx, q, workspaceID).Scan(
		&row.WorkspaceID, &row.OwningProject, &locked, &row.CreatedAt, &row.SchemaVersion,
	)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return WorkspaceRow{}, ErrNotFound
	case err != nil:
		return WorkspaceRow{}, fmt.Errorf("caronte/store/federation: GetWorkspace(%s): %w", workspaceID, err)
	}
	row.PolicyLocked = locked != 0
	return row, nil
}

func (w *WorkspaceFederationDB) ListWorkspaces(ctx context.Context) ([]WorkspaceRow, error) {
	if w.db == nil {
		return nil, ErrEmptyDB
	}
	const q = `SELECT workspace_id, owning_project, policy_locked, created_at, schema_version
	           FROM caronte_workspaces ORDER BY created_at ASC, workspace_id ASC`
	rows, err := w.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("caronte/store/federation: ListWorkspaces: %w", err)
	}
	defer rows.Close()
	out := make([]WorkspaceRow, 0, 8)
	for rows.Next() {
		var row WorkspaceRow
		var locked int
		if err := rows.Scan(
			&row.WorkspaceID, &row.OwningProject, &locked, &row.CreatedAt, &row.SchemaVersion,
		); err != nil {
			return nil, fmt.Errorf("caronte/store/federation: ListWorkspaces scan: %w", err)
		}
		row.PolicyLocked = locked != 0
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("caronte/store/federation: ListWorkspaces iterate: %w", err)
	}
	return out, nil
}

func (w *WorkspaceFederationDB) RemoveWorkspace(ctx context.Context, workspaceID string) (int64, error) {
	if w.db == nil {
		return 0, ErrEmptyDB
	}
	res, err := w.db.ExecContext(ctx, `DELETE FROM caronte_workspaces WHERE workspace_id = ?`, workspaceID)
	if err != nil {
		return 0, fmt.Errorf("caronte/store/federation: RemoveWorkspace(%s): %w", workspaceID, err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// SetWorkspacePolicy persists the operator-mutable policy_text (a
// JSON-encoded doctrine snapshot) for an existing workspace. Distinct
// from the registration-time policy_locked snapshot (which STAYS
// immutable for forensic continuity); SetWorkspacePolicy is the
// mutation path `hades workspace policy set` CLI / MCP surfaces.
// Emits a Tessera audit row (EvtWorkspacePolicySet) on every successful
// set so the doctrine-history trail is forensic.
//
// Audit-failure semantics (review I4):
//
// - The SQL UPDATE is committed FIRST; the audit-emit happens AFTER.
// - On audit-emit failure, the policy mutation has ALREADY been
// committed. Callers must treat audit-emit failure as a forensic-
// degradation signal (the chain has a missing leaf for this
// mutation), NOT a rollback signal — do NOT issue a compensating
// UPDATE to "undo" the mutation; the persisted state is the
// intended state and the missing leaf is the only loss.
// - This is INTENDED behavior — append-only audit chains cannot
// transact across an arbitrary persistence layer. The chain
// integrity is verified out-of-band Tessera STH; a
// missing leaf surfaces as a chain-recovery event in the next
// hades-day cycle.
// - Callers that need transactional ordering across the policy
// mutation AND its audit leaf should wrap SetWorkspacePolicy in
// a saga — outside this method's contract.
//
// Returns ErrEmptyDB on nil db, ErrNotFound when no row exists for the
// workspaceID, or a wrapped audit-emit / marshal error.
func (w *WorkspaceFederationDB) SetWorkspacePolicy(ctx context.Context, workspaceID, policy string) error {
	if w.db == nil {
		return ErrEmptyDB
	}
	res, err := w.db.ExecContext(ctx,
		`UPDATE caronte_workspaces SET policy_text = ? WHERE workspace_id = ?`,
		policy, workspaceID,
	)
	if err != nil {
		return fmt.Errorf("caronte/store/federation: SetWorkspacePolicy(%s): %w", workspaceID, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}

	if w.auditEmitter != nil {
		payload, err := json.Marshal(struct {
			WorkspaceID string `json:"workspace_id"`
			Policy      string `json:"policy"`
		}{WorkspaceID: workspaceID, Policy: policy})
		if err != nil {
			return fmt.Errorf("caronte/store/federation: SetWorkspacePolicy marshal: %w", err)
		}
		if err := w.auditEmitter.Emit(ctx, EvtWorkspacePolicySet, payload); err != nil {
			return fmt.Errorf("caronte/store/federation: SetWorkspacePolicy emit: %w", err)
		}
	}
	return nil
}

func (w *WorkspaceFederationDB) GetWorkspacePolicy(ctx context.Context, workspaceID string) (string, error) {
	if w.db == nil {
		return "", ErrEmptyDB
	}
	var policy sql.NullString
	err := w.db.QueryRowContext(ctx,
		`SELECT policy_text FROM caronte_workspaces WHERE workspace_id = ?`,
		workspaceID,
	).Scan(&policy)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return "", ErrNotFound
	case err != nil:
		return "", fmt.Errorf("caronte/store/federation: GetWorkspacePolicy(%s): %w", workspaceID, err)
	}
	if !policy.Valid {
		return "", nil
	}
	return policy.String, nil
}

func (w *WorkspaceFederationDB) EnableGraphQLNodeFallback(ctx context.Context, workspaceID string) (bool, error) {
	if w.db == nil {
		return false, ErrEmptyDB
	}
	var flag int
	err := w.db.QueryRowContext(ctx,
		`SELECT enable_graphql_node_fallback FROM caronte_workspaces WHERE workspace_id = ?`,
		workspaceID,
	).Scan(&flag)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return false, ErrNotFound
	case err != nil:
		return false, fmt.Errorf("caronte/store/federation: EnableGraphQLNodeFallback(%s): %w", workspaceID, err)
	}
	return flag != 0, nil
}

func (w *WorkspaceFederationDB) SetEnableGraphQLNodeFallback(ctx context.Context, workspaceID string, enabled bool) error {
	if w.db == nil {
		return ErrEmptyDB
	}
	flag := 0
	if enabled {
		flag = 1
	}
	res, err := w.db.ExecContext(ctx,
		`UPDATE caronte_workspaces SET enable_graphql_node_fallback = ? WHERE workspace_id = ?`,
		flag, workspaceID,
	)
	if err != nil {
		return fmt.Errorf("caronte/store/federation: SetEnableGraphQLNodeFallback(%s): %w", workspaceID, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
