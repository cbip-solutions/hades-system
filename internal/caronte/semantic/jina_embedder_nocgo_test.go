// go:build !cgo || nocgo
//go:build !cgo || nocgo
// +build !cgo nocgo

package semantic

import (
	"context"
	"errors"
	"testing"
)

func TestNewJinaEmbedder_NoCGO_ReturnsModelMissing(t *testing.T) {
	_, err := NewJinaEmbedder(EmbedderConfig{JinaModelPath: "/anything"})
	if !errors.Is(err, ErrJinaModelMissing) {
		t.Errorf("NewJinaEmbedder (nocgo stub) err = %v; want ErrJinaModelMissing", err)
	}
}

func TestBM25OnlyNoopEmbedder_NoCGO_ReturnsSentinel(t *testing.T) {
	emb := NewBM25OnlyEmbedder()
	if emb == nil {
		t.Fatal("NewBM25OnlyEmbedder (nocgo stub) returned nil")
	}
	if _, err := emb.Embed(context.Background(), "anything"); !errors.Is(err, ErrEmbedderUnavailable) {
		t.Errorf("BM25-only Embed (nocgo) err = %v; want ErrEmbedderUnavailable", err)
	}
}

func TestDefaultSelectorChain_NoCGO_FullDegrade(t *testing.T) {
	sel := NewDefaultSelector()
	emb, mode, err := sel.Select(context.Background(), EmbedderConfig{Mode: "auto"})
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if mode != EmbedderBM25Only {
		t.Errorf("mode = %q; want bm25-only (nocgo always degrades)", mode)
	}
	if emb == nil {
		t.Fatal("Select returned nil embedder under nocgo")
	}
}
