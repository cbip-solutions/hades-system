// go:build cgo
//go:build cgo
// +build cgo

package cache

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	"testing"
	"time"
)

func embed384Mock(seed int) []float32 {
	v := make([]float32, EmbeddingDim)
	v[seed%EmbeddingDim] = 1.0
	return v
}

func embed384Cos(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0.0
	}
	var dot, magA, magB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		magA += float64(a[i]) * float64(a[i])
		magB += float64(b[i]) * float64(b[i])
	}
	if magA == 0.0 || magB == 0.0 {
		return 0.0
	}
	return dot / (math.Sqrt(magA) * math.Sqrt(magB))
}

func seedDispatchWithEmbedding(t *testing.T, db *DB, query, projectID string, embedding []float32, n int) string {
	t.Helper()

	now := time.Now().UTC().Unix()
	hash := ComputeQueryHash(query)

	dispatchID := "sem-dispatch-" + hash[:8] + "-" + projectID

	result, err := db.SQL.ExecContext(context.Background(),
		`INSERT INTO research_dispatches
		 (id, query, query_text_hash, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		dispatchID, query, hash,
		string(DispatchStatusDone),
		now-10, now,
	)
	if err != nil {
		t.Fatalf("seedDispatchWithEmbedding: insert dispatch: %v", err)
	}
	rowid, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("seedDispatchWithEmbedding: last_insert_rowid: %v", err)
	}

	buf := make([]byte, 4*len(embedding))
	for i, f := range embedding {
		binary.LittleEndian.PutUint32(buf[4*i:], math.Float32bits(f))
	}

	_, err = db.SQL.ExecContext(context.Background(),
		`INSERT INTO research_query_vec(rowid, embedding) VALUES (?, ?)`,
		rowid, buf,
	)
	if err != nil {
		t.Fatalf("seedDispatchWithEmbedding: insert research_query_vec rowid=%d: %v", rowid, err)
	}

	for i := 0; i < n; i++ {
		fid := dispatchID + "-f-" + itoa(i)
		_, err := db.SQL.ExecContext(context.Background(),
			`INSERT INTO research_findings
			 (id, dispatch_id, url, title, snippet, freshness_status, retrieved_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			fid, dispatchID,
			"https://example.com/sem/"+itoa(i),
			"SemanticTitle "+itoa(i),
			"SemanticSnippet "+itoa(i),
			string(FreshnessFresh),
			now,
		)
		if err != nil {
			t.Fatalf("seedDispatchWithEmbedding: insert finding %d: %v", i, err)
		}
	}

	return dispatchID
}

func TestSemanticLookupHitAboveThreshold(t *testing.T) {
	t.Parallel()
	db := openTestCacheDB(t)

	emb := embed384Mock(0)
	seedDispatchWithEmbedding(t, db, "how does TLS 1.3 handshake work", "proj-sem-A", emb, 2)

	res, err := LookupSemantic(context.Background(), db, emb, "proj-sem-A", "session-sem-1")
	if err != nil {
		t.Fatalf("LookupSemantic: %v", err)
	}
	if !res.Hit {
		t.Errorf("Hit = false, want true (cosine 1.0 >> 0.92 threshold)")
	}
	if res.HitReason != CacheHitSemantic {
		t.Errorf("HitReason = %v, want %v", res.HitReason, CacheHitSemantic)
	}
	if res.Dispatch == nil {
		t.Fatal("Dispatch must not be nil on semantic hit")
	}
	if len(res.Findings) != 2 {
		t.Errorf("Findings count = %d, want 2", len(res.Findings))
	}
	if res.FreshnessStatus != FreshnessUnknown {
		t.Errorf("FreshnessStatus = %v, want FreshnessUnknown (revalidator decides)", res.FreshnessStatus)
	}
}

func TestSemanticLookupMissBelowThreshold(t *testing.T) {
	t.Parallel()
	db := openTestCacheDB(t)

	seedEmb := embed384Mock(0)
	queryEmb := embed384Mock(1)
	seedDispatchWithEmbedding(t, db, "how does BGP route selection work", "proj-sem-B", seedEmb, 1)

	cos := embed384Cos(seedEmb, queryEmb)
	if cos > 0.01 {
		t.Fatalf("test precondition: expected cos ≈ 0.0, got %v", cos)
	}

	_, err := LookupSemantic(context.Background(), db, queryEmb, "proj-sem-B", "s")
	if !errors.Is(err, ErrCacheMiss) {
		t.Errorf("err = %v, want ErrCacheMiss (cosine 0.0 below 0.92 threshold)", err)
	}
}

