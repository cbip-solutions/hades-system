//go:build cgo
// +build cgo

package cache

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const sweepFixedETag = `"zen-f11-fixture-sweep-abc"`

const sweepFreshBody = "sweep fixture fresh body — zen-swarm Plan 9 F-11"

type sweepFixtureServer struct {
	srv *httptest.Server
	URL string
}

func newSweepFixtureServer() *sweepFixtureServer {
	var s sweepFixtureServer
	mux := http.NewServeMux()

	mux.HandleFunc("/fresh-etag", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", sweepFixedETag)
		w.Header().Set("Last-Modified", "Sat, 01 Jan 2000 00:00:00 GMT")
		if r.Method == http.MethodHead {
			if r.Header.Get("If-None-Match") == sweepFixedETag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("Content-Length", "49")
			w.WriteHeader(http.StatusOK)
			return
		}

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sweepFreshBody))
	})

	mux.HandleFunc("/timeout", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Second)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("too late"))
	})

	s.srv = httptest.NewServer(mux)
	s.URL = s.srv.URL
	return &s
}

func (s *sweepFixtureServer) Close() { s.srv.Close() }

func seedStaleFinding(t *testing.T, db *DB, fixtureURL string, suffix string) string {
	t.Helper()
	ctx := context.Background()
	dispatchID := "sweep-test-dispatch-" + suffix
	findingID := "sweep-test-finding-" + suffix
	oneWeekAgo := time.Now().Add(-7 * 24 * time.Hour).Unix()

	_, err := db.SQL.ExecContext(ctx,
		`INSERT INTO research_dispatches
		 (id, query, query_text_hash, status, project_id, session_id,
		  cache_hit_reason, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		dispatchID,
		"sweep test query "+suffix,
		ComputeQueryHash("sweep test query "+suffix),
		string(DispatchStatusDone),
		"sweep-project",
		"sweep-session",
		string(CacheHitMiss),
		oneWeekAgo, oneWeekAgo,
	)
	if err != nil {
		t.Fatalf("seedStaleFinding: insert dispatch: %v", err)
	}

	_, err = db.SQL.ExecContext(ctx,
		`INSERT INTO research_findings
		 (id, dispatch_id, url, title, snippet, freshness_status,
		  retrieved_at, content_hash, body_inline_blob, body_path)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		findingID,
		dispatchID,
		fixtureURL,
		"sweep test title "+suffix,
		"sweep test snippet "+suffix,
		string(FreshnessFresh),
		oneWeekAgo,
		sweepFixedETag,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("seedStaleFinding: insert finding: %v", err)
	}

	return findingID
}

func TestSweeperRunsBatchAndExits(t *testing.T) {
	t.Parallel()

	srv := newSweepFixtureServer()
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	db, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.SQL.Close()

	for i := 0; i < 3; i++ {
		suffix := itoa(i)
		seedStaleFinding(t, db, srv.URL+"/fresh-etag", suffix)
	}

	var emitted []string
	sink := &captureSink{fn: func(eventType string, _ []byte) {
		emitted = append(emitted, eventType)
	}}

	reval := NewRevalidator(ValidateOpts{Timeout: 1 * time.Second})
	sweeper := &Sweeper{
		DB:          db,
		Revalidator: reval,
		Sink:        sink,
		Cadence:     50 * time.Millisecond,
		BatchSize:   50,
	}

	err = sweeper.Run(ctx)

	if err != nil && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Fatalf("Run: unexpected error: %v", err)
	}

	freshCount := 0
	for _, et := range emitted {
		if et == EventResearchCacheRevalidatedFresh {
			freshCount++
		}
	}
	if freshCount == 0 {
		t.Errorf("want ≥1 %s event, got 0 (all emitted: %v)", EventResearchCacheRevalidatedFresh, emitted)
	}
}

func TestSweeperContextCancelExitsCleanly(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	db, err := Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.SQL.Close()

	reval := NewRevalidator(ValidateOpts{})
	sink := &captureSink{}
	sweeper := &Sweeper{
		DB:          db,
		Revalidator: reval,
		Sink:        sink,
	}

	err = sweeper.Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("want context.Canceled, got %v", err)
	}
}

