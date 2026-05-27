// go:build chaos

// Drives the contract from spec §4.6 row "Disk full":
// - Daemon-owned writes (inbox.Aggregator.Insert,
// scheduler.Store.AppendHistory) MUST fail-loud when the backing
// storage returns ENOSPC / "database or disk is full" errors.
// The error propagates to the caller with the original cause
// reachable via errors.Is — operators can grep audit logs.
// - The daemon does NOT silently swallow disk errors; aggregator
// errors are visible at the Insert call site (inv: per-project
// state.db is authoritative; cache fanout is best-effort but the
// authoritative write failure is the source of truth).
// - Recovery: when disk space frees (storage stops returning the
// error), subsequent writes succeed. State is not corrupted by
// the prior failures.
// - Backpressure: when the cache is full or unwritable, the outbox
// queue depth grows but writes do not block forever — the
// buffered channel either accepts or returns ErrOutboxFull
// synchronously.
//
// Drives REAL production code: inbox.Aggregator + Outbox.Recover +
// scheduler.Fire stack. Distinct from K-2 (which corrupts the cache
// at the row level); K-6 simulates persistent IO errors at the
// store layer.
package chaos

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/inbox"
)

func uniqueEventType(base string, idx int) string {
	return base + "." + intToHex(idx)
}

func uniqueHash(t *testing.T, idx int) string {
	t.Helper()
	out := []byte("0000000000000000000000000000000000000000000000000000000000000000")
	hex := intToHex(idx)
	copy(out[len(out)-len(hex):], hex)
	return string(out)
}

func intToHex(idx int) string {
	const hexChars = "0123456789abcdef"
	if idx == 0 {
		return "0"
	}
	out := []byte{}
	for idx > 0 {
		out = append([]byte{hexChars[idx&0xf]}, out...)
		idx >>= 4
	}
	return string(out)
}

var errDiskFull = errors.New("chaos: disk full (simulated ENOSPC)")

type flakyDiskInboxStore struct {
	mu           sync.Mutex
	under        inbox.Store
	failuresLeft int32
}

func newFlakyDiskInboxStore(under inbox.Store, failures int32) *flakyDiskInboxStore {
	return &flakyDiskInboxStore{under: under, failuresLeft: failures}
}

func (f *flakyDiskInboxStore) Insert(ctx context.Context, n *inbox.Notification) error {
	if atomic.LoadInt32(&f.failuresLeft) > 0 {
		atomic.AddInt32(&f.failuresLeft, -1)
		return errDiskFull
	}
	return f.under.Insert(ctx, n)
}

func (f *flakyDiskInboxStore) Ack(ctx context.Context, id int64) error {
	return f.under.Ack(ctx, id)
}

func (f *flakyDiskInboxStore) Snooze(ctx context.Context, id int64, until time.Time) error {
	return f.under.Snooze(ctx, id, until)
}

func (f *flakyDiskInboxStore) List(ctx context.Context, filter inbox.ListFilter) ([]inbox.Notification, error) {
	return f.under.List(ctx, filter)
}

func (f *flakyDiskInboxStore) Delete(ctx context.Context, projectID string) error {
	return f.under.Delete(ctx, projectID)
}

type flakyDiskCacheStore struct {
	mu           sync.Mutex
	rows         []inbox.CacheRow
	insertFails  atomic.Int32
	rebuildFails atomic.Int32
}

func newFlakyDiskCacheStore() *flakyDiskCacheStore { return &flakyDiskCacheStore{} }

