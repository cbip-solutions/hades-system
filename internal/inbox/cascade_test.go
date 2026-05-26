package inbox

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestCascadeDeleteRemovesPerProjectAndCache(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srcA := NewMemStore()
	srcB := NewMemStore()
	cache := newMemCacheStore()
	out := NewOutbox(cache, 64)
	go out.Run(ctx)

	pidA := "a" + strings.Repeat("0", 63)
	pidB := "b" + strings.Repeat("1", 63)

	insert := func(s Store, pid string, idx int) *Notification {
		t.Helper()
		n := &Notification{
			ProjectID:   pid,
			Severity:    SeverityActionNeeded,
			EventType:   "evt",
			ContentHash: ComputeContentHash(map[string]any{"p": pid, "i": idx}),
			Payload:     json.RawMessage(`{}`),
			CreatedAt:   time.Now().UTC().Add(time.Duration(idx) * time.Second),
		}
		if err := s.Insert(ctx, n); err != nil {
			t.Fatalf("Insert: %v", err)
		}
		_ = out.Enqueue(CacheWrite{Notification: *n, ProjectAlias: pid[:5]})
		return n
	}

	for i := 0; i < 5; i++ {
		insert(srcA, pidA, i)
	}
	for i := 0; i < 3; i++ {
		insert(srcB, pidB, i)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rows, _ := cache.Query(ctx, ListFilter{})
		if len(rows) == 8 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	rows, _ := cache.Query(ctx, ListFilter{})
	if len(rows) != 8 {
		t.Fatalf("post-fanout cache rows = %d, want 8", len(rows))
	}

	if err := srcA.Delete(ctx, pidA); err != nil {
		t.Fatalf("srcA.Delete: %v", err)
	}
	if err := cache.DeleteByProject(ctx, pidA); err != nil {
		t.Fatalf("cache.DeleteByProject: %v", err)
	}

	rowsA, _ := srcA.List(ctx, ListFilter{ProjectID: pidA, IncludeAcked: true})
	if len(rowsA) != 0 {
		t.Errorf("after cascade: srcA rows for A = %d, want 0", len(rowsA))
	}
	cacheA, _ := cache.Query(ctx, ListFilter{ProjectID: pidA, IncludeAcked: true})
	if len(cacheA) != 0 {
		t.Errorf("after cascade: cache rows for A = %d, want 0", len(cacheA))
	}

	rowsB, _ := srcB.List(ctx, ListFilter{ProjectID: pidB, IncludeAcked: true})
	if len(rowsB) != 3 {
		t.Errorf("after cascade: srcB rows for B = %d, want 3", len(rowsB))
	}
	cacheB, _ := cache.Query(ctx, ListFilter{ProjectID: pidB, IncludeAcked: true})
	if len(cacheB) != 3 {
		t.Errorf("after cascade: cache rows for B = %d, want 3", len(cacheB))
	}
}
