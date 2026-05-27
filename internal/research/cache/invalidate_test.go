// go:build cgo
//go:build cgo
// +build cgo

package cache

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestInvalidateByQueryForcesExactMiss(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "research_cache.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.SQL.Close() })

	seedInvalidationDispatch(t, db, "d1", "force stale query", "proj-A", 100, FreshnessFresh)
	if _, err := LookupExact(ctx, db, "force stale query", "proj-A", "sess"); err != nil {
		t.Fatalf("LookupExact before invalidate: %v", err)
	}

	n, err := InvalidateByQuery(ctx, db, "force stale query", "operator requested refetch", 200)
	if err != nil {
		t.Fatalf("InvalidateByQuery: %v", err)
	}
	if n != 1 {
		t.Fatalf("invalidated = %d want 1", n)
	}
	if _, err := LookupExact(ctx, db, "force stale query", "proj-A", "sess"); !errors.Is(err, ErrCacheMiss) {
		t.Fatalf("LookupExact after invalidate err = %v want ErrCacheMiss", err)
	}
}

func TestInvalidateByQueryIsIdempotent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "research_cache.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.SQL.Close() })

	seedInvalidationDispatch(t, db, "d1", "same query", "proj-A", 100, FreshnessFresh)
	first, err := InvalidateByQuery(ctx, db, "same query", "first", 200)
	if err != nil {
		t.Fatalf("InvalidateByQuery first: %v", err)
	}
	second, err := InvalidateByQuery(ctx, db, "same query", "second", 300)
	if err != nil {
		t.Fatalf("InvalidateByQuery second: %v", err)
	}
	if first != 1 || second != 0 {
		t.Fatalf("invalidated counts = (%d,%d) want (1,0)", first, second)
	}
}

func TestSchemaVersionIncludesInvalidationColumns(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "research_cache.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.SQL.Close() })

	ver, err := SchemaVersion(ctx, db.SQL)
	if err != nil {
		t.Fatalf("SchemaVersion: %v", err)
	}
	if ver != cacheSchemaVersionV6 {
		t.Fatalf("SchemaVersion = %d want %d", ver, cacheSchemaVersionV6)
	}
	for _, col := range []string{"invalidated_at", "invalidated_reason"} {
		ok, err := tableHasColumn(ctx, db.SQL, "research_dispatches", col)
		if err != nil {
			t.Fatalf("tableHasColumn(%s): %v", col, err)
		}
		if !ok {
			t.Fatalf("research_dispatches missing %s", col)
		}
	}
}

func seedInvalidationDispatch(t *testing.T, db *DB, id, query, projectID string, ts int64, freshness FreshnessStatus) {
	t.Helper()
	_, err := db.SQL.Exec(
		`INSERT INTO research_dispatches
		 (id, query, query_text_hash, status, project_id, session_id,
		  cache_hit_reason, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, CanonicalizeQuery(query), ComputeQueryHash(query), string(DispatchStatusDone),
		projectID, "sess", string(CacheHitMiss), ts, ts,
	)
	if err != nil {
		t.Fatalf("insert dispatch: %v", err)
	}
	_, err = db.SQL.Exec(
		`INSERT INTO research_findings
		 (id, dispatch_id, url, title, snippet, freshness_status, retrieved_at,
		  content_hash, body_inline_blob, body_path, source_url_canonical)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id+"-f1", id, "https://example.com/doc", "title", "snippet",
		string(freshness), ts, "hash", []byte("body"), "", "https://example.com/doc",
	)
	if err != nil {
		t.Fatalf("insert finding: %v", err)
	}
}
