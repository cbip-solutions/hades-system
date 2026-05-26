package chain

import (
	"context"
	"errors"
	"testing"
)

var errBackfillBoom = errors.New("boom: injected backfill failure")

type backfillBoomStore struct {
	*fakeStore
	getChainTipErr    error
	backfillScanErr   error
	updateChainErr    error
	updateChainAfterN int
	updateChainCount  int
}

func newBackfillBoomStore() *backfillBoomStore {
	return &backfillBoomStore{fakeStore: newFakeStore()}
}

func (s *backfillBoomStore) GetChainTip(ctx context.Context) (string, error) {
	if s.getChainTipErr != nil {
		err := s.getChainTipErr
		s.getChainTipErr = nil
		return "", err
	}
	return s.fakeStore.GetChainTip(ctx)
}

func (s *backfillBoomStore) BackfillScan(ctx context.Context, afterRowID int64, limit int) ([]BackfillCursorRow, error) {
	if s.backfillScanErr != nil {
		err := s.backfillScanErr
		s.backfillScanErr = nil
		return nil, err
	}
	return s.fakeStore.BackfillScan(ctx, afterRowID, limit)
}

func (s *backfillBoomStore) UpdateChainColumns(ctx context.Context, id, prevHash, recordHash, partitionID string) error {
	if s.updateChainErr != nil {
		if s.updateChainCount >= s.updateChainAfterN {
			err := s.updateChainErr
			s.updateChainErr = nil
			return err
		}
		s.updateChainCount++
	}
	return s.fakeStore.UpdateChainColumns(ctx, id, prevHash, recordHash, partitionID)
}

func seedRawUnchainedEvents(t *testing.T, fs *fakeStore, projectID string, n int, baseTS int64) {
	t.Helper()
	for i := 0; i < n; i++ {
		id := "raw-" + projectID + "-" + string(rune('a'+i))
		row := &EventRow{
			ID:          id,
			ProjectID:   projectID,
			Type:        "raw.event",
			PayloadJSON: `{}`,
			EmittedAt:   baseTS + int64(i*86400),
		}
		fs.events[id] = row
		fs.insertOrder = append(fs.insertOrder, id)
	}
}

func TestBackfillEmptyTableNoOp(t *testing.T) {
	fs := newFakeStore()
	report, err := Backfill(context.Background(), fs, 100)
	if err != nil {
		t.Fatalf("Backfill: %v", err)
	}
	if report.RowsBackfilled != 0 {
		t.Errorf("RowsBackfilled = %d, want 0", report.RowsBackfilled)
	}
}

func TestBackfillSingleBatch(t *testing.T) {
	fs := newFakeStore()
	seedRawUnchainedEvents(t, fs, "p", 5, 1700000000)

	report, err := Backfill(context.Background(), fs, 1000)
	if err != nil {
		t.Fatalf("Backfill: %v", err)
	}
	if report.RowsBackfilled != 5 {
		t.Errorf("RowsBackfilled = %d, want 5", report.RowsBackfilled)
	}

	walkReport, _ := Walk(context.Background(), fs, "p")
	if len(walkReport.Tampered) != 0 || len(walkReport.GapsDetected) != 0 {
		t.Errorf("backfilled chain has tamper/gap: %+v", walkReport)
	}
	if walkReport.EventsWalked != 5 {
		t.Errorf("EventsWalked = %d, want 5", walkReport.EventsWalked)
	}
}

func TestBackfillMultipleBatches(t *testing.T) {
	fs := newFakeStore()
	seedRawUnchainedEvents(t, fs, "p", 10, 1700000000)

	report, err := Backfill(context.Background(), fs, 3)
	if err != nil {
		t.Fatalf("Backfill: %v", err)
	}
	if report.RowsBackfilled != 10 {
		t.Errorf("RowsBackfilled = %d, want 10", report.RowsBackfilled)
	}

	walkReport, _ := Walk(context.Background(), fs, "p")
	if len(walkReport.Tampered) != 0 {
		t.Errorf("backfilled chain tampered: %+v", walkReport.Tampered)
	}
}

func TestBackfillIdempotentReRun(t *testing.T) {
	fs := newFakeStore()
	seedRawUnchainedEvents(t, fs, "p", 4, 1700000000)

	report1, err := Backfill(context.Background(), fs, 100)
	if err != nil {
		t.Fatalf("first Backfill: %v", err)
	}

	preRecordHashes := map[string]string{}
	for _, id := range fs.insertOrder {
		preRecordHashes[id] = fs.events[id].RecordHash
	}

	report2, err := Backfill(context.Background(), fs, 100)
	if err != nil {
		t.Fatalf("second Backfill: %v", err)
	}

	if report1.RowsBackfilled != 4 {
		t.Errorf("first report.RowsBackfilled = %d, want 4", report1.RowsBackfilled)
	}
	if report2.RowsBackfilled != 0 {
		t.Errorf("idempotent re-run RowsBackfilled = %d, want 0", report2.RowsBackfilled)
	}

	for _, id := range fs.insertOrder {
		if fs.events[id].RecordHash != preRecordHashes[id] {
			t.Errorf("row %s record_hash changed across idempotent runs: pre=%s post=%s",
				id, preRecordHashes[id], fs.events[id].RecordHash)
		}
	}
}

