//go:build property && cgo

// tests/property/ecosystem/dispatcher_fanout_property_test.go (Plan 14 Phase H Task H-7-6)
//
// inv-zen-200: cross-eco fan-out parallel-correctness — 4-goroutine result
// merge MUST be deterministic given fixed per-eco inputs (no goroutine
// scheduling effects bleed into the final ranked list).
//
// We model the merge contract that `dispatcher.fanOutRetrieve` is
// required to honour (see internal/research/ecosystem/dispatcher.go
// lines 584-587, "inv-zen-200 partial: 4-goroutine result merge is
// deterministic given fixed inputs"):
//
//   - Each goroutine emits a per-eco ordered candidate list.
//   - The merge collects all candidates and produces a single list
//     sorted by a canonical key (ChunkID asc, ties broken by Ecosystem
//     name asc) with dedup on ChunkID.
//
// Property: same per-eco inputs → same merged output across 200 runs.
// We launch real goroutines so the test exercises scheduler concurrency,
// not a serial mock.

package ecosystem_property_test

import (
	"context"
	"sort"
	"sync"
	"testing"
	"testing/quick"

	_ "github.com/mattn/go-sqlite3"
	"github.com/cbip-solutions/hades-system/internal/research/ecosystem"
)

type fanOutCandidate struct {
	ChunkID int64
	Eco     ecosystem.Ecosystem
}

func fanOutMerge(ctx context.Context, perEco map[ecosystem.Ecosystem][]int64) []fanOutCandidate {
	type ecoBatch struct {
		eco        ecosystem.Ecosystem
		candidates []fanOutCandidate
	}
	mu := sync.Mutex{}
	batches := make([]ecoBatch, 0, len(perEco))

	var wg sync.WaitGroup
	for eco, ids := range perEco {
		eco := eco
		ids := ids
		wg.Add(1)
		go func() {
			defer wg.Done()
			cands := make([]fanOutCandidate, 0, len(ids))
			for _, id := range ids {
				if ctx.Err() != nil {
					return
				}
				cands = append(cands, fanOutCandidate{ChunkID: id, Eco: eco})
			}
			mu.Lock()
			batches = append(batches, ecoBatch{eco: eco, candidates: cands})
			mu.Unlock()
		}()
	}
	wg.Wait()

	all := make([]fanOutCandidate, 0)
	for _, b := range batches {
		all = append(all, b.candidates...)
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].ChunkID != all[j].ChunkID {
			return all[i].ChunkID < all[j].ChunkID
		}
		return string(all[i].Eco) < string(all[j].Eco)
	})
	seen := make(map[int64]struct{}, len(all))
	out := make([]fanOutCandidate, 0, len(all))
	for _, c := range all {
		if _, dup := seen[c.ChunkID]; dup {
			continue
		}
		seen[c.ChunkID] = struct{}{}
		out = append(out, c)
	}
	return out
}

// TestDispatcherFanOut_Property_DeterministicMerge launches the 4-eco
// merge 200 times per input shape and asserts every run returns the
// same merged list. Goroutine scheduling MUST NOT leak into the result.
//
// inv-zen-200 enforcement.
func TestDispatcherFanOut_Property_DeterministicMerge(t *testing.T) {
	prop := func(seed uint16, count uint8) bool {
		ctx := context.Background()
		nPerEco := int(count%6 + 1)

		perEco := make(map[ecosystem.Ecosystem][]int64, 4)
		for i, eco := range ecosystem.AllEcosystems {
			ids := make([]int64, nPerEco)
			for j := 0; j < nPerEco; j++ {

				ids[j] = int64(seed)*1000 + int64(i*100) + int64(j)
			}
			perEco[eco] = ids
		}

		if len(perEco[ecosystem.EcoGo]) > 0 && len(perEco[ecosystem.EcoPython]) > 0 {
			perEco[ecosystem.EcoPython][0] = perEco[ecosystem.EcoGo][0]
		}

		first := fanOutMerge(ctx, perEco)
		for run := 0; run < 5; run++ {
			next := fanOutMerge(ctx, perEco)
			if !equalCandidates(first, next) {
				t.Logf("inv-zen-200: non-deterministic merge: seed=%d nPerEco=%d run=%d first=%v next=%v",
					seed, nPerEco, run, first, next)
				return false
			}
		}
		return true
	}
	cfg := &quick.Config{MaxCount: 200}
	if err := quick.Check(prop, cfg); err != nil {
		t.Errorf("inv-zen-200: cross-eco fan-out merge non-deterministic: %v", err)
	}
}

func TestDispatcherFanOut_Property_DedupAcrossEcos(t *testing.T) {
	ctx := context.Background()

	perEco := map[ecosystem.Ecosystem][]int64{
		ecosystem.EcoGo:     {42},
		ecosystem.EcoPython: {42},
	}
	out := fanOutMerge(ctx, perEco)
	if len(out) != 1 {
		t.Fatalf("inv-zen-200: dedup failed: out=%v", out)
	}

	if out[0].Eco != ecosystem.EcoGo {
		t.Errorf("inv-zen-200: dedup tie-break violated: got Eco=%s, want %s", out[0].Eco, ecosystem.EcoGo)
	}
}

func TestDispatcherFanOut_Property_EmptyInputsYieldEmpty(t *testing.T) {
	ctx := context.Background()
	perEco := make(map[ecosystem.Ecosystem][]int64)
	for _, eco := range ecosystem.AllEcosystems {
		perEco[eco] = nil
	}
	out := fanOutMerge(ctx, perEco)
	if len(out) != 0 {
		t.Errorf("inv-zen-200: empty inputs produced non-empty merge: %v", out)
	}
}

func equalCandidates(a, b []fanOutCandidate) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ChunkID != b[i].ChunkID || a[i].Eco != b[i].Eco {
			return false
		}
	}
	return true
}
