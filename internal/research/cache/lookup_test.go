// go:build cgo
//go:build cgo
// +build cgo

// Package cache — lookup_test.go
//
// Tests for the Lookup façade.
//
// Lookup(ctx, db, query, embedding, projectID, sessionID) is an
// exact-then-semantic fall-through helper used by callers that do NOT need
// the full MCP dispatch path (LookupOrDispatch). It combines LookupExact
// and LookupSemantic without any MCP fallback.
//
// Test scenarios:
//
// TestLookupExactPreferredOverSemantic — exact hit is returned even when
// a semantic embedding is supplied.
// TestLookupFallThroughOnExactMiss — exact miss falls through to semantic.
// TestLookupNilEmbeddingSkipsSemantic — nil embedding returns ErrCacheMiss
// instead of attempting semantic.
// TestLookupBothMiss — no exact or semantic match → ErrCacheMiss.
// TestLookupRequiresProjectID — ErrProjectIDRequired when projectID empty.
package cache

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	"testing"
	"time"
)

func makeTestEmbedding() []float32 {
	v := make([]float32, EmbeddingDim)
	norm := float32(math.Sqrt(float64(EmbeddingDim)))
	for i := range v {
		v[i] = 1.0 / norm
	}
	return v
}

func seedQueryVec(t *testing.T, db *DB, rowid int64, emb []float32) {
	t.Helper()
	buf := make([]byte, 4*len(emb))
	for i, f := range emb {
		binary.LittleEndian.PutUint32(buf[4*i:4*(i+1)], math.Float32bits(f))
	}
	_, err := db.SQL.ExecContext(context.Background(),
		`INSERT INTO research_query_vec(rowid, embedding) VALUES (?, ?)`,
		rowid, buf,
	)
	if err != nil {
		t.Fatalf("seedQueryVec rowid=%d: %v", rowid, err)
	}
}

func seedDispatchWithRowID(t *testing.T, db *DB, query, projectID string, n int) (textID string, rowid int64) {
	t.Helper()
	hash := ComputeQueryHash(query)
	textID = "dispatch-lookup-" + hash[:8] + "-" + projectID

	res, err := db.SQL.ExecContext(context.Background(),
		`INSERT INTO research_dispatches
		 (id, query, query_text_hash, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		textID, query, hash,
		string(DispatchStatusDone),
		time.Now().UTC().Unix()-10,
		time.Now().UTC().Unix(),
	)
	if err != nil {
		t.Fatalf("seedDispatchWithRowID insert dispatch: %v", err)
	}
	rowid, err = res.LastInsertId()
	if err != nil {
		t.Fatalf("seedDispatchWithRowID LastInsertId: %v", err)
	}

	for i := 0; i < n; i++ {
		fid := textID + "-f-" + itoa(i)
		_, err := db.SQL.ExecContext(context.Background(),
			`INSERT INTO research_findings
			 (id, dispatch_id, url, title, snippet, freshness_status, retrieved_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			fid, textID,
			"https://example.com/lookup/"+itoa(i),
			"Lookup Title "+itoa(i),
			"Lookup Snippet "+itoa(i),
			string(FreshnessFresh),
			time.Now().UTC().Unix(),
		)
		if err != nil {
			t.Fatalf("seedDispatchWithRowID insert finding %d: %v", i, err)
		}
	}

	return textID, rowid
}

func TestLookupExactPreferredOverSemantic(t *testing.T) {
	t.Parallel()
	db := openTestCacheDB(t)

	const query = "exact preferred test"
	textID, rowid := seedDispatchWithRowID(t, db, query, "proj-lookup-A", 2)
	emb := makeTestEmbedding()
	seedQueryVec(t, db, rowid, emb)

	res, err := Lookup(context.Background(), db, query, emb, "proj-lookup-A", "s")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if res.HitReason != CacheHitExact {
		t.Errorf("HitReason = %v, want %v (exact should be preferred)", res.HitReason, CacheHitExact)
	}
	if res.Dispatch == nil || res.Dispatch.ID != textID {
		t.Errorf("unexpected dispatch: %v", res.Dispatch)
	}
	if len(res.Findings) != 2 {
		t.Errorf("Findings count = %d, want 2", len(res.Findings))
	}
}

func TestLookupFallThroughOnExactMiss(t *testing.T) {
	t.Parallel()
	db := openTestCacheDB(t)

	const storedQuery = "semantic fallthrough stored query"
	_, rowid := seedDispatchWithRowID(t, db, storedQuery, "proj-lookup-B", 1)

	emb := makeTestEmbedding()
	seedQueryVec(t, db, rowid, emb)

	const lookupQuery = "semantic fallthrough lookup query — different text"
	res, err := Lookup(context.Background(), db, lookupQuery, emb, "proj-lookup-B", "s")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if res.HitReason != CacheHitSemantic {
		t.Errorf("HitReason = %v, want %v (semantic fallthrough)", res.HitReason, CacheHitSemantic)
	}
	if res.Dispatch == nil {
		t.Fatal("Dispatch must not be nil on semantic hit")
	}
	if len(res.Findings) < 1 {
		t.Errorf("Findings count = %d, want ≥1 for semantic hit", len(res.Findings))
	}
}

// TestLookupNilEmbeddingSkipsSemantic verifies that passing a nil embedding
// causes Lookup to skip the semantic step and return ErrCacheMiss when there
// is no exact match. This is the caller's signal that they do not have an
// embedding available.
func TestLookupNilEmbeddingSkipsSemantic(t *testing.T) {
	t.Parallel()
	db := openTestCacheDB(t)

	const storedQuery = "nil embedding stored query"
	_, rowid := seedDispatchWithRowID(t, db, storedQuery, "proj-lookup-C", 1)
	emb := makeTestEmbedding()
	seedQueryVec(t, db, rowid, emb)

	const lookupQuery = "nil embedding lookup query — different text"
	_, err := Lookup(context.Background(), db, lookupQuery, nil, "proj-lookup-C", "s")
	if !errors.Is(err, ErrCacheMiss) {
		t.Errorf("nil embedding: err = %v, want ErrCacheMiss", err)
	}
}

func TestLookupBothMiss(t *testing.T) {
	t.Parallel()
	db := openTestCacheDB(t)

	emb := makeTestEmbedding()
	_, err := Lookup(context.Background(), db, "miss both paths", emb, "proj-lookup-D", "s")
	if !errors.Is(err, ErrCacheMiss) {
		t.Errorf("both miss: err = %v, want ErrCacheMiss", err)
	}
}

func TestLookupRequiresProjectID(t *testing.T) {
	t.Parallel()
	db := openTestCacheDB(t)
	emb := makeTestEmbedding()

	_, err := Lookup(context.Background(), db, "any query", emb, "", "s")
	if !errors.Is(err, ErrProjectIDRequired) {
		t.Errorf("empty projectID: err = %v, want ErrProjectIDRequired", err)
	}
}
