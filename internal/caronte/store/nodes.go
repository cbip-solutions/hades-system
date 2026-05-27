// go:build cgo
//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

func (s *Store) UpsertNode(ctx context.Context, n Node) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("caronte/store: UpsertNode begin: %w", err)
	}
	defer tx.Rollback()

	var existed bool
	var priorRowid int64
	err = tx.QueryRowContext(ctx, `SELECT rowid FROM graph_nodes WHERE node_id = ?`, n.NodeID).Scan(&priorRowid)
	switch {
	case err == nil:
		existed = true
	case errors.Is(err, sql.ErrNoRows):
		existed = false
	default:
		return fmt.Errorf("caronte/store: UpsertNode probe: %w", err)
	}

	if existed {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM graph_nodes_fts WHERE rowid = ?`, priorRowid,
		); err != nil {
			return fmt.Errorf("caronte/store: UpsertNode fts delete: %w", err)
		}
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO graph_nodes
			(node_id, name, kind, language, file_path, start_line, end_line,
			 signature, doc, coreness, scc_id, package_id, content_hash)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(node_id) DO UPDATE SET
			name = excluded.name,
			kind = excluded.kind,
			language = excluded.language,
			file_path = excluded.file_path,
			start_line = excluded.start_line,
			end_line = excluded.end_line,
			signature = excluded.signature,
			doc = excluded.doc,
			content_hash = excluded.content_hash`,
		n.NodeID, n.Name, n.Kind, n.Language, n.FilePath, n.StartLine, n.EndLine,
		n.Signature, n.Doc, n.Coreness, n.SCCID, n.PackageID, n.ContentHash,
	)
	if err != nil {
		return fmt.Errorf("caronte/store: UpsertNode insert: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO graph_nodes_fts (rowid, name, signature, doc)
		SELECT rowid, name, signature, doc FROM graph_nodes WHERE node_id = ?`,
		n.NodeID,
	); err != nil {
		return fmt.Errorf("caronte/store: UpsertNode fts insert: %w", err)
	}
	return tx.Commit()
}

// UpsertNodeVector stores (or replaces) the Jina-code embedding for an
// existing node in code_node_vec, rowid-aligned to graph_nodes. The node
// MUST already exist (UpsertNode first) — the rowid is resolved from
// graph_nodes. vec0 rejects duplicate-rowid INSERT, so re-store is
// DELETE-by-rowid then INSERT (mirrors aggregator). Rejects a non-1536-d
// vector to protect KNN correctness.
//
// Pre nodeID names a row in graph_nodes; embedding is 1536-dimensional.
// Post code_node_vec holds exactly one row for this rowid.
func (s *Store) UpsertNodeVector(ctx context.Context, nodeID string, embedding []float32) error {
	if len(embedding) != vecDimensions {
		return fmt.Errorf("caronte/store: UpsertNodeVector: embedding dim %d != %d", len(embedding), vecDimensions)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("caronte/store: UpsertNodeVector begin: %w", err)
	}
	defer tx.Rollback()

	var rowid int64
	err = tx.QueryRowContext(ctx, `SELECT rowid FROM graph_nodes WHERE node_id = ?`, nodeID).Scan(&rowid)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("caronte/store: UpsertNodeVector: %w (node %q)", ErrNotFound, nodeID)
	}
	if err != nil {
		return fmt.Errorf("caronte/store: UpsertNodeVector resolve rowid: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM code_node_vec WHERE rowid = ?`, rowid); err != nil {
		return fmt.Errorf("caronte/store: UpsertNodeVector delete: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO code_node_vec (rowid, embedding) VALUES (?, ?)`, rowid, float32SliceBytes(embedding),
	); err != nil {
		return fmt.Errorf("caronte/store: UpsertNodeVector insert: %w", err)
	}
	return tx.Commit()
}

func (s *Store) GetNode(ctx context.Context, nodeID string) (Node, error) {
	var n Node
	err := s.db.QueryRowContext(ctx, `
		SELECT node_id, name, kind, language, file_path, start_line, end_line,
		       signature, doc, coreness, scc_id, package_id, content_hash
		FROM graph_nodes WHERE node_id = ?`, nodeID,
	).Scan(
		&n.NodeID, &n.Name, &n.Kind, &n.Language, &n.FilePath, &n.StartLine, &n.EndLine,
		&n.Signature, &n.Doc, &n.Coreness, &n.SCCID, &n.PackageID, &n.ContentHash,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Node{}, fmt.Errorf("caronte/store: GetNode %q: %w", nodeID, ErrNotFound)
	}
	if err != nil {
		return Node{}, fmt.Errorf("caronte/store: GetNode %q: %w", nodeID, err)
	}
	return n, nil
}

func (s *Store) ListNodesByKind(ctx context.Context, kind NodeKind) ([]Node, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT node_id, name, kind, language, file_path, start_line, end_line,
		       signature, doc, coreness, scc_id, package_id, content_hash
		FROM graph_nodes WHERE kind = ? ORDER BY node_id ASC`, string(kind),
	)
	if err != nil {
		return nil, fmt.Errorf("caronte/store: ListNodesByKind %q: %w", kind, err)
	}
	defer rows.Close()
	out := []Node{}
	for rows.Next() {
		var n Node
		if err := rows.Scan(
			&n.NodeID, &n.Name, &n.Kind, &n.Language, &n.FilePath, &n.StartLine, &n.EndLine,
			&n.Signature, &n.Doc, &n.Coreness, &n.SCCID, &n.PackageID, &n.ContentHash,
		); err != nil {
			return nil, fmt.Errorf("caronte/store: ListNodesByKind scan: %w", err)
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("caronte/store: ListNodesByKind rows: %w", err)
	}
	return out, nil
}

func (s *Store) UpdateNodeStructure(ctx context.Context, nodeID string, coreness, sccID int, packageID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE graph_nodes SET coreness = ?, scc_id = ?, package_id = ? WHERE node_id = ?`,
		coreness, sccID, packageID, nodeID,
	)
	if err != nil {
		return fmt.Errorf("caronte/store: UpdateNodeStructure %q: %w", nodeID, err)
	}
	return nil
}

func (s *Store) ContentHashFor(ctx context.Context, nodeID string) (string, error) {
	var h string
	err := s.db.QueryRowContext(ctx, `SELECT content_hash FROM graph_nodes WHERE node_id = ?`, nodeID).Scan(&h)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("caronte/store: ContentHashFor %q: %w", nodeID, ErrNotFound)
	}
	if err != nil {
		return "", fmt.Errorf("caronte/store: ContentHashFor %q: %w", nodeID, err)
	}
	return h, nil
}
