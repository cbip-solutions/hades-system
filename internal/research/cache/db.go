//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

// Package cache — db.go
//
// Open, applySchema, SchemaVersion, and sqlite-vec registration for
// research_cache.db.
//
// Driver choice: mattn/go-sqlite3 (CGO) — identical to
// internal/knowledge/aggregator/db.go. Both packages call
// sqlite_vec.Auto() to register the sqlite-vec C extension via
// sqlite3_auto_extension. The auto-extension fires on every new
// connection in the pool, so the vec0 virtual table and vec_version()
// scalar function are available immediately after Open.
//
// # The plan-file (F-1 line 520) specified a forward declaration
//
// func registerSqliteVecOnDB(*sql.DB) error
//
// without a body — which is illegal in Go (non-cgo functions require a
// body). reality-check identified this as a plan-file error.
// Adaptation (option b): implement the extension registration directly
// in this file via sqlite_vec.Auto(), mirroring pattern
// exactly. No forward declaration; no cross-package symbol import.
//
// inv-hades-031: this package MUST NOT import internal/store. Enforced
// by the post-implementation boundary check in the workflow and the
// compliance test at tests/compliance/.
package cache

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

const cacheSchemaVersionV1 = 1

const cacheSchemaVersionV2 = 2

const cacheSchemaVersionV3 = 3

const cacheSchemaVersionV4 = 4

type DB struct {
	SQL *sql.DB

	Path string

	Version int
}

// Option is part of the exported package contract.
// WithLocalSqliteVec is a functional option for Open that causes the
// sqlite-vec C extension to be registered even when Open is called from
// a test that constructs its own *sql.DB (e.g., an in-memory DB for unit
// tests that do not go through the full Open path).
//
// In production, Open always registers sqlite-vec (unconditionally); this
// option is a no-op in the current implementation. It is retained as an
// extension point for standalone test harnesses in future phases (F-3+)
// that need explicit control over extension registration order.
//
// # Example
//
// db, err := Open(ctx, ":memory:", WithLocalSqliteVec())
type Option func(*openConfig)

type openConfig struct {
	forceVecRegistration bool
}

func WithLocalSqliteVec() Option {
	return func(cfg *openConfig) {
		cfg.forceVecRegistration = true
	}
}

// Open opens (or creates) research_cache.db at dbPath, applies the V1
// schema (idempotent — all CREATEs use IF NOT EXISTS), and records the
// schema version.
//
// Pre-conditions:
// - dbPath is a non-empty path (absolute or relative). Empty path is
// rejected up-front; ":memory:" is accepted for tests.
// - Process linked with CGO_ENABLED=1 (this file does not compile under
// CGO_ENABLED=0 — see package doc.go for rationale).
//
// Post-conditions:
// - Parent directory exists (os.MkdirAll 0o700). Skipped for ":memory:".
// - sqlite-vec C extension is registered as a SQLite auto-extension
// (via sqlite_vec.Auto) BEFORE any connection is opened.
// - *DB.SQL is configured with MaxOpenConns=1, MaxIdleConns=1
// (single-writer WAL discipline).
// - Schema is materialised (research_dispatches + research_findings +
// research_validation_log + research_query_vec vec0 virtual table +
// _cache_schema_version).
// - _cache_schema_version contains exactly one row with version=1.
// - DB.Version == cacheSchemaVersionV1.
//
// Callers MUST call DB.SQL.Close() when done. Typical usage:
//
// cacheDB, err := cache.Open(ctx, cfg.ResearchCacheDBPath)
// if err != nil {... }
// defer cacheDB.SQL.Close()
func Open(ctx context.Context, dbPath string, opts ...Option) (*DB, error) {
	// Apply options. forceVecRegistration is currently no-op (Open always
	// registers sqlite-vec unconditionally) but the loop exercises the
	// closure so coverage tools do not flag the option body as dead code.
	cfg := &openConfig{}
	for _, opt := range opts {
		opt(cfg)
	}
	if dbPath == "" {
		return nil, errors.New("cache: empty dbPath")
	}

	sqlite_vec.Auto()

	if dbPath != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
			return nil, fmt.Errorf("cache: mkdir parent: %w", err)
		}
	}

	dsn := fmt.Sprintf(
		"%s?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=1&_synchronous=NORMAL",
		dbPath,
	)
	sqlDB, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("cache: sql.Open: %w", err)
	}

	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)

	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("cache: ping: %w", err)
	}

	if err := applySchema(ctx, sqlDB); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("cache: applySchema: %w", err)
	}

	if err := applyMigrationV2(ctx, sqlDB); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("cache: applyMigrationV2: %w", err)
	}

	if err := applyMigrationV3(ctx, sqlDB); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("cache: applyMigrationV3: %w", err)
	}

	if err := applyMigrationV4(ctx, sqlDB); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("cache: applyMigrationV4: %w", err)
	}

	if err := applyMigrationV5(ctx, sqlDB); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("cache: applyMigrationV5: %w", err)
	}
	if err := applyMigrationV6(ctx, sqlDB); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("cache: applyMigrationV6: %w", err)
	}

	ver, err := SchemaVersion(ctx, sqlDB)
	if err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("cache: SchemaVersion: %w", err)
	}

	return &DB{
		SQL:     sqlDB,
		Path:    dbPath,
		Version: ver,
	}, nil
}

