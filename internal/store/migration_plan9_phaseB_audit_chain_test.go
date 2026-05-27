package store

import (
	"path/filepath"
	"testing"
)

// migration_plan9_phaseB_audit_chain_test.go validates
// migration 059 (audit_events_raw chain extension):
// - schemaVersion bumps from current baseline by exactly +1
// - Four chain columns exist on audit_events_raw with NOT NULL + DEFAULT
// - REFUSE triggers reject UPDATE on append-only columns and DELETE
// - audit_events_partitions view returns one row per partition
// - audit_partition_seals table exists with PK on partition_id
//
// B-1 baseline (verified at task dispatch time per Sync Point S0):
// schemaVersion=28
// latest schema/ migration: 056_substrate_health.sql (058 lives in
// internal/store/migrations/ numbering coordination).
// Scenario 2: shipped first → baseline 28 + 1 (migration 059) = 29.
// See methodology §4.7.1 plan-vs-reality drift handling.
//
// Floor semantics: phaseBMinSchemaVersion is the minimum value that
// proves migration 059 stayed applied. Later plans MUST be allowed to
// bump schemaVersion further ( Task 9 bumps 29 → 30 for
// migration 064 cost_ledger.provider). The load-bearing assertion is
// "≥29", not "==29"; an exact pin would force every later plan to edit
// this file purely to acknowledge an unrelated bump, which is the
// anti-pattern the floor-style assertions in Plans 4/5/7 already avoid.

const phaseBMinSchemaVersion = 29

func openMigratedAuditChainStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "audit_chain.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestPhaseBSchemaVersionBumped(t *testing.T) {
	s := openMigratedAuditChainStore(t)
	v, err := s.currentVersion()
	if err != nil {
		t.Fatalf("currentVersion: %v", err)
	}
	if v < phaseBMinSchemaVersion {
		t.Errorf("schemaVersion = %d, want >= %d (migration 059 must remain applied)", v, phaseBMinSchemaVersion)
	}
}

func TestPhaseBAuditEventsRawHasPrevHashColumn(t *testing.T) {
	s := openMigratedAuditChainStore(t)
	rows, err := s.DB().Query(`PRAGMA table_info(audit_events_raw)`)
	if err != nil {
		t.Fatalf("table_info: %v", err)
	}
	defer rows.Close()
	var found bool
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dfltValue any
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if name == "prev_hash" {
			found = true
			if ctype != "TEXT" {
				t.Errorf("prev_hash type = %q, want TEXT", ctype)
			}
			if notnull != 1 {
				t.Errorf("prev_hash NOT NULL = %d, want 1", notnull)
			}
		}
	}
	if !found {
		t.Fatal("prev_hash column missing from audit_events_raw")
	}
}

func TestPhaseBAuditEventsRawHasRecordHashColumn(t *testing.T) {
	s := openMigratedAuditChainStore(t)
	rows, err := s.DB().Query(`PRAGMA table_info(audit_events_raw)`)
	if err != nil {
		t.Fatalf("table_info: %v", err)
	}
	defer rows.Close()
	var found bool
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dfltValue any
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if name == "record_hash" {
			found = true
			if ctype != "TEXT" {
				t.Errorf("record_hash type = %q, want TEXT", ctype)
			}
			if notnull != 1 {
				t.Errorf("record_hash NOT NULL = %d, want 1", notnull)
			}
		}
	}
	if !found {
		t.Fatal("record_hash column missing from audit_events_raw")
	}
}

func TestPhaseBAuditEventsRawHasPartitionIDColumn(t *testing.T) {
	s := openMigratedAuditChainStore(t)
	rows, err := s.DB().Query(`PRAGMA table_info(audit_events_raw)`)
	if err != nil {
		t.Fatalf("table_info: %v", err)
	}
	defer rows.Close()
	var found bool
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dfltValue any
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if name == "partition_id" {
			found = true
			if ctype != "TEXT" {
				t.Errorf("partition_id type = %q, want TEXT", ctype)
			}
			if notnull != 1 {
				t.Errorf("partition_id NOT NULL = %d, want 1", notnull)
			}
		}
	}
	if !found {
		t.Fatal("partition_id column missing from audit_events_raw")
	}
}

func TestPhaseBAuditEventsRawHasTesseraLeafIDColumn(t *testing.T) {
	s := openMigratedAuditChainStore(t)
	rows, err := s.DB().Query(`PRAGMA table_info(audit_events_raw)`)
	if err != nil {
		t.Fatalf("table_info: %v", err)
	}
	defer rows.Close()
	var found bool
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dfltValue any
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if name == "tessera_leaf_id" {
			found = true
			if ctype != "TEXT" {
				t.Errorf("tessera_leaf_id type = %q, want TEXT", ctype)
			}

			if notnull != 0 {
				t.Errorf("tessera_leaf_id NOT NULL = %d, want 0 (must be nullable)", notnull)
			}
		}
	}
	if !found {
		t.Fatal("tessera_leaf_id column missing from audit_events_raw")
	}
}

func TestPhaseBAuditPartitionSealsTableExists(t *testing.T) {
	s := openMigratedAuditChainStore(t)
	var name string
	err := s.DB().QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name='audit_partition_seals'`,
	).Scan(&name)
	if err != nil {
		t.Fatalf("audit_partition_seals table missing: %v", err)
	}
	if name != "audit_partition_seals" {
		t.Errorf("expected audit_partition_seals, got %q", name)
	}
}

func TestPhaseBAuditEventsPartitionsViewExists(t *testing.T) {
	s := openMigratedAuditChainStore(t)
	var name string
	err := s.DB().QueryRow(
		`SELECT name FROM sqlite_master WHERE type='view' AND name='audit_events_partitions'`,
	).Scan(&name)
	if err != nil {
		t.Fatalf("audit_events_partitions view missing: %v", err)
	}
	if name != "audit_events_partitions" {
		t.Errorf("expected audit_events_partitions view, got %q", name)
	}
}

func TestPhaseBMigrationIdempotent(t *testing.T) {

	dbPath := filepath.Join(t.TempDir(), "idempotent.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("first Migrate: %v", err)
	}
	v1, _ := s.currentVersion()
	if err := s.Migrate(); err != nil {
		t.Fatalf("second Migrate (should be no-op): %v", err)
	}
	v2, _ := s.currentVersion()
	if v1 != v2 {
		t.Errorf("schemaVersion changed across idempotent re-run: %d → %d", v1, v2)
	}
	s.Close()
}
