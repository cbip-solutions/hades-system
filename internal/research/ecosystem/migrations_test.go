//go:build cgo
// +build cgo

package ecosystem

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "eco.db")
	db, err := sql.Open("sqlite3", path+"?_foreign_keys=on&_journal_mode=WAL")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := ApplyMigrations(db); err != nil {
		t.Fatalf("ApplyMigrations: %v", err)
	}
	return db
}

func TestSchemaVersionConstant(t *testing.T) {
	if SchemaVersion != 2 {
		t.Errorf("SchemaVersion = %d; want 2 (Phase G G-5 retention bump)", SchemaVersion)
	}

	files, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		t.Fatalf("read migrations dir: %v", err)
	}

	var migrationFiles []string
	for _, f := range files {
		if !f.IsDir() {
			migrationFiles = append(migrationFiles, f.Name())
		}
	}
	const expectedMigrationCount = 9
	if len(migrationFiles) != expectedMigrationCount {
		t.Errorf("migrations/*.sql count = %d (%v); want %d (Phase A: 8; Phase G G-5: +1)",
			len(migrationFiles), migrationFiles, expectedMigrationCount)
	}
}

func TestIndefiniteRetainColumn(t *testing.T) {
	db := openTestDB(t)
	rows, err := db.Query("PRAGMA table_info(ecosystem_versions)")
	if err != nil {
		t.Fatalf("table_info: %v", err)
	}
	defer rows.Close()
	var found bool
	for rows.Next() {
		var (
			cid     int
			name    string
			ctype   string
			notnull int
			dflt    any
			pk      int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if name == "indefinite_retain" {
			found = true
			if ctype != "INTEGER" {
				t.Errorf("indefinite_retain type = %q; want INTEGER", ctype)
			}
			if notnull != 1 {
				t.Errorf("indefinite_retain notnull = %d; want 1", notnull)
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	if !found {
		t.Fatal("indefinite_retain column missing from ecosystem_versions")
	}

	if _, err := db.Exec(
		`INSERT INTO ecosystem_packages (ecosystem, name, canonical_namespace, upstream_url, last_indexed_at)
		 VALUES ('go', 'example.org/x', 'example.org', 'https://example.org/x', CURRENT_TIMESTAMP)`,
	); err != nil {
		t.Fatalf("insert package: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO ecosystem_versions (package_id, version, released_at)
		 VALUES ((SELECT id FROM ecosystem_packages WHERE name='example.org/x'), 'v1.0.0', CURRENT_TIMESTAMP)`,
	); err != nil {
		t.Fatalf("insert version: %v", err)
	}
	var retain int
	if err := db.QueryRow(
		`SELECT indefinite_retain FROM ecosystem_versions
		 WHERE version='v1.0.0'`,
	).Scan(&retain); err != nil {
		t.Fatalf("query retain: %v", err)
	}
	if retain != 0 {
		t.Errorf("default indefinite_retain = %d; want 0", retain)
	}

	_, err = db.Exec(
		`UPDATE ecosystem_versions SET indefinite_retain = 2 WHERE version = 'v1.0.0'`,
	)
	if err == nil {
		t.Error("expected CHECK violation for indefinite_retain = 2")
	}
}

func TestApplyMigrationsCreatesAllTables(t *testing.T) {
	db := openTestDB(t)
	wantTables := []string{
		"ecosystem_packages",
		"ecosystem_versions",
		"ecosystem_chunks",
		"ecosystem_chunks_fp32",
		"ecosystem_symbols",
		"ecosystem_changes",
		"ecosystem_chunks_fts",
		"ecosystem_chunks_vec_bin",
		"ecosystem_audit_chain",
		"ecosystem_schema_meta",
	}
	for _, tbl := range wantTables {
		var name string

		err := db.QueryRow(
			"SELECT name FROM sqlite_master WHERE name = ?",
			tbl,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %q not present: %v", tbl, err)
			continue
		}
		if name != tbl {
			t.Errorf("table name = %q; want %q", name, tbl)
		}
	}
}

func TestApplyMigrationsCreatesAllIndexes(t *testing.T) {
	db := openTestDB(t)
	wantIndexes := []string{
		"idx_chunks_pkg_version",
		"idx_chunks_symbol_path",
		"idx_chunks_fingerprint",
		"idx_symbols_path",
		"idx_changes_versions",
	}
	for _, idx := range wantIndexes {
		var name string
		err := db.QueryRow(
			"SELECT name FROM sqlite_master WHERE type = 'index' AND name = ?",
			idx,
		).Scan(&name)
		if err != nil {
			t.Errorf("index %q not present: %v", idx, err)
			continue
		}
		if name != idx {
			t.Errorf("index name = %q; want %q", name, idx)
		}
	}
}

func TestApplyMigrationsIdempotent(t *testing.T) {
	db := openTestDB(t)

	if err := ApplyMigrations(db); err != nil {
		t.Fatalf("re-run ApplyMigrations: %v", err)
	}
	var v int
	if err := db.QueryRow(
		"SELECT version FROM ecosystem_schema_meta WHERE id = 1",
	).Scan(&v); err != nil {
		t.Fatalf("query schema_meta: %v", err)
	}
	if v != SchemaVersion {
		t.Errorf("schema version after re-run = %d; want %d", v, SchemaVersion)
	}
}

func TestPackagesTableColumns(t *testing.T) {
	db := openTestDB(t)
	wantCols := map[string]string{
		"id":                    "INTEGER",
		"name":                  "TEXT",
		"ecosystem":             "TEXT",
		"upstream_url":          "TEXT",
		"canonical_namespace":   "TEXT",
		"last_indexed_at":       "DATETIME",
		"last_upstream_check":   "DATETIME",
		"latest_stable_version": "TEXT",
	}
	rows, err := db.Query("PRAGMA table_info(ecosystem_packages)")
	if err != nil {
		t.Fatalf("PRAGMA: %v", err)
	}
	defer rows.Close()
	gotCols := make(map[string]string)
	for rows.Next() {
		var cid int
		var name, ctype, notnull, dfltVal, pk sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltVal, &pk); err != nil {
			t.Fatalf("scan: %v", err)
		}
		gotCols[name.String] = strings.ToUpper(ctype.String)
	}
	for col, wantType := range wantCols {
		if got := gotCols[col]; !strings.HasPrefix(got, wantType) {
			t.Errorf("column %q type = %q; want prefix %q", col, got, wantType)
		}
	}
	// Negative assertion: the legacy/plan-file "language" column MUST
	// NOT exist (drift reconciliation: we use "ecosystem" to match
	// types.go PackageRef.Ecosystem).
	if _, present := gotCols["language"]; present {
		t.Errorf("ecosystem_packages has unexpected legacy column 'language'; want 'ecosystem' instead")
	}
}

func TestPackagesUniqueConstraint(t *testing.T) {
	db := openTestDB(t)

	_, err := db.Exec(
		`INSERT INTO ecosystem_packages (ecosystem, name, upstream_url, canonical_namespace)
		 VALUES ('go', 'crypto/sha256', 'https://pkg.go.dev/crypto/sha256', 'crypto/sha256')`,
	)
	if err != nil {
		t.Fatalf("insert #1: %v", err)
	}
	_, err = db.Exec(
		`INSERT INTO ecosystem_packages (ecosystem, name, upstream_url, canonical_namespace)
		 VALUES ('go', 'crypto/sha256', 'https://example.com/duplicate', 'crypto/sha256')`,
	)
	if err == nil {
		t.Errorf("insert #2 (duplicate): want UNIQUE constraint error; got nil")
	}
	if err != nil && !strings.Contains(err.Error(), "UNIQUE") {
		t.Errorf("insert #2 error = %v; want UNIQUE-constraint error", err)
	}
}

func TestChunksFTSIngestion(t *testing.T) {
	db := openTestDB(t)

	_, err := db.Exec(
		`INSERT INTO ecosystem_packages (ecosystem, name, upstream_url, canonical_namespace)
		 VALUES ('go', 'crypto/sha256', 'https://pkg.go.dev/crypto/sha256', 'crypto/sha256')`,
	)
	if err != nil {
		t.Fatalf("insert pkg: %v", err)
	}
	_, err = db.Exec(
		`INSERT INTO ecosystem_chunks (package_id, version_introduced, stable_in_json,
		    content_text, contextual_prefix, chunk_fingerprint,
		    source_type, symbol_path, kind, source_url)
		 VALUES (1, '1.22.3', '["1.22.3"]',
		    'func Sum256(data []byte) [Size]byte', 'crypto/sha256 SHA-256 hashing primitives',
		    'abc123', 'package_doc', 'crypto/sha256.Sum256', 'function',
		    'https://pkg.go.dev/crypto/sha256#Sum256')`,
	)
	if err != nil {
		t.Fatalf("insert chunk: %v", err)
	}
	_, err = db.Exec(
		`INSERT INTO ecosystem_chunks_fts (chunk_id, content_text, contextual_prefix, symbol_path)
		 VALUES (1, 'func Sum256(data []byte) [Size]byte',
		    'crypto/sha256 SHA-256 hashing primitives', 'crypto/sha256.Sum256')`,
	)
	if err != nil {
		t.Fatalf("insert FTS: %v", err)
	}

	var got int
	err = db.QueryRow(
		`SELECT chunk_id FROM ecosystem_chunks_fts WHERE ecosystem_chunks_fts MATCH ?`,
		"Sum256",
	).Scan(&got)
	if err != nil {
		t.Fatalf("FTS MATCH query: %v", err)
	}
	if got != 1 {
		t.Errorf("FTS MATCH chunk_id = %d; want 1", got)
	}
}

func TestChunkOversizedColumn(t *testing.T) {
	db := openTestDB(t)
	_, err := db.Exec(
		`INSERT INTO ecosystem_packages (ecosystem, name, canonical_namespace, upstream_url, latest_stable_version)
		 VALUES ('go', 'crypto/sha256', 'crypto/sha256', 'https://pkg.go.dev/crypto/sha256', '1.22.3')`,
	)
	if err != nil {
		t.Fatalf("insert pkg: %v", err)
	}

	_, err = db.Exec(
		`INSERT INTO ecosystem_chunks (package_id, version_introduced, stable_in_json,
		    content_text, chunk_fingerprint, source_type, symbol_path, kind, source_url)
		 VALUES (1, '1.22.3', '["1.22.3"]', 'x', 'abc', 'package_doc', 'y', 'function', 'u')`,
	)
	if err != nil {
		t.Fatalf("insert default oversized: %v", err)
	}
	var ovs int
	if err := db.QueryRow(`SELECT oversized FROM ecosystem_chunks WHERE id = 1`).Scan(&ovs); err != nil {
		t.Fatalf("read oversized: %v", err)
	}
	if ovs != 0 {
		t.Errorf("default oversized = %d; want 0", ovs)
	}

	_, err = db.Exec(
		`INSERT INTO ecosystem_chunks (package_id, version_introduced, stable_in_json,
		    content_text, chunk_fingerprint, source_type, symbol_path, kind, source_url, oversized)
		 VALUES (1, '1.22.4', '["1.22.4"]', 'y', 'def', 'package_doc', 'z', 'function', 'u', 1)`,
	)
	if err != nil {
		t.Fatalf("insert oversized=1: %v", err)
	}

	_, err = db.Exec(
		`INSERT INTO ecosystem_chunks (package_id, version_introduced, stable_in_json,
		    content_text, chunk_fingerprint, source_type, symbol_path, kind, source_url, oversized)
		 VALUES (1, '1.22.5', '["1.22.5"]', 'z', 'ghi', 'package_doc', 'w', 'function', 'u', 7)`,
	)
	if err == nil {
		t.Error("oversized=7 must violate CHECK; got nil error")
	}
}

func TestAuditChainSchema(t *testing.T) {
	db := openTestDB(t)

	_, err := db.Exec(
		`INSERT INTO ecosystem_audit_chain (seq, event_type, payload_json, parent_hash, self_hash, emitted_at, doctrine, partition_id)
		 VALUES (1, 92, '{"q":"foo"}', '', 'abc', datetime('now'), 'max-scope', '2026-05')`,
	)
	if err != nil {
		t.Fatalf("insert audit chain: %v", err)
	}
	var seq int64
	var evtType int
	if err := db.QueryRow("SELECT seq, event_type FROM ecosystem_audit_chain WHERE seq = 1").Scan(&seq, &evtType); err != nil {
		t.Fatalf("query: %v", err)
	}
	if seq != 1 || evtType != 92 {
		t.Errorf("got (seq=%d, event_type=%d); want (1, 92)", seq, evtType)
	}
}

// TestChunksForeignKeyEnforced verifies foreign_keys=ON rejects a chunk
// with non-existent package_id. SQLite defaults FK enforcement off; the
// openTestDB DSN enables it (`_foreign_keys=on`) and production callers
// MUST do the same.
func TestChunksForeignKeyEnforced(t *testing.T) {
	db := openTestDB(t)

	_, err := db.Exec(
		`INSERT INTO ecosystem_chunks (package_id, version_introduced, stable_in_json,
		    content_text, chunk_fingerprint, source_type, symbol_path, kind, source_url)
		 VALUES (99999, '1.22.3', '["1.22.3"]', 'x', 'abc', 'package_doc', 'y', 'function', 'u')`,
	)
	if err == nil {
		t.Errorf("FK violation: want error; got nil")
	}
	if err != nil && !strings.Contains(err.Error(), "FOREIGN KEY") {
		t.Errorf("error = %v; want FOREIGN KEY constraint", err)
	}
}

// TestAuditChainEventTypeCheckRejectsOutOfRange covers both CHECK
// boundaries (91 = one below valid range; 100 = one above). Plan 14
// EventType slots are exactly 92..99; values outside MUST be rejected
// at the SQL layer (defense-in-depth: ChainEmitter validates Go-side
// too, but the schema CHECK is the load-bearing backstop).
func TestAuditChainEventTypeCheckRejectsOutOfRange(t *testing.T) {
	db := openTestDB(t)
	cases := []struct {
		name      string
		eventType int
	}{
		{"below_range_91", 91},
		{"above_range_100", 100},
		{"way_below_0", 0},
		{"way_above_127", 127},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := db.Exec(
				`INSERT INTO ecosystem_audit_chain (event_type, payload_json, parent_hash, self_hash, emitted_at, doctrine, partition_id)
				 VALUES (?, '{}', '', 'h', datetime('now'), 'max-scope', '2026-05')`,
				tc.eventType,
			)
			if err == nil {
				t.Errorf("event_type=%d: want CHECK violation; got nil", tc.eventType)
				return
			}
			if !strings.Contains(err.Error(), "CHECK") {
				t.Errorf("event_type=%d: err = %v; want CHECK constraint", tc.eventType, err)
			}
		})
	}
}

func TestChangesChangeTypeCheckRejectsInvalid(t *testing.T) {
	db := openTestDB(t)
	_, err := db.Exec(
		`INSERT INTO ecosystem_packages (ecosystem, name, upstream_url, canonical_namespace)
		 VALUES ('go', 'crypto/sha256', 'https://pkg.go.dev/crypto/sha256', 'crypto/sha256')`,
	)
	if err != nil {
		t.Fatalf("insert pkg: %v", err)
	}

	_, err = db.Exec(
		`INSERT INTO ecosystem_changes (package_id, version_from, version_to, change_type, symbol_path, description, source_extracted)
		 VALUES (1, '1.22.0', '1.22.1', 'added', 'crypto/sha256.NewSomething', 'added new fn', 'explicit_changelog')`,
	)
	if err != nil {
		t.Fatalf("valid change_type insert: %v", err)
	}

	_, err = db.Exec(
		`INSERT INTO ecosystem_changes (package_id, version_from, version_to, change_type, symbol_path, description, source_extracted)
		 VALUES (1, '1.22.0', '1.22.1', 'frobnicated', 'crypto/sha256.X', 'invalid', 'explicit_changelog')`,
	)
	if err == nil {
		t.Errorf("change_type='frobnicated': want CHECK violation; got nil")
	}
	if err != nil && !strings.Contains(err.Error(), "CHECK") {
		t.Errorf("err = %v; want CHECK constraint", err)
	}

	_, err = db.Exec(
		`INSERT INTO ecosystem_changes (package_id, version_from, version_to, change_type, symbol_path, description, source_extracted)
		 VALUES (1, '1.22.2', '1.22.3', 'added', 'x', 'd', 'some_other_source')`,
	)
	if err == nil {
		t.Errorf("source_extracted='some_other_source': want CHECK violation; got nil")
	}
	if err != nil && !strings.Contains(err.Error(), "CHECK") {
		t.Errorf("err = %v; want CHECK constraint", err)
	}
}

func TestApplyMigrationsNilDB(t *testing.T) {
	err := ApplyMigrations(nil)
	if err == nil {
		t.Fatal("ApplyMigrations(nil): want error; got nil")
	}
	if !strings.Contains(err.Error(), "nil") {
		t.Errorf("err = %v; want error mentioning 'nil'", err)
	}
}

func TestApplyMigrationsSchemaMetaSeedFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "eco-seed-fail.db")
	db, err := sql.Open("sqlite3", path+"?_foreign_keys=on")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if _, err := db.Exec(
		`CREATE TABLE ecosystem_schema_meta (id INTEGER PRIMARY KEY CHECK (id = 1), bogus_col INTEGER)`,
	); err != nil {
		t.Fatalf("seed pre-create: %v", err)
	}
	err = ApplyMigrations(db)
	if err == nil {
		t.Fatal("ApplyMigrations: want error; got nil")
	}
	if !strings.Contains(err.Error(), "schema_meta seed") {
		t.Errorf("err = %v; want wrap mentioning 'schema_meta seed'", err)
	}
}

func TestApplyMigrationsReadCurrentVersionFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "eco-readver-fail.db")
	db, err := sql.Open("sqlite3", path+"?_foreign_keys=on")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if _, err := db.Exec(
		`CREATE TABLE ecosystem_schema_meta (id INTEGER PRIMARY KEY CHECK (id = 1), version TEXT NOT NULL)`,
	); err != nil {
		t.Fatalf("pre-create: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO ecosystem_schema_meta (id, version) VALUES (1, 'not-an-int')`,
	); err != nil {
		t.Fatalf("seed text value: %v", err)
	}
	err = ApplyMigrations(db)
	if err == nil {
		t.Fatal("ApplyMigrations: want error; got nil")
	}
	if !strings.Contains(err.Error(), "read current version") {
		t.Errorf("err = %v; want wrap mentioning 'read current version'", err)
	}
}

func TestApplyMigrationsClosedDB(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "eco-closed.db")
	db, err := sql.Open("sqlite3", path+"?_foreign_keys=on")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	err = ApplyMigrations(db)
	if err == nil {
		t.Fatal("ApplyMigrations on closed db: want error; got nil")
	}
	if !strings.Contains(err.Error(), "schema_meta create") {
		t.Errorf("err = %v; want wrap mentioning 'schema_meta create'", err)
	}
}

func TestApplyMigrationsReReadsCurrentVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "eco-reread.db")

	db1, err := sql.Open("sqlite3", path+"?_foreign_keys=on&_journal_mode=WAL")
	if err != nil {
		t.Fatalf("open #1: %v", err)
	}
	if err := ApplyMigrations(db1); err != nil {
		t.Fatalf("ApplyMigrations #1: %v", err)
	}
	if err := db1.Close(); err != nil {
		t.Fatalf("close #1: %v", err)
	}

	db2, err := sql.Open("sqlite3", path+"?_foreign_keys=on&_journal_mode=WAL")
	if err != nil {
		t.Fatalf("open #2: %v", err)
	}
	t.Cleanup(func() { _ = db2.Close() })
	if err := ApplyMigrations(db2); err != nil {
		t.Fatalf("ApplyMigrations #2 (short-circuit): %v", err)
	}
	var v int
	if err := db2.QueryRow("SELECT version FROM ecosystem_schema_meta WHERE id = 1").Scan(&v); err != nil {
		t.Fatalf("post-re-apply version read: %v", err)
	}
	if v != SchemaVersion {
		t.Errorf("after re-open + re-apply: version = %d; want %d", v, SchemaVersion)
	}
}

func TestVersionsUniqueConstraint(t *testing.T) {
	db := openTestDB(t)
	_, err := db.Exec(
		`INSERT INTO ecosystem_packages (ecosystem, name, upstream_url, canonical_namespace)
		 VALUES ('go', 'crypto/sha256', 'https://pkg.go.dev/crypto/sha256', 'crypto/sha256')`,
	)
	if err != nil {
		t.Fatalf("insert pkg: %v", err)
	}
	_, err = db.Exec(
		`INSERT INTO ecosystem_versions (package_id, version, released_at)
		 VALUES (1, '1.22.3', datetime('now'))`,
	)
	if err != nil {
		t.Fatalf("insert version #1: %v", err)
	}
	_, err = db.Exec(
		`INSERT INTO ecosystem_versions (package_id, version, released_at)
		 VALUES (1, '1.22.3', datetime('now'))`,
	)
	if err == nil {
		t.Errorf("duplicate (package_id, version): want UNIQUE violation; got nil")
	}
	if err != nil && !strings.Contains(err.Error(), "UNIQUE") {
		t.Errorf("err = %v; want UNIQUE constraint", err)
	}

	_, err = db.Exec(
		`INSERT INTO ecosystem_packages (ecosystem, name, upstream_url, canonical_namespace)
		 VALUES ('go', 'crypto/sha512', 'https://pkg.go.dev/crypto/sha512', 'crypto/sha512')`,
	)
	if err != nil {
		t.Fatalf("insert pkg #2: %v", err)
	}
	_, err = db.Exec(
		`INSERT INTO ecosystem_versions (package_id, version, released_at)
		 VALUES (2, '1.22.3', datetime('now'))`,
	)
	if err != nil {
		t.Errorf("insert version under pkg #2: want no error; got %v", err)
	}
}
