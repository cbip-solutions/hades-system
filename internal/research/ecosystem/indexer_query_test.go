//go:build cgo
// +build cgo

// Package ecosystem — indexer_query_test.go
//
// Tests for Indexer.BinaryTop200 + FTS5Top200 + HydrateChunks (Plan 14
// Phase C Task C-9 — Stage 2 amendment 2026-05-15).
//
// Coverage discipline (CLAUDE.md hard rule 5): security/correctness-
// critical files require ≥90% per-function coverage. indexer_query.go
// ships the query-side surface Phase D dispatcher.go consumes; the
// Hamming + FTS5 + JOIN read paths must be fully exercised including
// every defense-in-depth branch (nil DB, bad query-vector length, empty
// queryText short-circuit, empty chunkIDs short-circuit, ctx
// cancellation, missing chunk IDs not erroring).
//
// Drift reconciliation (Stage 0 reality-check 2026-05-18):
//   1. aggregator constructor uses New(Options), NOT NewAggregator() +
//      ConfigureDatabase(). The DB() accessor (added by THIS task)
//      returns Options.DB.
//   2. migration helper is ApplyMigrations(db), NOT
//      applyEcosystemMigrations(t, db).
//   3. vec_bit(?) wire-format is MANDATORY on the read side too (C-3
//      inheritance) — sqlite-vec BIT[256] rejects raw `?` bindings.
//   4. existing Stage1Binary at indexer.go:732 is untouched; BinaryTop200
//      is the NEW adapter-named method with the topK fixed at 200 +
//      Candidate (vs ChunkCandidate) return type per master §3.13.
//
// Build tag `cgo`: this file requires sqlite3 + sqlite-vec virtual table
// support (registered via ApplyMigrations).

package ecosystem

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func setupQueryTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dir := t.TempDir()
	path := dir + "/indexer-query-test.db"
	db, err := sql.Open("sqlite3", path+"?_foreign_keys=on&_journal_mode=WAL")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := ApplyMigrations(db); err != nil {
		t.Fatalf("ApplyMigrations: %v", err)
	}
	return db
}

func newQueryTestIndexer(t *testing.T, db *sql.DB) *Indexer {
	t.Helper()
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: &fakeAuditEmitter{}})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	return idx
}

func seedTestChunks(t *testing.T, idx *Indexer, eco Ecosystem, count int) []int64 {
	t.Helper()
	pkg := PackageRef{
		Ecosystem:          eco,
		Name:               "testpkg",
		CanonicalNamespace: "crypto/sha256",
		UpstreamURL:        "https://example.test/testpkg",
	}
	ids := make([]int64, 0, count)
	for i := 0; i < count; i++ {
		bin := make([]byte, 32)

		bin[0] = byte(i & 0xFF)

		chunks := []Chunk{{
			VersionIntroduced: "1.0",
			StableIn:          []string{"1.0"},
			ContentText:       fmt.Sprintf("sha256 Sum256 chunk %d", i),
			Fingerprint:       indexerFingerprint(fmt.Sprintf("seed-%d", i)),
			SourceType:        SrcPackageDoc,
			SymbolPath:        fmt.Sprintf("crypto/sha256.Sum256_%d", i),
			Kind:              KindFunction,
			SourceURL:         fmt.Sprintf("https://example.test/%d", i),
			EmbeddingBin256d:  bin,

			EmbeddingFP32_1536d: indexerRandomFP32_1536(float32(i)),
		}}
		if err := idx.WriteChunks(context.Background(), pkg, "1.0", chunks, nil, nil); err != nil {
			t.Fatalf("seedTestChunks WriteChunks[%d]: %v", i, err)
		}
		ids = append(ids, chunks[0].ID)
	}
	return ids
}

