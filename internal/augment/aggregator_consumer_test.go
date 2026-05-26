package augment_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/augment"
)

type fakeIndex struct {
	queryFTSFn   func(ctx context.Context, queryText string, limit int) ([]augment.QueryResult, error)
	queryVecFn   func(ctx context.Context, embedding []float32, limit int, threshold float64) ([]augment.QueryResult, error)
	queryGraphFn func(ctx context.Context, seedNoteIDs []string, depth, limit int) ([]augment.QueryResult, error)
}

func (f *fakeIndex) QueryFTS(ctx context.Context, queryText string, limit int) ([]augment.QueryResult, error) {
	if f.queryFTSFn == nil {
		return nil, errors.New("queryFTSFn not configured")
	}
	return f.queryFTSFn(ctx, queryText, limit)
}
func (f *fakeIndex) QueryVec(ctx context.Context, embedding []float32, limit int, threshold float64) ([]augment.QueryResult, error) {
	if f.queryVecFn == nil {
		return nil, errors.New("queryVecFn not configured")
	}
	return f.queryVecFn(ctx, embedding, limit, threshold)
}
func (f *fakeIndex) QueryGraph(ctx context.Context, seedNoteIDs []string, depth, limit int) ([]augment.QueryResult, error) {
	if f.queryGraphFn == nil {
		return nil, errors.New("queryGraphFn not configured")
	}
	return f.queryGraphFn(ctx, seedNoteIDs, depth, limit)
}

type fakeEmbedder struct {
	embedFn func(ctx context.Context, text string) ([]float32, error)
}

func (f *fakeEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	if f.embedFn == nil {
		return nil, errors.New("embedFn not configured")
	}
	return f.embedFn(ctx, text)
}

func newFakeIndexEmbedder() (*fakeIndex, *fakeEmbedder) {
	return &fakeIndex{}, &fakeEmbedder{}
}

func TestAggregatorConsumer_Lane2FTSReturnsResults(t *testing.T) {
	idx, emb := newFakeIndexEmbedder()
	idx.queryFTSFn = func(_ context.Context, qt string, lim int) ([]augment.QueryResult, error) {
		if qt != "MergeEngine" {
			t.Errorf("queryText: want MergeEngine, got %q", qt)
		}
		if lim != 20 {
			t.Errorf("limit: want 20, got %d", lim)
		}
		return []augment.QueryResult{
			{NoteID: "n1", Title: "Engine.Select", Score: 1.5, ProjectID: "internal-platform-x", Source: "fts"},
			{NoteID: "n2", Title: "Engine.Run", Score: 1.2, ProjectID: "internal-platform-x", Source: "fts"},
		}, nil
	}
	c := augment.NewAggregatorConsumer(idx, emb)

	res, err := c.Lane2FTS(context.Background(), "MergeEngine", 20)
	if err != nil {
		t.Fatalf("Lane2FTS: %v", err)
	}
	if res.LaneID != 2 {
		t.Errorf("LaneID: want 2, got %d", res.LaneID)
	}
	if len(res.Results) != 2 {
		t.Errorf("Results: want 2, got %d", len(res.Results))
	}
	if res.ElapsedMs < 0 {
		t.Errorf("ElapsedMs: want >= 0, got %d", res.ElapsedMs)
	}
}

func TestAggregatorConsumer_Lane2FTSPropagatesError(t *testing.T) {
	idx, emb := newFakeIndexEmbedder()
	idx.queryFTSFn = func(_ context.Context, _ string, _ int) ([]augment.QueryResult, error) {
		return nil, errors.New("fts kaboom")
	}
	c := augment.NewAggregatorConsumer(idx, emb)
	_, err := c.Lane2FTS(context.Background(), "x", 10)
	if err == nil || !contains(err.Error(), "fts kaboom") {
		t.Fatalf("expected fts error, got %v", err)
	}
}

