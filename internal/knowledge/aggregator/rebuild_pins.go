//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT
package aggregator

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

var errRebuildEmbedderUnavailable = errors.New("aggregator: RebuildPinnedEmbeddings: embedder unavailable")

func (a *Aggregator) RebuildPinnedEmbeddings(ctx context.Context, projectID string) (int, error) {
	if a == nil || a.db == nil {
		return 0, errors.New("aggregator: RebuildPinnedEmbeddings: DB required")
	}
	if a.embedder == nil {
		return 0, errRebuildEmbedderUnavailable
	}
	where := ""
	args := []any{}
	if projectID != "" {
		where = " WHERE project_id = ?"
		args = append(args, projectID)
	}
	rows, err := a.db.QueryContext(ctx, `
		SELECT note_id, title, content
		FROM knowledge_pin_index`+where+`
		ORDER BY note_id ASC`,
		args...,
	)
	if err != nil {
		return 0, fmt.Errorf("aggregator: RebuildPinnedEmbeddings: list pins: %w", err)
	}
	defer rows.Close()

	type pin struct {
		noteID  string
		title   string
		content string
	}
	var pins []pin
	for rows.Next() {
		var p pin
		if err := rows.Scan(&p.noteID, &p.title, &p.content); err != nil {
			return 0, fmt.Errorf("aggregator: RebuildPinnedEmbeddings: scan: %w", err)
		}
		pins = append(pins, p)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("aggregator: RebuildPinnedEmbeddings: rows: %w", err)
	}

	rebuilt := 0
	for _, p := range pins {
		if err := a.rebuildPinnedRow(ctx, p.noteID, p.title, p.content); err != nil {
			return rebuilt, err
		}
		rebuilt++
	}
	return rebuilt, nil
}

func (a *Aggregator) rebuildPinnedRow(ctx context.Context, noteID, title, content string) error {
	emb, err := a.embedder.Embed(ctx, content)
	if err != nil {
		return fmt.Errorf("%w: %w", errRebuildEmbedderUnavailable, err)
	}
	if len(emb) != vecDimensions {
		return fmt.Errorf(
			"aggregator: RebuildPinnedEmbeddings: embedding dim %d != vecDimensions %d",
			len(emb), vecDimensions,
		)
	}

	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("aggregator: RebuildPinnedEmbeddings: begin tx: %w", err)
	}
	defer tx.Rollback()

	if err := rebuildFTSRow(ctx, tx, noteID, title, content); err != nil {
		return err
	}
	if !a.Degraded() {
		if err := upsertVecRowTx(ctx, tx, noteID, float32SliceBytes(emb)); err != nil {
			return fmt.Errorf("aggregator: RebuildPinnedEmbeddings: upsert vec %q: %w", noteID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("aggregator: RebuildPinnedEmbeddings: commit: %w", err)
	}
	return nil
}

func upsertVecRowTx(ctx context.Context, tx *sql.Tx, noteID string, embBytes []byte) error {
	_, _ = tx.ExecContext(ctx, `
		DELETE FROM knowledge_pin_vec
		WHERE rowid = (SELECT rowid FROM knowledge_pin_index WHERE note_id = ?)
	`, noteID)
	_, err := tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO knowledge_pin_vec(rowid, embedding)
		SELECT rowid, ? FROM knowledge_pin_index WHERE note_id = ?
	`, embBytes, noteID)
	return err
}

func rebuildFTSRow(ctx context.Context, tx *sql.Tx, noteID, title, content string) error {
	var rowID int64
	if err := tx.QueryRowContext(ctx,
		`SELECT rowid FROM knowledge_pin_index WHERE note_id = ?`,
		noteID,
	).Scan(&rowID); err != nil {
		return fmt.Errorf("aggregator: RebuildPinnedEmbeddings: rowid %q: %w", noteID, err)
	}
	_, _ = tx.ExecContext(ctx, `
		INSERT INTO knowledge_pin_fts(knowledge_pin_fts, rowid, content, title)
		VALUES('delete', ?, ?, ?)`,
		rowID, content, title,
	)
	_, err := tx.ExecContext(ctx, `
		INSERT INTO knowledge_pin_fts(rowid, content, title)
		VALUES (?, ?, ?)`,
		rowID, content, title,
	)
	if err != nil {
		return fmt.Errorf("aggregator: RebuildPinnedEmbeddings: insert fts %q: %w", noteID, err)
	}
	return nil
}
