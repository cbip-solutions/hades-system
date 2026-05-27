//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

package aggregator

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"
)

const DefaultDriver = "sqlite3"

var ErrCGODisabled = errors.New("aggregator: sqlite-vec requires CGO_ENABLED=1; degraded_mode active")

func LoadVecExtension() error {

	sqlite_vec.Auto()
	return nil
}

const vecDimensions = 384

// Open opens (or creates) the aggregator.db at dbPath using
// mattn/go-sqlite3 with the production-grade PRAGMA set encoded in the
// DSN (mattn driver). The caller MUST call Init after Open before
// running queries — Init both verifies the schema is materialised AND
// asserts the sqlite-vec auto-extension fired on the per-connection
// init pipeline.
//
// Pre-conditions:
// - dbPath is a non-empty absolute or repo-relative path. Empty path
// is rejected up-front; defaulting to ":memory:" or "$CWD/<x>"
// would silently mask configuration errors.
// - Process linked with CGO_ENABLED=1 (else the !cgo Open does not
// compile and the daemon fails at boot — that is the canonical
// degraded-mode entry point per Failure mode #8).
//
// Post-conditions:
// - Parent directory exists (mkdir-all 0o700 — operator-only). The
// 0o700 mode is load-bearing: aggregator.db lives in a global path
// under the operator's home and contains cross-project content;
// 0o755 would expose it to other macOS / Linux users on a shared
// host (rare but real).
// - SQLite connection pool is sized for SINGLE-WRITER WAL discipline
// (MaxOpenConns=1, MaxIdleConns=1). Two writers + WAL is a known
// SQLite footgun (busy-loop unless busy_timeout > inter-writer
// gap). The single-writer model matches release's auditadapter
// posture; aggregator inherits it for consistency + simplicity.
// - Ping verifies the file is openable AND the DSN PRAGMAs were
// accepted (an invalid PRAGMA name produces a Ping error with the
// mattn driver).
func Open(ctx context.Context, dbPath string) (*sql.DB, error) {
	if dbPath == "" {
		return nil, errors.New("aggregator: empty dbPath")
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		return nil, fmt.Errorf("aggregator: mkdir parent: %w", err)
	}

	if err := LoadVecExtension(); err != nil {
		return nil, fmt.Errorf("aggregator: %w", err)
	}
	// DSN-encoded PRAGMAs (mattn driver convention): each supported
	// `_<pragma>=` fires on connection-open before the first user
	// query. We do NOT add `_load_extension=1` because the auto-extension
	// path above already registers sqlite-vec at the C level — no
	// per-connection DSN flag is required.
	//
	// NOTE(release) on temp_store: mattn 1.14.44 does NOT support `_temp_store=`
	// as a DSN key (verified by reading sqlite3.go source 2026-05-09).
	// We apply it via a manual PRAGMA exec in Init below — the value
	// then persists for the lifetime of every connection in the pool
	// because the pool is single-conn (MaxOpenConns=1).
	dsn := fmt.Sprintf(
		"%s?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=1&_synchronous=NORMAL",
		dbPath,
	)
	db, err := sql.Open(DefaultDriver, dsn)
	if err != nil {
		return nil, fmt.Errorf("aggregator: sql.Open: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("aggregator: ping: %w", err)
	}
	return db, nil
}

func Init(ctx context.Context, db *sql.DB) error {

	if err := LoadVecExtension(); err != nil {
		return fmt.Errorf("aggregator: %w", err)
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("aggregator: acquire conn: %w", err)
	}
	defer conn.Close()

	pragmaStmts := []string{
		`PRAGMA temp_store = MEMORY`,
	}
	for i, p := range pragmaStmts {
		if _, err := conn.ExecContext(ctx, p); err != nil {
			return fmt.Errorf("aggregator: pragma[%d] %q: %w", i, p, err)
		}
	}
	stmts := []string{

		`CREATE TABLE IF NOT EXISTS knowledge_pin_index (
			note_id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			title TEXT NOT NULL,
			content TEXT NOT NULL,
			frontmatter_json TEXT NOT NULL,
			promoted_at DATETIME NOT NULL,
			promoted_by TEXT NOT NULL,
			promote_reason TEXT NOT NULL CHECK (length(promote_reason) > 0),
			audit_chain_anchor TEXT NOT NULL,
			embedding BLOB
		)`,

		`CREATE VIRTUAL TABLE IF NOT EXISTS knowledge_pin_fts USING fts5(
			content, title,
			content='knowledge_pin_index',
			content_rowid='rowid'
		)`,

		fmt.Sprintf(
			`CREATE VIRTUAL TABLE IF NOT EXISTS knowledge_pin_vec USING vec0(embedding float[%d])`,
			vecDimensions,
		),

		`CREATE TABLE IF NOT EXISTS knowledge_pin_wikilinks (
			source_note_id TEXT NOT NULL,
			target_note_id TEXT NOT NULL,
			link_type TEXT NOT NULL CHECK (link_type IN ('wikilink','backlink','relates')),
			PRIMARY KEY (source_note_id, target_note_id, link_type)
		)`,

		`CREATE INDEX IF NOT EXISTS idx_pin_wikilinks_target ON knowledge_pin_wikilinks(target_note_id)`,

		`CREATE INDEX IF NOT EXISTS idx_pin_index_project ON knowledge_pin_index(project_id, promoted_at DESC)`,

		`CREATE INDEX IF NOT EXISTS idx_pin_index_anchor ON knowledge_pin_index(audit_chain_anchor)`,
	}
	for i, stmt := range stmts {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("aggregator: stmt[%d]: %w", i, err)
		}
	}
	return nil
}