func TestSweeperDefaultCadence(t *testing.T) {
	t.Parallel()

	s := &Sweeper{}
	s.normalize()

	if s.Cadence != 24*time.Hour {
		t.Errorf("want Cadence=24h, got %v", s.Cadence)
	}
	if s.BatchSize != 100 {
		t.Errorf("want BatchSize=100, got %d", s.BatchSize)
	}
}

func TestSweeperRespectsContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	db, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.SQL.Close()

	reval := NewRevalidator(ValidateOpts{Timeout: 200 * time.Millisecond})
	sink := &captureSink{}
	sweeper := &Sweeper{
		DB:          db,
		Revalidator: reval,
		Sink:        sink,
		Cadence:     50 * time.Millisecond,
		BatchSize:   10,
	}

	start := time.Now()
	_ = sweeper.Run(ctx)
	elapsed := time.Since(start)

	if elapsed >= 500*time.Millisecond {
		t.Errorf("Run took %v — did not respect context deadline (want <500ms)", elapsed)
	}
}

func TestSweeperStalePathEmitsStaleEvent(t *testing.T) {
	t.Parallel()

	srv := newRevalFixtureServer()
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	db, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.SQL.Close()

	dispatchID := "sweep-stale-dispatch-1"
	findingID := "sweep-stale-finding-1"
	oneWeekAgo := time.Now().Add(-7 * 24 * time.Hour).Unix()
	storedHash := sha256Hex([]byte(testFreshBody))

	_, err = db.SQL.ExecContext(ctx,
		`INSERT INTO research_dispatches
		 (id, query, query_text_hash, status, project_id, session_id,
		  cache_hit_reason, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		dispatchID, "stale sweep query", ComputeQueryHash("stale sweep query"),
		string(DispatchStatusDone), "p", "s", string(CacheHitMiss),
		oneWeekAgo, oneWeekAgo,
	)
	if err != nil {
		t.Fatalf("insert dispatch: %v", err)
	}
	_, err = db.SQL.ExecContext(ctx,
		`INSERT INTO research_findings
		 (id, dispatch_id, url, title, snippet, freshness_status,
		  retrieved_at, content_hash, body_inline_blob, body_path)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		findingID, dispatchID,
		srv.URL+"/changed-etag",
		"stale title", "stale snippet",
		string(FreshnessFresh),
		oneWeekAgo, storedHash, nil, nil,
	)
	if err != nil {
		t.Fatalf("insert finding: %v", err)
	}

	var emitted []string
	sink := &captureSink{fn: func(et string, _ []byte) {
		emitted = append(emitted, et)
	}}

	reval := NewRevalidator(ValidateOpts{Timeout: 1 * time.Second})
	sweeper := &Sweeper{
		DB:          db,
		Revalidator: reval,
		Sink:        sink,
		Cadence:     50 * time.Millisecond,
		BatchSize:   10,
	}

	_ = sweeper.Run(ctx)

	staleCount := 0
	for _, et := range emitted {
		if et == EventResearchCacheRevalidatedStaleRefetched {
			staleCount++
		}
	}
	if staleCount == 0 {
		t.Errorf("want ≥1 %s event, got 0 (emitted: %v)", EventResearchCacheRevalidatedStaleRefetched, emitted)
	}
}

func TestSweeperRunNilFieldsReturnError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("nil DB", func(t *testing.T) {
		t.Parallel()
		s := &Sweeper{Revalidator: NewRevalidator(ValidateOpts{}), Sink: &captureSink{}}
		err := s.Run(ctx)
		if err == nil {
			t.Error("want error for nil DB, got nil")
		}
	})

	t.Run("nil Revalidator", func(t *testing.T) {
		t.Parallel()
		db, _ := Open(ctx, ":memory:")
		defer db.SQL.Close()
		s := &Sweeper{DB: db, Sink: &captureSink{}}
		err := s.Run(ctx)
		if err == nil {
			t.Error("want error for nil Revalidator, got nil")
		}
	})

	t.Run("nil Sink", func(t *testing.T) {
		t.Parallel()
		db, _ := Open(ctx, ":memory:")
		defer db.SQL.Close()
		s := &Sweeper{DB: db, Revalidator: NewRevalidator(ValidateOpts{})}
		err := s.Run(ctx)
		if err == nil {
			t.Error("want error for nil Sink, got nil")
		}
	})
}

