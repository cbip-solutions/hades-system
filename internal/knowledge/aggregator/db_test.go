// go:build cgo
//go:build cgo
// +build cgo

package aggregator

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func openBlocker(path string) (*os.File, error) {
	return os.Create(path)
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "aggregator.db")
	db, err := Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := Init(context.Background(), db); err != nil {
		_ = db.Close()
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestInitCreatesPinIndexTable(t *testing.T) {
	db := openTestDB(t)
	rows, err := db.Query(`PRAGMA table_info(knowledge_pin_index)`)
	if err != nil {
		t.Fatalf("PRAGMA table_info: %v", err)
	}
	defer rows.Close()
	want := map[string]bool{
		"note_id": false, "project_id": false, "title": false,
		"content": false, "frontmatter_json": false, "promoted_at": false,
		"promoted_by": false, "promote_reason": false, "audit_chain_anchor": false,
		"embedding": false,
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
			want[name] = true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err: %v", err)
	}
	for col, found := range want {
		if !found {
			t.Errorf("knowledge_pin_index missing column %s", col)
		}
	}
}

func TestInitCreatesFTSVirtualTable(t *testing.T) {
	db := openTestDB(t)
	var name, ddl string
	err := db.QueryRow(`
		SELECT name, sql FROM sqlite_master
		WHERE name = 'knowledge_pin_fts' AND type = 'table'
	`).Scan(&name, &ddl)
	if err != nil {
		t.Fatalf("knowledge_pin_fts not created: %v", err)
	}
	if !strings.Contains(ddl, "fts5") {
		t.Errorf("knowledge_pin_fts not declared as fts5: %s", ddl)
	}
	if !strings.Contains(ddl, "content='knowledge_pin_index'") {
		t.Errorf("knowledge_pin_fts not external-content with knowledge_pin_index: %s", ddl)
	}
}

func TestInitCreatesVecVirtualTable(t *testing.T) {
	db := openTestDB(t)
	var name, ddl string
	err := db.QueryRow(`
		SELECT name, sql FROM sqlite_master
		WHERE name = 'knowledge_pin_vec' AND type = 'table'
	`).Scan(&name, &ddl)
	if err != nil {
		t.Fatalf("knowledge_pin_vec not created: %v", err)
	}
	if !strings.Contains(ddl, "vec0") {
		t.Errorf("knowledge_pin_vec not declared as vec0: %s", ddl)
	}
	if !strings.Contains(ddl, "float[384]") {
		t.Errorf("knowledge_pin_vec not 384-dim: %s", ddl)
	}
}

func TestInitCreatesWikilinksTable(t *testing.T) {
	db := openTestDB(t)
	var count int
	err := db.QueryRow(`
		SELECT COUNT(*) FROM sqlite_master
		WHERE name = 'knowledge_pin_wikilinks' AND type = 'table'
	`).Scan(&count)
	if err != nil {
		t.Fatalf("query master (table): %v", err)
	}
	if count != 1 {
		t.Errorf("knowledge_pin_wikilinks not created (count=%d)", count)
	}
	err = db.QueryRow(`
		SELECT COUNT(*) FROM sqlite_master
		WHERE name = 'idx_pin_wikilinks_target' AND type = 'index'
	`).Scan(&count)
	if err != nil {
		t.Fatalf("query master (index): %v", err)
	}
	if count != 1 {
		t.Errorf("idx_pin_wikilinks_target not created")
	}
}

func TestInitCreatesProjectAndAnchorIndexes(t *testing.T) {
	db := openTestDB(t)
	for _, want := range []string{
		"idx_pin_index_project",
		"idx_pin_index_anchor",
	} {
		var count int
		if err := db.QueryRow(`
			SELECT COUNT(*) FROM sqlite_master
			WHERE name = ? AND type = 'index'
		`, want).Scan(&count); err != nil {
			t.Fatalf("query master (%s): %v", want, err)
		}
		if count != 1 {
			t.Errorf("%s not created", want)
		}
	}
}

func TestInitIdempotent(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "aggregator.db")
	for i := 0; i < 3; i++ {
		db, err := Open(context.Background(), dbPath)
		if err != nil {
			t.Fatalf("Open[%d]: %v", i, err)
		}
		if err := Init(context.Background(), db); err != nil {
			_ = db.Close()
			t.Fatalf("Init[%d]: %v (re-init should be idempotent)", i, err)
		}
		if err := db.Close(); err != nil {
			t.Fatalf("Close[%d]: %v", i, err)
		}
	}
}

