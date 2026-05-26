package store

import (
	"database/sql"
	"errors"
	"testing"
)

func insertRawAuditEvent(t *testing.T, s *Store, id, projectID, eventType, payloadJSON string, emittedAt int64) {
	t.Helper()
	_, err := s.DB().Exec(
		`INSERT INTO audit_events_raw (id, project_id, type, payload_json, emitted_at)
		 VALUES (?, ?, ?, ?, ?)`,
		id, projectID, eventType, payloadJSON, emittedAt,
	)
	if err != nil {
		t.Fatalf("insert raw audit event: %v", err)
	}
}

func TestGetChainTipEmpty(t *testing.T) {
	s := openMigratedAuditChainStore(t)
	_, err := s.GetChainTip()
	if !errors.Is(err, ErrNoChainTip) {
		t.Errorf("want ErrNoChainTip, got %v", err)
	}
}

func TestGetChainTipPopulated(t *testing.T) {
	s := openMigratedAuditChainStore(t)
	insertRawAuditEvent(t, s, "evt-1", "proj-A", "test.event", `{"k":1}`, 1700000000)
	if err := s.UpdateChainColumns("evt-1", "", "abc123", "2023_11"); err != nil {
		t.Fatalf("UpdateChainColumns: %v", err)
	}
	tip, err := s.GetChainTip()
	if err != nil {
		t.Fatalf("GetChainTip: %v", err)
	}
	if tip != "abc123" {
		t.Errorf("tip = %q, want %q", tip, "abc123")
	}
}

func TestGetEventByIDNotFound(t *testing.T) {
	s := openMigratedAuditChainStore(t)
	_, err := s.GetEventByID("nonexistent")
	if !errors.Is(err, ErrEventNotFound) {
		t.Errorf("want ErrEventNotFound, got %v", err)
	}
}

func TestGetEventByIDFound(t *testing.T) {
	s := openMigratedAuditChainStore(t)
	insertRawAuditEvent(t, s, "evt-x", "proj-X", "x.event", `{}`, 1700000001)
	got, err := s.GetEventByID("evt-x")
	if err != nil {
		t.Fatalf("GetEventByID: %v", err)
	}
	if got.ID != "evt-x" || got.ProjectID != "proj-X" || got.Type != "x.event" {
		t.Errorf("got %+v, want id=evt-x project=proj-X type=x.event", got)
	}
	if got.PrevHash != "" || got.RecordHash != "" || got.PartitionID != "" {
		t.Errorf("expected empty chain columns on fresh insert, got %+v", got)
	}
	if got.TesseraLeafID.Valid {
		t.Errorf("expected NULL tessera_leaf_id on fresh insert")
	}
}

func TestUpdateChainColumnsSuccess(t *testing.T) {
	s := openMigratedAuditChainStore(t)
	insertRawAuditEvent(t, s, "evt-c", "p", "t", `{}`, 1700000010)
	err := s.UpdateChainColumns("evt-c", "prev0", "rec1", "2023_11")
	if err != nil {
		t.Fatalf("UpdateChainColumns: %v", err)
	}
	got, _ := s.GetEventByID("evt-c")
	if got.PrevHash != "prev0" || got.RecordHash != "rec1" || got.PartitionID != "2023_11" {
		t.Errorf("chain columns not set: %+v", got)
	}
}

func TestUpdateChainColumnsAlreadyChainedRejected(t *testing.T) {
	s := openMigratedAuditChainStore(t)
	insertRawAuditEvent(t, s, "evt-d", "p", "t", `{}`, 1700000020)
	if err := s.UpdateChainColumns("evt-d", "p1", "r1", "2023_11"); err != nil {
		t.Fatalf("first UpdateChainColumns: %v", err)
	}

	err := s.UpdateChainColumns("evt-d", "p2", "r2", "2023_11")
	if err == nil {
		t.Fatal("expected error on second UpdateChainColumns; got nil")
	}
}

