//go:build integration

package ecosystem_test

import "testing"

// Phase D Task D-3 implementer fills this with the real BGE-reranker-v2-m3
// p95 latency benchmark (inv-zen-198 target ≤300ms for 100 candidates on
// M4 MPS). This file ships in Phase C C-8 as a known-location seed so
// Phase D's implementer doesn't have to scaffold the integration test
// path.
//
// The body intentionally only contains a SkipNow call; Phase D MUST
// remove the skip + add the real benchmark. The test path stays the same.
//
// inv-zen-198: BGE-reranker-v2-m3 query latency p95 ≤300ms for 100
// candidates on M4 MPS.

func TestReRankerBench_Phase_D_OWNED(t *testing.T) {
	t.Skip("Phase D Task D-3 implements BGE reranker p95 ≤300ms (inv-zen-198) benchmark here")
}
