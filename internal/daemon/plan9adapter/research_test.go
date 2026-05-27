// go:build cgo
//go:build cgo
// +build cgo

package plan9adapter

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/daemon/handlers"
	"github.com/cbip-solutions/hades-system/internal/research/cache"
)

func TestResearchAdapterHistoryFiltersAndLimit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := openResearchAdapterDB(t)
	seedResearchDispatch(t, db, seedResearchRow{
		ID: "d-hit", Query: "Cache Hit", ProjectID: "proj-A", CreatedAt: 100,
		CacheHitReason: cache.CacheHitExact, Findings: 2,
	})
	seedResearchDispatch(t, db, seedResearchRow{
		ID: "d-fresh", Query: "Fresh Dispatch", ProjectID: "proj-A", CreatedAt: 90,
		CacheHitReason: cache.CacheHitMiss, Findings: 1,
	})
	seedResearchDispatch(t, db, seedResearchRow{
		ID: "d-other", Query: "Other Project", ProjectID: "proj-B", CreatedAt: 110,
		CacheHitReason: cache.CacheHitSemantic, Findings: 3,
	})

	adapter, err := NewResearchAdapter(ResearchAdapterDeps{DB: db})
	if err != nil {
		t.Fatalf("NewResearchAdapter: %v", err)
	}
	rows, err := adapter.History(ctx, handlers.ResearchHistoryFilterP9{
		Filter:    "cache_hit",
		ProjectID: "proj-A",
		Limit:     1,
	})
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("History rows = %d want 1", len(rows))
	}
	got := rows[0]
	if got.Query != cache.CanonicalizeQuery("Cache Hit") ||
		got.Source != "cache_hit_exact" ||
		got.FindingsCount != 2 ||
		got.DispatchedAt != 100 {
		t.Fatalf("History row = %+v", got)
	}
}

