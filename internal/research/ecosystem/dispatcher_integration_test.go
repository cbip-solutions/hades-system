// go:build integration
package ecosystem

import (
	"context"
	"sort"
	"testing"
	"time"
)

// TestInvZen200Integration_1000IterDeterminism enforces invariant at the
// integration seam: 1000 iterations of an identical query against the real
// Dispatcher (fixture aggregators wired through the public Options surface)
// MUST yield the identical chunk-ID slice.
//
// Differs from the unit-level race test (dispatcher_race_test.go) in that
// THIS test is the canonical integration-level invariant enforcement
// expected by the verification matrix.
func TestInvZen200Integration_1000IterDeterminism(t *testing.T) {
	fix := buildHappyPathFixture(t)
	const iters = 1000
	reference := mustQueryChunkIDsForIntegration(t, fix.dispatcher, QueryRequest{
		Query:    "1000-iter integration determinism probe",
		Doctrine: "default",
	})
	if len(reference) == 0 {
		t.Fatalf("inv-zen-200 fixture must return chunks on happy path")
	}
	for i := 1; i < iters; i++ {
		got := mustQueryChunkIDsForIntegration(t, fix.dispatcher, QueryRequest{
			Query:    "1000-iter integration determinism probe",
			Doctrine: "default",
		})
		if !int64SliceEqualIntegration(got, reference) {
			t.Fatalf("inv-zen-200 integration violation at iter %d: got=%v reference=%v",
				i, got, reference)
		}
	}
}

// TestInvZen205Integration_DoctrineEventCounts enforces invariant
// (doctrine-strictness knob) at the integration seam: each doctrine mode
// MUST emit the expected canonical event-type set on the happy path.
//
// Happy-path event matrix (per doctrine + AuditEmissionLevel + spec §2.7
// Q7=A table + happy-path-with-AllVerified fixture wiring):
//
// - default doctrine (AuditQueryAbstainVerifyFailureAnswer): emits
// EvtRAGQuery + EvtRAGAnswer. Verify NOT emitted (AllVerified=true so
// verify-failure gate filters it out; see audit_emitter.go shouldEmit
// "EvtRAGVerify" case). Citation + Retrieval not in the emission set.
//
// - max-scope doctrine (AuditAll8Events): emits EvtRAGQuery + EvtRAGRetrieval
//
// - EvtRAGCitation + EvtRAGVerify + EvtRAGAnswer on happy path (no
// abstain triggered + LLM-judge accepts the fixture answer).
//
// - capa-firewall doctrine (AuditAll8Events + RefuseOnUnverified=true +
// LLMJudgeEnabled=true): same as max-scope on happy path because the
// fixture verifier returns AllVerified=true for crypto/sha256.Sum256
// (capa-firewall's refuse gate ONLY fires on AllVerified=false). Plus
// LLM-judge re-pass with judge.Acceptable=true completes Step 13.
//
// The N-query sweep across queries (one per ecosystem) confirms event-set
// is doctrine-bound, NOT query-bound, NOT ecosystem-bound.
func TestInvZen205Integration_DoctrineEventCounts(t *testing.T) {

	queries := []string{
		"how do I create a SHA-256 hash in Go?",
		"how do I parse JSON in Python?",
		"how do I read a file in TypeScript?",
		"how do I write a struct in Rust?",
	}
	matrix := []struct {
		doctrine    string
		wantEvents  []EventType
		needsJudge  bool
		description string
	}{
		{
			doctrine:    "default",
			wantEvents:  []EventType{EvtRAGQuery, EvtRAGAnswer},
			needsJudge:  false,
			description: "AuditQueryAbstainVerifyFailureAnswer + AllVerified=true (verify silent)",
		},
		{
			doctrine: "max-scope",
			wantEvents: []EventType{
				EvtRAGQuery, EvtRAGRetrieval, EvtRAGCitation, EvtRAGVerify, EvtRAGAnswer,
			},
			needsJudge:  true,
			description: "AuditAll8Events + LLM-judge accept",
		},
		{
			doctrine: "capa-firewall",
			wantEvents: []EventType{
				EvtRAGQuery, EvtRAGRetrieval, EvtRAGCitation, EvtRAGVerify, EvtRAGAnswer,
			},
			needsJudge:  true,
			description: "AuditAll8Events + RefuseOnUnverified gate clean + LLM-judge accept",
		},
	}

	for _, m := range matrix {
		t.Run(m.doctrine, func(t *testing.T) {
			for _, q := range queries {
				fix := buildHappyPathFixture(t)
				if m.needsJudge {
					judgeBackend := &fakeJudgeBackend{response: `{"acceptable": true}`}
					fix.llmJudge = mustNewHaikuJudge(t, judgeBackend)
					fix.dispatcher = newDispatcherFromFixture(t, fix)
				}
				res, err := fix.dispatcher.Query(context.Background(), QueryRequest{
					Query:    q,
					Doctrine: m.doctrine,
				})
				if err != nil {
					t.Fatalf("[%s] Query(%q): %v", m.doctrine, q, err)
				}
				if res.Abstained {
					t.Fatalf("[%s] Query(%q) abstained (%s); expected happy path",
						m.doctrine, q, res.AbstainReason)
				}
				have := auditEventTypes(fix.auditChain)
				if !auditChainHasOrdered(fix.auditChain, m.wantEvents) {
					t.Errorf("[%s/%q] inv-zen-205 event-order violation\n  description: %s\n  have=%v\n  want⊇%v",
						m.doctrine, q, m.description, have, m.wantEvents)
				}
				if contains(have, EvtRAGAbstain) {
					t.Errorf("[%s/%q] happy path must NOT emit EvtRAGAbstain; got=%v",
						m.doctrine, q, have)
				}
				if res.Provenance.DoctrineApplied != m.doctrine {
					t.Errorf("[%s/%q] Provenance.DoctrineApplied=%q; want=%q",
						m.doctrine, q, res.Provenance.DoctrineApplied, m.doctrine)
				}
			}
		})
	}
}

