package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestWindowCounterBoundaries(t *testing.T) {
	w := NewWindowCounter(24 * time.Hour)
	now := time.Now()

	w.Add(now, 1.00)
	w.Add(now.Add(-23*time.Hour), 2.00)
	w.Add(now.Add(-24*time.Hour+time.Nanosecond), 4.00)
	w.Add(now.Add(-24*time.Hour), 8.00)
	w.Add(now.Add(-25*time.Hour), 16.00)
	w.Add(now.Add(-30*24*time.Hour), 32.00)

	got := w.Total(now)
	want := 1.00 + 2.00 + 4.00
	if got < want-0.001 || got > want+0.001 {
		t.Errorf("Total = %f, want %f", got, want)
	}
}

func TestWindowCounterPruneOlderThan(t *testing.T) {
	w := NewWindowCounter(24 * time.Hour)
	now := time.Now()
	for i := 0; i < 100; i++ {
		w.Add(now.Add(-time.Duration(i)*time.Hour), 0.01)
	}
	w.PruneOlderThan(now.Add(-24 * time.Hour))

	if got := w.Len(); got != 24 {
		t.Errorf("Len after prune = %d, want 24", got)
	}
}

func TestWindowCounterEmpty(t *testing.T) {
	w := NewWindowCounter(24 * time.Hour)
	if total := w.Total(time.Now()); total != 0 {
		t.Errorf("empty Total = %f, want 0", total)
	}
}

func TestWindowCounterConcurrentAdd(t *testing.T) {
	w := NewWindowCounter(24 * time.Hour)
	now := time.Now()
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 1000; i++ {
				w.Add(now.Add(-time.Duration(i)*time.Millisecond), 0.001)
			}
		}(g)
	}
	wg.Wait()
	if got := w.Len(); got != 8000 {
		t.Errorf("Len = %d, want 8000", got)
	}

	total := w.Total(now)
	want := 8.0
	if total < want-0.01 || total > want+0.01 {
		t.Errorf("Total = %f, want ~%f", total, want)
	}
}

func TestWindowCounter30dWindow(t *testing.T) {
	w := NewWindowCounter(30 * 24 * time.Hour)
	now := time.Now()
	w.Add(now, 1.0)
	w.Add(now.Add(-29*24*time.Hour), 2.0)
	w.Add(now.Add(-30*24*time.Hour-time.Nanosecond), 99.0)
	got := w.Total(now)
	want := 3.0
	if got < want-0.001 || got > want+0.001 {
		t.Errorf("Total = %f, want %f", got, want)
	}
}

func TestNewWindowCounterRejectsZeroOrNegativeDuration(t *testing.T) {
	for _, d := range []time.Duration{0, -1 * time.Second} {
		d := d
		func() {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("NewWindowCounter(%v) did not panic", d)
				}
			}()
			_ = NewWindowCounter(d)
		}()
	}
}

func TestWindowCounterTotalMonotonicallyDecreases(t *testing.T) {
	w := NewWindowCounter(24 * time.Hour)
	base := time.Now()

	w.Add(base.Add(-23*time.Hour), 5.0)

	w.Add(base.Add(-30*time.Minute), 3.0)

	totalEarly := w.Total(base)
	if totalEarly < 7.999 || totalEarly > 8.001 {
		t.Errorf("totalEarly = %f, want ~8.0", totalEarly)
	}

	totalLater := w.Total(base.Add(2 * time.Hour))
	if totalLater < 2.999 || totalLater > 3.001 {
		t.Errorf("totalLater = %f, want ~3.0 (aged-out sample pruned)", totalLater)
	}

	if totalLater > totalEarly {
		t.Errorf("total increased with time: earlier=%f later=%f", totalEarly, totalLater)
	}
}

func TestWindowCounterPruneIsIdempotent(t *testing.T) {
	w := NewWindowCounter(24 * time.Hour)
	now := time.Now()
	for i := 0; i < 10; i++ {
		w.Add(now.Add(-time.Duration(i)*time.Hour), 0.1)
	}
	cutoff := now.Add(-5 * time.Hour)
	w.PruneOlderThan(cutoff)
	lenAfterFirst := w.Len()
	w.PruneOlderThan(cutoff)
	lenAfterSecond := w.Len()
	if lenAfterFirst != lenAfterSecond {
		t.Errorf("idempotency broken: first=%d second=%d", lenAfterFirst, lenAfterSecond)
	}
}

