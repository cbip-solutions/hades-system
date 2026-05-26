package auditadapter

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/audit/chain"
	"github.com/cbip-solutions/hades-system/internal/audit/tessera"
	"github.com/cbip-solutions/hades-system/internal/store"
)

func openMigratedStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "auditadapter.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestNewRejectsNilStore(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on nil store")
		}
	}()
	_ = New(nil)
}

func TestAdapterSatisfiesChainEventStore(t *testing.T) {
	s := openMigratedStore(t)
	a := New(s)
	var _ chain.EventStore = a
	_ = a
}

func TestAdapterGetChainTipEmpty(t *testing.T) {
	s := openMigratedStore(t)
	a := New(s)
	_, err := a.GetChainTip(context.Background())
	if !errors.Is(err, chain.ErrNoChainTip) {
		t.Errorf("want chain.ErrNoChainTip, got %v", err)
	}
}

func TestAdapterGetEventByIDNotFound(t *testing.T) {
	s := openMigratedStore(t)
	a := New(s)
	_, err := a.GetEventByID(context.Background(), "nonexistent")
	if !errors.Is(err, chain.ErrEventNotFound) {
		t.Errorf("want chain.ErrEventNotFound, got %v", err)
	}
}

func TestAdapterGetPartitionSealNotFound(t *testing.T) {
	s := openMigratedStore(t)
	a := New(s)
	_, err := a.GetPartitionSeal(context.Background(), "9999_99")
	if !errors.Is(err, chain.ErrPartitionSealNotFound) {
		t.Errorf("want chain.ErrPartitionSealNotFound, got %v", err)
	}
}

func insertRawAuditEvent(t *testing.T, s *store.Store, id, projectID, eventType, payloadJSON string, emittedAt int64) {
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

func TestAdapterFullChainComputeRoundTrip(t *testing.T) {
	s := openMigratedStore(t)
	a := New(s)
	ctx := context.Background()

	insertRawAuditEvent(t, s, "evt-1", "proj-X", "test.event", `{}`, 1700000000)
	insertRawAuditEvent(t, s, "evt-2", "proj-X", "test.event", `{}`, 1700000001)

	tip1, err := a.OnEmitRaw(ctx, "evt-1", "proj-X", "test.event", []byte(`{}`), 1700000000)
	if err != nil {
		t.Fatalf("OnEmitRaw evt-1: %v", err)
	}
	if tip1 == "" {
		t.Error("tip1 empty")
	}

	tip2, err := a.OnEmitRaw(ctx, "evt-2", "proj-X", "test.event", []byte(`{}`), 1700000001)
	if err != nil {
		t.Fatalf("OnEmitRaw evt-2: %v", err)
	}
	if tip2 == "" || tip2 == tip1 {
		t.Errorf("tip2 invalid: %q (tip1=%q)", tip2, tip1)
	}

	report, err := chain.Walk(ctx, a, "proj-X")
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(report.Tampered) != 0 {
		t.Errorf("Tampered = %+v", report.Tampered)
	}
	if report.EventsWalked != 2 {
		t.Errorf("EventsWalked = %d, want 2", report.EventsWalked)
	}
}

func TestAdapterOnTesseraBatchFlushed(t *testing.T) {
	s := openMigratedStore(t)
	a := New(s)
	ctx := context.Background()

	insertRawAuditEvent(t, s, "evt-T", "proj-X", "test.event", `{}`, 1700000000)
	if _, err := a.OnEmitRaw(ctx, "evt-T", "proj-X", "test.event", []byte(`{}`), 1700000000); err != nil {
		t.Fatalf("OnEmitRaw: %v", err)
	}

	if err := a.OnTesseraBatchFlushed(ctx, "evt-T", "leaf-42"); err != nil {
		t.Fatalf("OnTesseraBatchFlushed: %v", err)
	}

	got, err := a.GetEventByID(ctx, "evt-T")
	if err != nil {
		t.Fatalf("GetEventByID: %v", err)
	}
	if got.TesseraLeafID == nil || *got.TesseraLeafID != "leaf-42" {
		t.Errorf("tessera_leaf_id = %v, want leaf-42", got.TesseraLeafID)
	}
}

func TestAdapterListPartitionsAfterChainCompute(t *testing.T) {
	s := openMigratedStore(t)
	a := New(s)
	ctx := context.Background()

	insertRawAuditEvent(t, s, "e1", "p", "t", `{}`, 1700000000)
	insertRawAuditEvent(t, s, "e2", "p", "t", `{}`, 1703000000)
	insertRawAuditEvent(t, s, "e3", "p", "t", `{}`, 1703100000)

	for _, id := range []string{"e1", "e2", "e3"} {
		row, _ := s.GetEventByID(id)
		if _, err := a.OnEmitRaw(ctx, id, "p", row.Type, []byte(row.PayloadJSON), row.EmittedAt); err != nil {
			t.Fatalf("OnEmitRaw %s: %v", id, err)
		}
	}

	parts, err := a.ListPartitions(ctx)
	if err != nil {
		t.Fatalf("ListPartitions: %v", err)
	}
	if len(parts) != 2 {
		t.Errorf("len(parts) = %d, want 2", len(parts))
	}
}

func TestAdapterInsertAndGetPartitionSeal(t *testing.T) {
	s := openMigratedStore(t)
	a := New(s)
	ctx := context.Background()

	seal := chain.SealRecord{
		PartitionID:            "2023_11",
		SealedAt:               1701000000,
		FinalRecordHash:        "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		TesseraSealLeafID:      "seal-leaf-1",
		DaemonWitnessSignature: "ecdsa-sig-bytes",
	}
	if err := a.InsertPartitionSeal(ctx, seal); err != nil {
		t.Fatalf("InsertPartitionSeal: %v", err)
	}
	got, err := a.GetPartitionSeal(ctx, "2023_11")
	if err != nil {
		t.Fatalf("GetPartitionSeal: %v", err)
	}
	if got.PartitionID != seal.PartitionID || got.FinalRecordHash != seal.FinalRecordHash {
		t.Errorf("seal round-trip mismatch")
	}
}

func TestAdapterBackfillIntegration(t *testing.T) {
	s := openMigratedStore(t)
	a := New(s)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		id := "bf-" + string(rune('a'+i))
		insertRawAuditEvent(t, s, id, "p", "t", `{}`, 1700000000+int64(i*86400))
	}
	report, err := chain.Backfill(ctx, a, 100)
	if err != nil {
		t.Fatalf("Backfill: %v", err)
	}
	if report.RowsBackfilled != 5 {
		t.Errorf("RowsBackfilled = %d, want 5", report.RowsBackfilled)
	}

	report2, _ := chain.Backfill(ctx, a, 100)
	if report2.RowsBackfilled != 0 {
		t.Errorf("idempotent re-run RowsBackfilled = %d, want 0", report2.RowsBackfilled)
	}
}

