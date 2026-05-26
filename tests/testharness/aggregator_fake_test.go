package testharness_test

import (
	"context"
	"errors"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/augment"
	"github.com/cbip-solutions/hades-system/internal/knowledge/aggregator"
	"github.com/cbip-solutions/hades-system/tests/testharness"
)

func TestAggregatorFake_ImplementsKnowledgeIndex(t *testing.T) {
	var _ augment.KnowledgeIndex = (*testharness.AggregatorFake)(nil)
}

func TestAggregatorFake_ImplementsEmbedder(t *testing.T) {
	var _ augment.Embedder = (*testharness.AggregatorFake)(nil)
}

func TestAggregatorFake_LaneRoundtrip(t *testing.T) {
	fake := testharness.NewAggregatorFake()
	fake.SeedFTSResults([]augment.QueryResult{
		{NoteID: "fts-1", Score: 0.9, ProjectID: "p-test", Source: "fts"},
	})
	fake.SeedVecResults([]augment.QueryResult{
		{NoteID: "vec-1", Score: 0.85, ProjectID: "p-test", Source: "vec"},
	})
	fake.SeedGraphResults([]augment.QueryResult{
		{NoteID: "graph-1", Score: 0.7, ProjectID: "p-test", Source: "graph"},
	})

	ctx := context.Background()

	if r, err := fake.QueryFTS(ctx, "x", 10); err != nil || len(r) != 1 {
		t.Errorf("QueryFTS: err=%v len=%d want 1", err, len(r))
	}
	if r, err := fake.QueryVec(ctx, []float32{0.1, 0.2, 0.3}, 10, 0.92); err != nil || len(r) != 1 {
		t.Errorf("QueryVec: err=%v len=%d want 1", err, len(r))
	}
	if r, err := fake.QueryGraph(ctx, []string{"seed-1"}, 2, 10); err != nil || len(r) != 1 {
		t.Errorf("QueryGraph: err=%v len=%d want 1", err, len(r))
	}
}

func TestAggregatorFake_FuseDirectInvocation(t *testing.T) {
	fake := testharness.NewAggregatorFake()
	fake.SeedFTSResults([]augment.QueryResult{
		{NoteID: "n1", Score: 5.0, Title: "t1", ProjectID: "p", Source: "fts"},
		{NoteID: "n2", Score: 4.0, Title: "t2", ProjectID: "p", Source: "fts"},
	})
	fake.SeedVecResults([]augment.QueryResult{
		{NoteID: "n2", Score: 0.9, Title: "t2", ProjectID: "p", Source: "vec"},
		{NoteID: "n3", Score: 0.7, Title: "t3", ProjectID: "p", Source: "vec"},
	})

	ctx := context.Background()
	ftsRes, _ := fake.QueryFTS(ctx, "q", 10)
	vecRes, _ := fake.QueryVec(ctx, []float32{0.1}, 10, 0.92)

	toAgg := func(rs []augment.QueryResult) []aggregator.QueryResult {
		out := make([]aggregator.QueryResult, len(rs))
		for i, r := range rs {
			out[i] = aggregator.QueryResult{
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

	topKs := []aggregator.TopK{
		{Source: "fts", Results: toAgg(ftsRes)},
		{Source: "vec", Results: toAgg(vecRes)},
	}
	fused := aggregator.Fuse(topKs, 60, 10)
	if len(fused) != 3 {
		t.Fatalf("Fuse returned %d results, want 3 (deduped union)", len(fused))
	}

	if fused[0].NoteID != "n2" {
		t.Errorf("fused[0].NoteID = %q, want n2 (multi-lane dedup wins)", fused[0].NoteID)
	}
}

func TestAggregatorFake_EmbedRoundtrip(t *testing.T) {
	fake := testharness.NewAggregatorFake()
	fake.SeedEmbedding([]float32{0.1, 0.2, 0.3, 0.4})

	got, err := fake.Embed(context.Background(), "any text")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(got) != 4 || got[0] != 0.1 {
		t.Errorf("Embed returned %v, want [0.1, 0.2, 0.3, 0.4]", got)
	}
}

func TestAggregatorFake_ErrorInjection(t *testing.T) {
	fake := testharness.NewAggregatorFake()
	injected := errors.New("chaos: knowledge index unavailable")
	fake.InjectError(testharness.AggregatorOpFTS, injected)

	if _, err := fake.QueryFTS(context.Background(), "x", 10); err == nil {
		t.Fatal("expected injected error from QueryFTS, got nil")
	}
}

func TestAggregatorFake_CallLog(t *testing.T) {
	fake := testharness.NewAggregatorFake()

	ctx := context.Background()
	_, _ = fake.QueryFTS(ctx, "foo", 5)
	_, _ = fake.QueryVec(ctx, []float32{0.1, 0.2}, 7, 0.9)
	_, _ = fake.QueryGraph(ctx, []string{"s1", "s2"}, 3, 8)
	_, _ = fake.Embed(ctx, "txt")

	calls := fake.Calls()
	if len(calls) != 4 {
		t.Fatalf("Calls() len = %d, want 4", len(calls))
	}
	if calls[0].Op != testharness.AggregatorOpFTS || calls[0].QueryText != "foo" || calls[0].Limit != 5 {
		t.Errorf("calls[0] = %+v", calls[0])
	}
	if calls[1].Op != testharness.AggregatorOpVec || calls[1].EmbeddingLen != 2 || calls[1].Threshold != 0.9 {
		t.Errorf("calls[1] = %+v", calls[1])
	}
	if calls[2].Op != testharness.AggregatorOpGraph || len(calls[2].SeedNoteIDs) != 2 || calls[2].Depth != 3 {
		t.Errorf("calls[2] = %+v", calls[2])
	}
	if calls[3].Op != testharness.AggregatorOpEmbed || calls[3].QueryText != "txt" {
		t.Errorf("calls[3] = %+v", calls[3])
	}

	fake.ResetCalls()
	if len(fake.Calls()) != 0 {
		t.Errorf("after ResetCalls, len = %d, want 0", len(fake.Calls()))
	}
}

func TestAggregatorFake_ClearError(t *testing.T) {
	fake := testharness.NewAggregatorFake()
	fake.InjectError(testharness.AggregatorOpFTS, errors.New("boom"))
	if _, err := fake.QueryFTS(context.Background(), "x", 10); err == nil {
		t.Fatal("expected injected error")
	}
	fake.ClearError(testharness.AggregatorOpFTS)
	if _, err := fake.QueryFTS(context.Background(), "x", 10); err != nil {
		t.Errorf("after ClearError, QueryFTS err = %v, want nil", err)
	}
}