func TestInitAppliesPragmas(t *testing.T) {
	db := openTestDB(t)
	tests := []struct {
		pragma string
		want   string
	}{
		{"journal_mode", "wal"},
		{"foreign_keys", "1"},
		{"synchronous", "1"},
	}
	for _, tt := range tests {
		var got string
		err := db.QueryRow("PRAGMA " + tt.pragma).Scan(&got)
		if err != nil {
			t.Fatalf("PRAGMA %s: %v", tt.pragma, err)
		}
		if !strings.EqualFold(got, tt.want) {
			t.Errorf("PRAGMA %s = %s; want %s", tt.pragma, got, tt.want)
		}
	}
}

func TestInitBusyTimeoutAndTempStore(t *testing.T) {
	db := openTestDB(t)
	var bt int
	if err := db.QueryRow(`PRAGMA busy_timeout`).Scan(&bt); err != nil {
		t.Fatalf("PRAGMA busy_timeout: %v", err)
	}
	if bt != 5000 {
		t.Errorf("busy_timeout = %d; want 5000", bt)
	}
	var ts int
	if err := db.QueryRow(`PRAGMA temp_store`).Scan(&ts); err != nil {
		t.Fatalf("PRAGMA temp_store: %v", err)
	}

	if ts != 2 {
		t.Errorf("temp_store = %d; want 2 (MEMORY)", ts)
	}
}

func TestInitPromoteReasonCheckRejectsEmpty(t *testing.T) {
	db := openTestDB(t)
	_, err := db.Exec(`
		INSERT INTO knowledge_pin_index
		(note_id, project_id, title, content, frontmatter_json,
		 promoted_at, promoted_by, promote_reason, audit_chain_anchor, embedding)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		"n1", "p1", "Title", "Body", "{}",
		"2026-05-08 12:00:00", "testuser", "", "anchor", nil)
	if err == nil {
		t.Errorf("INSERT with empty promote_reason succeeded; CHECK constraint missing")
	} else if !strings.Contains(err.Error(), "CHECK") {
		t.Errorf("expected CHECK constraint error, got: %v", err)
	}
}

func TestInitWikilinksLinkTypeCheckRejectsBogus(t *testing.T) {
	db := openTestDB(t)
	_, err := db.Exec(`
		INSERT INTO knowledge_pin_wikilinks
		(source_note_id, target_note_id, link_type) VALUES (?,?,?)`,
		"n1", "n2", "bogus")
	if err == nil {
		t.Errorf("INSERT with bogus link_type succeeded; CHECK constraint missing")
	} else if !strings.Contains(err.Error(), "CHECK") {
		t.Errorf("expected CHECK constraint error, got: %v", err)
	}
}

func TestOpenRejectsEmptyPath(t *testing.T) {
	_, err := Open(context.Background(), "")
	if err == nil {
		t.Error("Open accepted empty dbPath; expected error")
	}
}

func TestOpenCreatesParentDir(t *testing.T) {
	tempDir := t.TempDir()
	nested := filepath.Join(tempDir, "a", "b", "c", "aggregator.db")
	db, err := Open(context.Background(), nested)
	if err != nil {
		t.Fatalf("Open with nested path: %v", err)
	}
	defer db.Close()
}

func TestOpenFailsOnUnwritableParent(t *testing.T) {
	tempDir := t.TempDir()

	blocker := filepath.Join(tempDir, "blocker")
	f, err := openBlocker(blocker)
	if err != nil {
		t.Fatalf("create blocker: %v", err)
	}
	_ = f.Close()

	bad := filepath.Join(blocker, "aggregator.db")
	_, err = Open(context.Background(), bad)
	if err == nil {
		t.Fatalf("Open succeeded on bad parent path %q; expected mkdir failure", bad)
	}
	if !strings.Contains(err.Error(), "mkdir parent") {
		t.Errorf("error did not mention mkdir parent: %v", err)
	}
}

func TestOpenFailsOnDirectoryAsTarget(t *testing.T) {
	tempDir := t.TempDir()

	_, err := Open(context.Background(), tempDir)
	if err == nil {
		t.Fatalf("Open succeeded on directory path; expected ping failure")
	}

}
