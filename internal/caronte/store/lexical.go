//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

package store

import (
	"context"
	"fmt"
)

type NodeBM25 struct {
	NodeID string
	Score  float64
}

func (s *Store) LexicalSearchNodeIDs(ctx context.Context, query string, k int) ([]NodeBM25, error) {
	if k <= 0 {
		return []NodeBM25{}, nil
	}
	if query == "" {

		return []NodeBM25{}, nil
	}

	quoted := `"` + escapeFTS5(query) + `"`
	rows, err := s.db.QueryContext(ctx, `
		SELECT gn.node_id, bm25(graph_nodes_fts) AS rank
		FROM graph_nodes_fts
		JOIN graph_nodes gn ON gn.rowid = graph_nodes_fts.rowid
		WHERE graph_nodes_fts MATCH ?
		ORDER BY rank
		LIMIT ?`,
		quoted, k,
	)
	if err != nil {
		return nil, fmt.Errorf("caronte/store: LexicalSearchNodeIDs query: %w", err)
	}
	defer rows.Close()

	out := []NodeBM25{}
	for rows.Next() {
		var (
			id   string
			rank float64
		)
		if err := rows.Scan(&id, &rank); err != nil {
			return nil, fmt.Errorf("caronte/store: LexicalSearchNodeIDs scan: %w", err)
		}

		score := 1.0 / (1.0 + absFloat(rank))
		out = append(out, NodeBM25{NodeID: id, Score: score})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("caronte/store: LexicalSearchNodeIDs rows: %w", err)
	}
	return out, nil
}

func escapeFTS5(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '"' {
			out = append(out, '"', '"')
			continue
		}
		out = append(out, c)
	}
	return string(out)
}

func absFloat(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
