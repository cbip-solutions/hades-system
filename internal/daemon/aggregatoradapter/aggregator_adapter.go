// SPDX-License-Identifier: MIT
// Package aggregatoradapter bridges release D's *aggregator.Aggregator
// (query surface) + aggregator.Embedder (DI'd at New(opts) time) <->
// internal/augment's KnowledgeIndex + Embedder interfaces.
//
// invariant boundary: this package lives in internal/daemon/ (NOT under
// internal/augment/). The daemon owns the release D lifecycle; the augment
// package consumes the augment.KnowledgeIndex / augment.Embedder interfaces
// only. Compliance test sentinel `aggregatorAdapterBoundarySentinel`
// verifies no internal/store import.
//
// fix (2026-05-10): replaces an earlier broken design where
// `Aggregator` interface incorrectly claimed an `Embed` method. The split
// (KnowledgeIndex.Query{FTS,Vec,Graph} + separate Embedder) reflects release
// D's actual API: *Aggregator has no Embed; Embedder is a Go interface
// dependency-injected at aggregator.New(opts).
package aggregatoradapter

import (
	"context"
	"fmt"

	"github.com/cbip-solutions/hades-system/internal/augment"
	"github.com/cbip-solutions/hades-system/internal/knowledge/aggregator"
)

type p9Querier interface {
	QueryFTS(ctx context.Context, queryText string, limit int) ([]aggregator.QueryResult, error)
	QueryVec(ctx context.Context, queryEmbedding []float32, limit int, threshold float64) ([]aggregator.QueryResult, error)
	QueryGraph(ctx context.Context, seedNoteIDs []string, depth, limit int) ([]aggregator.QueryResult, error)
}

type Adapter struct {
	q   p9Querier
	emb aggregator.Embedder
}

func New(q p9Querier, emb aggregator.Embedder) *Adapter {
	if q == nil || emb == nil {
		panic("aggregatoradapter: New requires non-nil querier + embedder")
	}
	return &Adapter{q: q, emb: emb}
}

func (ad *Adapter) QueryFTS(ctx context.Context, queryText string, limit int) ([]augment.QueryResult, error) {
	rows, err := ad.q.QueryFTS(ctx, queryText, limit)
	if err != nil {
		return nil, fmt.Errorf("aggregatoradapter QueryFTS: %w", err)
	}
	return convertResults(rows), nil
}

func (ad *Adapter) QueryVec(ctx context.Context, queryEmbedding []float32, limit int, threshold float64) ([]augment.QueryResult, error) {
	rows, err := ad.q.QueryVec(ctx, queryEmbedding, limit, threshold)
	if err != nil {
		return nil, fmt.Errorf("aggregatoradapter QueryVec: %w", err)
	}
	return convertResults(rows), nil
}

func (ad *Adapter) QueryGraph(ctx context.Context, seedNoteIDs []string, depth, limit int) ([]augment.QueryResult, error) {
	rows, err := ad.q.QueryGraph(ctx, seedNoteIDs, depth, limit)
	if err != nil {
		return nil, fmt.Errorf("aggregatoradapter QueryGraph: %w", err)
	}
	return convertResults(rows), nil
}

type embedderShim struct {
	inner aggregator.Embedder
}

func (e *embedderShim) Embed(ctx context.Context, text string) ([]float32, error) {
	return e.inner.Embed(ctx, text)
}

func (ad *Adapter) Embedder() augment.Embedder {
	return &embedderShim{inner: ad.emb}
}

func convertResults(in []aggregator.QueryResult) []augment.QueryResult {
	out := make([]augment.QueryResult, len(in))
	for i, r := range in {
		out[i] = augment.QueryResult{
			NoteID:           r.NoteID,
			Title:            r.Title,
			Snippet:          r.Snippet,
			ProjectID:        r.ProjectID,
			AuditChainAnchor: r.AuditChainAnchor,
			Source:           r.Source,
			Score:            r.Score,
		}
	}
	return out
}
