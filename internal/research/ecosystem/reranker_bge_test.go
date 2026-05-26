package ecosystem

import (
	"context"
	"errors"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestBGEReRanker_RerankOrderCorrectness(t *testing.T) {
	r := newTestBGEReRanker(t)
	candidates := []Candidate{
		{ChunkID: 1, Ecosystem: EcoGo, ContentText: "completely unrelated text about cooking", SimilarityScore: 0.1},
		{ChunkID: 2, Ecosystem: EcoGo, ContentText: "goroutine pattern with context.Context cancellation", SimilarityScore: 0.5},
		{ChunkID: 3, Ecosystem: EcoGo, ContentText: "goroutine pipeline with channels and context", SimilarityScore: 0.7},
		{ChunkID: 4, Ecosystem: EcoGo, ContentText: "another unrelated text about painting", SimilarityScore: 0.15},
	}
	query := "how do I use goroutines with context for cancellation?"
	out, err := r.Rerank(context.Background(), query, candidates, 4)
	if err != nil {
		t.Fatalf("Rerank: %v", err)
	}
	if len(out) != 4 {
		t.Fatalf("want 4 results, got %d", len(out))
	}

	relevantSeen := 0
	for i := 0; i < 2; i++ {
		if out[i].ChunkID == 2 || out[i].ChunkID == 3 {
			relevantSeen++
		}
	}
	if relevantSeen != 2 {
		t.Errorf("top-2 must contain the two relevant chunks; got %+v", out[:2])
	}

	sorted := sort.SliceIsSorted(out, func(i, j int) bool { return out[i].RerankerScore > out[j].RerankerScore })
	if !sorted {
		t.Errorf("output not sorted by RerankerScore desc: %+v", out)
	}

	for i := range out {
		if out[i].Rank != i+1 {
			t.Errorf("Rank[%d] = %d, want %d", i, out[i].Rank, i+1)
		}
	}
}

func TestBGEReRanker_TopKTruncation(t *testing.T) {
	r := newTestBGEReRanker(t)
	candidates := buildFakeCandidates(20)
	out, err := r.Rerank(context.Background(), "any", candidates, 5)
	if err != nil {
		t.Fatalf("Rerank: %v", err)
	}
	if len(out) != 5 {
		t.Errorf("topK=5 must return exactly 5; got %d", len(out))
	}
}

func TestBGEReRanker_LatencyBenchmark100Candidates(t *testing.T) {
	if testing.Short() {
		t.Skip("perf; skip -short")
	}
	r := newTestBGEReRanker(t)
	candidates := buildFakeCandidates(100)
	ctx := context.Background()
	start := timeNow()
	_, err := r.Rerank(ctx, "goroutine context cancel", candidates, 10)
	elapsed := timeSince(start)
	if err != nil {
		t.Fatalf("Rerank: %v", err)
	}

	if elapsed.Milliseconds() > 10 {
		t.Errorf("mock rerank latency %dms exceeds unit-mode budget (10ms)", elapsed.Milliseconds())
	}
}

func TestBGEReRanker_BatchThroughput(t *testing.T) {
	r := newTestBGEReRanker(t)
	candidates := buildFakeCandidates(64)
	out, err := r.Rerank(context.Background(), "batch test", candidates, 64)
	if err != nil {
		t.Fatalf("Rerank: %v", err)
	}
	if len(out) != 64 {
		t.Errorf("topK=64 over 64 candidates: want 64; got %d", len(out))
	}
}

func TestBGEReRanker_EmptyCandidates_ReturnsEmpty(t *testing.T) {
	r := newTestBGEReRanker(t)
	out, err := r.Rerank(context.Background(), "any", nil, 10)
	if err != nil {
		t.Fatalf("Rerank: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("empty input must yield empty output; got %d", len(out))
	}
}

func TestBGEReRanker_ContextCancel(t *testing.T) {
	r := newTestBGEReRanker(t)
	ctx, cancel := contextWithCancel()
	cancel()
	_, err := r.Rerank(ctx, "any", buildFakeCandidates(10), 5)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("want context.Canceled, got %v", err)
	}
}

func TestBGEReRanker_TopKLessThan1_DefaultsToLenCandidates(t *testing.T) {
	r := newTestBGEReRanker(t)
	out, err := r.Rerank(context.Background(), "any", buildFakeCandidates(7), 0)
	if err != nil {
		t.Fatalf("Rerank: %v", err)
	}
	if len(out) != 7 {
		t.Errorf("topK=0 must return all 7; got %d", len(out))
	}
}

func TestBGEReRanker_TopKLargerThanInput_ReturnsAll(t *testing.T) {
	r := newTestBGEReRanker(t)
	out, err := r.Rerank(context.Background(), "any", buildFakeCandidates(3), 99)
	if err != nil {
		t.Fatalf("Rerank: %v", err)
	}
	if len(out) != 3 {
		t.Errorf("topK=99 over 3 candidates: want 3; got %d", len(out))
	}
}

func TestBGEReRanker_Close_Idempotent(t *testing.T) {
	r := newTestBGEReRanker(t)
	if err := r.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}

	if err := r.Close(); err != nil {
		t.Errorf("second Close errored: %v", err)
	}
}

