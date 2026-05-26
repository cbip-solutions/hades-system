package chain

import (
	"context"
	"errors"
	"testing"
)

func TestWalkEmptyChain(t *testing.T) {
	fs := newFakeStore()
	report, err := Walk(context.Background(), fs, "p1")
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if report.EventsWalked != 0 {
		t.Errorf("EventsWalked = %d, want 0", report.EventsWalked)
	}
	if len(report.Tampered) != 0 {
		t.Errorf("Tampered = %v, want empty", report.Tampered)
	}
}

func TestWalkValidChain(t *testing.T) {
	fs := newFakeStore()
	seedPartitionEvents(t, fs, "p1", "2023_11", 3, 1700000000)
	report, err := Walk(context.Background(), fs, "p1")
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if report.EventsWalked != 3 {
		t.Errorf("EventsWalked = %d, want 3", report.EventsWalked)
	}
	if len(report.Tampered) != 0 {
		t.Errorf("clean chain reported tamper: %+v", report.Tampered)
	}
	if len(report.GapsDetected) != 0 {
		t.Errorf("clean chain reported gap: %+v", report.GapsDetected)
	}
}

func TestWalkDetectsTamperedRecordHash(t *testing.T) {
	fs := newFakeStore()
	seedPartitionEvents(t, fs, "p1", "2023_11", 3, 1700000000)

	for _, id := range fs.insertOrder {
		if id == "evt-p1-2023_11-b" {
			fs.events[id].RecordHash = "tampered_hash_value_padded_to_64_chars_aaaaaaaaaaaaaaaaaaaaaaaa"
			break
		}
	}
	report, err := Walk(context.Background(), fs, "p1")
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(report.Tampered) != 1 {
		t.Errorf("len(Tampered) = %d, want 1", len(report.Tampered))
	}
	if report.Tampered[0].EventID != "evt-p1-2023_11-b" {
		t.Errorf("Tampered[0].EventID = %q", report.Tampered[0].EventID)
	}
}

func TestWalkDetectsChainGap(t *testing.T) {
	fs := newFakeStore()
	seedPartitionEvents(t, fs, "p1", "2023_11", 3, 1700000000)

	for _, id := range fs.insertOrder {
		if id == "evt-p1-2023_11-c" {
			fs.events[id].PrevHash = "broken_link_padded_to_64_chars_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			break
		}
	}
	report, err := Walk(context.Background(), fs, "p1")
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(report.GapsDetected) != 1 {
		t.Errorf("len(GapsDetected) = %d, want 1", len(report.GapsDetected))
	}
	if report.GapsDetected[0].EventID != "evt-p1-2023_11-c" {
		t.Errorf("Gap[0].EventID = %q", report.GapsDetected[0].EventID)
	}
}

