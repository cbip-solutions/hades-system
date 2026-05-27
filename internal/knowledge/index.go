// SPDX-License-Identifier: MIT
package knowledge

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	_ "github.com/ncruces/go-sqlite3/driver"
)

const (
	schemaCreateFTS = `
		CREATE VIRTUAL TABLE IF NOT EXISTS knowledge_fts
		USING fts5(content_text);
	`

	schemaCreateMeta = `
		CREATE TABLE IF NOT EXISTS knowledge_meta (
			rowid                INTEGER PRIMARY KEY,
			file_path            TEXT    NOT NULL UNIQUE,
			project_id           TEXT,
			project_alias        TEXT,
			file_type            TEXT    NOT NULL CHECK (file_type IN ('memory','research','adr','spec','plan','handoff')),
			title                TEXT,
			frontmatter_json     TEXT,
			last_modified        INTEGER NOT NULL,
			last_indexed         INTEGER NOT NULL,
			audit_chain_anchor   TEXT,
			ecosystem_join_keys  TEXT,
			caronte_symbol_refs TEXT
		);
	`

	schemaCreateProjectIndex = `
		CREATE INDEX IF NOT EXISTS idx_knowledge_meta_project
		ON knowledge_meta (project_id, file_type, last_modified DESC);
	`

	schemaCreateFilePathIndex = `
		CREATE INDEX IF NOT EXISTS idx_knowledge_meta_file_path
		ON knowledge_meta (file_path);
	`
)

func Open(ctx context.Context, dbPath string) (*sql.DB, error) {
	if dbPath == "" {
		return nil, errors.New("knowledge: dbPath required")
	}
	parent := filepath.Dir(dbPath)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return nil, fmt.Errorf("knowledge: mkdir parent %q: %w", parent, err)
	}
	dsn := buildDSN(dbPath)

	db, err := sql.Open("sqlite3_ncruces", dsn)
	if err != nil {
		return nil, fmt.Errorf("knowledge: open %q: %w", dbPath, err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("knowledge: ping %q: %w", dbPath, err)
	}

	if err := knowledgeFTS5SchemaSentinel(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("knowledge: schema sentinel: %w", err)
	}
	return db, nil
}

func buildDSN(dbPath string) string {
	q := url.Values{}

	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "foreign_keys(ON)")
	return "file:" + dbPath + "?" + q.Encode()
}

// Init creates the FTS5 virtual table + knowledge_meta supplementary
// metadata table + supporting indexes if they do not already exist.
// Idempotent — safe to call repeatedly (uses CREATE... IF NOT EXISTS
// throughout) and preserves any existing data across re-Init.
//
// Per spec §1 Q17 D + invariant: the three extension-hook columns
// (audit_chain_anchor, ecosystem_join_keys, caronte_symbol_refs)
// declared here ship NULL by default in release; INSERT statements
// MUST NOT populate them (G-16 compliance test enforces).
func Init(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return errors.New("knowledge: Init: db is nil")
	}
	for _, ddl := range []string{
		schemaCreateFTS,
		schemaCreateMeta,
		schemaCreateProjectIndex,
		schemaCreateFilePathIndex,
	} {
		if _, err := db.ExecContext(ctx, ddl); err != nil {
			return fmt.Errorf("knowledge: schema apply: %w", err)
		}
	}
	return nil
}

// indexInsertSQL is the canonical INSERT statement for knowledge_meta.
// invariant enforcement site (compile-time-visible): the column list
// MUST NOT include audit_chain_anchor, ecosystem_join_keys, or
// caronte_symbol_refs. SQLite defaults the absent columns to NULL
// automatically, which is the contract release / release / Caronte
// rely on (they fill those columns at index time without retrofit migrations).
//
// The column rowid is bound explicitly as NULL so SQLite assigns the
// next autoincrement value; we then read it via LastInsertId for the
// FTS5 join below.
//
// Two grep sites cover this constant:
// - in-package: TestIndexInsertSQLDoesNotMentionExtensionHookColumns
// - tree-wide compliance: tests/compliance/inv_hades_130_*_test.go (G-16)
const indexInsertSQL = `
INSERT INTO knowledge_meta (
    rowid,
    file_path,
    project_id,
    project_alias,
    file_type,
    title,
    frontmatter_json,
    last_modified,
    last_indexed
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);
`

const indexInsertFTSSQL = `
INSERT INTO knowledge_fts (rowid, content_text) VALUES (?, ?);
`

const (
	indexDeleteMetaByPathSQL  = `DELETE FROM knowledge_meta WHERE file_path = ?;`
	indexDeleteFTSByRowidSQL  = `DELETE FROM knowledge_fts  WHERE rowid     = ?;`
	indexLookupRowidByPathSQL = `SELECT rowid FROM knowledge_meta WHERE file_path = ?;`
)