func TestUpdateChainColumnsEventNotFound(t *testing.T) {
	s := openMigratedAuditChainStore(t)
	err := s.UpdateChainColumns("nonexistent-id", "p", "r", "2023_11")
	if err == nil {
		t.Fatal("expected error on non-existent id; got nil")
	}
}

func TestUpdateTesseraLeafIDSuccess(t *testing.T) {
	s := openMigratedAuditChainStore(t)
	insertRawAuditEvent(t, s, "evt-l", "p", "t", `{}`, 1700000030)
	if err := s.UpdateChainColumns("evt-l", "p", "r", "2023_11"); err != nil {
		t.Fatalf("UpdateChainColumns: %v", err)
	}
	if err := s.UpdateTesseraLeafID("evt-l", "leaf-42"); err != nil {
		t.Fatalf("UpdateTesseraLeafID: %v", err)
	}
	got, _ := s.GetEventByID("evt-l")
	if !got.TesseraLeafID.Valid || got.TesseraLeafID.String != "leaf-42" {
		t.Errorf("tessera_leaf_id = %v, want leaf-42", got.TesseraLeafID)
	}
}

func TestUpdateTesseraLeafIDAlreadySetRejected(t *testing.T) {
	s := openMigratedAuditChainStore(t)
	insertRawAuditEvent(t, s, "evt-l2", "p", "t", `{}`, 1700000040)
	if err := s.UpdateChainColumns("evt-l2", "p", "r", "2023_11"); err != nil {
		t.Fatalf("UpdateChainColumns: %v", err)
	}
	if err := s.UpdateTesseraLeafID("evt-l2", "leaf-100"); err != nil {
		t.Fatalf("first UpdateTesseraLeafID: %v", err)
	}
	err := s.UpdateTesseraLeafID("evt-l2", "leaf-101")
	if err == nil {
		t.Fatal("expected error on second UpdateTesseraLeafID; got nil")
	}
}

func TestUpdateTesseraLeafIDEventNotFound(t *testing.T) {
	s := openMigratedAuditChainStore(t)
	err := s.UpdateTesseraLeafID("nonexistent-id", "leaf-x")
	if err == nil {
		t.Fatal("expected error on non-existent id; got nil")
	}
}

func TestInsertAndGetPartitionSeal(t *testing.T) {
	s := openMigratedAuditChainStore(t)
	row := AuditPartitionSealRow{
		PartitionID:            "2023_11",
		SealedAt:               1701000000,
		FinalRecordHash:        "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		TesseraSealLeafID:      "seal-leaf-1",
		DaemonWitnessSignature: "ecdsa-sig-bytes",
	}
	if err := s.InsertPartitionSeal(row); err != nil {
		t.Fatalf("InsertPartitionSeal: %v", err)
	}
	got, err := s.GetPartitionSeal("2023_11")
	if err != nil {
		t.Fatalf("GetPartitionSeal: %v", err)
	}
	if got.PartitionID != row.PartitionID || got.FinalRecordHash != row.FinalRecordHash {
		t.Errorf("seal round-trip mismatch: got %+v want %+v", got, row)
	}
	if got.TesseraSealLeafID != row.TesseraSealLeafID || got.DaemonWitnessSignature != row.DaemonWitnessSignature {
		t.Errorf("seal mismatch on tessera/witness fields: got %+v want %+v", got, row)
	}
	if got.SealedAt != row.SealedAt {
		t.Errorf("seal sealed_at = %d, want %d", got.SealedAt, row.SealedAt)
	}
	if got.ColdArchiveURL.Valid || got.ColdArchiveContentHash.Valid {
		t.Errorf("cold archive fields should be NULL on initial insert: %+v", got)
	}
}