func seedTestChunksMultiVersion(t *testing.T, idx *Indexer, eco Ecosystem) []int64 {
	t.Helper()
	pkg := PackageRef{
		Ecosystem:          eco,
		Name:               "multi",
		CanonicalNamespace: "multi",
		UpstreamURL:        "https://example.test/multi",
	}
	ids := make([]int64, 0, 6)
	for i, ver := range []string{"1.22.0", "1.22.0", "1.22.0", "1.23.0", "1.23.0", "1.23.0"} {
		bin := make([]byte, 32)
		bin[0] = byte(i + 1)

		chunks := []Chunk{{
			VersionIntroduced:   ver,
			StableIn:            []string{ver},
			ContentText:         fmt.Sprintf("multiversionchunk %d at %s tokenized", i, ver),
			Fingerprint:         indexerFingerprint(fmt.Sprintf("mv-%d-%s", i, ver)),
			SourceType:          SrcPackageDoc,
			SymbolPath:          fmt.Sprintf("multi.S%d", i),
			Kind:                KindFunction,
			SourceURL:           fmt.Sprintf("https://example.test/mv/%d", i),
			EmbeddingBin256d:    bin,
			EmbeddingFP32_1536d: indexerRandomFP32_1536(float32(i)),
		}}
		if err := idx.WriteChunks(context.Background(), pkg, ver, chunks, nil, nil); err != nil {
			t.Fatalf("seedTestChunksMultiVersion WriteChunks[%d]: %v", i, err)
		}
		ids = append(ids, chunks[0].ID)
	}
	return ids
}

func TestIndexerQuery_BinaryTop200(t *testing.T) {
	db := setupQueryTestDB(t)
	idx := newQueryTestIndexer(t, db)

	seedTestChunks(t, idx, EcoGo, 250)

	queryEmb := make([]byte, 32)
	cands, err := idx.BinaryTop200(context.Background(), queryEmb, "", EcoGo)
	if err != nil {
		t.Fatalf("BinaryTop200: %v", err)
	}
	if len(cands) != 200 {
		t.Errorf("BinaryTop200: got %d candidates; want 200", len(cands))
	}

	for i, c := range cands {
		if c.SimilarityScore < 0 || c.SimilarityScore > 1 {
			t.Errorf("cand[%d] SimilarityScore = %f; want in [0, 1]", i, c.SimilarityScore)
		}
		if c.Ecosystem != EcoGo {
			t.Errorf("cand[%d] Ecosystem = %q; want %q", i, c.Ecosystem, EcoGo)
		}
		if c.ChunkID == 0 {
			t.Errorf("cand[%d] ChunkID == 0; want non-zero", i)
		}
		if c.ContentText == "" {
			t.Errorf("cand[%d] ContentText empty", i)
		}
	}

	for i := 1; i < len(cands); i++ {
		if cands[i-1].SimilarityScore < cands[i].SimilarityScore {
			t.Errorf("not sorted: cand[%d].sim=%f < cand[%d].sim=%f",
				i-1, cands[i-1].SimilarityScore, i, cands[i].SimilarityScore)
		}
	}
}

// TestIndexerQuery_BinaryTop200_EcosystemScoped verifies the per-eco
// JOIN filter scopes the binary KNN to the requested ecosystem only —
// a Go chunk MUST NOT appear in a Python query result.
func TestIndexerQuery_BinaryTop200_EcosystemScoped(t *testing.T) {
	db := setupQueryTestDB(t)
	idx := newQueryTestIndexer(t, db)

	seedTestChunks(t, idx, EcoGo, 5)
	seedTestChunks(t, idx, EcoPython, 5)

	gotGo, err := idx.BinaryTop200(context.Background(), make([]byte, 32), "", EcoGo)
	if err != nil {
		t.Fatalf("BinaryTop200 go: %v", err)
	}
	if len(gotGo) != 5 {
		t.Errorf("len(go) = %d; want 5 (ecosystem-scoped)", len(gotGo))
	}
	for _, c := range gotGo {
		if c.Ecosystem != EcoGo {
			t.Errorf("got Ecosystem=%q in go-scoped query; want %q", c.Ecosystem, EcoGo)
		}
	}

	gotPy, err := idx.BinaryTop200(context.Background(), make([]byte, 32), "", EcoPython)
	if err != nil {
		t.Fatalf("BinaryTop200 python: %v", err)
	}
	if len(gotPy) != 5 {
		t.Errorf("len(python) = %d; want 5 (ecosystem-scoped)", len(gotPy))
	}
}