// IndexDoc inserts or replaces a Doc in the knowledge index. Idempotent:
// if a row with the same file_path already exists, it is deleted (with
// its FTS5 counterpart) before the new row is inserted. Wrapped in a
// single explicit transaction so the operation is atomic — a re-index
// either fully succeeds or fully rolls back, never half-applied.
//
// Per invariant: the canonical INSERT (indexInsertSQL) does NOT list
// the three extension-hook columns. Doc fields AuditChainAnchor /
// EcosystemJoinKeys / CaronteSymbolRefs are IGNORED here even if
// Valid=true — the data flows / release / Caronte
// writers, NOT from this code path. The runtime check
// TestIndexExtensionHookColumnsNullByDefault asserts the row's columns
// are NULL post-INSERT regardless of Doc field values; the compile-time
// check TestIndexInsertSQLDoesNotMentionExtensionHookColumns asserts
// the SQL string itself never references those columns.
//
// Goroutine-safe: SQLite WAL mode (set in buildDSN) handles
// serialization at the storage layer, so multiple watcher goroutines
// (G-7+) may call IndexDoc concurrently. The transaction boundary keeps
// each (DELETE, DELETE, INSERT, INSERT) sequence atomic per call.
//
// Validation
// - FilePath MUST be non-empty (defensive guard before BeginTx).
// - FileType MUST be non-empty (defensive guard; schema CHECK would
// also reject, but a Go-side error is more actionable).
//
// Failure modes wrap the underlying driver error via %w; callers may
// errors.Is/errors.As against sql sentinels when needed.
//
// Renamed from `Index` → `IndexDoc` in G-17 to free the `Index`
// identifier for the method-bound façade type (see facade.go). The
// free-function form is preserved for direct callers (Reindex hot path,
// compliance test); the façade `(*Index).Reindex` ultimately delegates
// here via the ColdRebuild + IncrementalUpdate free functions.
func IndexDoc(ctx context.Context, db *sql.DB, doc Doc) error {
	if doc.FilePath == "" {
		return errors.New("knowledge: IndexDoc requires non-empty FilePath")
	}
	if doc.FileType == "" {
		return errors.New("knowledge: IndexDoc requires non-empty FileType")
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("knowledge: begin tx: %w", err)
	}

	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	var existingRowid sql.NullInt64
	row := tx.QueryRowContext(ctx, indexLookupRowidByPathSQL, doc.FilePath)
	if err := row.Scan(&existingRowid); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("knowledge: lookup rowid: %w", err)
	}

	if existingRowid.Valid {
		if _, err := tx.ExecContext(ctx, indexDeleteFTSByRowidSQL, existingRowid.Int64); err != nil {
			return fmt.Errorf("knowledge: delete fts: %w", err)
		}
		if _, err := tx.ExecContext(ctx, indexDeleteMetaByPathSQL, doc.FilePath); err != nil {
			return fmt.Errorf("knowledge: delete meta: %w", err)
		}
	}

	res, err := tx.ExecContext(ctx, indexInsertSQL,
		nil,
		doc.FilePath,
		nullStringFromString(doc.ProjectID),
		nullStringFromString(doc.ProjectAlias),
		string(doc.FileType),
		nullStringFromString(doc.Title),
		nullBytesFromJSON(doc.FrontmatterJSON),
		doc.LastModified.UnixNano(),
		doc.LastIndexed.UnixNano(),
	)
	if err != nil {
		return fmt.Errorf("knowledge: insert meta: %w", err)
	}
	rowid, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("knowledge: last insert id: %w", err)
	}

	if _, err := tx.ExecContext(ctx, indexInsertFTSSQL, rowid, doc.ContentText); err != nil {
		return fmt.Errorf("knowledge: insert fts: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("knowledge: commit: %w", err)
	}
	committed = true
	return nil
}

func Delete(ctx context.Context, db *sql.DB, filePath string) error {
	if filePath == "" {
		return errors.New("knowledge: Delete requires non-empty filePath")
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("knowledge: begin tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	var rowid sql.NullInt64
	row := tx.QueryRowContext(ctx, indexLookupRowidByPathSQL, filePath)
	if err := row.Scan(&rowid); err != nil {
		if errors.Is(err, sql.ErrNoRows) {

			if err := tx.Commit(); err != nil {
				return fmt.Errorf("knowledge: commit: %w", err)
			}
			committed = true
			return nil
		}
		return fmt.Errorf("knowledge: lookup rowid: %w", err)
	}

	if rowid.Valid {
		if _, err := tx.ExecContext(ctx, indexDeleteFTSByRowidSQL, rowid.Int64); err != nil {
			return fmt.Errorf("knowledge: delete fts: %w", err)
		}
		if _, err := tx.ExecContext(ctx, indexDeleteMetaByPathSQL, filePath); err != nil {
			return fmt.Errorf("knowledge: delete meta: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("knowledge: commit: %w", err)
	}
	committed = true
	return nil
}

func nullStringFromString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func nullBytesFromJSON(j []byte) any {
	if len(j) == 0 {
		return nil
	}
	return string(j)
}
