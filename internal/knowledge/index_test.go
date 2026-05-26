// Package knowledge — tests for the FTS5 + supplementary metadata
// schema initialiser (Plan 7 Phase G Task G-1).
//
// Spec reference: docs/superpowers/plans/2026-05-01-plan-7-phase-G-knowledge.md
// §"Task G-1" lines 78–516 (canonical) — schema lockstep between
// 061_knowledge_index_extension_hooks.sql and Init() is enforced by
// TestSchemaParityWithMigrationFile (a CI-grep-equivalent in-process
// check).
//
// The three extension-hook columns (audit_chain_anchor,
// ecosystem_join_keys, caronte_symbol_refs) MUST ship NULL by default
// (inv-zen-130). TestMetaTableHasExtensionHookColumns is the production
// anchor; the compliance test in G-16 will additionally enforce no
// INSERT statement populates them.
package knowledge

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func openTestIndex(t *testing.T) (*sql.DB, string) {
	t.Helper()
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "index.db")
	db, err := Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := Init(context.Background(), db); err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, dbPath
}

func TestInitCreatesFTS5VirtualTable(t *testing.T) {
	db, _ := openTestIndex(t)
	var name, ttype string
	err := db.QueryRow(`
		SELECT name, type FROM sqlite_master
		WHERE name = 'knowledge_fts' AND type = 'table'
	`).Scan(&name, &ttype)
	if err != nil {
		t.Fatalf("knowledge_fts virtual table not created: %v", err)
	}
	if name != "knowledge_fts" {
		t.Errorf("name = %s, want knowledge_fts", name)
	}
}

func TestFTS5VirtualTableIsSearchable(t *testing.T) {
	db, _ := openTestIndex(t)
	ctx := context.Background()

	if _, err := db.ExecContext(ctx,
		`INSERT INTO knowledge_fts (rowid, content_text) VALUES (1, ?)`,
		"the quick brown fox jumps over the lazy dog",
	); err != nil {
		t.Fatalf("INSERT into FTS5 table: %v", err)
	}

	var rowid int64
	err := db.QueryRowContext(ctx,
		`SELECT rowid FROM knowledge_fts WHERE knowledge_fts MATCH ?`,
		"fox",
	).Scan(&rowid)
	if err != nil {
		t.Fatalf("FTS5 MATCH search: %v", err)
	}
	if rowid != 1 {
		t.Errorf("rowid = %d, want 1", rowid)
	}
}

func TestInitCreatesMetaTable(t *testing.T) {
	db, _ := openTestIndex(t)
	var name string
	err := db.QueryRow(`
		SELECT name FROM sqlite_master
		WHERE name = 'knowledge_meta' AND type = 'table'
	`).Scan(&name)
	if err != nil {
		t.Fatalf("knowledge_meta table not created: %v", err)
	}
}

func TestMetaTableHasExtensionHookColumns(t *testing.T) {

	db, _ := openTestIndex(t)
	rows, err := db.Query(`PRAGMA table_info(knowledge_meta)`)
	if err != nil {
		t.Fatalf("PRAGMA table_info: %v", err)
	}
	defer rows.Close()

	want := map[string]bool{
		"audit_chain_anchor":  false,
		"ecosystem_join_keys": false,
		"caronte_symbol_refs": false,
	}
	for rows.Next() {
		var cid int
		var name, ttype string
		var notnull int
		var dfltValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &ttype, &notnull, &dfltValue, &pk); err != nil {
			t.Fatalf("Scan: %v", err)
		}
		if _, ok := want[name]; ok {
			if notnull != 0 {
				t.Errorf("column %s: NOT NULL constraint present (must be nullable per inv-zen-130)", name)
			}
			if dfltValue.Valid {
				t.Errorf("column %s: default value %q present (must be NULL by default per inv-zen-130)", name, dfltValue.String)
			}
			if ttype != "TEXT" {
				t.Errorf("column %s: type %s, want TEXT", name, ttype)
			}
			want[name] = true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err: %v", err)
	}
	for col, found := range want {
		if !found {
			t.Errorf("extension-hook column %s missing from knowledge_meta schema", col)
		}
	}
}

func TestMetaTableHasAllRequiredColumns(t *testing.T) {
	db, _ := openTestIndex(t)
	rows, err := db.Query(`PRAGMA table_info(knowledge_meta)`)
	if err != nil {
		t.Fatalf("PRAGMA table_info: %v", err)
	}
	defer rows.Close()

	type colSpec struct {
		ttype   string
		notnull int
	}
	want := map[string]colSpec{
		"rowid":               {"INTEGER", 0},
		"file_path":           {"TEXT", 1},
		"project_id":          {"TEXT", 0},
		"project_alias":       {"TEXT", 0},
		"file_type":           {"TEXT", 1},
		"title":               {"TEXT", 0},
		"frontmatter_json":    {"TEXT", 0},
		"last_modified":       {"INTEGER", 1},
		"last_indexed":        {"INTEGER", 1},
		"audit_chain_anchor":  {"TEXT", 0},
		"ecosystem_join_keys": {"TEXT", 0},
		"caronte_symbol_refs": {"TEXT", 0},
	}
	got := map[string]colSpec{}
	for rows.Next() {
		var cid int
		var name, ttype string
		var notnull int
		var dfltValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &ttype, &notnull, &dfltValue, &pk); err != nil {
			t.Fatalf("Scan: %v", err)
		}
		got[name] = colSpec{ttype: ttype, notnull: notnull}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err: %v", err)
	}
	for col, w := range want {
		g, ok := got[col]
		if !ok {
			t.Errorf("column %s missing from knowledge_meta", col)
			continue
		}
		if g.ttype != w.ttype {
			t.Errorf("column %s: type %s, want %s", col, g.ttype, w.ttype)
		}
		if g.notnull != w.notnull {
			t.Errorf("column %s: notnull=%d, want %d", col, g.notnull, w.notnull)
		}
	}
	for col := range got {
		if _, ok := want[col]; !ok {
			t.Errorf("unexpected column %s in knowledge_meta (extra)", col)
		}
	}
}

