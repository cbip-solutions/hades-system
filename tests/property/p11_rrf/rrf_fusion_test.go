//go:build property

package p11_rrf

import (
	"fmt"
	"math/rand"
	"reflect"
	"testing"
	"testing/quick"

	"github.com/cbip-solutions/hades-system/internal/knowledge/aggregator"
)

func genTopKs(seed int64, lanes, perLane int) []aggregator.TopK {
	rnd := rand.New(rand.NewSource(seed))
	sources := []string{"fts", "vec", "graph"}
	out := make([]aggregator.TopK, 0, lanes)
	for i := 0; i < lanes; i++ {
		src := sources[i%len(sources)]
		results := make([]aggregator.QueryResult, perLane)
		for j := 0; j < perLane; j++ {
			results[j] = aggregator.QueryResult{
				NoteID:    fmt.Sprintf("n-%d-%d", i, j),
				Score:     rnd.Float64(),
				Title:     fmt.Sprintf("t-%d-%d", i, j),
				ProjectID: "p",
				Source:    src,
			}
		}
		out = append(out, aggregator.TopK{Source: src, Results: results})
	}
	return out
}

func TestRRFFusion_Idempotent(t *testing.T) {
	prop := func(seed int64, lanes, perLane uint8) bool {
		l := int(lanes%5 + 1)
		p := int(perLane%9 + 1)
		topKs := genTopKs(seed, l, p)
		out1 := aggregator.Fuse(topKs, 60, 10)
		out2 := aggregator.Fuse(topKs, 60, 10)
		return reflect.DeepEqual(out1, out2)
	}
	if err := quick.Check(prop, &quick.Config{MaxCount: 200}); err != nil {
		t.Errorf("idempotence violated: %v", err)
	}
}

func TestRRFFusion_BoundedByLimit(t *testing.T) {
	prop := func(seed int64, lanes, perLane uint8, limit uint8) bool {
		l := int(lanes%5 + 1)
		p := int(perLane%9 + 1)
		lim := int(limit%20 + 1)
		topKs := genTopKs(seed, l, p)
		out := aggregator.Fuse(topKs, 60, lim)
		return len(out) <= lim
	}
	if err := quick.Check(prop, &quick.Config{MaxCount: 200}); err != nil {
		t.Errorf("limit bound violated: %v", err)
	}
}

func TestRRFFusion_NonIncreasingScores(t *testing.T) {
	prop := func(seed int64, lanes, perLane uint8) bool {
		l := int(lanes%5 + 1)
		p := int(perLane%9 + 1)
		topKs := genTopKs(seed, l, p)
		out := aggregator.Fuse(topKs, 60, 50)
		for i := 0; i+1 < len(out); i++ {
			if out[i].Score < out[i+1].Score {
				return false
			}
		}
		return true
	}
	if err := quick.Check(prop, &quick.Config{MaxCount: 200}); err != nil {
		t.Errorf("non-increasing scores violated: %v", err)
	}
}

func TestRRFFusion_SingleLaneIdentity(t *testing.T) {
	for _, src := range []string{"fts", "vec", "graph"} {
		results := []aggregator.QueryResult{
			{NoteID: "a", Score: 10.0, Source: src, ProjectID: "p"},
			{NoteID: "b", Score: 5.0, Source: src, ProjectID: "p"},
			{NoteID: "c", Score: 1.0, Source: src, ProjectID: "p"},
		}
		topKs := []aggregator.TopK{{Source: src, Results: results}}
		out := aggregator.Fuse(topKs, 60, 10)
		if len(out) != 3 {
			t.Errorf("source %s: got %d, want 3", src, len(out))
			continue
		}
		for i, expectedID := range []string{"a", "b", "c"} {
			if out[i].NoteID != expectedID {
				t.Errorf("source %s: out[%d].NoteID = %q, want %q", src, i, out[i].NoteID, expectedID)
			}
		}
	}
}

func TestRRFFusion_DedupesSameNoteAcrossLanes(t *testing.T) {
	common := aggregator.QueryResult{NoteID: "shared", ProjectID: "p"}
	common.Source = "fts"
	tk1 := aggregator.TopK{Source: "fts", Results: []aggregator.QueryResult{
		{NoteID: "shared", Score: 1.0, Source: "fts", ProjectID: "p"},
		{NoteID: "fts-only", Score: 0.5, Source: "fts", ProjectID: "p"},
	}}
	tk2 := aggregator.TopK{Source: "vec", Results: []aggregator.QueryResult{
		{NoteID: "shared", Score: 0.9, Source: "vec", ProjectID: "p"},
		{NoteID: "vec-only", Score: 0.4, Source: "vec", ProjectID: "p"},
	}}
	out := aggregator.Fuse([]aggregator.TopK{tk1, tk2}, 60, 10)
	if len(out) != 3 {
		t.Fatalf("got %d results, want 3 (dedup of 'shared')", len(out))
	}
	if out[0].NoteID != "shared" {
		t.Errorf("out[0].NoteID = %q, want shared (multi-lane RRF sum is highest)", out[0].NoteID)
	}
}
