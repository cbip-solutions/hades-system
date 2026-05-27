//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

package store

import (
	"context"
	"database/sql"
	"fmt"
)

// UpsertEdge inserts or updates a relation row. Enforces invariant:
// e.Confidence MUST be Valid() (one of the frozen C-3 tiers) — an invalid
// confidence is rejected before any write, so no graph_edges row can carry
// an unknown confidence. PK is (source_id,target_id,kind,site_line); a
// re-resolve of the same call site updates confidence + reachable.
//
// reachable is a *bool → NULL when nil (CHA/SCIP, not pruned), else 1/0.
func (s *Store) UpsertEdge(ctx context.Context, e Edge) error {
	if !e.Confidence.Valid() {
		return fmt.Errorf("caronte/store: UpsertEdge: invalid confidence %q (inv-hades-233)", e.Confidence)
	}
	var reachable sql.NullInt64
	if e.Reachable != nil {
		reachable.Valid = true
		if *e.Reachable {
			reachable.Int64 = 1
		}
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO graph_edges
			(source_id, target_id, kind, confidence, reachable, site_file, site_line)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(source_id, target_id, kind, site_line) DO UPDATE SET
			confidence = excluded.confidence,
			reachable = excluded.reachable,
			site_file = excluded.site_file`,
		e.SourceID, e.TargetID, string(e.Kind), string(e.Confidence), reachable, e.SiteFile, e.SiteLine,
	)
	if err != nil {
		return fmt.Errorf("caronte/store: UpsertEdge: %w", err)
	}
	return nil
}

func scanEdges(rows *sql.Rows) ([]Edge, error) {
	out := []Edge{}
	for rows.Next() {
		var e Edge
		var kind, confidence string
		var reachable sql.NullInt64
		if err := rows.Scan(&e.SourceID, &e.TargetID, &kind, &confidence, &reachable, &e.SiteFile, &e.SiteLine); err != nil {
			return nil, fmt.Errorf("caronte/store: scan edge: %w", err)
		}
		e.Kind = kind
		e.Confidence = Confidence(confidence)
		if reachable.Valid {
			b := reachable.Int64 != 0
			e.Reachable = &b
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("caronte/store: edge rows: %w", err)
	}
	return out, nil
}

func (s *Store) ListEdgesByTarget(ctx context.Context, targetID string, kind EdgeKind) ([]Edge, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT source_id, target_id, kind, confidence, reachable, site_file, site_line
		FROM graph_edges WHERE target_id = ? AND kind = ?
		ORDER BY source_id ASC, site_line ASC`, targetID, string(kind),
	)
	if err != nil {
		return nil, fmt.Errorf("caronte/store: ListEdgesByTarget %q: %w", targetID, err)
	}
	defer rows.Close()
	return scanEdges(rows)
}

func (s *Store) ListEdgesBySource(ctx context.Context, sourceID string, kind EdgeKind) ([]Edge, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT source_id, target_id, kind, confidence, reachable, site_file, site_line
		FROM graph_edges WHERE source_id = ? AND kind = ?
		ORDER BY target_id ASC, site_line ASC`, sourceID, string(kind),
	)
	if err != nil {
		return nil, fmt.Errorf("caronte/store: ListEdgesBySource %q: %w", sourceID, err)
	}
	defer rows.Close()
	return scanEdges(rows)
}