// TestMetaTableFileTypeCheckConstraint asserts the CHECK constraint on
// file_type is active — the enum is the contract for downstream
// scanners (G-3..G-6). An out-of-range insert MUST fail.
func TestMetaTableFileTypeCheckConstraint(t *testing.T) {
	db, _ := openTestIndex(t)
	_, err := db.Exec(`
		INSERT INTO knowledge_meta
		(rowid, file_path, file_type, last_modified, last_indexed)
		VALUES (1, '/tmp/x.md', 'invalid_type', 0, 0)
	`)
	if err == nil {
		t.Fatal("expected CHECK constraint failure on file_type='invalid_type', got nil")
	}

	for i, ft := range []string{"memory", "research", "adr", "spec", "plan", "handoff"} {
		_, err := db.Exec(`
			INSERT INTO knowledge_meta
			(rowid, file_path, file_type, last_modified, last_indexed)
			VALUES (?, ?, ?, 0, 0)
		`, 100+i, "/tmp/"+ft+".md", ft)
		if err != nil {
			t.Errorf("valid file_type %q rejected: %v", ft, err)
		}
	}
}

func TestMetaTableUniqueFilePath(t *testing.T) {
	db, _ := openTestIndex(t)
	_, err := db.Exec(`
		INSERT INTO knowledge_meta
		(rowid, file_path, file_type, last_modified, last_indexed)
		VALUES (1, '/tmp/dup.md', 'memory', 0, 0)
	`)
	if err != nil {
		t.Fatalf("first insert: %v", err)
	}
	_, err = db.Exec(`
		INSERT INTO knowledge_meta
		(rowid, file_path, file_type, last_modified, last_indexed)
		VALUES (2, '/tmp/dup.md', 'memory', 0, 0)
	`)
	if err == nil {
		t.Fatal("expected UNIQUE constraint failure on duplicate file_path, got nil")
	}
}

func TestInitIdempotent(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "index.db")
	db, err := Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := Init(context.Background(), db); err != nil {
		t.Fatalf("first Init: %v", err)
	}
	if err := Init(context.Background(), db); err != nil {
		t.Fatalf("second Init (idempotent): %v", err)
	}
	if err := Init(context.Background(), db); err != nil {
		t.Fatalf("third Init (idempotent): %v", err)
	}
}