func TestBGEReRanker_RerankAfterClose_ReturnsError(t *testing.T) {
	r := newTestBGEReRanker(t)
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_, err := r.Rerank(context.Background(), "any", buildFakeCandidates(3), 3)
	if err == nil {
		t.Errorf("Rerank after Close must error; got nil")
	}
}

// TestBGEReRanker_Close_BlocksUntilInFlightRerankFinishes verifies the
// happens-before edge introduced by acquiring r.mu in Close(): when Close
// fires while Rerank → backend.Score is still executing, Close MUST block
// until the in-flight Score returns. Without this serialization, the cgo
// ONNX backend would nil its runner pointer concurrently with Score
// reading it (data race detected by `go test -race`).
//
// Mechanism a channel-instrumented mock backend gates the Score call
// open until the test releases it. Close is launched in a separate
// goroutine after Score is observed in-flight; the test asserts Close
// does NOT return inside a small window, then unblocks Score and
// confirms Close returns shortly after.
func TestBGEReRanker_Close_BlocksUntilInFlightRerankFinishes(t *testing.T) {
	backend := newSlowMockBackend()
	r := &BGEReRankerV2M3{
		cfg:     BGEConfig{Backend: BGEBackendMock, MaxLatencyMs: 300, MaxSeqLen: 512, BatchSize: 32},
		backend: backend,
	}

	rerankDone := make(chan struct{})
	go func() {
		defer close(rerankDone)
		_, _ = r.Rerank(context.Background(), "q", buildFakeCandidates(1), 1)
	}()

	select {
	case <-backend.scoreStarted:
	case <-time.After(2 * time.Second):
		t.Fatalf("Score did not start within 2s — test setup error")
	}

	// Fire Close concurrently; it MUST block on r.mu until Score returns.
	closeReturned := make(chan error, 1)
	go func() { closeReturned <- r.Close() }()

	select {
	case err := <-closeReturned:
		t.Errorf("Close returned (err=%v) before in-flight Score completed — race not guarded", err)
	case <-time.After(50 * time.Millisecond):

	}

	close(backend.scoreContinue)

	select {
	case <-rerankDone:
	case <-time.After(1 * time.Second):
		t.Fatalf("Rerank did not complete after unblocking Score")
	}

	select {
	case err := <-closeReturned:
		if err != nil {
			t.Errorf("Close returned err=%v after Score finished; want nil", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("Close did not return after Rerank completed")
	}

	if backend.closeCalled.Load() != 1 {
		t.Errorf("backend.Close should have been called exactly once; got %d", backend.closeCalled.Load())
	}
}

func TestBGEReRanker_CountReranks_Incremented(t *testing.T) {
	r := newTestBGEReRanker(t)
	for i := 0; i < 5; i++ {
		_, err := r.Rerank(context.Background(), "any", buildFakeCandidates(3), 3)
		if err != nil {
			t.Fatalf("Rerank iter %d: %v", i, err)
		}
	}
	if got := r.CountReranks(); got != 5 {
		t.Errorf("CountReranks = %d, want 5", got)
	}
}

func TestBGEReRanker_TieBreak_SimilarityThenChunkID(t *testing.T) {

	r := newTestBGEReRanker(t)

	candidates := []Candidate{
		{ChunkID: 7, Ecosystem: EcoGo, ContentText: "alpha beta gamma", SimilarityScore: 0.3},
		{ChunkID: 3, Ecosystem: EcoGo, ContentText: "alpha beta gamma", SimilarityScore: 0.5},
		{ChunkID: 5, Ecosystem: EcoGo, ContentText: "alpha beta gamma", SimilarityScore: 0.5},
	}
	out, err := r.Rerank(context.Background(), "completely orthogonal terms here", candidates, 3)
	if err != nil {
		t.Fatalf("Rerank: %v", err)
	}

	if out[0].ChunkID != 3 {
		t.Errorf("first result must be ChunkID 3 (highest sim, smallest ID); got %d", out[0].ChunkID)
	}
	if out[1].ChunkID != 5 {
		t.Errorf("second result must be ChunkID 5 (highest sim, larger ID); got %d", out[1].ChunkID)
	}
	if out[2].ChunkID != 7 {
		t.Errorf("third result must be ChunkID 7 (lowest sim); got %d", out[2].ChunkID)
	}
}

func TestBGEReRanker_NewBGEReRankerV2M3_RejectsEmptyBackend(t *testing.T) {
	_, err := NewBGEReRankerV2M3(BGEConfig{Backend: ""})
	if err == nil {
		t.Errorf("empty Backend must error")
	}
}

func TestBGEReRanker_NewBGEReRankerV2M3_RejectsUnknownBackend(t *testing.T) {
	_, err := NewBGEReRankerV2M3(BGEConfig{Backend: BGEBackend("does-not-exist")})
	if err == nil {
		t.Errorf("unknown Backend must error")
	}
}

func TestBGEReRanker_NewBGEReRankerV2M3_DefaultsMaxLatencyMs(t *testing.T) {
	r, err := NewBGEReRankerV2M3(BGEConfig{Backend: BGEBackendMock})
	if err != nil {
		t.Fatalf("NewBGEReRankerV2M3: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	if r.cfg.MaxLatencyMs != 300 {
		t.Errorf("MaxLatencyMs default = %d, want 300", r.cfg.MaxLatencyMs)
	}
	if r.cfg.MaxSeqLen != 512 {
		t.Errorf("MaxSeqLen default = %d, want 512", r.cfg.MaxSeqLen)
	}
	if r.cfg.BatchSize != 32 {
		t.Errorf("BatchSize default = %d, want 32", r.cfg.BatchSize)
	}
}

func TestBGEReRanker_NewBGEReRankerV2M3_ONNXBackendUnreachable(t *testing.T) {

	t.Setenv("ZEN_BGE_MODEL_PATH", "/nonexistent/path/to/bge-reranker-v2-m3.onnx")
	t.Setenv("HOME", t.TempDir())
	_, err := NewBGEReRankerV2M3(BGEConfig{Backend: BGEBackendMPS})
	if err == nil {
		t.Errorf("MPS backend without model path must error")
	}
}

func TestBGEReRanker_SatisfiesRerankerInterface(t *testing.T) {

	var _ Reranker = (*BGEReRankerV2M3)(nil)
}

func TestBGEReRanker_BGEModelPathFromEnv_AbsentReturnsFalse(t *testing.T) {

	t.Setenv("ZEN_BGE_MODEL_PATH", "")
	_, ok := bgeModelPathFromEnv()
	if ok {
		t.Errorf("absent ZEN_BGE_MODEL_PATH must yield ok=false")
	}
}

func TestBGEReRanker_BGEModelPathFromEnv_PresentReturnsTrue(t *testing.T) {
	t.Setenv("ZEN_BGE_MODEL_PATH", "/tmp/fake-model.onnx")
	v, ok := bgeModelPathFromEnv()
	if !ok || v != "/tmp/fake-model.onnx" {
		t.Errorf("present env: want (/tmp/fake-model.onnx, true); got (%q, %v)", v, ok)
	}
}

func TestBGEReRanker_BuildRealisticCandidates_ShapeAndDeterminism(t *testing.T) {

	c1 := buildRealisticCandidates(10)
	c2 := buildRealisticCandidates(10)
	if len(c1) != 10 || len(c2) != 10 {
		t.Fatalf("want 10 candidates each; got %d / %d", len(c1), len(c2))
	}

	for i := range c1 {
		if c1[i] != c2[i] {
			t.Errorf("non-deterministic at idx %d: %+v vs %+v", i, c1[i], c2[i])
		}
	}

	for i, c := range c1 {
		if c.ChunkID != int64(i+1) {
			t.Errorf("ChunkID[%d] = %d, want %d", i, c.ChunkID, i+1)
		}
	}

	if c1[0].SymbolPath == c1[1].SymbolPath {
		t.Errorf("SymbolPath should vary; got %q for both 0 and 1", c1[0].SymbolPath)
	}
}

func TestBGEReRanker_JaccardSet_BothEmpty(t *testing.T) {

	if got := jaccardSet(nil, nil); got != 0 {
		t.Errorf("jaccardSet(nil, nil) = %f, want 0", got)
	}
}

func TestBGEReRanker_MockBackend_EmptyQuery_PreservesOriginalOrder(t *testing.T) {

	r := newTestBGEReRanker(t)
	candidates := []Candidate{
		{ChunkID: 1, Ecosystem: EcoGo, ContentText: "alpha", SimilarityScore: 0.1},
		{ChunkID: 2, Ecosystem: EcoGo, ContentText: "beta", SimilarityScore: 0.5},
		{ChunkID: 3, Ecosystem: EcoGo, ContentText: "gamma", SimilarityScore: 0.3},
	}
	out, err := r.Rerank(context.Background(), "", candidates, 3)
	if err != nil {
		t.Fatalf("Rerank: %v", err)
	}

	want := []int64{2, 3, 1}
	for i, w := range want {
		if out[i].ChunkID != w {
			t.Errorf("position %d: want ChunkID %d, got %d", i, w, out[i].ChunkID)
		}
	}
}

func newTestBGEReRanker(t *testing.T) *BGEReRankerV2M3 {
	t.Helper()
	r, err := NewBGEReRankerV2M3(BGEConfig{
		Backend:      BGEBackendMock,
		MaxLatencyMs: 300,
	})
	if err != nil {
		t.Fatalf("NewBGEReRankerV2M3: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func buildFakeCandidates(n int) []Candidate {
	out := make([]Candidate, n)
	for i := 0; i < n; i++ {
		out[i] = Candidate{
			ChunkID:         int64(i + 1),
			Ecosystem:       AllEcosystems[i%len(AllEcosystems)],
			ContentText:     fakeContent(i),
			SymbolPath:      fakeSymbolPath(i),
			SimilarityScore: float64(i) * 0.01,
		}
	}
	return out
}

func fakeContent(i int) string    { return "fake content body number " + itoa(i) }
func fakeSymbolPath(i int) string { return "fake.pkg.Symbol" + itoa(i) }

type slowMockBackend struct {
	scoreStarted  chan struct{}
	scoreContinue chan struct{}
	closeCalled   atomic.Int32

	scoreOnce sync.Once
}

func newSlowMockBackend() *slowMockBackend {
	return &slowMockBackend{
		scoreStarted:  make(chan struct{}),
		scoreContinue: make(chan struct{}),
	}
}

func (s *slowMockBackend) Score(ctx context.Context, query string, cands []Candidate) ([]float64, error) {
	s.scoreOnce.Do(func() { close(s.scoreStarted) })
	select {
	case <-s.scoreContinue:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	out := make([]float64, len(cands))
	for i := range cands {
		out[i] = 0.5 + 0.001*float64(i)
	}
	return out, nil
}

func (s *slowMockBackend) Close() error {
	s.closeCalled.Add(1)
	return nil
}
