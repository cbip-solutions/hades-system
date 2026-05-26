package tamperinject_test

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/cbip-solutions/hades-system/tests/testhelpers/tamperinject"
)

func TestTamperInjector_ModifyRecordHashRaw(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "audit.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE audit_events_raw (
			id TEXT PRIMARY KEY,
			event_type INTEGER NOT NULL,
			payload BLOB NOT NULL,
			prev_hash BLOB,
			record_hash BLOB
		);
		INSERT INTO audit_events_raw (id, event_type, payload, prev_hash, record_hash)
		VALUES ('row-1', 1, x'00', NULL, x'aaaa');
	`)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	newHash := sha256.Sum256([]byte("forged"))
	if err := tamperinject.ModifyRecordHashRaw(dbPath, "row-1", newHash[:]); err != nil {
		t.Fatalf("ModifyRecordHashRaw: %v", err)
	}

	var got []byte
	err = db.QueryRow("SELECT record_hash FROM audit_events_raw WHERE id='row-1'").Scan(&got)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if string(got) != string(newHash[:]) {
		t.Errorf("record_hash not modified")
	}
}

func TestTamperInjector_CorruptTesseraTile(t *testing.T) {
	t.Parallel()
	tilePath := filepath.Join(t.TempDir(), "tile-0001.dat")
	original := []byte{0x01, 0x02, 0x03, 0x04, 0x05}
	if err := os.WriteFile(tilePath, original, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := tamperinject.CorruptTesseraTile(tilePath, 2); err != nil {
		t.Fatalf("CorruptTesseraTile: %v", err)
	}

	got, _ := os.ReadFile(tilePath)
	if got[2] == original[2] {
		t.Errorf("byte at offset 2 not corrupted: %v", got)
	}
}

func TestTamperInjector_SwapWitnessSig(t *testing.T) {
	t.Parallel()
	cpPath := filepath.Join(t.TempDir(), "checkpoint.json")
	original := []byte(`{"size":10,"root_hash":"abc","sig":"originalSig","sig_b64":"orig"}`)
	if err := os.WriteFile(cpPath, original, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := tamperinject.SwapWitnessSig(cpPath, []byte("fake_attacker_sig_bytes")); err != nil {
		t.Fatalf("SwapWitnessSig: %v", err)
	}

	got, _ := os.ReadFile(cpPath)
	if string(got) == string(original) {
		t.Errorf("sig not swapped: %s", got)
	}
}

func TestTamperInjector_ModifyRecordHashRaw_OpenError(t *testing.T) {
	t.Parallel()

	badPath := filepath.Join(t.TempDir(), "nodir", "audit.db")
	err := tamperinject.ModifyRecordHashRaw(badPath, "row-1", []byte("h"))
	if err == nil {
		t.Errorf("expected error for unopenable path, got nil")
	}
}

func TestTamperInjector_ModifyRecordHashRaw_NoTable(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "empty.db")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("setup open: %v", err)
	}

	if _, err := db.Exec("CREATE TABLE unused (x INT)"); err != nil {
		t.Fatalf("setup exec: %v", err)
	}
	_ = db.Close()

	if err := tamperinject.ModifyRecordHashRaw(dbPath, "row-1", []byte("h")); err == nil {
		t.Errorf("expected error when audit_events_raw missing, got nil")
	}
}

func TestTamperInjector_CorruptTesseraTile_OffsetOutOfRange(t *testing.T) {
	t.Parallel()
	tilePath := filepath.Join(t.TempDir(), "small.dat")
	if err := os.WriteFile(tilePath, []byte{0x01, 0x02}, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := tamperinject.CorruptTesseraTile(tilePath, 100); err == nil {
		t.Errorf("expected out-of-range error for offset=100 on 2-byte file")
	}
	if err := tamperinject.CorruptTesseraTile(tilePath, -1); err == nil {
		t.Errorf("expected out-of-range error for negative offset")
	}
}

func TestTamperInjector_CorruptTesseraTile_MissingFile(t *testing.T) {
	t.Parallel()
	missing := filepath.Join(t.TempDir(), "does-not-exist.dat")
	if err := tamperinject.CorruptTesseraTile(missing, 0); err == nil {
		t.Errorf("expected error for missing file, got nil")
	}
}

func TestTamperInjector_SwapWitnessSig_MissingFile(t *testing.T) {
	t.Parallel()
	missing := filepath.Join(t.TempDir(), "does-not-exist.json")
	if err := tamperinject.SwapWitnessSig(missing, []byte("x")); err == nil {
		t.Errorf("expected error for missing file, got nil")
	}
}

func TestTamperInjector_SwapWitnessSig_BadJSON(t *testing.T) {
	t.Parallel()
	cpPath := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(cpPath, []byte("not-json{{{"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := tamperinject.SwapWitnessSig(cpPath, []byte("x")); err == nil {
		t.Errorf("expected JSON parse error, got nil")
	}
}

func TestTamperInjector_CorruptTilePartial(t *testing.T) {
	t.Parallel()
	tilePath := filepath.Join(t.TempDir(), "tile.dat")
	original := []byte{0x01, 0x02, 0x03, 0x04, 0x05}
	if err := os.WriteFile(tilePath, original, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := tamperinject.CorruptTilePartial(tilePath, 2); err != nil {
		t.Fatalf("CorruptTilePartial: %v", err)
	}
	got, _ := os.ReadFile(tilePath)
	if len(got) != 2 {
		t.Errorf("len after truncate = %d, want 2", len(got))
	}
}

func TestTamperInjector_CorruptTilePartial_MissingFile(t *testing.T) {
	t.Parallel()
	missing := filepath.Join(t.TempDir(), "does-not-exist.dat")
	if err := tamperinject.CorruptTilePartial(missing, 0); err == nil {
		t.Errorf("expected error for missing file, got nil")
	}
}

func TestTamperInjector_ModifyRecordHashRaw_BypassesRealMigrationTriggers(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "audit.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	for _, rel := range []string{
		"internal/store/schema/055_audit_events_raw.sql",
		"internal/store/schema/059_audit_chain_extension.sql",
	} {
		migrationBytes, err := os.ReadFile(repoRootRelative(t, rel))
		if err != nil {
			t.Fatalf("read migration %s: %v", rel, err)
		}
		if _, err := db.Exec(string(migrationBytes)); err != nil {
			t.Fatalf("execute migration %s: %v", rel, err)
		}
	}

	const rowID = "evt-row-1"
	sealed := sha256.Sum256([]byte("sealed-original"))
	sealedHex := hexEncode(sealed[:])
	_, err = db.Exec(`
		INSERT INTO audit_events_raw (id, project_id, type, payload_json, emitted_at, prev_hash, record_hash, partition_id, tessera_leaf_id)
		VALUES (?, '', 'test.event', '{}', 1700000000, '', ?, '', NULL)
	`, rowID, sealedHex)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	forged := sha256.Sum256([]byte("forged"))
	forgedHex := hexEncode(forged[:])
	_, err = db.Exec("UPDATE audit_events_raw SET record_hash = ? WHERE id = ?", forgedHex, rowID)
	if err == nil {
		t.Fatalf("real migration triggers did NOT block direct UPDATE — migration 059 may be wrong, or the chain_hashes trigger's WHEN clause did not fire")
	}

	if err := tamperinject.ModifyRecordHashRaw(dbPath, rowID, []byte(forgedHex)); err != nil {
		t.Fatalf("ModifyRecordHashRaw against real migrated DB: %v", err)
	}

	var got []byte
	err = db.QueryRow("SELECT record_hash FROM audit_events_raw WHERE id = ?", rowID).Scan(&got)
	if err != nil {
		t.Fatalf("query post-bypass: %v", err)
	}
	if !bytes.Equal(got, []byte(forgedHex)) {
		t.Errorf("record_hash NOT modified after bypass: got %x, want %x", got, forgedHex)
	}
}

func hexEncode(b []byte) string {
	const hexChars = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[i*2] = hexChars[v>>4]
		out[i*2+1] = hexChars[v&0x0f]
	}
	return string(out)
}

func repoRootRelative(t *testing.T, rel string) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Join(dir, rel)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("repo root not found from %s", filepath.Dir(file))
		}
		dir = parent
	}
}