func TestDispatcherIntegration_AllFourEcosystemsBroadcast(t *testing.T) {
	fix := buildHappyPathFixture(t)
	fix.router = newTestRouterWithUniformClassifier(t)
	fix.dispatcher = newDispatcherFromFixture(t, fix)

	res, err := fix.dispatcher.Query(context.Background(), QueryRequest{
		Query:    "ambiguous cross-eco integration query",
		Doctrine: "default",
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.Abstained {
		t.Fatalf("broadcast happy path must not abstain: %+v", res)
	}
	if res.Provenance.RoutingMethod != "broadcast" {
		t.Errorf("RoutingMethod=%q; want broadcast", res.Provenance.RoutingMethod)
	}
	if got := len(res.Provenance.RoutingEcosystems); got != 4 {
		t.Errorf("RoutingEcosystems count=%d; want 4 (all ecosystems)", got)
	}
	have := map[Ecosystem]bool{}
	for _, e := range res.Provenance.RoutingEcosystems {
		have[e] = true
	}
	for _, want := range AllEcosystems {
		if !have[want] {
			t.Errorf("broadcast must touch ecosystem %s; have=%v", want, res.Provenance.RoutingEcosystems)
		}
	}
}

// TestDispatcherIntegration_LatencyBudget_P50P95 enforces spec §4.7 latency
// budget at the integration seam. 20 sequential happy-path Query() calls
// under default doctrine MUST satisfy P50 ≤350ms + P95 ≤700ms.
//
// Note the fixture happy-path Query is sub-millisecond on M4 dev hardware
// (no real BGE inference, no SQLite I/O, no subprocess verifier — pure
// in-memory fixture). The budget enforcement here is a smoke-test: real
// budget enforcement against production-shaped fixtures lives in
// follow-up tests (with real *.db + real BGE + real verifier).
//
// THIS test catches a coarse regression — if the dispatcher's orchestration
// or rerank or fan-out path picks up a Sleep / synchronous I/O / accidental
// quadratic loop, the P95 will blow past 700ms.
func TestDispatcherIntegration_LatencyBudget_P50P95(t *testing.T) {
	if testing.Short() {
		t.Skip("perf; skip -short")
	}
	fix := buildHappyPathFixture(t)

	const trials = 20
	durations := make([]time.Duration, trials)
	for i := 0; i < trials; i++ {
		start := time.Now()
		res, err := fix.dispatcher.Query(context.Background(), QueryRequest{
			Query:    "latency probe",
			Doctrine: "default",
		})
		if err != nil {
			t.Fatalf("trial %d: %v", i, err)
		}
		if res.Abstained {
			t.Fatalf("trial %d: must not abstain (reason=%q)", i, res.AbstainReason)
		}
		durations[i] = time.Since(start)
	}
	p50 := percentileDurationIntegration(durations, 0.50)
	p95 := percentileDurationIntegration(durations, 0.95)
	t.Logf("dispatcher.Query latency: p50=%v p95=%v over %d trials (fixture path)", p50, p95, trials)
	if p50 > 350*time.Millisecond {
		t.Errorf("inv-zen latency: P50 %v > 350ms budget (spec §4.7)", p50)
	}
	if p95 > 700*time.Millisecond {
		t.Errorf("inv-zen latency: P95 %v > 700ms budget (spec §4.7)", p95)
	}
}

func percentileDurationIntegration(d []time.Duration, p float64) time.Duration {
	if len(d) == 0 {
		return 0
	}
	sorted := append([]time.Duration(nil), d...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	if p <= 0 {
		return sorted[0]
	}
	if p >= 1 {
		return sorted[len(sorted)-1]
	}
	h := float64(len(sorted)-1) * p
	loIdx := int(h)
	hiIdx := loIdx + 1
	if hiIdx >= len(sorted) {
		return sorted[len(sorted)-1]
	}
	frac := h - float64(loIdx)
	lo := float64(sorted[loIdx])
	hi := float64(sorted[hiIdx])
	return time.Duration(lo + frac*(hi-lo))
}

// TestDispatcherIntegration_LiveFallback_AbstainOnEmpty verifies the
// live-fallback / abstain branch end-to-end. With an empty-aggregator
// configuration (no candidates from any ecosystem), the dispatcher MUST
// emit EvtRAGAbstain + return Abstained=true. This exercises the D-11
// live-fallback abstain path through the full orchestration.
func TestDispatcherIntegration_LiveFallback_AbstainOnEmpty(t *testing.T) {
	fix := buildHappyPathFixture(t)
	fix.aggregators = emptyAggregatorsAllEco()
	fix.dispatcher = newDispatcherFromFixture(t, fix)
	res, err := fix.dispatcher.Query(context.Background(), QueryRequest{
		Query:    "no-coverage live-fallback probe",
		Doctrine: "default",
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if !res.Abstained {
		t.Errorf("live-fallback empty-candidates MUST abstain; got chunks=%d", len(res.Chunks))
	}
	if !auditChainHas(fix.auditChain, EvtRAGAbstain) {
		t.Errorf("live-fallback abstain MUST emit EvtRAGAbstain; chain=%v",
			auditEventTypes(fix.auditChain))
	}
}

func mustQueryChunkIDsForIntegration(t *testing.T, d *Dispatcher, req QueryRequest) []int64 {
	t.Helper()
	res, err := d.Query(context.Background(), req)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.Abstained {
		t.Fatalf("happy-path Query abstained (%q); cannot enforce determinism",
			res.AbstainReason)
	}
	ids := make([]int64, len(res.Chunks))
	for i, c := range res.Chunks {
		ids[i] = c.ChunkID
	}
	return ids
}

func int64SliceEqualIntegration(a, b []int64) bool {
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
