package daemon

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/store"
)

func openTestServerForEvictor(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return &Server{store: s}
}

func TestEvictResearchCacheExpired_DeletesExpiredOnly(t *testing.T) {
	srv := openTestServerForEvictor(t)
	now := time.Now().Unix()

	_, err := srv.store.DB().Exec(
		`INSERT INTO research_cache(hash, response_json, ttl_unix) VALUES (?, ?, ?), (?, ?, ?), (?, ?, ?)`,
		"expired", `{"k":"v"}`, now-3600,
		"fresh1", `{"k":"v"}`, now+3600,
		"fresh2", `{"k":"v"}`, now+7200,
	)
	if err != nil {
		t.Fatalf("seed insert: %v", err)
	}

	n, err := srv.EvictResearchCacheExpired()
	if err != nil {
		t.Fatalf("EvictResearchCacheExpired: %v", err)
	}
	if n != 1 {
		t.Errorf("EvictResearchCacheExpired returned %d, want 1", n)
	}

	var count int
	if err := srv.store.DB().QueryRow(`SELECT count(*) FROM research_cache`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("post-evict row count = %d, want 2", count)
	}
}

func TestEvictResearchCacheExpired_NilStore(t *testing.T) {
	srv := &Server{store: nil}
	n, err := srv.EvictResearchCacheExpired()
	if err != nil {
		t.Errorf("EvictResearchCacheExpired(nilStore) err = %v, want nil", err)
	}
	if n != 0 {
		t.Errorf("EvictResearchCacheExpired(nilStore) n = %d, want 0", n)
	}
}

func TestEvictResearchCacheExpired_BoundaryTTLNow(t *testing.T) {
	srv := openTestServerForEvictor(t)
	now := time.Now().Unix()

	_, err := srv.store.DB().Exec(
		`INSERT INTO research_cache(hash, response_json, ttl_unix) VALUES (?, ?, ?)`,
		"boundary", `{"k":"v"}`, now,
	)
	if err != nil {
		t.Fatalf("seed insert: %v", err)
	}

	n, evictErr := srv.EvictResearchCacheExpired()
	if evictErr != nil {
		t.Errorf("EvictResearchCacheExpired boundary: unexpected error %v", evictErr)
	}

	if n > 1 {
		t.Errorf("boundary: deleted %d rows, want at most 1", n)
	}
}

type fakeEvictor struct {
	calls atomic.Int32
}

func (f *fakeEvictor) EvictResearchCacheExpired() (int64, error) {
	f.calls.Add(1)
	return 0, nil
}

func TestStartResearchCacheEvictor_TickerFires(t *testing.T) {
	f := &fakeEvictor{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := startResearchCacheEvictor(ctx, f, 10*time.Millisecond)

	time.Sleep(55 * time.Millisecond)
	cancel()
	<-done

	if got := f.calls.Load(); got < 2 {
		t.Errorf("evictor calls = %d, want >= 2 within 55ms at 10ms interval", got)
	}
}

func TestStartResearchCacheEvictor_ContextCancelStops(t *testing.T) {
	f := &fakeEvictor{}
	ctx, cancel := context.WithCancel(context.Background())
	done := startResearchCacheEvictor(ctx, f, 1*time.Hour)

	cancel()
	select {
	case <-done:

	case <-time.After(200 * time.Millisecond):
		t.Fatal("evictor goroutine did not exit within 200ms of context cancel")
	}
}

func TestStartResearchCacheEvictor_NilEvictor(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 55*time.Millisecond)
	defer cancel()

	done := startResearchCacheEvictor(ctx, nil, 10*time.Millisecond)
	<-done
}

func TestStartResearchCacheEvictor_ZeroIntervalFallsBackToDefault(t *testing.T) {
	f := &fakeEvictor{}
	ctx, cancel := context.WithCancel(context.Background())

	done := startResearchCacheEvictor(ctx, f, 0)

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("evictor did not exit after cancel with zero interval")
	}

	if got := f.calls.Load(); got > 1 {
		t.Errorf("zero-interval: expected at most 1 call (immediate sweep only), got %d — interval may not have defaulted to 1h", got)
	}
}

type errEvictor struct {
	err error
	n   int64
}

func (e *errEvictor) EvictResearchCacheExpired() (int64, error) {
	return e.n, e.err
}

func TestRunEvictionSweep_ErrorPathLogs(t *testing.T) {
	e := &errEvictor{err: context.DeadlineExceeded, n: 0}
	ctx := context.Background()

	runEvictionSweep(ctx, e)
}

func TestRunEvictionSweep_DeletedRowsPathLogs(t *testing.T) {
	e := &errEvictor{err: nil, n: 3}
	ctx := context.Background()

	runEvictionSweep(ctx, e)
}
