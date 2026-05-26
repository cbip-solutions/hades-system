package inboxadapter

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/inbox"
	"github.com/cbip-solutions/hades-system/internal/store"
)

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "inbox-adapter.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestAdapterImplementsInboxStore(t *testing.T) {
	var _ inbox.Store = (*Adapter)(nil)
}

func TestAdapterImplementsAggregatorCacheStore(t *testing.T) {
	var _ inbox.AggregatorCacheStore = (*cacheView)(nil)
	var a *Adapter
	var _ inbox.AggregatorCacheStore = a.Cache()
}

func TestNewAdapterPanicsOnNilDaemonStore(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("NewAdapter(nil daemonStore) must panic")
		}
	}()
	_ = NewAdapter(nil, nil)
}

func TestAdapterInsertRoundTrip(t *testing.T) {
	s := openTestStore(t)
	a := NewAdapter(map[string]*store.Store{"internal-platform-x-id": s}, s)

	ctx := context.Background()
	pid := strings.Repeat("a", 64)
	a.RegisterProject(pid, "internal-platform-x", s)

	n := &inbox.Notification{
		ProjectID:   pid,
		Severity:    inbox.SeverityActionNeeded,
		EventType:   "gate.failed",
		ContentHash: strings.Repeat("a", 64),
		Payload:     json.RawMessage(`{"finding":"x"}`),
		CreatedAt:   time.Now().UTC(),
	}
	if err := a.Insert(ctx, n); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if n.ID == 0 {
		t.Error("ID was not populated")
	}

	got, err := a.List(ctx, inbox.ListFilter{ProjectID: pid})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("List: len = %d, want 1", len(got))
	}
	if got[0].EventType != "gate.failed" {
		t.Errorf("EventType = %q", got[0].EventType)
	}
}

func TestAdapterCacheFanoutPopulatesCacheRow(t *testing.T) {
	s := openTestStore(t)
	a := NewAdapter(map[string]*store.Store{}, s)

	ctx := context.Background()
	pid := strings.Repeat("a", 64)
	a.RegisterProject(pid, "internal-platform-x", s)

	r := inbox.CacheRow{
		ProjectID:      pid,
		ProjectAlias:   "internal-platform-x",
		NotificationID: 1,
		Severity:       inbox.SeverityUrgent,
		EventType:      "panic",
		ContentHash:    strings.Repeat("a", 64),
		CreatedAt:      time.Now().UTC(),
	}
	if err := a.InsertCache(ctx, r); err != nil {
		t.Fatalf("InsertCache: %v", err)
	}

	rows, err := a.Query(ctx, inbox.ListFilter{ProjectID: pid})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("Query: len = %d, want 1", len(rows))
	}
	if rows[0].ProjectAlias != "internal-platform-x" {
		t.Errorf("ProjectAlias = %q", rows[0].ProjectAlias)
	}
}

func TestAdapterCacheInterfaceInsertEquivalent(t *testing.T) {
	s := openTestStore(t)
	a := NewAdapter(nil, s)

	ctx := context.Background()
	pid := strings.Repeat("a", 64)
	a.RegisterProject(pid, "alpha", s)

	cache := a.Cache()
	r := inbox.CacheRow{
		ProjectID:      pid,
		ProjectAlias:   "alpha",
		NotificationID: 7,
		Severity:       inbox.SeverityInfoImmediate,
		EventType:      "evt",
		ContentHash:    strings.Repeat("e", 64),
		CreatedAt:      time.Now().UTC(),
	}
	if err := cache.Insert(ctx, r); err != nil {
		t.Fatalf("cache.Insert: %v", err)
	}

	rows, err := a.Query(ctx, inbox.ListFilter{ProjectID: pid})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len = %d, want 1", len(rows))
	}
	if rows[0].NotificationID != 7 {
		t.Errorf("NotificationID = %d, want 7", rows[0].NotificationID)
	}
}

func TestAdapterDeleteCascadesCache(t *testing.T) {
	s := openTestStore(t)
	a := NewAdapter(map[string]*store.Store{}, s)

	ctx := context.Background()
	pidA := strings.Repeat("a", 64)
	pidB := strings.Repeat("b", 64)
	a.RegisterProject(pidA, "alpha", s)
	a.RegisterProject(pidB, "beta", s)

	for i, pid := range []string{pidA, pidB} {
		_ = a.Insert(ctx, &inbox.Notification{
			ProjectID:   pid,
			Severity:    inbox.SeverityInfoDigest,
			EventType:   "evt",
			ContentHash: strings.Repeat([]string{"c", "d"}[i], 64),
			Payload:     json.RawMessage(`{}`),
			CreatedAt:   time.Now().UTC(),
		})
		_ = a.InsertCache(ctx, inbox.CacheRow{
			ProjectID:      pid,
			ProjectAlias:   pid[:5],
			NotificationID: int64(i + 1),
			Severity:       inbox.SeverityInfoDigest,
			EventType:      "evt",
			ContentHash:    strings.Repeat([]string{"c", "d"}[i], 64),
			CreatedAt:      time.Now().UTC(),
		})
	}

	if err := a.DeleteByProject(ctx, pidA); err != nil {
		t.Fatalf("DeleteByProject(A): %v", err)
	}

	rowsA, _ := a.Query(ctx, inbox.ListFilter{ProjectID: pidA})
	if len(rowsA) != 0 {
		t.Errorf("after Delete A: cache rows = %d, want 0", len(rowsA))
	}
	rowsB, _ := a.Query(ctx, inbox.ListFilter{ProjectID: pidB})
	if len(rowsB) != 1 {
		t.Errorf("after Delete A: B cache rows = %d, want 1", len(rowsB))
	}

	inboxA, _ := a.List(ctx, inbox.ListFilter{ProjectID: pidA, IncludeAcked: true})
	if len(inboxA) != 0 {
		t.Errorf("after Delete A: per-project inbox rows = %d, want 0", len(inboxA))
	}
	inboxB, _ := a.List(ctx, inbox.ListFilter{ProjectID: pidB, IncludeAcked: true})
	if len(inboxB) != 1 {
		t.Errorf("after Delete A: per-project inbox B rows = %d, want 1", len(inboxB))
	}
}

