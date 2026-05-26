package store

import (
	"path/filepath"
	"testing"
)

func TestMigration056CreatesSubstrateHealth(t *testing.T) {
	t.Parallel()
	s, err := Open(filepath.Join(t.TempDir(), "substrate_health.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	rows, err := s.DB().Query(`PRAGMA table_info(substrate_health)`)
	if err != nil {
		t.Fatalf("PRAGMA table_info: %v", err)
	}
	got := map[string]string{}
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			rows.Close()
			t.Fatalf("scan: %v", err)
		}
		got[name] = ctype
	}
	rows.Close()

	want := map[string]string{
		"id":                          "INTEGER",
		"commit_sha":                  "TEXT",
		"authored_by":                 "TEXT",
		"test_pass_rate":              "REAL",
		"test_total":                  "INTEGER",
		"test_passed":                 "INTEGER",
		"doctrine_lint_pass":          "BOOLEAN",
		"doctrine_lint_findings_json": "TEXT",
		"recorded_at":                 "INTEGER",
	}
	for col, ty := range want {
		if got[col] != ty {
			t.Errorf("column %q: got type %q, want %q", col, got[col], ty)
		}
	}

	for _, idx := range []string{"idx_substrate_health_authored_by", "idx_substrate_health_recorded_at"} {
		var name string
		if err := s.DB().QueryRow(`SELECT name FROM sqlite_master WHERE type='index' AND name=?`, idx).Scan(&name); err != nil {
			t.Errorf("index %q missing: %v", idx, err)
		}
	}

	if _, err := s.DB().Exec(`INSERT INTO substrate_health
		(commit_sha, authored_by, test_pass_rate, test_total, test_passed,
		 doctrine_lint_pass, doctrine_lint_findings_json, recorded_at)
		VALUES ('abc', 'invalid_role', 1.0, 1, 1, 1, '[]', 1)`); err == nil {
		t.Fatal("expected CHECK constraint violation for authored_by='invalid_role', got nil")
	}

	if _, err := s.DB().Exec(`INSERT INTO substrate_health
		(commit_sha, authored_by, test_pass_rate, test_total, test_passed,
		 doctrine_lint_pass, doctrine_lint_findings_json, recorded_at)
		VALUES ('abc', 'substrate', 1.5, 1, 1, 1, '[]', 1)`); err == nil {
		t.Fatal("expected CHECK constraint violation for test_pass_rate=1.5, got nil")
	}

	res, err := s.DB().Exec(`INSERT INTO substrate_health
		(commit_sha, authored_by, test_pass_rate, test_total, test_passed,
		 doctrine_lint_pass, doctrine_lint_findings_json, recorded_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"deadbeef", "substrate", 0.95, 100, 95, 1, `[{"rule":"r1","severity":"soft"}]`, 1714521600)
	if err != nil {
		t.Fatalf("valid insert: %v", err)
	}
	id, _ := res.LastInsertId()
	if id <= 0 {
		t.Errorf("LastInsertId=%d", id)
	}
}

func TestSchemaVersionIs25AtLeast(t *testing.T) {
	t.Parallel()
	if schemaVersion < 25 {
		t.Errorf("schemaVersion = %d, want >= 25 (Plan 7 Phase B-6 migration 060 must remain applied)", schemaVersion)
	}
}
