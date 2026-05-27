// go:build cgo

// SPDX-License-Identifier: MIT

package federation

import (
	"context"
	"database/sql"
	"fmt"
)

type BreakingChangeConsumer struct {
	ChangeID string
	CallID   string
	CallRepo string
}

const insertBreakingChangeConsumerSQL = `INSERT INTO breaking_change_consumers
	(change_id, call_id, call_repo) VALUES (?, ?, ?)`

func (w *WorkspaceFederationDB) InsertBreakingChangeConsumer(ctx context.Context, row BreakingChangeConsumer) error {
	if w.db == nil {
		return ErrEmptyDB
	}
	if _, err := w.db.ExecContext(ctx, insertBreakingChangeConsumerSQL, row.ChangeID, row.CallID, row.CallRepo); err != nil {
		return fmt.Errorf("caronte/store/federation: InsertBreakingChangeConsumer(%s/%s): %w", row.ChangeID, row.CallID, err)
	}
	return nil
}

// InsertBreakingChangeConsumerTx persists a consumer row INSIDE a
// caller-owned *sql.Tx — used by Pipeline.Fan to keep the N consumer
// inserts atomic with their parent breaking_changes row (per code-review
// I-3 fix). Caller MUST Commit or Rollback the returned tx; this method
// only issues the INSERT.
//
// Mirrors InsertBreakingChangeConsumer's column-set verbatim (shared
// insertBreakingChangeConsumerSQL constant).
func (w *WorkspaceFederationDB) InsertBreakingChangeConsumerTx(ctx context.Context, tx *sql.Tx, row BreakingChangeConsumer) error {
	if tx == nil {
		return fmt.Errorf("caronte/store/federation: InsertBreakingChangeConsumerTx: tx is nil")
	}
	if _, err := tx.ExecContext(ctx, insertBreakingChangeConsumerSQL, row.ChangeID, row.CallID, row.CallRepo); err != nil {
		return fmt.Errorf("caronte/store/federation: InsertBreakingChangeConsumerTx(%s/%s): %w", row.ChangeID, row.CallID, err)
	}
	return nil
}

func (w *WorkspaceFederationDB) ListBreakingChangeConsumers(ctx context.Context, changeID string) ([]BreakingChangeConsumer, error) {
	if w.db == nil {
		return nil, ErrEmptyDB
	}
	const q = `SELECT change_id, call_id, call_repo
	           FROM breaking_change_consumers
	           WHERE change_id = ?
	           ORDER BY call_repo ASC, call_id ASC`
	rows, err := w.db.QueryContext(ctx, q, changeID)
	if err != nil {
		return nil, fmt.Errorf("caronte/store/federation: ListBreakingChangeConsumers(%s): %w", changeID, err)
	}
	defer rows.Close()
	out := make([]BreakingChangeConsumer, 0, 4)
	for rows.Next() {
		var r BreakingChangeConsumer
		if err := rows.Scan(&r.ChangeID, &r.CallID, &r.CallRepo); err != nil {
			return nil, fmt.Errorf("caronte/store/federation: ListBreakingChangeConsumers scan: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("caronte/store/federation: ListBreakingChangeConsumers iterate: %w", err)
	}
	return out, nil
}

func (w *WorkspaceFederationDB) DeleteConsumersByChange(ctx context.Context, changeID string) (int64, error) {
	if w.db == nil {
		return 0, ErrEmptyDB
	}
	res, err := w.db.ExecContext(ctx,
		`DELETE FROM breaking_change_consumers WHERE change_id = ?`, changeID,
	)
	if err != nil {
		return 0, fmt.Errorf("caronte/store/federation: DeleteConsumersByChange(%s): %w", changeID, err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}
