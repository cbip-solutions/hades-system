package embed

import (
	"context"
	"math"
	"testing"
)

func TestMockEmbedderDeterministic(t *testing.T) {
	m := NewMockEmbedder(384)
	ctx := context.Background()
	v1, err := m.Embed(ctx, "hello world")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	v2, err := m.Embed(ctx, "hello world")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(v1) != 384 {
		t.Errorf("dim = %d; want 384", len(v1))
	}
	for i := range v1 {
		if v1[i] != v2[i] {
			t.Errorf("non-deterministic at i=%d: %f vs %f", i, v1[i], v2[i])
		}
	}
}

func TestMockEmbedderDifferentInputDifferentOutput(t *testing.T) {
	m := NewMockEmbedder(384)
	ctx := context.Background()
	v1, _ := m.Embed(ctx, "hello")
	v2, _ := m.Embed(ctx, "world")
	allEqual := true
	for i := range v1 {
		if v1[i] != v2[i] {
			allEqual = false
			break
		}
	}
	if allEqual {
		t.Error("different inputs produced identical embeddings")
	}
}

func TestMockEmbedderDimensions(t *testing.T) {
	m := NewMockEmbedder(512)
	if m.Dimensions() != 512 {
		t.Errorf("Dimensions = %d; want 512", m.Dimensions())
	}
}

func TestMockEmbedderContextCancellation(t *testing.T) {
	m := NewMockEmbedder(384)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := m.Embed(ctx, "hello")
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}

func TestMockEmbedder_TokenSumMode_Deterministic(t *testing.T) {
	m := NewMockEmbedder(384).WithTokenSumMode()
	ctx := context.Background()

	v1, err := m.Embed(ctx, "research caching strategies")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	v2, err := m.Embed(ctx, "research caching strategies")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(v1) != 384 {
		t.Fatalf("dim = %d, want 384", len(v1))
	}
	for i := range v1 {
		if v1[i] != v2[i] {
			t.Fatalf("non-deterministic at index %d: %f vs %f", i, v1[i], v2[i])
		}
	}
}

func TestMockEmbedder_TokenSumMode_L2Normalized(t *testing.T) {
	m := NewMockEmbedder(384).WithTokenSumMode()
	v, _ := m.Embed(context.Background(), "hello world")
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	norm := math.Sqrt(sum)
	if math.Abs(norm-1.0) > 1e-5 {
		t.Errorf("not L2-normalized: norm = %f, want 1.0", norm)
	}
}

func TestMockEmbedder_TokenSumMode_OverlappingTokensCosine(t *testing.T) {
	m := NewMockEmbedder(384).WithTokenSumMode()
	ctx := context.Background()

	v1, _ := m.Embed(ctx, "research caching strategies")
	v2, _ := m.Embed(ctx, "caching strategies for research")
	cos := cosineForMockTest(v1, v2)

	if cos < 0.85 {
		t.Errorf("overlapping-tokens cosine = %f, want >= 0.85", cos)
	}
}

func TestMockEmbedder_TokenSumMode_DisjointCosine(t *testing.T) {
	m := NewMockEmbedder(384).WithTokenSumMode()
	ctx := context.Background()

	v1, _ := m.Embed(ctx, "alpha beta gamma")
	v2, _ := m.Embed(ctx, "delta epsilon zeta")
	cos := cosineForMockTest(v1, v2)
	if cos > 0.5 {
		t.Errorf("disjoint cosine = %f, want < 0.5", cos)
	}
}

func TestMockEmbedder_TokenSumMode_EmptyInput(t *testing.T) {
	m := NewMockEmbedder(384).WithTokenSumMode()
	v, err := m.Embed(context.Background(), "")
	if err != nil {
		t.Fatalf("Embed empty: %v", err)
	}
	if len(v) != 384 {
		t.Errorf("dim = %d, want 384", len(v))
	}
	for i, x := range v {
		if x != 0 {
			t.Errorf("empty input must produce zero vector, got v[%d]=%f", i, x)
			break
		}
	}
}

func TestMockEmbedder_TokenSumMode_Dimensions(t *testing.T) {
	m := NewMockEmbedder(512).WithTokenSumMode()
	if got := m.Dimensions(); got != 512 {
		t.Errorf("Dimensions() = %d, want 512", got)
	}
	v, _ := m.Embed(context.Background(), "test query")
	if len(v) != 512 {
		t.Errorf("vector dim = %d, want 512", len(v))
	}
}

func TestMockEmbedder_TokenSumMode_PunctuationStripped(t *testing.T) {
	m := NewMockEmbedder(384).WithTokenSumMode()
	ctx := context.Background()

	v1, _ := m.Embed(ctx, "hello, world!")
	v2, _ := m.Embed(ctx, "hello world")
	for i := range v1 {
		if math.Abs(float64(v1[i]-v2[i])) > 1e-6 {
			t.Errorf("punctuation should be stripped: differ at index %d (%f vs %f)", i, v1[i], v2[i])
			break
		}
	}
}

func TestMockEmbedder_TokenSumMode_ContextCancellation(t *testing.T) {
	m := NewMockEmbedder(384).WithTokenSumMode()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := m.Embed(ctx, "hello world")
	if err == nil {
		t.Error("expected error for cancelled context (token-sum mode)")
	}
}

func TestMockEmbedder_TokenSumMode_AllPunctuationInputZeroVector(t *testing.T) {

	m := NewMockEmbedder(384).WithTokenSumMode()
	v, err := m.Embed(context.Background(), "!!! ??? ...")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(v) != 384 {
		t.Errorf("dim = %d, want 384", len(v))
	}
	for i, x := range v {
		if x != 0 {
			t.Errorf("all-punctuation input must produce zero vector, got v[%d]=%f", i, x)
			break
		}
	}
}

func cosineForMockTest(a, b []float32) float64 {
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
