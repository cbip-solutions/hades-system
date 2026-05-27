// go:build cgo
//go:build cgo
// +build cgo

package cache

import (
	"context"
	"errors"
	"testing"
	"time"
)

func seedDispatch(t *testing.T, db *DB, query, projectID string, n int) string {
	t.Helper()
	hash := ComputeQueryHash(query)

	id := "dispatch-" + hash[:8] + "-" + projectID

	_, err := db.SQL.ExecContext(context.Background(),
		`INSERT INTO research_dispatches
		 (id, query, query_text_hash, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		id, query, hash,
		string(DispatchStatusDone),
		time.Now().UTC().Unix()-10,
		time.Now().UTC().Unix(),
	)
	if err != nil {
		t.Fatalf("seedDispatch insert dispatch: %v", err)
	}

	for i := 0; i < n; i++ {
		fid := id + "-finding-" + itoa(i)
		_, err := db.SQL.ExecContext(context.Background(),
			`INSERT INTO research_findings
			 (id, dispatch_id, url, title, snippet, freshness_status, retrieved_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			fid, id,
			"https://example.com/"+itoa(i),
			"Title "+itoa(i),
			"Snippet "+itoa(i),
			string(FreshnessFresh),
			time.Now().UTC().Unix(),
		)
		if err != nil {
			t.Fatalf("seedDispatch insert finding %d: %v", i, err)
		}
	}

	return id
}

func TestExactLookupHit(t *testing.T) {
	t.Parallel()
	db := openTestCacheDB(t)
	seedDispatch(t, db, "what is RFC 9162", "proj-A", 2)

	res, err := LookupExact(context.Background(), db, "what is RFC 9162", "proj-A", "session-test")
	if err != nil {
		t.Fatalf("LookupExact: %v", err)
	}
	if res.HitReason != CacheHitExact {
		t.Errorf("HitReason = %v, want %v", res.HitReason, CacheHitExact)
	}
	if len(res.Findings) != 2 {
		t.Errorf("findings count = %d, want 2", len(res.Findings))
	}
	if res.FreshnessStatus != FreshnessUnknown {
		t.Errorf("FreshnessStatus = %v, want %v (revalidator decides)", res.FreshnessStatus, FreshnessUnknown)
	}
	if res.Dispatch == nil {
		t.Fatal("Dispatch must not be nil on hit")
	}
}

func TestExactLookupMissReturnsErrCacheMiss(t *testing.T) {
	t.Parallel()
	db := openTestCacheDB(t)

	_, err := LookupExact(context.Background(), db, "never seen query", "proj-A", "session-test")
	if !errors.Is(err, ErrCacheMiss) {
		t.Errorf("err = %v, want ErrCacheMiss", err)
	}
}

func TestExactLookupCanonicalEquivalence(t *testing.T) {
	t.Parallel()
	db := openTestCacheDB(t)
	seedDispatch(t, db, "RFC 9162", "proj-A", 1)

	variants := []string{"RFC 9162", "rfc 9162", "  RFC  9162  "}
	for _, query := range variants {
		query := query
		res, err := LookupExact(context.Background(), db, query, "proj-A", "s")
		if err != nil {
			t.Errorf("variant %q: unexpected error: %v", query, err)
			continue
		}
		if res.HitReason != CacheHitExact {
			t.Errorf("variant %q: HitReason = %v, want %v", query, res.HitReason, CacheHitExact)
		}
	}
}

func TestExactLookupRequiresProjectID(t *testing.T) {
	t.Parallel()
	db := openTestCacheDB(t)

	_, err := LookupExact(context.Background(), db, "any query", "", "session-test")
	if !errors.Is(err, ErrProjectIDRequired) {
		t.Errorf("empty project_id: err = %v, want ErrProjectIDRequired (inv-zen-148)", err)
	}
}

func TestExactLookupZeroFindings(t *testing.T) {
	t.Parallel()
	db := openTestCacheDB(t)
	seedDispatch(t, db, "empty result query", "proj-B", 0)

	res, err := LookupExact(context.Background(), db, "empty result query", "proj-B", "s")
	if err != nil {
		t.Fatalf("LookupExact: %v", err)
	}
	if res.HitReason != CacheHitExact {
		t.Errorf("HitReason = %v, want %v", res.HitReason, CacheHitExact)
	}
	if res.Findings != nil {
		t.Errorf("expected nil findings for zero-result dispatch, got %d findings", len(res.Findings))
	}
}

func TestExactLookupCancelledContextDispatch(t *testing.T) {
	t.Parallel()
	db := openTestCacheDB(t)

	seedDispatch(t, db, "ctx cancel query", "proj-C", 1)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := LookupExact(ctx, db, "ctx cancel query", "proj-C", "s")
	if err == nil {
		t.Fatal("expected error with cancelled context, got nil")
	}

	if errors.Is(err, ErrCacheMiss) {
		t.Errorf("cancelled context returned ErrCacheMiss — should be context error, got: %v", err)
	}
}

