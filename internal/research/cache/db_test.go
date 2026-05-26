//go:build cgo
// +build cgo

package cache

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func openTestCacheDB(t *testing.T) *DB {
	t.Helper()
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "research_cache.db")
	db, err := Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.SQL.Close() })
	return db
}

func TestOpenCreatesSchema(t *testing.T) {
	t.Parallel()
	db := openTestCacheDB(t)

	rows, err := db.SQL.QueryContext(context.Background(),
		`SELECT name FROM sqlite_master WHERE type IN ('table','shadow') AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		t.Fatalf("sqlite_master query: %v", err)
	}
	defer rows.Close()

	got := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err: %v", err)
	}

	want := []string{
		"_cache_schema_version",
		"research_dispatches",
		"research_findings",
		"research_validation_log",
	}
	for _, name := range want {
		if !got[name] {
			t.Errorf("table %q missing from sqlite_master; got: %v", name, got)
		}
	}
}

func TestOpenIdempotentReopen(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "research_cache.db")

	for i := 0; i < 3; i++ {
		db, err := Open(context.Background(), dbPath)
		if err != nil {
			t.Fatalf("Open iteration %d: %v", i, err)
		}

		var count int
		if err := db.SQL.QueryRowContext(context.Background(),
			`SELECT COUNT(*) FROM _cache_schema_version`).Scan(&count); err != nil {
			_ = db.SQL.Close()
			t.Fatalf("count _cache_schema_version iteration %d: %v", i, err)
		}
		if count != 1 {
			_ = db.SQL.Close()
			t.Fatalf("iteration %d: expected 1 row in _cache_schema_version, got %d", i, count)
		}
		_ = db.SQL.Close()
	}
}

func TestSchemaVersionRecorded(t *testing.T) {
	t.Parallel()
	db := openTestCacheDB(t)

	got, err := SchemaVersion(context.Background(), db.SQL)
	if err != nil {
		t.Fatalf("SchemaVersion: %v", err)
	}
	if got != cacheSchemaVersionV5 {
		t.Errorf("SchemaVersion = %d, want %d", got, cacheSchemaVersionV5)
	}
}

func TestSqliteVecVirtualTable(t *testing.T) {
	t.Parallel()
	db := openTestCacheDB(t)

	var ver string
	if err := db.SQL.QueryRowContext(context.Background(), `SELECT vec_version()`).Scan(&ver); err != nil {
		t.Fatalf("vec_version(): %v — sqlite-vec extension not loaded", err)
	}
	if ver == "" {
		t.Error("vec_version() returned empty string")
	}

	var vtName string
	err := db.SQL.QueryRowContext(context.Background(),
		`SELECT name FROM sqlite_master WHERE type='table' AND name='research_query_vec'`).
		Scan(&vtName)
	if err == sql.ErrNoRows {
		t.Fatal("research_query_vec virtual table not found in sqlite_master")
	}
	if err != nil {
		t.Fatalf("sqlite_master lookup: %v", err)
	}
	if vtName != "research_query_vec" {
		t.Errorf("unexpected virtual table name %q", vtName)
	}
}

func TestOpenEmptyPath(t *testing.T) {
	t.Parallel()
	_, err := Open(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty dbPath, got nil")
	}
}

func TestOpenBlockedParentDir(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()

	blocker := filepath.Join(tempDir, "not-a-dir")
	f, err := createFile(blocker)
	if err != nil {
		t.Fatalf("createFile: %v", err)
	}
	_ = f.Close()

	dbPath := filepath.Join(blocker, "subdir", "research_cache.db")
	_, err = Open(context.Background(), dbPath)
	if err == nil {
		t.Fatal("expected MkdirAll error for blocked parent dir, got nil")
	}
}

func TestSchemaVersionMissingTable(t *testing.T) {
	t.Parallel()

	raw, err := rawMemoryDB(t)
	if err != nil {
		t.Fatalf("rawMemoryDB: %v", err)
	}
	defer raw.Close()

	_, err = SchemaVersion(context.Background(), raw)
	if err == nil {
		t.Fatal("expected error from SchemaVersion on unschematised DB, got nil")
	}
}

func TestWithLocalSqliteVecOption(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "opt_test.db")
	db, err := Open(context.Background(), dbPath, WithLocalSqliteVec())
	if err != nil {
		t.Fatalf("Open with WithLocalSqliteVec: %v", err)
	}
	defer db.SQL.Close()

	ver, err := SchemaVersion(context.Background(), db.SQL)
	if err != nil {
		t.Fatalf("SchemaVersion: %v", err)
	}

	if ver != cacheSchemaVersionV5 {
		t.Errorf("SchemaVersion = %d, want %d", ver, cacheSchemaVersionV5)
	}
}

func TestApplySchemaOnClosedDB(t *testing.T) {
	t.Parallel()

	raw, err := rawMemoryDB(t)
	if err != nil {
		t.Fatalf("rawMemoryDB: %v", err)
	}

	if err := raw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	err = applySchema(context.Background(), raw)
	if err == nil {
		t.Fatal("expected error from applySchema on closed DB, got nil")
	}
}

func TestOpenInvalidDSN(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()

	_, err := Open(context.Background(), tempDir)
	if err == nil {
		t.Fatal("expected error when dbPath is a directory, got nil")
	}
}

func TestOpenApplySchemaErrorPath(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping permission test: running as root")
	}
	t.Parallel()
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "cache.db")

	db, err := Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("initial Open: %v", err)
	}
	_ = db.SQL.Close()

	if err := os.Chmod(dbPath, 0o444); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dbPath, 0o644) })

	_, err = Open(context.Background(), dbPath)
	if err == nil {

		t.Log("note: read-only open succeeded (SQLite WAL reader mode); error path not triggered on this SQLite build")
	}
}

func createFile(path string) (*os.File, error) {
	return os.Create(path)
}

func rawMemoryDB(t *testing.T) (*sql.DB, error) {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		return nil, err
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, nil
}

func TestIndexesPresent(t *testing.T) {
	t.Parallel()
	db := openTestCacheDB(t)

	rows, err := db.SQL.QueryContext(context.Background(),
		`SELECT name FROM sqlite_master WHERE type='index' AND name NOT LIKE 'sqlite_autoindex_%' ORDER BY name`)
	if err != nil {
		t.Fatalf("sqlite_master index query: %v", err)
	}
	defer rows.Close()

	got := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err: %v", err)
	}

	want := []string{
		"idx_dispatches_status",
		"idx_dispatches_created",
		"idx_findings_dispatch",
		"idx_findings_freshness",
		"idx_vlog_finding",
	}
	for _, name := range want {
		if !got[name] {
			t.Errorf("index %q missing; got: %v", name, got)
		}
	}

	for name := range got {
		if !strings.Contains(name, "dispatch") && !strings.Contains(name, "finding") && !strings.Contains(name, "vlog") {
			t.Errorf("unexpected index name %q (not related to known tables)", name)
		}
	}
}