func TestInitSurvivesDataAcrossInit(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "index.db")
	db, err := Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := Init(context.Background(), db); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO knowledge_meta
		(rowid, file_path, file_type, last_modified, last_indexed)
		VALUES (42, '/tmp/persist.md', 'memory', 1000, 2000)
	`); err != nil {
		t.Fatalf("seed insert: %v", err)
	}

	if err := Init(context.Background(), db); err != nil {
		t.Fatalf("re-Init: %v", err)
	}
	var got int64
	if err := db.QueryRow(`
		SELECT last_modified FROM knowledge_meta WHERE rowid = 42
	`).Scan(&got); err != nil {
		t.Fatalf("re-read after re-Init: %v", err)
	}
	if got != 1000 {
		t.Errorf("last_modified = %d, want 1000 (data lost across re-Init)", got)
	}
}

func TestInitCreatesProjectIndex(t *testing.T) {
	db, _ := openTestIndex(t)
	var name string
	err := db.QueryRow(`
		SELECT name FROM sqlite_master
		WHERE type = 'index' AND name = 'idx_knowledge_meta_project'
	`).Scan(&name)
	if err != nil {
		t.Fatalf("idx_knowledge_meta_project not created: %v", err)
	}
}

func TestInitCreatesFilePathIndex(t *testing.T) {
	db, _ := openTestIndex(t)
	var name string
	err := db.QueryRow(`
		SELECT name FROM sqlite_master
		WHERE type = 'index' AND name = 'idx_knowledge_meta_file_path'
	`).Scan(&name)
	if err != nil {
		t.Fatalf("idx_knowledge_meta_file_path not created: %v", err)
	}
}

func TestKnowledgeFTS5SchemaSentinelReachable(t *testing.T) {
	if err := knowledgeFTS5SchemaSentinel(); err != nil {
		t.Errorf("sentinel returned %v, want nil (proves schema code path reachable)", err)
	}
}

func TestOpenCreatesParentDir(t *testing.T) {
	tempDir := t.TempDir()
	nested := filepath.Join(tempDir, "a", "b", "c")
	dbPath := filepath.Join(nested, "index.db")
	db, err := Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("Open with non-existent parent dir: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := os.Stat(nested); err != nil {
		t.Errorf("parent dir %s not created: %v", nested, err)
	}
}

func TestOpenRejectsEmptyPath(t *testing.T) {
	_, err := Open(context.Background(), "")
	if err == nil {
		t.Fatal("expected error on empty dbPath, got nil")
	}
	if !strings.Contains(err.Error(), "dbPath") {
		t.Errorf("error %q does not mention dbPath", err)
	}
}

// TestOpenFailsOnUnwritableParent asserts Open returns an informative
// error when the parent directory cannot be created. We do not attempt
// to test this on every OS — POSIX-style permission checks only run
// when the test binary is not root.
func TestOpenFailsOnUnwritableParent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission semantics only")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses POSIX permission checks")
	}
	tempDir := t.TempDir()
	readonly := filepath.Join(tempDir, "ro")
	if err := os.MkdirAll(readonly, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.Chmod(readonly, 0o500); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	t.Cleanup(func() {

		_ = os.Chmod(readonly, 0o755)
	})
	dbPath := filepath.Join(readonly, "child", "index.db")
	_, err := Open(context.Background(), dbPath)
	if err == nil {
		t.Fatal("expected error on unwritable parent, got nil")
	}
}

func TestInitRejectsNilDB(t *testing.T) {
	err := Init(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error on nil db, got nil")
	}
	if !strings.Contains(err.Error(), "nil") {
		t.Errorf("error %q does not mention nil", err)
	}
}

func TestInitFailsOnClosedDB(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "index.db")
	db, err := Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	err = Init(context.Background(), db)
	if err == nil {
		t.Fatal("expected error on closed db, got nil")
	}
	if !strings.Contains(err.Error(), "knowledge:") {
		t.Errorf("error %q missing 'knowledge:' prefix", err)
	}
}

func TestOpenFailsOnPingContextCanceled(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "index.db")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Open(ctx, dbPath)
	if err == nil {
		t.Fatal("expected error on canceled context, got nil")
	}
	if !strings.Contains(err.Error(), "knowledge:") {
		t.Errorf("error %q missing 'knowledge:' prefix", err)
	}
}

func TestOpenWrapsSQLOpenError(t *testing.T) {
	tempDir := t.TempDir()

	dbPath := filepath.Join(tempDir, "x?_txlock=bogus")
	_, err := Open(context.Background(), dbPath)
	if err == nil {
		t.Fatal("expected error on invalid _txlock smuggled in dbPath, got nil")
	}
	if !strings.Contains(err.Error(), "knowledge:") {
		t.Errorf("error %q missing 'knowledge:' prefix (wrap lost?)", err)
	}
	if !strings.Contains(err.Error(), "_txlock") {
		t.Errorf("error %q does not surface underlying driver error", err)
	}
}

func TestBuildDSNContainsCanonicalPragmas(t *testing.T) {
	dsn := buildDSN("/tmp/x.db")
	for _, want := range []string{
		"file:/tmp/x.db",
		"_pragma=busy_timeout%285000%29",
		"_pragma=journal_mode%28WAL%29",
		"_pragma=foreign_keys%28ON%29",
	} {
		if !strings.Contains(dsn, want) {
			t.Errorf("DSN %q missing fragment %q", dsn, want)
		}
	}
}

func TestSchemaParityWithMigrationFile(t *testing.T) {

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")
	sqlPath := filepath.Join(repoRoot, "internal", "store", "schema", "061_knowledge_index_extension_hooks.sql")
	sqlBytes, err := os.ReadFile(sqlPath)
	if err != nil {
		t.Fatalf("read 061 migration file: %v", err)
	}
	sqlText := string(sqlBytes)

	if !strings.Contains(sqlText, "NOT applied via internal/store") {
		t.Error("061 SQL file missing 'NOT applied via internal/store' header marker")
	}

	for _, col := range []string{
		"rowid", "file_path", "project_id", "project_alias", "file_type",
		"title", "frontmatter_json", "last_modified", "last_indexed",
		"audit_chain_anchor", "ecosystem_join_keys", "caronte_symbol_refs",
	} {
		if !strings.Contains(sqlText, col) {
			t.Errorf("061 SQL file does not reference column %q", col)
		}
	}

	for _, frag := range []string{
		"CREATE VIRTUAL TABLE",
		"USING fts5",
		"CREATE TABLE IF NOT EXISTS knowledge_meta",
		"idx_knowledge_meta_project",
		"idx_knowledge_meta_file_path",
	} {
		if !strings.Contains(sqlText, frag) {
			t.Errorf("061 SQL file missing fragment %q", frag)
		}
	}
}

func TestIndexInsertsRow(t *testing.T) {
	db, _ := openTestIndex(t)
	doc := Doc{
		FilePath:     "/tmp/test/memory.md",
		ProjectID:    "abc123",
		ProjectAlias: "internal-platform-x",
		FileType:     FileTypeMemory,
		Title:        "Test memory",
		ContentText:  "the content body",
		LastModified: time.Now(),
		LastIndexed:  time.Now(),
	}
	if err := IndexDoc(context.Background(), db, doc); err != nil {
		t.Fatalf("IndexDoc: %v", err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM knowledge_meta`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("knowledge_meta count = %d, want 1", count)
	}
	var ftsCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM knowledge_fts`).Scan(&ftsCount); err != nil {
		t.Fatalf("fts count: %v", err)
	}
	if ftsCount != 1 {
		t.Errorf("knowledge_fts count = %d, want 1", ftsCount)
	}
}

func TestIndexInsertSearchableViaFTS5(t *testing.T) {
	db, _ := openTestIndex(t)
	doc := Doc{
		FilePath:     "/tmp/searchable.md",
		ProjectID:    "p1",
		ProjectAlias: "alpha",
		FileType:     FileTypeMemory,
		Title:        "alpha title",
		ContentText:  "the quick brown fox jumps over the lazy dog",
		LastModified: time.Unix(1_000_000, 0),
		LastIndexed:  time.Unix(2_000_000, 0),
	}
	if err := IndexDoc(context.Background(), db, doc); err != nil {
		t.Fatalf("IndexDoc: %v", err)
	}
	var path, title string
	err := db.QueryRow(`
		SELECT m.file_path, m.title
		FROM knowledge_meta m
		JOIN knowledge_fts f ON f.rowid = m.rowid
		WHERE knowledge_fts MATCH ?
	`, "fox").Scan(&path, &title)
	if err != nil {
		t.Fatalf("FTS5 MATCH JOIN knowledge_meta: %v", err)
	}
	if path != doc.FilePath {
		t.Errorf("path = %q, want %q", path, doc.FilePath)
	}
	if title != doc.Title {
		t.Errorf("title = %q, want %q", title, doc.Title)
	}
}

func TestIndexInsertPersistsFrontmatterJSON(t *testing.T) {
	db, _ := openTestIndex(t)
	fm := json.RawMessage(`{"date":"2026-05-01","tags":["a","b"]}`)
	doc := Doc{
		FilePath:        "/tmp/fm-yes.md",
		ProjectID:       "p",
		FileType:        FileTypeMemory,
		Title:           "x",
		ContentText:     "body",
		FrontmatterJSON: fm,
		LastModified:    time.Now(),
		LastIndexed:     time.Now(),
	}
	if err := IndexDoc(context.Background(), db, doc); err != nil {
		t.Fatalf("IndexDoc with FM: %v", err)
	}
	var got sql.NullString
	if err := db.QueryRow(`SELECT frontmatter_json FROM knowledge_meta WHERE file_path = ?`, doc.FilePath).Scan(&got); err != nil {
		t.Fatalf("read frontmatter_json: %v", err)
	}
	if !got.Valid {
		t.Fatalf("frontmatter_json is NULL, want %q", string(fm))
	}
	if got.String != string(fm) {
		t.Errorf("frontmatter_json = %q, want %q", got.String, string(fm))
	}

	doc2 := Doc{
		FilePath:     "/tmp/fm-no.md",
		ProjectID:    "p",
		FileType:     FileTypeMemory,
		Title:        "x",
		ContentText:  "body",
		LastModified: time.Now(),
		LastIndexed:  time.Now(),
	}
	if err := IndexDoc(context.Background(), db, doc2); err != nil {
		t.Fatalf("IndexDoc without FM: %v", err)
	}
	var got2 sql.NullString
	if err := db.QueryRow(`SELECT frontmatter_json FROM knowledge_meta WHERE file_path = ?`, doc2.FilePath).Scan(&got2); err != nil {
		t.Fatalf("read frontmatter_json (empty): %v", err)
	}
	if got2.Valid {
		t.Errorf("frontmatter_json = %q (Valid), want NULL", got2.String)
	}
}

// TestIndexExtensionHookColumnsNullByDefault asserts the runtime-observable
// half of inv-zen-130: post-INSERT, the three extension-hook columns MUST
// be NULL. Even if a Doc carries Valid=true on AuditChainAnchor /
// EcosystemJoinKeys / CaronteSymbolRefs (e.g., tests, malicious caller,
// future-Plan code paths reused incorrectly), the canonical INSERT does
// NOT route them through — they remain NULL in the row.
func TestIndexExtensionHookColumnsNullByDefault(t *testing.T) {
	db, _ := openTestIndex(t)
	doc := Doc{
		FilePath:     "/tmp/x.md",
		ProjectID:    "p",
		FileType:     FileTypeMemory,
		Title:        "x",
		ContentText:  "body",
		LastModified: time.Now(),
		LastIndexed:  time.Now(),

		AuditChainAnchor:  sql.NullString{String: "should-be-dropped", Valid: true},
		EcosystemJoinKeys: sql.NullString{String: "should-be-dropped", Valid: true},
		CaronteSymbolRefs: sql.NullString{String: "should-be-dropped", Valid: true},
	}
	if err := IndexDoc(context.Background(), db, doc); err != nil {
		t.Fatalf("IndexDoc: %v", err)
	}
	var auditNull, ecoNull, caronteNull int
	err := db.QueryRow(`
		SELECT
		    audit_chain_anchor IS NULL,
		    ecosystem_join_keys IS NULL,
		    caronte_symbol_refs IS NULL
		FROM knowledge_meta WHERE file_path = ?
	`, doc.FilePath).Scan(&auditNull, &ecoNull, &caronteNull)
	if err != nil {
		t.Fatalf("post-INSERT NULL check: %v", err)
	}
	if auditNull != 1 || ecoNull != 1 || caronteNull != 1 {
		t.Errorf("inv-zen-130 violation: audit=%d eco=%d caronte=%d (all want 1=NULL)",
			auditNull, ecoNull, caronteNull)
	}
}

func TestIndexUpsertReplacesExisting(t *testing.T) {
	db, _ := openTestIndex(t)
	d1 := Doc{
		FilePath:     "/tmp/u.md",
		ProjectID:    "p",
		FileType:     FileTypeMemory,
		Title:        "v1",
		ContentText:  "first version",
		LastModified: time.Now(),
		LastIndexed:  time.Now(),
	}
	if err := IndexDoc(context.Background(), db, d1); err != nil {
		t.Fatalf("IndexDoc v1: %v", err)
	}
	d2 := d1
	d2.Title = "v2"
	d2.ContentText = "second version"
	if err := IndexDoc(context.Background(), db, d2); err != nil {
		t.Fatalf("IndexDoc v2: %v", err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM knowledge_meta WHERE file_path = ?`, d1.FilePath).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("after upsert, row count = %d, want 1", count)
	}
	var title, content string
	err := db.QueryRow(`
		SELECT m.title, f.content_text
		FROM knowledge_meta m
		JOIN knowledge_fts f ON f.rowid = m.rowid
		WHERE m.file_path = ?
	`, d1.FilePath).Scan(&title, &content)
	if err != nil {
		t.Fatalf("read upsert row: %v", err)
	}
	if title != "v2" || content != "second version" {
		t.Errorf("upsert did not replace: got title=%q content=%q", title, content)
	}

	var ftsCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM knowledge_fts`).Scan(&ftsCount); err != nil {
		t.Fatalf("fts count: %v", err)
	}
	if ftsCount != 1 {
		t.Errorf("after upsert, fts count = %d, want 1 (old FTS5 row dropped)", ftsCount)
	}
}

func TestIndexDeleteRemovesRow(t *testing.T) {
	db, _ := openTestIndex(t)
	d := Doc{
		FilePath:     "/tmp/d.md",
		ProjectID:    "p",
		FileType:     FileTypeMemory,
		Title:        "x",
		ContentText:  "y",
		LastModified: time.Now(),
		LastIndexed:  time.Now(),
	}
	if err := IndexDoc(context.Background(), db, d); err != nil {
		t.Fatalf("IndexDoc: %v", err)
	}
	if err := Delete(context.Background(), db, d.FilePath); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM knowledge_meta WHERE file_path = ?`, d.FilePath).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Errorf("after Delete, count = %d, want 0", count)
	}
	var ftsCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM knowledge_fts`).Scan(&ftsCount); err != nil {
		t.Fatalf("fts count: %v", err)
	}
	if ftsCount != 0 {
		t.Errorf("after Delete, fts count = %d, want 0 (FTS5 row also dropped)", ftsCount)
	}
}

// TestIndexDeleteIdempotentOnMissingPath proves Delete is a clean no-op
// when the path does not exist. File-watcher unlink events on files we
// never indexed (concurrent-create-then-delete) MUST NOT error.
func TestIndexDeleteIdempotentOnMissingPath(t *testing.T) {
	db, _ := openTestIndex(t)
	if err := Delete(context.Background(), db, "/tmp/never-indexed.md"); err != nil {
		t.Fatalf("Delete on missing path: %v", err)
	}

	if err := Delete(context.Background(), db, "/tmp/never-indexed.md"); err != nil {
		t.Fatalf("Delete on missing path (second call): %v", err)
	}
}

// TestIndexInsertSQLDoesNotMentionExtensionHookColumns is the COMPILE-TIME
// half of inv-zen-130: the canonical INSERT statement string MUST NOT
// reference any of the three extension-hook column names. This is a
// source-level grep on the package-level constant. The companion
// runtime check is TestIndexExtensionHookColumnsNullByDefault.
func TestIndexInsertSQLDoesNotMentionExtensionHookColumns(t *testing.T) {
	for _, col := range []string{"audit_chain_anchor", "ecosystem_join_keys", "caronte_symbol_refs"} {
		if strings.Contains(indexInsertSQL, col) {
			t.Errorf("indexInsertSQL contains %q (inv-zen-130 violation: must not be in INSERT VALUES)", col)
		}
	}
}

func TestIndexRejectsEmptyFilePath(t *testing.T) {
	db, _ := openTestIndex(t)
	doc := Doc{
		FilePath:     "",
		FileType:     FileTypeMemory,
		LastModified: time.Now(),
		LastIndexed:  time.Now(),
	}
	err := IndexDoc(context.Background(), db, doc)
	if err == nil {
		t.Fatal("expected error on empty FilePath, got nil")
	}
	if !strings.Contains(err.Error(), "FilePath") {
		t.Errorf("error %q does not mention FilePath", err)
	}
}

func TestIndexRejectsEmptyFileType(t *testing.T) {
	db, _ := openTestIndex(t)
	doc := Doc{
		FilePath:     "/tmp/x.md",
		FileType:     "",
		LastModified: time.Now(),
		LastIndexed:  time.Now(),
	}
	err := IndexDoc(context.Background(), db, doc)
	if err == nil {
		t.Fatal("expected error on empty FileType, got nil")
	}
	if !strings.Contains(err.Error(), "FileType") {
		t.Errorf("error %q does not mention FileType", err)
	}
}

func TestDeleteRejectsEmptyFilePath(t *testing.T) {
	db, _ := openTestIndex(t)
	err := Delete(context.Background(), db, "")
	if err == nil {
		t.Fatal("expected error on empty filePath, got nil")
	}
	if !strings.Contains(err.Error(), "filePath") {
		t.Errorf("error %q does not mention filePath", err)
	}
}

func TestIndexFailsOnClosedDB(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "index.db")
	db, err := Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := Init(context.Background(), db); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	doc := Doc{
		FilePath:     "/tmp/x.md",
		FileType:     FileTypeMemory,
		LastModified: time.Now(),
		LastIndexed:  time.Now(),
	}
	err = IndexDoc(context.Background(), db, doc)
	if err == nil {
		t.Fatal("expected error on closed db, got nil")
	}
	if !strings.Contains(err.Error(), "knowledge:") {
		t.Errorf("error %q missing 'knowledge:' prefix", err)
	}
}

func TestDeleteFailsOnClosedDB(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "index.db")
	db, err := Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := Init(context.Background(), db); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	err = Delete(context.Background(), db, "/tmp/x.md")
	if err == nil {
		t.Fatal("expected error on closed db, got nil")
	}
	if !strings.Contains(err.Error(), "knowledge:") {
		t.Errorf("error %q missing 'knowledge:' prefix", err)
	}
}

func TestIndexCanceledContextRollsBack(t *testing.T) {
	db, _ := openTestIndex(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	doc := Doc{
		FilePath:     "/tmp/cancel.md",
		FileType:     FileTypeMemory,
		Title:        "x",
		ContentText:  "y",
		LastModified: time.Now(),
		LastIndexed:  time.Now(),
	}
	err := IndexDoc(ctx, db, doc)
	if err == nil {
		t.Fatal("expected error on canceled context, got nil")
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM knowledge_meta WHERE file_path = ?`, doc.FilePath).Scan(&count); err != nil {
		t.Fatalf("post-cancel count: %v", err)
	}
	if count != 0 {
		t.Errorf("partial state after cancel: count = %d, want 0", count)
	}
}

