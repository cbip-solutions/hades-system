// internal/research/ecosystem/dispatcher_race_test.go
//
// test. Concurrent Query() invocations against a shared Dispatcher MUST be
// race-clean AND deterministic (identical input → identical chunk-ID
// permutation across all workers, all iterations).
//
// inv-zen-200 statement: "Cross-eco fan-out parallel-correctness — 4-goroutine
// result merge deterministic." This file is the runtime enforcement of that
// invariant. Static enforcement lives in `verify-invariants` (plan-file
// reference) + per-step unit tests in dispatcher_test.go.
//
// Plan-file drift navigated (recorded inline at each touchpoint):
//
//   - The plan-file body uses `c.SymbolPath` as the determinism key; in the
//     happy-path fixture every chunk has SymbolPath="crypto/sha256.Sum256"
//     so SymbolPath alone is constant — useless as a determinism key. We
//     use ChunkID instead (the fan-out merge contract — inv-zen-197 §4.6 —
//     is "stable per-ChunkID ordering after RRF tie-break"). Plan-file
//     intent ("identical chunk set across workers") is preserved.
//
//   - The plan-file scale (32 workers × 50 iters = 1,600 invocations) is
//     bumped to 32 × 32 = 1,024 to keep race-detector wall-time under the
//     repo's 60s default test timeout on M4 dev hardware with -count=2.
//     Task-description scale "1000 iterations identical query → identical
//     chunk ID set" maps to this 1,024-shot run (32×32).
//
//   - The 1000-iter inv-zen-200 enforcement requested in the task description
//     lives in this same file as a sequential loop (TestInvZen200_Determinism_
//     1000Iters); the concurrent variant tests no-race + determinism under
//     parallel pressure.
//
//   - No build-tag gating (vs plan-file `cgo` tag): the package is already
//     cgo-only via sqlite-vec transitive deps; an explicit `cgo` tag here
//     would cause `go test ./...` (default invocation, which already has
//     CGO_ENABLED=1) to skip these tests with "no tests to run". The race
//     detector binary itself is cgo-only so this file is implicitly cgo-
//     gated. Other race-sensitive files in this package follow the same
//     pattern (no explicit cgo tag).

package ecosystem

import (
	"context"
	"sync"
	"testing"
)

// TestInvZen200_Determinism_1000Iters enforces inv-zen-200 (cross-eco fan-out
// determinism) sequentially. 1,000 identical Query() calls MUST return the
// identical chunk-ID set in the identical order.
//
// Rationale (max-scope): if Query() is non-deterministic, downstream consumers
// (CR-prefix LLM, capa-firewall replay, audit-chain replay) can't reproduce
// the dispatcher's reasoning. Determinism is load-bearing for inv-zen-194
// (citation grammar replay) + inv-zen-197 (canonical 8-event order).
func TestInvZen200_Determinism_1000Iters(t *testing.T) {
	if testing.Short() {
		t.Skip("inv-zen-200 1000-iter determinism; skip -short")
	}
	fix := buildHappyPathFixture(t)
	const iters = 1000
	reference := queryChunkIDsForRace(t, fix.dispatcher, "deterministic 1000-iter probe")
	if len(reference) == 0 {
		t.Fatalf("happy-path Query returned no chunks; cannot enforce inv-zen-200")
	}
	for i := 1; i < iters; i++ {
		got := queryChunkIDsForRace(t, fix.dispatcher, "deterministic 1000-iter probe")
		if !int64SliceEqualRace(got, reference) {
			t.Fatalf("inv-zen-200 violation at iter %d: got=%v reference=%v", i, got, reference)
		}
	}
}

// TestDispatcher_FanoutParallelCorrectness_NoRace enforces inv-zen-200 under
// concurrent pressure. 32 workers × 32 iters = 1,024 concurrent Query()
// invocations against a shared Dispatcher MUST:
//   - return identical chunk-ID order across all 1,024 results (determinism)
//   - run race-clean under -race (no shared-state corruption)
//
// Drift navigated (vs plan-file): plan-file uses SymbolPath as the
// determinism key. SymbolPath is constant in the happy-path fixture (every
// chunk = "crypto/sha256.Sum256"), so SymbolPath cannot detect non-determinism
// of merge order. We use ChunkID instead — that's the actual cross-eco RRF
// tie-break key per inv-zen-200 statement.
func TestDispatcher_FanoutParallelCorrectness_NoRace(t *testing.T) {
	fix := buildHappyPathFixture(t)
	const workers = 32
	const iters = 32
	results := make([][]int64, workers*iters)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				res, err := fix.dispatcher.Query(context.Background(), QueryRequest{
					Query:    "deterministic broadcast query",
					Doctrine: "default",
				})
				if err != nil {
					t.Errorf("worker %d iter %d: Query: %v", workerID, i, err)
					return
				}
				ids := make([]int64, len(res.Chunks))
				for j, c := range res.Chunks {
					ids[j] = c.ChunkID
				}
				results[workerID*iters+i] = ids
			}
		}(w)
	}
	wg.Wait()

	// Determinism check: every result must match results[0]. Because every
	// worker fires the SAME QueryRequest against the SAME Dispatcher with
	// fixture aggregators returning the SAME candidate set, fan-out merge
	// MUST yield the identical permutation.
	reference := results[0]
	if len(reference) == 0 {
		t.Fatalf("happy-path Query returned no chunks; cannot enforce inv-zen-200")
	}
	for i := 1; i < len(results); i++ {
		if !int64SliceEqualRace(results[i], reference) {
			t.Fatalf("inv-zen-200 violation at result %d: got=%v reference=%v",
				i, results[i], reference)
		}
	}
}

// TestInvZen200_RandomDelayDeterminism enforces inv-zen-200 under the harder
// scenario where per-ecosystem aggregators complete in shuffled orders
// (randomDelayAggregators). Even when goroutine completion order is
// non-deterministic, the merge output MUST be deterministic.
//
// This is the strongest enforcement of inv-zen-200: the fan-out + RRF + stable
// tie-break must produce identical output regardless of goroutine scheduling.
func TestInvZen200_RandomDelayDeterminism(t *testing.T) {
	if testing.Short() {
		t.Skip("inv-zen-200 random-delay determinism; skip -short")
	}
	const trials = 200
	var reference []int64
	for trial := 0; trial < trials; trial++ {
		fix := buildHappyPathFixture(t)

		fix.aggregators = randomDelayAggregators(trial)
		fix.dispatcher = newDispatcherFromFixture(t, fix)
		got := queryChunkIDsForRace(t, fix.dispatcher, "random-delay determinism probe")
		if trial == 0 {
			reference = got
			if len(reference) == 0 {
				t.Fatalf("random-delay Query returned no chunks at trial 0")
			}
			continue
		}
		if !int64SliceEqualRace(got, reference) {
			t.Fatalf("inv-zen-200 violation at trial %d (seed=%d): got=%v reference=%v",
				trial, trial, got, reference)
		}
	}
}

func queryChunkIDsForRace(t *testing.T, d *Dispatcher, query string) []int64 {
	t.Helper()
	res, err := d.Query(context.Background(), QueryRequest{
		Query:    query,
		Doctrine: "default",
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.Abstained {
		t.Fatalf("inv-zen-200 fixture must not abstain on happy path; got reason=%q", res.AbstainReason)
	}
	ids := make([]int64, len(res.Chunks))
	for i, c := range res.Chunks {
		ids[i] = c.ChunkID
	}
	return ids
}

func int64SliceEqualRace(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
