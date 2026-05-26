//go:build chaos

// Drives the recovery contract from spec §4.4 row "Aggregator cache
// divergence":
//   - Per-project authoritative store (inbox.Store) is the source of
//     truth (inv-zen-113).
//   - The daemon-level cache (inbox.AggregatorCacheStore) is a
//     denormalized read-optimization layer; rebuildable on detection
//     of drift via Outbox.Recover (cache.Rebuild).
//   - Detection: comparing per-project authoritative COUNT(*) to cache
//     COUNT(*) reveals drift; production driver is inbox.Prober but
//     here we synthesize it directly so the test is hermetic.
//   - Recovery: cache.Rebuild discards every existing row + rehydrates
//     from the per-project sources. Post-rebuild every cache row's
//     ProjectID matches the source for that NotificationID.
//   - Idempotency: a clean rebuild cycle (no further corruption) MUST
//     produce a cache identical to the previous one — same row set.
//
// This test uses inbox.NewMemStore() (the public per-project fake) +
// a chaos-local in-memory AggregatorCacheStore that allows surgical
// corruption (the production *store.Store path is untouched).
package chaos

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/inbox"
)

type chaosCacheStore struct {
	mu   sync.Mutex
	rows []inbox.CacheRow
	nid  int64
}

func newChaosCacheStore() *chaosCacheStore { return &chaosCacheStore{nid: 1} }

func (m *chaosCacheStore) Insert(_ context.Context, r inbox.CacheRow) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r.CacheID = m.nid
	m.nid++
	m.rows = append(m.rows, r)
	return nil
}