// ---- Coverage-completion tests (security-critical 100% target per spec §5.2)

func TestAdapterGetByEventIDAlias(t *testing.T) {
	s := openMigratedStore(t)
	a := New(s)
	ctx := context.Background()

	insertRawAuditEvent(t, s, "alias-evt", "p", "t", `{}`, 1700000000)
	if _, err := a.OnEmitRaw(ctx, "alias-evt", "p", "t", []byte(`{}`), 1700000000); err != nil {
		t.Fatalf("OnEmitRaw: %v", err)
	}

	got, err := a.GetByEventID(ctx, "alias-evt")
	if err != nil {
		t.Fatalf("GetByEventID: %v", err)
	}
	if got == nil || got.ID != "alias-evt" {
		t.Errorf("GetByEventID returned %+v, want id=alias-evt", got)
	}

	if _, err := a.GetByEventID(ctx, "nonexistent"); !errors.Is(err, chain.ErrEventNotFound) {
		t.Errorf("GetByEventID(missing) = %v, want chain.ErrEventNotFound", err)
	}
}

func TestAdapterContextCancelledPaths(t *testing.T) {
	s := openMigratedStore(t)
	a := New(s)

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	cases := []struct {
		name string
		fn   func() error
	}{
		{"GetChainTip", func() error { _, err := a.GetChainTip(cancelled); return err }},
		{"GetEventByID", func() error { _, err := a.GetEventByID(cancelled, "x"); return err }},
		{"GetByEventID", func() error { _, err := a.GetByEventID(cancelled, "x"); return err }},
		{"UpdateChainColumns", func() error { return a.UpdateChainColumns(cancelled, "x", "", "", "") }},
		{"UpdateTesseraLeafID", func() error { return a.UpdateTesseraLeafID(cancelled, "x", "leaf") }},
		{"InsertPartitionSeal", func() error { return a.InsertPartitionSeal(cancelled, chain.SealRecord{}) }},
		{"GetPartitionSeal", func() error { _, err := a.GetPartitionSeal(cancelled, "p"); return err }},
		{"ListPartitions", func() error { _, err := a.ListPartitions(cancelled); return err }},
		{"ListEventsForPartition", func() error { _, err := a.ListEventsForPartition(cancelled, "p"); return err }},
		{"BackfillScan", func() error { _, err := a.BackfillScan(cancelled, 0, 10); return err }},
		{"OnEmitRaw", func() error {
			_, err := a.OnEmitRaw(cancelled, "id", "proj", "t", []byte(`{}`), 1700000000)
			return err
		}},
		{"OnTesseraBatchFlushed", func() error { return a.OnTesseraBatchFlushed(cancelled, "id", "leaf") }},
	}
	for _, c := range cases {
		err := c.fn()
		if !errors.Is(err, context.Canceled) {
			t.Errorf("%s: want context.Canceled, got %v", c.name, err)
		}
	}
}

