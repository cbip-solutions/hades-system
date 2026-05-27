// go:build cgo

// SPDX-License-Identifier: MIT

package federation

import (
	"context"
	"fmt"
)

// MemberRow is the on-disk row shape for caronte_workspace_members. The
// PK on (workspace_id, project_id) makes a re-add of the same pair fail at
// the SQL layer; callers MUST use the §10.2 zen workspace link CLI
// to grow a roster (which validates ordering + capa-firewall + audit).
type MemberRow struct {
	WorkspaceID  string
	ProjectID    string
	RegisteredAt int64
}

func (w *WorkspaceFederationDB) AddMember(ctx context.Context, row MemberRow) error {
	if w.db == nil {
		return ErrEmptyDB
	}
	const q = `INSERT INTO caronte_workspace_members
	    (workspace_id, project_id, registered_at) VALUES (?, ?, ?)`
	if _, err := w.db.ExecContext(ctx, q, row.WorkspaceID, row.ProjectID, row.RegisteredAt); err != nil {
		return fmt.Errorf("caronte/store/federation: AddMember(%s/%s): %w", row.WorkspaceID, row.ProjectID, err)
	}
	return nil
}

func (w *WorkspaceFederationDB) ListWorkspaceMembers(ctx context.Context, workspaceID string) ([]MemberRow, error) {
	if w.db == nil {
		return nil, ErrEmptyDB
	}
	const q = `SELECT workspace_id, project_id, registered_at
	           FROM caronte_workspace_members
	           WHERE workspace_id = ?
	           ORDER BY registered_at ASC, project_id ASC`
	rows, err := w.db.QueryContext(ctx, q, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("caronte/store/federation: ListWorkspaceMembers(%s): %w", workspaceID, err)
	}
	defer rows.Close()
	out := make([]MemberRow, 0, 4)
	for rows.Next() {
		var m MemberRow
		if err := rows.Scan(&m.WorkspaceID, &m.ProjectID, &m.RegisteredAt); err != nil {
			return nil, fmt.Errorf("caronte/store/federation: ListWorkspaceMembers scan: %w", err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("caronte/store/federation: ListWorkspaceMembers iterate: %w", err)
	}
	return out, nil
}

func (w *WorkspaceFederationDB) RemoveMember(ctx context.Context, workspaceID, projectID string) (int64, error) {
	if w.db == nil {
		return 0, ErrEmptyDB
	}
	res, err := w.db.ExecContext(ctx,
		`DELETE FROM caronte_workspace_members WHERE workspace_id = ? AND project_id = ?`,
		workspaceID, projectID,
	)
	if err != nil {
		return 0, fmt.Errorf("caronte/store/federation: RemoveMember(%s/%s): %w", workspaceID, projectID, err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}
