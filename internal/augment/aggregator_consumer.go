// SPDX-License-Identifier: MIT
// Package augment — AggregatorConsumer wraps HADES design D's
// internal/knowledge/aggregator query surface (via the KnowledgeIndex +
// Embedder seams) for single-project augmentation.
//
// design choice=α:
// methods + a package-level aggregator.Fuse function + aggregator.Embedder
// interface. fix: the wrapper consumes the *Aggregator query surface
// via the KnowledgeIndex seam; the Embedder is a separate seam.
//
// Method mapping (5-lane RRF design choice):
// - Lane 2 (FTS5 BM25): AggregatorConsumer.Lane2FTS -> KnowledgeIndex.QueryFTS
// - Lane 4 (sqlite-vec KNN): AggregatorConsumer.Lane4Vec -> Embedder.Embed + KnowledgeIndex.QueryVec
// - Lane 5 (temporal): AggregatorConsumer.Lane5Temporal -> KnowledgeIndex.QueryFTS + decay
// - RRF k=60 fusion: AggregatorConsumer.RunRRF -> rrf.Fuse (canonical CGO-free)
//
// Lanes 1 + 3 (caronte.query / caronte.context) are NOT served by this
// consumer — they go through Pipeline's McpGateway.CallTool.
//
// invariant: this file imports github.com/cbip-solutions/hades-system/internal/knowledge/rrf
//
// but NOT internal/knowledge/aggregator (which transitively pulls CGO sqlite3)
// and NOT internal/store.

package augment

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/cbip-solutions/hades-system/internal/knowledge/rrf"
)

const VecSimilarityThreshold = 0.92

const TemporalDecayHalfLifeDays = 7.0

func NewAggregatorConsumer(index KnowledgeIndex, embedder Embedder) *AggregatorConsumer {
	return &AggregatorConsumer{index: index, embedder: embedder}
}

func (c *AggregatorConsumer) Lane2FTS(ctx context.Context, queryText string, limit int) (Lane2Result, error) {
	start := time.Now()
	results, err := c.index.QueryFTS(ctx, queryText, limit)
	if err != nil {
		return Lane2Result{LaneID: 2}, fmt.Errorf("aggregator_consumer: Lane2FTS: %w", err)
	}
	return Lane2Result{
		Results:   results,
		ElapsedMs: time.Since(start).Milliseconds(),
		LaneID:    2,
	}, nil
}

func (c *AggregatorConsumer) Lane4Vec(ctx context.Context, queryText string, limit int, threshold float64) (Lane4Result, error) {
	start := time.Now()

	if threshold <= 0 {
		threshold = VecSimilarityThreshold
	}

	embedding, err := c.embedder.Embed(ctx, queryText)
	if err != nil {

		return Lane4Result{
			Results:   nil,
			ElapsedMs: time.Since(start).Milliseconds(),
			LaneID:    4,
			Degraded:  true,
		}, nil
	}
	if len(embedding) == 0 {
		return Lane4Result{
			Results:   nil,
			ElapsedMs: time.Since(start).Milliseconds(),
			LaneID:    4,
			Degraded:  true,
		}, nil
	}

	results, err := c.index.QueryVec(ctx, embedding, limit, threshold)
	if err != nil {
		if isVecUnavailableErr(err) {
			return Lane4Result{
				Results:   nil,
				ElapsedMs: time.Since(start).Milliseconds(),
				LaneID:    4,
				Degraded:  true,
			}, nil
		}
		return Lane4Result{LaneID: 4}, fmt.Errorf("aggregator_consumer: Lane4Vec: %w", err)
	}

	return Lane4Result{
		Results:   results,
		ElapsedMs: time.Since(start).Milliseconds(),
		LaneID:    4,
		Degraded:  false,
	}, nil
}

func isVecUnavailableErr(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "sqlite-vec unavailable")
}

func (c *AggregatorConsumer) Lane5Temporal(ctx context.Context, queryText string, sinceTS time.Time, limit int) (Lane5Result, error) {
	start := time.Now()
	results, err := c.index.QueryFTS(ctx, queryText, limit)
	if err != nil {
		return Lane5Result{LaneID: 5}, fmt.Errorf("aggregator_consumer: Lane5Temporal QueryFTS: %w", err)
	}

	now := time.Now()
	filtered := make([]QueryResult, 0, len(results))
	for _, r := range results {
		decay := 1.0
		if anchorPartition := parsePartitionFromAnchor(r.AuditChainAnchor); anchorPartition != "" {
			if anchorTime, ok := partitionToTime(anchorPartition); ok {
				if !sinceTS.IsZero() && anchorTime.Before(sinceTS) {
					continue
				}
				ageDays := now.Sub(anchorTime).Hours() / 24
				if ageDays > 0 {
					decay = math.Pow(0.5, ageDays/TemporalDecayHalfLifeDays)
				}
			}
		}
		r.Score *= decay
		r.Source = "temporal"
		filtered = append(filtered, r)
	}

	return Lane5Result{
		Results:   filtered,
		ElapsedMs: time.Since(start).Milliseconds(),
		LaneID:    5,
	}, nil
}

func parsePartitionFromAnchor(anchor string) string {
	if anchor == "" {
		return ""
	}
	idx := strings.Index(anchor, ":")
	if idx <= 0 {
		return ""
	}
	return anchor[:idx]
}

func partitionToTime(partition string) (time.Time, bool) {
	if len(partition) != 7 || partition[4] != '_' {
		return time.Time{}, false
	}
	yearStr := partition[:4]
	monthStr := partition[5:]
	year, err := atoiSimple(yearStr)
	if err != nil {
		return time.Time{}, false
	}
	month, err := atoiSimple(monthStr)
	if err != nil || month < 1 || month > 12 {
		return time.Time{}, false
	}
	return time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC), true
}

func atoiSimple(s string) (int, error) {
	if s == "" {
		return 0, errors.New("empty")
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("non-digit %q", c)
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}

func (c *AggregatorConsumer) RunRRF(_ context.Context, lanes []TopK, limit int) []QueryResult {
	if len(lanes) == 0 {
		return nil
	}

	in := make([]rrf.TopK, len(lanes))
	for i, lane := range lanes {
		in[i] = rrf.TopK{
			Source:  lane.Source,
			Results: laneResultsToRRF(lane.Results),
		}
	}
	out := rrf.Fuse(in, rrf.DefaultK, limit)
	return rrfResultsToLane(out)
}

func laneResultsToRRF(in []QueryResult) []rrf.QueryResult {
	out := make([]rrf.QueryResult, len(in))
	for i, r := range in {
		out[i] = rrf.QueryResult{
			NoteID:           r.NoteID,
			Score:            r.Score,
			Title:            r.Title,
			Snippet:          r.Snippet,
			ProjectID:        r.ProjectID,
			AuditChainAnchor: r.AuditChainAnchor,
			Source:           r.Source,
		}
	}
	return out
}

func rrfResultsToLane(in []rrf.QueryResult) []QueryResult {
	out := make([]QueryResult, len(in))
	for i, r := range in {
		out[i] = QueryResult{
			NoteID:           r.NoteID,
			Score:            r.Score,
			Title:            r.Title,
			Snippet:          r.Snippet,
			ProjectID:        r.ProjectID,
			AuditChainAnchor: r.AuditChainAnchor,
			Source:           r.Source,
		}
	}
	return out
}