func TestIndexerQuery_FTS5Top200(t *testing.T) {
	db := setupQueryTestDB(t)
	idx := newQueryTestIndexer(t, db)

	seedTestChunks(t, idx, EcoGo, 50)

	cands, err := idx.FTS5Top200(context.Background(), "sha256", "", EcoGo)
	if err != nil {
		t.Fatalf("FTS5Top200: %v", err)
	}
	if len(cands) == 0 {
		t.Errorf("FTS5Top200: got 0 candidates; want > 0 for token 'sha256'")
	}
	if len(cands) > 200 {
		t.Errorf("FTS5Top200: got %d; want ≤ 200", len(cands))
	}
	for i, c := range cands {
		if c.ChunkID == 0 {
			t.Errorf("cand[%d] ChunkID == 0", i)
		}
		if c.Ecosystem != EcoGo {
			t.Errorf("cand[%d] Ecosystem = %q; want %q", i, c.Ecosystem, EcoGo)
		}
		if c.ContentText == "" {
			t.Errorf("cand[%d] ContentText empty", i)
		}
		if c.SimilarityScore <= 0 {
			t.Errorf("cand[%d] SimilarityScore = %f; want > 0", i, c.SimilarityScore)
		}
	}
}

func TestIndexerQuery_FTS5Top200_EcosystemScoped(t *testing.T) {
	db := setupQueryTestDB(t)
	idx := newQueryTestIndexer(t, db)

	seedTestChunks(t, idx, EcoGo, 5)
	seedTestChunks(t, idx, EcoPython, 5)

	gotGo, err := idx.FTS5Top200(context.Background(), "sha256", "", EcoGo)
	if err != nil {
		t.Fatalf("FTS5Top200 go: %v", err)
	}
	for _, c := range gotGo {
		if c.Ecosystem != EcoGo {
			t.Errorf("got Ecosystem=%q under go-scoped FTS query; want %q", c.Ecosystem, EcoGo)
		}
	}
	if len(gotGo) != 5 {
		t.Errorf("len(gotGo) = %d; want 5", len(gotGo))
	}
}

func TestIndexerQuery_HydrateChunks(t *testing.T) {
	db := setupQueryTestDB(t)
	idx := newQueryTestIndexer(t, db)

	ids := seedTestChunks(t, idx, EcoGo, 5)
	chunks, err := idx.HydrateChunks(context.Background(), ids, EcoGo)
	if err != nil {
		t.Fatalf("HydrateChunks: %v", err)
	}
	if len(chunks) != 5 {
		t.Errorf("HydrateChunks: got %d; want 5", len(chunks))
	}
	idSet := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		idSet[id] = struct{}{}
	}
	for _, qc := range chunks {
		if qc.PackageName == "" {
			t.Errorf("PackageName empty after JOIN (chunk %d)", qc.ChunkID)
		}
		if qc.PackageName != "testpkg" {
			t.Errorf("PackageName = %q; want %q", qc.PackageName, "testpkg")
		}
		if _, ok := idSet[qc.ChunkID]; !ok {
			t.Errorf("HydrateChunks returned ChunkID=%d not in input set", qc.ChunkID)
		}
		if qc.Kind != KindFunction {
			t.Errorf("Kind = %q; want %q", qc.Kind, KindFunction)
		}
		if qc.Version != "1.0" {
			t.Errorf("Version = %q; want \"1.0\"", qc.Version)
		}
		if qc.ContentText == "" {
			t.Errorf("ContentText empty for chunk %d", qc.ChunkID)
		}
		if qc.SourceURL == "" {
			t.Errorf("SourceURL empty for chunk %d", qc.ChunkID)
		}
	}
}

func TestIndexerQuery_VersionFilter(t *testing.T) {
	db := setupQueryTestDB(t)
	idx := newQueryTestIndexer(t, db)
	seedTestChunksMultiVersion(t, idx, EcoGo)

	cands, err := idx.BinaryTop200(context.Background(), make([]byte, 32), "1.22.0", EcoGo)
	if err != nil {
		t.Fatalf("BinaryTop200 v1.22.0: %v", err)
	}
	if len(cands) != 3 {
		t.Errorf("BinaryTop200 v1.22.0: got %d; want 3", len(cands))
	}
	for _, c := range cands {

		var stableJSON string
		if err := db.QueryRow(`SELECT stable_in_json FROM ecosystem_chunks WHERE id = ?`, c.ChunkID).Scan(&stableJSON); err != nil {
			t.Fatalf("scan stable_in_json: %v", err)
		}
		if !strings.Contains(stableJSON, "1.22.0") {
			t.Errorf("chunk %d stable_in_json=%q does not contain 1.22.0", c.ChunkID, stableJSON)
		}
	}

	cands, err = idx.FTS5Top200(context.Background(), "multiversionchunk", "1.22.0", EcoGo)
	if err != nil {
		t.Fatalf("FTS5Top200 v1.22.0: %v", err)
	}
	if len(cands) != 3 {
		t.Errorf("FTS5Top200 v1.22.0: got %d; want 3", len(cands))
	}
}

