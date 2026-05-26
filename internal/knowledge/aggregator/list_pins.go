// SPDX-License-Identifier: MIT
// Package aggregator — list_pins.go (Plan 9 Phase D-12).
//
// ListPins returns all rows from knowledge_pin_index, optionally filtered
// by projectID. This is a direct SELECT on aggregator.db (a.db) — the
// cross-project pin index owned by the Aggregator.
//
// Phase ownership: D-12 ships the basic SELECT path. Phase H extends with
// pagination (LIMIT + OFFSET) if operator UX reveals the need.
//
// Boundary (inv-zen-031): this file imports NO internal/store. Direct access
// to a.db is within the aggregator package's own DB — not the daemon store.
// inv-zen-129: no web calls.
package aggregator

import (
	"context"
	"errors"
	"fmt"
)

var ErrEmbedWorkerNotStarted = errors.New(
	"aggregator: embed_worker not started (Phase J seam; D-12 forward-compat)",
)

func (a *Aggregator) ListPins(ctx context.Context, projectID string) ([]PinNote, error) {
	if a.db == nil {
		return []PinNote{}, nil
	}

	var query string
	var args []any
	if projectID == "" {
		query = `
			SELECT note_id, project_id, title, content, frontmatter_json,
			       promoted_at, promoted_by, promote_reason, audit_chain_anchor
			FROM   knowledge_pin_index
			ORDER  BY promoted_at DESC
		`
	} else {
		query = `
			SELECT note_id, project_id, title, content, frontmatter_json,
			       promoted_at, promoted_by, promote_reason, audit_chain_anchor
			FROM   knowledge_pin_index
			WHERE  project_id = ?
			ORDER  BY promoted_at DESC
		`
		args = []any{projectID}
	}

	rows, err := a.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("aggregator: ListPins: query: %w", err)
	}
	defer rows.Close()

	var notes []PinNote
	for rows.Next() {
		var n PinNote
		if err := rows.Scan(
			&n.NoteID, &n.ProjectID, &n.Title, &n.Content,
			&n.FrontmatterJSON, &n.PromotedAt, &n.PromotedBy,
			&n.PromoteReason, &n.AuditChainAnchor,
		); err != nil {
			return nil, fmt.Errorf("aggregator: ListPins: scan: %w", err)
		}
		notes = append(notes, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("aggregator: ListPins: rows: %w", err)
	}
	if notes == nil {
		notes = []PinNote{}
	}
	return notes, nil
}

func (a *Aggregator) EnqueueRebuild(ctx context.Context, projectID string) error {
	_ = ctx
	a.mu.Lock()
	ch := a.rebuildCh
	a.mu.Unlock()

	if ch == nil {
		return ErrEmbedWorkerNotStarted
	}

	select {
	case ch <- VaultChangeEvent{ProjectID: projectID}:
	default:

	}
	return nil
}

func (a *Aggregator) SetRebuildChannel(ch chan VaultChangeEvent) {
	a.mu.Lock()
	a.rebuildCh = ch
	a.mu.Unlock()
}
