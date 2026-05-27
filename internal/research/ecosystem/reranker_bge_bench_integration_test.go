// go:build integration
//go:build integration
// +build integration

package ecosystem

import (
	"context"
	"sort"
	"testing"
	"time"
)

func TestBGEReRanker_RealMPS_P95_LatencyInvariant(t *testing.T) {
	if testing.Short() {
		t.Skip("perf; skip -short")
	}
	modelPath, ok := bgeModelPathFromEnv()
	if !ok {
		t.Skip("ZEN_BGE_MODEL_PATH not set; skipping real-MPS benchmark")
	}
	r, err := NewBGEReRankerV2M3(BGEConfig{
		Backend:      BGEBackendMPS,
		ModelPath:    modelPath,
		MaxLatencyMs: 300,
	})
	if err != nil {
		t.Fatalf("NewBGEReRankerV2M3: %v", err)
	}
	defer func() { _ = r.Close() }()

	candidates := buildRealisticCandidates(100)
	query := "how do I create a goroutine pipeline that respects context cancellation?"

	const trials = 20
	durations := make([]time.Duration, trials)
	for i := 0; i < trials; i++ {
		start := time.Now()
		_, err := r.Rerank(context.Background(), query, candidates, 10)
		if err != nil {
			t.Fatalf("Rerank iter %d: %v", i, err)
		}
		durations[i] = time.Since(start)
	}
	p95 := percentile(durations, 0.95)
	t.Logf("BGE-reranker-v2-m3 p95 latency (M4 MPS, 100 cands): %v", p95)
	if p95 > 300*time.Millisecond {
		t.Fatalf("inv-zen-198 violation: p95 = %v > 300ms", p95)
	}
}

func TestBGEReRanker_RealMPS_OrderingSanity(t *testing.T) {
	if testing.Short() {
		t.Skip("perf; skip -short")
	}
	modelPath, ok := bgeModelPathFromEnv()
	if !ok {
		t.Skip("ZEN_BGE_MODEL_PATH not set; skipping real-MPS ordering check")
	}
	r, err := NewBGEReRankerV2M3(BGEConfig{
		Backend:      BGEBackendMPS,
		ModelPath:    modelPath,
		MaxLatencyMs: 300,
	})
	if err != nil {
		t.Fatalf("NewBGEReRankerV2M3: %v", err)
	}
	defer func() { _ = r.Close() }()

	candidates := []Candidate{
		{ChunkID: 1, Ecosystem: EcoGo, ContentText: "unrelated text about cooking pasta", SimilarityScore: 0.5},
		{ChunkID: 2, Ecosystem: EcoGo, ContentText: "goroutine pattern with context.Context cancellation", SimilarityScore: 0.5},
		{ChunkID: 3, Ecosystem: EcoGo, ContentText: "another unrelated text about painting walls", SimilarityScore: 0.5},
		{ChunkID: 4, Ecosystem: EcoGo, ContentText: "goroutine pipeline with channels and context done", SimilarityScore: 0.5},
	}
	out, err := r.Rerank(context.Background(), "how do I use goroutines with context for cancellation?", candidates, 4)
	if err != nil {
		t.Fatalf("Rerank: %v", err)
	}
	top2 := map[int64]struct{}{out[0].ChunkID: {}, out[1].ChunkID: {}}
	_, has2 := top2[2]
	_, has4 := top2[4]
	if !has2 || !has4 {
		t.Errorf("real-MPS ranker must put on-topic ChunkIDs 2 + 4 in top-2; got %+v", out[:2])
	}
}

func percentile(d []time.Duration, p float64) time.Duration {
	if len(d) == 0 {
		return 0
	}
	if p <= 0 {
		return d[0]
	}
	if p >= 1 {
		return d[len(d)-1]
	}
	sorted := make([]time.Duration, len(d))
	copy(sorted, d)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	idx := p * float64(len(sorted)-1)
	lo := int(idx)
	hi := lo + 1
	if hi >= len(sorted) {
		return sorted[lo]
	}
	frac := idx - float64(lo)
	return sorted[lo] + time.Duration(float64(sorted[hi]-sorted[lo])*frac)
}
