//go:build cgo && !nocgo
// +build cgo,!nocgo

// SPDX-License-Identifier: MIT

package semantic

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"sync"

	"github.com/cbip-solutions/hades-system/internal/caronte/intent"
)

type EmbedderMode string

const (
	EmbedderJinaLocal EmbedderMode = "jina-local"

	EmbedderEcosystemMCP EmbedderMode = "ecosystem-mcp"

	EmbedderBM25Only EmbedderMode = "bm25-only"
)

var ErrEmbedderUnavailable = errors.New("caronte/semantic: embedder unavailable (BM25-only degrade)")

var ErrJinaModelMissing = errors.New("caronte/semantic: jina-code ONNX model not found")

var ErrONNXRuntimeUnavailable = errors.New("caronte/semantic: ONNX runtime not linked (build with -tags onnx + libonnxruntime installed)")

type onnxSession interface {
	Run(ctx context.Context, inputIDs []int64) ([]float32, error)

	Close() error
}

type tokenizer interface {
	// Encode returns the token IDs for the input string. MUST be safe for
	// concurrent calls (the embedder fans out across goroutines).
	Encode(text string) ([]int64, error)

	Close() error
}

var newONNXSession = func(_ string) (onnxSession, error) {
	return nil, ErrONNXRuntimeUnavailable
}

var newTokenizer = func(_ string) (tokenizer, error) {
	return nil, ErrONNXRuntimeUnavailable
}

type JinaEmbedder struct {
	modelPath string

	session   onnxSession
	tokenizer tokenizer

	mu     sync.RWMutex
	closed bool
}

func NewJinaEmbedder(cfg EmbedderConfig) (*JinaEmbedder, error) {
	if cfg.JinaModelPath == "" {

		return nil, ErrJinaModelMissing
	}
	if _, err := os.Stat(cfg.JinaModelPath); err != nil {
		if os.IsNotExist(err) {
			return nil, ErrJinaModelMissing
		}

		return nil, fmt.Errorf("caronte/semantic: NewJinaEmbedder stat %s: %w", cfg.JinaModelPath, err)
	}

	sess, err := newONNXSession(cfg.JinaModelPath)
	if err != nil {
		return nil, err
	}
	tok, err := newTokenizer(cfg.JinaModelPath)
	if err != nil {
		_ = sess.Close()
		return nil, err
	}
	return &JinaEmbedder{
		modelPath: cfg.JinaModelPath,
		session:   sess,
		tokenizer: tok,
	}, nil
}

func (j *JinaEmbedder) Dimensions() int { return 1536 }

func (j *JinaEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	j.mu.RLock()
	defer j.mu.RUnlock()
	if j.closed {
		return nil, errors.New("caronte/semantic: JinaEmbedder closed")
	}

	ids, err := j.tokenizer.Encode(text)
	if err != nil {
		return nil, fmt.Errorf("caronte/semantic: tokenize: %w", err)
	}
	if len(ids) == 0 {

		return make([]float32, 1536), nil
	}
	raw, err := j.session.Run(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("caronte/semantic: session run: %w", err)
	}
	if len(raw) != 1536 {
		return nil, fmt.Errorf("caronte/semantic: session returned %d dims; want 1536", len(raw))
	}
	return l2Normalize(raw), nil
}

func (j *JinaEmbedder) Close() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed {
		return nil
	}
	j.closed = true
	var firstErr error
	if j.session != nil {
		if err := j.session.Close(); err != nil {
			firstErr = err
		}
	}
	if j.tokenizer != nil {
		if err := j.tokenizer.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// inv-hades-274 compile anchor — *JinaEmbedder MUST satisfy intent.CodeEmbedder.
// If this stops compiling, the selector cannot wire it into NewSemanticIndexer
// and the engine will surface "wiring bug" at boot. The test file
// (jina_embedder_test.go) hosts the runtime double-check.
var _ intent.CodeEmbedder = (*JinaEmbedder)(nil)

func l2Normalize(v []float32) []float32 {
	var sumSq float64
	for _, x := range v {
		sumSq += float64(x) * float64(x)
	}
	if sumSq == 0 {
		return v
	}
	inv := float32(1.0 / math.Sqrt(sumSq))
	for i := range v {
		v[i] *= inv
	}
	return v
}

type bm25OnlyEmbedder struct{}

// NewBM25OnlyEmbedder returns the BM25-only sentinel. The returned value
// satisfies intent.CodeEmbedder but Embed always errors with
// ErrEmbedderUnavailable; callers MUST detect via errors.Is and short-circuit
// to BM25 retrieval instead of treating the error as a hard failure.
func NewBM25OnlyEmbedder() intent.CodeEmbedder { return bm25OnlyEmbedder{} }

func (bm25OnlyEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	return nil, ErrEmbedderUnavailable
}

func (bm25OnlyEmbedder) Dimensions() int { return 1536 }

// inv-hades-274 compile anchor — the BM25-only sentinel MUST satisfy
// intent.CodeEmbedder so the selector can return it from Select without an
// extra adapter layer.
var _ intent.CodeEmbedder = bm25OnlyEmbedder{}