func TestIndexerQuery_BinaryTop200_BadLen(t *testing.T) {
	db := setupQueryTestDB(t)
	idx := newQueryTestIndexer(t, db)
	for _, length := range []int{0, 1, 31, 33, 64, 256} {
		_, err := idx.BinaryTop200(context.Background(), make([]byte, length), "", EcoGo)
		if err == nil {
			t.Errorf("len=%d: expected error, got nil", length)
			continue
		}
		if !strings.Contains(err.Error(), "want 32") {
			t.Errorf("len=%d: error %q does not mention `want 32`", length, err.Error())
		}
	}
}

func TestIndexerQuery_BinaryTop200_NoDB(t *testing.T) {

	db := setupQueryTestDB(t)
	idx := newQueryTestIndexer(t, db)
	idx.opts.DB = nil
	_, err := idx.BinaryTop200(context.Background(), make([]byte, 32), "", EcoGo)
	if err == nil {
		t.Fatal("expected error on nil DB; got nil")
	}
	if !strings.Contains(err.Error(), "no DB configured") {
		t.Errorf("err=%q does not mention `no DB configured`", err.Error())
	}
}

func TestIndexerQuery_FTS5Top200_NoDB(t *testing.T) {
	db := setupQueryTestDB(t)
	idx := newQueryTestIndexer(t, db)
	idx.opts.DB = nil
	_, err := idx.FTS5Top200(context.Background(), "sha256", "", EcoGo)
	if err == nil {
		t.Fatal("expected error on nil DB; got nil")
	}
	if !strings.Contains(err.Error(), "no DB configured") {
		t.Errorf("err=%q does not mention `no DB configured`", err.Error())
	}
}

func TestIndexerQuery_HydrateChunks_NoDB(t *testing.T) {
	db := setupQueryTestDB(t)
	idx := newQueryTestIndexer(t, db)
	idx.opts.DB = nil
	_, err := idx.HydrateChunks(context.Background(), []int64{1, 2, 3}, EcoGo)
	if err == nil {
		t.Fatal("expected error on nil DB; got nil")
	}
	if !strings.Contains(err.Error(), "no DB configured") {
		t.Errorf("err=%q does not mention `no DB configured`", err.Error())
	}
}

func TestIndexerQuery_BinaryTop200_CtxCanceled(t *testing.T) {
	db := setupQueryTestDB(t)
	idx := newQueryTestIndexer(t, db)
	seedTestChunks(t, idx, EcoGo, 1)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := idx.BinaryTop200(ctx, make([]byte, 32), "", EcoGo)
	if err == nil {
		t.Fatal("expected ctx error; got nil")
	}
}

func TestIndexerQuery_FTS5Top200_CtxCanceled(t *testing.T) {
	db := setupQueryTestDB(t)
	idx := newQueryTestIndexer(t, db)
	seedTestChunks(t, idx, EcoGo, 1)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := idx.FTS5Top200(ctx, "sha256", "", EcoGo)
	if err == nil {
		t.Fatal("expected ctx error; got nil")
	}
}

func TestIndexerQuery_HydrateChunks_CtxCanceled(t *testing.T) {
	db := setupQueryTestDB(t)
	idx := newQueryTestIndexer(t, db)
	ids := seedTestChunks(t, idx, EcoGo, 1)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := idx.HydrateChunks(ctx, ids, EcoGo)
	if err == nil {
		t.Fatal("expected ctx error; got nil")
	}
}