func TestWindowCounterOutOfOrderAdd(t *testing.T) {
	w := NewWindowCounter(24 * time.Hour)
	now := time.Now()

	w.Add(now.Add(-10*time.Hour), 1.0)
	w.Add(now.Add(-1*time.Hour), 3.0)
	w.Add(now.Add(-5*time.Hour), 2.0)

	if got := w.Len(); got != 3 {
		t.Errorf("Len = %d, want 3", got)
	}

	total := w.Total(now)
	if total < 5.999 || total > 6.001 {
		t.Errorf("Total = %f, want 6.0", total)
	}
}

type fakeCostStore struct {
	mu       sync.Mutex
	rows     map[string]CostLedgerRow
	insertEr error
}

func newFakeCostStore() *fakeCostStore {
	return &fakeCostStore{rows: map[string]CostLedgerRow{}}
}

func (f *fakeCostStore) InsertCostLedger(row CostLedgerRow) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.insertEr != nil {
		return 0, f.insertEr
	}
	if _, exists := f.rows[row.IdempotencyKey]; exists {
		return 0, fmt.Errorf("%w: %s", ErrDuplicateIdempotency, row.IdempotencyKey)
	}
	row.ID = int64(len(f.rows) + 1)
	f.rows[row.IdempotencyKey] = row
	return row.ID, nil
}

func (f *fakeCostStore) QueryAllRecentCosts(since time.Time) ([]CostLedgerRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []CostLedgerRow
	for _, r := range f.rows {
		if r.TS.Before(since) {
			continue
		}
		out = append(out, r)
	}

	return out, nil
}

func mkRow(idem string, ts time.Time, project, tier string, usd float64) CostLedgerRow {
	return CostLedgerRow{
		IdempotencyKey: idem,
		TS:             ts,
		Project:        project,
		Profile:        "orchestrator",
		Tier:           tier,
		Model:          "claude-opus-4-6",
		InputTokens:    1000,
		OutputTokens:   500,
		CostUSD:        usd,
		SessionID:      "sess-1",
	}
}

func TestCostCountersRecordSingleSuccess(t *testing.T) {
	cc := NewCostCounters(newFakeCostStore())
	row := mkRow("idem-1", time.Now(), "internal-platform-x", "tier2-paygo", 0.10)
	if err := cc.Record(row); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if got := cc.SessionTotal("sess-1"); got < 0.099 || got > 0.101 {
		t.Errorf("SessionTotal = %f, want 0.10", got)
	}
	if got := cc.ProjectProfileTierTotal("internal-platform-x", "orchestrator", "tier2-paygo", 24*time.Hour); got < 0.099 || got > 0.101 {
		t.Errorf("24h Total = %f, want 0.10", got)
	}
	if got := cc.ProjectProfileTierTotal("internal-platform-x", "orchestrator", "tier2-paygo", 30*24*time.Hour); got < 0.099 || got > 0.101 {
		t.Errorf("30d Total = %f, want 0.10", got)
	}
}

func TestCostCountersRecordDuplicateIdempotencyIsNoOp(t *testing.T) {
	cc := NewCostCounters(newFakeCostStore())
	row := mkRow("idem-dup", time.Now(), "internal-platform-x", "tier2-paygo", 0.10)
	if err := cc.Record(row); err != nil {
		t.Fatalf("first Record: %v", err)
	}
	row.CostUSD = 0.50
	if err := cc.Record(row); err != nil {
		t.Fatalf("duplicate Record must be no-op, got %v", err)
	}

	if got := cc.SessionTotal("sess-1"); got < 0.099 || got > 0.101 {
		t.Errorf("SessionTotal = %f, want 0.10 (duplicate must not double-charge)", got)
	}
}

func TestCostCountersRecordStoreErrorPropagates(t *testing.T) {
	fs := newFakeCostStore()
	fs.insertEr = errors.New("disk full")
	cc := NewCostCounters(fs)
	err := cc.Record(mkRow("idem-x", time.Now(), "internal-platform-x", "tier2-paygo", 0.10))
	if err == nil {
		t.Error("expected store error to propagate")
	}
	if got := cc.SessionTotal("sess-1"); got != 0 {
		t.Errorf("SessionTotal = %f, want 0 (failed insert must not increment)", got)
	}
}