func (m *chaosCacheStore) DeleteByProject(_ context.Context, projectID string) error {
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

func (m *chaosCacheStore) Query(_ context.Context, filter inbox.ListFilter) ([]inbox.CacheRow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []inbox.CacheRow
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

func (m *chaosCacheStore) Rebuild(ctx context.Context, sources []inbox.Store) error {
	m.mu.Lock()
	m.rows = nil
	m.nid = 1
	m.mu.Unlock()
	for _, s := range sources {
		ns, err := s.List(ctx, inbox.ListFilter{IncludeAcked: true})
		if err != nil {
			return err
		}
		for _, n := range ns {
			r := inbox.CacheRow{
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

func (m *chaosCacheStore) snapshot() []inbox.CacheRow {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]inbox.CacheRow, len(m.rows))
	copy(out, m.rows)
	return out
}

func (m *chaosCacheStore) corruptProjectID(idx int, newProjectID string) int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	old := m.rows[idx].NotificationID
	m.rows[idx].ProjectID = newProjectID
	return old
}

func (m *chaosCacheStore) corruptByEventType(eventType, newProjectID string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.rows {
		if m.rows[i].EventType == eventType {
			m.rows[i].ProjectID = newProjectID
			return 1
		}
	}
	return 0
}

func (m *chaosCacheStore) rowCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.rows)
}

func authoritativeCount(t *testing.T, ctx context.Context, sources []inbox.Store) int {
	t.Helper()
	total := 0
	for _, s := range sources {
		ns, err := s.List(ctx, inbox.ListFilter{IncludeAcked: true})
		if err != nil {
			t.Fatalf("authoritative List: %v", err)
		}
		total += len(ns)
	}
	return total
}

func TestChaos_AggregatorCacheDivergence_CountDriftDetectedAndRebuilt(t *testing.T) {
	ctx := context.Background()

	pA := "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111"
	pB := "bbbb2222bbbb2222bbbb2222bbbb2222bbbb2222bbbb2222bbbb2222bbbb2222"

	storeA := inbox.NewMemStore()
	storeB := inbox.NewMemStore()
	cache := newChaosCacheStore()

	aggA := inbox.NewAggregator(storeA, cache)
	aggB := inbox.NewAggregator(storeB, cache)

	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 4; i++ {
		if err := aggA.Insert(ctx, inbox.Notification{
			ProjectID:   pA,
			Severity:    inbox.SeverityActionNeeded,
			EventType:   "chaos.A",
			ContentHash: hashFor(t, "A", i),
			CreatedAt:   now.Add(time.Duration(i) * time.Second),
		}); err != nil {
			t.Fatalf("aggA.Insert(%d): %v", i, err)
		}
	}
	for i := 0; i < 3; i++ {
		if err := aggB.Insert(ctx, inbox.Notification{
			ProjectID:   pB,
			Severity:    inbox.SeverityInfoImmediate,
			EventType:   "chaos.B",
			ContentHash: hashFor(t, "B", i),
			CreatedAt:   now.Add(time.Duration(i) * time.Second),
		}); err != nil {
			t.Fatalf("aggB.Insert(%d): %v", i, err)
		}
	}

	drainCtx, cancel := context.WithCancel(ctx)
	go aggA.Outbox().Run(drainCtx)
	go aggB.Outbox().Run(drainCtx)
	waitForCount(t, cache.rowCount, 7, 30*time.Second)
	cancel()

	authTotal := authoritativeCount(t, ctx, []inbox.Store{storeA, storeB})
	if authTotal != 7 {
		t.Fatalf("pre-corruption authoritative count: got %d, want 7", authTotal)
	}
	if cache.rowCount() != 7 {
		t.Fatalf("pre-corruption cache count: got %d, want 7", cache.rowCount())
	}

	if err := cache.DeleteByProject(ctx, pA); err != nil {
		t.Fatalf("DeleteByProject(pA): %v", err)
	}
	postCorruption := cache.rowCount()
	if postCorruption != 3 {
		t.Fatalf("post-corruption cache count: got %d, want 3 (only pB rows)",
			postCorruption)
	}
	authTotal = authoritativeCount(t, ctx, []inbox.Store{storeA, storeB})
	if authTotal != 7 {
		t.Fatalf("authoritative source must NOT be affected by cache corruption: got %d", authTotal)
	}

	if err := aggA.Outbox().Recover(ctx, []inbox.Store{storeA, storeB}); err != nil {
		t.Fatalf("Outbox.Recover: %v", err)
	}

	if got, want := cache.rowCount(), authTotal; got != want {
		t.Fatalf("post-rebuild cache count: got %d, want %d", got, want)
	}

	pre := cache.snapshot()
	if err := aggA.Outbox().Recover(ctx, []inbox.Store{storeA, storeB}); err != nil {
		t.Fatalf("Outbox.Recover (idempotency): %v", err)
	}
	post := cache.snapshot()
	if !sameRowSet(pre, post) {
		t.Fatalf("idempotency violated: pre %d rows, post %d rows; first divergence near top of slice",
			len(pre), len(post))
	}
}

// TestChaos_AggregatorCacheDivergence_ProjectIDCorruptionRepairedByRebuild
// asserts that a cache row's ProjectID corruption (the inv-zen-113
// anchor — every cache row's ProjectID MUST match the authoritative
// source for that NotificationID) is repaired by Outbox.Recover.
func TestChaos_AggregatorCacheDivergence_ProjectIDCorruptionRepairedByRebuild(t *testing.T) {
	ctx := context.Background()
	pA := "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111"
	pB := "bbbb2222bbbb2222bbbb2222bbbb2222bbbb2222bbbb2222bbbb2222bbbb2222"
	pC := "cccc3333cccc3333cccc3333cccc3333cccc3333cccc3333cccc3333cccc3333"

	storeA := inbox.NewMemStore()
	storeB := inbox.NewMemStore()
	storeC := inbox.NewMemStore()
	cache := newChaosCacheStore()

	aggA := inbox.NewAggregator(storeA, cache)
	aggB := inbox.NewAggregator(storeB, cache)
	aggC := inbox.NewAggregator(storeC, cache)

	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)

	insertN := func(t *testing.T, agg *inbox.Aggregator, pid, kind string, n int) {
		t.Helper()
		for i := 0; i < n; i++ {
			if err := agg.Insert(ctx, inbox.Notification{
				ProjectID:   pid,
				Severity:    inbox.SeverityActionNeeded,
				EventType:   "chaos." + kind,
				ContentHash: hashFor(t, kind, i),
				CreatedAt:   now.Add(time.Duration(i) * time.Millisecond),
			}); err != nil {
				t.Fatalf("agg.Insert: %v", err)
			}
		}
	}
	insertN(t, aggA, pA, "A", 3)
	insertN(t, aggB, pB, "B", 3)
	insertN(t, aggC, pC, "C", 3)

	drainCtx, cancel := context.WithCancel(ctx)
	go aggA.Outbox().Run(drainCtx)
	go aggB.Outbox().Run(drainCtx)
	go aggC.Outbox().Run(drainCtx)
	waitForCount(t, cache.rowCount, 9, 30*time.Second)
	cancel()

	if n := cache.corruptByEventType("chaos.A", pB); n != 1 {
		t.Fatalf("corrupt A row: flipped %d rows, want 1", n)
	}
	if n := cache.corruptByEventType("chaos.C", pA); n != 1 {
		t.Fatalf("corrupt C row: flipped %d rows, want 1", n)
	}

	violations := countProjectIDViolations(t, cache, map[string]inbox.Store{
		pA: storeA, pB: storeB, pC: storeC,
	})
	if violations < 2 {
		t.Fatalf("expected >=2 ProjectID violations after corruption, got %d", violations)
	}

	if err := aggA.Outbox().Recover(ctx, []inbox.Store{storeA, storeB, storeC}); err != nil {
		t.Fatalf("Outbox.Recover: %v", err)
	}

	postViol := countProjectIDViolations(t, cache, map[string]inbox.Store{
		pA: storeA, pB: storeB, pC: storeC,
	})
	if postViol != 0 {
		t.Fatalf("post-rebuild ProjectID violations: got %d, want 0", postViol)
	}
}

