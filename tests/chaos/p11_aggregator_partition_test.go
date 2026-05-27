// go:build chaos
package chaos

import (
	"context"
	"errors"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/augment"
	"github.com/cbip-solutions/hades-system/tests/testharness"
)

func TestNetworkPartition_AllLanesErrorThenRecover(t *testing.T) {
	fake := testharness.NewAggregatorFake()
	partitioned := errors.New("chaos: partitioned from aggregator")
	fake.InjectError(testharness.AggregatorOpFTS, partitioned)
	fake.InjectError(testharness.AggregatorOpVec, partitioned)
	fake.InjectError(testharness.AggregatorOpGraph, partitioned)
	fake.InjectError(testharness.AggregatorOpEmbed, partitioned)

	c := augment.NewAggregatorConsumer(fake, fake)

	ctx := context.Background()

	if _, err := c.Lane2FTS(ctx, "x", 10); err == nil {
		t.Error("Lane2FTS: expected partition error, got nil")
	}

	r4, err := c.Lane4Vec(ctx, "x", 10, 0.92)
	if err != nil {
		t.Errorf("Lane4Vec: err=%v, want nil (graceful degrade)", err)
	}
	if !r4.Degraded {
		t.Error("Lane4Vec: Degraded=false, want true under embed partition")
	}

	fake.SeedFTSResults([]augment.QueryResult{
		{NoteID: "n1", Score: 0.9, Title: "post-recovery", Source: "fts", ProjectID: "p"},
	})
	fake.ClearError(testharness.AggregatorOpFTS)

	res, err := c.Lane2FTS(ctx, "x", 10)
	if err != nil {
		t.Fatalf("post-recovery Lane2FTS err = %v, want nil", err)
	}
	if len(res.Results) != 1 || res.Results[0].NoteID != "n1" {
		t.Errorf("post-recovery results = %+v, want [n1]", res.Results)
	}
}

func TestNetworkPartition_PartialDegradation(t *testing.T) {
	fake := testharness.NewAggregatorFake()
	fake.InjectError(testharness.AggregatorOpVec, errors.New("chaos: vec extension down"))
	fake.SeedFTSResults([]augment.QueryResult{
		{NoteID: "fts1", Score: 0.9, Source: "fts", ProjectID: "p"},
	})
	fake.SeedGraphResults([]augment.QueryResult{
		{NoteID: "graph1", Score: 0.8, Source: "graph", ProjectID: "p"},
	})

	c := augment.NewAggregatorConsumer(fake, fake)
	ctx := context.Background()

	ftsRes, ftsErr := c.Lane2FTS(ctx, "x", 10)
	if ftsErr != nil || len(ftsRes.Results) != 1 {
		t.Errorf("Lane2FTS: err=%v results=%d, want healthy", ftsErr, len(ftsRes.Results))
	}

	if _, err := c.Lane4Vec(ctx, "x", 10, 0.92); err == nil {
		t.Error("Lane4Vec: expected wrapped error from QueryVec failure, got nil")
	}
}