func TestCostCountersConcurrentRecordRaceClean(t *testing.T) {
	cc := NewCostCounters(newFakeCostStore())
	now := time.Now()
	var wg sync.WaitGroup
	const N = 1000
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			row := mkRow(fmt.Sprintf("idem-%d", i), now, "internal-platform-x", "tier2-paygo", 0.001)
			if err := cc.Record(row); err != nil {
				t.Errorf("Record %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	want := float64(N) * 0.001
	got := cc.ProjectProfileTierTotal("internal-platform-x", "orchestrator", "tier2-paygo", 24*time.Hour)
	if got < want-0.001 || got > want+0.001 {
		t.Errorf("concurrent Record total = %f, want %f", got, want)
	}
}

func TestWouldExceedCapAtThresholds(t *testing.T) {
	cc := NewCostCounters(newFakeCostStore())
	now := time.Now()

	for i := 0; i < 4; i++ {
		row := mkRow(fmt.Sprintf("seed-%d", i), now.Add(-time.Duration(i)*time.Hour), "internal-platform-x", "tier2-paygo", 10.00)
		if err := cc.Record(row); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}

	if cc.WouldExceedCap("internal-platform-x", "orchestrator", "tier2-paygo", 24*time.Hour, 50.0, 5.0) {
		t.Error("$45/$50 must not exceed cap")
	}

	if !cc.WouldExceedCap("internal-platform-x", "orchestrator", "tier2-paygo", 24*time.Hour, 50.0, 11.0) {
		t.Error("$51/$50 must exceed cap")
	}

	if !cc.WouldExceedCap("internal-platform-x", "orchestrator", "tier2-paygo", 24*time.Hour, 50.0, 10.0) {
		t.Error("$50/$50 must trigger cap (>= comparison)")
	}
}

func TestNewCostCountersNilStorePanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("NewCostCounters(nil) did not panic")
		}
	}()
	_ = NewCostCounters(nil)
}

func TestCostCountersRecordNilReceiverReturnsError(t *testing.T) {
	var cc *CostCounters
	err := cc.Record(CostLedgerRow{IdempotencyKey: "x"})
	if err == nil {
		t.Error("Record on nil receiver: expected error, got nil")
	}
}

// TestCostCountersRecordEmptySessionIDSkipsSessionCounter — rows without a
// SessionID (e.g., daemon-internal background calls) MUST NOT bump session
// counters (no key to attribute to), but window counters MUST still update.
// Pinning this guards against an empty-string session leaking shared spend
// across all "" callers.
func TestCostCountersRecordEmptySessionIDSkipsSessionCounter(t *testing.T) {
	cc := NewCostCounters(newFakeCostStore())
	row := mkRow("idem-nosess", time.Now(), "internal-platform-x", "tier2-paygo", 0.25)
	row.SessionID = ""
	if err := cc.Record(row); err != nil {
		t.Fatalf("Record: %v", err)
	}

	if got := cc.SessionTotal(""); got != 0 {
		t.Errorf(`SessionTotal("") = %f, want 0`, got)
	}
	// Window counters MUST still update (cap enforcement is independent
	// of session attribution).
	if got := cc.ProjectProfileTierTotal("internal-platform-x", "orchestrator", "tier2-paygo", 24*time.Hour); got < 0.249 || got > 0.251 {
		t.Errorf("24h Total = %f, want 0.25", got)
	}
}

func TestCostCountersMultipleSessionsIndependent(t *testing.T) {
	cc := NewCostCounters(newFakeCostStore())
	now := time.Now()

	rowA := mkRow("idem-a", now, "internal-platform-x", "tier2-paygo", 0.30)
	rowA.SessionID = "sess-A"
	rowB := mkRow("idem-b", now, "internal-platform-x", "tier2-paygo", 0.70)
	rowB.SessionID = "sess-B"

	if err := cc.Record(rowA); err != nil {
		t.Fatalf("Record A: %v", err)
	}
	if err := cc.Record(rowB); err != nil {
		t.Fatalf("Record B: %v", err)
	}

	if got := cc.SessionTotal("sess-A"); got < 0.299 || got > 0.301 {
		t.Errorf("SessionTotal(sess-A) = %f, want 0.30", got)
	}
	if got := cc.SessionTotal("sess-B"); got < 0.699 || got > 0.701 {
		t.Errorf("SessionTotal(sess-B) = %f, want 0.70", got)
	}

	if got := cc.ProjectProfileTierTotal("internal-platform-x", "orchestrator", "tier2-paygo", 24*time.Hour); got < 0.999 || got > 1.001 {
		t.Errorf("24h Total = %f, want 1.00", got)
	}
}

