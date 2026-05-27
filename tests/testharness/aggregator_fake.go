// SPDX-License-Identifier: MIT
// Package testharness provides shared in-memory mocks + 12
// test suites. AggregatorFake implements augment.KnowledgeIndex +
// augment.Embedder ( narrow seams over the D shipped
// substrate) and lets tests inject canned per-lane results +
// deterministic embeddings.
//
// Canonical contract (verified against internal/augment/types.go):
// - augment.KnowledgeIndex returns []augment.QueryResult (NOT
// []aggregator.QueryResult). The augment package re-declares
// QueryResult as a thin alias to keep its import surface narrow.
// - augment.Embedder is the upstream embedding seam.
// - Tests that want to exercise RRF fusion directly use the
// package-level aggregator.Fuse(perSourceTopKs, k, limit); convert
// augment.QueryResult <-> aggregator.QueryResult via field copy.
//
// Mock contract: AggregatorFake is a stateful mock; tests Seed* +
// Inject* before calling methods. Concurrent access is safe via
// embedded sync.Mutex.
//
// Why a fake (not a stub): integration + property + chaos
// tests need real lane-by-lane behavior (deterministic ordering, error
// injection, per-lane breakdown). A bare stub returning empty would
// not exercise pipeline glue.
package testharness

import (
	"context"
	"sync"

	"github.com/cbip-solutions/hades-system/internal/augment"
)

type AggregatorOp int

const (
	AggregatorOpFTS AggregatorOp = iota
	AggregatorOpVec
	AggregatorOpGraph
	AggregatorOpEmbed
)

type AggregatorFake struct {
	mu sync.Mutex

	ftsResults   []augment.QueryResult
	vecResults   []augment.QueryResult
	graphResults []augment.QueryResult
	embedding    []float32

	injectedErrors map[AggregatorOp]error
	callLog        []AggregatorCall
}

type AggregatorCall struct {
	Op        AggregatorOp
	QueryText string
	Limit     int

	EmbeddingLen int
	Threshold    float64

	SeedNoteIDs []string
	Depth       int
}

func NewAggregatorFake() *AggregatorFake {
	return &AggregatorFake{
		injectedErrors: make(map[AggregatorOp]error),
	}
}

func (f *AggregatorFake) SeedFTSResults(rs []augment.QueryResult) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ftsResults = append([]augment.QueryResult(nil), rs...)
}

func (f *AggregatorFake) SeedVecResults(rs []augment.QueryResult) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.vecResults = append([]augment.QueryResult(nil), rs...)
}

func (f *AggregatorFake) SeedGraphResults(rs []augment.QueryResult) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.graphResults = append([]augment.QueryResult(nil), rs...)
}

func (f *AggregatorFake) SeedEmbedding(v []float32) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.embedding = append([]float32(nil), v...)
}

func (f *AggregatorFake) InjectError(op AggregatorOp, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.injectedErrors[op] = err
}

func (f *AggregatorFake) ClearError(op AggregatorOp) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.injectedErrors, op)
}

func (f *AggregatorFake) Calls() []AggregatorCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]AggregatorCall(nil), f.callLog...)
}

func (f *AggregatorFake) ResetCalls() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.callLog = nil
}

func (f *AggregatorFake) QueryFTS(ctx context.Context, queryText string, limit int) ([]augment.QueryResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.callLog = append(f.callLog, AggregatorCall{Op: AggregatorOpFTS, QueryText: queryText, Limit: limit})
	if err, ok := f.injectedErrors[AggregatorOpFTS]; ok && err != nil {
		return nil, err
	}
	return append([]augment.QueryResult(nil), f.ftsResults...), nil
}

func (f *AggregatorFake) QueryVec(ctx context.Context, queryEmbedding []float32, limit int, threshold float64) ([]augment.QueryResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.callLog = append(f.callLog, AggregatorCall{
		Op:           AggregatorOpVec,
		Limit:        limit,
		EmbeddingLen: len(queryEmbedding),
		Threshold:    threshold,
	})
	if err, ok := f.injectedErrors[AggregatorOpVec]; ok && err != nil {
		return nil, err
	}
	return append([]augment.QueryResult(nil), f.vecResults...), nil
}

func (f *AggregatorFake) QueryGraph(ctx context.Context, seedNoteIDs []string, depth, limit int) ([]augment.QueryResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.callLog = append(f.callLog, AggregatorCall{
		Op:          AggregatorOpGraph,
		Limit:       limit,
		SeedNoteIDs: append([]string(nil), seedNoteIDs...),
		Depth:       depth,
	})
	if err, ok := f.injectedErrors[AggregatorOpGraph]; ok && err != nil {
		return nil, err
	}
	return append([]augment.QueryResult(nil), f.graphResults...), nil
}

func (f *AggregatorFake) Embed(ctx context.Context, text string) ([]float32, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.callLog = append(f.callLog, AggregatorCall{Op: AggregatorOpEmbed, QueryText: text})
	if err, ok := f.injectedErrors[AggregatorOpEmbed]; ok && err != nil {
		return nil, err
	}
	if len(f.embedding) == 0 {
		return []float32{0}, nil
	}
	return append([]float32(nil), f.embedding...), nil
}
