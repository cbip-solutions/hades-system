//go:build replay

package p11_augment_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/augment"
	"github.com/cbip-solutions/hades-system/internal/knowledge/aggregator"
	"github.com/cbip-solutions/hades-system/tests/testharness"
)

func TestReplay_AugmentLaneDeterministic(t *testing.T) {
	fake := testharness.NewAggregatorFake()
	fake.SeedFTSResults([]augment.QueryResult{
		{NoteID: "n-fts-1", Score: 0.9, Source: "fts", ProjectID: "p", Title: "alpha"},
		{NoteID: "n-fts-2", Score: 0.7, Source: "fts", ProjectID: "p", Title: "beta"},
	})
	fake.SeedVecResults([]augment.QueryResult{
		{NoteID: "n-vec-1", Score: 0.85, Source: "vec", ProjectID: "p", Title: "gamma"},
	})
	fake.SeedEmbedding([]float32{0.1, 0.2, 0.3})

	c := augment.NewAggregatorConsumer(fake, fake)
	ctx := context.Background()

	r1Fts, err := c.Lane2FTS(ctx, "needle", 10)
	if err != nil {
		t.Fatalf("run1 Lane2FTS: %v", err)
	}
	r1Vec, err := c.Lane4Vec(ctx, "needle", 10, 0.92)
	if err != nil {
		t.Fatalf("run1 Lane4Vec: %v", err)
	}

	r2Fts, err := c.Lane2FTS(ctx, "needle", 10)
	if err != nil {
		t.Fatalf("run2 Lane2FTS: %v", err)
	}
	r2Vec, err := c.Lane4Vec(ctx, "needle", 10, 0.92)
	if err != nil {
		t.Fatalf("run2 Lane4Vec: %v", err)
	}

	if !reflect.DeepEqual(r1Fts.Results, r2Fts.Results) {
		t.Errorf("Lane2FTS not deterministic:\nrun1=%+v\nrun2=%+v", r1Fts.Results, r2Fts.Results)
	}
	if r1Fts.LaneID != r2Fts.LaneID {
		t.Errorf("LaneID drift: run1=%d run2=%d", r1Fts.LaneID, r2Fts.LaneID)
	}
	if !reflect.DeepEqual(r1Vec.Results, r2Vec.Results) {
		t.Errorf("Lane4Vec not deterministic:\nrun1=%+v\nrun2=%+v", r1Vec.Results, r2Vec.Results)
	}
}

func TestReplay_RRFFusionDeterministic(t *testing.T) {
	topKs := []aggregator.TopK{
		{
			Source: "fts",
			Results: []aggregator.QueryResult{
				{NoteID: "a", Score: 5.0, Source: "fts", ProjectID: "p", Title: "A"},
				{NoteID: "b", Score: 4.0, Source: "fts", ProjectID: "p", Title: "B"},
				{NoteID: "c", Score: 3.0, Source: "fts", ProjectID: "p", Title: "C"},
			},
		},
		{
			Source: "vec",
			Results: []aggregator.QueryResult{
				{NoteID: "b", Score: 0.9, Source: "vec", ProjectID: "p", Title: "B"},
				{NoteID: "d", Score: 0.85, Source: "vec", ProjectID: "p", Title: "D"},
				{NoteID: "e", Score: 0.7, Source: "vec", ProjectID: "p", Title: "E"},
			},
		},
		{
			Source: "graph",
			Results: []aggregator.QueryResult{
				{NoteID: "c", Score: 0.6, Source: "graph", ProjectID: "p", Title: "C"},
				{NoteID: "f", Score: 0.5, Source: "graph", ProjectID: "p", Title: "F"},
			},
		},
	}

	run1 := aggregator.Fuse(topKs, 60, 10)
	run2 := aggregator.Fuse(topKs, 60, 10)

	if !reflect.DeepEqual(run1, run2) {
		t.Errorf("aggregator.Fuse not deterministic:\nrun1=%+v\nrun2=%+v", run1, run2)
	}

	wantTopIDs := []string{run1[0].NoteID, run1[1].NoteID, run1[2].NoteID}
	got := []string{run2[0].NoteID, run2[1].NoteID, run2[2].NoteID}
	if !reflect.DeepEqual(wantTopIDs, got) {
		t.Errorf("top-3 ordering drift: run1=%v run2=%v", wantTopIDs, got)
	}
}

func TestReplay_CallLogStable(t *testing.T) {
	fake1 := testharness.NewAggregatorFake()
	fake1.SeedFTSResults([]augment.QueryResult{{NoteID: "x", Source: "fts", ProjectID: "p"}})

	fake2 := testharness.NewAggregatorFake()
	fake2.SeedFTSResults([]augment.QueryResult{{NoteID: "x", Source: "fts", ProjectID: "p"}})

	c1 := augment.NewAggregatorConsumer(fake1, fake1)
	c2 := augment.NewAggregatorConsumer(fake2, fake2)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		_, _ = c1.Lane2FTS(ctx, "q", 10)
		_, _ = c2.Lane2FTS(ctx, "q", 10)
	}

	if !reflect.DeepEqual(fake1.Calls(), fake2.Calls()) {
		t.Errorf("call log drift:\nfake1=%+v\nfake2=%+v", fake1.Calls(), fake2.Calls())
	}
}