func TestExactLookupFindingWithNullBodyFields(t *testing.T) {
	t.Parallel()
	db := openTestCacheDB(t)

	hash := ComputeQueryHash("null body test")
	_, err := db.SQL.ExecContext(context.Background(),
		`INSERT INTO research_dispatches
		 (id, query, query_text_hash, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		"dispatch-nullbody", "null body test", hash,
		string(DispatchStatusDone),
		time.Now().UTC().Unix()-5,
		time.Now().UTC().Unix(),
	)
	if err != nil {
		t.Fatalf("seed dispatch: %v", err)
	}

	_, err = db.SQL.ExecContext(context.Background(),
		`INSERT INTO research_findings
		 (id, dispatch_id, url, title, snippet, freshness_status, retrieved_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"finding-nullbody",
		"dispatch-nullbody",
		"https://example.com/nullbody",
		"Null Body Title",
		"Null body snippet",
		string(FreshnessFresh),
		time.Now().UTC().Unix(),
	)
	if err != nil {
		t.Fatalf("seed finding: %v", err)
	}

	res, err := LookupExact(context.Background(), db, "null body test", "proj-D", "s")
	if err != nil {
		t.Fatalf("LookupExact: %v", err)
	}
	if len(res.Findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(res.Findings))
	}
	f := res.Findings[0]
	if f.ContentHash != "" {
		t.Errorf("ContentHash: expected empty for NULL, got %q", f.ContentHash)
	}
	if f.BodyPath != "" {
		t.Errorf("BodyPath: expected empty for NULL, got %q", f.BodyPath)
	}
	if f.BodyInlineBlob != nil {
		t.Errorf("BodyInlineBlob: expected nil for NULL, got %v", f.BodyInlineBlob)
	}
}

func TestExactLookupFindingWithBodyFields(t *testing.T) {
	t.Parallel()
	db := openTestCacheDB(t)

	hash := ComputeQueryHash("body fields test")
	_, err := db.SQL.ExecContext(context.Background(),
		`INSERT INTO research_dispatches
		 (id, query, query_text_hash, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		"dispatch-bodyfields", "body fields test", hash,
		string(DispatchStatusDone),
		time.Now().UTC().Unix()-5,
		time.Now().UTC().Unix(),
	)
	if err != nil {
		t.Fatalf("seed dispatch: %v", err)
	}

	const wantHash = "abc123def456abc123def456abc123def456abc123def456abc123def456abcd"
	const wantPath = "/cas/ab/c123def456"
	_, err = db.SQL.ExecContext(context.Background(),
		`INSERT INTO research_findings
		 (id, dispatch_id, url, title, snippet, freshness_status, retrieved_at,
		  content_hash, body_path)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"finding-bodyfields",
		"dispatch-bodyfields",
		"https://example.com/bodyfields",
		"Body Fields Title",
		"Body fields snippet",
		string(FreshnessFresh),
		time.Now().UTC().Unix(),
		wantHash,
		wantPath,
	)
	if err != nil {
		t.Fatalf("seed finding with body fields: %v", err)
	}

	res, err := LookupExact(context.Background(), db, "body fields test", "proj-E", "s")
	if err != nil {
		t.Fatalf("LookupExact: %v", err)
	}
	if len(res.Findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(res.Findings))
	}
	f := res.Findings[0]
	if f.ContentHash != wantHash {
		t.Errorf("ContentHash = %q, want %q", f.ContentHash, wantHash)
	}
	if f.BodyPath != wantPath {
		t.Errorf("BodyPath = %q, want %q", f.BodyPath, wantPath)
	}
}

func TestSelectFindingsByDispatchIDClosedDB(t *testing.T) {
	t.Parallel()
	db := openTestCacheDB(t)

	if err := db.SQL.Close(); err != nil {
		t.Fatalf("close DB: %v", err)
	}

	_, err := selectFindingsByDispatchID(context.Background(), db.SQL, "any-id")
	if err == nil {
		t.Fatal("expected error from selectFindingsByDispatchID on closed DB, got nil")
	}
}

func TestExactLookupReturnsMostRecent(t *testing.T) {
	t.Parallel()
	db := openTestCacheDB(t)

	hash := ComputeQueryHash("dup query")

	olderCreatedAt := time.Now().UTC().Unix() - 3600
	_, err := db.SQL.ExecContext(context.Background(),
		`INSERT INTO research_dispatches
		 (id, query, query_text_hash, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		"dispatch-old", "dup query", hash,
		string(DispatchStatusDone),
		olderCreatedAt, olderCreatedAt,
	)
	if err != nil {
		t.Fatalf("seed older dispatch: %v", err)
	}

	newerCreatedAt := time.Now().UTC().Unix()
	_, err = db.SQL.ExecContext(context.Background(),
		`INSERT INTO research_dispatches
		 (id, query, query_text_hash, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		"dispatch-new", "dup query", hash,
		string(DispatchStatusDone),
		newerCreatedAt, newerCreatedAt,
	)
	if err != nil {
		t.Fatalf("seed newer dispatch: %v", err)
	}

	res, err := LookupExact(context.Background(), db, "dup query", "proj-A", "s")
	if err != nil {
		t.Fatalf("LookupExact: %v", err)
	}
	if res.Dispatch == nil {
		t.Fatal("Dispatch must not be nil on hit")
	}

	if res.Dispatch.ID != "dispatch-new" {
		t.Errorf("expected most recent dispatch (id=dispatch-new), got id=%q", res.Dispatch.ID)
	}

	if res.Dispatch.CreatedAt != newerCreatedAt {
		t.Errorf("Dispatch.CreatedAt = %d, want %d (most recent)", res.Dispatch.CreatedAt, newerCreatedAt)
	}
}
