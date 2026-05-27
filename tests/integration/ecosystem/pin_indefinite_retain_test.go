// go:build integration && cgo

// Package ecosystem_test — pin_indefinite_retain_test.go
//
// the indefinite_retain column has a production writer (the EcosystemPin
// daemon handler → seam Pin method → SQL UPDATE).
//
// This file is the integration-tier counterpart to the unit test in
// internal/daemon/handlers/ecosystem_test.go::
// TestEcosystemPinRoundTripCapturesIndefiniteRetain (which mocks the
// seam and asserts the handler delegates the eco/version tuple). The
// integration test exercises the SQL layer end-to-end:
//
// (1) Open ecosystem.db + apply migrations 001..009 (incl. the
// G-5 indefinite_retain column).
// (2) Seed an ecosystem_packages + ecosystem_versions row with
// indefinite_retain=0 (column default).
// (3) Issue the SQL UPDATE the production EcosystemHandler.Pin adapter
// will issue (`UPDATE ecosystem_versions SET indefinite_retain=1
// WHERE...`).
// (4) Re-query and assert the column reads 1.
//
// This proves the database round-trip works: before the fix-cycle
// the column had no production writer in any code path (G-5 added the
// column + a CLI command but no daemon handler invoked the UPDATE). The
// daemon-side EcosystemPin handler IS the production
// writer; its seam adapter (deferred per option B) MUST issue this same
// SQL UPDATE — this integration test guards the SQL contract.
//
// Build tags `integration && cgo`: matches every other ecosystem
// integration test in this directory. The cgo gate is required because
// ecosystem.ApplyMigrations uses sqlite-vec (CGO bridge); CGO-disabled
// builds exclude this whole subdirectory.
//
// Driver: mattn/go-sqlite3 (same as other ecosystem integration tests in
// this dir). This avoids importing the daemon handlers package (which
// transitively brings in ncruces/go-sqlite3 via internal/store and would
// cause a sql.Register-twice panic). The unit tests in
// internal/daemon/handlers/ecosystem_test.go cover the handler↔seam
// dispatch via mocks; this test covers the SQL persistence layer.
package ecosystem_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/cbip-solutions/hades-system/internal/research/ecosystem"
)

// TestPinIndefiniteRetain_SQLRoundTrip_I2 — I-2 finding absorption.
//
// SQL-level end-to-end: the indefinite_retain column accepts the
// production-equivalent UPDATE statement that the EcosystemHandler.Pin
// seam adapter MUST issue. Pre-state: default 0; post-UPDATE: 1.
//
// Why test the SQL UPDATE in isolation (vs the HTTP round-trip): the
// HTTP handler↔seam dispatch is unit-tested in
// internal/daemon/handlers/ecosystem_test.go with a mock seam (faster +
// no DB required). This integration test guards the OTHER half of the
// contract — the SQL UPDATE statement against a real schema-migrated
// ecosystem.db. Drift between the column schema and the production
// UPDATE statement would surface here without requiring the handler↔db
// adapter wiring (which lands in a deferred follow-up phase).
//
// Production contract reference: the daemon-side EcosystemPin handler
// (internal/daemon/handlers/ecosystem_pin.go) decodes (ecosystem,
// version) from the request body and calls
// EcosystemHandler.Pin(ctx, eco, ver). The production adapter (deferred)
// will translate this to:
//
// UPDATE ecosystem_versions SET indefinite_retain = 1
// WHERE id = (
// SELECT v.id FROM ecosystem_versions v
// JOIN ecosystem_packages p ON p.id = v.package_id
// WHERE p.ecosystem = ? AND v.version = ?)
//
// This test asserts the UPDATE works against the migrated schema.
func TestPinIndefiniteRetain_SQLRoundTrip_I2(t *testing.T) {

	dir := t.TempDir()
	db, err := sql.Open("sqlite3",
		filepath.Join(dir, "ecosystem.db")+"?_foreign_keys=on&_journal_mode=WAL")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()
	if err := ecosystem.ApplyMigrations(db); err != nil {
		t.Fatalf("ApplyMigrations: %v", err)
	}

	res, err := db.Exec(`
		INSERT INTO ecosystem_packages (ecosystem, name, canonical_namespace, upstream_url, last_indexed_at)
		VALUES ('go', 'example.org/x', 'example.org', 'https://example.org/x', CURRENT_TIMESTAMP)`)
	if err != nil {
		t.Fatalf("insert package: %v", err)
	}
	pkgID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO ecosystem_versions (package_id, version, released_at)
		VALUES (?, '1.21.0', CURRENT_TIMESTAMP)`, pkgID); err != nil {
		t.Fatalf("insert version: %v", err)
	}

	var pre int
	if err := db.QueryRow(`
		SELECT v.indefinite_retain
		  FROM ecosystem_versions v
		  JOIN ecosystem_packages p ON p.id = v.package_id
		 WHERE p.ecosystem = 'go' AND v.version = '1.21.0'`).Scan(&pre); err != nil {
		t.Fatalf("pre-pin retain query: %v", err)
	}
	if pre != 0 {
		t.Fatalf("pre-pin indefinite_retain = %d; want 0 (column default)", pre)
	}

	res, err = db.Exec(`
		UPDATE ecosystem_versions SET indefinite_retain = 1
		 WHERE id = (
		   SELECT v.id FROM ecosystem_versions v
		   JOIN ecosystem_packages p ON p.id = v.package_id
		   WHERE p.ecosystem = ? AND v.version = ?)`, "go", "1.21.0")
	if err != nil {
		t.Fatalf("UPDATE indefinite_retain: %v", err)
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		t.Fatalf("RowsAffected: %v", err)
	}
	if rowsAffected != 1 {
		t.Errorf("rowsAffected = %d, want 1 (single (eco, ver) tuple)", rowsAffected)
	}

	var post int
	if err := db.QueryRow(`
		SELECT v.indefinite_retain
		  FROM ecosystem_versions v
		  JOIN ecosystem_packages p ON p.id = v.package_id
		 WHERE p.ecosystem = 'go' AND v.version = '1.21.0'`).Scan(&post); err != nil {
		t.Fatalf("post-pin retain query: %v", err)
	}
	if post != 1 {
		t.Errorf("post-pin indefinite_retain = %d; want 1 "+
			"(UPDATE should have written 1, closing I-2 finding)", post)
	}
}

func TestPinIndefiniteRetain_CheckConstraint_RejectsTwo(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite3",
		filepath.Join(dir, "ecosystem.db")+"?_foreign_keys=on&_journal_mode=WAL")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()
	if err := ecosystem.ApplyMigrations(db); err != nil {
		t.Fatalf("ApplyMigrations: %v", err)
	}

	res, err := db.Exec(`
		INSERT INTO ecosystem_packages (ecosystem, name, canonical_namespace, upstream_url, last_indexed_at)
		VALUES ('go', 'example.org/x', 'example.org', 'https://example.org/x', CURRENT_TIMESTAMP)`)
	if err != nil {
		t.Fatalf("insert package: %v", err)
	}
	pkgID, _ := res.LastInsertId()
	if _, err := db.Exec(`
		INSERT INTO ecosystem_versions (package_id, version, released_at)
		VALUES (?, '1.0.0', CURRENT_TIMESTAMP)`, pkgID); err != nil {
		t.Fatalf("insert version: %v", err)
	}

	_, err = db.Exec(`
		UPDATE ecosystem_versions SET indefinite_retain = 2 WHERE version = '1.0.0'`)
	if err == nil {
		t.Fatal("UPDATE indefinite_retain=2 did NOT fail; CHECK (indefinite_retain IN (0, 1)) is missing")
	}
}