func TestSweeperMigrationIdempotent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.SQL.Close()

	if err := applyMigrationV5(ctx, db.SQL); err != nil {
		t.Fatalf("second applyMigrationV5 call: %v", err)
	}
}

func TestSweeperNonExpiredFindingsSkipped(t *testing.T) {
	t.Parallel()

	srv := newSweepFixtureServer()
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	db, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.SQL.Close()

	dispatchID := "sweep-fresh-dispatch-1"
	findingID := "sweep-fresh-finding-1"
	nowUnix := time.Now().Unix()

	_, err = db.SQL.ExecContext(ctx,
		`INSERT INTO research_dispatches
		 (id, query, query_text_hash, status, project_id, session_id,
		  cache_hit_reason, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		dispatchID, "fresh sweep query", ComputeQueryHash("fresh sweep query"),
		string(DispatchStatusDone), "p", "s", string(CacheHitMiss),
		nowUnix, nowUnix,
	)
	if err != nil {
		t.Fatalf("insert dispatch: %v", err)
	}
	_, err = db.SQL.ExecContext(ctx,
		`INSERT INTO research_findings
		 (id, dispatch_id, url, title, snippet, freshness_status,
		  retrieved_at, content_hash, body_inline_blob, body_path)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		findingID, dispatchID,
		srv.URL+"/fresh-etag",
		"fresh title", "fresh snippet",
		string(FreshnessFresh),
		nowUnix, sweepFixedETag, nil, nil,
	)
	if err != nil {
		t.Fatalf("insert finding: %v", err)
	}

	var emitted []string
	sink := &captureSink{fn: func(et string, _ []byte) {
		emitted = append(emitted, et)
	}}

	reval := NewRevalidator(ValidateOpts{Timeout: 1 * time.Second})
	sweeper := &Sweeper{
		DB:          db,
		Revalidator: reval,
		Sink:        sink,
		Cadence:     50 * time.Millisecond,
		BatchSize:   10,
	}

	_ = sweeper.Run(ctx)

	for _, et := range emitted {
		if et == EventResearchCacheRevalidatedFresh || et == EventResearchCacheRevalidatedStaleRefetched {
			t.Errorf("unexpected revalidation event %q for non-expired finding", et)
		}
	}
}

func TestSweeperStalePathEmptyHashPreservesOldHash(t *testing.T) {
	t.Parallel()

	srv := newRevalFixtureServer()
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	db, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.SQL.Close()

	dispatchID := "sweep-404-dispatch-1"
	findingID := "sweep-404-finding-1"
	oneWeekAgo := time.Now().Add(-7 * 24 * time.Hour).Unix()
	oldHash := "oldhash404abc"

	_, err = db.SQL.ExecContext(ctx,
		`INSERT INTO research_dispatches
		 (id, query, query_text_hash, status, project_id, session_id,
		  cache_hit_reason, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		dispatchID, "sweep 404 query", ComputeQueryHash("sweep 404 query"),
		string(DispatchStatusDone), "p", "s", string(CacheHitMiss),
		oneWeekAgo, oneWeekAgo,
	)
	if err != nil {
		t.Fatalf("insert dispatch: %v", err)
	}
	_, err = db.SQL.ExecContext(ctx,
		`INSERT INTO research_findings
		 (id, dispatch_id, url, title, snippet, freshness_status,
		  retrieved_at, content_hash, body_inline_blob, body_path)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		findingID, dispatchID,
		srv.URL+"/404",
		"404 title", "404 snippet",
		string(FreshnessFresh),
		oneWeekAgo, oldHash, nil, nil,
	)
	if err != nil {
		t.Fatalf("insert finding: %v", err)
	}

	var emitted []string
	sink := &captureSink{fn: func(et string, _ []byte) {
		emitted = append(emitted, et)
	}}

	reval := NewRevalidator(ValidateOpts{Timeout: 1 * time.Second})
	sweeper := &Sweeper{
		DB:          db,
		Revalidator: reval,
		Sink:        sink,
		Cadence:     50 * time.Millisecond,
		BatchSize:   10,
	}

	_ = sweeper.Run(ctx)

	verCtx := context.Background()

	var gotHash string
	err = db.SQL.QueryRowContext(verCtx,
		`SELECT content_hash FROM research_findings WHERE id = ?`, findingID,
	).Scan(&gotHash)
	if err != nil {
		t.Fatalf("select content_hash: %v", err)
	}
	if gotHash != oldHash {
		t.Errorf("want preserved hash %q, got %q", oldHash, gotHash)
	}

	staleCount := 0
	for _, et := range emitted {
		if et == EventResearchCacheRevalidatedStaleRefetched {
			staleCount++
		}
	}
	if staleCount == 0 {
		t.Errorf("want ≥1 stale refetched event, got 0 (emitted: %v)", emitted)
	}
}