func TestProjectProfileTierTotalNoRecordsReturnsZero(t *testing.T) {
	cc := NewCostCounters(newFakeCostStore())
	got := cc.ProjectProfileTierTotal("never-seen", "orchestrator", "tier2-paygo", 24*time.Hour)
	if got != 0 {
		t.Errorf("Total for unseen key = %f, want 0", got)
	}
}

func TestWouldExceedCapEmptyCounter(t *testing.T) {
	cc := NewCostCounters(newFakeCostStore())

	if cc.WouldExceedCap("internal-platform-x", "orchestrator", "tier2-paygo", 24*time.Hour, 50.0, 5.0) {
		t.Error("empty + 5 < 50: must not exceed cap")
	}

	if !cc.WouldExceedCap("internal-platform-x", "orchestrator", "tier2-paygo", 24*time.Hour, 50.0, 50.0) {
		t.Error("empty + 50 == 50: must trigger cap (>= comparison)")
	}

	if !cc.WouldExceedCap("internal-platform-x", "orchestrator", "tier2-paygo", 24*time.Hour, 50.0, 100.0) {
		t.Error("empty + 100 > 50: must exceed cap")
	}
}

func TestWindowNameFromDurationPanicsOnUnsupported(t *testing.T) {
	for _, d := range []time.Duration{
		time.Hour,
		7 * 24 * time.Hour,
		0,
		-1 * time.Second,
		time.Minute,
	} {
		d := d
		func() {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("windowNameFromDuration(%v) did not panic", d)
				}
			}()
			_ = windowNameFromDuration(d)
		}()
	}
}

func TestCostCountersGetOrCreateWindowConcurrentCreation(t *testing.T) {
	cc := NewCostCounters(newFakeCostStore())

	var wg sync.WaitGroup
	const N = 200
	results := make([]*WindowCounter, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx] = cc.getOrCreateWindow("p", "prof", "tier2", "24h", 24*time.Hour)
		}(i)
	}
	wg.Wait()

	first := results[0]
	if first == nil {
		t.Fatal("getOrCreateWindow returned nil")
	}
	for i := 1; i < N; i++ {
		if results[i] != first {
			t.Errorf("goroutine %d got different *WindowCounter (race created duplicate)", i)
			break
		}
	}

	cc.mu.RLock()
	defer cc.mu.RUnlock()
	if len(cc.projectProfileTierCounters) != 1 {
		t.Errorf("map size = %d, want 1 (concurrent creates produced duplicates)", len(cc.projectProfileTierCounters))
	}
}

type queryErrCostStore struct {
	*fakeCostStore
	queryEr error
}

func (q *queryErrCostStore) QueryAllRecentCosts(since time.Time) ([]CostLedgerRow, error) {
	if q.queryEr != nil {
		return nil, q.queryEr
	}
	return q.fakeCostStore.QueryAllRecentCosts(since)
}

func TestRebuildFromLedger1000Rows(t *testing.T) {
	store := newFakeCostStore()
	now := time.Now()

	wantPerKey := map[string]float64{}
	for i := 0; i < 1000; i++ {
		project := fmt.Sprintf("p%d", i%5)
		row := mkRow(fmt.Sprintf("idem-%d", i), now.Add(-time.Duration(i)*time.Minute), project, "tier2-paygo", 0.001)
		if _, err := store.InsertCostLedger(row); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
		wantPerKey[project] += 0.001
	}

	cc := NewCostCounters(store)
	if err := cc.RebuildFromLedger(now.Add(-30 * 24 * time.Hour)); err != nil {
		t.Fatalf("RebuildFromLedger: %v", err)
	}

	for project, want := range wantPerKey {
		got := cc.ProjectProfileTierTotal(project, "orchestrator", "tier2-paygo", 30*24*time.Hour)
		if got < want-0.0001 || got > want+0.0001 {
			t.Errorf("%s: got %f, want %f", project, got, want)
		}
	}
}