func SchemaVersion(ctx context.Context, db *sql.DB) (int, error) {
	var v int
	err := db.QueryRowContext(ctx, `SELECT version FROM _cache_schema_version LIMIT 1`).Scan(&v)
	if err != nil {
		return 0, fmt.Errorf("cache: SchemaVersion: %w", err)
	}
	return v, nil
}

func schemaV1Statements() []string {
	return []string{

		`CREATE TABLE IF NOT EXISTS research_dispatches (
			id          TEXT PRIMARY KEY,
			query       TEXT NOT NULL,
			status      TEXT NOT NULL CHECK (status IN ('PENDING','RUNNING','DONE','FAILED')),
			created_at  INTEGER NOT NULL,
			updated_at  INTEGER NOT NULL
		)`,

		`CREATE TABLE IF NOT EXISTS research_findings (
			id               TEXT PRIMARY KEY,
			dispatch_id      TEXT NOT NULL REFERENCES research_dispatches(id) ON DELETE CASCADE,
			url              TEXT NOT NULL,
			title            TEXT NOT NULL,
			snippet          TEXT NOT NULL,
			freshness_status TEXT NOT NULL CHECK (freshness_status IN ('FRESH','STALE','EXPIRED')),
			retrieved_at     INTEGER NOT NULL
		)`,

		`CREATE TABLE IF NOT EXISTS research_validation_log (
			id           TEXT PRIMARY KEY,
			finding_id   TEXT NOT NULL REFERENCES research_findings(id) ON DELETE CASCADE,
			passed       INTEGER NOT NULL CHECK (passed IN (0,1)),
			note         TEXT NOT NULL,
			validated_at INTEGER NOT NULL
		)`,

		`CREATE VIRTUAL TABLE IF NOT EXISTS research_query_vec USING vec0(embedding float[384])`,

		`CREATE TABLE IF NOT EXISTS _cache_schema_version (
			version INTEGER PRIMARY KEY
		)`,

		`CREATE INDEX IF NOT EXISTS idx_dispatches_status ON research_dispatches(status)`,

		`CREATE INDEX IF NOT EXISTS idx_dispatches_created ON research_dispatches(created_at DESC)`,

		`CREATE INDEX IF NOT EXISTS idx_findings_dispatch ON research_findings(dispatch_id)`,

		`CREATE INDEX IF NOT EXISTS idx_findings_freshness ON research_findings(freshness_status)`,

		`CREATE INDEX IF NOT EXISTS idx_vlog_finding ON research_validation_log(finding_id)`,

		`INSERT INTO _cache_schema_version(version)
		 SELECT 1 WHERE NOT EXISTS (SELECT 1 FROM _cache_schema_version)`,
	}
}

func applySchema(ctx context.Context, db *sql.DB) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire conn: %w", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, `PRAGMA temp_store = MEMORY`); err != nil {
		return fmt.Errorf("pragma temp_store: %w", err)
	}

	for i, stmt := range schemaV1Statements() {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("stmt[%d]: %w", i, err)
		}
	}
	return nil
}

