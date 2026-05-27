// go:build cgo
//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

package ecosystem

import (
	"database/sql"
	"embed"
	"fmt"
	"sort"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
)

const SchemaVersion = 2

// migrationsFS embeds the.sql files under migrations/ (8 at,
// 9 at G-5 after 009_ecosystem_versions_indefinite_retain).
// The embed directive is relative to this source file's directory;
// go:embed + SQL files together compile into the binary at build time
// so there is no runtime filesystem dependency.
//
// go:embed migrations/*.sql
var migrationsFS embed.FS

// ApplyMigrations applies all embedded migration.sql files to db in
// numerical order. Idempotent: re-applying on an already-migrated DB
// is a no-op (the ecosystem_schema_meta single-row meta table tracks
// the current version and ApplyMigrations short-circuits when
// currentVersion >= SchemaVersion).
//
// Pre-conditions:
// - db is non-nil + opened. Foreign-key enforcement (PRAGMA
// foreign_keys = ON) is RECOMMENDED — the schema declares FK
// constraints (ecosystem_chunks.package_id REFERENCES
// ecosystem_packages(id), etc.) but SQLite defaults FK enforcement
// OFF; callers MUST enable it in the DSN (_foreign_keys=on) or via
// PRAGMA foreign_keys = ON. Tests (openTestDB) demonstrate the
// DSN pattern.
// - Process linked with CGO_ENABLED=1 (this file is build-tagged
// cgo because sqlite-vec is a CGO bridge). The no-cgo build
// omits this file entirely; callers in that build must not invoke
// this package.
//
// Post-conditions on nil error:
// - All 9 production tables/virtual-tables exist (ecosystem_packages,
// ecosystem_versions, ecosystem_chunks, ecosystem_chunks_fp32,
// ecosystem_symbols, ecosystem_changes, ecosystem_chunks_fts,
// ecosystem_chunks_vec_bin, ecosystem_audit_chain).
// - All 5 spec-named indexes exist (idx_chunks_pkg_version,
// idx_chunks_symbol_path, idx_chunks_fingerprint, idx_symbols_path,
// idx_changes_versions).
// - ecosystem_schema_meta single-row meta table contains version =
// SchemaVersion.
// - sqlite-vec auto-extension is registered (idempotently) for any
// subsequent connection from any *sql.DB in this process.
//
// Failure modes:
// - nil db → returns error (no panic).
// - schema_meta create failure → returned wrapped.
// - read of current version failure → returned wrapped.
// - embed.FS ReadDir failure (should never happen with successful
// compile) → returned wrapped.
// - per-file db.Exec failure → returned wrapped with the offending
// file name (e.g., "apply \"007_ecosystem_chunks_fts.sql\": no
// such module: vec0" when sqlite-vec is unavailable at link time).
//
// ApplyMigrations does NOT wrap the work in a single TRANSACTION
// because CREATE VIRTUAL TABLE (FTS5 + vec0) is implicitly outside a
// transaction in SQLite (it modifies the schema in ways that interact
// with the schema cookie at commit time, and some virtual-table
// modules refuse to run inside an explicit BEGIN). The idempotent
// `IF NOT EXISTS` guards make re-application safe even on partial
// prior-run failure: subsequent ApplyMigrations re-runs the same DDL
// and the version cookie is only bumped at the end.
func ApplyMigrations(db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("research/ecosystem: ApplyMigrations: db is nil")
	}

	sqlite_vec.Auto()

	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS ecosystem_schema_meta (
			id         INTEGER PRIMARY KEY CHECK (id = 1),
			version    INTEGER NOT NULL,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
	`); err != nil {
		return fmt.Errorf("research/ecosystem: schema_meta create: %w", err)
	}
	if _, err := db.Exec(
		`INSERT OR IGNORE INTO ecosystem_schema_meta (id, version) VALUES (1, 0)`,
	); err != nil {
		return fmt.Errorf("research/ecosystem: schema_meta seed: %w", err)
	}

	var currentVersion int
	if err := db.QueryRow(
		"SELECT version FROM ecosystem_schema_meta WHERE id = 1",
	).Scan(&currentVersion); err != nil {
		return fmt.Errorf("research/ecosystem: read current version: %w", err)
	}

	if currentVersion >= SchemaVersion {

		return nil
	}

	files, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("research/ecosystem: read migrations dir: %w", err)
	}

	names := make([]string, 0, len(files))
	for _, f := range files {
		if !f.IsDir() {
			names = append(names, f.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		sqlBytes, rerr := migrationsFS.ReadFile("migrations/" + name)
		if rerr != nil {
			return fmt.Errorf("research/ecosystem: read %q: %w", name, rerr)
		}
		if _, eerr := db.Exec(string(sqlBytes)); eerr != nil {
			return fmt.Errorf("research/ecosystem: apply %q: %w", name, eerr)
		}
	}

	if _, err := db.Exec(
		`UPDATE ecosystem_schema_meta SET version = ?, updated_at = CURRENT_TIMESTAMP WHERE id = 1`,
		SchemaVersion,
	); err != nil {
		return fmt.Errorf("research/ecosystem: update schema_meta: %w", err)
	}

	return nil
}