func TestAdapterInsertPartitionSealColdArchiveFields(t *testing.T) {
	s := openMigratedStore(t)
	a := New(s)
	ctx := context.Background()

	seal := chain.SealRecord{
		PartitionID:            "2024_01",
		SealedAt:               1704067200,
		FinalRecordHash:        "feedfacefeedfacefeedfacefeedfacefeedfacefeedfacefeedfacefeedface",
		TesseraSealLeafID:      "seal-leaf-cold",
		DaemonWitnessSignature: "sig",
		ColdArchiveURL:         "s3://zen-cold/2024_01.tar.zst",
		ColdArchiveContentHash: "abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234",
	}
	if err := a.InsertPartitionSeal(ctx, seal); err != nil {
		t.Fatalf("InsertPartitionSeal: %v", err)
	}
	got, err := a.GetPartitionSeal(ctx, "2024_01")
	if err != nil {
		t.Fatalf("GetPartitionSeal: %v", err)
	}
	if got.ColdArchiveURL != seal.ColdArchiveURL {
		t.Errorf("ColdArchiveURL = %q, want %q", got.ColdArchiveURL, seal.ColdArchiveURL)
	}
	if got.ColdArchiveContentHash != seal.ColdArchiveContentHash {
		t.Errorf("ColdArchiveContentHash = %q, want %q", got.ColdArchiveContentHash, seal.ColdArchiveContentHash)
	}
}

func TestAdapterListEventsForPartition(t *testing.T) {
	s := openMigratedStore(t)
	a := New(s)
	ctx := context.Background()

	insertRawAuditEvent(t, s, "lp-1", "p", "t", `{}`, 1700000000)
	insertRawAuditEvent(t, s, "lp-2", "p", "t", `{}`, 1700000001)
	for _, id := range []string{"lp-1", "lp-2"} {
		if _, err := a.OnEmitRaw(ctx, id, "p", "t", []byte(`{}`), 1700000000); err != nil {
			t.Fatalf("OnEmitRaw %s: %v", id, err)
		}
	}
	if err := a.OnTesseraBatchFlushed(ctx, "lp-1", "lp-leaf-1"); err != nil {
		t.Fatalf("OnTesseraBatchFlushed: %v", err)
	}

	rows, err := a.ListEventsForPartition(ctx, "2023_11")
	if err != nil {
		t.Fatalf("ListEventsForPartition: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2", len(rows))
	}

	var sawSet, sawNil bool
	for _, r := range rows {
		switch r.ID {
		case "lp-1":
			if r.TesseraLeafID == nil || *r.TesseraLeafID != "lp-leaf-1" {
				t.Errorf("lp-1 TesseraLeafID = %v, want lp-leaf-1", r.TesseraLeafID)
			}
			sawSet = true
		case "lp-2":
			if r.TesseraLeafID != nil {
				t.Errorf("lp-2 TesseraLeafID = %v, want nil", *r.TesseraLeafID)
			}
			sawNil = true
		}
	}
	if !sawSet || !sawNil {
		t.Errorf("did not see both Valid + NULL TesseraLeafID branches (set=%v nil=%v)", sawSet, sawNil)
	}
}

func TestNewWithOptions(t *testing.T) {
	s := openMigratedStore(t)

	tess := stubTessera{}
	s3 := stubS3{}
	ls := stubLitestream{}
	ca := stubColdArchive{}

	a := New(s,
		WithTessera(tess),
		WithS3(s3),
		WithLitestream(ls),
		WithColdArchive(ca),
	)
	if a.tessera == nil || a.s3 == nil || a.litestream == nil || a.coldArchive == nil {
		t.Fatalf("Adapter optional fields not wired: %+v", a)
	}
}