func TestAdapterDedupRejectsViolation(t *testing.T) {
	s := openTestStore(t)
	a := NewAdapter(map[string]*store.Store{}, s)

	ctx := context.Background()
	pid := strings.Repeat("a", 64)
	a.RegisterProject(pid, "alpha", s)

	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	n := &inbox.Notification{
		ProjectID:   pid,
		Severity:    inbox.SeverityInfoImmediate,
		EventType:   "evt",
		ContentHash: strings.Repeat("a", 64),
		Payload:     json.RawMessage(`{}`),
		CreatedAt:   now,
	}
	if err := a.Insert(ctx, n); err != nil {
		t.Fatalf("first Insert: %v", err)
	}

	dup := &inbox.Notification{
		ProjectID:   pid,
		Severity:    inbox.SeverityInfoImmediate,
		EventType:   "evt",
		ContentHash: strings.Repeat("a", 64),
		Payload:     json.RawMessage(`{}`),
		CreatedAt:   now.Add(2 * time.Minute),
	}
	err := a.Insert(ctx, dup)
	if err == nil {
		t.Fatal("Insert duplicate must fail UNIQUE")
	}

	if !errorsContains(err, "dedup") {
		t.Errorf("expected dedup violation message, got: %v", err)
	}
}

func errorsContains(err error, sub string) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), sub)
}

func TestAdapterAckSetsAckedAt(t *testing.T) {
	s := openTestStore(t)
	a := NewAdapter(map[string]*store.Store{}, s)

	ctx := context.Background()
	pid := strings.Repeat("a", 64)
	a.RegisterProject(pid, "alpha", s)

	n := &inbox.Notification{
		ProjectID:   pid,
		Severity:    inbox.SeverityActionNeeded,
		EventType:   "x",
		ContentHash: strings.Repeat("a", 64),
		Payload:     json.RawMessage(`{}`),
		CreatedAt:   time.Now().UTC(),
	}
	if err := a.Insert(ctx, n); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	if err := a.Ack(ctx, n.ID); err != nil {
		t.Fatalf("Ack: %v", err)
	}

	got, _ := a.List(ctx, inbox.ListFilter{ProjectID: pid, IncludeAcked: true})
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].AckedAt == nil {
		t.Error("AckedAt should be set")
	}
}

func TestAdapterAckUnknownIDReturnsErrNotFound(t *testing.T) {
	s := openTestStore(t)
	a := NewAdapter(nil, s)

	ctx := context.Background()
	if err := a.Ack(ctx, 999_999); err == nil {
		t.Fatal("Ack(unknown) must fail")
	} else if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected ErrNotFound chain, got: %v", err)
	}
}

func TestAdapterSnoozeSetsSnoozedUntil(t *testing.T) {
	s := openTestStore(t)
	a := NewAdapter(nil, s)

	ctx := context.Background()
	pid := strings.Repeat("a", 64)
	a.RegisterProject(pid, "alpha", s)

	n := &inbox.Notification{
		ProjectID:   pid,
		Severity:    inbox.SeverityInfoImmediate,
		EventType:   "evt",
		ContentHash: strings.Repeat("f", 64),
		Payload:     json.RawMessage(`{}`),
		CreatedAt:   time.Now().UTC(),
	}
	if err := a.Insert(ctx, n); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	until := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Second)
	if err := a.Snooze(ctx, n.ID, until); err != nil {
		t.Fatalf("Snooze: %v", err)
	}

	got, _ := a.List(ctx, inbox.ListFilter{ProjectID: pid, IncludeAcked: true})
	if len(got) != 1 {
		t.Fatalf("len = %d", len(got))
	}
	if got[0].SnoozedUntil == nil {
		t.Fatal("SnoozedUntil should be set")
	}
	if !got[0].SnoozedUntil.Equal(until) {
		t.Errorf("SnoozedUntil = %v, want %v", got[0].SnoozedUntil, until)
	}
}

func TestAdapterSnoozeUnknownIDReturnsErrNotFound(t *testing.T) {
	s := openTestStore(t)
	a := NewAdapter(nil, s)

	ctx := context.Background()
	if err := a.Snooze(ctx, 12345, time.Now().UTC().Add(time.Hour)); err == nil {
		t.Fatal("Snooze(unknown) must fail")
	} else if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected ErrNotFound chain, got: %v", err)
	}
}

