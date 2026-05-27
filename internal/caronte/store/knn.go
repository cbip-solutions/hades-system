// go:build cgo
//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

package store

import (
	"context"
	"fmt"
)

type NodeDistance struct {
	NodeID   string
	Distance float64
}

// KNNNodeIDs runs a k-nearest-neighbour search over code_node_vec and
// returns the matching node_ids with their distances, ascending by
// distance. The query vector MUST be 1536-d (vecDimensions); a mismatched
// dimension is rejected (a mis-sized query silently corrupts distances).
// k<=0 is a no-op empty result. An empty index returns ([]NodeDistance{},
// nil), not an error.
//
// This is the read counterpart to UpsertNodeVector: it owns the vec0
// wire-format (float32SliceBytes) + the rowid→graph_nodes join so the
// vec0/rowid contract stays inside the store boundary (invariant — the
// intent package never re-encodes vectors or joins rowids itself).
// semantic retrieval calls this, then BGE-reranks the node text.
func (s *Store) KNNNodeIDs(ctx context.Context, embedding []float32, k int) ([]NodeDistance, error) {
	if len(embedding) != vecDimensions {
		return nil, fmt.Errorf("caronte/store: KNNNodeIDs: query dim %d != %d", len(embedding), vecDimensions)
	}
	if k <= 0 {
		return []NodeDistance{}, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT gn.node_id, cv.distance
		FROM code_node_vec cv
		JOIN graph_nodes gn ON gn.rowid = cv.rowid
		WHERE cv.embedding MATCH ? AND k = ?
		ORDER BY cv.distance`,
		float32SliceBytes(embedding), k,
	)
	if err != nil {
		return nil, fmt.Errorf("caronte/store: KNNNodeIDs query: %w", err)
	}
	defer rows.Close()
	out := []NodeDistance{}
	for rows.Next() {
		var nd NodeDistance
		if err := rows.Scan(&nd.NodeID, &nd.Distance); err != nil {
			return nil, fmt.Errorf("caronte/store: KNNNodeIDs scan: %w", err)
		}
		out = append(out, nd)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("caronte/store: KNNNodeIDs rows: %w", err)
	}
	return out, nil
}