func TestInsertPartitionSealWithColdArchive(t *testing.T) {
	s := openMigratedAuditChainStore(t)
	row := AuditPartitionSealRow{
		PartitionID:            "2024_01",
		SealedAt:               1704000000,
		FinalRecordHash:        "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
		TesseraSealLeafID:      "seal-leaf-2",
		DaemonWitnessSignature: "witness-sig-2",
		ColdArchiveURL:         sqlNullStringFor("s3://archive/2024_01.tar"),
		ColdArchiveContentHash: sqlNullStringFor("cafebabecafebabecafebabecafebabecafebabecafebabecafebabecafebabe"),
	}
	if err := s.InsertPartitionSeal(row); err != nil {
		t.Fatalf("InsertPartitionSeal: %v", err)
	}
	got, err := s.GetPartitionSeal("2024_01")
	if err != nil {
		t.Fatalf("GetPartitionSeal: %v", err)
	}
	if !got.ColdArchiveURL.Valid || got.ColdArchiveURL.String != "s3://archive/2024_01.tar" {
		t.Errorf("cold archive url = %v, want s3://archive/2024_01.tar", got.ColdArchiveURL)
	}
	if !got.ColdArchiveContentHash.Valid || got.ColdArchiveContentHash.String != "cafebabecafebabecafebabecafebabecafebabecafebabecafebabecafebabe" {
		t.Errorf("cold archive content hash = %v, want cafebabe...", got.ColdArchiveContentHash)
	}
}

func TestInsertPartitionSealDuplicateRejected(t *testing.T) {
	s := openMigratedAuditChainStore(t)
	row := AuditPartitionSealRow{
		PartitionID:            "2023_12",
		SealedAt:               1702000000,
		FinalRecordHash:        "1111111111111111111111111111111111111111111111111111111111111111",
		TesseraSealLeafID:      "seal-leaf-dup",
		DaemonWitnessSignature: "witness-dup",
	}
	if err := s.InsertPartitionSeal(row); err != nil {
		t.Fatalf("first InsertPartitionSeal: %v", err)
	}
	err := s.InsertPartitionSeal(row)
	if err == nil {
		t.Fatal("expected duplicate-PK error on second InsertPartitionSeal; got nil")
	}
}

func TestGetPartitionSealNotFound(t *testing.T) {
	s := openMigratedAuditChainStore(t)
	_, err := s.GetPartitionSeal("9999_99")
	if !errors.Is(err, ErrPartitionSealNotFound) {
		t.Errorf("want ErrPartitionSealNotFound, got %v", err)
	}
}

func TestListPartitionsEmpty(t *testing.T) {
	s := openMigratedAuditChainStore(t)
	parts, err := s.ListPartitions()
	if err != nil {
		t.Fatalf("ListPartitions: %v", err)
	}
	if len(parts) != 0 {
		t.Errorf("len(parts) = %d, want 0", len(parts))
	}
}

func TestListPartitionsAfterChainCompute(t *testing.T) {
	s := openMigratedAuditChainStore(t)
	insertRawAuditEvent(t, s, "e1", "p", "t", `{}`, 1700000000)
	insertRawAuditEvent(t, s, "e2", "p", "t", `{}`, 1700100000)
	insertRawAuditEvent(t, s, "e3", "p", "t", `{}`, 1703000000)
	if err := s.UpdateChainColumns("e1", "", "h1", "2023_11"); err != nil {
		t.Fatalf("UpdateChainColumns e1: %v", err)
	}
	if err := s.UpdateChainColumns("e2", "h1", "h2", "2023_11"); err != nil {
		t.Fatalf("UpdateChainColumns e2: %v", err)
	}
	if err := s.UpdateChainColumns("e3", "h2", "h3", "2023_12"); err != nil {
		t.Fatalf("UpdateChainColumns e3: %v", err)
	}
	parts, err := s.ListPartitions()
	if err != nil {
		t.Fatalf("ListPartitions: %v", err)
	}
	if len(parts) != 2 {
		t.Errorf("len(parts) = %d, want 2 (2023_11 + 2023_12)", len(parts))
	}
	if parts[0].PartitionID != "2023_11" || parts[0].EventCount != 2 {
		t.Errorf("parts[0] = %+v, want partition_id=2023_11 event_count=2", parts[0])
	}
	if parts[1].PartitionID != "2023_12" || parts[1].EventCount != 1 {
		t.Errorf("parts[1] = %+v, want partition_id=2023_12 event_count=1", parts[1])
	}

	if parts[0].FinalRecordHash != "h2" {
		t.Errorf("parts[0].FinalRecordHash = %q, want h2 (chain tip of 2023_11)", parts[0].FinalRecordHash)
	}
	if parts[1].FinalRecordHash != "h3" {
		t.Errorf("parts[1].FinalRecordHash = %q, want h3 (chain tip of 2023_12)", parts[1].FinalRecordHash)
	}
}

