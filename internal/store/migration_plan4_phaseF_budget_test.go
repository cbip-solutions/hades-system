package store

import (
	"path/filepath"
	"testing"
)

func TestMigrationV18_BudgetAxesApplied(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "v18.db"))
	defer s.Close()
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	row := s.DB().QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name='cost_axis_tags'`,
	)
	var name string
	if err := row.Scan(&name); err != nil {
		t.Fatalf("cost_axis_tags missing: %v", err)
	}
	row = s.DB().QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name='axis_tag_loss_events'`,
	)
	if err := row.Scan(&name); err != nil {
		t.Fatalf("axis_tag_loss_events missing: %v", err)
	}
}

func TestMigrationV19_BudgetPauseApplied(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "v19.db"))
	defer s.Close()
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	for _, table := range []string{"budget_pauses", "budget_anomalies", "budget_anomaly_samples"} {
		row := s.DB().QueryRow(
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table,
		)
		var name string
		if err := row.Scan(&name); err != nil {
			t.Fatalf("table %q missing: %v", table, err)
		}
	}
}

func TestMigrationV20_AnomalySamplesCostIDApplied(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "v20.db"))
	defer s.Close()
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	rows, err := s.DB().Query(`PRAGMA table_info(budget_anomaly_samples)`)
	if err != nil {
		t.Fatalf("PRAGMA table_info: %v", err)
	}
	defer rows.Close()
	var hasCostID bool
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan PRAGMA: %v", err)
		}
		if name == "cost_id" {
			hasCostID = true
		}
	}
	if !hasCostID {
		t.Error("budget_anomaly_samples.cost_id column missing")
	}

	_, err = s.DB().Exec(
		`INSERT INTO budget_anomaly_samples (scope, scope_value, cost_id, sample_usd, sampled_at)
		 VALUES ('stage', 'design', 42, 1.0, 100)`,
	)
	if err != nil {
		t.Fatalf("first insert: %v", err)
	}
	_, err = s.DB().Exec(
		`INSERT INTO budget_anomaly_samples (scope, scope_value, cost_id, sample_usd, sampled_at)
		 VALUES ('stage', 'design', 42, 2.0, 200)`,
	)
	if err == nil {
		t.Error("duplicate INSERT succeeded; UNIQUE constraint not enforced")
	}
}

func TestSchemaVersionAt22AfterPlan4PhaseG(t *testing.T) {

	if schemaVersion < 22 {
		t.Errorf("schemaVersion = %d, want ≥22 (Plan 4 Phase G: research_cache v21 + audit_events_raw v22)", schemaVersion)
	}
}
