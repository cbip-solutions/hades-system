// SPDX-License-Identifier: MIT
package ecosystem

import (
	"context"
)

// Reranker re-orders a candidate list given the query text using a
// cross-encoder model (query+doc seen together; resolves cross-eco
// score-space drift implicitly per Bruch 2023 + ZeroEntropy 2025).
//
// Concurrency implementations MUST be safe for concurrent Rerank calls.
// dispatcher.Query issues one Rerank per query (not parallelized
// per ecosystem — RRF fusion runs BEFORE rerank).
type Reranker interface {
	Rerank(ctx context.Context, query string, candidates []Candidate, topK int) ([]RankedResult, error)

	Close() error
}

type Candidate struct {
	ChunkID         int64
	Ecosystem       Ecosystem
	ContentText     string
	SymbolPath      string
	SourceURL       string
	SimilarityScore float64
}

type RankedResult struct {
	Candidate
	RerankerScore float64
	Rank          int
}

type RerankerConfig struct {
	Model        string
	Backend      string
	APITokenKey  string
	MaxLatencyMs int
}

type NoopReranker struct{}

func (NoopReranker) Rerank(ctx context.Context, _ string, candidates []Candidate, topK int) ([]RankedResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	n := topK
	if n > len(candidates) {
		n = len(candidates)
	}
	out := make([]RankedResult, n)
	for i := 0; i < n; i++ {
		out[i] = RankedResult{
			Candidate:     candidates[i],
			RerankerScore: candidates[i].SimilarityScore,
			Rank:          i + 1,
		}
	}
	return out, nil
}

func (NoopReranker) Close() error { return nil }