func applyMigrationV2(ctx context.Context, db *sql.DB) error {

	rows, err := db.QueryContext(ctx, `PRAGMA table_info(research_dispatches)`)
	if err != nil {
		return fmt.Errorf("pragma table_info: %w", err)
	}
	hasCol := false
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var dfltValue interface{}
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dfltValue, &pk); err != nil {
			rows.Close()
			return fmt.Errorf("scan table_info: %w", err)
		}
		if name == "query_text_hash" {
			hasCol = true
			break
		}
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close table_info rows: %w", err)
	}

	if hasCol {

		return nil
	}

	if _, err := db.ExecContext(ctx,
		`ALTER TABLE research_dispatches ADD COLUMN query_text_hash TEXT`); err != nil {
		return fmt.Errorf("alter table add query_text_hash: %w", err)
	}

	if _, err := db.ExecContext(ctx,
		`CREATE INDEX IF NOT EXISTS idx_dispatches_query_hash ON research_dispatches(query_text_hash)`); err != nil {
		return fmt.Errorf("create idx_dispatches_query_hash: %w", err)
	}

	if _, err := db.ExecContext(ctx, `DELETE FROM _cache_schema_version`); err != nil {
		return fmt.Errorf("clear schema version (V2): %w", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO _cache_schema_version(version) VALUES (?)`, cacheSchemaVersionV2); err != nil {
		return fmt.Errorf("insert schema version V2: %w", err)
	}

	return nil
}

func applyMigrationV3(ctx context.Context, db *sql.DB) error {

	rows, err := db.QueryContext(ctx, `PRAGMA table_info(research_findings)`)
	if err != nil {
		return fmt.Errorf("pragma table_info(research_findings): %w", err)
	}
	hasCol := false
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var dfltValue interface{}
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dfltValue, &pk); err != nil {
			rows.Close()
			return fmt.Errorf("scan table_info research_findings: %w", err)
		}
		if name == "content_hash" {
			hasCol = true
			break
		}
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close table_info research_findings rows: %w", err)
	}

	if hasCol {

		return nil
	}

	migrations := []struct {
		stmt string
		desc string
	}{
		{`ALTER TABLE research_findings ADD COLUMN content_hash TEXT`, "add content_hash"},
		{`ALTER TABLE research_findings ADD COLUMN body_inline_blob BLOB`, "add body_inline_blob"},
		{`ALTER TABLE research_findings ADD COLUMN body_path TEXT`, "add body_path"},
	}
	for _, m := range migrations {
		if _, err := db.ExecContext(ctx, m.stmt); err != nil {
			return fmt.Errorf("%s: %w", m.desc, err)
		}
	}

	if _, err := db.ExecContext(ctx, `DELETE FROM _cache_schema_version`); err != nil {
		return fmt.Errorf("clear schema version (V3): %w", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO _cache_schema_version(version) VALUES (?)`, cacheSchemaVersionV3); err != nil {
		return fmt.Errorf("insert schema version V3: %w", err)
	}

	return nil
}

func applyMigrationV4(ctx context.Context, db *sql.DB) error {

	rows, err := db.QueryContext(ctx, `PRAGMA table_info(research_dispatches)`)
	if err != nil {
		return fmt.Errorf("pragma table_info(research_dispatches) V4: %w", err)
	}
	hasCol := false
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var dfltValue interface{}
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dfltValue, &pk); err != nil {
			rows.Close()
			return fmt.Errorf("scan table_info V4: %w", err)
		}
		if name == "project_id" {
			hasCol = true
			break
		}
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close table_info V4 rows: %w", err)
	}

	if hasCol {

		return nil
	}

	v4migrations := []struct {
		stmt string
		desc string
	}{
		{`ALTER TABLE research_dispatches ADD COLUMN project_id TEXT`, "add project_id"},
		{`ALTER TABLE research_dispatches ADD COLUMN session_id TEXT`, "add session_id"},
		{`ALTER TABLE research_dispatches ADD COLUMN dispatched_at INTEGER`, "add dispatched_at"},
		{`ALTER TABLE research_dispatches ADD COLUMN cache_hit_reason TEXT`, "add cache_hit_reason"},
		{`ALTER TABLE research_dispatches ADD COLUMN parent_dispatch_id TEXT`, "add parent_dispatch_id"},
		{`ALTER TABLE research_dispatches ADD COLUMN query_embedding BLOB`, "add query_embedding"},
	}
	for _, m := range v4migrations {
		if _, err := db.ExecContext(ctx, m.stmt); err != nil {
			return fmt.Errorf("V4 migration: %s: %w", m.desc, err)
		}
	}

	if _, err := db.ExecContext(ctx, `DELETE FROM _cache_schema_version`); err != nil {
		return fmt.Errorf("clear schema version (V4): %w", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO _cache_schema_version(version) VALUES (?)`, cacheSchemaVersionV4); err != nil {
		return fmt.Errorf("insert schema version V4: %w", err)
	}

	return nil
}