func TestListEventsForPartition(t *testing.T) {
	s := openMigratedAuditChainStore(t)
	insertRawAuditEvent(t, s, "p-e1", "p", "t", `{}`, 1700000000)
	insertRawAuditEvent(t, s, "p-e2", "p", "t", `{}`, 1700100000)
	if err := s.UpdateChainColumns("p-e1", "", "h1", "2023_11"); err != nil {
		t.Fatalf("UpdateChainColumns p-e1: %v", err)
	}
	if err := s.UpdateChainColumns("p-e2", "h1", "h2", "2023_11"); err != nil {
		t.Fatalf("UpdateChainColumns p-e2: %v", err)
	}
	events, err := s.ListEventsForPartition("2023_11")
	if err != nil {
		t.Fatalf("ListEventsForPartition: %v", err)
	}
	if len(events) != 2 {
		t.Errorf("len(events) = %d, want 2", len(events))
	}
	if events[0].ID != "p-e1" || events[1].ID != "p-e2" {
		t.Errorf("events out of insertion order: %+v", events)
	}
}

func TestListEventsForPartitionEmpty(t *testing.T) {
	s := openMigratedAuditChainStore(t)
	events, err := s.ListEventsForPartition("nonexistent-partition")
	if err != nil {
		t.Fatalf("ListEventsForPartition: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("len(events) = %d, want 0 for unknown partition", len(events))
	}
}

func TestBackfillScanCursor(t *testing.T) {
	s := openMigratedAuditChainStore(t)
	insertRawAuditEvent(t, s, "b-1", "p", "t", `{}`, 1700000000)
	insertRawAuditEvent(t, s, "b-2", "p", "t", `{}`, 1700100000)
	insertRawAuditEvent(t, s, "b-3", "p", "t", `{}`, 1700200000)
	rows, err := s.BackfillScan(0, 2)
	if err != nil {
		t.Fatalf("BackfillScan: %v", err)
	}
	if len(rows) != 2 {
		t.Errorf("first batch len = %d, want 2", len(rows))
	}
	if rows[0].ID != "b-1" || rows[1].ID != "b-2" {
		t.Errorf("first batch wrong order: %+v", rows)
	}

	rows2, err := s.BackfillScan(rows[len(rows)-1].RowID, 2)
	if err != nil {
		t.Fatalf("BackfillScan continuation: %v", err)
	}
	if len(rows2) != 1 {
		t.Errorf("second batch len = %d, want 1", len(rows2))
	}
	if rows2[0].ID != "b-3" {
		t.Errorf("second batch wrong: %+v", rows2)
	}
}

func TestBackfillScanSkipsAlreadyChained(t *testing.T) {
	s := openMigratedAuditChainStore(t)
	insertRawAuditEvent(t, s, "f-1", "p", "t", `{}`, 1700000000)
	insertRawAuditEvent(t, s, "f-2", "p", "t", `{}`, 1700100000)
	if err := s.UpdateChainColumns("f-1", "", "h1", "2023_11"); err != nil {
		t.Fatalf("UpdateChainColumns: %v", err)
	}
	rows, err := s.BackfillScan(0, 100)
	if err != nil {
		t.Fatalf("BackfillScan: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("len = %d, want 1 (f-1 already chained must be skipped)", len(rows))
	}
	if rows[0].ID != "f-2" {
		t.Errorf("got %s, want f-2", rows[0].ID)
	}
}

func TestBackfillScanEmpty(t *testing.T) {
	s := openMigratedAuditChainStore(t)
	rows, err := s.BackfillScan(0, 100)
	if err != nil {
		t.Fatalf("BackfillScan empty: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("len = %d, want 0 on empty table", len(rows))
	}
}

func closeStoreUnderlyingDB(t *testing.T, s *Store) {
	t.Helper()
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestGetChainTipDBError(t *testing.T) {
	s := openMigratedAuditChainStore(t)
	closeStoreUnderlyingDB(t, s)
	_, err := s.GetChainTip()
	if err == nil {
		t.Fatal("expected error after Close; got nil")
	}
	if errors.Is(err, ErrNoChainTip) {
		t.Errorf("got ErrNoChainTip, want generic DB error wrap")
	}
}

func TestGetEventByIDDBError(t *testing.T) {
	s := openMigratedAuditChainStore(t)
	closeStoreUnderlyingDB(t, s)
	_, err := s.GetEventByID("evt-x")
	if err == nil {
		t.Fatal("expected error after Close; got nil")
	}
	if errors.Is(err, ErrEventNotFound) {
		t.Errorf("got ErrEventNotFound, want generic DB error wrap")
	}
}

func TestUpdateChainColumnsDBError(t *testing.T) {
	s := openMigratedAuditChainStore(t)
	closeStoreUnderlyingDB(t, s)
	err := s.UpdateChainColumns("evt-x", "p", "r", "2023_11")
	if err == nil {
		t.Fatal("expected error after Close; got nil")
	}
}

func TestUpdateTesseraLeafIDDBError(t *testing.T) {
	s := openMigratedAuditChainStore(t)
	closeStoreUnderlyingDB(t, s)
	err := s.UpdateTesseraLeafID("evt-x", "leaf")
	if err == nil {
		t.Fatal("expected error after Close; got nil")
	}
}

func TestInsertPartitionSealDBError(t *testing.T) {
	s := openMigratedAuditChainStore(t)
	closeStoreUnderlyingDB(t, s)
	err := s.InsertPartitionSeal(AuditPartitionSealRow{
		PartitionID:            "2023_11",
		SealedAt:               1,
		FinalRecordHash:        "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		TesseraSealLeafID:      "leaf",
		DaemonWitnessSignature: "sig",
	})
	if err == nil {
		t.Fatal("expected error after Close; got nil")
	}
}

func TestGetPartitionSealDBError(t *testing.T) {
	s := openMigratedAuditChainStore(t)
	closeStoreUnderlyingDB(t, s)
	_, err := s.GetPartitionSeal("2023_11")
	if err == nil {
		t.Fatal("expected error after Close; got nil")
	}
	if errors.Is(err, ErrPartitionSealNotFound) {
		t.Errorf("got ErrPartitionSealNotFound, want generic DB error wrap")
	}
}

func TestListPartitionsDBError(t *testing.T) {
	s := openMigratedAuditChainStore(t)
	closeStoreUnderlyingDB(t, s)
	_, err := s.ListPartitions()
	if err == nil {
		t.Fatal("expected error after Close; got nil")
	}
}

func TestListEventsForPartitionDBError(t *testing.T) {
	s := openMigratedAuditChainStore(t)
	closeStoreUnderlyingDB(t, s)
	_, err := s.ListEventsForPartition("2023_11")
	if err == nil {
		t.Fatal("expected error after Close; got nil")
	}
}

func TestBackfillScanDBError(t *testing.T) {
	s := openMigratedAuditChainStore(t)
	closeStoreUnderlyingDB(t, s)
	_, err := s.BackfillScan(0, 10)
	if err == nil {
		t.Fatal("expected error after Close; got nil")
	}
}

func sqlNullStringFor(s string) sql.NullString {
	return sql.NullString{String: s, Valid: true}
}
