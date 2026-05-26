package ecosystem

import (
	"context"
	"sort"
	"testing"
)

func TestNoopRerankerImplementsRerankerInterface(t *testing.T) {
	var _ Reranker = (*NoopReranker)(nil)
}

func TestNoopRerankerRerankReturnsInputOrder(t *testing.T) {
	r := &NoopReranker{}
	cands := []Candidate{
		{ChunkID: 3, Ecosystem: EcoGo, ContentText: "third", SymbolPath: "x", SourceURL: "u3", SimilarityScore: 0.30},
		{ChunkID: 1, Ecosystem: EcoGo, ContentText: "first", SymbolPath: "y", SourceURL: "u1", SimilarityScore: 0.90},
		{ChunkID: 2, Ecosystem: EcoGo, ContentText: "second", SymbolPath: "z", SourceURL: "u2", SimilarityScore: 0.50},
	}
	got, err := r.Rerank(context.Background(), "query", cands, 10)
	if err != nil {
		t.Fatalf("Rerank: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len(got) = %d; want 3", len(got))
	}

	for i, rr := range got {
		if rr.Rank != i+1 {
			t.Errorf("got[%d].Rank = %d; want %d", i, rr.Rank, i+1)
		}
		if rr.ChunkID != cands[i].ChunkID {
			t.Errorf("got[%d].ChunkID = %d; want %d (input order preserved)",
				i, rr.ChunkID, cands[i].ChunkID)
		}
	}
}

func TestNoopRerankerTopKTruncates(t *testing.T) {
	r := &NoopReranker{}
	cands := make([]Candidate, 100)
	for i := range cands {
		cands[i] = Candidate{ChunkID: int64(i), Ecosystem: EcoGo}
	}
	got, err := r.Rerank(context.Background(), "q", cands, 10)
	if err != nil {
		t.Fatalf("Rerank: %v", err)
	}
	if len(got) != 10 {
		t.Errorf("len(got) = %d; want 10 (topK truncation)", len(got))
	}
}

func TestNoopRerankerTopKLargerThanCandidates(t *testing.T) {
	r := &NoopReranker{}
	cands := []Candidate{{ChunkID: 1}, {ChunkID: 2}}
	got, err := r.Rerank(context.Background(), "q", cands, 10)
	if err != nil {
		t.Fatalf("Rerank: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("len(got) = %d; want 2 (topK > len(cands) caps at input len)", len(got))
	}
}

func TestNoopRerankerEmptyCandidates(t *testing.T) {
	r := &NoopReranker{}
	got, err := r.Rerank(context.Background(), "q", nil, 10)
	if err != nil {
		t.Fatalf("Rerank(nil): %v", err)
	}
	if len(got) != 0 {
		t.Errorf("len(got) = %d; want 0", len(got))
	}
}

func TestNoopRerankerRanksAreContiguous(t *testing.T) {
	r := &NoopReranker{}
	cands := make([]Candidate, 5)
	for i := range cands {
		cands[i] = Candidate{ChunkID: int64(i + 1)}
	}
	got, err := r.Rerank(context.Background(), "q", cands, 5)
	if err != nil {
		t.Fatalf("Rerank: %v", err)
	}
	sort.Slice(got, func(i, j int) bool { return got[i].Rank < got[j].Rank })
	for i, rr := range got {
		if rr.Rank != i+1 {
			t.Errorf("got[%d].Rank = %d; want %d (1-based contiguous)", i, rr.Rank, i+1)
		}
	}
}

func TestNoopRerankerContextCancel(t *testing.T) {
	r := &NoopReranker{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.Rerank(ctx, "q", []Candidate{{ChunkID: 1}}, 5); err == nil {
		t.Errorf("Rerank(cancelled): want error; got nil")
	}
}

func TestNoopRerankerClose(t *testing.T) {
	r := &NoopReranker{}
	if err := r.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestRerankerConfigFields(t *testing.T) {
	c := RerankerConfig{
		Model:        "bge-reranker-v2-m3",
		Backend:      "mps",
		APITokenKey:  "",
		MaxLatencyMs: 300,
	}
	if c.Model == "" || c.MaxLatencyMs <= 0 {
		t.Errorf("RerankerConfig field-set mismatch: %+v", c)
	}
}

func TestCandidateFields(t *testing.T) {
	c := Candidate{
		ChunkID:         42,
		Ecosystem:       EcoGo,
		ContentText:     "...",
		SymbolPath:      "crypto/sha256.Sum256",
		SourceURL:       "https://pkg.go.dev/crypto/sha256#Sum256",
		SimilarityScore: 0.82,
	}
	if c.ChunkID != 42 || c.SimilarityScore < 0 {
		t.Errorf("Candidate field-set mismatch: %+v", c)
	}
}

func TestRankedResultFields(t *testing.T) {
	r := RankedResult{
		Candidate: Candidate{
			ChunkID:         42,
			Ecosystem:       EcoGo,
			ContentText:     "...",
			SymbolPath:      "crypto/sha256.Sum256",
			SourceURL:       "https://pkg.go.dev/crypto/sha256#Sum256",
			SimilarityScore: 0.82,
		},
		RerankerScore: 0.91,
		Rank:          1,
	}

	if r.ChunkID != 42 || r.RerankerScore < 0 || r.Rank < 1 {
		t.Errorf("RankedResult field-set mismatch: %+v", r)
	}
}
