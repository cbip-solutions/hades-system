package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/audit/chain"
	"github.com/cbip-solutions/hades-system/internal/daemon/auditadapter"
	"github.com/cbip-solutions/hades-system/internal/store"
)

func openMigratedStoreForBootTest(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "boot.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func insertRawAuditEventForBootTest(t *testing.T, s *store.Store, id, projectID, eventType, payloadJSON string, emittedAt int64) {
	t.Helper()
	_, err := s.DB().Exec(
		`INSERT INTO audit_events_raw (id, project_id, type, payload_json, emitted_at)
		 VALUES (?, ?, ?, ?, ?)`,
		id, projectID, eventType, payloadJSON, emittedAt,
	)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
}

func TestBootBackfillChainEmptyStore(t *testing.T) {
	s := openMigratedStoreForBootTest(t)
	a := auditadapter.New(s)

	report, err := bootBackfillChain(context.Background(), a)
	if err != nil {
		t.Fatalf("bootBackfillChain on empty store: %v", err)
	}
	if report.RowsBackfilled != 0 {
		t.Errorf("RowsBackfilled = %d, want 0 on empty store", report.RowsBackfilled)
	}
	if report.BatchesRun != 0 {
		t.Errorf("BatchesRun = %d, want 0 on empty store", report.BatchesRun)
	}
}

func TestBootBackfillChainHistoricalRows(t *testing.T) {
	s := openMigratedStoreForBootTest(t)
	a := auditadapter.New(s)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		id := "hist-" + string(rune('a'+i))
		insertRawAuditEventForBootTest(t, s, id, "p", "test.event", `{}`, 1700000000+int64(i*86400))
	}

	report, err := bootBackfillChain(ctx, a)
	if err != nil {
		t.Fatalf("bootBackfillChain: %v", err)
	}
	if report.RowsBackfilled != 5 {
		t.Errorf("RowsBackfilled = %d, want 5", report.RowsBackfilled)
	}

	// Every seeded row now has non-empty chain columns. Sample two
	// representative rows: the first (prev_hash MUST be empty — first
	// row in the chain) and the last (prev_hash MUST equal the
	// previous row's record_hash).
	first, err := a.GetEventByID(ctx, "hist-a")
	if err != nil {
		t.Fatalf("GetEventByID hist-a: %v", err)
	}
	if first.RecordHash == "" || first.PartitionID == "" {
		t.Errorf("first row chain columns empty after backfill: record_hash=%q partition_id=%q",
			first.RecordHash, first.PartitionID)
	}
	if first.PrevHash != "" {
		t.Errorf("first row prev_hash = %q, want empty (chain genesis)", first.PrevHash)
	}

	walkReport, err := chain.Walk(ctx, a, "p")
	if err != nil {
		t.Fatalf("chain.Walk: %v", err)
	}
	if len(walkReport.Tampered) != 0 {
		t.Errorf("Walk reports Tampered = %+v after backfill", walkReport.Tampered)
	}
	if len(walkReport.GapsDetected) != 0 {
		t.Errorf("Walk reports GapsDetected = %+v after backfill", walkReport.GapsDetected)
	}
	if walkReport.EventsWalked != 5 {
		t.Errorf("Walk EventsWalked = %d, want 5", walkReport.EventsWalked)
	}
}

func TestBootBackfillChainIdempotentReboot(t *testing.T) {
	s := openMigratedStoreForBootTest(t)
	a := auditadapter.New(s)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		id := "warm-" + string(rune('a'+i))
		insertRawAuditEventForBootTest(t, s, id, "p", "test.event", `{}`, 1700000000+int64(i*86400))
	}
	report1, err := bootBackfillChain(ctx, a)
	if err != nil {
		t.Fatalf("first bootBackfillChain: %v", err)
	}
	if report1.RowsBackfilled != 3 {
		t.Errorf("first run RowsBackfilled = %d, want 3", report1.RowsBackfilled)
	}

	report2, err := bootBackfillChain(ctx, a)
	if err != nil {
		t.Fatalf("second bootBackfillChain: %v", err)
	}
	if report2.RowsBackfilled != 0 {
		t.Errorf("idempotent reboot RowsBackfilled = %d, want 0", report2.RowsBackfilled)
	}
}
