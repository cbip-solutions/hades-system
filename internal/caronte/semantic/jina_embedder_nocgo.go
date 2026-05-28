//go:build !cgo || nocgo
// +build !cgo nocgo

// SPDX-License-Identifier: MIT

package semantic

import (
	"context"
	"errors"
	"log/slog"

	"github.com/cbip-solutions/hades-system/internal/caronte/intent"
)

type EmbedderMode string

const (
	EmbedderJinaLocal    EmbedderMode = "jina-local"
	EmbedderEcosystemMCP EmbedderMode = "ecosystem-mcp"
	EmbedderBM25Only     EmbedderMode = "bm25-only"
)

var ErrEmbedderUnavailable = errors.New("caronte/semantic: embedder unavailable (BM25-only degrade)")

var ErrJinaModelMissing = errors.New("caronte/semantic: jina-code ONNX model not found")

var ErrONNXRuntimeUnavailable = errors.New("caronte/semantic: ONNX runtime not linked (CGO_ENABLED=0 or -tags nocgo)")

type JinaEmbedder struct{}

func NewJinaEmbedder(_ EmbedderConfig) (*JinaEmbedder, error) {
	return nil, ErrJinaModelMissing
}

func (*JinaEmbedder) Dimensions() int { return 1536 }

func (*JinaEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	return nil, ErrEmbedderUnavailable
}

func (*JinaEmbedder) Close() error { return nil }

var _ intent.CodeEmbedder = (*JinaEmbedder)(nil)

type bm25OnlyEmbedder struct{}

func NewBM25OnlyEmbedder() intent.CodeEmbedder { return bm25OnlyEmbedder{} }

func (bm25OnlyEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	return nil, ErrEmbedderUnavailable
}

func (bm25OnlyEmbedder) Dimensions() int { return 1536 }

var _ intent.CodeEmbedder = bm25OnlyEmbedder{}

type Selector interface {
	Select(ctx context.Context, cfg EmbedderConfig) (intent.CodeEmbedder, EmbedderMode, error)
}

type defaultSelector struct {
	logger *slog.Logger
}

func NewDefaultSelector() Selector {
	return &defaultSelector{logger: slog.Default()}
}

func NewDefaultSelectorWithLogger(logger *slog.Logger) Selector {
	if logger == nil {
		logger = slog.Default()
	}
	return &defaultSelector{logger: logger}
}

func NewDefaultSelectorWithProber(logger *slog.Logger, _ func(ctx context.Context, endpoint string) error) Selector {
	if logger == nil {
		logger = slog.Default()
	}
	return &defaultSelector{logger: logger}
}

var ErrEcosystemMCPProbeNotWired = errors.New("caronte/semantic: ecosystem-mcp http probe unavailable (composition-root injection required for http endpoints)")

func (s *defaultSelector) Select(ctx context.Context, cfg EmbedderConfig) (intent.CodeEmbedder, EmbedderMode, error) {
	if err := cfg.Validate(); err != nil {
		return nil, "", err
	}
	if cfg.Mode != "bm25-only" {
		s.logger.WarnContext(ctx, "caronte.embedder degraded to bm25-only (CGO_ENABLED=0 or -tags nocgo)")
	}
	return NewBM25OnlyEmbedder(), EmbedderBM25Only, nil
}
