// go:build cgo
//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

package store

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"
)

type Store struct {
	db *sql.DB
}

// DB returns the wrapped handle. Exposed for the engine's degraded-mode
// health probe and for tests; callers MUST NOT Close it (caronteadapter
// owns the lifecycle).
func (s *Store) DB() *sql.DB { return s.db }

func caronteBoundarySentinel() error { return nil }

func Open(ctx context.Context, db *sql.DB) (*Store, error) {
	if err := caronteBoundarySentinel(); err != nil {
		return nil, err
	}
	if db == nil {
		return nil, ErrEmptyDB
	}

	sqlite_vec.Auto()
	s := &Store{db: db}
	if err := s.Init(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Init(ctx context.Context) error {
	if s.db == nil {
		return ErrEmptyDB
	}
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("caronte/store: acquire conn: %w", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, `PRAGMA temp_store = MEMORY`); err != nil {
		return fmt.Errorf("caronte/store: pragma temp_store: %w", err)
	}
	for i, stmt := range schemaStatements() {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("caronte/store: ddl[%d]: %w", i, err)
		}
	}
	return nil
}

func (s *Store) GetNodeByPosition(ctx context.Context, filePath string, startLine int) (string, bool, error) {
	const q = `SELECT node_id FROM graph_nodes WHERE file_path = ? AND start_line = ? ORDER BY node_id LIMIT 1`
	var nodeID string
	switch err := s.db.QueryRowContext(ctx, q, filePath, startLine).Scan(&nodeID); err {
	case nil:
		return nodeID, true, nil
	case sql.ErrNoRows:
		return "", false, nil
	default:
		return "", false, fmt.Errorf("caronte/store: GetNodeByPosition(%s:%d): %w", filePath, startLine, err)
	}
}

func float32SliceBytes(v []float32) []byte {
	if len(v) == 0 {
		return nil
	}
	buf := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[4*i:4*(i+1)], math.Float32bits(f))
	}
	return buf
}

func (s *Store) DeleteNodesByFile(ctx context.Context, filePath string) (int, error) {
	if s.db == nil {
		return 0, ErrEmptyDB
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("caronte/store: DeleteNodesByFile begin: %w", err)
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx,
		`SELECT rowid, node_id FROM graph_nodes WHERE file_path = ?`, filePath)
	if err != nil {
		return 0, fmt.Errorf("caronte/store: DeleteNodesByFile select: %w", err)
	}
	var rowids []int64
	var nodeIDs []string
	for rows.Next() {
		var rid int64
		var nid string
		if err := rows.Scan(&rid, &nid); err != nil {
			_ = rows.Close()
			return 0, fmt.Errorf("caronte/store: DeleteNodesByFile scan: %w", err)
		}
		rowids = append(rowids, rid)
		nodeIDs = append(nodeIDs, nid)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, fmt.Errorf("caronte/store: DeleteNodesByFile rows: %w", err)
	}
	_ = rows.Close()

	if len(rowids) == 0 {

		return 0, tx.Commit()
	}

	for _, rid := range rowids {
		if _, err := tx.ExecContext(ctx, `DELETE FROM graph_nodes_fts WHERE rowid = ?`, rid); err != nil {
			return 0, fmt.Errorf("caronte/store: DeleteNodesByFile fts delete: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM code_node_vec WHERE rowid = ?`, rid); err != nil {
			return 0, fmt.Errorf("caronte/store: DeleteNodesByFile vec delete: %w", err)
		}
	}

	for _, nid := range nodeIDs {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM graph_edges WHERE source_id = ? OR target_id = ?`, nid, nid,
		); err != nil {
			return 0, fmt.Errorf("caronte/store: DeleteNodesByFile edge delete: %w", err)
		}
	}

	res, err := tx.ExecContext(ctx, `DELETE FROM graph_nodes WHERE file_path = ?`, filePath)
	if err != nil {
		return 0, fmt.Errorf("caronte/store: DeleteNodesByFile node delete: %w", err)
	}
	deleted, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("caronte/store: DeleteNodesByFile rows-affected: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("caronte/store: DeleteNodesByFile commit: %w", err)
	}
	return int(deleted), nil
}