func TestIndexCheckConstraintErrorWrapped(t *testing.T) {
	db, _ := openTestIndex(t)
	doc := Doc{
		FilePath:     "/tmp/bad-ft.md",
		FileType:     FileType("not-a-real-type"),
		Title:        "x",
		ContentText:  "y",
		LastModified: time.Now(),
		LastIndexed:  time.Now(),
	}
	err := IndexDoc(context.Background(), db, doc)
	if err == nil {
		t.Fatal("expected CHECK constraint failure, got nil")
	}
	if !strings.Contains(err.Error(), "knowledge:") {
		t.Errorf("error %q missing 'knowledge:' prefix", err)
	}
	if !strings.Contains(err.Error(), "insert meta") {
		t.Errorf("error %q does not surface 'insert meta' wrap site", err)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM knowledge_meta WHERE file_path = ?`, doc.FilePath).Scan(&count); err != nil {
		t.Fatalf("post-fail count: %v", err)
	}
	if count != 0 {
		t.Errorf("after CHECK failure, count = %d, want 0 (rollback failed)", count)
	}
}

// TestIndexLookupFailureWraps reaches the `lookup rowid` non-ErrNoRows
// error branch. We DROP knowledge_meta after Init so the SELECT inside
// IndexDoc errors with "no such table" — distinct from sql.ErrNoRows. The
// wrap site (`knowledge: lookup rowid:`) MUST surface this driver error.
func TestIndexLookupFailureWraps(t *testing.T) {
	db, _ := openTestIndex(t)
	if _, err := db.Exec(`DROP TABLE knowledge_meta`); err != nil {
		t.Fatalf("DROP knowledge_meta: %v", err)
	}
	doc := Doc{
		FilePath: "/tmp/no-meta.md", FileType: FileTypeMemory,
		Title: "x", ContentText: "y",
		LastModified: time.Now(), LastIndexed: time.Now(),
	}
	err := IndexDoc(context.Background(), db, doc)
	if err == nil {
		t.Fatal("expected lookup failure with knowledge_meta dropped, got nil")
	}
	if !strings.Contains(err.Error(), "lookup rowid") {
		t.Errorf("error %q does not mention 'lookup rowid' wrap site", err)
	}
	if !strings.Contains(err.Error(), "knowledge:") {
		t.Errorf("error %q missing 'knowledge:' prefix", err)
	}
}

func TestIndexUpsertDeleteFTSFailureWraps(t *testing.T) {
	db, _ := openTestIndex(t)
	d := Doc{
		FilePath: "/tmp/upsert-fts-fail.md", FileType: FileTypeMemory,
		Title: "v1", ContentText: "first",
		LastModified: time.Now(), LastIndexed: time.Now(),
	}
	if err := IndexDoc(context.Background(), db, d); err != nil {
		t.Fatalf("first Index: %v", err)
	}

	if _, err := db.Exec(`DROP TABLE knowledge_fts`); err != nil {
		t.Fatalf("DROP knowledge_fts: %v", err)
	}
	d2 := d
	d2.Title = "v2"
	err := IndexDoc(context.Background(), db, d2)
	if err == nil {
		t.Fatal("expected upsert delete-fts failure with knowledge_fts dropped, got nil")
	}
	if !strings.Contains(err.Error(), "delete fts") {
		t.Errorf("error %q does not mention 'delete fts' wrap site", err)
	}
	if !strings.Contains(err.Error(), "knowledge:") {
		t.Errorf("error %q missing 'knowledge:' prefix", err)
	}
}

func TestDeleteLookupFailureWraps(t *testing.T) {
	db, _ := openTestIndex(t)
	if _, err := db.Exec(`DROP TABLE knowledge_meta`); err != nil {
		t.Fatalf("DROP knowledge_meta: %v", err)
	}
	err := Delete(context.Background(), db, "/tmp/anything.md")
	if err == nil {
		t.Fatal("expected lookup failure with knowledge_meta dropped, got nil")
	}
	if !strings.Contains(err.Error(), "lookup rowid") {
		t.Errorf("error %q does not mention 'lookup rowid' wrap site", err)
	}
	if !strings.Contains(err.Error(), "knowledge:") {
		t.Errorf("error %q missing 'knowledge:' prefix", err)
	}
}

// TestIndexUpsertDeleteMetaFailureWraps reaches the upsert
// `delete meta` error branch. After delete-fts succeeds (knowledge_fts
// intact), delete-meta hits a BEFORE DELETE trigger that ABORTs the
// statement. The wrap site (`knowledge: delete meta:`) MUST surface the
// SQLite "no delete allowed" raise.
func TestIndexUpsertDeleteMetaFailureWraps(t *testing.T) {
	db, _ := openTestIndex(t)
	d := Doc{
		FilePath: "/tmp/upsert-delmeta.md", FileType: FileTypeMemory,
		Title: "v1", ContentText: "first",
		LastModified: time.Now(), LastIndexed: time.Now(),
	}
	if err := IndexDoc(context.Background(), db, d); err != nil {
		t.Fatalf("first Index: %v", err)
	}

	if _, err := db.Exec(`
		CREATE TRIGGER abort_delete_meta
		BEFORE DELETE ON knowledge_meta
		BEGIN
		    SELECT RAISE(ABORT, 'no delete allowed');
		END;
	`); err != nil {
		t.Fatalf("CREATE TRIGGER: %v", err)
	}
	d2 := d
	d2.Title = "v2"
	err := IndexDoc(context.Background(), db, d2)
	if err == nil {
		t.Fatal("expected upsert delete-meta failure with trigger, got nil")
	}
	if !strings.Contains(err.Error(), "delete meta") {
		t.Errorf("error %q does not mention 'delete meta' wrap site", err)
	}
	if !strings.Contains(err.Error(), "knowledge:") {
		t.Errorf("error %q missing 'knowledge:' prefix", err)
	}
}

func TestDeleteMetaFailureWraps(t *testing.T) {
	db, _ := openTestIndex(t)
	d := Doc{
		FilePath: "/tmp/del-meta-fail.md", FileType: FileTypeMemory,
		Title: "x", ContentText: "y",
		LastModified: time.Now(), LastIndexed: time.Now(),
	}
	if err := IndexDoc(context.Background(), db, d); err != nil {
		t.Fatalf("seed IndexDoc: %v", err)
	}
	if _, err := db.Exec(`
		CREATE TRIGGER abort_delete_meta
		BEFORE DELETE ON knowledge_meta
		BEGIN
		    SELECT RAISE(ABORT, 'no delete allowed');
		END;
	`); err != nil {
		t.Fatalf("CREATE TRIGGER: %v", err)
	}
	err := Delete(context.Background(), db, d.FilePath)
	if err == nil {
		t.Fatal("expected delete-meta failure with trigger, got nil")
	}
	if !strings.Contains(err.Error(), "delete meta") {
		t.Errorf("error %q does not mention 'delete meta' wrap site", err)
	}
	if !strings.Contains(err.Error(), "knowledge:") {
		t.Errorf("error %q missing 'knowledge:' prefix", err)
	}
}

func TestDeleteFTSFailureWraps(t *testing.T) {
	db, _ := openTestIndex(t)
	d := Doc{
		FilePath: "/tmp/del-fts-fail.md", FileType: FileTypeMemory,
		Title: "x", ContentText: "y",
		LastModified: time.Now(), LastIndexed: time.Now(),
	}
	if err := IndexDoc(context.Background(), db, d); err != nil {
		t.Fatalf("seed IndexDoc: %v", err)
	}
	if _, err := db.Exec(`DROP TABLE knowledge_fts`); err != nil {
		t.Fatalf("DROP knowledge_fts: %v", err)
	}
	err := Delete(context.Background(), db, d.FilePath)
	if err == nil {
		t.Fatal("expected delete-fts failure with knowledge_fts dropped, got nil")
	}
	if !strings.Contains(err.Error(), "delete fts") {
		t.Errorf("error %q does not mention 'delete fts' wrap site", err)
	}
	if !strings.Contains(err.Error(), "knowledge:") {
		t.Errorf("error %q missing 'knowledge:' prefix", err)
	}
}

// TestIndexFTS5RowidConflictWraps asserts the FTS5-INSERT error branch is
// reachable: a phantom FTS5 row at the rowid SQLite is about to assign
// triggers SQLite's "constraint failed" on the FTS5 INSERT. The wrap site
// (`knowledge: insert fts:`) MUST surface the underlying driver error and
// the rolled-back transaction MUST leave knowledge_meta clean (the meta
// row was inserted earlier in the same tx so rollback drops it).
//
// We force the rowid alignment by pre-seeding meta at a high rowid (so
// the next autoincrement value is N+1) and FTS5 at N+1 (the value the
// canonical INSERT will use). The IndexDoc call then collides on FTS5 PK.
func TestIndexFTS5RowidConflictWraps(t *testing.T) {
	db, _ := openTestIndex(t)

	if _, err := db.Exec(`INSERT INTO knowledge_meta(rowid, file_path, file_type, last_modified, last_indexed) VALUES (5000, '/seed-conflict', 'memory', 0, 0)`); err != nil {
		t.Fatalf("seed meta: %v", err)
	}

	if _, err := db.Exec(`INSERT INTO knowledge_fts(rowid, content_text) VALUES (5001, 'phantom')`); err != nil {
		t.Fatalf("seed fts: %v", err)
	}
	doc := Doc{
		FilePath: "/tmp/conflict.md", FileType: FileTypeMemory,
		Title: "x", ContentText: "y",
		LastModified: time.Now(), LastIndexed: time.Now(),
	}
	err := IndexDoc(context.Background(), db, doc)
	if err == nil {
		t.Fatal("expected FTS5 PK conflict, got nil")
	}
	if !strings.Contains(err.Error(), "insert fts") {
		t.Errorf("error %q does not mention 'insert fts' wrap site", err)
	}
	if !strings.Contains(err.Error(), "knowledge:") {
		t.Errorf("error %q missing 'knowledge:' prefix", err)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM knowledge_meta WHERE file_path = ?`, doc.FilePath).Scan(&count); err != nil {
		t.Fatalf("post-fail count: %v", err)
	}
	if count != 0 {
		t.Errorf("after FTS5 conflict, meta rows for %q = %d, want 0 (rollback failed)", doc.FilePath, count)
	}
}