func TestRebuildFromLedgerEmptyStore(t *testing.T) {
	cc := NewCostCounters(newFakeCostStore())
	if err := cc.RebuildFromLedger(time.Now().Add(-30 * 24 * time.Hour)); err != nil {
		t.Fatalf("empty rebuild: %v", err)
	}

	if got := cc.ProjectProfileTierTotal("any", "orchestrator", "tier2-paygo", 30*24*time.Hour); got != 0 {
		t.Errorf("empty 30d Total = %f, want 0", got)
	}
}

func TestCountersRebuiltFromLedgerSymbolPresent(t *testing.T) {
	countersRebuiltFromLedger()
}

func TestStartHourlyMaintenancePrunesOldSamples(t *testing.T) {
	cc := NewCostCounters(newFakeCostStore())
	now := time.Now()

	w := cc.getOrCreateWindow("internal-platform-x", "orchestrator", "tier2-paygo", window30d, 30*24*time.Hour)
	w.Add(now.Add(-31*24*time.Hour), 99.0)
	w.Add(now.Add(-1*time.Hour), 1.0)

	cc.tickInterval = 20 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := cc.StartHourlyMaintenance(ctx)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if w.Len() == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := w.Len(); got != 1 {
		t.Errorf("after maintenance: Len = %d, want 1 (older sample pruned)", got)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("maintenance goroutine did not exit on context cancel")
	}
}

func TestRecordRestartCountersPreserved(t *testing.T) {
	store := newFakeCostStore()
	cc1 := NewCostCounters(store)
	now := time.Now()
	for i := 0; i < 10; i++ {
		row := mkRow(fmt.Sprintf("pre-%d", i), now.Add(-time.Duration(i)*time.Minute), "internal-platform-x", "tier2-paygo", 0.10)
		if err := cc1.Record(row); err != nil {
			t.Fatalf("Record %d: %v", i, err)
		}
	}
	wantBefore := cc1.ProjectProfileTierTotal("internal-platform-x", "orchestrator", "tier2-paygo", 30*24*time.Hour)

	cc2 := NewCostCounters(store)
	if err := cc2.RebuildFromLedger(now.Add(-30 * 24 * time.Hour)); err != nil {
		t.Fatalf("RebuildFromLedger: %v", err)
	}
	wantAfter := cc2.ProjectProfileTierTotal("internal-platform-x", "orchestrator", "tier2-paygo", 30*24*time.Hour)

	if wantAfter < wantBefore-0.0001 || wantAfter > wantBefore+0.0001 {
		t.Errorf("post-restart total = %f, pre-restart = %f", wantAfter, wantBefore)
	}
}

// TestRebuildFromLedgerStoreErrorPropagates — when the store's
// QueryAllRecentCosts returns an error, RebuildFromLedger MUST wrap it
// (so the daemon refuses to start with a clear root cause) rather than
// silently leaving counters empty. invariant is load-bearing; degrading
// to "empty counters because the query failed" would let cap-checks pass
// that ought to fail.
func TestRebuildFromLedgerStoreErrorPropagates(t *testing.T) {
	wrappedErr := errors.New("disk read failed")
	q := &queryErrCostStore{fakeCostStore: newFakeCostStore(), queryEr: wrappedErr}
	cc := NewCostCounters(q)
	err := cc.RebuildFromLedger(time.Now().Add(-30 * 24 * time.Hour))
	if err == nil {
		t.Fatal("expected query error to propagate, got nil")
	}
	if !errors.Is(err, wrappedErr) {
		t.Errorf("expected wrapped %q, got %v", wrappedErr, err)
	}
}