func TestWalkAccumulatesAllTampers(t *testing.T) {

	fs := newFakeStore()
	seedPartitionEvents(t, fs, "p1", "2023_11", 3, 1700000000)

	for _, id := range fs.insertOrder {
		if id == "evt-p1-2023_11-a" || id == "evt-p1-2023_11-c" {
			fs.events[id].RecordHash = "tampered_padded_to_64_chars_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		}
	}
	report, err := Walk(context.Background(), fs, "p1")
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(report.Tampered) != 2 {
		t.Errorf("len(Tampered) = %d, want 2 (forensic accumulation)", len(report.Tampered))
	}
}

func TestWalkPerProjectIsolation(t *testing.T) {

	fs := newFakeStore()
	seedPartitionEvents(t, fs, "pA", "2023_11", 2, 1700000000)
	seedPartitionEvents(t, fs, "pB", "2023_11", 2, 1700200000)

	for _, id := range fs.insertOrder {
		if id == "evt-pA-2023_11-a" {
			fs.events[id].RecordHash = "tampered_padded_to_64_chars_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		}
	}
	reportA, _ := Walk(context.Background(), fs, "pA")
	reportB, _ := Walk(context.Background(), fs, "pB")
	if len(reportA.Tampered) == 0 {
		t.Error("project A should detect tamper")
	}
	if len(reportB.Tampered) != 0 {
		t.Errorf("project B reported tamper despite isolation: %+v", reportB.Tampered)
	}
}

func TestWalkMultiPartition(t *testing.T) {
	fs := newFakeStore()
	seedPartitionEvents(t, fs, "p1", "2023_11", 2, 1700000000)

	seedPartitionEvents(t, fs, "p1", "2024_01", 2, 1704067200)
	report, err := Walk(context.Background(), fs, "p1")
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if report.PartitionsWalked != 2 {
		t.Errorf("PartitionsWalked = %d, want 2", report.PartitionsWalked)
	}
	if report.EventsWalked != 4 {
		t.Errorf("EventsWalked = %d, want 4", report.EventsWalked)
	}
}

func TestWalkContextCancelled(t *testing.T) {
	fs := newFakeStore()
	seedPartitionEvents(t, fs, "p1", "2023_11", 5, 1700000000)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Walk(ctx, fs, "p1")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("want context.Canceled, got %v", err)
	}
}

func TestNewWalkerAndReceiverDelegate(t *testing.T) {
	fs := newFakeStore()
	seedPartitionEvents(t, fs, "p1", "2023_11", 2, 1700000000)
	w := NewWalker(fs)
	if w == nil {
		t.Fatal("NewWalker returned nil")
	}
	report, err := w.Walk(context.Background(), "p1")
	if err != nil {
		t.Fatalf("Walker.Walk: %v", err)
	}
	if report.EventsWalked != 2 {
		t.Errorf("EventsWalked = %d, want 2", report.EventsWalked)
	}
}

func TestWalkListPartitionsErrorBubbles(t *testing.T) {
	fs := newFakeStore()
	seedPartitionEvents(t, fs, "p1", "2023_11", 1, 1700000000)
	fs.listPartsErr = errBoom
	_, err := Walk(context.Background(), fs, "p1")
	if err == nil || !errors.Is(err, errBoom) {
		t.Fatalf("want wrapped errBoom, got %v", err)
	}
}

type listEventsErrStore struct {
	*fakeStore
	err error
}

func (l *listEventsErrStore) ListEventsForPartition(ctx context.Context, partitionID string) ([]EventRow, error) {
	return nil, l.err
}

func TestWalkListEventsErrorBubbles(t *testing.T) {
	fs := newFakeStore()
	seedPartitionEvents(t, fs, "p1", "2023_11", 1, 1700000000)
	wrapped := &listEventsErrStore{fakeStore: fs, err: errBoom}
	_, err := Walk(context.Background(), wrapped, "p1")
	if err == nil || !errors.Is(err, errBoom) {
		t.Fatalf("want wrapped errBoom from ListEventsForPartition, got %v", err)
	}
}

type cancelOnFirstPartitionStore struct {
	*fakeStore
	cancel context.CancelFunc
}

func (c *cancelOnFirstPartitionStore) ListPartitions(ctx context.Context) ([]PartitionStat, error) {
	parts, err := c.fakeStore.ListPartitions(ctx)
	c.cancel()
	return parts, err
}

func TestWalkContextCancelledMidLoop(t *testing.T) {
	fs := newFakeStore()

	seedPartitionEvents(t, fs, "p1", "2023_11", 1, 1700000000)
	seedPartitionEvents(t, fs, "p1", "2023_12", 1, 1701000000)
	ctx, cancel := context.WithCancel(context.Background())
	wrapped := &cancelOnFirstPartitionStore{fakeStore: fs, cancel: cancel}
	_, err := Walk(ctx, wrapped, "p1")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("want context.Canceled mid-loop, got %v", err)
	}
}

type cancelOnFirstListEventsStore struct {
	*fakeStore
	cancel context.CancelFunc
	once   bool
}

func (c *cancelOnFirstListEventsStore) ListEventsForPartition(ctx context.Context, partitionID string) ([]EventRow, error) {
	rows, err := c.fakeStore.ListEventsForPartition(ctx, partitionID)
	if !c.once {
		c.cancel()
		c.once = true
	}
	return rows, err
}

func TestWalkContextCancelledInsideEventsLoop(t *testing.T) {
	fs := newFakeStore()
	seedPartitionEvents(t, fs, "p1", "2023_11", 3, 1700000000)
	ctx, cancel := context.WithCancel(context.Background())
	wrapped := &cancelOnFirstListEventsStore{fakeStore: fs, cancel: cancel}
	_, err := Walk(ctx, wrapped, "p1")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("want context.Canceled inside events loop, got %v", err)
	}
}

func TestWalkSkipsPartitionWithNoMatchingProjectEvents(t *testing.T) {
	fs := newFakeStore()
	seedPartitionEvents(t, fs, "pA", "2023_11", 2, 1700000000)
	report, err := Walk(context.Background(), fs, "pZ")
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if report.PartitionsWalked != 0 {
		t.Errorf("PartitionsWalked = %d, want 0 (no pZ events anywhere)", report.PartitionsWalked)
	}
	if report.EventsWalked != 0 {
		t.Errorf("EventsWalked = %d, want 0", report.EventsWalked)
	}
}