type stubTessera struct{}

func (stubTessera) AppendLeaf(ctx context.Context, leaf tessera.Leaf) (tessera.LeafID, error) {
	return "leaf", nil
}

type stubS3 struct{}

func (stubS3) PutObject(ctx context.Context, bucket, key string, body []byte) error { return nil }
func (stubS3) GetObject(ctx context.Context, bucket, key string) ([]byte, error) {
	return nil, nil
}

type stubLitestream struct{}

func (stubLitestream) Status(ctx context.Context) (string, int64, error) { return "ok", 0, nil }

type stubColdArchive struct{}

func (stubColdArchive) Archive(ctx context.Context, partitionID string) (string, string, error) {
	return "url", "hash", nil
}

func TestAdapterStoreErrorPathsWrapped(t *testing.T) {
	s := openMigratedStore(t)
	a := New(s)
	ctx := context.Background()

	insertRawAuditEvent(t, s, "pre-seed", "p", "t", `{}`, 1700000000)
	if _, err := a.OnEmitRaw(ctx, "pre-seed", "p", "t", []byte(`{}`), 1700000000); err != nil {
		t.Fatalf("OnEmitRaw pre-seed: %v", err)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	cases := []struct {
		name string
		fn   func() error
	}{
		{"GetChainTip", func() error { _, err := a.GetChainTip(ctx); return err }},
		{"GetEventByID", func() error { _, err := a.GetEventByID(ctx, "x"); return err }},
		{"UpdateChainColumns", func() error { return a.UpdateChainColumns(ctx, "x", "", "", "p") }},
		{"UpdateTesseraLeafID", func() error { return a.UpdateTesseraLeafID(ctx, "x", "leaf") }},
		{"InsertPartitionSeal", func() error {
			return a.InsertPartitionSeal(ctx, chain.SealRecord{PartitionID: "p", FinalRecordHash: "x"})
		}},
		{"GetPartitionSeal", func() error { _, err := a.GetPartitionSeal(ctx, "p"); return err }},
		{"ListPartitions", func() error { _, err := a.ListPartitions(ctx); return err }},
		{"ListEventsForPartition", func() error { _, err := a.ListEventsForPartition(ctx, "p"); return err }},
		{"BackfillScan", func() error { _, err := a.BackfillScan(ctx, 0, 10); return err }},
		{"OnEmitRaw_GetChainTipFails", func() error {
			_, err := a.OnEmitRaw(ctx, "x", "p", "t", []byte(`{}`), 1700000000)
			return err
		}},
	}
	for _, c := range cases {
		err := c.fn()
		if err == nil {
			t.Errorf("%s: expected error after Close, got nil", c.name)
			continue
		}

		if errors.Is(err, chain.ErrNoChainTip) ||
			errors.Is(err, chain.ErrEventNotFound) ||
			errors.Is(err, chain.ErrPartitionSealNotFound) {
			t.Errorf("%s: post-Close error returned chain sentinel %v; want generic wrap", c.name, err)
		}
	}
}

func TestAdapterOnEmitRawUpdateChainColumnsErrorBubbles(t *testing.T) {
	s := openMigratedStore(t)
	a := New(s)
	ctx := context.Background()

	insertRawAuditEvent(t, s, "seed", "p", "t", `{}`, 1700000000)
	if _, err := a.OnEmitRaw(ctx, "seed", "p", "t", []byte(`{}`), 1700000000); err != nil {
		t.Fatalf("OnEmitRaw seed: %v", err)
	}

	_, err := a.OnEmitRaw(ctx, "nonexistent-id", "p", "t", []byte(`{}`), 1700000001)
	if err == nil {
		t.Fatal("expected UpdateChainColumns error for missing row, got nil")
	}
}

func TestAdapterOnEmitRawComputeErrorBubbles(t *testing.T) {
	s := openMigratedStore(t)
	a := New(s)
	ctx := context.Background()

	_, err := a.OnEmitRaw(ctx, "evt-X", "p", "", []byte(`{}`), 1700000000)
	if err == nil {
		t.Fatal("expected compute error, got nil")
	}

	if msg := err.Error(); msg == "" {
		t.Fatal("error message empty")
	}
}
