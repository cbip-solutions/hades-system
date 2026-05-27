package auditadapter

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"

	"github.com/cbip-solutions/hades-system/internal/audit/recovery"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3_ncruces", "file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`CREATE TABLE audit_partition_seals (
		partition_id TEXT PRIMARY KEY,
		sealed_at INTEGER NOT NULL,
		final_record_hash TEXT NOT NULL,
		tessera_seal_leaf_id TEXT NOT NULL,
		daemon_witness_signature TEXT NOT NULL,
		cold_archive_url TEXT,
		cold_archive_content_hash TEXT
	)`)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	_, err = db.Exec(`CREATE TABLE audit_events_partitions (
		partition_id TEXT PRIMARY KEY,
		first_id TEXT NOT NULL,
		last_id TEXT NOT NULL,
		event_count INTEGER NOT NULL,
		final_record_hash TEXT NOT NULL
	)`)
	if err != nil {
		t.Fatalf("create partition stats: %v", err)
	}
	return db
}

func seedSeal(t *testing.T, db *sql.DB, partitionID string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO audit_partition_seals
		(partition_id, sealed_at, final_record_hash, tessera_seal_leaf_id, daemon_witness_signature)
		VALUES (?, ?, ?, ?, ?)`,
		partitionID, int64(1700000000000), "hashvalue", "leafid", "sigvalue")
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
}

func TestUpdateColdArchiveSetsColumns(t *testing.T) {
	db := openTestDB(t)
	seedSeal(t, db, "2026_05")
	store := NewPartitionSealStore(db)
	err := store.UpdateColdArchive(context.Background(), "zen-swarm", "2026_05", "s3://bucket/key", "abc123")
	if err != nil {
		t.Fatalf("UpdateColdArchive: %v", err)
	}
	got, err := store.GetSealRow(context.Background(), "zen-swarm", "2026_05")
	if err != nil {
		t.Fatalf("GetSealRow: %v", err)
	}
	if got.ColdArchiveURL != "s3://bucket/key" {
		t.Errorf("ColdArchiveURL = %q", got.ColdArchiveURL)
	}
	if got.ColdArchiveContentHash != "abc123" {
		t.Errorf("ColdArchiveContentHash = %q", got.ColdArchiveContentHash)
	}
}

func TestUpdateColdArchiveMissingRowReturnsError(t *testing.T) {
	db := openTestDB(t)
	store := NewPartitionSealStore(db)
	err := store.UpdateColdArchive(context.Background(), "zen-swarm", "2026_05", "s3://x", "h")
	if !errors.Is(err, ErrSealRowMissing) {
		t.Errorf("err = %v, want ErrSealRowMissing", err)
	}
}

func TestGetSealRowMissingReturnsError(t *testing.T) {
	db := openTestDB(t)
	store := NewPartitionSealStore(db)
	_, err := store.GetSealRow(context.Background(), "zen-swarm", "2026_05")
	if !errors.Is(err, ErrSealRowMissing) {
		t.Errorf("err = %v, want ErrSealRowMissing", err)
	}
}

func TestUpdateRejectsEmptyArgs(t *testing.T) {
	db := openTestDB(t)
	store := NewPartitionSealStore(db)
	if err := store.UpdateColdArchive(context.Background(), "", "2026_05", "u", "h"); err == nil {
		t.Error("empty project_id accepted")
	}
	if err := store.UpdateColdArchive(context.Background(), "zen", "", "u", "h"); err == nil {
		t.Error("empty partition_id accepted")
	}
}

func seedSealFull(t *testing.T, db *sql.DB, partitionID string, sealedAtMs int64,
	finalHash, leafID, witnessSig, coldURL, coldHash string) {
	t.Helper()

	_, err := db.Exec(`INSERT INTO audit_partition_seals
		(partition_id, sealed_at, final_record_hash, tessera_seal_leaf_id,
		 daemon_witness_signature, cold_archive_url, cold_archive_content_hash)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		partitionID, sealedAtMs, finalHash, leafID, witnessSig, coldURL, coldHash)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
}