func TestAdapterListWithoutProjectFilterAggregates(t *testing.T) {
	s := openTestStore(t)
	a := NewAdapter(nil, s)

	ctx := context.Background()
	pidA := strings.Repeat("a", 64)
	pidB := strings.Repeat("b", 64)
	a.RegisterProject(pidA, "alpha", s)
	a.RegisterProject(pidB, "beta", s)

	for i, pid := range []string{pidA, pidB} {
		_ = a.Insert(ctx, &inbox.Notification{
			ProjectID:   pid,
			Severity:    inbox.SeverityInfoImmediate,
			EventType:   "evt",
			ContentHash: strings.Repeat([]string{"a", "b"}[i], 64),
			Payload:     json.RawMessage(`{}`),
			CreatedAt:   time.Now().UTC().Add(time.Duration(i) * time.Hour),
		})
	}

	got, err := a.List(ctx, inbox.ListFilter{IncludeAcked: true})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) < 2 {
		t.Errorf("len = %d, want >= 2", len(got))
	}

	limited, err := a.List(ctx, inbox.ListFilter{Limit: 1, IncludeAcked: true})
	if err != nil {
		t.Fatalf("List(limit=1): %v", err)
	}
	if len(limited) != 1 {
		t.Errorf("limited len = %d, want 1", len(limited))
	}
}