func TestIndexerQuery_FTS5Top200_EmptyQuery(t *testing.T) {
	db := setupQueryTestDB(t)
	idx := newQueryTestIndexer(t, db)
	cands, err := idx.FTS5Top200(context.Background(), "", "", EcoGo)
	if err != nil {
		t.Fatalf("FTS5Top200 empty query: %v", err)
	}
	if cands != nil {
		t.Errorf("FTS5Top200 empty query: got %v; want nil", cands)
	}
}

func TestIndexerQuery_HydrateChunks_EmptyIDs(t *testing.T) {
	db := setupQueryTestDB(t)
	idx := newQueryTestIndexer(t, db)
	chunks, err := idx.HydrateChunks(context.Background(), nil, EcoGo)
	if err != nil {
		t.Fatalf("HydrateChunks nil ids: %v", err)
	}
	if chunks != nil {
		t.Errorf("HydrateChunks nil ids: got %v; want nil", chunks)
	}

	chunks, err = idx.HydrateChunks(context.Background(), []int64{}, EcoGo)
	if err != nil {
		t.Fatalf("HydrateChunks []int64{}: %v", err)
	}
	if chunks != nil {
		t.Errorf("HydrateChunks empty slice: got %v; want nil", chunks)
	}
}

// TestIndexerQuery_HydrateChunks_NotFound verifies that requesting
// chunk IDs that don't exist in the DB returns an empty slice with
// nil error (not an error). The dispatcher MUST tolerate a partial
// hit: if 50 IDs are passed and only 47 exist (e.g., a chunk was
// deleted between the rerank step and the hydration step), the
// HydrateChunks return is a 47-row slice.
func TestIndexerQuery_HydrateChunks_NotFound(t *testing.T) {
	db := setupQueryTestDB(t)
	idx := newQueryTestIndexer(t, db)
	ids := seedTestChunks(t, idx, EcoGo, 3)

	requested := append(ids, 9999, 10000, 10001)
	chunks, err := idx.HydrateChunks(context.Background(), requested, EcoGo)
	if err != nil {
		t.Fatalf("HydrateChunks: %v", err)
	}
	if len(chunks) != 3 {
		t.Errorf("HydrateChunks partial-miss: got %d; want 3", len(chunks))
	}
}

func TestIndexerQuery_BinaryTop200_QueryErrorOnClosedDB(t *testing.T) {
	db := setupQueryTestDB(t)
	idx := newQueryTestIndexer(t, db)
	if err := db.Close(); err != nil {
		t.Fatalf("db.Close: %v", err)
	}
	_, err := idx.BinaryTop200(context.Background(), make([]byte, 32), "", EcoGo)
	if err == nil {
		t.Error("expected query error on closed DB; got nil")
	}
}

func TestIndexerQuery_FTS5Top200_QueryErrorOnClosedDB(t *testing.T) {
	db := setupQueryTestDB(t)
	idx := newQueryTestIndexer(t, db)
	if err := db.Close(); err != nil {
		t.Fatalf("db.Close: %v", err)
	}
	_, err := idx.FTS5Top200(context.Background(), "sha256", "", EcoGo)
	if err == nil {
		t.Error("expected query error on closed DB; got nil")
	}
}

func TestIndexerQuery_HydrateChunks_QueryErrorOnClosedDB(t *testing.T) {
	db := setupQueryTestDB(t)
	idx := newQueryTestIndexer(t, db)
	if err := db.Close(); err != nil {
		t.Fatalf("db.Close: %v", err)
	}
	_, err := idx.HydrateChunks(context.Background(), []int64{1, 2, 3}, EcoGo)
	if err == nil {
		t.Error("expected query error on closed DB; got nil")
	}
}

type indexerQueryAdapter interface {
	BinaryTop200(ctx context.Context, queryEmbBin []byte, versionFilter string, eco Ecosystem) ([]Candidate, error)
	FTS5Top200(ctx context.Context, queryText, versionFilter string, eco Ecosystem) ([]Candidate, error)
	HydrateChunks(ctx context.Context, chunkIDs []int64, eco Ecosystem) ([]QueryChunk, error)
}

func TestIndexerQuery_SatisfiesAdapter(t *testing.T) {
	db := setupQueryTestDB(t)
	idx := newQueryTestIndexer(t, db)
	var _ indexerQueryAdapter = idx
	_ = idx
}