func TestAggregatorConsumer_Lane2FTSPassesAllProjects(t *testing.T) {

	idx, emb := newFakeIndexEmbedder()
	idx.queryFTSFn = func(_ context.Context, _ string, _ int) ([]augment.QueryResult, error) {
		return []augment.QueryResult{
			{NoteID: "n1", ProjectID: "internal-platform-x", Source: "fts"},
			{NoteID: "n2", ProjectID: "other-proj", Source: "fts"},
			{NoteID: "n3", ProjectID: "", Source: "fts"},
		}, nil
	}
	c := augment.NewAggregatorConsumer(idx, emb)
	res, err := c.Lane2FTS(context.Background(), "x", 10)
	if err != nil {
		t.Fatalf("Lane2FTS: %v", err)
	}

	if len(res.Results) != 3 {
		t.Errorf("expected 3 rows (no per-lane project filter), got %d", len(res.Results))
	}
}

func TestAggregatorConsumer_Lane4VecHappyPath(t *testing.T) {
	idx, emb := newFakeIndexEmbedder()
	emb.embedFn = func(_ context.Context, text string) ([]float32, error) {
		if text != "MergeEngine" {
			t.Errorf("embed text: want MergeEngine, got %q", text)
		}
		return []float32{0.1, 0.2, 0.3}, nil
	}
	idx.queryVecFn = func(_ context.Context, embedding []float32, lim int, thr float64) ([]augment.QueryResult, error) {
		if len(embedding) != 3 {
			t.Errorf("embedding dim: want 3, got %d", len(embedding))
		}
		if thr != 0.92 {
			t.Errorf("threshold: want 0.92, got %f", thr)
		}
		return []augment.QueryResult{
			{NoteID: "n1", Score: 0.95, Source: "vec", ProjectID: "internal-platform-x"},
		}, nil
	}
	c := augment.NewAggregatorConsumer(idx, emb)
	res, err := c.Lane4Vec(context.Background(), "MergeEngine", 20, 0.92)
	if err != nil {
		t.Fatalf("Lane4Vec: %v", err)
	}
	if res.Degraded {
		t.Fatal("expected Degraded=false on happy path")
	}
	if len(res.Results) != 1 {
		t.Errorf("Results: want 1, got %d", len(res.Results))
	}
	if res.LaneID != 4 {
		t.Errorf("LaneID: want 4, got %d", res.LaneID)
	}
}

func TestAggregatorConsumer_Lane4VecDegradesOnEmbedError(t *testing.T) {
	idx, emb := newFakeIndexEmbedder()
	emb.embedFn = func(_ context.Context, _ string) ([]float32, error) {
		return nil, errors.New("MPS GPU unavailable")
	}
	c := augment.NewAggregatorConsumer(idx, emb)
	res, err := c.Lane4Vec(context.Background(), "MergeEngine", 20, 0.92)
	if err != nil {
		t.Fatalf("Lane4Vec: should not error on embed failure (graceful degrade), got %v", err)
	}
	if !res.Degraded {
		t.Fatal("expected Degraded=true on embed failure")
	}
	if len(res.Results) != 0 {
		t.Errorf("Results: want 0 on degraded mode, got %d", len(res.Results))
	}
}

func TestAggregatorConsumer_Lane4VecDegradesOnEmptyEmbedding(t *testing.T) {
	idx, emb := newFakeIndexEmbedder()
	emb.embedFn = func(_ context.Context, _ string) ([]float32, error) {
		return nil, nil
	}
	c := augment.NewAggregatorConsumer(idx, emb)
	res, err := c.Lane4Vec(context.Background(), "x", 10, 0.9)
	if err != nil {
		t.Fatalf("Lane4Vec: %v", err)
	}
	if !res.Degraded {
		t.Error("expected Degraded=true on empty embedding")
	}
}

func TestAggregatorConsumer_Lane4VecDegradesOnVecUnavailable(t *testing.T) {
	idx, emb := newFakeIndexEmbedder()
	emb.embedFn = func(_ context.Context, _ string) ([]float32, error) {
		return []float32{0.1, 0.2, 0.3}, nil
	}
	idx.queryVecFn = func(_ context.Context, _ []float32, _ int, _ float64) ([]augment.QueryResult, error) {
		return nil, errors.New("aggregator: sqlite-vec unavailable (degraded mode)")
	}
	c := augment.NewAggregatorConsumer(idx, emb)
	res, err := c.Lane4Vec(context.Background(), "MergeEngine", 20, 0.92)
	if err != nil {
		t.Fatalf("Lane4Vec: should degrade silently when vec unavailable, got %v", err)
	}
	if !res.Degraded {
		t.Fatal("expected Degraded=true on sqlite-vec unavailable")
	}
}

