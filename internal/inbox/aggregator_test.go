package inbox

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestRebuildFromEmptySources covers the cold-rebuild boot path where the
// daemon starts up with NO per-project sources yet wired (fresh install,
// or after `zen project rm` of the only project). Pre-seeded cache rows
// MUST be discarded so the post-rebuild cache reflects ground truth
// (i.e. empty), not a stale residue.
//
// Also exercises the nil-slice-of-sources branch defensively — `nil` and
// an empty `[]Store{}` MUST be equivalent.
func TestRebuildFromEmptySources(t *testing.T) {
	ctx := context.Background()
	cache := newMemCacheStore()

	_ = cache.Insert(ctx, CacheRow{
		ProjectID: "stale-id-not-in-sources",
		Severity:  SeverityInfoImmediate, EventType: "x", ContentHash: strings.Repeat("a", 64),
		CreatedAt: time.Now().UTC(), NotificationID: 999,
	})

	if err := cache.Rebuild(ctx, nil); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	rows, err := cache.Query(ctx, ListFilter{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("Rebuild from empty sources: rows = %d, want 0", len(rows))
	}
}

// TestRebuildFromMultipleSources covers the typical daemon-boot path: N
// per-project Stores, each with a few rows. Rebuild MUST union them into
// the cache, MUST discard pre-seeded rows that no longer originate from
// any source, and MUST preserve inv-zen-113 (every cache row's ProjectID
// matches its origin source — no cross-project leak through the cold
// rehydration code path).
func TestRebuildFromMultipleSources(t *testing.T) {
	ctx := context.Background()

	pidA := "a" + strings.Repeat("0", 63)
	pidB := "b" + strings.Repeat("1", 63)

	srcA := NewMemStore()
	srcB := NewMemStore()

	insertN := func(s Store, pid string, ev string) {
		t.Helper()
		n := &Notification{
			ProjectID:   pid,
			Severity:    SeverityInfoImmediate,
			EventType:   ev,
			ContentHash: ComputeContentHash(map[string]any{"k": pid + ev}),
			Payload:     json.RawMessage(`{}`),
			CreatedAt:   time.Now().UTC(),
		}
		if err := s.Insert(ctx, n); err != nil {
			t.Fatalf("source Insert: %v", err)
		}
	}
	insertN(srcA, pidA, "evt-a-1")
	insertN(srcA, pidA, "evt-a-2")
	insertN(srcB, pidB, "evt-b-1")

	cache := newMemCacheStore()

	_ = cache.Insert(ctx, CacheRow{
		ProjectID: "garbage", Severity: SeverityInfoDigest,
		EventType: "garbage", ContentHash: strings.Repeat("0", 64),
		CreatedAt: time.Now().UTC(), NotificationID: 1,
	})

	if err := cache.Rebuild(ctx, []Store{srcA, srcB}); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	rows, err := cache.Query(ctx, ListFilter{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(rows) != 3 {
		t.Errorf("Rebuild: rows = %d, want 3 (2 from srcA + 1 from srcB)", len(rows))
	}

	got := map[string]int{}
	for _, r := range rows {
		got[r.ProjectID]++
	}
	if got[pidA] != 2 || got[pidB] != 1 {
		t.Errorf("Rebuild project_id distribution: %v, want %s=2, %s=1", got, pidA, pidB)
	}
}

func TestRebuildIsIdempotent(t *testing.T) {
	ctx := context.Background()
	pid := "a" + strings.Repeat("0", 63)
	src := NewMemStore()
	n := &Notification{
		ProjectID:   pid,
		Severity:    SeverityInfoImmediate,
		EventType:   "evt",
		ContentHash: strings.Repeat("a", 64),
		Payload:     json.RawMessage(`{}`),
		CreatedAt:   time.Now().UTC(),
	}
	if err := src.Insert(ctx, n); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	cache := newMemCacheStore()
	if err := cache.Rebuild(ctx, []Store{src}); err != nil {
		t.Fatalf("Rebuild#1: %v", err)
	}
	if err := cache.Rebuild(ctx, []Store{src}); err != nil {
		t.Fatalf("Rebuild#2: %v", err)
	}

	rows, _ := cache.Query(ctx, ListFilter{})
	if len(rows) != 1 {
		t.Errorf("Idempotent Rebuild: rows = %d, want 1 (no duplicates)", len(rows))
	}
}

// TestOutboxRecoverRebuildsBeforeDrain locks in the daemon-boot ordering
// contract: Outbox.Recover(ctx, sources) MUST call cache.Rebuild
// synchronously and return. Run is started AFTER Recover so live drain
// never races against the cold rehydration write path. Without this
// ordering, a drain goroutine could observe a half-populated cache and
// emit an inv-zen-113 false positive in the doctor probe.
func TestOutboxRecoverRebuildsBeforeDrain(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pid := "a" + strings.Repeat("0", 63)
	src := NewMemStore()
	if err := src.Insert(ctx, &Notification{
		ProjectID: pid, Severity: SeverityActionNeeded, EventType: "evt",
		ContentHash: strings.Repeat("a", 64),
		Payload:     json.RawMessage(`{}`), CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Insert source: %v", err)
	}

	cache := newMemCacheStore()
	out := NewOutbox(cache, 16)

	if err := out.Recover(ctx, []Store{src}); err != nil {
		t.Fatalf("Recover: %v", err)
	}

	rows, _ := cache.Query(ctx, ListFilter{})
	if len(rows) != 1 {
		t.Errorf("Recover: cache rows = %d, want 1", len(rows))
	}
}
