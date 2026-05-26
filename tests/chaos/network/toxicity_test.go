//go:build chaos

// Toxiproxy-driven chaos package (inv-zen-305).
//
// toxicity_test.go pins the 10-toxic-type set + the scenario
// generator's Cartesian product contract. These tests do NOT need a
// running Toxiproxy daemon — they exercise pure table semantics + the
// scenario expansion. inv-zen-305 scenario count = 10 toxics × 8 edges
// = 80 holds load-bearing across the suite, so each test is single-
// responsibility.

package network

import (
	"slices"
	"testing"
)

func TestAllToxicTypesHasExactlyTen(t *testing.T) {
	if got := len(AllToxicTypes()); got != 10 {
		t.Fatalf("AllToxicTypes() = %d, want 10", got)
	}
}

func TestAllToxicTypesAreDistinct(t *testing.T) {
	toxics := AllToxicTypes()
	seen := make(map[ToxicType]struct{}, len(toxics))
	for _, tox := range toxics {
		if _, dup := seen[tox]; dup {
			t.Errorf("duplicate toxic type: %s", tox)
		}
		seen[tox] = struct{}{}
	}
}

func TestGenerateScenariosCartesianProduct(t *testing.T) {
	reg := &Registry{
		Edges: map[string]EdgeConfig{
			"a": {Listen: "127.0.0.1:39001"},
			"b": {Listen: "127.0.0.1:39002"},
		},
	}
	scenarios := GenerateScenarios(reg)
	if got, want := len(scenarios), 2*10; got != want {
		t.Fatalf("scenarios = %d, want %d (2 edges × 10 toxics)", got, want)
	}

	type pair struct {
		tox  ToxicType
		edge string
	}
	seen := make(map[pair]struct{}, len(scenarios))
	for _, s := range scenarios {
		p := pair{s.Toxic, s.Edge}
		if _, dup := seen[p]; dup {
			t.Errorf("duplicate scenario: %s", s)
		}
		seen[p] = struct{}{}
	}
	if got, want := len(seen), 2*10; got != want {
		t.Fatalf("unique scenarios = %d, want %d", got, want)
	}

	edgeCount := make(map[string]int)
	for _, s := range scenarios {
		edgeCount[s.Edge]++
	}
	for edge, n := range edgeCount {
		if n != 10 {
			t.Errorf("edge %s appears %d times, want 10", edge, n)
		}
	}

	gotEdges := slices.Collect(func(yield func(string) bool) {
		for edge := range edgeCount {
			if !yield(edge) {
				return
			}
		}
	})
	slices.Sort(gotEdges)
	if !slices.Equal(gotEdges, []string{"a", "b"}) {
		t.Errorf("edge set drifted: got=%v", gotEdges)
	}
}