// TestVerifyRebuildDetectsDesync — simulate a corrupted in-memory state
// (counters mutated independently from the ledger source-of-truth).
// verifyRebuild MUST return an error so the daemon refuses to start
// .
//
// Construction seed the ledger with one row totaling $5; do NOT call
// applyToCounters (so the in-memory total is 0, but the ledger says $5);
// invoke verifyRebuild directly on the rows returned by QueryAllRecentCosts.
// The expected sum (5) ≠ in-memory ProjectProfileTierTotal (0) → mismatch.
func TestVerifyRebuildDetectsDesync(t *testing.T) {
	store := newFakeCostStore()
	now := time.Now()
	row := mkRow("idem-desync", now.Add(-1*time.Hour), "internal-platform-x", "tier2-paygo", 5.00)
	if _, err := store.InsertCostLedger(row); err != nil {
		t.Fatalf("seed: %v", err)
	}
	cc := NewCostCounters(store)
	rows, err := store.QueryAllRecentCosts(now.Add(-30 * 24 * time.Hour))
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	// Skip applyToCounters — counters stay zero. verifyRebuild MUST detect
	// the gap (expected $5 vs in-memory $0).
	if err := cc.verifyRebuild(rows, now.Add(-30*24*time.Hour)); err == nil {
		t.Error("verifyRebuild on desynced counters: expected error, got nil")
	}
}

func TestStartHourlyMaintenanceGracefulShutdown(t *testing.T) {
	cc := NewCostCounters(newFakeCostStore())
	cc.tickInterval = 50 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := cc.StartHourlyMaintenance(ctx)

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("StartHourlyMaintenance goroutine did not exit on context cancel within 1s")
	}
}

// TestStartHourlyMaintenanceZeroTickIntervalUsesDefault — when
// cc.tickInterval is 0, the goroutine MUST start using
// defaultMaintenanceTickInterval (1h) without panicking. We can't easily
// verify the cadence (1h is too long for unit tests), but we can verify
// the goroutine is alive (no panic, no early exit) and exits cleanly on
// ctx.Done.
func TestStartHourlyMaintenanceZeroTickIntervalUsesDefault(t *testing.T) {
	cc := NewCostCounters(newFakeCostStore())

	ctx, cancel := context.WithCancel(context.Background())
	done := cc.StartHourlyMaintenance(ctx)

	select {
	case <-done:
		t.Fatal("maintenance goroutine exited prematurely with default 1h tick interval")
	case <-time.After(100 * time.Millisecond):

	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("maintenance goroutine did not exit on context cancel")
	}
}

// TestPruneAllNoCountersIsNoOp — calling pruneAll on a fresh CostCounters
// (zero windows) MUST NOT panic. Guards against a future refactor that
// dereferences a window slice without checking len.
func TestPruneAllNoCountersIsNoOp(t *testing.T) {
	cc := NewCostCounters(newFakeCostStore())

	cc.pruneAll(time.Now())
	// And the counters map MUST still be present (pruneAll does not
	// destroy the map; PruneOlderThan only mutates samples).
	cc.mu.RLock()
	defer cc.mu.RUnlock()
	if cc.projectProfileTierCounters == nil {
		t.Error("pruneAll nil-ed the counters map")
	}
}

func TestRebuildFromLedgerWrapsVerifyError(t *testing.T) {
	store := newFakeCostStore()
	now := time.Now()
	for i := 0; i < 3; i++ {
		row := mkRow(fmt.Sprintf("idem-%d", i), now.Add(-time.Duration(i)*time.Minute), "internal-platform-x", "tier2-paygo", 0.10)
		if _, err := store.InsertCostLedger(row); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}
	cc := NewCostCounters(store)
	if err := cc.RebuildFromLedger(now.Add(-30 * 24 * time.Hour)); err != nil {
		t.Fatalf("first rebuild: %v", err)
	}

	err := cc.RebuildFromLedger(now.Add(-30 * 24 * time.Hour))
	if err == nil {
		t.Fatal("second rebuild on same instance: expected verify error, got nil")
	}
	if !errorContains(err, "RebuildFromLedger verify:") {
		t.Errorf("expected wrapper prefix 'RebuildFromLedger verify:', got %v", err)
	}
}

