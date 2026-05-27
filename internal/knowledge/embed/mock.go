// SPDX-License-Identifier: MIT
package embed

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"math"
	"math/rand"
	"strings"
)

// MockEmbedder produces deterministic embeddings from text hash. Used for
// unit tests; do NOT use in production (no semantic meaning).
//
// Two algorithms are available:
//
// - Default (sha256+sin): preserves cross-position variation but does NOT
// produce realistic cosine semantics for overlapping tokens. Use when
// tests only care about determinism + dimensionality, not similarity.
//
// - Token-sum (opt-in via WithTokenSumMode): tokenize on whitespace +
// strip non-alphanumeric → per-token sha256-seeded contribution vector
// → sum across tokens → L2-normalize. Two queries that share tokens
// produce vectors with high cosine similarity (≥0.85 for fully-shared
// vocabularies, <0.5 for disjoint). Required by sqlite-vec
// KNN semantic-match unit tests.
//
// Default-mode algorithm: sha256(text) → 8 uint32 seeds; per-dimension value
// derived via math.Sin(seed/100 + i*0.137) for stable cross-position
// variation; output is L2-normalized.
//
// Token-sum-mode algorithm: tokenize(text) → for each token compute
// sha256 → first 8 bytes as int64 seed for math/rand PRNG → produce
// dimensions-dim contribution vector with values in [-1, 1) → sum across
// tokens → L2-normalize via NormalizeL2. Empty input or empty token list
// returns a zero vector (no panic, no error).
type MockEmbedder struct {
	dimensions  int
	useTokenSum bool
}

func NewMockEmbedder(dimensions int) *MockEmbedder {
	return &MockEmbedder{dimensions: dimensions}
}

func (m *MockEmbedder) WithTokenSumMode() *MockEmbedder {
	m.useTokenSum = true
	return m
}

func (m *MockEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if m.useTokenSum {
		return m.embedTokenSum(text), nil
	}
	return m.embedSha256Sin(text), nil
}

func (m *MockEmbedder) embedSha256Sin(text string) []float32 {
	h := sha256.Sum256([]byte(text))
	v := make([]float32, m.dimensions)
	for i := 0; i < m.dimensions; i++ {

		base := (i % 8) * 4
		u := binary.LittleEndian.Uint32(h[base : base+4])

		v[i] = float32(math.Sin(float64(u%1000)/100.0 + float64(i)*0.137))
	}
	return NormalizeL2(v)
}

func (m *MockEmbedder) embedTokenSum(text string) []float32 {
	tokens := tokenizeForEmbed(text)
	if len(tokens) == 0 {
		return make([]float32, m.dimensions)
	}

	sum := make([]float64, m.dimensions)
	for _, tok := range tokens {
		contribution := tokenContributionVector(tok, m.dimensions)
		for i, v := range contribution {
			sum[i] += float64(v)
		}
	}

	var norm float64
	for _, x := range sum {
		norm += x * x
	}
	norm = math.Sqrt(norm)
	if norm == 0 {
		return make([]float32, m.dimensions)
	}

	out := make([]float32, m.dimensions)
	for i, x := range sum {
		out[i] = float32(x / norm)
	}
	return out
}

func (m *MockEmbedder) Dimensions() int { return m.dimensions }

func tokenizeForEmbed(text string) []string {
	lower := strings.ToLower(text)
	fields := strings.Fields(lower)
	out := make([]string, 0, len(fields))
	for _, tok := range fields {
		clean := strings.Map(func(r rune) rune {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
				return r
			}
			return -1
		}, tok)
		if clean != "" {
			out = append(out, clean)
		}
	}
	return out
}

func tokenContributionVector(token string, dim int) []float32 {
	hash := sha256.Sum256([]byte(token))

	seed := int64(binary.BigEndian.Uint64(hash[:8]))
	rng := rand.New(rand.NewSource(seed))

	v := make([]float32, dim)
	for i := 0; i < dim; i++ {
		v[i] = float32(rng.Float64()*2 - 1)
	}
	return v
}