func seedPartitionStat(t *testing.T, db *sql.DB, partitionID, firstID, lastID string, eventCount int64, finalHash string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO audit_events_partitions
		(partition_id, first_id, last_id, event_count, final_record_hash)
		VALUES (?, ?, ?, ?, ?)`,
		partitionID, firstID, lastID, eventCount, finalHash)
	if err != nil {
		t.Fatalf("seed partition stat: %v", err)
	}
}

var (
	_ recovery.SealRowReader   = (*PartitionSealStore)(nil)
	_ recovery.SealStoreReader = (*PartitionSealStore)(nil)
)

func TestListSealsReturnsRowsOrderedBySealedAt(t *testing.T) {
	db := openTestDB(t)

	seedSealFull(t, db, "2026_03", 1700000003000, "h3", "leaf3", "sig3", "s3://b/3", "ch3")
	seedSealFull(t, db, "2026_01", 1700000001000, "h1", "leaf1", "sig1", "s3://b/1", "ch1")
	seedSealFull(t, db, "2026_02", 1700000002000, "h2", "leaf2", "sig2", "", "")
	seedPartitionStat(t, db, "2026_02", "evt-first-2", "evt-last-2", 7, "h2")
	store := NewPartitionSealStore(db)

	got, err := store.ListSeals(context.Background(), "zen-swarm")
	if err != nil {
		t.Fatalf("ListSeals: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len(got) = %d, want 3", len(got))
	}
	wantIDs := []string{"2026_01", "2026_02", "2026_03"}
	for i, w := range wantIDs {
		if got[i].PartitionID != w {
			t.Errorf("got[%d].PartitionID = %q, want %q", i, got[i].PartitionID, w)
		}
	}

	if got[1].FinalRecordHash != "h2" {
		t.Errorf("FinalRecordHash[1] = %q, want h2", got[1].FinalRecordHash)
	}
	if got[1].TesseraSealLeafID != "leaf2" {
		t.Errorf("TesseraSealLeafID[1] = %q, want leaf2", got[1].TesseraSealLeafID)
	}
	if got[1].DaemonWitnessSignature != "sig2" {
		t.Errorf("DaemonWitnessSignature[1] = %q, want sig2", got[1].DaemonWitnessSignature)
	}
	if got[1].EventCount != 7 {
		t.Errorf("EventCount[1] = %d, want 7 from audit_events_partitions", got[1].EventCount)
	}
	if got[1].LastID != "evt-last-2" {
		t.Errorf("LastID[1] = %q, want evt-last-2 from audit_events_partitions", got[1].LastID)
	}
}

func TestListSealsEmpty(t *testing.T) {
	db := openTestDB(t)
	store := NewPartitionSealStore(db)
	got, err := store.ListSeals(context.Background(), "zen-swarm")
	if err != nil {
		t.Fatalf("ListSeals: %v", err)
	}
	if got == nil {
		t.Error("got = nil, want non-nil empty slice")
	}
	if len(got) != 0 {
		t.Errorf("len(got) = %d, want 0", len(got))
	}
}

func TestListSealsProjectIDIsCurrentlyNoOp(t *testing.T) {

	db := openTestDB(t)
	seedSealFull(t, db, "2026_01", 1700000001000, "h1", "leaf1", "sig1", "", "")
	store := NewPartitionSealStore(db)

	gotA, err := store.ListSeals(context.Background(), "project-a")
	if err != nil {
		t.Fatalf("ListSeals: %v", err)
	}
	gotB, err := store.ListSeals(context.Background(), "project-b")
	if err != nil {
		t.Fatalf("ListSeals: %v", err)
	}
	if len(gotA) != 1 || len(gotB) != 1 {
		t.Fatalf("len(gotA)=%d len(gotB)=%d, want 1/1", len(gotA), len(gotB))
	}
}

func TestColdArchiveMetaForReturnsMetadata(t *testing.T) {
	db := openTestDB(t)
	seedSealFull(t, db, "2026_05", 1700000005000, "h5", "leaf5", "sig5",
		"s3://bucket/2026_05.tar.gz", "deadbeefcafebabe")
	store := NewPartitionSealStore(db)

	got, err := store.ColdArchiveMetaFor(context.Background(), "zen-swarm", "2026_05")
	if err != nil {
		t.Fatalf("ColdArchiveMetaFor: %v", err)
	}
	if got.URL != "s3://bucket/2026_05.tar.gz" {
		t.Errorf("URL = %q", got.URL)
	}
	if got.ContentHash != "deadbeefcafebabe" {
		t.Errorf("ContentHash = %q", got.ContentHash)
	}
}

func TestColdArchiveMetaForReturnsEmptyWhenNullColumns(t *testing.T) {

	db := openTestDB(t)
	seedSealFull(t, db, "2026_06", 1700000006000, "h6", "leaf6", "sig6", "", "")
	store := NewPartitionSealStore(db)

	got, err := store.ColdArchiveMetaFor(context.Background(), "zen-swarm", "2026_06")
	if err != nil {
		t.Fatalf("ColdArchiveMetaFor: %v", err)
	}
	if got.URL != "" {
		t.Errorf("URL = %q, want empty", got.URL)
	}
	if got.ContentHash != "" {
		t.Errorf("ContentHash = %q, want empty", got.ContentHash)
	}
}

func TestColdArchiveMetaForMissingReturnsErrSealRowMissing(t *testing.T) {
	db := openTestDB(t)
	store := NewPartitionSealStore(db)
	_, err := store.ColdArchiveMetaFor(context.Background(), "zen-swarm", "missing")
	if !errors.Is(err, ErrSealRowMissing) {
		t.Errorf("err = %v, want ErrSealRowMissing", err)
	}
}

func TestColdArchiveMetaForEmptyArgsRejected(t *testing.T) {
	db := openTestDB(t)
	store := NewPartitionSealStore(db)
	if _, err := store.ColdArchiveMetaFor(context.Background(), "", "2026_05"); err == nil {
		t.Error("empty project_id accepted")
	}
	if _, err := store.ColdArchiveMetaFor(context.Background(), "zen", ""); err == nil {
		t.Error("empty partition_id accepted")
	}
}

func TestListSealsQueryErrorWraps(t *testing.T) {
	db := openTestDB(t)
	store := NewPartitionSealStore(db)
	_ = db.Close()
	_, err := store.ListSeals(context.Background(), "zen-swarm")
	if err == nil {
		t.Fatal("expected error after db close")
	}
	if !contains(err.Error(), "list seals") {
		t.Errorf("err = %v; want wrapped 'list seals'", err)
	}
}

func TestColdArchiveMetaForQueryErrorWraps(t *testing.T) {
	db := openTestDB(t)
	store := NewPartitionSealStore(db)
	_ = db.Close()
	_, err := store.ColdArchiveMetaFor(context.Background(), "zen-swarm", "2026_05")
	if err == nil {
		t.Fatal("expected error after db close")
	}
	if errors.Is(err, ErrSealRowMissing) {
		t.Errorf("err = ErrSealRowMissing; closed-DB should NOT collapse to missing-row")
	}
	if !contains(err.Error(), "get cold archive meta") {
		t.Errorf("err = %v; want wrapped 'get cold archive meta'", err)
	}
}

func TestListSealsScanErrorWraps(t *testing.T) {

	db, err := sql.Open("sqlite3_ncruces", "file::memory:?cache=shared&_pragma=foreign_keys(0)")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`CREATE TABLE audit_partition_seals (
		partition_id TEXT PRIMARY KEY,
		sealed_at INTEGER NOT NULL,
		final_record_hash TEXT,
		tessera_seal_leaf_id TEXT NOT NULL,
		daemon_witness_signature TEXT NOT NULL,
		cold_archive_url TEXT,
		cold_archive_content_hash TEXT
	)`)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	_, err = db.Exec(`CREATE TABLE audit_events_partitions (
		partition_id TEXT PRIMARY KEY,
		first_id TEXT NOT NULL,
		last_id TEXT NOT NULL,
		event_count INTEGER NOT NULL,
		final_record_hash TEXT NOT NULL
	)`)
	if err != nil {
		t.Fatalf("create partition stats: %v", err)
	}

	_, err = db.Exec(`INSERT INTO audit_partition_seals
		(partition_id, sealed_at, final_record_hash, tessera_seal_leaf_id, daemon_witness_signature)
		VALUES (?, ?, NULL, ?, ?)`,
		"2026_07", int64(1700000007000), "leaf7", "sig7")
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	store := NewPartitionSealStore(db)
	_, err = store.ListSeals(context.Background(), "zen-swarm")
	if err == nil {
		t.Fatal("expected scan error on NULL final_record_hash")
	}
	if !contains(err.Error(), "scan seal") {
		t.Errorf("err = %v; want wrapped 'scan seal'", err)
	}
}

func TestUpdateColdArchiveExecError(t *testing.T) {
	db := openTestDB(t)
	seedSeal(t, db, "2026_09")
	store := NewPartitionSealStore(db)
	_ = db.Close()
	err := store.UpdateColdArchive(context.Background(), "zen-swarm", "2026_09", "s3://x", "h")
	if err == nil {
		t.Fatal("expected error after db close")
	}
	if errors.Is(err, ErrSealRowMissing) {
		t.Errorf("err = ErrSealRowMissing; closed-DB should NOT collapse to missing-row")
	}
	if !contains(err.Error(), "update seal") {
		t.Errorf("err = %v; want wrapped 'update seal'", err)
	}
}

func TestGetSealRowNonNoRowsScanError(t *testing.T) {
	db := openTestDB(t)
	seedSeal(t, db, "2026_10")
	store := NewPartitionSealStore(db)
	_ = db.Close()
	_, err := store.GetSealRow(context.Background(), "zen-swarm", "2026_10")
	if err == nil {
		t.Fatal("expected error after db close")
	}
	if errors.Is(err, ErrSealRowMissing) {
		t.Errorf("err = ErrSealRowMissing; closed-DB should NOT collapse to missing-row")
	}
	if !contains(err.Error(), "get seal") {
		t.Errorf("err = %v; want wrapped 'get seal'", err)
	}
}

func contains(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
