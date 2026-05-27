//go:build cgo

// SPDX-License-Identifier: MIT

package federation

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type LinkRow struct {
	CallID       string
	CallRepo     string
	EndpointID   string
	EndpointRepo string
	Confidence   string
	WorkspaceID  string
	ResolvedAt   int64
	LinkMethod   string
}

type LinkStore interface {
	Append(ctx context.Context, row LinkRow) error
}

type linkStoreImpl struct {
	parent *WorkspaceFederationDB
}

func (w *WorkspaceFederationDB) LinkStore() LinkStore { return &linkStoreImpl{parent: w} }

func (l *linkStoreImpl) Append(ctx context.Context, row LinkRow) error {
	if l.parent == nil || l.parent.db == nil {
		return ErrEmptyDB
	}
	const q = `INSERT INTO contract_links
	    (call_id, call_repo, endpoint_id, endpoint_repo, confidence,
	     workspace_id, resolved_at, link_method)
	    VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	if _, err := l.parent.db.ExecContext(ctx, q,
		row.CallID, row.CallRepo, row.EndpointID, row.EndpointRepo, row.Confidence,
		row.WorkspaceID, row.ResolvedAt, row.LinkMethod,
	); err != nil {

		return fmt.Errorf("caronte/store/federation: LinkStore.Append(%s→%s): %w",
			row.CallID, row.EndpointID, err)
	}
	return nil
}

func (w *WorkspaceFederationDB) GetLink(ctx context.Context, workspaceID, callID, endpointID string) (LinkRow, error) {
	if w.db == nil {
		return LinkRow{}, ErrEmptyDB
	}
	const q = `SELECT call_id, call_repo, endpoint_id, endpoint_repo, confidence,
	                  workspace_id, resolved_at, link_method
	           FROM contract_links
	           WHERE workspace_id = ? AND call_id = ? AND endpoint_id = ?`
	var r LinkRow
	err := w.db.QueryRowContext(ctx, q, workspaceID, callID, endpointID).Scan(
		&r.CallID, &r.CallRepo, &r.EndpointID, &r.EndpointRepo, &r.Confidence,
		&r.WorkspaceID, &r.ResolvedAt, &r.LinkMethod,
	)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return LinkRow{}, ErrNotFound
	case err != nil:
		return LinkRow{}, fmt.Errorf("caronte/store/federation: GetLink(%s/%s→%s): %w", workspaceID, callID, endpointID, err)
	}
	return r, nil
}

func (w *WorkspaceFederationDB) ListByEndpoint(ctx context.Context, workspaceID, endpointID, endpointRepo string) ([]LinkRow, error) {
	if w.db == nil {
		return nil, ErrEmptyDB
	}
	const q = `SELECT call_id, call_repo, endpoint_id, endpoint_repo, confidence,
	                  workspace_id, resolved_at, link_method
	           FROM contract_links
	           WHERE workspace_id = ? AND endpoint_id = ? AND endpoint_repo = ?
	           ORDER BY call_repo ASC, call_id ASC`
	rows, err := w.db.QueryContext(ctx, q, workspaceID, endpointID, endpointRepo)
	if err != nil {
		return nil, fmt.Errorf("caronte/store/federation: ListByEndpoint: %w", err)
	}
	defer rows.Close()
	return scanLinkRows(rows)
}

func (w *WorkspaceFederationDB) ListByCall(ctx context.Context, workspaceID, callID, callRepo string) ([]LinkRow, error) {
	if w.db == nil {
		return nil, ErrEmptyDB
	}
	const q = `SELECT call_id, call_repo, endpoint_id, endpoint_repo, confidence,
	                  workspace_id, resolved_at, link_method
	           FROM contract_links
	           WHERE workspace_id = ? AND call_id = ? AND call_repo = ?
	           ORDER BY endpoint_repo ASC, endpoint_id ASC`
	rows, err := w.db.QueryContext(ctx, q, workspaceID, callID, callRepo)
	if err != nil {
		return nil, fmt.Errorf("caronte/store/federation: ListByCall: %w", err)
	}
	defer rows.Close()
	return scanLinkRows(rows)
}

func (w *WorkspaceFederationDB) DeleteLinksByWorkspace(ctx context.Context, workspaceID string) (int64, error) {
	if w.db == nil {
		return 0, ErrEmptyDB
	}
	res, err := w.db.ExecContext(ctx,
		`DELETE FROM contract_links WHERE workspace_id = ?`, workspaceID,
	)
	if err != nil {
		return 0, fmt.Errorf("caronte/store/federation: DeleteLinksByWorkspace(%s): %w", workspaceID, err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func (w *WorkspaceFederationDB) ListContractLinks(ctx context.Context, workspaceID string, limit int) ([]LinkRow, error) {
	if w.db == nil {
		return nil, ErrEmptyDB
	}
	if limit <= 0 {
		limit = 1000
	}
	const q = `SELECT call_id, call_repo, endpoint_id, endpoint_repo, confidence,
	                  workspace_id, resolved_at, link_method
	           FROM contract_links
	           WHERE workspace_id = ?
	           ORDER BY call_repo ASC, call_id ASC, endpoint_id ASC
	           LIMIT ?`
	rows, err := w.db.QueryContext(ctx, q, workspaceID, limit)
	if err != nil {
		return nil, fmt.Errorf("caronte/store/federation: ListContractLinks(%s): %w", workspaceID, err)
	}
	defer rows.Close()
	return scanLinkRows(rows)
}

func scanLinkRows(rows *sql.Rows) ([]LinkRow, error) {
	out := make([]LinkRow, 0, 4)
	for rows.Next() {
		var r LinkRow
		if err := rows.Scan(
			&r.CallID, &r.CallRepo, &r.EndpointID, &r.EndpointRepo, &r.Confidence,
			&r.WorkspaceID, &r.ResolvedAt, &r.LinkMethod,
		); err != nil {
			return nil, fmt.Errorf("caronte/store/federation: scanLinkRows: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("caronte/store/federation: scanLinkRows iterate: %w", err)
	}
	return out, nil
}