func (f *flakyDiskCacheStore) Insert(_ context.Context, r inbox.CacheRow) error {
	if f.insertFails.Load() > 0 {
		f.insertFails.Add(-1)
		return errDiskFull
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	r.CacheID = int64(len(f.rows) + 1)
	f.rows = append(f.rows, r)
	return nil
}

func (f *flakyDiskCacheStore) DeleteByProject(_ context.Context, projectID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	kept := f.rows[:0]
	for _, r := range f.rows {
		if r.ProjectID != projectID {
			kept = append(kept, r)
		}
	}
	f.rows = kept
	return nil
}

func (f *flakyDiskCacheStore) Query(_ context.Context, filter inbox.ListFilter) ([]inbox.CacheRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []inbox.CacheRow
	for _, r := range f.rows {
		if filter.ProjectID != "" && r.ProjectID != filter.ProjectID {
			continue
		}
		if !filter.IncludeAcked && r.AckedAt != nil {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

func (f *flakyDiskCacheStore) Rebuild(ctx context.Context, sources []inbox.Store) error {
	if f.rebuildFails.Load() > 0 {
		f.rebuildFails.Add(-1)
		return errDiskFull
	}
	f.mu.Lock()
	f.rows = nil
	f.mu.Unlock()
	for _, s := range sources {
		ns, err := s.List(ctx, inbox.ListFilter{IncludeAcked: true})
		if err != nil {
			return err
		}
		for _, n := range ns {
			r := inbox.CacheRow{
				ProjectID:      n.ProjectID,
				NotificationID: n.ID,
				Severity:       n.Severity,
				EventType:      n.EventType,
				ContentHash:    n.ContentHash,
				CreatedAt:      n.CreatedAt,
				AckedAt:        n.AckedAt,
			}
			if err := f.Insert(ctx, r); err != nil {
				return err
			}
		}
	}
	return nil
}

func (f *flakyDiskCacheStore) RowCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.rows)
}

func TestChaos_DiskFull_AggregatorInsertFailLoud(t *testing.T) {
	ctx := context.Background()
	pid := "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111"
	flakyStore := newFlakyDiskInboxStore(inbox.NewMemStore(), 3)
	cache := newFlakyDiskCacheStore()
	agg := inbox.NewAggregator(flakyStore, cache)

	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	n := inbox.Notification{
		ProjectID:   pid,
		Severity:    inbox.SeverityActionNeeded,
		EventType:   "chaos.diskfull",
		ContentHash: "00000000000000000000000000000000000000000000000000000000abcdef01",
		CreatedAt:   now,
	}

	for i := 0; i < 3; i++ {
		err := agg.Insert(ctx, n)
		if err == nil {
			t.Fatalf("attempt %d: expected disk-full error, got nil", i)
		}
		if !errors.Is(err, errDiskFull) {
			t.Fatalf("attempt %d: expected errors.Is(err, errDiskFull); got %v", i, err)
		}
	}

	n.ContentHash = "00000000000000000000000000000000000000000000000000000000abcdef02"
	if err := agg.Insert(ctx, n); err != nil {
		t.Fatalf("post-recovery Insert: %v", err)
	}
}

func TestChaos_DiskFull_OutboxBackpressureNoBlocking(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pid := "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111"
	authStore := inbox.NewMemStore()
	cache := newFlakyDiskCacheStore()
	cache.insertFails.Store(20)

	agg := inbox.NewAggregator(authStore, cache)
	go agg.Outbox().Run(ctx)

	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)

	for i := 0; i < 30; i++ {
		n := inbox.Notification{
			ProjectID:   pid,
			Severity:    inbox.SeverityActionNeeded,
			EventType:   uniqueEventType("chaos.cachefail", i),
			ContentHash: uniqueHash(t, i),
			CreatedAt:   now.Add(time.Duration(i) * time.Millisecond),
		}
		if err := agg.Insert(ctx, n); err != nil {
			t.Fatalf("authoritative Insert %d failed: %v", i, err)
		}
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_, drained, _, cacheErr := agg.Outbox().Stats()
		if drained+cacheErr >= 30 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	_, drained, _, cacheErr := agg.Outbox().Stats()
	if drained+cacheErr < 30 {
		t.Fatalf("outbox drain stalled: drained=%d cacheError=%d (want sum>=30)",
			drained, cacheErr)
	}
	if cacheErr < 20 {
		t.Fatalf("cacheError count: got %d, want >=20 (the disk-full window)",
			cacheErr)
	}

	if drained < 10 {
		t.Fatalf("post-recovery drained: got %d, want >=10", drained)
	}
}

func TestChaos_DiskFull_RebuildRetryablePostRecovery(t *testing.T) {
	ctx := context.Background()
	pid := "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111"
	authStore := inbox.NewMemStore()
	cache := newFlakyDiskCacheStore()
	agg := inbox.NewAggregator(authStore, cache)

	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		if err := authStore.Insert(ctx, &inbox.Notification{
			ProjectID:   pid,
			Severity:    inbox.SeverityInfoImmediate,
			EventType:   uniqueEventType("chaos.rebuild", i),
			ContentHash: uniqueHash(t, i),
			CreatedAt:   now.Add(time.Duration(i) * time.Millisecond),
		}); err != nil {
			t.Fatalf("seed Insert %d: %v", i, err)
		}
	}

	cache.rebuildFails.Store(1)
	err := agg.Outbox().Recover(ctx, []inbox.Store{authStore})
	if err == nil {
		t.Fatalf("expected first rebuild to fail with disk-full, got nil")
	}
	if !errors.Is(err, errDiskFull) {
		t.Fatalf("expected errors.Is(err, errDiskFull), got %v", err)
	}

	if err := agg.Outbox().Recover(ctx, []inbox.Store{authStore}); err != nil {
		t.Fatalf("post-recovery Recover: %v", err)
	}
	if got := cache.RowCount(); got != 5 {
		t.Fatalf("post-recovery cache row count: got %d, want 5", got)
	}
}

func TestChaos_DiskFull_OutboxFullReturnsSentinel(t *testing.T) {

	cache := newFlakyDiskCacheStore()
	outbox := inbox.NewOutbox(cache, 4)

	for i := 0; i < 10; i++ {
		err := outbox.Enqueue(inbox.CacheWrite{
			Notification: inbox.Notification{
				ProjectID: "p",
				Severity:  inbox.SeverityActionNeeded,
				EventType: "chaos.outboxfull",
			},
			ProjectAlias: "p",
		})
		if i < 4 {
			if err != nil {
				t.Fatalf("Enqueue %d: unexpected error: %v", i, err)
			}
		} else {
			if !errors.Is(err, inbox.ErrOutboxFull) {
				t.Fatalf("Enqueue %d: expected ErrOutboxFull, got %v", i, err)
			}
		}
	}

	if outbox.Pending() != 4 {
		t.Fatalf("Pending: got %d, want 4 (cap)", outbox.Pending())
	}
	_, _, rejected, _ := outbox.Stats()
	if rejected != 6 {
		t.Fatalf("rejected count: got %d, want 6", rejected)
	}
}
