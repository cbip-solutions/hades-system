package caronteembedadapter

import (
	"context"
	"errors"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/caronte/intent"
	"github.com/cbip-solutions/hades-system/internal/research/ecosystem"
)

type fakeJina struct{ dim int }

func (f fakeJina) EmbedFP32_1536d(_ context.Context, text string) ([]float32, error) {
	v := make([]float32, f.dim)
	if f.dim > 0 {
		v[len(text)%f.dim] = 1.0
	}
	return v, nil
}

func TestEmbedderBridgeDimensions(t *testing.T) {
	var emb intent.CodeEmbedder = NewEmbedder(fakeJina{dim: 1536})
	if emb.Dimensions() != 1536 {
		t.Errorf("Dimensions() = %d; want 1536", emb.Dimensions())
	}
	v, err := emb.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(v) != 1536 {
		t.Errorf("Embed len = %d; want 1536", len(v))
	}
}

type fakeBGE struct{}

func (fakeBGE) Rerank(_ context.Context, _ string, candidates []ecosystem.Candidate, topK int) ([]ecosystem.RankedResult, error) {
	out := make([]ecosystem.RankedResult, 0, len(candidates))
	for i := len(candidates) - 1; i >= 0; i-- {
		out = append(out, ecosystem.RankedResult{Candidate: candidates[i], RerankerScore: float64(len(candidates) - i), Rank: len(candidates) - 1 - i})
	}
	if topK > 0 && len(out) > topK {
		out = out[:topK]
	}
	return out, nil
}

func TestRerankerBridgeMapsPassages(t *testing.T) {
	var rr intent.Reranker = NewReranker(fakeBGE{})
	in := []intent.SemanticPassage{
		{SourceID: "a", SourceKind: "adr", Text: "alpha", Score: 0.1},
		{SourceID: "b", SourceKind: "code", Text: "bravo", Score: 0.2},
	}
	out, err := rr.Rerank(context.Background(), "q", in)
	if err != nil {
		t.Fatalf("Rerank: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("Rerank len = %d; want 2", len(out))
	}

	if out[0].SourceID != "b" {
		t.Errorf("Rerank[0].SourceID = %q; want b (reranker order)", out[0].SourceID)
	}

	if out[0].Score == 0.2 {
		t.Errorf("Rerank[0].Score = %v; want the reranker score, not the input KNN score", out[0].Score)
	}

	if out[0].SourceKind != "code" || out[0].Text != "bravo" {
		t.Errorf("Rerank[0] lost fields: %+v", out[0])
	}
}

type errBGE struct{}

func (errBGE) Rerank(_ context.Context, _ string, _ []ecosystem.Candidate, _ int) ([]ecosystem.RankedResult, error) {
	return nil, errors.New("backend failure")
}

func TestRerankerBridgeErrorPath(t *testing.T) {
	rr := NewReranker(errBGE{})
	in := []intent.SemanticPassage{
		{SourceID: "x", SourceKind: "adr", Text: "text", Score: 0.5},
	}
	_, err := rr.Rerank(context.Background(), "q", in)
	if err == nil {
		t.Fatal("expected error from errBGE; got nil")
	}
}

type errJina struct{}

func (errJina) EmbedFP32_1536d(_ context.Context, _ string) ([]float32, error) {
	return nil, errors.New("jina failure")
}

func TestEmbedderBridgeErrorPath(t *testing.T) {
	emb := NewEmbedder(errJina{})
	_, err := emb.Embed(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected error from errJina; got nil")
	}
}

func TestRerankerBridgeEmptyPassages(t *testing.T) {
	rr := NewReranker(fakeBGE{})
	out, err := rr.Rerank(context.Background(), "q", nil)
	if err != nil {
		t.Fatalf("Rerank nil: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("Rerank nil len = %d; want 0", len(out))
	}
}

func TestRerankerBridgeSinglePassage(t *testing.T) {
	rr := NewReranker(fakeBGE{})
	in := []intent.SemanticPassage{
		{SourceID: "src-1", SourceKind: "spec", Text: "some text", Score: 0.5},
	}
	out, err := rr.Rerank(context.Background(), "query", in)
	if err != nil {
		t.Fatalf("Rerank: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("Rerank len = %d; want 1", len(out))
	}
	if out[0].SourceID != "src-1" {
		t.Errorf("SourceID = %q; want src-1", out[0].SourceID)
	}
	if out[0].SourceKind != "spec" {
		t.Errorf("SourceKind = %q; want spec", out[0].SourceKind)
	}
	if out[0].Text != "some text" {
		t.Errorf("Text = %q; want some text", out[0].Text)
	}
	if out[0].Score == 0.5 {
		t.Errorf("Score = 0.5; want the reranker score, not the KNN score")
	}
}

type oobBGE struct{}

func (oobBGE) Rerank(_ context.Context, _ string, candidates []ecosystem.Candidate, _ int) ([]ecosystem.RankedResult, error) {
	out := make([]ecosystem.RankedResult, len(candidates))
	for i, c := range candidates {

		c.ChunkID = int64(len(candidates) + 99)
		out[i] = ecosystem.RankedResult{Candidate: c, RerankerScore: float64(i + 1), Rank: i + 1}
	}
	return out, nil
}

func TestRerankerBridgeOutOfRangeChunkID(t *testing.T) {
	rr := NewReranker(oobBGE{})
	in := []intent.SemanticPassage{
		{SourceID: "sym-1", SourceKind: "adr", Text: "text-1", Score: 0.5},
	}
	out, err := rr.Rerank(context.Background(), "q", in)
	if err != nil {
		t.Fatalf("Rerank: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("Rerank len = %d; want 1", len(out))
	}

	if out[0].SourceKind != "" {
		t.Errorf("SourceKind = %q; want empty string (defensive fallback, SourceKind unavailable)", out[0].SourceKind)
	}

	if out[0].SourceID != "sym-1" {
		t.Errorf("SourceID = %q; want sym-1", out[0].SourceID)
	}
}

var _ intent.CodeEmbedder = (*embedderBridge)(nil)
var _ intent.Reranker = (*rerankerBridge)(nil)