func TestAggregatorConsumer_Lane4VecPropagatesGenuineError(t *testing.T) {
	idx, emb := newFakeIndexEmbedder()
	emb.embedFn = func(_ context.Context, _ string) ([]float32, error) {
		return []float32{0.1}, nil
	}
	idx.queryVecFn = func(_ context.Context, _ []float32, _ int, _ float64) ([]augment.QueryResult, error) {
		return nil, errors.New("real db corruption")
	}
	c := augment.NewAggregatorConsumer(idx, emb)
	_, err := c.Lane4Vec(context.Background(), "x", 10, 0.9)
	if err == nil {
		t.Fatal("expected genuine error to propagate")
	}
}

func TestAggregatorConsumer_Lane4VecDefaultsThreshold(t *testing.T) {
	idx, emb := newFakeIndexEmbedder()
	emb.embedFn = func(_ context.Context, _ string) ([]float32, error) {
		return []float32{0.1}, nil
	}
	idx.queryVecFn = func(_ context.Context, _ []float32, _ int, thr float64) ([]augment.QueryResult, error) {
		if thr != augment.VecSimilarityThreshold {
			t.Errorf("threshold default: want %f, got %f", augment.VecSimilarityThreshold, thr)
		}
		return nil, nil
	}
	c := augment.NewAggregatorConsumer(idx, emb)
	_, _ = c.Lane4Vec(context.Background(), "x", 10, 0)
}

func TestAggregatorConsumer_Lane4VecPassesAllProjects(t *testing.T) {
	idx, emb := newFakeIndexEmbedder()
	emb.embedFn = func(_ context.Context, _ string) ([]float32, error) {
		return []float32{0.1}, nil
	}
	idx.queryVecFn = func(_ context.Context, _ []float32, _ int, _ float64) ([]augment.QueryResult, error) {
		return []augment.QueryResult{
			{NoteID: "n1", ProjectID: "internal-platform-x"},
			{NoteID: "n2", ProjectID: "other"},
		}, nil
	}
	c := augment.NewAggregatorConsumer(idx, emb)
	res, err := c.Lane4Vec(context.Background(), "x", 10, 0.9)
	if err != nil {
		t.Fatalf("Lane4Vec: %v", err)
	}

	if len(res.Results) != 2 {
		t.Errorf("expected 2 rows (no per-lane project filter), got %d", len(res.Results))
	}
}

func TestAggregatorConsumer_Lane5TemporalBasic(t *testing.T) {
	idx, emb := newFakeIndexEmbedder()
	idx.queryFTSFn = func(_ context.Context, _ string, _ int) ([]augment.QueryResult, error) {
		return []augment.QueryResult{
			{NoteID: "recent", Title: "Recent", Score: 1.0, ProjectID: "internal-platform-x", Source: "fts"},
			{NoteID: "old", Title: "Old", Score: 1.0, ProjectID: "internal-platform-x", Source: "fts"},
		}, nil
	}
	c := augment.NewAggregatorConsumer(idx, emb)
	res, err := c.Lane5Temporal(context.Background(), "MergeEngine", time.Time{}, 20)
	if err != nil {
		t.Fatalf("Lane5Temporal: %v", err)
	}
	if res.LaneID != 5 {
		t.Errorf("LaneID: want 5, got %d", res.LaneID)
	}
	if len(res.Results) != 2 {
		t.Errorf("Results: want 2, got %d", len(res.Results))
	}
	for _, r := range res.Results {
		if r.Source != "temporal" {
			t.Errorf("Source: want temporal, got %q", r.Source)
		}
	}
}

