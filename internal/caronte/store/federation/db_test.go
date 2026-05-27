// go:build cgo
package federation

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func openTestDB(t *testing.T) *WorkspaceFederationDB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "zen-swarm", "workspace.db")
	db, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open(%s): %v", path, err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestOpenRejectsEmptyStatePath(t *testing.T) {
	_, err := Open(context.Background(), "")
	if err == nil {
		t.Fatal("Open(\"\") returned nil err; want ErrEmptyStatePath")
	}
	if !errors.Is(err, ErrEmptyStatePath) {
		t.Errorf("Open(\"\") err = %v; want ErrEmptyStatePath", err)
	}
}

func TestOpenCreatesParentDir(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "deep", "nested", "workspace.db")
	db, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		t.Errorf("parent dir not created: %v", err)
	}
}

func TestInitMaterializesC2Schema(t *testing.T) {
	db := openTestDB(t)
	tables := []string{
		"caronte_workspaces", "caronte_workspace_members",
		"contract_links", "breaking_changes", "breaking_change_consumers",
	}
	for _, name := range tables {
		var got string
		err := db.DB().QueryRow(
			`SELECT name FROM sqlite_master WHERE name = ? AND type = 'table'`, name,
		).Scan(&got)
		if err != nil {
			t.Errorf("table %s not materialized: %v", name, err)
		}
	}
	indexes := []string{
		"idx_contract_links_endpoint", "idx_contract_links_call",
		"idx_breaking_changes_endpoint", "idx_break_consumers_call",
	}
	for _, name := range indexes {
		var got string
		err := db.DB().QueryRow(
			`SELECT name FROM sqlite_master WHERE name = ? AND type = 'index'`, name,
		).Scan(&got)
		if err != nil {
			t.Errorf("index %s not materialized: %v", name, err)
		}
	}
}

func TestInitIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "zen-swarm", "workspace.db")
	for i := 0; i < 3; i++ {
		db, err := Open(context.Background(), path)
		if err != nil {
			t.Fatalf("Open[%d]: %v (re-init must be idempotent)", i, err)
		}
		if err := db.Close(); err != nil {
			t.Fatalf("Close[%d]: %v", i, err)
		}
	}
}

func TestSingleWriterWAL(t *testing.T) {
	db := openTestDB(t)
	var mode string
	if err := db.DB().QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatalf("PRAGMA journal_mode: %v", err)
	}
	if !strings.EqualFold(mode, "wal") {
		t.Errorf("journal_mode = %q; want wal", mode)
	}
}

func TestForeignKeysEnabled(t *testing.T) {
	db := openTestDB(t)
	var fk int
	if err := db.DB().QueryRow(`PRAGMA foreign_keys`).Scan(&fk); err != nil {
		t.Fatalf("PRAGMA foreign_keys: %v", err)
	}
	if fk != 1 {
		t.Errorf("PRAGMA foreign_keys = %d; want 1 (CASCADE enforcement requires it)", fk)
	}
}

func TestDBSurfaceNotNil(t *testing.T) {
	db := openTestDB(t)
	if db.DB() == nil {
		t.Fatal("WorkspaceFederationDB.DB() = nil; want a live handle")
	}
	if err := db.DB().Ping(); err != nil {
		t.Errorf("DB().Ping(): %v", err)
	}
}

func TestCloseIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "zen-swarm", "workspace.db")
	db, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Errorf("Close first call: %v", err)
	}

	_ = db.Close()
}

func TestBoundarySentinelReachable(t *testing.T) {
	if err := federationBoundarySentinel(); err != nil {
		t.Errorf("federationBoundarySentinel() = %v; want nil", err)
	}
}

var _ = func() *sql.DB { var d *WorkspaceFederationDB; return d.DB() }
