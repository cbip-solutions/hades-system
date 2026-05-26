package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestMigration044DoctrineStateTable(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "doctrine-state.db"))
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
	if v < 12 {
		t.Errorf("schemaVersion = %d, want >= 12", v)
	}

	rows, err := s.DB().Query(`PRAGMA table_info(doctrine_state)`)
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
			dfltValue sql.NullString
			pk        int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			t.Fatalf("scan: %v", err)
		}
		cols[name] = true
	}
	for _, want := range []string{"id", "schema_json", "provenance_json", "loaded_at_unix", "doctrine_name"} {
		if !cols[want] {
			t.Errorf("doctrine_state missing column %q", want)
		}
	}
}

func TestMigration044SingletonConstraint(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "doctrine-singleton.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if _, err := s.DB().Exec(
		`INSERT INTO doctrine_state(id, schema_json, provenance_json, loaded_at_unix, doctrine_name) VALUES (1, '{}', '{}', 1, 'max-scope')`,
	); err != nil {
		t.Fatalf("first insert: %v", err)
	}

	if _, err := s.DB().Exec(
		`INSERT INTO doctrine_state(id, schema_json, provenance_json, loaded_at_unix, doctrine_name) VALUES (2, '{}', '{}', 2, 'default')`,
	); err == nil {
		t.Error("INSERT id=2 succeeded; want CHECK violation")
	}
}

func TestMigration044Reapplicable(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "doctrine-reapply.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	if _, err := s.DB().Exec(migrationV10); err != nil {
		t.Errorf("re-applying migrationV10 failed: %v", err)
	}
}