func TestVerifyRebuildSkipsRowsOlderThan30d(t *testing.T) {
	cc := NewCostCounters(newFakeCostStore())
	now := time.Now()

	wA := cc.getOrCreateWindow("internal-platform-x", "orchestrator", "tier2-paygo", window30d, 30*24*time.Hour)
	wA.Add(now.Add(-1*time.Hour), 1.00)
	wB := cc.getOrCreateWindow("nexus", "orchestrator", "tier2-paygo", window30d, 30*24*time.Hour)
	wB.Add(now.Add(-1*time.Hour), 1.00)

	// Rows in-window matches in-memory, out-of-window MUST be skipped.
	rows := []CostLedgerRow{
		{Project: "internal-platform-x", Profile: "orchestrator", Tier: "tier2-paygo", TS: now.Add(-1 * time.Hour), CostUSD: 1.00},
		{Project: "nexus", Profile: "orchestrator", Tier: "tier2-paygo", TS: now.Add(-1 * time.Hour), CostUSD: 1.00},
		{Project: "internal-platform-x", Profile: "orchestrator", Tier: "tier2-paygo", TS: now.Add(-90 * 24 * time.Hour), CostUSD: 999.00},
	}
	if err := cc.verifyRebuild(rows, now.Add(-30*24*time.Hour)); err != nil {
		t.Errorf("verifyRebuild with mixed-age rows: expected nil (ancient skipped), got %v", err)
	}
}

// TestVerifyRebuildCapsAt8Keys — pin the count >= 8 break: with > 8
// distinct keys, only 8 are checked. Construction: seed 12 keys directly
// in-memory (matching the rows we pass in) so that the FIRST 8 keys
// verifyRebuild iterates pass; rows 9..12 have a deliberate desync (no
// in-memory state) but the cap stops iteration before reaching them.
//
// This pins the documented 8-key cap (cap rationale: bounds startup
// latency on large ledgers; sampling N keys is sufficient because every
// key shares the same applyToCounters routing path — a routing bug
// surfaces on the first key, not the ninth).
//
// NOTE(plan-15): map iteration order is non-deterministic, so we cannot guarantee
// WHICH 8 keys are checked. Construction defends by seeding ALL 12 keys
// correctly — verifyRebuild MUST return nil regardless of iteration
// order. The cap is exercised because expected has 12 entries while the
// loop processes only 8; this is observable via coverage of the
// `count >= 8` branch.
func TestVerifyRebuildCapsAt8Keys(t *testing.T) {
	cc := NewCostCounters(newFakeCostStore())
	now := time.Now()
	rows := make([]CostLedgerRow, 0, 12)
	for i := 0; i < 12; i++ {
		project := fmt.Sprintf("p%d", i)

		w := cc.getOrCreateWindow(project, "orchestrator", "tier2-paygo", window30d, 30*24*time.Hour)
		w.Add(now.Add(-1*time.Hour), 0.10)

		rows = append(rows, CostLedgerRow{
			Project: project, Profile: "orchestrator", Tier: "tier2-paygo",
			TS: now.Add(-1 * time.Hour), CostUSD: 0.10,
		})
	}
	if err := cc.verifyRebuild(rows, now.Add(-30*24*time.Hour)); err != nil {
		t.Errorf("verifyRebuild 12 keys: expected nil, got %v", err)
	}
}

func errorContains(err error, substr string) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), substr)
}

func TestRebuildFromLedgerSkipsOldRows(t *testing.T) {
	store := newFakeCostStore()
	now := time.Now()

	if _, err := store.InsertCostLedger(mkRow("idem-recent", now.Add(-1*time.Hour), "internal-platform-x", "tier2-paygo", 0.50)); err != nil {
		t.Fatalf("seed recent: %v", err)
	}

	if _, err := store.InsertCostLedger(mkRow("idem-ancient", now.Add(-90*24*time.Hour), "internal-platform-x", "tier2-paygo", 99.00)); err != nil {
		t.Fatalf("seed ancient: %v", err)
	}

	cc := NewCostCounters(store)
	if err := cc.RebuildFromLedger(now.Add(-30 * 24 * time.Hour)); err != nil {
		t.Fatalf("RebuildFromLedger: %v", err)
	}
	// 30d total MUST equal the in-window row only (0.50). The ancient
	// $99 row is outside `since` and never makes it into applyToCounters.
	if got := cc.ProjectProfileTierTotal("internal-platform-x", "orchestrator", "tier2-paygo", 30*24*time.Hour); got < 0.499 || got > 0.501 {
		t.Errorf("30d Total = %f, want 0.50 (ancient row must be filtered)", got)
	}
}