func TestAggregatorConsumer_Lane5TemporalAppliesDecay(t *testing.T) {
	idx, emb := newFakeIndexEmbedder()
	idx.queryFTSFn = func(_ context.Context, _ string, _ int) ([]augment.QueryResult, error) {

		return []augment.QueryResult{
			{NoteID: "old", Title: "Old", Score: 10.0, ProjectID: "p", Source: "fts", AuditChainAnchor: "2025_01:evt-x:hash"},
		}, nil
	}
	c := augment.NewAggregatorConsumer(idx, emb)
	res, err := c.Lane5Temporal(context.Background(), "x", time.Time{}, 5)
	if err != nil {
		t.Fatalf("Lane5Temporal: %v", err)
	}
	if len(res.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(res.Results))
	}

	if res.Results[0].Score >= 10.0 {
		t.Errorf("expected decayed score, got %f", res.Results[0].Score)
	}
}

func TestAggregatorConsumer_Lane5TemporalSinceFilter(t *testing.T) {
	idx, emb := newFakeIndexEmbedder()
	idx.queryFTSFn = func(_ context.Context, _ string, _ int) ([]augment.QueryResult, error) {
		return []augment.QueryResult{
			{NoteID: "old", Title: "Old", Score: 1.0, ProjectID: "p", AuditChainAnchor: "2024_01:e:h"},
		}, nil
	}
	c := augment.NewAggregatorConsumer(idx, emb)

	res, err := c.Lane5Temporal(context.Background(), "x", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), 5)
	if err != nil {
		t.Fatalf("Lane5Temporal: %v", err)
	}
	if len(res.Results) != 0 {
		t.Errorf("expected 0 results past since filter, got %d", len(res.Results))
	}
}

func TestAggregatorConsumer_Lane5TemporalQueryFTSError(t *testing.T) {
	idx, emb := newFakeIndexEmbedder()
	idx.queryFTSFn = func(_ context.Context, _ string, _ int) ([]augment.QueryResult, error) {
		return nil, errors.New("fts boom")
	}
	c := augment.NewAggregatorConsumer(idx, emb)
	_, err := c.Lane5Temporal(context.Background(), "x", time.Time{}, 5)
	if err == nil {
		t.Fatal("expected FTS error to propagate")
	}
}

func TestAggregatorConsumer_Lane5TemporalPassesAllProjects(t *testing.T) {
	idx, emb := newFakeIndexEmbedder()
	idx.queryFTSFn = func(_ context.Context, _ string, _ int) ([]augment.QueryResult, error) {
		return []augment.QueryResult{
			{NoteID: "p1", ProjectID: "p"},
			{NoteID: "other", ProjectID: "other"},
		}, nil
	}
	c := augment.NewAggregatorConsumer(idx, emb)
	res, err := c.Lane5Temporal(context.Background(), "x", time.Time{}, 5)
	if err != nil {
		t.Fatalf("Lane5Temporal: %v", err)
	}

	if len(res.Results) != 2 {
		t.Errorf("expected 2 rows (no per-lane project filter), got %d", len(res.Results))
	}
}

func TestAggregatorConsumer_Lane5TemporalMalformedAnchor(t *testing.T) {
	idx, emb := newFakeIndexEmbedder()
	idx.queryFTSFn = func(_ context.Context, _ string, _ int) ([]augment.QueryResult, error) {
		return []augment.QueryResult{
			{NoteID: "x", ProjectID: "p", Score: 1.0, AuditChainAnchor: "malformed"},
			{NoteID: "y", ProjectID: "p", Score: 1.0, AuditChainAnchor: "BAD_xx:e:h"},
			{NoteID: "z", ProjectID: "p", Score: 1.0, AuditChainAnchor: "2026_99:e:h"},
		}, nil
	}
	c := augment.NewAggregatorConsumer(idx, emb)
	res, err := c.Lane5Temporal(context.Background(), "x", time.Time{}, 5)
	if err != nil {
		t.Fatalf("Lane5Temporal: %v", err)
	}

	if len(res.Results) != 3 {
		t.Errorf("expected 3 results (no anchor filter), got %d", len(res.Results))
	}
}