func TestRevalidateOneValidateError(t *testing.T) {
	t.Parallel()

	srv := newRevalFixtureServer()
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	db, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.SQL.Close()

	dispatchID := "sweep-5xx-dispatch-1"
	findingID := "sweep-5xx-finding-1"
	oneWeekAgo := time.Now().Add(-7 * 24 * time.Hour).Unix()

	_, err = db.SQL.ExecContext(ctx,
		`INSERT INTO research_dispatches
		 (id, query, query_text_hash, status, project_id, session_id,
		  cache_hit_reason, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		dispatchID, "sweep 5xx query", ComputeQueryHash("sweep 5xx query"),
		string(DispatchStatusDone), "p", "s", string(CacheHitMiss),
		oneWeekAgo, oneWeekAgo,
	)
	if err != nil {
		t.Fatalf("insert dispatch: %v", err)
	}

	_, err = db.SQL.ExecContext(ctx,
		`INSERT INTO research_findings
		 (id, dispatch_id, url, title, snippet, freshness_status,
		  retrieved_at, content_hash, body_inline_blob, body_path)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		findingID, dispatchID,
		srv.URL+"/500",
		"5xx title", "5xx snippet",
		string(FreshnessFresh),
		oneWeekAgo, "some-hash", nil, nil,
	)
	if err != nil {
		t.Fatalf("insert finding: %v", err)
	}

	var emitted []string
	sink := &captureSink{fn: func(et string, _ []byte) {
		emitted = append(emitted, et)
	}}

	reval := NewRevalidator(ValidateOpts{Timeout: 1 * time.Second})
	sweeper := &Sweeper{
		DB:          db,
		Revalidator: reval,
		Sink:        sink,
		Cadence:     50 * time.Millisecond,
		BatchSize:   10,
	}

	_ = sweeper.Run(ctx)

	for _, et := range emitted {
		if et == EventResearchCacheRevalidatedFresh || et == EventResearchCacheRevalidatedStaleRefetched {
			t.Errorf("unexpected revalidation event %q for 5xx-error finding", et)
		}
	}

	verCtx := context.Background()

	var vlogCount int
	err = db.SQL.QueryRowContext(verCtx,
		`SELECT COUNT(*) FROM research_validation_log WHERE finding_id = ?`, findingID,
	).Scan(&vlogCount)
	if err != nil {
		t.Fatalf("select vlog count: %v", err)
	}
	if vlogCount == 0 {
		t.Errorf("want ≥1 validation_log row for error finding, got 0")
	}
}

func TestNullableIntZeroReturnsNil(t *testing.T) {
	t.Parallel()
	if v := nullableInt(0); v != nil {
		t.Errorf("nullableInt(0): want nil, got %v", v)
	}
	if v := nullableInt(200); v == nil {
		t.Errorf("nullableInt(200): want non-nil, got nil")
	}
}

func TestNullableNullInt64BothBranches(t *testing.T) {
	t.Parallel()
	invalid := nullableNullInt64(sql.NullInt64{Valid: false})
	if invalid != nil {
		t.Errorf("invalid NullInt64: want nil, got %v", invalid)
	}
	valid := nullableNullInt64(sql.NullInt64{Int64: 1, Valid: true})
	if valid == nil {
		t.Errorf("valid NullInt64: want non-nil, got nil")
	}
}

type captureSink struct {
	fn func(eventType string, payload []byte)
}

func (s *captureSink) Emit(_ context.Context, eventType string, payload []byte) error {
	if s.fn != nil {
		s.fn(eventType, payload)
	}
	return nil
}
