package store

import (
	"path/filepath"
	"testing"
)

func TestNumberedMigrationsApply(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	var hasCol int
	err = s.DB().QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('bypass_audit') WHERE name = 'conversation_id'`,
	).Scan(&hasCol)
	if err != nil || hasCol != 1 {
		t.Errorf("conversation_id missing (count=%d, err=%v)", hasCol, err)
	}

	for _, table := range []string{"bypass_audit_bodies", "bypass_audit_pins"} {
		var name string
		err := s.DB().QueryRow(
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table,
		).Scan(&name)
		if err != nil || name != table {
			t.Errorf("%s missing: %v", table, err)
		}
	}
}

func TestMigrateIdempotent(t *testing.T) {
	p := filepath.Join(t.TempDir(), "i.db")
	s, err := Open(p)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("first Migrate: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Errorf("second Migrate: %v", err)
	}
	s.Close()
}

func TestSchemaVersionRecorded(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "v.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	v, err := s.currentVersion()
	if err != nil {
		t.Fatalf("currentVersion: %v", err)
	}
	if v != schemaVersion {
		t.Errorf("currentVersion=%d, want %d", v, schemaVersion)
	}
}
