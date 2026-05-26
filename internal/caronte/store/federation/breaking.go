//go:build cgo

// SPDX-License-Identifier: MIT

package federation

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

type BreakingChange struct {
	ChangeID       string
	WorkspaceID    string
	EndpointID     string
	EndpointRepo   string
	Kind           string
	Detail         string
	DetectedAt     int64
	DetectorID     string
	LoreAuthor     string
	LoreCommitSHA  string
	LoreADRRefs    string
	LoreSupersedes string
}

const insertBreakingChangeSQL = `INSERT INTO breaking_changes
	(change_id, workspace_id, endpoint_id, endpoint_repo, kind, detail,
	 detected_at, detector_id, lore_author, lore_commit_sha,
	 lore_adr_refs, lore_supersedes)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

func (w *WorkspaceFederationDB) InsertBreakingChange(ctx context.Context, row BreakingChange) error {
	if w.db == nil {
		return ErrEmptyDB
	}
	if _, err := w.db.ExecContext(ctx, insertBreakingChangeSQL,
		row.ChangeID, row.WorkspaceID, row.EndpointID, row.EndpointRepo,
		row.Kind, row.Detail, row.DetectedAt, row.DetectorID,
		nullString(row.LoreAuthor), nullString(row.LoreCommitSHA),
		nullString(row.LoreADRRefs), nullString(row.LoreSupersedes),
	); err != nil {
		return fmt.Errorf("caronte/store/federation: InsertBreakingChange(%s): %w", row.ChangeID, err)
	}
	return nil
}

// InsertBreakingChangeTx persists a breaking-change row INSIDE a
// caller-owned *sql.Tx — used by Pipeline.Fan to wrap one row + N consumer
// inserts in one atomic block (per code-review I-3 fix: a mid-finding
// consumer-insert failure must NOT leave a half-finding state in
// breaking_changes / breaking_change_consumers). Caller MUST Commit or
// Rollback the returned tx.
//
// Mirrors InsertBreakingChange's column-set verbatim (shared
// insertBreakingChangeSQL constant) so drift between the two paths is
// structurally impossible.
func (w *WorkspaceFederationDB) InsertBreakingChangeTx(ctx context.Context, tx *sql.Tx, row BreakingChange) error {
	if tx == nil {
		return fmt.Errorf("caronte/store/federation: InsertBreakingChangeTx: tx is nil")
	}
	if _, err := tx.ExecContext(ctx, insertBreakingChangeSQL,
		row.ChangeID, row.WorkspaceID, row.EndpointID, row.EndpointRepo,
		row.Kind, row.Detail, row.DetectedAt, row.DetectorID,
		nullString(row.LoreAuthor), nullString(row.LoreCommitSHA),
		nullString(row.LoreADRRefs), nullString(row.LoreSupersedes),
	); err != nil {
		return fmt.Errorf("caronte/store/federation: InsertBreakingChangeTx(%s): %w", row.ChangeID, err)
	}
	return nil
}

func (w *WorkspaceFederationDB) GetBreakingChange(ctx context.Context, changeID string) (BreakingChange, error) {
	if w.db == nil {
		return BreakingChange{}, ErrEmptyDB
	}
	const q = `SELECT change_id, workspace_id, endpoint_id, endpoint_repo,
	                  kind, detail, detected_at, detector_id,
	                  COALESCE(lore_author, ''), COALESCE(lore_commit_sha, ''),
	                  COALESCE(lore_adr_refs, ''), COALESCE(lore_supersedes, '')
	           FROM breaking_changes WHERE change_id = ?`
	var r BreakingChange
	err := w.db.QueryRowContext(ctx, q, changeID).Scan(
		&r.ChangeID, &r.WorkspaceID, &r.EndpointID, &r.EndpointRepo,
		&r.Kind, &r.Detail, &r.DetectedAt, &r.DetectorID,
		&r.LoreAuthor, &r.LoreCommitSHA, &r.LoreADRRefs, &r.LoreSupersedes,
	)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return BreakingChange{}, ErrNotFound
	case err != nil:
		return BreakingChange{}, fmt.Errorf("caronte/store/federation: GetBreakingChange(%s): %w", changeID, err)
	}
	return r, nil
}

func (w *WorkspaceFederationDB) ListBreakingChangesByEndpoint(ctx context.Context, workspaceID, endpointID, endpointRepo string) ([]BreakingChange, error) {
	if w.db == nil {
		return nil, ErrEmptyDB
	}
	const q = `SELECT change_id, workspace_id, endpoint_id, endpoint_repo,
	                  kind, detail, detected_at, detector_id,
	                  COALESCE(lore_author, ''), COALESCE(lore_commit_sha, ''),
	                  COALESCE(lore_adr_refs, ''), COALESCE(lore_supersedes, '')
	           FROM breaking_changes
	           WHERE workspace_id = ? AND endpoint_id = ? AND endpoint_repo = ?
	           ORDER BY detected_at DESC, change_id ASC`
	rows, err := w.db.QueryContext(ctx, q, workspaceID, endpointID, endpointRepo)
	if err != nil {
		return nil, fmt.Errorf("caronte/store/federation: ListBreakingChangesByEndpoint: %w", err)
	}
	defer rows.Close()
	out := make([]BreakingChange, 0, 4)
	for rows.Next() {
		var r BreakingChange
		if err := rows.Scan(
			&r.ChangeID, &r.WorkspaceID, &r.EndpointID, &r.EndpointRepo,
			&r.Kind, &r.Detail, &r.DetectedAt, &r.DetectorID,
			&r.LoreAuthor, &r.LoreCommitSHA, &r.LoreADRRefs, &r.LoreSupersedes,
		); err != nil {
			return nil, fmt.Errorf("caronte/store/federation: ListBreakingChangesByEndpoint scan: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("caronte/store/federation: ListBreakingChangesByEndpoint iterate: %w", err)
	}
	return out, nil
}

func (w *WorkspaceFederationDB) DeleteBreakingChangesByWorkspace(ctx context.Context, workspaceID string) (int64, error) {
	if w.db == nil {
		return 0, ErrEmptyDB
	}
	res, err := w.db.ExecContext(ctx,
		`DELETE FROM breaking_changes WHERE workspace_id = ?`, workspaceID,
	)
	if err != nil {
		return 0, fmt.Errorf("caronte/store/federation: DeleteBreakingChangesByWorkspace(%s): %w", workspaceID, err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// GetBreakingChangeWithConsumers performs an ATOMIC read of one
// breaking_changes row plus its full consumer fan-out (breaking_change_consumers
// rows joined by change_id). The two-query pattern uses a single
// transaction at the SQL layer so a concurrent consumer-INSERT can't
// straddle the read (Phase H's Coordinator MUST see a coherent snapshot
// to drive coordinated-fix dispatch).
//
// Returns ErrNotFound when the breaking_changes row is absent; the
// consumer slice is empty when present-but-no-consumers (legal — Phase G
// may emit a breaking-change before the linker has surfaced consumers).
func (w *WorkspaceFederationDB) GetBreakingChangeWithConsumers(ctx context.Context, changeID string) (BreakingChange, []BreakingChangeConsumer, error) {
	if w.db == nil {
		return BreakingChange{}, nil, ErrEmptyDB
	}
	tx, err := w.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return BreakingChange{}, nil, fmt.Errorf("caronte/store/federation: GetBreakingChangeWithConsumers begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	const qChange = `SELECT change_id, workspace_id, endpoint_id, endpoint_repo,
	                        kind, detail, detected_at, detector_id,
	                        COALESCE(lore_author, ''), COALESCE(lore_commit_sha, ''),
	                        COALESCE(lore_adr_refs, ''), COALESCE(lore_supersedes, '')
	                 FROM breaking_changes WHERE change_id = ?`
	var r BreakingChange
	err = tx.QueryRowContext(ctx, qChange, changeID).Scan(
		&r.ChangeID, &r.WorkspaceID, &r.EndpointID, &r.EndpointRepo,
		&r.Kind, &r.Detail, &r.DetectedAt, &r.DetectorID,
		&r.LoreAuthor, &r.LoreCommitSHA, &r.LoreADRRefs, &r.LoreSupersedes,
	)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return BreakingChange{}, nil, ErrNotFound
	case err != nil:
		return BreakingChange{}, nil, fmt.Errorf("caronte/store/federation: GetBreakingChangeWithConsumers scan change: %w", err)
	}
	const qConsumers = `SELECT change_id, call_id, call_repo
	                    FROM breaking_change_consumers
	                    WHERE change_id = ?
	                    ORDER BY call_repo ASC, call_id ASC`
	rows, err := tx.QueryContext(ctx, qConsumers, changeID)
	if err != nil {
		return BreakingChange{}, nil, fmt.Errorf("caronte/store/federation: GetBreakingChangeWithConsumers consumers: %w", err)
	}
	defer rows.Close()
	consumers := make([]BreakingChangeConsumer, 0, 4)
	for rows.Next() {
		var c BreakingChangeConsumer
		if err := rows.Scan(&c.ChangeID, &c.CallID, &c.CallRepo); err != nil {
			return BreakingChange{}, nil, fmt.Errorf("caronte/store/federation: GetBreakingChangeWithConsumers scan consumer: %w", err)
		}
		consumers = append(consumers, c)
	}
	if err := rows.Err(); err != nil {
		return BreakingChange{}, nil, fmt.Errorf("caronte/store/federation: GetBreakingChangeWithConsumers iterate: %w", err)
	}
	return r, consumers, nil
}

func (w *WorkspaceFederationDB) ListRecentBreakingChanges(ctx context.Context, workspaceID string, limit int) ([]BreakingChange, error) {
	if w.db == nil {
		return nil, ErrEmptyDB
	}
	if limit <= 0 {
		limit = 100
	}
	const q = `SELECT change_id, workspace_id, endpoint_id, endpoint_repo,
	                  kind, detail, detected_at, detector_id,
	                  COALESCE(lore_author, ''), COALESCE(lore_commit_sha, ''),
	                  COALESCE(lore_adr_refs, ''), COALESCE(lore_supersedes, '')
	           FROM breaking_changes
	           WHERE workspace_id = ?
	           ORDER BY detected_at DESC, change_id ASC
	           LIMIT ?`
	rows, err := w.db.QueryContext(ctx, q, workspaceID, limit)
	if err != nil {
		return nil, fmt.Errorf("caronte/store/federation: ListRecentBreakingChanges(%s): %w", workspaceID, err)
	}
	defer rows.Close()
	out := make([]BreakingChange, 0, 4)
	for rows.Next() {
		var r BreakingChange
		if err := rows.Scan(
			&r.ChangeID, &r.WorkspaceID, &r.EndpointID, &r.EndpointRepo,
			&r.Kind, &r.Detail, &r.DetectedAt, &r.DetectorID,
			&r.LoreAuthor, &r.LoreCommitSHA, &r.LoreADRRefs, &r.LoreSupersedes,
		); err != nil {
			return nil, fmt.Errorf("caronte/store/federation: ListRecentBreakingChanges scan: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("caronte/store/federation: ListRecentBreakingChanges iterate: %w", err)
	}
	return out, nil
}

func ToStoreBreakingChange(row BreakingChange) store.BreakingChange {
	return store.BreakingChange{
		ChangeID:       row.ChangeID,
		WorkspaceID:    row.WorkspaceID,
		EndpointID:     row.EndpointID,
		EndpointRepo:   row.EndpointRepo,
		Kind:           row.Kind,
		Detail:         []byte(row.Detail),
		DetectedAt:     row.DetectedAt,
		DetectorID:     row.DetectorID,
		LoreAuthor:     row.LoreAuthor,
		LoreCommitSHA:  row.LoreCommitSHA,
		LoreADRRefs:    []byte(row.LoreADRRefs),
		LoreSupersedes: []byte(row.LoreSupersedes),
	}
}

func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{Valid: false}
	}
	return sql.NullString{String: s, Valid: true}
}