func TestDeleteCanceledContextRollsBack(t *testing.T) {
	db, _ := openTestIndex(t)

	d := Doc{
		FilePath:     "/tmp/seed-cancel.md",
		FileType:     FileTypeMemory,
		Title:        "x",
		ContentText:  "y",
		LastModified: time.Now(),
		LastIndexed:  time.Now(),
	}
	if err := IndexDoc(context.Background(), db, d); err != nil {
		t.Fatalf("seed IndexDoc: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := Delete(ctx, db, d.FilePath)
	if err == nil {
		t.Fatal("expected error on canceled Delete, got nil")
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM knowledge_meta WHERE file_path = ?`, d.FilePath).Scan(&count); err != nil {
		t.Fatalf("post-cancel count: %v", err)
	}
	if count != 1 {
		t.Errorf("after canceled Delete, count = %d, want 1 (row dropped despite rollback)", count)
	}
}

func TestIndexErrorsAreSentinelChainable(t *testing.T) {
	db, _ := openTestIndex(t)
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	doc := Doc{
		FilePath:     "/tmp/x.md",
		FileType:     FileTypeMemory,
		LastModified: time.Now(),
		LastIndexed:  time.Now(),
	}
	err := IndexDoc(context.Background(), db, doc)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if errors.Unwrap(err) == nil {
		t.Errorf("error %v has no Unwrap target (wrap chain broken)", err)
	}
}

func tableColumns(t *testing.T, db *sql.DB, table string) map[string]bool {
	t.Helper()
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		t.Fatalf("tableColumns: PRAGMA table_info(%s): %v", table, err)
	}
	defer rows.Close()
	cols := make(map[string]bool)
	for rows.Next() {
		var cid int
		var name, ttype string
		var notnull int
		var dfltValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &ttype, &notnull, &dfltValue, &pk); err != nil {
			t.Fatalf("tableColumns: Scan: %v", err)
		}
		cols[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("tableColumns: rows.Err: %v", err)
	}
	return cols
}

func keys(m map[string]bool) []string {
	result := make([]string, 0, len(m))
	for k := range m {
		result = append(result, k)
	}
	return result
}

func TestKnowledgeMetaHasCaronteSymbolRefsColumn(t *testing.T) {
	db, _ := openTestIndex(t)

	cols := tableColumns(t, db, "knowledge_meta")
	if !cols["caronte_symbol_refs"] {
		t.Errorf("knowledge_meta missing caronte_symbol_refs column; got %v", keys(cols))
	}
	if cols["gitnexus_symbol_refs"] {
		t.Errorf("knowledge_meta still has gitnexus_symbol_refs; Plan 19 renames it")
	}
}
