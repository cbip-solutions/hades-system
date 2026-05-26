package embed

import (
	"context"
	"math"
	"testing"
)

func TestCPUEmbedderProducesNormalizedVector(t *testing.T) {
	e, err := NewCPUEmbedder(CPUOptions{Dimensions: 384, Model: "gte-small-placeholder"})
	if err != nil {
		t.Fatalf("NewCPUEmbedder: %v", err)
	}
	v, err := e.Embed(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(v) != 384 {
		t.Errorf("dim = %d; want 384", len(v))
	}
	mag := 0.0
	for _, x := range v {
		mag += float64(x * x)
	}
	mag = math.Sqrt(mag)
	if math.Abs(mag-1.0) > 1e-3 {
		t.Errorf("output not L2-normalized: |v| = %f; want 1.0", mag)
	}
}

func TestCPUEmbedderDeterministic(t *testing.T) {
	e, _ := NewCPUEmbedder(CPUOptions{Dimensions: 384, Model: "gte-small-placeholder"})
	ctx := context.Background()
	v1, _ := e.Embed(ctx, "doctrine bundle TOML")
	v2, _ := e.Embed(ctx, "doctrine bundle TOML")
	for i := range v1 {
		if v1[i] != v2[i] {
			t.Errorf("non-deterministic at i=%d", i)
			break
		}
	}
}

func TestCPUEmbedderEmptyText(t *testing.T) {
	e, _ := NewCPUEmbedder(CPUOptions{Dimensions: 384, Model: "gte-small-placeholder"})
	v, err := e.Embed(context.Background(), "")
	if err != nil {
		t.Fatalf("Embed empty: %v", err)
	}
	if len(v) != 384 {
		t.Errorf("dim = %d; want 384 even for empty text", len(v))
	}
}

func TestCPUEmbedderDimensionsAndClose(t *testing.T) {
	e, err := NewCPUEmbedder(CPUOptions{Dimensions: 128, Model: "gte-small-placeholder"})
	if err != nil {
		t.Fatalf("NewCPUEmbedder: %v", err)
	}
	if e.Dimensions() != 128 {
		t.Errorf("Dimensions = %d; want 128", e.Dimensions())
	}
	if err := e.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestCPUEmbedderInvalidDimensionsReturnsError(t *testing.T) {
	_, err := NewCPUEmbedder(CPUOptions{Dimensions: 0})
	if err == nil {
		t.Error("NewCPUEmbedder with Dimensions=0 should return error")
	}
}

func TestCPUEmbedderContextCancellation(t *testing.T) {
	e, _ := NewCPUEmbedder(CPUOptions{Dimensions: 384})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := e.Embed(ctx, "hello")
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}
