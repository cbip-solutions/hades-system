package aggregatoradapter_test

import (
	"context"
	"errors"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/augment"
	"github.com/cbip-solutions/hades-system/internal/daemon/aggregatoradapter"
	"github.com/cbip-solutions/hades-system/internal/knowledge/aggregator"
)

type fakeP9Aggregator struct {
	queryFTSFn   func(ctx context.Context, qt string, lim int) ([]aggregator.QueryResult, error)
	queryVecFn   func(ctx context.Context, emb []float32, lim int, thr float64) ([]aggregator.QueryResult, error)
	queryGraphFn func(ctx context.Context, seeds []string, depth, lim int) ([]aggregator.QueryResult, error)
}

func (f *fakeP9Aggregator) QueryFTS(ctx context.Context, qt string, lim int) ([]aggregator.QueryResult, error) {
	return f.queryFTSFn(ctx, qt, lim)
}
func (f *fakeP9Aggregator) QueryVec(ctx context.Context, emb []float32, lim int, thr float64) ([]aggregator.QueryResult, error) {
	return f.queryVecFn(ctx, emb, lim, thr)
}
func (f *fakeP9Aggregator) QueryGraph(ctx context.Context, seeds []string, depth, lim int) ([]aggregator.QueryResult, error) {
	return f.queryGraphFn(ctx, seeds, depth, lim)
}

type fakeP9Embedder struct {
	embedFn func(ctx context.Context, text string) ([]float32, error)
	dim     int
}

func (f *fakeP9Embedder) Dimensions() int { return f.dim }

func (f *fakeP9Embedder) Embed(ctx context.Context, text string) ([]float32, error) {
	return f.embedFn(ctx, text)
}

func TestAdapter_SatisfiesKnowledgeIndex(t *testing.T) {
	a := &fakeP9Aggregator{
		queryFTSFn: func(_ context.Context, qt string, _ int) ([]aggregator.QueryResult, error) {
			return []aggregator.QueryResult{{NoteID: "n1", Title: qt, Score: 1.5}}, nil
		},
		queryVecFn: func(_ context.Context, _ []float32, _ int, _ float64) ([]aggregator.QueryResult, error) {
			return nil, nil
		},
		queryGraphFn: func(_ context.Context, _ []string, _, _ int) ([]aggregator.QueryResult, error) {
			return nil, nil
		},
	}
	emb := &fakeP9Embedder{
		embedFn: func(_ context.Context, _ string) ([]float32, error) {
			return []float32{0.1, 0.2, 0.3}, nil
		},
		dim: 3,
	}
	ad := aggregatoradapter.New(a, emb)

	var _ augment.KnowledgeIndex = ad

	var _ augment.Embedder = ad.Embedder()

	got, err := ad.QueryFTS(context.Background(), "test", 10)
	if err != nil {
		t.Fatalf("QueryFTS: %v", err)
	}
	if len(got) != 1 || got[0].NoteID != "n1" {
		t.Errorf("QueryFTS round-trip: got %+v", got)
	}
}

func TestAdapter_QueryFTSError(t *testing.T) {
	a := &fakeP9Aggregator{
		queryFTSFn: func(_ context.Context, _ string, _ int) ([]aggregator.QueryResult, error) {
			return nil, errors.New("fts kaboom")
		},
	}
	emb := &fakeP9Embedder{}
	ad := aggregatoradapter.New(a, emb)
	_, err := ad.QueryFTS(context.Background(), "x", 5)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAdapter_QueryVecError(t *testing.T) {
	a := &fakeP9Aggregator{
		queryVecFn: func(_ context.Context, _ []float32, _ int, _ float64) ([]aggregator.QueryResult, error) {
			return nil, errors.New("vec kaboom")
		},
	}
	emb := &fakeP9Embedder{}
	ad := aggregatoradapter.New(a, emb)
	_, err := ad.QueryVec(context.Background(), []float32{0.1}, 5, 0.9)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAdapter_QueryGraphError(t *testing.T) {
	a := &fakeP9Aggregator{
		queryGraphFn: func(_ context.Context, _ []string, _, _ int) ([]aggregator.QueryResult, error) {
			return nil, errors.New("graph kaboom")
		},
	}
	emb := &fakeP9Embedder{}
	ad := aggregatoradapter.New(a, emb)
	_, err := ad.QueryGraph(context.Background(), []string{"n1"}, 1, 5)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAdapter_QueryFTSHappy(t *testing.T) {
	a := &fakeP9Aggregator{
		queryFTSFn: func(_ context.Context, _ string, _ int) ([]aggregator.QueryResult, error) {
			return []aggregator.QueryResult{
				{NoteID: "n1", Title: "T", Score: 1.5, Snippet: "s", ProjectID: "p", Source: "fts", AuditChainAnchor: "anchor"},
			}, nil
		},
	}
	emb := &fakeP9Embedder{}
	ad := aggregatoradapter.New(a, emb)
	got, err := ad.QueryFTS(context.Background(), "q", 5)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 1 || got[0].AuditChainAnchor != "anchor" {
		t.Errorf("field translation failed: %+v", got)
	}
}

func TestAdapter_QueryVecHappy(t *testing.T) {
	a := &fakeP9Aggregator{
		queryVecFn: func(_ context.Context, _ []float32, _ int, _ float64) ([]aggregator.QueryResult, error) {
			return []aggregator.QueryResult{{NoteID: "n1"}}, nil
		},
	}
	emb := &fakeP9Embedder{}
	ad := aggregatoradapter.New(a, emb)
	got, err := ad.QueryVec(context.Background(), []float32{0.1}, 5, 0.9)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("want 1, got %d", len(got))
	}
}

func TestAdapter_QueryGraphHappy(t *testing.T) {
	a := &fakeP9Aggregator{
		queryGraphFn: func(_ context.Context, _ []string, _, _ int) ([]aggregator.QueryResult, error) {
			return []aggregator.QueryResult{{NoteID: "n1"}}, nil
		},
	}
	emb := &fakeP9Embedder{}
	ad := aggregatoradapter.New(a, emb)
	got, err := ad.QueryGraph(context.Background(), []string{"seed"}, 1, 5)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("want 1, got %d", len(got))
	}
}

func TestAdapter_EmbedderPassthrough(t *testing.T) {
	a := &fakeP9Aggregator{}
	emb := &fakeP9Embedder{
		embedFn: func(_ context.Context, text string) ([]float32, error) {
			if text != "MergeEngine" {
				t.Errorf("text: want MergeEngine, got %q", text)
			}
			return []float32{0.5, 0.5}, nil
		},
		dim: 2,
	}
	ad := aggregatoradapter.New(a, emb)
	got, err := ad.Embedder().Embed(context.Background(), "MergeEngine")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("embedding: want len 2, got %d", len(got))
	}
}

func TestAdapter_NilQuerierPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on nil querier")
		}
	}()
	_ = aggregatoradapter.New(nil, &fakeP9Embedder{})
}

func TestAdapter_NilEmbedderPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on nil embedder")
		}
	}()
	_ = aggregatoradapter.New(&fakeP9Aggregator{}, nil)
}
