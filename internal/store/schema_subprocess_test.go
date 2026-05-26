package store

import (
	"path/filepath"
	"testing"
)

func TestMigration048SubprocessSessionsTable(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "subprocess.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	var v int
	if err := s.DB().QueryRow(`SELECT MAX(version) FROM schema_version`).Scan(&v); err != nil {
		t.Fatalf("read schema_version: %v", err)
	}
	if v < 14 {
		t.Errorf("schemaVersion = %d, want >= 14", v)
	}

	rows, err := s.DB().Query(`PRAGMA table_info(subprocess_sessions)`)
	if err != nil {
		t.Fatalf("PRAGMA: %v", err)
	}
	defer rows.Close()
	cols := map[string]bool{}
	for rows.Next() {
		var (
			cid       int
			name      string
			ctype     string
			notnull   int
			dfltValue any
			pk        int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			t.Fatalf("scan: %v", err)
		}
		cols[name] = true
	}
	for _, want := range []string{
		"spec_id", "doctrine_name", "thread_id", "worktree", "project_id",
		"pid", "started_at", "last_use_at", "ttl_seconds",
	} {
		if !cols[want] {
			t.Errorf("subprocess_sessions missing column %q", want)
		}
	}
}

func TestMigration048CompositePrimaryKey(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "subprocess-pk.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	if _, err := s.DB().Exec(
		`INSERT INTO subprocess_sessions
		 (spec_id, doctrine_name, thread_id, worktree, project_id, pid, started_at, last_use_at, ttl_seconds)
		 VALUES ('s1', 'default', 'tid-1', '/tmp', 'p1', 1234, 1, 1, 3600)`,
	); err != nil {
		t.Fatalf("first insert: %v", err)
	}

	if _, err := s.DB().Exec(
		`INSERT INTO subprocess_sessions
		 (spec_id, doctrine_name, thread_id, worktree, project_id, pid, started_at, last_use_at, ttl_seconds)
		 VALUES ('s1', 'default', 'tid-2', '/tmp', 'p1', 5678, 2, 2, 3600)`,
	); err == nil {
		t.Error("duplicate (spec_id, doctrine_name) accepted; want PRIMARY KEY violation")
	}

	if _, err := s.DB().Exec(
		`INSERT INTO subprocess_sessions
		 (spec_id, doctrine_name, thread_id, worktree, project_id, pid, started_at, last_use_at, ttl_seconds)
		 VALUES ('s1', 'max-scope', 'tid-3', '/tmp', 'p1', 9999, 3, 3, 28800)`,
	); err != nil {
		t.Errorf("different doctrine under same spec rejected: %v", err)
	}
}

func TestMigration048SmokeInsertSelect(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "subprocess-smoke.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if _, err := s.DB().Exec(
		`INSERT INTO subprocess_sessions
		 (spec_id, doctrine_name, thread_id, worktree, project_id, pid, started_at, last_use_at, ttl_seconds)
		 VALUES ('s', 'default', 'tid-x', '/tmp/wt', 'p', 1234, 1, 1, 3600)`,
	); err != nil {
		t.Fatalf("insert: %v", err)
	}
	var pid int
	if err := s.DB().QueryRow(
		`SELECT pid FROM subprocess_sessions WHERE spec_id='s' AND doctrine_name='default'`,
	).Scan(&pid); err != nil {
		t.Fatalf("select: %v", err)
	}
	if pid != 1234 {
		t.Errorf("pid = %d, want 1234", pid)
	}
}

func TestMigration048Reapplicable(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "subprocess-reapply.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if _, err := s.DB().Exec(migrationV14); err != nil {
		t.Errorf("re-applying migrationV14 failed: %v", err)
	}
}