func TestResearchAdapterCacheStatsAndList(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := openResearchAdapterDB(t)
	seedResearchDispatch(t, db, seedResearchRow{
		ID: "d1", Query: "Docs", ProjectID: "proj-A", CreatedAt: 100,
		CacheHitReason: cache.CacheHitMiss, Findings: 1,
		SourceURL: "https://docs.example.com/a", ContentHash: "hash-a",
		Body: []byte("alpha"), Freshness: cache.FreshnessFresh,
	})
	seedResearchDispatch(t, db, seedResearchRow{
		ID: "d2", Query: "RFC", ProjectID: "proj-A", CreatedAt: 200,
		CacheHitReason: cache.CacheHitMiss, Findings: 1,
		SourceURL: "https://rfc.example.com/b", ContentHash: "hash-b",
		Body: []byte("beta-gamma"), Freshness: cache.FreshnessExpired,
	})
	seedResearchDispatch(t, db, seedResearchRow{
		ID: "d3", Query: "Other", ProjectID: "proj-B", CreatedAt: 300,
		CacheHitReason: cache.CacheHitMiss, Findings: 1,
		SourceURL: "https://docs.example.com/c", ContentHash: "hash-c",
		Body: []byte("ignored"), Freshness: cache.FreshnessFresh,
	})

	adapter, err := NewResearchAdapter(ResearchAdapterDeps{
		DB:  db,
		Now: func() int64 { return 250 },
	})
	if err != nil {
		t.Fatalf("NewResearchAdapter: %v", err)
	}
	stats, err := adapter.CacheStats(ctx, "proj-A")
	if err != nil {
		t.Fatalf("CacheStats: %v", err)
	}
	if stats.TotalEntries != 2 || stats.TotalBytes != int64(len("alpha")+len("beta-gamma")) ||
		stats.OldestUnix != 100 || stats.NewestUnix != 200 || stats.ExpiredCount != 1 ||
		stats.StuckQueriesCount != 0 {
		t.Fatalf("CacheStats = %+v", stats)
	}

	list, err := adapter.CacheList(ctx, "proj-A", "https://docs.example.com")
	if err != nil {
		t.Fatalf("CacheList: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("CacheList rows = %d want 1", len(list))
	}
	if list[0].Hash != "d1-f0" || list[0].SourceURL != "https://docs.example.com/a" ||
		list[0].ContentHash != "hash-a" || list[0].BytesSize != int64(len("alpha")) ||
		list[0].CreatedAt != 100 {
		t.Fatalf("CacheList row = %+v", list[0])
	}
}

func TestResearchAdapterCacheInvalidateForcesLookupMiss(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := openResearchAdapterDB(t)
	seedResearchDispatch(t, db, seedResearchRow{
		ID: "d1", Query: "Poisoned", ProjectID: "proj-A", CreatedAt: 100,
		CacheHitReason: cache.CacheHitMiss, Findings: 1,
	})
	if _, err := cache.LookupExact(ctx, db, "poisoned", "proj-A", "sess"); err != nil {
		t.Fatalf("LookupExact before invalidate: %v", err)
	}
	adapter, err := NewResearchAdapter(ResearchAdapterDeps{
		DB:  db,
		Now: func() int64 { return 200 },
	})
	if err != nil {
		t.Fatalf("NewResearchAdapter: %v", err)
	}
	n, err := adapter.CacheInvalidate(ctx, "poisoned")
	if err != nil {
		t.Fatalf("CacheInvalidate: %v", err)
	}
	if n != 1 {
		t.Fatalf("CacheInvalidate = %d want 1", n)
	}
	if _, err := cache.LookupExact(ctx, db, "poisoned", "proj-A", "sess"); !errors.Is(err, cache.ErrCacheMiss) {
		t.Fatalf("LookupExact after invalidate err = %v want ErrCacheMiss", err)
	}
}

func openResearchAdapterDB(t *testing.T) *cache.DB {
	t.Helper()
	db, err := cache.Open(context.Background(), filepath.Join(t.TempDir(), "research_cache.db"))
	if err != nil {
		t.Fatalf("cache.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.SQL.Close() })
	return db
}

type seedResearchRow struct {
	ID             string
	Query          string
	ProjectID      string
	CreatedAt      int64
	CacheHitReason cache.CacheHitReason
	Findings       int
	SourceURL      string
	ContentHash    string
	Body           []byte
	Freshness      cache.FreshnessStatus
}

func seedResearchDispatch(t *testing.T, db *cache.DB, row seedResearchRow) {
	t.Helper()
	if row.Findings == 0 {
		row.Findings = 1
	}
	if row.SourceURL == "" {
		row.SourceURL = "https://example.com/" + row.ID
	}
	if row.ContentHash == "" {
		row.ContentHash = "hash-" + row.ID
	}
	if row.Body == nil {
		row.Body = []byte("body-" + row.ID)
	}
	if row.Freshness == "" {
		row.Freshness = cache.FreshnessFresh
	}
	_, err := db.SQL.Exec(
		`INSERT INTO research_dispatches
		 (id, query, query_text_hash, status, project_id, session_id,
		  dispatched_at, cache_hit_reason, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		row.ID, cache.CanonicalizeQuery(row.Query), cache.ComputeQueryHash(row.Query),
		string(cache.DispatchStatusDone), row.ProjectID, "sess",
		row.CreatedAt, string(row.CacheHitReason), row.CreatedAt, row.CreatedAt,
	)
	if err != nil {
		t.Fatalf("insert dispatch: %v", err)
	}
	for i := 0; i < row.Findings; i++ {
		_, err = db.SQL.Exec(
			`INSERT INTO research_findings
			 (id, dispatch_id, url, title, snippet, freshness_status, retrieved_at,
			  content_hash, body_inline_blob, body_path, source_url_canonical)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			row.ID+"-f"+string(rune('0'+i)), row.ID, row.SourceURL, "title", "snippet",
			string(row.Freshness), row.CreatedAt, row.ContentHash, row.Body, "", row.SourceURL,
		)
		if err != nil {
			t.Fatalf("insert finding: %v", err)
		}
	}
}