func TestAdapterListWithSeverityAndSinceFilters(t *testing.T) {
	s := openTestStore(t)
	a := NewAdapter(nil, s)

	ctx := context.Background()
	pid := strings.Repeat("a", 64)
	a.RegisterProject(pid, "alpha", s)

	t0 := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	rows := []struct {
		sev   inbox.Severity
		hash  string
		delta time.Duration
	}{
		{inbox.SeverityUrgent, strings.Repeat("a", 64), 0},
		{inbox.SeverityInfoDigest, strings.Repeat("b", 64), time.Hour},
		{inbox.SeverityUrgent, strings.Repeat("c", 64), 2 * time.Hour},
	}
	for _, r := range rows {
		_ = a.Insert(ctx, &inbox.Notification{
			ProjectID:   pid,
			Severity:    r.sev,
			EventType:   "evt",
			ContentHash: r.hash,
			Payload:     json.RawMessage(`{}`),
			CreatedAt:   t0.Add(r.delta),
		})
	}

	urg := inbox.SeverityUrgent
	got, err := a.List(ctx, inbox.ListFilter{
		ProjectID:    pid,
		Severity:     &urg,
		IncludeAcked: true,
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("severity-filter len = %d, want 2", len(got))
	}

	floor := t0.Add(90 * time.Minute)
	got2, err := a.List(ctx, inbox.ListFilter{
		ProjectID:    pid,
		Since:        &floor,
		IncludeAcked: true,
	})
	if err != nil {
		t.Fatalf("List(since): %v", err)
	}
	if len(got2) != 1 {
		t.Errorf("since-filter len = %d, want 1", len(got2))
	}
}

func TestAdapterListExcludesAckedByDefault(t *testing.T) {
	s := openTestStore(t)
	a := NewAdapter(nil, s)

	ctx := context.Background()
	pid := strings.Repeat("a", 64)
	a.RegisterProject(pid, "alpha", s)

	n := &inbox.Notification{
		ProjectID:   pid,
		Severity:    inbox.SeverityInfoImmediate,
		EventType:   "evt",
		ContentHash: strings.Repeat("a", 64),
		Payload:     json.RawMessage(`{}`),
		CreatedAt:   time.Now().UTC(),
	}
	_ = a.Insert(ctx, n)
	_ = a.Ack(ctx, n.ID)

	got, _ := a.List(ctx, inbox.ListFilter{ProjectID: pid})
	if len(got) != 0 {
		t.Errorf("default List should exclude acked, got len=%d", len(got))
	}

	got2, _ := a.List(ctx, inbox.ListFilter{ProjectID: pid, IncludeAcked: true})
	if len(got2) != 1 {
		t.Errorf("IncludeAcked=true should return 1, got %d", len(got2))
	}
}

func TestAdapterDeleteIsAliasOfDeleteByProject(t *testing.T) {
	s := openTestStore(t)
	a := NewAdapter(nil, s)

	ctx := context.Background()
	pid := strings.Repeat("a", 64)
	a.RegisterProject(pid, "alpha", s)

	_ = a.Insert(ctx, &inbox.Notification{
		ProjectID:   pid,
		Severity:    inbox.SeverityInfoImmediate,
		EventType:   "evt",
		ContentHash: strings.Repeat("a", 64),
		Payload:     json.RawMessage(`{}`),
		CreatedAt:   time.Now().UTC(),
	})
	_ = a.InsertCache(ctx, inbox.CacheRow{
		ProjectID:      pid,
		ProjectAlias:   "alpha",
		NotificationID: 1,
		Severity:       inbox.SeverityInfoImmediate,
		EventType:      "evt",
		ContentHash:    strings.Repeat("a", 64),
		CreatedAt:      time.Now().UTC(),
	})

	if err := a.Delete(ctx, pid); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	rows, _ := a.Query(ctx, inbox.ListFilter{ProjectID: pid})
	if len(rows) != 0 {
		t.Errorf("after Delete: cache rows = %d, want 0", len(rows))
	}
}

func TestAdapterRebuildCacheFromSources(t *testing.T) {
	src := openTestStore(t)
	dst := openTestStore(t)
	a := NewAdapter(nil, dst)

	ctx := context.Background()
	pidA := strings.Repeat("a", 64)
	pidB := strings.Repeat("b", 64)
	a.RegisterProject(pidA, "alpha", src)
	a.RegisterProject(pidB, "beta", src)

	for i, pid := range []string{pidA, pidB} {
		_ = a.Insert(ctx, &inbox.Notification{
			ProjectID:   pid,
			Severity:    inbox.SeverityInfoImmediate,
			EventType:   "evt",
			ContentHash: strings.Repeat([]string{"a", "b"}[i], 64),
			Payload:     json.RawMessage(`{}`),
			CreatedAt:   time.Now().UTC(),
		})
	}

	_ = a.InsertCache(ctx, inbox.CacheRow{
		ProjectID:      pidA,
		ProjectAlias:   "stale",
		NotificationID: 999,
		Severity:       inbox.SeverityInfoDigest,
		EventType:      "stale",
		ContentHash:    strings.Repeat("9", 64),
		CreatedAt:      time.Now().UTC(),
	})

	sources := []inbox.Store{a}
	if err := a.Rebuild(ctx, sources); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	rows, _ := a.Query(ctx, inbox.ListFilter{IncludeAcked: true})

	hasStale := false
	for _, r := range rows {
		if r.EventType == "stale" {
			hasStale = true
		}
	}
	if hasStale {
		t.Error("Rebuild did not wipe stale cache row")
	}
	if len(rows) < 2 {
		t.Errorf("post-Rebuild len = %d, want >= 2", len(rows))
	}
}

func TestAdapterQueryFiltersAcked(t *testing.T) {
	s := openTestStore(t)
	a := NewAdapter(nil, s)

	ctx := context.Background()
	pid := strings.Repeat("a", 64)
	a.RegisterProject(pid, "alpha", s)

	now := time.Now().UTC().Truncate(time.Second)
	acked := now
	_ = a.InsertCache(ctx, inbox.CacheRow{
		ProjectID:      pid,
		ProjectAlias:   "alpha",
		NotificationID: 1,
		Severity:       inbox.SeverityInfoImmediate,
		EventType:      "evt",
		ContentHash:    strings.Repeat("a", 64),
		CreatedAt:      now,
		AckedAt:        &acked,
	})
	_ = a.InsertCache(ctx, inbox.CacheRow{
		ProjectID:      pid,
		ProjectAlias:   "alpha",
		NotificationID: 2,
		Severity:       inbox.SeverityInfoImmediate,
		EventType:      "evt",
		ContentHash:    strings.Repeat("b", 64),
		CreatedAt:      now,
	})

	got, _ := a.Query(ctx, inbox.ListFilter{ProjectID: pid})
	if len(got) != 1 {
		t.Errorf("default Query (exclude acked) len = %d, want 1", len(got))
	}

	got2, _ := a.Query(ctx, inbox.ListFilter{ProjectID: pid, IncludeAcked: true})
	if len(got2) != 2 {
		t.Errorf("IncludeAcked Query len = %d, want 2", len(got2))
	}
}

func TestAdapterQueryWithSeveritySinceLimit(t *testing.T) {
	s := openTestStore(t)
	a := NewAdapter(nil, s)

	ctx := context.Background()
	pid := strings.Repeat("a", 64)
	a.RegisterProject(pid, "alpha", s)

	t0 := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	specs := []struct {
		sev   inbox.Severity
		t     time.Time
		hash  byte
		notif int64
	}{
		{inbox.SeverityUrgent, t0, 'a', 1},
		{inbox.SeverityUrgent, t0.Add(time.Hour), 'b', 2},
		{inbox.SeverityInfoDigest, t0.Add(2 * time.Hour), 'c', 3},
	}
	for _, sp := range specs {
		_ = a.InsertCache(ctx, inbox.CacheRow{
			ProjectID:      pid,
			ProjectAlias:   "alpha",
			NotificationID: sp.notif,
			Severity:       sp.sev,
			EventType:      "evt",
			ContentHash:    strings.Repeat(string(sp.hash), 64),
			CreatedAt:      sp.t,
		})
	}

	urg := inbox.SeverityUrgent
	got, err := a.Query(ctx, inbox.ListFilter{
		ProjectID: pid,
		Severity:  &urg,
		Limit:     5,
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("severity-filtered len = %d, want 2", len(got))
	}

	floor := t0.Add(90 * time.Minute)
	got2, err := a.Query(ctx, inbox.ListFilter{
		ProjectID: pid,
		Since:     &floor,
	})
	if err != nil {
		t.Fatalf("Query(since): %v", err)
	}
	if len(got2) != 1 {
		t.Errorf("since-filter len = %d, want 1", len(got2))
	}

	got3, _ := a.Query(ctx, inbox.ListFilter{ProjectID: pid, Limit: 1})
	if len(got3) != 1 {
		t.Errorf("limit=1 len = %d, want 1", len(got3))
	}
}

func TestAdapterConcurrentMultiProjectIsolation(t *testing.T) {

	s := openTestStore(t)
	a := NewAdapter(map[string]*store.Store{}, s)

	ctx := context.Background()
	pids := []string{
		"a" + strings.Repeat("0", 63),
		"b" + strings.Repeat("1", 63),
		"c" + strings.Repeat("2", 63),
	}
	for i, pid := range pids {
		a.RegisterProject(pid, []string{"alpha", "beta", "gamma"}[i], s)
	}

	const insertsPerProject = 30
	var wg sync.WaitGroup
	for _, pid := range pids {
		pid := pid
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < insertsPerProject; i++ {
				_ = a.Insert(ctx, &inbox.Notification{
					ProjectID:   pid,
					Severity:    inbox.SeverityInfoImmediate,
					EventType:   "concurrent",
					ContentHash: inbox.ComputeContentHash(map[string]any{"p": pid, "i": i}),
					Payload:     json.RawMessage(`{}`),
					CreatedAt:   time.Now().UTC().Add(time.Duration(i) * time.Second),
				})
			}
		}()
	}
	wg.Wait()

	for _, pid := range pids {
		got, err := a.List(ctx, inbox.ListFilter{ProjectID: pid})
		if err != nil {
			t.Fatalf("List(%s): %v", pid, err)
		}

		for _, n := range got {
			if n.ProjectID != pid {
				t.Errorf("CROSS-PROJECT LEAK: List(%s) returned row from %s", pid, n.ProjectID)
			}
		}
	}
}

func TestAdapterContextCancellation(t *testing.T) {
	s := openTestStore(t)
	a := NewAdapter(nil, s)

	pid := strings.Repeat("a", 64)
	a.RegisterProject(pid, "alpha", s)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cases := map[string]func() error{
		"Insert": func() error {
			return a.Insert(ctx, &inbox.Notification{
				ProjectID:   pid,
				Severity:    inbox.SeverityInfoImmediate,
				EventType:   "evt",
				ContentHash: strings.Repeat("a", 64),
				Payload:     json.RawMessage(`{}`),
				CreatedAt:   time.Now().UTC(),
			})
		},
		"Ack":             func() error { return a.Ack(ctx, 1) },
		"Snooze":          func() error { return a.Snooze(ctx, 1, time.Now().UTC()) },
		"List":            func() error { _, e := a.List(ctx, inbox.ListFilter{ProjectID: pid}); return e },
		"DeleteByProject": func() error { return a.DeleteByProject(ctx, pid) },
		"InsertCache": func() error {
			return a.InsertCache(ctx, inbox.CacheRow{
				ProjectID:      pid,
				ProjectAlias:   "alpha",
				NotificationID: 1,
				Severity:       inbox.SeverityInfoImmediate,
				EventType:      "evt",
				ContentHash:    strings.Repeat("a", 64),
				CreatedAt:      time.Now().UTC(),
			})
		},
		"Query":   func() error { _, e := a.Query(ctx, inbox.ListFilter{ProjectID: pid}); return e },
		"Rebuild": func() error { return a.Rebuild(ctx, []inbox.Store{a}) },
	}
	for name, fn := range cases {
		if err := fn(); err == nil {
			t.Errorf("%s: cancelled context must surface error", name)
		}
	}
}

func TestRouteStoreFallbackToDaemonStore(t *testing.T) {
	s := openTestStore(t)
	a := NewAdapter(nil, s)

	pid := strings.Repeat("z", 64)
	got, err := a.routeStore(pid)
	if err != nil {
		t.Fatalf("routeStore: %v", err)
	}
	if got != s {
		t.Errorf("routeStore returned wrong handle (want daemonStore)")
	}
}

func TestRouteStoreMissingPerProjectAndDaemon(t *testing.T) {
	s := openTestStore(t)
	a := NewAdapter(nil, s)
	a.daemonStore = nil

	pid := strings.Repeat("z", 64)
	if _, err := a.routeStore(pid); err == nil {
		t.Fatal("routeStore must fail when no store registered and no daemonStore")
	}
}

func TestAdapterInsertRejectsInvalidProjectID(t *testing.T) {
	s := openTestStore(t)
	a := NewAdapter(nil, s)

	ctx := context.Background()
	err := a.Insert(ctx, &inbox.Notification{
		ProjectID:   "too-short",
		Severity:    inbox.SeverityInfoImmediate,
		EventType:   "evt",
		ContentHash: strings.Repeat("a", 64),
		Payload:     json.RawMessage(`{}`),
		CreatedAt:   time.Now().UTC(),
	})
	if err == nil {
		t.Fatal("Insert with short projectID must fail")
	}
}

func TestAdapterInsertRejectsInvalidSeverity(t *testing.T) {
	s := openTestStore(t)
	a := NewAdapter(nil, s)

	ctx := context.Background()
	pid := strings.Repeat("a", 64)
	a.RegisterProject(pid, "alpha", s)

	err := a.Insert(ctx, &inbox.Notification{
		ProjectID:   pid,
		Severity:    inbox.Severity("made-up"),
		EventType:   "evt",
		ContentHash: strings.Repeat("a", 64),
		Payload:     json.RawMessage(`{}`),
		CreatedAt:   time.Now().UTC(),
	})
	if err == nil {
		t.Fatal("Insert with invalid severity must fail")
	}
}

func TestAdapterAliasAccessor(t *testing.T) {
	s := openTestStore(t)
	a := NewAdapter(nil, s)

	pid := strings.Repeat("a", 64)
	a.RegisterProject(pid, "internal-platform-x", s)

	if got := a.alias(pid); got != "internal-platform-x" {
		t.Errorf("alias = %q, want internal-platform-x", got)
	}
	if got := a.alias("unknown"); got != "" {
		t.Errorf("alias(unknown) = %q, want empty", got)
	}
}

func TestInsertCacheNoDaemonStore(t *testing.T) {
	s := openTestStore(t)
	a := NewAdapter(nil, s)
	a.daemonStore = nil

	err := a.InsertCache(context.Background(), inbox.CacheRow{
		ProjectID:      strings.Repeat("a", 64),
		ProjectAlias:   "alpha",
		NotificationID: 1,
		Severity:       inbox.SeverityInfoImmediate,
		EventType:      "evt",
		ContentHash:    strings.Repeat("a", 64),
		CreatedAt:      time.Now().UTC(),
	})
	if err == nil || !strings.Contains(err.Error(), "no daemonStore") {
		t.Errorf("expected no-daemonStore error; got: %v", err)
	}
}

func TestQueryNoDaemonStore(t *testing.T) {
	s := openTestStore(t)
	a := NewAdapter(nil, s)
	a.daemonStore = nil

	_, err := a.Query(context.Background(), inbox.ListFilter{ProjectID: strings.Repeat("a", 64)})
	if err == nil || !strings.Contains(err.Error(), "no daemonStore") {
		t.Errorf("expected no-daemonStore error; got: %v", err)
	}
}

func TestRebuildNoDaemonStore(t *testing.T) {
	s := openTestStore(t)
	a := NewAdapter(nil, s)
	a.daemonStore = nil

	err := a.Rebuild(context.Background(), []inbox.Store{})
	if err == nil || !strings.Contains(err.Error(), "no daemonStore") {
		t.Errorf("expected no-daemonStore error; got: %v", err)
	}
}

type errSource struct {
	err error
}

func (e *errSource) Insert(_ context.Context, _ *inbox.Notification) error { return e.err }
func (e *errSource) Ack(_ context.Context, _ int64) error                  { return e.err }
func (e *errSource) Snooze(_ context.Context, _ int64, _ time.Time) error  { return e.err }
func (e *errSource) List(_ context.Context, _ inbox.ListFilter) ([]inbox.Notification, error) {
	return nil, e.err
}
func (e *errSource) Delete(_ context.Context, _ string) error { return e.err }

func TestRebuildSourceListErrorRollsBack(t *testing.T) {
	s := openTestStore(t)
	a := NewAdapter(nil, s)

	pid := strings.Repeat("a", 64)
	a.RegisterProject(pid, "alpha", s)
	_ = a.InsertCache(context.Background(), inbox.CacheRow{
		ProjectID:      pid,
		ProjectAlias:   "alpha",
		NotificationID: 1,
		Severity:       inbox.SeverityInfoImmediate,
		EventType:      "evt",
		ContentHash:    strings.Repeat("a", 64),
		CreatedAt:      time.Now().UTC(),
	})

	src := &errSource{err: errors.New("simulated source failure")}
	err := a.Rebuild(context.Background(), []inbox.Store{src})
	if err == nil {
		t.Fatal("Rebuild must propagate source error")
	}
	if !strings.Contains(err.Error(), "Rebuild source") {
		t.Errorf("expected wrapped 'Rebuild source' error; got: %v", err)
	}
}

func TestUniqueStoresSkipsNilHandles(t *testing.T) {
	s := openTestStore(t)
	a := NewAdapter(nil, s)

	a.mu.Lock()
	a.perProject["nil-pid"] = nil
	a.mu.Unlock()

	got := a.uniqueStores()
	for _, s2 := range got {
		if s2 == nil {
			t.Error("uniqueStores returned a nil handle")
		}
	}

	if len(got) == 0 {
		t.Error("uniqueStores must always include the daemonStore")
	}
}

func TestIsDedupViolationOnNilErr(t *testing.T) {
	if isDedupViolation(nil) {
		t.Error("isDedupViolation(nil) must return false")
	}
}

func TestIsDedupViolationOnNonUniqueErr(t *testing.T) {
	if isDedupViolation(errors.New("disk IO error")) {
		t.Error("isDedupViolation must reject non-UNIQUE errors")
	}
}

func TestIsDedupViolationOnUniqueErr(t *testing.T) {
	if !isDedupViolation(errors.New("UNIQUE constraint failed: inbox.event_type, inbox.content_hash, inbox.created_at_bucket")) {
		t.Error("isDedupViolation must accept UNIQUE-constraint error")
	}
}

func TestIsDedupViolationOnConstraintErr(t *testing.T) {
	if !isDedupViolation(errors.New("constraint failed")) {
		t.Error("isDedupViolation must accept bare 'constraint' error")
	}
}

func TestNullableUnixOnNil(t *testing.T) {
	got := nullableUnix(nil)
	if got.Valid {
		t.Errorf("nullableUnix(nil) must be invalid; got %+v", got)
	}
}

func TestNullableUnixOnValue(t *testing.T) {
	t0 := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	got := nullableUnix(&t0)
	if !got.Valid {
		t.Error("nullableUnix(non-nil) must be valid")
	}
	if got.Int64 != t0.Unix() {
		t.Errorf("nullableUnix value = %d, want %d", got.Int64, t0.Unix())
	}
}

func TestRebuildClearOnExistingCachePopulatedRows(t *testing.T) {
	s := openTestStore(t)
	a := NewAdapter(nil, s)

	ctx := context.Background()
	pid := strings.Repeat("a", 64)
	_ = a.InsertCache(ctx, inbox.CacheRow{
		ProjectID:      pid,
		ProjectAlias:   "alpha",
		NotificationID: 1,
		Severity:       inbox.SeverityInfoImmediate,
		EventType:      "evt",
		ContentHash:    strings.Repeat("a", 64),
		CreatedAt:      time.Now().UTC(),
	})

	if err := a.Rebuild(ctx, nil); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	rows, _ := a.Query(ctx, inbox.ListFilter{IncludeAcked: true})
	if len(rows) != 0 {
		t.Errorf("post-Rebuild rows = %d, want 0", len(rows))
	}
}

func TestAdapterInsertNilNotification(t *testing.T) {
	s := openTestStore(t)
	a := NewAdapter(nil, s)
	if err := a.Insert(context.Background(), nil); err == nil {
		t.Fatal("Insert(nil) must fail")
	} else if !strings.Contains(err.Error(), "nil Notification") {
		t.Errorf("expected 'nil Notification' error; got: %v", err)
	}
}

func TestAdapterInsertUnknownProjectIDFallsBackToDaemonStore(t *testing.T) {
	s := openTestStore(t)
	a := NewAdapter(nil, s)
	// Do NOT call RegisterProject — the projectID resolves via the
	// daemonStore fallback path inside routeStore.
	pid := strings.Repeat("z", 64)
	err := a.Insert(context.Background(), &inbox.Notification{
		ProjectID:   pid,
		Severity:    inbox.SeverityInfoImmediate,
		EventType:   "evt",
		ContentHash: strings.Repeat("a", 64),
		Payload:     json.RawMessage(`{}`),
		CreatedAt:   time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("Insert via daemon fallback: %v", err)
	}
}

func TestSQLErrorPathsAfterStoreClose(t *testing.T) {
	s := openTestStore(t)
	a := NewAdapter(nil, s)

	ctx := context.Background()
	pid := strings.Repeat("a", 64)
	a.RegisterProject(pid, "alpha", s)

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	insertErr := a.Insert(ctx, &inbox.Notification{
		ProjectID:   pid,
		Severity:    inbox.SeverityInfoImmediate,
		EventType:   "evt",
		ContentHash: strings.Repeat("a", 64),
		Payload:     json.RawMessage(`{}`),
		CreatedAt:   time.Now().UTC(),
	})
	if insertErr == nil {
		t.Error("Insert against closed store must fail")
	}

	if err := a.Ack(ctx, 1); err == nil {
		t.Error("Ack against closed store must fail")
	}
	if err := a.Snooze(ctx, 1, time.Now().UTC()); err == nil {
		t.Error("Snooze against closed store must fail")
	}
	if _, err := a.List(ctx, inbox.ListFilter{ProjectID: pid}); err == nil {
		t.Error("List against closed store must fail")
	}
	if err := a.DeleteByProject(ctx, pid); err == nil {
		t.Error("DeleteByProject against closed store must fail")
	}
	if err := a.InsertCache(ctx, inbox.CacheRow{
		ProjectID:      pid,
		ProjectAlias:   "alpha",
		NotificationID: 1,
		Severity:       inbox.SeverityInfoImmediate,
		EventType:      "evt",
		ContentHash:    strings.Repeat("a", 64),
		CreatedAt:      time.Now().UTC(),
	}); err == nil {
		t.Error("InsertCache against closed store must fail")
	}
	if _, err := a.Query(ctx, inbox.ListFilter{ProjectID: pid}); err == nil {
		t.Error("Query against closed store must fail")
	}
	if err := a.Rebuild(ctx, nil); err == nil {
		t.Error("Rebuild against closed store must fail")
	}
}

func TestRebuildInsertErrorRollsBack(t *testing.T) {
	s := openTestStore(t)
	a := NewAdapter(nil, s)

	ctx := context.Background()
	pid := strings.Repeat("a", 64)
	a.RegisterProject(pid, "alpha", s)

	bad := &badSource{rows: []inbox.Notification{{
		ID:          1,
		ProjectID:   pid,
		Severity:    inbox.Severity("not-a-real-tier"),
		EventType:   "evt",
		ContentHash: strings.Repeat("a", 64),
		CreatedAt:   time.Now().UTC(),
	}}}

	err := a.Rebuild(ctx, []inbox.Store{bad})
	if err == nil {
		t.Fatal("Rebuild with CHECK-violating row must fail")
	}
	if !strings.Contains(err.Error(), "Rebuild insert") {
		t.Errorf("expected 'Rebuild insert' wrap; got: %v", err)
	}
}

type badSource struct {
	rows []inbox.Notification
}

func (b *badSource) Insert(_ context.Context, _ *inbox.Notification) error { return nil }
func (b *badSource) Ack(_ context.Context, _ int64) error                  { return nil }
func (b *badSource) Snooze(_ context.Context, _ int64, _ time.Time) error  { return nil }
func (b *badSource) List(_ context.Context, _ inbox.ListFilter) ([]inbox.Notification, error) {
	return b.rows, nil
}
func (b *badSource) Delete(_ context.Context, _ string) error { return nil }

func TestRouteStoreErrorInInsertList(t *testing.T) {
	s := openTestStore(t)
	a := NewAdapter(nil, s)
	a.daemonStore = nil

	ctx := context.Background()
	pid := strings.Repeat("a", 64)

	if err := a.Insert(ctx, &inbox.Notification{
		ProjectID:   pid,
		Severity:    inbox.SeverityInfoImmediate,
		EventType:   "evt",
		ContentHash: strings.Repeat("a", 64),
		Payload:     json.RawMessage(`{}`),
		CreatedAt:   time.Now().UTC(),
	}); err == nil {
		t.Error("Insert with routeStore error must fail")
	}

	if _, err := a.List(ctx, inbox.ListFilter{ProjectID: pid}); err == nil {
		t.Error("List with routeStore error must fail")
	}
}

func TestListLimitTruncationCrossStore(t *testing.T) {
	s1 := openTestStore(t)
	s2 := openTestStore(t)
	a := NewAdapter(nil, s1)

	ctx := context.Background()
	pidA := strings.Repeat("a", 64)
	pidB := strings.Repeat("b", 64)
	a.RegisterProject(pidA, "alpha", s1)
	a.RegisterProject(pidB, "beta", s2)

	for i, pid := range []string{pidA, pidB} {
		_ = a.Insert(ctx, &inbox.Notification{
			ProjectID:   pid,
			Severity:    inbox.SeverityInfoImmediate,
			EventType:   "evt",
			ContentHash: strings.Repeat([]string{"a", "b"}[i], 64),
			Payload:     json.RawMessage(`{}`),
			CreatedAt:   time.Now().UTC().Add(time.Duration(i) * time.Hour),
		})
	}

	got, err := a.List(ctx, inbox.ListFilter{Limit: 1, IncludeAcked: true})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("Limit=1 across two stores len = %d, want 1", len(got))
	}
}

func TestListAggregateErrorPropagates(t *testing.T) {
	s := openTestStore(t)
	a := NewAdapter(nil, s)
	pid := strings.Repeat("a", 64)
	a.RegisterProject(pid, "alpha", s)

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if _, err := a.List(context.Background(), inbox.ListFilter{}); err == nil {
		t.Error("List aggregate must propagate underlying SQL error")
	}
}

func TestQueryAckedAtRoundTrip(t *testing.T) {
	s := openTestStore(t)
	a := NewAdapter(nil, s)

	ctx := context.Background()
	pid := strings.Repeat("a", 64)

	now := time.Now().UTC().Truncate(time.Second)
	acked := now
	_ = a.InsertCache(ctx, inbox.CacheRow{
		ProjectID:      pid,
		ProjectAlias:   "alpha",
		NotificationID: 1,
		Severity:       inbox.SeverityInfoImmediate,
		EventType:      "evt",
		ContentHash:    strings.Repeat("a", 64),
		CreatedAt:      now,
		AckedAt:        &acked,
	})

	rows, _ := a.Query(ctx, inbox.ListFilter{ProjectID: pid, IncludeAcked: true})
	if len(rows) != 1 {
		t.Fatalf("len = %d, want 1", len(rows))
	}
	if rows[0].AckedAt == nil {
		t.Fatal("AckedAt should be set after round-trip")
	}
	if !rows[0].AckedAt.Equal(acked) {
		t.Errorf("AckedAt = %v, want %v", rows[0].AckedAt, acked)
	}
}

func TestSnoozedUntilRoundTripInList(t *testing.T) {
	s := openTestStore(t)
	a := NewAdapter(nil, s)

	ctx := context.Background()
	pid := strings.Repeat("a", 64)
	a.RegisterProject(pid, "alpha", s)

	n := &inbox.Notification{
		ProjectID:   pid,
		Severity:    inbox.SeverityInfoImmediate,
		EventType:   "evt",
		ContentHash: strings.Repeat("a", 64),
		Payload:     json.RawMessage(`{}`),
		CreatedAt:   time.Now().UTC(),
	}
	_ = a.Insert(ctx, n)

	until := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Second)
	_ = a.Snooze(ctx, n.ID, until)

	got, _ := a.List(ctx, inbox.ListFilter{ProjectID: pid, IncludeAcked: true})
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].SnoozedUntil == nil {
		t.Error("SnoozedUntil should round-trip from List")
	}
}

