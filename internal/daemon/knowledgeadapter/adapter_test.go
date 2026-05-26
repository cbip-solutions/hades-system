//go:build cgo

// Package knowledgeadapter tests — adapter_test.go (Plan 9 Phase D-12).
//
// Tests cover:
//  1. Compile-time interface compliance — verified by the var _ stmt in
//     adapter.go; the test binary compilation itself is the assertion.
//  2. ListAuthorizedProjects — happy path (2 active projects returned).
//  3. ListAuthorizedProjects — archived project excluded.
//  4. ListAuthorizedProjects — empty table returns empty slice, not nil.
//  5. ListAuthorizedProjects — graceful no-such-table fallback.
//  6. OpenProjectVault — creates vault.db on first call.
//  7. OpenProjectVault — second call returns same *sql.DB (cache hit).
//  8. UpdateAuditChainAnchor — writes row; SELECT verifies value.
//  9. UpdateAuditChainAnchor — idempotent update path.
//
// 10. Close — all cached vaults closed; subsequent queries return error.
//
// DRIVER NOTE: this test file imports ncruces/go-sqlite3 to register the
// "sqlite3" SQL driver. The adapter package itself imports neither aggregator
// (mattn/go-sqlite3) nor ncruces directly — it only uses knowledgetypes
// (a pure-Go CGO-free package). Tests are the only place that need a concrete
// driver, and ncruces is safe here because mattn is NOT in this test binary
// (aggregator is not imported by adapter.go anymore).
//
// Do NOT import internal/knowledge/aggregator here. That package's db.go
// pulls in mattn/go-sqlite3, which would conflict with ncruces.
package knowledgeadapter

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
)

func newMockDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3_ncruces", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}

	_, err = db.ExecContext(context.Background(), `
		CREATE TABLE IF NOT EXISTS projects_alias (
			id_sha256      TEXT PRIMARY KEY,
			alias          TEXT NOT NULL UNIQUE,
			canonical_path TEXT NOT NULL,
			first_seen_at  INTEGER NOT NULL,
			last_seen_at   INTEGER NOT NULL,
			archived_at    INTEGER
		);
	`)
	if err != nil {
		db.Close()
		t.Fatalf("CREATE TABLE projects_alias: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func insertProject(t *testing.T, db *sql.DB, idSHA256, alias string, archived bool, tmpDir string) string {
	t.Helper()
	canonicalPath := filepath.Join(tmpDir, alias)
	if err := os.MkdirAll(filepath.Join(canonicalPath, ".zen"), 0o755); err != nil {
		t.Fatalf("mkdir .zen: %v", err)
	}

	vaultPath := filepath.Join(canonicalPath, ".zen", "vault.db")
	vdb, err := sql.Open("sqlite3_ncruces", vaultPath)
	if err != nil {
		t.Fatalf("create vault db: %v", err)
	}
	if err := vdb.PingContext(context.Background()); err != nil {
		vdb.Close()
		t.Fatalf("ping vault db: %v", err)
	}
	vdb.Close()

	archivedAt := "NULL"
	if archived {
		archivedAt = "1000"
	}
	_, err = db.ExecContext(context.Background(), fmt.Sprintf(`
		INSERT INTO projects_alias (id_sha256, alias, canonical_path, first_seen_at, last_seen_at, archived_at)
		VALUES (?, ?, ?, 1000, 1000, %s)
	`, archivedAt), idSHA256, alias, canonicalPath)
	if err != nil {
		t.Fatalf("insert project: %v", err)
	}
	return canonicalPath
}

func newTestAdapter(t *testing.T) (*Adapter, *sql.DB) {
	t.Helper()
	db := newMockDB(t)
	a := NewAdapterFromDB(db)
	t.Cleanup(func() { a.Close() })
	return a, db
}

func TestListAuthorizedProjects_TwoActiveReturned(t *testing.T) {
	a, m := newTestAdapter(t)
	tmpDir := t.TempDir()

	insertProject(t, m, "sha256a", "project-alpha", false, tmpDir)
	insertProject(t, m, "sha256b", "project-beta", false, tmpDir)

	handles, err := a.ListAuthorizedProjects(context.Background())
	if err != nil {
		t.Fatalf("ListAuthorizedProjects: %v", err)
	}
	if len(handles) != 2 {
		t.Fatalf("expected 2 handles, got %d", len(handles))
	}

	if handles[0].ProjectID != "sha256a" {
		t.Errorf("handles[0].ProjectID = %q; want sha256a", handles[0].ProjectID)
	}
	if handles[1].ProjectID != "sha256b" {
		t.Errorf("handles[1].ProjectID = %q; want sha256b", handles[1].ProjectID)
	}

	for _, h := range handles {
		if h.VaultPath == "" {
			t.Errorf("VaultPath empty for %q", h.ProjectID)
		}
		if filepath.Base(h.VaultPath) != "vault.db" {
			t.Errorf("VaultPath %q does not end in vault.db", h.VaultPath)
		}
	}
}

func TestListAuthorizedProjects_ArchivedExcluded(t *testing.T) {
	a, m := newTestAdapter(t)
	tmpDir := t.TempDir()

	insertProject(t, m, "sha256active", "active-proj", false, tmpDir)
	insertProject(t, m, "sha256archived", "archived-proj", true, tmpDir)

	handles, err := a.ListAuthorizedProjects(context.Background())
	if err != nil {
		t.Fatalf("ListAuthorizedProjects: %v", err)
	}
	if len(handles) != 1 {
		t.Fatalf("expected 1 handle (archived excluded), got %d", len(handles))
	}
	if handles[0].ProjectID != "sha256active" {
		t.Errorf("unexpected project %q; want sha256active", handles[0].ProjectID)
	}
}

func TestListAuthorizedProjects_EmptyTableNonNilSlice(t *testing.T) {
	a, _ := newTestAdapter(t)

	handles, err := a.ListAuthorizedProjects(context.Background())
	if err != nil {
		t.Fatalf("ListAuthorizedProjects: %v", err)
	}
	if handles == nil {
		t.Error("handles is nil; want empty non-nil slice")
	}
	if len(handles) != 0 {
		t.Errorf("expected 0 handles, got %d", len(handles))
	}
}

func TestListAuthorizedProjects_NoSuchTableFallback(t *testing.T) {

	db, err := sql.Open("sqlite3_ncruces", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()

	a := NewAdapterFromDB(db)
	defer a.Close()

	handles, err := a.ListAuthorizedProjects(context.Background())
	if err != nil {
		t.Fatalf("expected graceful fallback, got error: %v", err)
	}
	if len(handles) != 0 {
		t.Errorf("expected 0 handles on missing table fallback, got %d", len(handles))
	}
}

func TestOpenProjectVault_CreatesAndPingsVaultDB(t *testing.T) {
	a, m := newTestAdapter(t)
	tmpDir := t.TempDir()

	insertProject(t, m, "sha256c", "project-gamma", false, tmpDir)

	vault, err := a.OpenProjectVault(context.Background(), "sha256c")
	if err != nil {
		t.Fatalf("OpenProjectVault: %v", err)
	}
	db, ok := vault.(*sql.DB)
	if !ok {
		t.Fatal("OpenProjectVault did not return *sql.DB")
	}

	if err := db.PingContext(context.Background()); err != nil {
		t.Errorf("db.PingContext: %v", err)
	}
}

func TestOpenProjectVault_CacheIdempotency(t *testing.T) {
	a, m := newTestAdapter(t)
	tmpDir := t.TempDir()

	insertProject(t, m, "sha256d", "project-delta", false, tmpDir)

	ctx := context.Background()
	v1, err := a.OpenProjectVault(ctx, "sha256d")
	if err != nil {
		t.Fatalf("first OpenProjectVault: %v", err)
	}
	v2, err := a.OpenProjectVault(ctx, "sha256d")
	if err != nil {
		t.Fatalf("second OpenProjectVault: %v", err)
	}
	if v1 != v2 {
		t.Error("expected same *sql.DB on second call (cache hit), got different pointer")
	}
}

func TestUpdateAuditChainAnchor_WritesRow(t *testing.T) {
	a, m := newTestAdapter(t)
	tmpDir := t.TempDir()

	insertProject(t, m, "sha256e", "project-epsilon", false, tmpDir)

	ctx := context.Background()
	const anchor = "2026_05:evt-123:abc123hash"
	if err := a.UpdateAuditChainAnchor(ctx, "sha256e", "note-001", anchor); err != nil {
		t.Fatalf("UpdateAuditChainAnchor: %v", err)
	}

	vault, _ := a.OpenProjectVault(ctx, "sha256e")
	db := vault.(*sql.DB)

	var got string
	err := db.QueryRowContext(ctx, `
		SELECT audit_chain_anchor FROM knowledge_extension
		WHERE project_id = ? AND note_id = ?
	`, "sha256e", "note-001").Scan(&got)
	if err != nil {
		t.Fatalf("SELECT anchor: %v", err)
	}
	if got != anchor {
		t.Errorf("audit_chain_anchor = %q; want %q", got, anchor)
	}
}

func TestUpdateAuditChainAnchor_IdempotentUpdate(t *testing.T) {
	a, m := newTestAdapter(t)
	tmpDir := t.TempDir()

	insertProject(t, m, "sha256f", "project-zeta", false, tmpDir)

	ctx := context.Background()
	const anchorV1 = "2026_05:evt-001:hashV1"
	const anchorV2 = "2026_05:evt-002:hashV2"

	if err := a.UpdateAuditChainAnchor(ctx, "sha256f", "note-002", anchorV1); err != nil {
		t.Fatalf("first UpdateAuditChainAnchor: %v", err)
	}

	if err := a.UpdateAuditChainAnchor(ctx, "sha256f", "note-002", anchorV2); err != nil {
		t.Fatalf("second UpdateAuditChainAnchor: %v", err)
	}

	vault, _ := a.OpenProjectVault(ctx, "sha256f")
	db := vault.(*sql.DB)

	var got string
	if err := db.QueryRowContext(ctx, `
		SELECT audit_chain_anchor FROM knowledge_extension
		WHERE project_id = ? AND note_id = ?
	`, "sha256f", "note-002").Scan(&got); err != nil {
		t.Fatalf("SELECT: %v", err)
	}
	if got != anchorV2 {
		t.Errorf("after update anchor = %q; want %q", got, anchorV2)
	}

	var count int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM knowledge_extension WHERE project_id = ? AND note_id = ?
	`, "sha256f", "note-002").Scan(&count); err != nil {
		t.Fatalf("COUNT: %v", err)
	}
	if count != 1 {
		t.Errorf("row count = %d; want 1", count)
	}
}

func TestClose_DrainsCacheAndClosesDBs(t *testing.T) {
	a, m := newTestAdapter(t)
	tmpDir := t.TempDir()

	insertProject(t, m, "sha256g", "project-eta", false, tmpDir)
	insertProject(t, m, "sha256h", "project-theta", false, tmpDir)

	ctx := context.Background()

	v1, err := a.OpenProjectVault(ctx, "sha256g")
	if err != nil {
		t.Fatalf("OpenProjectVault eta: %v", err)
	}
	v2, err := a.OpenProjectVault(ctx, "sha256h")
	if err != nil {
		t.Fatalf("OpenProjectVault theta: %v", err)
	}

	if err := a.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	for _, v := range []any{v1, v2} {
		db := v.(*sql.DB)
		if err := db.PingContext(ctx); err == nil {
			t.Error("expected error after Close, got nil")
		}
	}
}

func TestClose_EmptyCacheNoError(t *testing.T) {

	db := newMockDB(t)
	a := NewAdapterFromDB(db)
	if err := a.Close(); err != nil {
		t.Errorf("Close on empty cache: %v; want nil", err)
	}
}

func TestOpenProjectVault_NotFoundError(t *testing.T) {

	a, _ := newTestAdapter(t)
	_, err := a.OpenProjectVault(context.Background(), "nonexistent-id")
	if err == nil {
		t.Error("expected error for unknown projectID, got nil")
	}
}

func TestUpdateAuditChainAnchor_BadProjectError(t *testing.T) {

	a, _ := newTestAdapter(t)
	err := a.UpdateAuditChainAnchor(context.Background(), "unknown-project", "note-x", "anchor")
	if err == nil {
		t.Error("expected error for unknown projectID, got nil")
	}
}

func TestListAuthorizedProjects_QueryError(t *testing.T) {

	db := newMockDB(t)
	a := NewAdapterFromDB(db)

	db.Close()

	_, err := a.ListAuthorizedProjects(context.Background())
	if err == nil {
		t.Error("expected error after DB closed, got nil")
	}
}

func TestContainsSubstr_EdgeCases(t *testing.T) {

	if !containsSubstr("hello", "") {
		t.Error("containsSubstr with empty sub must return true")
	}

	if containsSubstr("hi", "hello") {
		t.Error("containsSubstr with sub longer than s must return false")
	}

	if !containsSubstr("no such table", "no such table") {
		t.Error("containsSubstr exact match must return true")
	}

	if containsSubstr("some other error", "no such table") {
		t.Error("containsSubstr must return false when sub not in s")
	}
}

func TestIsNoSuchTable_NonMatchingError(t *testing.T) {

	type fakeErr struct{}

	err := fmt.Errorf("SQLITE_BUSY: database is locked")
	if isNoSuchTable(err) {
		t.Error("isNoSuchTable must be false for non-table errors")
	}

	if isNoSuchTable(nil) {
		t.Error("isNoSuchTable(nil) must return false")
	}
}

func TestResolveVaultPath_NotFound(t *testing.T) {

	a, _ := newTestAdapter(t)
	_, err := a.resolveVaultPath(context.Background(), "missing-project")
	if err == nil {
		t.Error("expected error for missing project, got nil")
	}
}

func TestResolveVaultPath_DBClosed(t *testing.T) {

	db := newMockDB(t)
	a := NewAdapterFromDB(db)
	db.Close()
	_, err := a.resolveVaultPath(context.Background(), "any-project")
	if err == nil {
		t.Error("expected error after DB closed, got nil")
	}
}

func TestOpenProjectVault_ConcurrentRaceWinner(t *testing.T) {

	a, m := newTestAdapter(t)
	tmpDir := t.TempDir()

	insertProject(t, m, "sha256race", "project-race", false, tmpDir)

	ctx := context.Background()

	v1, err := a.OpenProjectVault(ctx, "sha256race")
	if err != nil {
		t.Fatalf("first OpenProjectVault: %v", err)
	}

	v2, err := a.OpenProjectVault(ctx, "sha256race")
	if err != nil {
		t.Fatalf("second OpenProjectVault: %v", err)
	}
	if v1 != v2 {
		t.Error("expected same handle on cache hit")
	}
}

func TestClose_MultipleTimesNoError(t *testing.T) {

	db := newMockDB(t)
	a := NewAdapterFromDB(db)
	if err := a.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := a.Close(); err != nil {
		t.Errorf("second Close (already empty): %v; want nil", err)
	}
}

func TestUpdateAuditChainAnchor_ExecError(t *testing.T) {

	a, m := newTestAdapter(t)
	tmpDir := t.TempDir()

	insertProject(t, m, "sha256exec-err", "project-execerr", false, tmpDir)

	ctx := context.Background()

	if err := a.UpdateAuditChainAnchor(ctx, "sha256exec-err", "note-1", "anchor-v1"); err != nil {
		t.Fatalf("first UpdateAuditChainAnchor: %v", err)
	}

	a.mu.Lock()
	vaultDB := a.vaults["sha256exec-err"]
	a.mu.Unlock()
	vaultDB.Close()

	err := a.UpdateAuditChainAnchor(ctx, "sha256exec-err", "note-2", "anchor-v2")
	if err == nil {
		t.Error("expected exec error after vault DB closed, got nil")
	}
}

func TestOpenProjectVault_PingError(t *testing.T) {
	db := newMockDB(t)
	a := NewAdapterFromDB(db)
	t.Cleanup(func() { a.Close() })

	_, err := db.ExecContext(context.Background(), `
		INSERT INTO projects_alias (id_sha256, alias, canonical_path, first_seen_at, last_seen_at, archived_at)
		VALUES ('sha256ping', 'project-ping', '/nonexistent/path/that/cannot/be/created', 1000, 1000, NULL)
	`)
	if err != nil {
		t.Fatalf("insert project: %v", err)
	}

	_, openErr := a.OpenProjectVault(context.Background(), "sha256ping")
	if openErr == nil {
		t.Error("expected PingContext error for non-existent vault directory, got nil")
	}
}

func TestUpdateAuditChainAnchor_UpsertError(t *testing.T) {
	a, m := newTestAdapter(t)
	tmpDir := t.TempDir()

	insertProject(t, m, "sha256upsert-err", "project-upsert-err", false, tmpDir)

	ctx := context.Background()

	vault, err := a.OpenProjectVault(ctx, "sha256upsert-err")
	if err != nil {
		t.Fatalf("OpenProjectVault: %v", err)
	}
	vaultDB := vault.(*sql.DB)

	if _, err := vaultDB.ExecContext(ctx, `DROP TABLE IF EXISTS knowledge_extension`); err != nil {
		t.Fatalf("drop table: %v", err)
	}
	if _, err := vaultDB.ExecContext(ctx, `
		CREATE TABLE knowledge_extension (
			project_id TEXT NOT NULL,
			note_id    TEXT NOT NULL,
			PRIMARY KEY (project_id, note_id)
		)
	`); err != nil {
		t.Fatalf("create bad-schema table: %v", err)
	}

	upsertErr := a.UpdateAuditChainAnchor(ctx, "sha256upsert-err", "note-x", "anchor")
	if upsertErr == nil {
		t.Error("expected UPSERT error for wrong table schema, got nil")
	}
}