func TestSemanticLookupThresholdEdgeCase(t *testing.T) {
	t.Parallel()
	db := openTestCacheDB(t)

	queryEmb := embed384Mock(0)

	aboveEmb := embed384Mock(0)

	belowEmb := make([]float32, EmbeddingDim)
	belowEmb[0] = 0.90
	belowEmb[1] = float32(math.Sqrt(1.0 - 0.90*0.90))

	cosAbove := embed384Cos(aboveEmb, queryEmb)
	if cosAbove < 0.999 {
		t.Fatalf("precondition: aboveEmb cos = %v, want ≈ 1.0", cosAbove)
	}
	cosBelow := embed384Cos(belowEmb, queryEmb)
	if math.Abs(cosBelow-0.90) > 0.001 {
		t.Fatalf("precondition: belowEmb cos = %v, want ≈ 0.90", cosBelow)
	}

	seedDispatchWithEmbedding(t, db, "threshold test above", "proj-thresh-A", aboveEmb, 1)

	seedDispatchWithEmbedding(t, db, "threshold test below", "proj-thresh-B", belowEmb, 1)

	res, err := LookupSemantic(context.Background(), db, queryEmb, "proj-thresh-A", "s")
	if err != nil {
		t.Fatalf("LookupSemantic (above threshold): %v", err)
	}
	if !res.Hit {
		t.Errorf("Hit = false, want true (cos 1.0 ≥ 0.92)")
	}
	if res.HitReason != CacheHitSemantic {
		t.Errorf("HitReason = %v, want CacheHitSemantic", res.HitReason)
	}
}

func TestSemanticLookupRequiresProjectID(t *testing.T) {
	t.Parallel()
	db := openTestCacheDB(t)

	emb := embed384Mock(0)
	_, err := LookupSemantic(context.Background(), db, emb, "", "session-test")
	if !errors.Is(err, ErrProjectIDRequired) {
		t.Errorf("empty project_id: err = %v, want ErrProjectIDRequired (inv-zen-148)", err)
	}
}

func TestSemanticLookupRequires384Dim(t *testing.T) {
	t.Parallel()
	db := openTestCacheDB(t)

	cases := []struct {
		name string
		dim  int
	}{
		{"empty", 0},
		{"one", 1},
		{"383", 383},
		{"385", 385},
		{"768", 768},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			emb := make([]float32, tc.dim)
			_, err := LookupSemantic(context.Background(), db, emb, "proj-dim", "s")
			if !errors.Is(err, ErrEmbeddingDimension) {
				t.Errorf("dim=%d: err = %v, want ErrEmbeddingDimension", tc.dim, err)
			}
		})
	}
}

func TestSemanticLookupOrderByHighestSimilarity(t *testing.T) {
	t.Parallel()
	db := openTestCacheDB(t)

	queryEmb := embed384Mock(0)

	embA := embed384Mock(0)

	embB := make([]float32, EmbeddingDim)
	embB[0] = 0.97
	embB[1] = float32(math.Sqrt(1.0 - 0.97*0.97))

	embC := make([]float32, EmbeddingDim)
	embC[0] = 0.95
	embC[1] = float32(math.Sqrt(1.0 - 0.95*0.95))

	cosA := embed384Cos(embA, queryEmb)
	cosB := embed384Cos(embB, queryEmb)
	cosC := embed384Cos(embC, queryEmb)
	if cosA < 0.999 {
		t.Fatalf("precondition: cosA = %v, want ≈ 1.0", cosA)
	}
	if math.Abs(cosB-0.97) > 0.002 {
		t.Fatalf("precondition: cosB = %v, want ≈ 0.97", cosB)
	}
	if math.Abs(cosC-0.95) > 0.002 {
		t.Fatalf("precondition: cosC = %v, want ≈ 0.95", cosC)
	}

	if cosA < SemanticThresholdCosine || cosB < SemanticThresholdCosine || cosC < SemanticThresholdCosine {
		t.Fatalf("precondition: all cosines must be ≥ %v; got A=%v B=%v C=%v",
			SemanticThresholdCosine, cosA, cosB, cosC)
	}

	idA := seedDispatchWithEmbedding(t, db, "order test alpha", "proj-ord-A", embA, 1)
	_ = seedDispatchWithEmbedding(t, db, "order test bravo", "proj-ord-B", embB, 1)
	_ = seedDispatchWithEmbedding(t, db, "order test charlie", "proj-ord-C", embC, 1)

	res, err := LookupSemantic(context.Background(), db, queryEmb, "proj-ord-A", "s")
	if err != nil {
		t.Fatalf("LookupSemantic: %v", err)
	}
	if !res.Hit {
		t.Fatalf("Hit = false, want true")
	}
	if res.Dispatch == nil {
		t.Fatal("Dispatch must not be nil")
	}

	if res.Dispatch.ID != idA {
		t.Errorf("expected dispatch ID %q (most similar, cos=1.0), got %q", idA, res.Dispatch.ID)
	}
}
