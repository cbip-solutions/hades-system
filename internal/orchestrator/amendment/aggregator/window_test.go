package aggregator_test

import (
	"fmt"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/amendment/aggregator"
)

func TestWindowStateRecordsSessionAndCounts(t *testing.T) {
	w := aggregator.NewWindowState(20)
	now := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)

	w.Record(aggregator.SessionRecord{
		SessionID: "s-1", Anomaly: false, Timestamp: now, SourceADR: "ADR-0024",
	})
	w.Record(aggregator.SessionRecord{
		SessionID: "s-2", Anomaly: true, Timestamp: now.Add(time.Minute), SourceADR: "ADR-0024",
	})

	pct, total, last := w.Evaluate(20)
	if total != 2 {
		t.Errorf("total=%d, want 2", total)
	}
	wantPct := 0.5
	if pct != wantPct {
		t.Errorf("pct=%v, want %v", pct, wantPct)
	}
	if last != "ADR-0024" {
		t.Errorf("lastApplied=%q, want ADR-0024", last)
	}
}

func TestWindowStateBoundedDropsOldest(t *testing.T) {
	w := aggregator.NewWindowState(3)
	now := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)

	for i := 0; i < 5; i++ {
		w.Record(aggregator.SessionRecord{
			SessionID: fmt.Sprintf("s-%d", i),
			Anomaly:   i%2 == 0,
			Timestamp: now.Add(time.Duration(i) * time.Minute),
			SourceADR: "ADR-0025",
		})
	}

	pct, total, last := w.Evaluate(3)
	if total != 3 {
		t.Errorf("total=%d, want 3 (bounded buffer)", total)
	}
	wantPct := 1.0 / 3.0
	if math.Abs(pct-wantPct) > 1e-9 {
		t.Errorf("pct=%v, want %v (1 of 3 passing after FIFO drop)", pct, wantPct)
	}
	if last != "ADR-0025" {
		t.Errorf("lastApplied=%q, want ADR-0025", last)
	}
}

func TestWindowStateEvaluateBelowMinSessionsReturnsTotal(t *testing.T) {
	w := aggregator.NewWindowState(20)
	now := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)

	for i := 0; i < 5; i++ {
		w.Record(aggregator.SessionRecord{
			SessionID: fmt.Sprintf("s-%d", i),
			Anomaly:   true,
			Timestamp: now.Add(time.Duration(i) * time.Minute),
			SourceADR: "ADR-0026",
		})
	}

	pct, total, last := w.Evaluate(20)
	if total != 5 {
		t.Errorf("total=%d, want 5 (caller sees the truth)", total)
	}
	if pct != 0.0 {
		t.Errorf("pct=%v, want 0.0 (all 5 anomalies)", pct)
	}
	if last != "ADR-0026" {
		t.Errorf("lastApplied=%q, want ADR-0026", last)
	}
}

func TestWindowStateLastAppliedTracksMostRecentNonEmpty(t *testing.T) {
	w := aggregator.NewWindowState(20)
	now := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)

	w.Record(aggregator.SessionRecord{SessionID: "s-1", Timestamp: now, SourceADR: "ADR-0024"})
	w.Record(aggregator.SessionRecord{SessionID: "s-2", Timestamp: now.Add(time.Minute), SourceADR: ""})
	w.Record(aggregator.SessionRecord{SessionID: "s-3", Timestamp: now.Add(2 * time.Minute), SourceADR: "ADR-0025"})
	w.Record(aggregator.SessionRecord{SessionID: "s-4", Timestamp: now.Add(3 * time.Minute), SourceADR: ""})

	_, _, last := w.Evaluate(20)
	if last != "ADR-0025" {
		t.Errorf("lastApplied=%q, want ADR-0025 (most recent non-empty SourceADR)", last)
	}
}

func TestWindowStateEmptyEvaluateNoBreach(t *testing.T) {
	w := aggregator.NewWindowState(10)
	pct, total, last := w.Evaluate(10)
	if pct != 1.0 || total != 0 || last != "" {
		t.Errorf("Evaluate empty=(%v, %d, %q), want (1.0, 0, \"\")", pct, total, last)
	}
}

func TestWindowStateClampsBoundToOne(t *testing.T) {
	for _, b := range []int{0, -1, -100} {
		w := aggregator.NewWindowState(b)
		w.Record(aggregator.SessionRecord{SessionID: "s-1", Timestamp: time.Now()})
		w.Record(aggregator.SessionRecord{SessionID: "s-2", Timestamp: time.Now()})
		_, total, _ := w.Evaluate(1)
		if total != 1 {
			t.Errorf("bound=%d clamp not applied: total=%d, want 1", b, total)
		}
	}
}

func TestWindowStateSnapshotIsIndependent(t *testing.T) {
	w := aggregator.NewWindowState(5)
	w.Record(aggregator.SessionRecord{SessionID: "s-1", Anomaly: true, Timestamp: time.Now(), SourceADR: "ADR-A"})
	snap := w.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("snap len=%d, want 1", len(snap))
	}
	snap[0].SourceADR = "ADR-MUTATED"
	_, _, last := w.Evaluate(5)
	if last != "ADR-A" {
		t.Errorf("internal state mutated via Snapshot: lastApplied=%q, want ADR-A", last)
	}
}

func TestWindowStateConcurrentRecordEvaluate(t *testing.T) {
	w := aggregator.NewWindowState(100)
	now := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			w.Record(aggregator.SessionRecord{
				SessionID: fmt.Sprintf("s-%d", i),
				Anomaly:   i%3 == 0,
				Timestamp: now.Add(time.Duration(i) * time.Millisecond),
				SourceADR: "ADR-0027",
			})
		}
	}()
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_, _, _ = w.Evaluate(100)
			}
		}()
	}
	time.Sleep(50 * time.Millisecond)
	close(stop)
	wg.Wait()
}