func TestAggregatorConsumer_RunRRFCallsFuse(t *testing.T) {
	idx, emb := newFakeIndexEmbedder()
	c := augment.NewAggregatorConsumer(idx, emb)
	out := c.RunRRF(context.Background(), []augment.TopK{
		{Source: "fts", Results: []augment.QueryResult{
			{NoteID: "n-shared", Score: 2.0, Source: "fts"},
			{NoteID: "n-fts-only", Score: 1.0, Source: "fts"},
		}},
		{Source: "vec", Results: []augment.QueryResult{
			{NoteID: "n-shared", Score: 0.95, Source: "vec"},
			{NoteID: "n-vec-only", Score: 0.90, Source: "vec"},
		}},
	}, 25)
	if len(out) == 0 {
		t.Fatal("RunRRF returned 0 fused results; expected ≥1")
	}
	if out[0].NoteID != "n-shared" {
		t.Errorf("RRF top result: want n-shared, got %s", out[0].NoteID)
	}
}

func TestAggregatorConsumer_RunRRFEmptyLanes(t *testing.T) {
	idx, emb := newFakeIndexEmbedder()
	c := augment.NewAggregatorConsumer(idx, emb)
	out := c.RunRRF(context.Background(), nil, 10)
	if len(out) != 0 {
		t.Errorf("expected empty output, got %d", len(out))
	}
}

func TestAggregatorConsumer_RunRRFAllEmptyResultsLanes(t *testing.T) {
	idx, emb := newFakeIndexEmbedder()
	c := augment.NewAggregatorConsumer(idx, emb)
	out := c.RunRRF(context.Background(), []augment.TopK{
		{Source: "fts", Results: nil},
		{Source: "vec", Results: nil},
	}, 10)
	if len(out) != 0 {
		t.Errorf("expected empty output, got %d", len(out))
	}
}

func TestAggregatorConsumer_RunRRFPinSourceBoost(t *testing.T) {
	idx, emb := newFakeIndexEmbedder()
	c := augment.NewAggregatorConsumer(idx, emb)
	out := c.RunRRF(context.Background(), []augment.TopK{
		{Source: "pin", Results: []augment.QueryResult{
			{NoteID: "p1", Title: "PinnedNote", Source: "pin"},
		}},
		{Source: "fts", Results: []augment.QueryResult{
			{NoteID: "f1", Title: "FTSNote", Source: "fts"},
		}},
	}, 10)

	if len(out) != 2 {
		t.Errorf("expected 2 results, got %d", len(out))
	}
}

func TestAggregatorConsumer_RunRRFCapsAtLimit(t *testing.T) {
	idx, emb := newFakeIndexEmbedder()
	c := augment.NewAggregatorConsumer(idx, emb)
	results := make([]augment.QueryResult, 30)
	for i := range results {
		results[i] = augment.QueryResult{NoteID: fmt.Sprintf("n%d", i), Source: "fts"}
	}
	out := c.RunRRF(context.Background(), []augment.TopK{
		{Source: "fts", Results: results},
	}, 5)
	if len(out) != 5 {
		t.Errorf("expected 5 results (capped), got %d", len(out))
	}
}

func TestAggregatorConsumer_RunRRFDefaultLimit(t *testing.T) {
	idx, emb := newFakeIndexEmbedder()
	c := augment.NewAggregatorConsumer(idx, emb)
	out := c.RunRRF(context.Background(), []augment.TopK{
		{Source: "fts", Results: []augment.QueryResult{{NoteID: "n1", Score: 1.0, Source: "fts"}}},
	}, 0)
	if len(out) != 1 {
		t.Errorf("expected 1 result, got %d", len(out))
	}
}

func TestAggregatorConsumer_Lane2FTSCancellation(t *testing.T) {
	idx, emb := newFakeIndexEmbedder()
	idx.queryFTSFn = func(ctx context.Context, _ string, _ int) ([]augment.QueryResult, error) {
		return nil, ctx.Err()
	}
	c := augment.NewAggregatorConsumer(idx, emb)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.Lane2FTS(ctx, "x", 10)
	if err == nil {
		t.Fatal("expected cancellation to propagate")
	}
}