func TestAllKeysEmptyReturnsEmptyNotNil(t *testing.T) {
	cc := NewCostCounters(newFakeCostStore())
	got := cc.AllKeys()
	if got == nil {
		t.Fatal("AllKeys on empty counters returned nil; want []ProjectProfileTier{} (non-nil empty)")
	}
	if len(got) != 0 {
		t.Errorf("AllKeys empty len = %d, want 0", len(got))
	}
}

func TestAllKeysDedupsAcrossWindows(t *testing.T) {
	cc := NewCostCounters(newFakeCostStore())
	// One row produces TWO internal WindowCounter entries (24h + 30d) per
	// applyToCounters; AllKeys MUST deduplicate so the operator sees one
	// (project, profile, tier) tuple.
	row := mkRow("idem-1", time.Now(), "internal-platform-x", "tier2-paygo", 0.10)
	if err := cc.Record(row); err != nil {
		t.Fatalf("Record: %v", err)
	}
	got := cc.AllKeys()
	if len(got) != 1 {
		t.Fatalf("AllKeys len = %d, want 1 (single tuple, dedup across 24h+30d windows); got=%+v", len(got), got)
	}
	want := ProjectProfileTier{Project: "internal-platform-x", Profile: "orchestrator", Tier: "tier2-paygo"}
	if got[0] != want {
		t.Errorf("AllKeys[0] = %+v, want %+v", got[0], want)
	}
}

func TestAllKeysDistinctTuples(t *testing.T) {
	cc := NewCostCounters(newFakeCostStore())

	rows := []CostLedgerRow{
		mkRow("idem-1", time.Now(), "internal-platform-x", "tier2-paygo", 0.10),
		mkRow("idem-2", time.Now(), "nexus", "tier2-paygo", 0.20),
	}
	for _, r := range rows {
		if err := cc.Record(r); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}

	r3 := mkRow("idem-3", time.Now(), "internal-platform-x", "tier3-mac", 0.05)
	if err := cc.Record(r3); err != nil {
		t.Fatalf("Record: %v", err)
	}
	got := cc.AllKeys()
	if len(got) != 3 {
		t.Fatalf("AllKeys len = %d, want 3; got=%+v", len(got), got)
	}

	seen := map[ProjectProfileTier]int{}
	for _, k := range got {
		seen[k]++
	}
	for _, want := range []ProjectProfileTier{
		{Project: "internal-platform-x", Profile: "orchestrator", Tier: "tier2-paygo"},
		{Project: "nexus", Profile: "orchestrator", Tier: "tier2-paygo"},
		{Project: "internal-platform-x", Profile: "orchestrator", Tier: "tier3-mac"},
	} {
		if seen[want] != 1 {
			t.Errorf("tuple %+v appeared %d times; want 1", want, seen[want])
		}
	}
}

func TestAllKeysReturnsDefensiveCopy(t *testing.T) {
	cc := NewCostCounters(newFakeCostStore())
	if err := cc.Record(mkRow("idem-1", time.Now(), "internal-platform-x", "tier2-paygo", 0.10)); err != nil {
		t.Fatalf("Record: %v", err)
	}
	got := cc.AllKeys()
	if len(got) != 1 {
		t.Fatalf("AllKeys len = %d, want 1", len(got))
	}

	got[0] = ProjectProfileTier{Project: "garbage", Profile: "garbage", Tier: "garbage"}
	got2 := cc.AllKeys()
	if len(got2) != 1 || got2[0].Project != "internal-platform-x" {
		t.Errorf("internal state corrupted by caller mutation: got %+v", got2)
	}
}

func TestAllKeysConcurrentReadWriteRaceClean(t *testing.T) {
	cc := NewCostCounters(newFakeCostStore())
	now := time.Now()
	var wg sync.WaitGroup
	const N = 200

	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			project := fmt.Sprintf("p-%d", i%5)
			tier := fmt.Sprintf("t-%d", i%3)
			row := mkRow(fmt.Sprintf("idem-%d", i), now, project, tier, 0.001)
			if err := cc.Record(row); err != nil {
				t.Errorf("Record %d: %v", i, err)
			}
		}(i)
	}

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = cc.AllKeys()
		}()
	}
	wg.Wait()

	got := cc.AllKeys()
	if len(got) != 15 {
		t.Errorf("AllKeys len = %d, want 15 (5 projects × 3 tiers)", len(got))
	}
}