func TestAckedAtRoundTripInList(t *testing.T) {
	s := openTestStore(t)
	a := NewAdapter(nil, s)

	ctx := context.Background()
	pid := strings.Repeat("a", 64)
	a.RegisterProject(pid, "alpha", s)

	n := &inbox.Notification{
		ProjectID:   pid,
		Severity:    inbox.SeverityInfoImmediate,
		EventType:   "evt",
		ContentHash: strings.Repeat("a", 64),
		Payload:     json.RawMessage(`{}`),
		CreatedAt:   time.Now().UTC(),
	}
	_ = a.Insert(ctx, n)
	_ = a.Ack(ctx, n.ID)

	got, _ := a.List(ctx, inbox.ListFilter{ProjectID: pid, IncludeAcked: true})
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].AckedAt == nil {
		t.Error("AckedAt should round-trip from List")
	}
}

func TestDeleteByProjectCachePathErrorsWhenDaemonClosed(t *testing.T) {
	src := openTestStore(t)
	dst := openTestStore(t)
	a := NewAdapter(nil, dst)

	ctx := context.Background()
	pid := strings.Repeat("a", 64)
	a.RegisterProject(pid, "alpha", src)

	if err := dst.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	err := a.DeleteByProject(ctx, pid)
	if err == nil {
		t.Fatal("DeleteByProject must fail when daemon cache delete fails")
	}
	if !strings.Contains(err.Error(), "cache") {
		t.Errorf("expected cache-error wrap; got: %v", err)
	}
}

func TestRebuildClearErrorBranch(t *testing.T) {
	s := openTestStore(t)
	a := NewAdapter(nil, s)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := a.Rebuild(ctx, nil)
	if err == nil {
		t.Fatal("Rebuild with cancelled ctx must fail")
	}

	if !strings.Contains(err.Error(), "Rebuild") {
		t.Errorf("expected 'Rebuild' wrap; got: %v", err)
	}
}