func countProjectIDViolations(t *testing.T, cache *chaosCacheStore, sources map[string]inbox.Store) int {
	t.Helper()
	type sig struct {
		ContentHash string
		EventType   string
	}
	expected := map[sig]string{}
	for pid, s := range sources {
		ns, err := s.List(context.Background(), inbox.ListFilter{IncludeAcked: true})
		if err != nil {
			t.Fatalf("List source %s: %v", pid, err)
		}
		for _, n := range ns {
			expected[sig{n.ContentHash, n.EventType}] = pid
		}
	}
	rows := cache.snapshot()
	v := 0
	for _, r := range rows {
		want, ok := expected[sig{r.ContentHash, r.EventType}]
		if !ok {
			continue
		}
		if r.ProjectID != want {
			v++
		}
	}
	return v
}

func hashFor(t *testing.T, kind string, idx int) string {
	t.Helper()
	out := make([]byte, 64)
	for i := range out {
		out[i] = byte('0' + ((int(kind[0])+idx+i)%16)&0xf)
	}
	return string(out)
}

func waitForCount(t *testing.T, fn func() int, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() == want {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("waitForCount timeout: got %d, want %d", fn(), want)
}

func sameRowSet(a, b []inbox.CacheRow) bool {
	if len(a) != len(b) {
		return false
	}
	key := func(r inbox.CacheRow) string {
		return r.ProjectID + "|" + r.ContentHash + "|" + r.EventType
	}
	keysA := make([]string, len(a))
	keysB := make([]string, len(b))
	for i := range a {
		keysA[i] = key(a[i])
		keysB[i] = key(b[i])
	}
	sort.Strings(keysA)
	sort.Strings(keysB)
	for i := range keysA {
		if keysA[i] != keysB[i] {
			return false
		}
	}
	return true
}