func TestAggregatorConsumer_Lane2FTSEmptyResults(t *testing.T) {
	idx, emb := newFakeIndexEmbedder()
	idx.queryFTSFn = func(_ context.Context, _ string, _ int) ([]augment.QueryResult, error) {
		return nil, nil
	}
	c := augment.NewAggregatorConsumer(idx, emb)
	res, err := c.Lane2FTS(context.Background(), "no-match", 10)
	if err != nil {
		t.Fatalf("Lane2FTS: %v", err)
	}
	if len(res.Results) != 0 {
		t.Errorf("Results: want 0, got %d", len(res.Results))
	}
	if res.LaneID != 2 {
		t.Errorf("LaneID: want 2, got %d", res.LaneID)
	}
}

func TestAggregatorConsumer_Lane5TemporalBadYearAnchor(t *testing.T) {

	idx, emb := newFakeIndexEmbedder()
	idx.queryFTSFn = func(_ context.Context, _ string, _ int) ([]augment.QueryResult, error) {
		return []augment.QueryResult{
			{NoteID: "n", ProjectID: "p", Score: 1.0, AuditChainAnchor: "ABCD_05:e:h"},
		}, nil
	}
	c := augment.NewAggregatorConsumer(idx, emb)
	res, err := c.Lane5Temporal(context.Background(), "x", time.Time{}, 5)
	if err != nil {
		t.Fatalf("Lane5Temporal: %v", err)
	}

	if len(res.Results) != 1 {
		t.Errorf("expected 1 result, got %d", len(res.Results))
	}
}

func TestAggregatorConsumer_Lane5TemporalEmptyAnchor(t *testing.T) {
	idx, emb := newFakeIndexEmbedder()
	idx.queryFTSFn = func(_ context.Context, _ string, _ int) ([]augment.QueryResult, error) {
		return []augment.QueryResult{
			{NoteID: "n", ProjectID: "p", Score: 5.0, AuditChainAnchor: ""},
		}, nil
	}
	c := augment.NewAggregatorConsumer(idx, emb)
	res, err := c.Lane5Temporal(context.Background(), "x", time.Time{}, 5)
	if err != nil {
		t.Fatalf("Lane5Temporal: %v", err)
	}

	if len(res.Results) != 1 || res.Results[0].Score != 5.0 {
		t.Errorf("expected score preserved with empty anchor, got %v", res.Results)
	}
}

func TestAggregatorConsumer_Lane5TemporalAnchorNoColon(t *testing.T) {

	idx, emb := newFakeIndexEmbedder()
	idx.queryFTSFn = func(_ context.Context, _ string, _ int) ([]augment.QueryResult, error) {
		return []augment.QueryResult{
			{NoteID: "n", ProjectID: "p", Score: 1.0, AuditChainAnchor: "no_colon_here"},
		}, nil
	}
	c := augment.NewAggregatorConsumer(idx, emb)
	res, err := c.Lane5Temporal(context.Background(), "x", time.Time{}, 5)
	if err != nil {
		t.Fatalf("Lane5Temporal: %v", err)
	}
	if len(res.Results) != 1 {
		t.Errorf("expected 1 result, got %d", len(res.Results))
	}
}

func TestAggregatorConsumer_IsVecUnavailableErr(t *testing.T) {

	idx, emb := newFakeIndexEmbedder()
	emb.embedFn = func(_ context.Context, _ string) ([]float32, error) {
		return []float32{0.1}, nil
	}
	idx.queryVecFn = func(_ context.Context, _ []float32, _ int, _ float64) ([]augment.QueryResult, error) {
		return []augment.QueryResult{{NoteID: "n1", ProjectID: ""}}, nil
	}
	c := augment.NewAggregatorConsumer(idx, emb)
	res, err := c.Lane4Vec(context.Background(), "x", 10, 0.9)
	if err != nil {
		t.Fatalf("happy: %v", err)
	}
	if res.Degraded {
		t.Error("happy path should not be degraded")
	}
}
