package inbox

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNoCrossProjectInboxLeakSentinelReturnsErr(t *testing.T) {
	if !errors.Is(noCrossProjectInboxLeakSentinel(), ErrCrossProjectInboxLeakAnchor) {
		t.Fatal("expected ErrCrossProjectInboxLeakAnchor")
	}
}

func TestOutboxFanoutSingleProject(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cache := newMemCacheStore()
	out := NewOutbox(cache, 16)
	go out.Run(ctx)

	pid := "a" + strings.Repeat("0", 63)
	n := Notification{
		ID: 1, ProjectID: pid, Severity: SeverityUrgent,
		EventType: "x.y", ContentHash: strings.Repeat("a", 64),
		Payload: json.RawMessage(`{}`), CreatedAt: time.Now().UTC(),
	}

	if err := out.Enqueue(CacheWrite{Notification: n, ProjectAlias: "internal-platform-x"}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		rows, _ := cache.Query(ctx, ListFilter{})
		if len(rows) == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	rows, err := cache.Query(ctx, ListFilter{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("cache rows = %d, want 1", len(rows))
	}
	if rows[0].ProjectID != pid {
		t.Errorf("cache.ProjectID = %q, want %q", rows[0].ProjectID, pid)
	}
}

func TestOutboxNeverCrossesProjectBoundary(t *testing.T) {

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cache := newMemCacheStore()
	out := NewOutbox(cache, 256)
	go out.Run(ctx)

	pids := []string{
		"a" + strings.Repeat("0", 63),
		"b" + strings.Repeat("1", 63),
		"c" + strings.Repeat("2", 63),
		"d" + strings.Repeat("3", 63),
		"e" + strings.Repeat("4", 63),
	}

	type pair struct {
		pid string
		nid int64
	}
	expected := map[pair]bool{}
	var nextID int64 = 1
	for i := 0; i < 100; i++ {
		pid := pids[i%len(pids)]
		nid := nextID
		nextID++
		n := Notification{
			ID: nid, ProjectID: pid, Severity: SeverityInfoImmediate,
			EventType: "x.y", ContentHash: ComputeContentHash(map[string]any{"k": pid, "i": i}),
			Payload: json.RawMessage(`{}`), CreatedAt: time.Now().UTC().Add(time.Duration(i) * time.Millisecond),
		}
		expected[pair{pid: pid, nid: nid}] = true
		if err := out.Enqueue(CacheWrite{Notification: n, ProjectAlias: pid[:8]}); err != nil {
			t.Fatalf("Enqueue: %v", err)
		}
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rows, _ := cache.Query(ctx, ListFilter{})
		if len(rows) == 100 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	rows, err := cache.Query(ctx, ListFilter{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(rows) != 100 {
		t.Fatalf("cache len = %d, want 100", len(rows))
	}

	for _, r := range rows {
		key := pair{pid: r.ProjectID, nid: r.NotificationID}
		if !expected[key] {
			t.Errorf("inv-zen-113 violation: cache row %+v not in expected set", r)
		}
	}
}

func TestOutboxBackpressureRejectsWhenFull(t *testing.T) {

	cache := newMemCacheStore()
	out := NewOutbox(cache, 2)

	pid := "a" + strings.Repeat("0", 63)
	mk := func(i int64) CacheWrite {
		n := Notification{
			ID: i, ProjectID: pid, Severity: SeverityInfoImmediate,
			EventType: "x.y", ContentHash: strings.Repeat("a", 64),
			Payload: json.RawMessage(`{}`), CreatedAt: time.Now().UTC(),
		}
		return CacheWrite{Notification: n, ProjectAlias: "alias"}
	}

	if err := out.Enqueue(mk(1)); err != nil {
		t.Fatalf("first Enqueue: %v", err)
	}
	if err := out.Enqueue(mk(2)); err != nil {
		t.Fatalf("second Enqueue: %v", err)
	}

	err := out.Enqueue(mk(3))
	if !errors.Is(err, ErrOutboxFull) {
		t.Errorf("expected ErrOutboxFull, got: %v", err)
	}
}

func TestOutboxRunCancellationStopsCleanly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cache := newMemCacheStore()
	out := NewOutbox(cache, 16)

	done := make(chan struct{})
	go func() {
		out.Run(ctx)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not stop after context cancellation")
	}
}

func TestOutboxObservabilityHelpers(t *testing.T) {
	cache := newMemCacheStore()
	out := NewOutbox(cache, 4)

	if got := out.Capacity(); got != 4 {
		t.Errorf("Capacity = %d, want 4", got)
	}
	if got := out.Pending(); got != 0 {
		t.Errorf("Pending (initial) = %d, want 0", got)
	}

	pid := "a" + strings.Repeat("0", 63)
	mk := func(i int64) CacheWrite {
		return CacheWrite{
			Notification: Notification{
				ID: i, ProjectID: pid, Severity: SeverityInfoImmediate,
				EventType: "x.y", ContentHash: strings.Repeat("a", 64),
				Payload: json.RawMessage(`{}`), CreatedAt: time.Now().UTC(),
			},
			ProjectAlias: "alias",
		}
	}

	if err := out.Enqueue(mk(1)); err != nil {
		t.Fatalf("Enqueue 1: %v", err)
	}
	if err := out.Enqueue(mk(2)); err != nil {
		t.Fatalf("Enqueue 2: %v", err)
	}
	if got := out.Pending(); got != 2 {
		t.Errorf("Pending after 2 enqueues = %d, want 2", got)
	}

	enq, drained, rejected, cacheErr := out.Stats()
	if enq != 2 {
		t.Errorf("Stats.enqueued = %d, want 2", enq)
	}
	if drained != 0 {
		t.Errorf("Stats.drained = %d, want 0 (Run not started)", drained)
	}
	if rejected != 0 {
		t.Errorf("Stats.rejected = %d, want 0", rejected)
	}
	if cacheErr != 0 {
		t.Errorf("Stats.cacheError = %d, want 0", cacheErr)
	}

	if err := out.Enqueue(mk(3)); err != nil {
		t.Fatalf("Enqueue 3: %v", err)
	}
	if err := out.Enqueue(mk(4)); err != nil {
		t.Fatalf("Enqueue 4: %v", err)
	}
	if err := out.Enqueue(mk(5)); !errors.Is(err, ErrOutboxFull) {
		t.Fatalf("Enqueue 5 should reject ErrOutboxFull, got %v", err)
	}

	_, _, rejected, _ = out.Stats()
	if rejected != 1 {
		t.Errorf("Stats.rejected after overflow = %d, want 1", rejected)
	}
}

func TestOutboxRunCacheErrorContinues(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cache := &errCacheStore{failOn: map[int64]bool{2: true}}
	out := NewOutbox(cache, 16)
	go out.Run(ctx)

	pid := "a" + strings.Repeat("0", 63)
	for i := int64(1); i <= 3; i++ {
		n := Notification{
			ID: i, ProjectID: pid, Severity: SeverityInfoImmediate,
			EventType: "x.y", ContentHash: strings.Repeat("a", 64),
			Payload: json.RawMessage(`{}`), CreatedAt: time.Now().UTC(),
		}
		if err := out.Enqueue(CacheWrite{Notification: n, ProjectAlias: "alias"}); err != nil {
			t.Fatalf("Enqueue %d: %v", i, err)
		}
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if cache.SuccessCount() >= 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	if got := cache.SuccessCount(); got != 2 {
		t.Fatalf("cache successes = %d, want 2", got)
	}
	_, drained, _, cacheErr := out.Stats()
	if drained != 2 {
		t.Errorf("Stats.drained = %d, want 2", drained)
	}
	if cacheErr != 1 {
		t.Errorf("Stats.cacheError = %d, want 1", cacheErr)
	}
}

type errCacheStore struct {
	mu       sync.Mutex
	failOn   map[int64]bool
	successN int
}

func (e *errCacheStore) Insert(_ context.Context, r CacheRow) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.failOn[r.NotificationID] {
		return errors.New("simulated cache failure")
	}
	e.successN++
	return nil
}

func (e *errCacheStore) SuccessCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.successN
}

func (e *errCacheStore) DeleteByProject(_ context.Context, _ string) error { return nil }

func (e *errCacheStore) Query(_ context.Context, _ ListFilter) ([]CacheRow, error) { return nil, nil }

func (e *errCacheStore) Rebuild(_ context.Context, _ []Store) error { return nil }

func TestAggregatorTwoStageWrite(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store := NewMemStore()
	cache := newMemCacheStore()
	agg := NewAggregator(store, cache)

	if agg.Outbox() == nil {
		t.Fatal("Outbox() returned nil")
	}
	if got := agg.Outbox().Capacity(); got != 1024 {
		t.Errorf("default outbox capacity = %d, want 1024", got)
	}

	go agg.Run(ctx)

	pid := "a" + strings.Repeat("0", 63)
	n := Notification{
		ProjectID: pid, Severity: SeverityUrgent,
		EventType: "x.y", ContentHash: strings.Repeat("a", 64),
		Payload: json.RawMessage(`{}`), CreatedAt: time.Now().UTC(),
	}
	if err := agg.Insert(ctx, n); err != nil {
		t.Fatalf("Aggregator.Insert: %v", err)
	}

	rowsPP, err := store.List(ctx, ListFilter{IncludeAcked: true})
	if err != nil {
		t.Fatalf("perProject.List: %v", err)
	}
	if len(rowsPP) != 1 {
		t.Fatalf("per-project rows = %d, want 1", len(rowsPP))
	}
	if rowsPP[0].ProjectID != pid {
		t.Errorf("per-project ProjectID = %q, want %q", rowsPP[0].ProjectID, pid)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		rows, _ := agg.Query(ctx, ListFilter{})
		if len(rows) == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	rowsCache, err := agg.Query(ctx, ListFilter{})
	if err != nil {
		t.Fatalf("Aggregator.Query: %v", err)
	}
	if len(rowsCache) != 1 {
		t.Fatalf("cache rows after fanout = %d, want 1", len(rowsCache))
	}
	if rowsCache[0].ProjectID != pid {
		t.Errorf("cache ProjectID = %q, want %q (inv-zen-113)", rowsCache[0].ProjectID, pid)
	}
}

// TestAggregatorInsertRollbackOnPerProjectFailure covers the "stage 1
// fails → return err, do NOT enqueue" branch. If the authoritative
// store rejects the write (validation, dedup), the cache must NOT
// receive a phantom row.
func TestAggregatorInsertRollbackOnPerProjectFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store := NewMemStore()
	cache := newMemCacheStore()
	agg := NewAggregator(store, cache)
	go agg.Run(ctx)

	// Notification with empty ProjectID — perProject.Insert will reject
	// via validateNotificationForInsert; outbox MUST NOT see this.
	bad := Notification{
		ProjectID: "", Severity: SeverityUrgent,
		EventType: "x.y", ContentHash: strings.Repeat("a", 64),
		Payload: json.RawMessage(`{}`), CreatedAt: time.Now().UTC(),
	}
	if err := agg.Insert(ctx, bad); err == nil {
		t.Fatal("Aggregator.Insert with empty ProjectID should fail")
	}

	time.Sleep(20 * time.Millisecond)
	rows, _ := agg.Query(ctx, ListFilter{})
	if len(rows) != 0 {
		t.Errorf("cache rows after rejected insert = %d, want 0", len(rows))
	}
	enq, _, _, _ := agg.Outbox().Stats()
	if enq != 0 {
		t.Errorf("outbox enqueued count after rejected insert = %d, want 0", enq)
	}
}

type memCacheStore struct {
	mu   sync.Mutex
	rows []CacheRow
	nid  int64
}

func newMemCacheStore() *memCacheStore { return &memCacheStore{nid: 1} }

func (m *memCacheStore) Insert(_ context.Context, r CacheRow) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r.CacheID = m.nid
	m.nid++
	m.rows = append(m.rows, r)
	return nil
}

func (m *memCacheStore) DeleteByProject(_ context.Context, projectID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	kept := m.rows[:0]
	for _, r := range m.rows {
		if r.ProjectID != projectID {
			kept = append(kept, r)
		}
	}
	m.rows = kept
	return nil
}

func (m *memCacheStore) Query(_ context.Context, filter ListFilter) ([]CacheRow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []CacheRow
	for _, r := range m.rows {
		if filter.ProjectID != "" && r.ProjectID != filter.ProjectID {
			continue
		}
		if filter.Severity != nil && r.Severity != *filter.Severity {
			continue
		}
		if filter.Since != nil && r.CreatedAt.Before(*filter.Since) {
			continue
		}
		if !filter.IncludeAcked && r.AckedAt != nil {
			continue
		}
		out = append(out, r)
	}
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, nil
}

func (m *memCacheStore) Rebuild(ctx context.Context, sources []Store) error {
	m.mu.Lock()
	m.rows = nil
	m.nid = 1
	m.mu.Unlock()
	for _, s := range sources {
		ns, err := s.List(ctx, ListFilter{IncludeAcked: true})
		if err != nil {
			return err
		}
		for _, n := range ns {
			r := CacheRow{
				ProjectID:      n.ProjectID,
				ProjectAlias:   "",
				NotificationID: n.ID,
				Severity:       n.Severity,
				EventType:      n.EventType,
				ContentHash:    n.ContentHash,
				CreatedAt:      n.CreatedAt,
				AckedAt:        n.AckedAt,
			}
			if err := m.Insert(ctx, r); err != nil {
				return err
			}
		}
	}
	return nil
}