func TestBackfillCrashRecovery(t *testing.T) {

	fs := newFakeStore()
	seedRawUnchainedEvents(t, fs, "p", 4, 1700000000)
	if _, err := Backfill(context.Background(), fs, 100); err != nil {
		t.Fatalf("first Backfill: %v", err)
	}

	for i := 0; i < 4; i++ {
		id := "raw-p-late-" + string(rune('a'+i))
		row := &EventRow{
			ID:          id,
			ProjectID:   "p",
			Type:        "raw.event",
			PayloadJSON: `{}`,
			EmittedAt:   1701000000 + int64(i*86400),
		}
		fs.events[id] = row
		fs.insertOrder = append(fs.insertOrder, id)
	}

	report, err := Backfill(context.Background(), fs, 100)
	if err != nil {
		t.Fatalf("recovery Backfill: %v", err)
	}
	if report.RowsBackfilled != 4 {
		t.Errorf("crash-recovery RowsBackfilled = %d, want 4 (only the new rows)",
			report.RowsBackfilled)
	}

	walkReport, _ := Walk(context.Background(), fs, "p")
	if len(walkReport.Tampered) != 0 || len(walkReport.GapsDetected) != 0 {
		t.Errorf("crash-recovery chain integrity broken: %+v", walkReport)
	}
}

func TestBackfillContextCancelled(t *testing.T) {
	fs := newFakeStore()
	seedRawUnchainedEvents(t, fs, "p", 100, 1700000000)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Backfill(ctx, fs, 10)
	if err == nil {
		t.Fatal("expected context.Canceled, got nil")
	}
}

func TestBackfillBatchSizeValidation(t *testing.T) {
	fs := newFakeStore()
	_, err := Backfill(context.Background(), fs, 0)
	if err == nil {
		t.Fatal("expected error for batch size 0")
	}
	_, err = Backfill(context.Background(), fs, -1)
	if err == nil {
		t.Fatal("expected error for negative batch size")
	}
}

// --- Adversarial coverage tests (no defer, doctrine bar = ≥95%). ---
// These exercise the wrapped-error bubble paths in backfill.go that the
// canonical 7 plan-file tests do not reach. Pattern mirrors seal_test.go's
// errBoom-driven adversarial suite (B-6).

func TestBackfillGetChainTipNonNoTipErrorBubbles(t *testing.T) {

	bs := newBackfillBoomStore()
	seedRawUnchainedEvents(t, bs.fakeStore, "p", 2, 1700000000)
	bs.getChainTipErr = errBackfillBoom
	_, err := Backfill(context.Background(), bs, 100)
	if err == nil || !errors.Is(err, errBackfillBoom) {
		t.Fatalf("want wrapped errBackfillBoom, got %v", err)
	}
}

func TestBackfillScanErrorBubbles(t *testing.T) {

	bs := newBackfillBoomStore()
	seedRawUnchainedEvents(t, bs.fakeStore, "p", 5, 1700000000)
	bs.backfillScanErr = errBackfillBoom
	_, err := Backfill(context.Background(), bs, 100)
	if err == nil || !errors.Is(err, errBackfillBoom) {
		t.Fatalf("want wrapped errBackfillBoom, got %v", err)
	}
}

func TestBackfillUpdateChainColumnsErrorBubbles(t *testing.T) {

	bs := newBackfillBoomStore()
	seedRawUnchainedEvents(t, bs.fakeStore, "p", 5, 1700000000)
	bs.updateChainErr = errBackfillBoom
	bs.updateChainAfterN = 2
	report, err := Backfill(context.Background(), bs, 100)
	if err == nil || !errors.Is(err, errBackfillBoom) {
		t.Fatalf("want wrapped errBackfillBoom, got %v", err)
	}

	if report.RowsBackfilled != 2 {
		t.Errorf("RowsBackfilled = %d, want 2 (partial progress before failure)",
			report.RowsBackfilled)
	}
}

func TestBackfillContextCancelledMidBatch(t *testing.T) {

	bs := newBackfillBoomStore()
	seedRawUnchainedEvents(t, bs.fakeStore, "p", 10, 1700000000)
	ctx, cancel := context.WithCancel(context.Background())
	cs := &cancelOnUpdateStore{backfillBoomStore: bs, cancel: cancel, after: 1}
	_, err := Backfill(ctx, cs, 100)
	if err == nil {
		t.Fatal("expected context.Canceled mid-batch, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("want context.Canceled, got %v", err)
	}
}

type cancelOnUpdateStore struct {
	*backfillBoomStore
	cancel context.CancelFunc
	after  int
	count  int
}

func (s *cancelOnUpdateStore) UpdateChainColumns(ctx context.Context, id, prevHash, recordHash, partitionID string) error {
	err := s.backfillBoomStore.UpdateChainColumns(ctx, id, prevHash, recordHash, partitionID)
	if err == nil {
		s.count++
		if s.count == s.after {
			s.cancel()
		}
	}
	return err
}

func TestBackfillComputeErrorBubbles(t *testing.T) {

	bs := newBackfillBoomStore()
	cs := &computePoisonScanStore{backfillBoomStore: bs}
	_, err := Backfill(context.Background(), cs, 100)
	if err == nil {
		t.Fatal("expected Compute error to bubble; got nil")
	}
	if !errors.Is(err, ErrInvalidTimestamp) {
		t.Errorf("want wrapped ErrInvalidTimestamp, got %v", err)
	}
}

type computePoisonScanStore struct {
	*backfillBoomStore
	scanned bool
}

func (s *computePoisonScanStore) BackfillScan(ctx context.Context, afterRowID int64, limit int) ([]BackfillCursorRow, error) {
	if s.scanned {
		return nil, nil
	}
	s.scanned = true
	return []BackfillCursorRow{
		{
			RowID: 1,
			EventRow: EventRow{
				ID:          "poison",
				ProjectID:   "p",
				Type:        "raw.event",
				PayloadJSON: `{}`,
				EmittedAt:   0,
			},
		},
	}, nil
}
