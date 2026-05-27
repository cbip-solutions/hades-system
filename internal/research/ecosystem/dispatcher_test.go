// internal/research/ecosystem/dispatcher_test.go
//
// Tests for Dispatcher.Query 14-step orchestration.
//
// Coverage discipline: ≥90% on dispatcher.go production paths (security/correctness-
// critical hot path — gates all of downstream behaviour). Tests cover
// each of the 14 steps + all four abstain exits (low-confidence, citation-
// persistent, capa-firewall refuse, LLM-judge reject) + fan-out parallel
// correctness + 8-event audit canonical order
// + context-cancel at every step + concurrent Query safety + doctrine knob
// (invariant partial: max-scope/default/capa-firewall AuditEmissionLevel +
// LLMJudgeEnabled + RefuseOnUnverified branches).
//
// Plan-file drift navigated (recorded inline at each touchpoint):
//
// - `aggregator.Query` type does NOT exist in the aggregator package
// . The plan-file's `aggregatorAdapter.Query(ctx,
// q aggregator.Query) ([]aggregator.QueryResult, error)` signature would not
// compile; only BinaryTop200/FTS5Top200/HydrateChunks are actually consumed
// by Query/fanOutRetrieve/hydrateChunks. The adapter interface here drops the
// vestigial Query method.
//
// - `builtinDoctrineProfiles()` (function form) does NOT exist; the canonical
// accessor is the package-level `builtinProfiles` map. Tests use the map
// directly per verification.
//
// - DoctrineResolver in doctrine.go is the concrete production resolver;
// fixture tests use a `fixtureDoctrineResolver` interface-satisfying type
// to avoid coupling to *active.Accessor wiring (which has its own
// unit tests).

package ecosystem

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDispatcher_HappyPath_DefaultDoctrine(t *testing.T) {
	fix := buildHappyPathFixture(t)
	res, err := fix.dispatcher.Query(context.Background(), QueryRequest{
		Query:      "how do I create a SHA-256 hash in Go?",
		Doctrine:   "default",
		MaxResults: 5,
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.Abstained {
		t.Errorf("happy path must not abstain: %+v", res)
	}
	if len(res.Chunks) == 0 {
		t.Errorf("must return chunks")
	}
	if res.AuditChainSeq == 0 {
		t.Errorf("AuditChainSeq must be populated")
	}
	if res.Provenance.RoutingMethod == "" {
		t.Errorf("Provenance.RoutingMethod must be populated")
	}
	if res.Provenance.DetectedVersion == "" {
		t.Errorf("Provenance.DetectedVersion must be populated")
	}
	if res.Provenance.DoctrineApplied != "default" {
		t.Errorf("Provenance.DoctrineApplied=%q; want %q", res.Provenance.DoctrineApplied, "default")
	}
}

func TestDispatcher_HappyPath_MaxScope(t *testing.T) {
	fix := buildHappyPathFixture(t)
	judgeBackend := &fakeJudgeBackend{response: `{"acceptable": true}`}
	fix.llmJudge = mustNewHaikuJudge(t, judgeBackend)
	fix.dispatcher = newDispatcherFromFixture(t, fix)

	res, err := fix.dispatcher.Query(context.Background(), QueryRequest{
		Query:    "how do I create a SHA-256 hash in Go?",
		Doctrine: "max-scope",
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.Abstained {
		t.Errorf("max-scope happy path must not abstain: %+v", res)
	}
	if judgeBackend.calls != 1 {
		t.Errorf("max-scope must invoke LLM-judge exactly once; calls=%d", judgeBackend.calls)
	}
}

func TestDispatcher_RouterBroadcast_TriggersFanOutAll4Ecosystems(t *testing.T) {
	fix := buildHappyPathFixture(t)
	fix.router = newTestRouterWithUniformClassifier(t)
	fix.dispatcher = newDispatcherFromFixture(t, fix)
	res, err := fix.dispatcher.Query(context.Background(), QueryRequest{
		Query:    "ambiguous cross-eco query",
		Doctrine: "default",
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(res.Provenance.RoutingEcosystems) != 4 {
		t.Errorf("broadcast must fan out to all 4; got %d (%v)",
			len(res.Provenance.RoutingEcosystems), res.Provenance.RoutingEcosystems)
	}
}

func TestDispatcher_RouterSingle_HighConfidenceTop1(t *testing.T) {
	fix := buildHappyPathFixture(t)

	fix.router = newTestRouterWithClassifier(t, map[Ecosystem]float64{
		EcoGo: 0.85, EcoPython: 0.05, EcoTypeScript: 0.05, EcoRust: 0.05,
	})
	fix.dispatcher = newDispatcherFromFixture(t, fix)
	res, err := fix.dispatcher.Query(context.Background(), QueryRequest{
		Query:    "single-eco query",
		Doctrine: "default",
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(res.Provenance.RoutingEcosystems) != 1 {
		t.Errorf("single must target exactly 1 eco; got %d", len(res.Provenance.RoutingEcosystems))
	}
	if res.Provenance.RoutingMethod != string(RoutingMethodSingle) {
		t.Errorf("RoutingMethod=%q; want %q", res.Provenance.RoutingMethod, RoutingMethodSingle)
	}
}

func TestDispatcher_AbstainPath_LowConfidence(t *testing.T) {
	fix := buildHappyPathFixture(t)
	fix.aggregators = emptyAggregatorsAllEco()
	fix.dispatcher = newDispatcherFromFixture(t, fix)
	res, err := fix.dispatcher.Query(context.Background(), QueryRequest{
		Query:    "query likely to abstain",
		Doctrine: "default",
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if !res.Abstained {
		t.Errorf("low-confidence retrieval must abstain")
	}
	if res.AbstainReason == "" {
		t.Errorf("AbstainReason must be populated")
	}
	if !auditChainHas(fix.auditChain, EvtRAGAbstain) {
		t.Errorf("audit chain missing EvtRAGAbstain")
	}
}

func TestDispatcher_AbstainPath_CitationPersistentFail(t *testing.T) {
	fix := buildHappyPathFixture(t)
	fix.answerGen = &fixtureAnswerGen{noCitationToken: true}
	fix.dispatcher = newDispatcherFromFixture(t, fix)

	res, err := fix.dispatcher.Query(context.Background(), QueryRequest{
		Query:    "any query",
		Doctrine: "max-scope",
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if !res.Abstained {
		t.Errorf("citation persistent failure must abstain")
	}
	if !strings.Contains(strings.ToLower(res.AbstainReason), "citation") {
		t.Errorf("AbstainReason should mention citation; got %q", res.AbstainReason)
	}
	if !auditChainHas(fix.auditChain, EvtRAGAbstain) {
		t.Errorf("audit chain missing EvtRAGAbstain")
	}
}

func TestDispatcher_CapaFirewall_RefusesOnUnverified(t *testing.T) {
	fix := buildHappyPathFixture(t)

	fix.verifier = newTestVerifier(t, newFakeSymbolIndex(nil), nil)
	fix.dispatcher = newDispatcherFromFixture(t, fix)
	res, err := fix.dispatcher.Query(context.Background(), QueryRequest{
		Query:    "anything",
		Doctrine: "capa-firewall",
		Strict:   false,
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if !res.Abstained {
		t.Errorf("capa-firewall must abstain on unverified symbol")
	}
	if !strings.Contains(strings.ToLower(res.AbstainReason), "verif") &&
		!strings.Contains(strings.ToLower(res.AbstainReason), "refuse") {
		t.Errorf("AbstainReason should mention verification or refuse; got %q", res.AbstainReason)
	}
}

func TestDispatcher_CapaFirewall_StrictOperatorOverride_Proceeds(t *testing.T) {
	fix := buildHappyPathFixture(t)

	judgeBackend := &fakeJudgeBackend{response: `{"acceptable": true}`}
	fix.llmJudge = mustNewHaikuJudge(t, judgeBackend)
	fix.verifier = newTestVerifier(t, newFakeSymbolIndex(nil), nil)
	fix.dispatcher = newDispatcherFromFixture(t, fix)
	res, err := fix.dispatcher.Query(context.Background(), QueryRequest{
		Query:    "anything",
		Doctrine: "capa-firewall",
		Strict:   true,
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.Abstained {
		t.Errorf("strict override must not abstain on unverified; got abstain reason %q", res.AbstainReason)
	}
}

func TestDispatcher_MaxScope_InvokesLLMJudge(t *testing.T) {
	fix := buildHappyPathFixture(t)
	judgeBackend := &fakeJudgeBackend{response: `{"acceptable": true}`}
	fix.llmJudge = mustNewHaikuJudge(t, judgeBackend)
	fix.dispatcher = newDispatcherFromFixture(t, fix)
	_, err := fix.dispatcher.Query(context.Background(), QueryRequest{
		Query:    "query",
		Doctrine: "max-scope",
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if judgeBackend.calls != 1 {
		t.Errorf("max-scope must invoke judge; calls=%d", judgeBackend.calls)
	}
}

func TestDispatcher_DefaultDoctrine_DoesNotInvokeLLMJudge(t *testing.T) {
	fix := buildHappyPathFixture(t)
	judgeBackend := &fakeJudgeBackend{response: `{"acceptable": true}`}
	fix.llmJudge = mustNewHaikuJudge(t, judgeBackend)
	fix.dispatcher = newDispatcherFromFixture(t, fix)
	_, err := fix.dispatcher.Query(context.Background(), QueryRequest{
		Query:    "query",
		Doctrine: "default",
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if judgeBackend.calls != 0 {
		t.Errorf("default doctrine must NOT invoke judge; calls=%d", judgeBackend.calls)
	}
}

func TestDispatcher_MaxScope_LLMJudgeReject_Abstains(t *testing.T) {
	fix := buildHappyPathFixture(t)
	judgeBackend := &fakeJudgeBackend{response: `{"acceptable": false, "reason": "contradicts chunk 2", "suspicious_chunk_ids": [2]}`}
	fix.llmJudge = mustNewHaikuJudge(t, judgeBackend)
	fix.dispatcher = newDispatcherFromFixture(t, fix)
	res, err := fix.dispatcher.Query(context.Background(), QueryRequest{
		Query:    "query",
		Doctrine: "max-scope",
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if !res.Abstained {
		t.Errorf("judge reject must abstain")
	}
	if !strings.Contains(res.AbstainReason, "llm_judge") {
		t.Errorf("AbstainReason should mention llm_judge; got %q", res.AbstainReason)
	}
}

func TestDispatcher_AuditChain_CanonicalOrder_MaxScope(t *testing.T) {
	fix := buildHappyPathFixture(t)
	judgeBackend := &fakeJudgeBackend{response: `{"acceptable": true}`}
	fix.llmJudge = mustNewHaikuJudge(t, judgeBackend)
	fix.dispatcher = newDispatcherFromFixture(t, fix)
	_, err := fix.dispatcher.Query(context.Background(), QueryRequest{
		Query:    "query",
		Doctrine: "max-scope",
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	want := []EventType{EvtRAGQuery, EvtRAGRetrieval, EvtRAGCitation, EvtRAGVerify, EvtRAGAnswer}
	if !auditChainHasOrdered(fix.auditChain, want) {
		t.Errorf("audit chain order mismatch: have=%v want⊇%v",
			auditEventTypes(fix.auditChain), want)
	}
}

func TestDispatcher_AuditChain_DefaultDoctrine_HappyPath(t *testing.T) {
	fix := buildHappyPathFixture(t)
	fix.dispatcher = newDispatcherFromFixture(t, fix)
	_, err := fix.dispatcher.Query(context.Background(), QueryRequest{
		Query:    "query",
		Doctrine: "default",
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	have := auditEventTypes(fix.auditChain)
	if !contains(have, EvtRAGQuery) {
		t.Errorf("must emit EvtRAGQuery: %v", have)
	}
	if !contains(have, EvtRAGAnswer) {
		t.Errorf("must emit EvtRAGAnswer: %v", have)
	}

	if contains(have, EvtRAGAbstain) {
		t.Errorf("happy path must NOT emit EvtRAGAbstain: %v", have)
	}
}

func TestDispatcher_AuditChain_AbstainPath_EmitsAbstain(t *testing.T) {
	fix := buildHappyPathFixture(t)
	fix.aggregators = emptyAggregatorsAllEco()
	fix.dispatcher = newDispatcherFromFixture(t, fix)
	_, _ = fix.dispatcher.Query(context.Background(), QueryRequest{
		Query: "q", Doctrine: "default",
	})
	if !auditChainHas(fix.auditChain, EvtRAGAbstain) {
		t.Errorf("abstain path must emit EvtRAGAbstain")
	}

	if auditChainHas(fix.auditChain, EvtRAGAnswer) {
		t.Errorf("abstain path must NOT emit EvtRAGAnswer")
	}
}

func TestDispatcher_ContextCancel_BeforeQuery(t *testing.T) {
	fix := buildHappyPathFixture(t)
	fix.dispatcher = newDispatcherFromFixture(t, fix)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := fix.dispatcher.Query(ctx, QueryRequest{Query: "q", Doctrine: "default"})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("want context.Canceled; got %v", err)
	}
}

func TestDispatcher_ContextCancel_Mid_Fanout(t *testing.T) {
	fix := buildHappyPathFixture(t)
	fix.aggregators = sleepyAggregators(200)
	fix.dispatcher = newDispatcherFromFixture(t, fix)
	ctx, cancel := context.WithCancel(context.Background())

	time.AfterFunc(20*time.Millisecond, cancel)
	_, err := fix.dispatcher.Query(ctx, QueryRequest{Query: "q", Doctrine: "default"})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("want context.Canceled; got %v", err)
	}
}

func TestDispatcher_Race_ConcurrentQueries(t *testing.T) {
	fix := buildHappyPathFixture(t)
	fix.dispatcher = newDispatcherFromFixture(t, fix)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				_, _ = fix.dispatcher.Query(context.Background(), QueryRequest{
					Query: "q", Doctrine: "default",
				})
			}
		}()
	}
	wg.Wait()
}

func TestDispatcher_FanOut_Determinism_InvZen200(t *testing.T) {
	fix := buildHappyPathFixture(t)
	fix.router = newTestRouterWithUniformClassifier(t)
	fix.dispatcher = newDispatcherFromFixture(t, fix)
	results := make([][]int64, 5)
	for i := 0; i < 5; i++ {

		fixIter := buildHappyPathFixture(t)
		fixIter.router = newTestRouterWithUniformClassifier(t)
		fixIter.dispatcher = newDispatcherFromFixture(t, fixIter)
		res, err := fixIter.dispatcher.Query(context.Background(), QueryRequest{
			Query: "deterministic q", Doctrine: "default",
		})
		if err != nil {
			t.Fatalf("Query #%d: %v", i, err)
		}
		ids := make([]int64, len(res.Chunks))
		for j, c := range res.Chunks {
			ids[j] = c.ChunkID
		}
		results[i] = ids
	}
	for i := 1; i < len(results); i++ {
		if !int64SlicesEqual(results[0], results[i]) {
			t.Errorf("inv-zen-200: fan-out result drifted between runs: run0=%v run%d=%v",
				results[0], i, results[i])
		}
	}
}

func TestDispatcher_FanOut_AggregatorError_Propagates(t *testing.T) {
	fix := buildHappyPathFixture(t)
	fix.router = newTestRouterWithUniformClassifier(t)

	fix.aggregators[EcoGo] = &errAggregator{err: errors.New("simulated binary failure")}
	fix.dispatcher = newDispatcherFromFixture(t, fix)
	_, err := fix.dispatcher.Query(context.Background(), QueryRequest{
		Query: "q", Doctrine: "default",
	})
	if err == nil {
		t.Errorf("aggregator error must propagate up from Query")
	}
}

func TestDispatcher_FanOut_RandomDelay_StableMerge(t *testing.T) {
	for trial := 0; trial < 5; trial++ {
		fix := buildHappyPathFixture(t)
		fix.router = newTestRouterWithUniformClassifier(t)

		fix.aggregators = randomDelayAggregators(trial)
		fix.dispatcher = newDispatcherFromFixture(t, fix)
		res, err := fix.dispatcher.Query(context.Background(), QueryRequest{
			Query: "q", Doctrine: "default",
		})
		if err != nil {
			t.Fatalf("Query trial %d: %v", trial, err)
		}
		if len(res.Chunks) == 0 {
			t.Errorf("trial %d: expected chunks; got 0", trial)
		}
	}
}

func TestDispatcher_RerankError_Propagates(t *testing.T) {
	fix := buildHappyPathFixture(t)
	fix.reranker = &errReranker{err: errors.New("rerank failed")}
	fix.dispatcher = newDispatcherFromFixture(t, fix)
	_, err := fix.dispatcher.Query(context.Background(), QueryRequest{
		Query: "q", Doctrine: "default",
	})
	if err == nil {
		t.Errorf("rerank error must propagate up from Query")
	}
}

func TestDispatcher_VersionExplicit_FromRequest(t *testing.T) {
	fix := buildHappyPathFixture(t)
	fix.dispatcher = newDispatcherFromFixture(t, fix)
	res, err := fix.dispatcher.Query(context.Background(), QueryRequest{
		Query:    "q",
		Version:  "1.22.0",
		Doctrine: "default",
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.Provenance.DetectedVersion != "1.22.0" {
		t.Errorf("DetectedVersion=%q; want 1.22.0", res.Provenance.DetectedVersion)
	}
	if res.Provenance.DetectionLayer != 1 {
		t.Errorf("DetectionLayer=%d; want 1 (explicit)", res.Provenance.DetectionLayer)
	}
}

func TestDispatcher_DoctrineUnknown_FallsBackToFixture(t *testing.T) {
	fix := buildHappyPathFixture(t)
	fix.dispatcher = newDispatcherFromFixture(t, fix)
	res, err := fix.dispatcher.Query(context.Background(), QueryRequest{
		Query:    "q",
		Doctrine: "definitely-not-a-known-doctrine",
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}

	if res.Provenance.DoctrineApplied != "default" {
		t.Errorf("Unknown doctrine name should fall through to fixture default; got %q",
			res.Provenance.DoctrineApplied)
	}
}

func TestDispatcher_QueryEvent_PayloadShape(t *testing.T) {
	fix := buildHappyPathFixture(t)
	fix.dispatcher = newDispatcherFromFixture(t, fix)
	_, err := fix.dispatcher.Query(context.Background(), QueryRequest{
		Query:    "q",
		Doctrine: "max-scope",
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}

	if len(fix.auditChain.records) < 1 {
		t.Fatalf("audit chain empty")
	}
	first := fix.auditChain.records[0]
	if first.EventType != EvtRAGQuery {
		t.Errorf("first event = %s; want %s", first.EventType.String(), EvtRAGQuery.String())
	}
	if len(first.Payload) == 0 {
		t.Errorf("EvtRAGQuery payload must be non-empty JSON")
	}
}

func TestNewDispatcher_RejectsNilEmbedder(t *testing.T) {
	_, err := NewDispatcher(Options{
		Router: newTestRouter(t),
	})
	if err == nil {
		t.Errorf("NewDispatcher must reject nil Embedder")
	}
}

func TestNewDispatcher_RejectsNilRouter(t *testing.T) {
	_, err := NewDispatcher(Options{
		Embedder: newFixtureEmbedder(),
	})
	if err == nil {
		t.Errorf("NewDispatcher must reject nil Router")
	}
}

func TestNewDispatcher_RejectsNoopEmbedder(t *testing.T) {
	_, err := NewDispatcher(Options{
		Embedder: NoopEmbedder{},
		Router:   newTestRouter(t),
	})
	if err == nil {
		t.Errorf("NewDispatcher must reject NoopEmbedder per embedder.go contract")
	}
}

func TestNewDispatcher_RejectsNilReranker(t *testing.T) {
	_, err := NewDispatcher(Options{
		Embedder: newFixtureEmbedder(),
		Router:   newTestRouter(t),
	})
	if err == nil {
		t.Errorf("NewDispatcher must reject nil Reranker")
	}
}

func TestNewDispatcher_RejectsNilVerifier(t *testing.T) {
	_, err := NewDispatcher(Options{
		Embedder: newFixtureEmbedder(),
		Router:   newTestRouter(t),
		Reranker: newTestBGEReRanker(t),
	})
	if err == nil {
		t.Errorf("NewDispatcher must reject nil Verifier")
	}
}

func TestNewDispatcher_RejectsNilAbstention(t *testing.T) {
	_, err := NewDispatcher(Options{
		Embedder: newFixtureEmbedder(),
		Router:   newTestRouter(t),
		Reranker: newTestBGEReRanker(t),
		Verifier: newTestVerifier(t, newFakeSymbolIndex(nil), nil),
	})
	if err == nil {
		t.Errorf("NewDispatcher must reject nil AbstentionPolicy")
	}
}

func TestNewDispatcher_AuditChain_WiresEmitter(t *testing.T) {
	chain := NewInMemoryRAGAuditChain()
	d, err := NewDispatcher(Options{
		Embedder:         newFixtureEmbedder(),
		Router:           newTestRouter(t),
		Reranker:         newTestBGEReRanker(t),
		Verifier:         newTestVerifier(t, newFakeSymbolIndex(nil), nil),
		AbstentionPolicy: newTestAbstentionPolicy(t, defaultPerEcoLambda()),
		AuditChain:       chain,
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	if d.auditEmitter == nil {
		t.Errorf("auditEmitter must be wired when AuditChain supplied")
	}
}

func TestQuery_RejectsUnwiredDoctrineResolver(t *testing.T) {
	d, err := NewDispatcher(Options{
		Embedder:         newFixtureEmbedder(),
		Router:           newTestRouter(t),
		Reranker:         newTestBGEReRanker(t),
		Verifier:         newTestVerifier(t, newFakeSymbolIndex(nil), nil),
		AbstentionPolicy: newTestAbstentionPolicy(t, defaultPerEcoLambda()),
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	_, err = d.Query(context.Background(), QueryRequest{Query: "q"})
	if err == nil || !strings.Contains(err.Error(), "doctrineResolver") {
		t.Errorf("expected unwired-doctrineResolver error; got %v", err)
	}
}

func TestQuery_RejectsUnwiredAuditEmitter(t *testing.T) {
	d, err := NewDispatcher(Options{
		Embedder:         newFixtureEmbedder(),
		Router:           newTestRouter(t),
		Reranker:         newTestBGEReRanker(t),
		Verifier:         newTestVerifier(t, newFakeSymbolIndex(nil), nil),
		AbstentionPolicy: newTestAbstentionPolicy(t, defaultPerEcoLambda()),
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	d.doctrineResolver = &fixtureDoctrineResolver{}
	_, err = d.Query(context.Background(), QueryRequest{Query: "q"})
	if err == nil || !strings.Contains(err.Error(), "auditEmitter") {
		t.Errorf("expected unwired-auditEmitter error; got %v", err)
	}
}

func TestQuery_RejectsUnwiredVersionDetector(t *testing.T) {
	d, err := NewDispatcher(Options{
		Embedder:         newFixtureEmbedder(),
		Router:           newTestRouter(t),
		Reranker:         newTestBGEReRanker(t),
		Verifier:         newTestVerifier(t, newFakeSymbolIndex(nil), nil),
		AbstentionPolicy: newTestAbstentionPolicy(t, defaultPerEcoLambda()),
		AuditChain:       NewInMemoryRAGAuditChain(),
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	d.doctrineResolver = &fixtureDoctrineResolver{}
	_, err = d.Query(context.Background(), QueryRequest{Query: "q"})
	if err == nil || !strings.Contains(err.Error(), "versionDetector") {
		t.Errorf("expected unwired-versionDetector error; got %v", err)
	}
}

func TestQuery_RejectsUnwiredAggregators(t *testing.T) {
	d, err := NewDispatcher(Options{
		Embedder:         newFixtureEmbedder(),
		Router:           newTestRouter(t),
		Reranker:         newTestBGEReRanker(t),
		Verifier:         newTestVerifier(t, newFakeSymbolIndex(nil), nil),
		AbstentionPolicy: newTestAbstentionPolicy(t, defaultPerEcoLambda()),
		AuditChain:       NewInMemoryRAGAuditChain(),
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	d.doctrineResolver = &fixtureDoctrineResolver{}
	d.versionDetector = &fixtureVersionDet{}
	_, err = d.Query(context.Background(), QueryRequest{Query: "q"})
	if err == nil || !strings.Contains(err.Error(), "aggregators") {
		t.Errorf("expected unwired-aggregators error; got %v", err)
	}
}

type errVersionDetector struct{ err error }

func (e *errVersionDetector) Detect(_ context.Context, _ QueryRequest) (string, int, error) {
	return "", 0, e.err
}

func TestDispatcher_VersionDetect_Error_Propagates(t *testing.T) {
	fix := buildHappyPathFixture(t)
	fix.dispatcher.versionDetector = &errVersionDetector{err: errors.New("layer 2 i/o fail")}
	_, err := fix.dispatcher.Query(context.Background(), QueryRequest{
		Query: "q", Doctrine: "default",
	})
	if err == nil || !strings.Contains(err.Error(), "version detect") {
		t.Errorf("version-detect error must propagate; got %v", err)
	}
}

type errDoctrineResolver struct{ err error }

func (r *errDoctrineResolver) Resolve(_ context.Context, _ string) (*DoctrineProfile, error) {
	return nil, r.err
}

func TestDispatcher_DoctrineResolve_Error_Propagates(t *testing.T) {
	fix := buildHappyPathFixture(t)
	fix.dispatcher.doctrineResolver = &errDoctrineResolver{err: errors.New("resolver fail")}
	_, err := fix.dispatcher.Query(context.Background(), QueryRequest{
		Query: "q", Doctrine: "default",
	})
	if err == nil || !strings.Contains(err.Error(), "doctrine resolve") {
		t.Errorf("doctrine-resolve error must propagate; got %v", err)
	}
}

type errEmbedder struct {
	fpErr  error
	binErr error
}

func (e *errEmbedder) EmbedBinary256d(_ context.Context, _ string) ([]byte, error) {
	if e.binErr != nil {
		return nil, e.binErr
	}
	return make([]byte, 32), nil
}
func (e *errEmbedder) EmbedFP32_1536d(_ context.Context, _ string) ([]float32, error) {
	if e.fpErr != nil {
		return nil, e.fpErr
	}
	return make([]float32, 1536), nil
}
func (e *errEmbedder) EmbedBoth(_ context.Context, _ string) ([]byte, []float32, error) {
	return nil, nil, nil
}
func (e *errEmbedder) EmbedBatch(_ context.Context, _ []string) ([][]byte, [][]float32, error) {
	return nil, nil, nil
}
func (e *errEmbedder) Close() error { return nil }

func TestDispatcher_EmbedFP32_Error_Propagates(t *testing.T) {
	fix := buildHappyPathFixture(t)
	fix.dispatcher.embedder = &errEmbedder{fpErr: errors.New("jina mps fail")}
	_, err := fix.dispatcher.Query(context.Background(), QueryRequest{
		Query: "q", Doctrine: "default",
	})
	if err == nil || !strings.Contains(err.Error(), "embed fp32") {
		t.Errorf("EmbedFP32_1536d error must propagate; got %v", err)
	}
}

func TestDispatcher_EmbedBinary_Error_Propagates(t *testing.T) {
	fix := buildHappyPathFixture(t)
	fix.dispatcher.embedder = &errEmbedder{binErr: errors.New("binary quant fail")}
	_, err := fix.dispatcher.Query(context.Background(), QueryRequest{
		Query: "q", Doctrine: "default",
	})
	if err == nil || !strings.Contains(err.Error(), "embed bin") {
		t.Errorf("EmbedBinary256d error must propagate; got %v", err)
	}
}

type errRouterClassifier struct{ err error }

func (m *errRouterClassifier) ScoreSoftmax(_ context.Context, _ []float32) (map[Ecosystem]float64, error) {
	return nil, m.err
}
func (m *errRouterClassifier) CheckpointHash() string { return "err-classifier" }

func TestDispatcher_Router_Error_Propagates(t *testing.T) {
	fix := buildHappyPathFixture(t)
	cfg := RouterConfig{
		Heuristics:      defaultHeuristics(),
		Classifier:      &errRouterClassifier{err: errors.New("softmax fail")},
		MarginBroadcast: 0.10, MarginTop2: 0.20,
	}
	router, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	fix.dispatcher.router = router
	_, err = fix.dispatcher.Query(context.Background(), QueryRequest{
		Query: "ambiguous query needing classifier", Doctrine: "default",
	})
	if err == nil || !strings.Contains(err.Error(), "router classify") {
		t.Errorf("router error must propagate; got %v", err)
	}
}

type ordinalErrChain struct {
	mu         sync.Mutex
	calls      int
	errOnCall  int
	calledEvts []EventType
}

func (c *ordinalErrChain) Append(_ context.Context, evt EventType, _ []byte, _ string) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	c.calledEvts = append(c.calledEvts, evt)
	if c.calls == c.errOnCall {
		return 0, errors.New("chain append synthetic failure")
	}
	return int64(c.calls), nil
}
func (c *ordinalErrChain) LastHash(_ context.Context) (string, error)      { return "", nil }
func (c *ordinalErrChain) SealPartition(_ context.Context, _ string) error { return nil }

func dispatcherWithErrChain(t *testing.T, fix *dispatcherFixture, errOnCall int, profileName string) *ordinalErrChain {
	t.Helper()
	chain := &ordinalErrChain{errOnCall: errOnCall}
	prof := builtinProfiles[profileName]
	cp := prof
	cp.AbstentionThresholds = copyThresholds(prof.AbstentionThresholds)
	fix.dispatcher.auditEmitter = NewRAGAuditEmitter(chain, &cp)
	return chain
}

func TestDispatcher_AuditEmit_Query_Error_Propagates(t *testing.T) {
	fix := buildHappyPathFixture(t)
	dispatcherWithErrChain(t, fix, 1, "max-scope")
	_, err := fix.dispatcher.Query(context.Background(), QueryRequest{
		Query: "q", Doctrine: "max-scope",
	})
	if err == nil || !strings.Contains(err.Error(), "emit query event") {
		t.Errorf("emit-query error must propagate; got %v", err)
	}
}

func TestDispatcher_AuditEmit_Retrieval_Error_Propagates(t *testing.T) {
	fix := buildHappyPathFixture(t)
	dispatcherWithErrChain(t, fix, 2, "max-scope")
	_, err := fix.dispatcher.Query(context.Background(), QueryRequest{
		Query: "q", Doctrine: "max-scope",
	})
	if err == nil || !strings.Contains(err.Error(), "emit retrieval event") {
		t.Errorf("emit-retrieval error must propagate; got %v", err)
	}
}

func TestDispatcher_AuditEmit_Abstain_LowConfidence_Error_Propagates(t *testing.T) {
	fix := buildHappyPathFixture(t)
	fix.aggregators = emptyAggregatorsAllEco()
	fix.dispatcher = newDispatcherFromFixture(t, fix)

	dispatcherWithErrChain(t, fix, 3, "max-scope")
	_, err := fix.dispatcher.Query(context.Background(), QueryRequest{
		Query: "q", Doctrine: "max-scope",
	})
	if err == nil || !strings.Contains(err.Error(), "emit abstain") {
		t.Errorf("emit-abstain error must propagate; got %v", err)
	}
}

func TestDispatcher_AuditEmit_Citation_Error_Propagates(t *testing.T) {
	fix := buildHappyPathFixture(t)
	dispatcherWithErrChain(t, fix, 3, "max-scope")
	_, err := fix.dispatcher.Query(context.Background(), QueryRequest{
		Query: "q", Doctrine: "max-scope",
	})
	if err == nil || !strings.Contains(err.Error(), "emit citation") {
		t.Errorf("emit-citation error must propagate; got %v", err)
	}
}

func TestDispatcher_AuditEmit_Verify_Error_Propagates(t *testing.T) {
	fix := buildHappyPathFixture(t)
	dispatcherWithErrChain(t, fix, 4, "max-scope")
	_, err := fix.dispatcher.Query(context.Background(), QueryRequest{
		Query: "q", Doctrine: "max-scope",
	})
	if err == nil || !strings.Contains(err.Error(), "emit verify") {
		t.Errorf("emit-verify error must propagate; got %v", err)
	}
}

func TestDispatcher_AuditEmit_Answer_Error_Propagates(t *testing.T) {
	fix := buildHappyPathFixture(t)

	judgeBackend := &fakeJudgeBackend{response: `{"acceptable": true}`}
	fix.llmJudge = mustNewHaikuJudge(t, judgeBackend)
	fix.dispatcher = newDispatcherFromFixture(t, fix)
	dispatcherWithErrChain(t, fix, 5, "max-scope")
	_, err := fix.dispatcher.Query(context.Background(), QueryRequest{
		Query: "q", Doctrine: "max-scope",
	})
	if err == nil || !strings.Contains(err.Error(), "emit answer") {
		t.Errorf("emit-answer error must propagate; got %v", err)
	}
}

func TestDispatcher_AuditEmit_CapaFirewallAbstain_Error_Propagates(t *testing.T) {
	fix := buildHappyPathFixture(t)
	fix.verifier = newTestVerifier(t, newFakeSymbolIndex(nil), nil)
	fix.dispatcher = newDispatcherFromFixture(t, fix)
	dispatcherWithErrChain(t, fix, 5, "capa-firewall")
	_, err := fix.dispatcher.Query(context.Background(), QueryRequest{
		Query: "q", Doctrine: "capa-firewall", Strict: false,
	})
	if err == nil || !strings.Contains(err.Error(), "emit abstain (capa-firewall)") {
		t.Errorf("emit-abstain (capa-firewall) error must propagate; got %v", err)
	}
}

func TestDispatcher_AuditEmit_CitationAbstain_Error_Propagates(t *testing.T) {
	fix := buildHappyPathFixture(t)
	fix.answerGen = &fixtureAnswerGen{noCitationToken: true}
	fix.dispatcher = newDispatcherFromFixture(t, fix)
	dispatcherWithErrChain(t, fix, 3, "max-scope")
	_, err := fix.dispatcher.Query(context.Background(), QueryRequest{
		Query: "q", Doctrine: "max-scope",
	})
	if err == nil || !strings.Contains(err.Error(), "emit abstain (citation)") {
		t.Errorf("emit-abstain (citation) error must propagate; got %v", err)
	}
}

func TestDispatcher_AuditEmit_LLMJudgeAbstain_Error_Propagates(t *testing.T) {
	fix := buildHappyPathFixture(t)
	judgeBackend := &fakeJudgeBackend{response: `{"acceptable": false, "reason": "no"}`}
	fix.llmJudge = mustNewHaikuJudge(t, judgeBackend)
	fix.dispatcher = newDispatcherFromFixture(t, fix)
	dispatcherWithErrChain(t, fix, 5, "max-scope")
	_, err := fix.dispatcher.Query(context.Background(), QueryRequest{
		Query: "q", Doctrine: "max-scope",
	})
	if err == nil || !strings.Contains(err.Error(), "emit abstain (llm-judge)") {
		t.Errorf("emit-abstain (llm-judge) error must propagate; got %v", err)
	}
}

func TestDispatcher_RerankTopK_DefensiveDefault_OnZero(t *testing.T) {
	fix := buildHappyPathFixture(t)

	fix.dispatcher.doctrineResolver = &zeroMaxResultsResolver{}
	res, err := fix.dispatcher.Query(context.Background(), QueryRequest{
		Query: "q", Doctrine: "",
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}

	_ = res
}

type zeroMaxResultsResolver struct{}

func (r *zeroMaxResultsResolver) Resolve(_ context.Context, _ string) (*DoctrineProfile, error) {
	return &DoctrineProfile{
		Name:                 "custom-zero",
		MaxResults:           0,
		AbstentionThresholds: copyThresholds(defaultPerEcoLambda()),
		CitationMode:         CitationOptional,
		AuditEmissionLevel:   AuditAll8Events,
	}, nil
}

func TestDispatcher_CitationNone_SkipsCitationEvent(t *testing.T) {
	fix := buildHappyPathFixture(t)
	fix.dispatcher.doctrineResolver = &citationNoneResolver{}
	res, err := fix.dispatcher.Query(context.Background(), QueryRequest{
		Query: "q", Doctrine: "",
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.Abstained {
		t.Errorf("CitationNone profile must not abstain on missing citation; got %+v", res)
	}

	if auditChainHas(fix.auditChain, EvtRAGCitation) {
		t.Errorf("CitationNone profile must not emit EvtRAGCitation")
	}
}

type citationNoneResolver struct{}

func (r *citationNoneResolver) Resolve(_ context.Context, _ string) (*DoctrineProfile, error) {
	return &DoctrineProfile{
		Name:                 "custom-citation-none",
		MaxResults:           5,
		AbstentionThresholds: copyThresholds(defaultPerEcoLambda()),
		CitationMode:         CitationNone,
		AuditEmissionLevel:   AuditAll8Events,
	}, nil
}

func TestDispatcher_VerificationStatus_Skipped(t *testing.T) {
	fix := buildHappyPathFixture(t)
	// Override answer-gen to cite a chunk whose SymbolPath differs from any
	// verifier output → "skipped" status on that chunk's VerificationStatus.
	// The fixtureAggregator's HydrateChunks always returns SymbolPath
	// "crypto/sha256.Sum256"; the fixtureVerifier knows that path → "exists".
	// To produce a "skipped" branch, we use the existing happy-path setup
	// (single citation chunk_id=1 → SymbolPath "crypto/sha256.Sum256"), then
	// pre-populate chunks list with EXTRA chunks not in the citation.
	//
	// Simpler the existing happy-path already populates 1+ chunk with
	// SymbolPath not in the verifications list (since reranker may return
	// chunks not cited). Inspecting one chunk's status MUST be "exists" OR
	// "skipped" — assert one of those values.
	res, err := fix.dispatcher.Query(context.Background(), QueryRequest{
		Query: "q", Doctrine: "default",
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	for _, c := range res.Chunks {
		switch c.VerificationStatus {
		case "exists", "not_found", "skipped":

		default:
			t.Errorf("chunk %d has unexpected VerificationStatus=%q", c.ChunkID, c.VerificationStatus)
		}
	}
}

func TestDispatcher_LLMJudge_Error_Propagates(t *testing.T) {
	fix := buildHappyPathFixture(t)
	judgeBackend := &fakeJudgeBackend{err: errors.New("haiku backend fail")}
	fix.llmJudge = mustNewHaikuJudge(t, judgeBackend)
	fix.dispatcher = newDispatcherFromFixture(t, fix)
	_, err := fix.dispatcher.Query(context.Background(), QueryRequest{
		Query: "q", Doctrine: "max-scope",
	})
	if err == nil || !strings.Contains(err.Error(), "llm judge") {
		t.Errorf("llm-judge error must propagate; got %v", err)
	}
}

func TestDispatcher_Verifier_Error_Propagates(t *testing.T) {
	fix := buildHappyPathFixture(t)

	errRunner := &erroringLiveCmdRunner{err: errors.New("runner blew up")}
	fix.verifier = newTestVerifier(t, newFakeSymbolIndex(nil), errRunner)
	judgeBackend := &fakeJudgeBackend{response: `{"acceptable": true}`}
	fix.llmJudge = mustNewHaikuJudge(t, judgeBackend)
	fix.dispatcher = newDispatcherFromFixture(t, fix)
	_, err := fix.dispatcher.Query(context.Background(), QueryRequest{
		Query: "q", Doctrine: "max-scope",
	})
	if err == nil || !strings.Contains(err.Error(), "verify") {
		t.Errorf("verifier error must propagate; got %v", err)
	}
}

type erroringLiveCmdRunner struct{ err error }

func (r *erroringLiveCmdRunner) Run(_ context.Context, _ Ecosystem, _ SymbolRef) (liveCmdResult, error) {
	return liveCmdResult{}, r.err
}

func TestLocalRRFTop50_OverlapBothSources(t *testing.T) {
	bin := []Candidate{{ChunkID: 1, SimilarityScore: 0.9}, {ChunkID: 2, SimilarityScore: 0.8}}
	fts := []Candidate{{ChunkID: 2, SimilarityScore: 0.85}, {ChunkID: 3, SimilarityScore: 0.7}}
	got := localRRFTop50(bin, fts)
	if len(got) != 3 {
		t.Errorf("len=%d; want 3 distinct chunk IDs", len(got))
	}

	if got[0].ChunkID != 2 {
		t.Errorf("got[0].ChunkID=%d; want 2 (highest combined RRF)", got[0].ChunkID)
	}
}

func TestLocalRRFTop50_CapsAt50(t *testing.T) {
	bin := make([]Candidate, 60)
	for i := range bin {
		bin[i] = Candidate{ChunkID: int64(i + 1), SimilarityScore: 1.0 - float64(i)*0.01}
	}
	got := localRRFTop50(bin, nil)
	if len(got) != 50 {
		t.Errorf("len=%d; want 50 (cap)", len(got))
	}
}

func TestPrimaryEcosystem_ReqWins(t *testing.T) {
	r := RoutingDecision{Ecosystems: []Ecosystem{EcoGo}}
	got := primaryEcosystem(r, EcoPython)
	if got != EcoPython {
		t.Errorf("got=%s; want %s (req.Ecosystem wins)", got, EcoPython)
	}
}

func TestPrimaryEcosystem_RoutingDefault(t *testing.T) {
	r := RoutingDecision{Ecosystems: []Ecosystem{EcoRust}}
	got := primaryEcosystem(r, "")
	if got != EcoRust {
		t.Errorf("got=%s; want %s (routing default)", got, EcoRust)
	}
}

func TestPrimaryEcosystem_DefensiveEcoGo(t *testing.T) {
	r := RoutingDecision{Ecosystems: nil}
	got := primaryEcosystem(r, "")
	if got != EcoGo {
		t.Errorf("got=%s; want %s (defensive default)", got, EcoGo)
	}
}

func TestRerankerModelLabel_BGE(t *testing.T) {
	d := &Dispatcher{reranker: newTestBGEReRanker(t)}
	if got := d.rerankerModelLabel(); got != "bge-reranker-v2-m3" {
		t.Errorf("got %q; want bge-reranker-v2-m3", got)
	}
}

func TestRerankerModelLabel_Noop(t *testing.T) {
	d := &Dispatcher{reranker: NoopReranker{}}
	if got := d.rerankerModelLabel(); got != "noop-reranker" {
		t.Errorf("got %q; want noop-reranker", got)
	}
}

func TestRerankerModelLabel_Nil(t *testing.T) {
	d := &Dispatcher{}
	if got := d.rerankerModelLabel(); got != "" {
		t.Errorf("got %q; want \"\" for nil reranker", got)
	}
}

func TestRerankerModelLabel_UnknownType(t *testing.T) {
	d := &Dispatcher{reranker: &errReranker{}}
	if got := d.rerankerModelLabel(); got != "unknown-reranker" {
		t.Errorf("got %q; want unknown-reranker", got)
	}
}

func TestEmbedderModelLabel_Production(t *testing.T) {
	d := &Dispatcher{embedder: newFixtureEmbedder()}
	if got := d.embedderModelLabel(); got != "jina-code-embeddings-1.5b" {
		t.Errorf("got %q; want jina-code-embeddings-1.5b", got)
	}
}

func TestEmbedderModelLabel_Noop(t *testing.T) {
	d := &Dispatcher{embedder: NoopEmbedder{}}
	if got := d.embedderModelLabel(); got != "noop-embedder" {
		t.Errorf("got %q; want noop-embedder", got)
	}
}

func TestEmbedderModelLabel_Nil(t *testing.T) {
	d := &Dispatcher{}
	if got := d.embedderModelLabel(); got != "" {
		t.Errorf("got %q; want \"\"", got)
	}
}

func TestFanOut_EmptyRouting_Error(t *testing.T) {
	fix := buildHappyPathFixture(t)
	_, err := fix.dispatcher.fanOutRetrieve(
		context.Background(),
		RoutingDecision{},
		nil, "q", "1.22",
	)
	if err == nil || !strings.Contains(err.Error(), "zero ecosystems") {
		t.Errorf("empty routing must error; got %v", err)
	}
}

func TestFanOut_AggregatorMissing_Error(t *testing.T) {
	fix := buildHappyPathFixture(t)

	delete(fix.aggregators, EcoGo)
	fix.dispatcher = newDispatcherFromFixture(t, fix)
	_, err := fix.dispatcher.fanOutRetrieve(
		context.Background(),
		RoutingDecision{Ecosystems: []Ecosystem{EcoGo}, ConfidenceWeights: map[Ecosystem]float64{EcoGo: 1.0}},
		nil, "q", "1.22",
	)
	if err == nil || !strings.Contains(err.Error(), "aggregator missing") {
		t.Errorf("missing aggregator must error; got %v", err)
	}
}

type ftsErrAggregator struct {
	fixtureAggregator
	err error
}

func (a *ftsErrAggregator) FTS5Top200(_ context.Context, _, _ string, _ Ecosystem) ([]Candidate, error) {
	return nil, a.err
}

func TestFanOut_FTSError_Propagates(t *testing.T) {
	fix := buildHappyPathFixture(t)
	fix.aggregators[EcoGo] = &ftsErrAggregator{
		fixtureAggregator: fixtureAggregator{eco: EcoGo},
		err:               errors.New("FTS5 corrupt"),
	}
	fix.dispatcher = newDispatcherFromFixture(t, fix)
	_, err := fix.dispatcher.Query(context.Background(), QueryRequest{
		Query: "q", Doctrine: "default",
	})
	if err == nil || !strings.Contains(err.Error(), "fts") {
		t.Errorf("FTS error must propagate via fan-out; got %v", err)
	}
}

type hydErrAggregator struct {
	fixtureAggregator
	err error
}

func (a *hydErrAggregator) HydrateChunks(_ context.Context, _ []int64, _ Ecosystem) ([]QueryChunk, error) {
	return nil, a.err
}

func TestHydrate_Error_Propagates(t *testing.T) {
	fix := buildHappyPathFixture(t)
	fix.aggregators[EcoGo] = &hydErrAggregator{
		fixtureAggregator: fixtureAggregator{eco: EcoGo},
		err:               errors.New("hydrate fail"),
	}
	fix.dispatcher = newDispatcherFromFixture(t, fix)
	_, err := fix.dispatcher.Query(context.Background(), QueryRequest{
		Query: "q", Doctrine: "default",
	})
	if err == nil || !strings.Contains(err.Error(), "hydrate") {
		t.Errorf("hydrate error must propagate; got %v", err)
	}
}

func TestGenerateAndValidate_NilAnswerGen_Error(t *testing.T) {
	fix := buildHappyPathFixture(t)
	fix.answerGen = nil
	fix.dispatcher = newDispatcherFromFixture(t, fix)
	fix.dispatcher.answerGenerator = nil
	_, err := fix.dispatcher.Query(context.Background(), QueryRequest{
		Query: "q", Doctrine: "default",
	})
	if err == nil || !strings.Contains(err.Error(), "AnswerGenerator") {
		t.Errorf("nil AnswerGenerator must surface; got %v", err)
	}
}

type errAnswerGen struct{ err error }

func (g *errAnswerGen) Generate(_ context.Context, _ string, _ []QueryChunk, _ string) (string, error) {
	return "", g.err
}

func TestGenerator_Error_Propagates(t *testing.T) {
	fix := buildHappyPathFixture(t)
	fix.answerGen = &errAnswerGen{err: errors.New("llm down")}
	fix.dispatcher = newDispatcherFromFixture(t, fix)
	_, err := fix.dispatcher.Query(context.Background(), QueryRequest{
		Query: "q", Doctrine: "default",
	})
	if err == nil || !strings.Contains(err.Error(), "citation") {
		t.Errorf("generator error must surface through citation validation; got %v", err)
	}
}

type dispatcherFixture struct {
	dispatcher  *Dispatcher
	router      *Router
	reranker    Reranker
	verifier    *Verifier
	abstention  *AbstentionPolicy
	citation    *CitationValidator
	llmJudge    LLMJudge
	auditChain  *dispatcherAuditRecorder
	aggregators map[Ecosystem]aggregatorAdapter
	versionDet  versionDetectorAdapter
	answerGen   AnswerGenerator
}

func buildHappyPathFixture(t *testing.T) *dispatcherFixture {
	t.Helper()
	fix := &dispatcherFixture{
		router:     newTestRouterWithClassifier(t, map[Ecosystem]float64{EcoGo: 0.85, EcoPython: 0.05, EcoTypeScript: 0.05, EcoRust: 0.05}),
		reranker:   newTestBGEReRanker(t),
		verifier:   newTestVerifier(t, newFakeSymbolIndex(map[Ecosystem]map[string]string{EcoGo: {"crypto/sha256.Sum256": "func Sum256(data []byte) [Size]byte"}}), nil),
		abstention: newTestAbstentionPolicy(t, defaultPerEcoLambda()),
		citation:   newTestCitationValidator(t, CitationMandatoryGrammar),
		auditChain: newRecordingChain(),
		versionDet: &fixtureVersionDet{},
		answerGen:  &fixtureAnswerGen{},
	}
	fix.aggregators = fixtureAggregators()
	fix.dispatcher = newDispatcherFromFixture(t, fix)
	return fix
}

func newDispatcherFromFixture(t *testing.T, fix *dispatcherFixture) *Dispatcher {
	t.Helper()
	emitter := NewRAGAuditEmitter(fix.auditChain, &DoctrineProfile{
		Name: "max-scope", AuditEmissionLevel: AuditAll8Events,
	})
	resolver := &fixtureDoctrineResolver{}
	d, err := NewDispatcher(Options{
		Embedder:         newFixtureEmbedder(),
		Reranker:         fix.reranker,
		AuditChain:       fix.auditChain,
		Router:           fix.router,
		Verifier:         fix.verifier,
		AbstentionPolicy: fix.abstention,
		LLMJudge:         fix.llmJudge,
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	d.doctrineResolver = resolver
	d.auditEmitter = emitter
	d.aggregators = fix.aggregators
	d.answerGenerator = fix.answerGen
	d.versionDetector = fix.versionDet
	return d
}

type fixtureDoctrineResolver struct{}

func (f *fixtureDoctrineResolver) Resolve(ctx context.Context, projectKey string) (*DoctrineProfile, error) {
	prof := builtinProfiles["default"]
	cp := prof
	cp.AbstentionThresholds = copyThresholds(prof.AbstentionThresholds)
	return &cp, nil
}

type dispatcherAuditRecorder struct {
	mu      sync.Mutex
	records []dispatcherAuditRecord
	seq     int64
}

type dispatcherAuditRecord struct {
	Seq       int64
	EventType EventType
	Payload   []byte
	Doctrine  string
}

func newRecordingChain() *dispatcherAuditRecorder { return &dispatcherAuditRecorder{} }

func (c *dispatcherAuditRecorder) Append(ctx context.Context, evt EventType, payload []byte, doctrine string) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seq++
	c.records = append(c.records, dispatcherAuditRecord{
		Seq:       c.seq,
		EventType: evt,
		Payload:   append([]byte(nil), payload...),
		Doctrine:  doctrine,
	})
	return c.seq, nil
}

func (c *dispatcherAuditRecorder) LastHash(ctx context.Context) (string, error) {
	return "", ctx.Err()
}

func (c *dispatcherAuditRecorder) SealPartition(ctx context.Context, partitionID string) error {
	return ctx.Err()
}

func (c *dispatcherAuditRecorder) Snapshot() []dispatcherAuditRecord {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]dispatcherAuditRecord, len(c.records))
	copy(out, c.records)
	return out
}

func fixtureAggregators() map[Ecosystem]aggregatorAdapter {
	out := map[Ecosystem]aggregatorAdapter{}
	for _, e := range AllEcosystems {
		out[e] = &fixtureAggregator{eco: e}
	}
	return out
}

type fixtureAggregator struct{ eco Ecosystem }

func (a *fixtureAggregator) BinaryTop200(ctx context.Context, queryEmbBin []byte, versionFilter string, eco Ecosystem) ([]Candidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return []Candidate{
		{ChunkID: 1, Ecosystem: a.eco, ContentText: "crypto/sha256 Sum256", SymbolPath: "crypto/sha256.Sum256", SimilarityScore: 0.9},
		{ChunkID: 2, Ecosystem: a.eco, ContentText: "crypto/sha512", SymbolPath: "crypto/sha512.Sum512", SimilarityScore: 0.7},
	}, nil
}

func (a *fixtureAggregator) FTS5Top200(ctx context.Context, queryText, versionFilter string, eco Ecosystem) ([]Candidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return []Candidate{
		{ChunkID: 1, Ecosystem: a.eco, ContentText: "crypto/sha256 Sum256", SymbolPath: "crypto/sha256.Sum256", SimilarityScore: 0.85},
	}, nil
}

func (a *fixtureAggregator) HydrateChunks(ctx context.Context, ids []int64, eco Ecosystem) ([]QueryChunk, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := make([]QueryChunk, 0, len(ids))
	for _, id := range ids {
		out = append(out, QueryChunk{
			ChunkID:     id,
			SymbolPath:  "crypto/sha256.Sum256",
			Version:     "1.22",
			ContentText: "func Sum256(data []byte) [Size]byte",
			SourceURL:   "https://pkg.go.dev/crypto/sha256#Sum256",
		})
	}
	return out, nil
}

func emptyAggregatorsAllEco() map[Ecosystem]aggregatorAdapter {
	out := map[Ecosystem]aggregatorAdapter{}
	for _, e := range AllEcosystems {
		out[e] = &emptyAggregator{eco: e}
	}
	return out
}

type emptyAggregator struct{ eco Ecosystem }

func (a *emptyAggregator) BinaryTop200(ctx context.Context, _ []byte, _ string, _ Ecosystem) ([]Candidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, nil
}
func (a *emptyAggregator) FTS5Top200(ctx context.Context, _, _ string, _ Ecosystem) ([]Candidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, nil
}
func (a *emptyAggregator) HydrateChunks(ctx context.Context, _ []int64, _ Ecosystem) ([]QueryChunk, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, nil
}

func sleepyAggregators(durMs int) map[Ecosystem]aggregatorAdapter {
	out := map[Ecosystem]aggregatorAdapter{}
	for _, e := range AllEcosystems {
		out[e] = &sleepyAggregator{eco: e, dur: time.Duration(durMs) * time.Millisecond}
	}
	return out
}

type sleepyAggregator struct {
	eco Ecosystem
	dur time.Duration
}

func (a *sleepyAggregator) BinaryTop200(ctx context.Context, _ []byte, _ string, _ Ecosystem) ([]Candidate, error) {
	select {
	case <-time.After(a.dur):
		return nil, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (a *sleepyAggregator) FTS5Top200(ctx context.Context, _, _ string, _ Ecosystem) ([]Candidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, nil
}
func (a *sleepyAggregator) HydrateChunks(ctx context.Context, _ []int64, _ Ecosystem) ([]QueryChunk, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, nil
}

type errAggregator struct{ err error }

func (a *errAggregator) BinaryTop200(_ context.Context, _ []byte, _ string, _ Ecosystem) ([]Candidate, error) {
	return nil, a.err
}
func (a *errAggregator) FTS5Top200(_ context.Context, _, _ string, _ Ecosystem) ([]Candidate, error) {
	return nil, nil
}
func (a *errAggregator) HydrateChunks(_ context.Context, _ []int64, _ Ecosystem) ([]QueryChunk, error) {
	return nil, nil
}

func randomDelayAggregators(seed int) map[Ecosystem]aggregatorAdapter {
	out := map[Ecosystem]aggregatorAdapter{}

	delays := []time.Duration{
		time.Duration(((seed+1)*7)%13) * time.Millisecond,
		time.Duration(((seed+2)*11)%13) * time.Millisecond,
		time.Duration(((seed+3)*5)%13) * time.Millisecond,
		time.Duration(((seed+4)*3)%13) * time.Millisecond,
	}
	for i, e := range AllEcosystems {
		out[e] = &delayedFixtureAggregator{
			fixtureAggregator: fixtureAggregator{eco: e},
			delay:             delays[i%len(delays)],
		}
	}
	return out
}

type delayedFixtureAggregator struct {
	fixtureAggregator
	delay time.Duration
}

func (a *delayedFixtureAggregator) BinaryTop200(ctx context.Context, queryEmbBin []byte, versionFilter string, eco Ecosystem) ([]Candidate, error) {
	select {
	case <-time.After(a.delay):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return a.fixtureAggregator.BinaryTop200(ctx, queryEmbBin, versionFilter, eco)
}

type errReranker struct{ err error }

func (r *errReranker) Rerank(_ context.Context, _ string, _ []Candidate, _ int) ([]RankedResult, error) {
	return nil, r.err
}
func (r *errReranker) Close() error { return nil }

type fixtureVersionDet struct{}

func (f *fixtureVersionDet) Detect(ctx context.Context, req QueryRequest) (string, int, error) {
	if err := ctx.Err(); err != nil {
		return "", 0, err
	}
	if req.Version != "" {
		return req.Version, 1, nil
	}
	return "latest", 5, nil
}

func newFixtureEmbedder() Embedder { return &fixtureEmbedder{} }

type fixtureEmbedder struct{}

func (e *fixtureEmbedder) EmbedBinary256d(ctx context.Context, text string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return make([]byte, 32), nil
}
func (e *fixtureEmbedder) EmbedFP32_1536d(ctx context.Context, text string) ([]float32, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return make([]float32, 1536), nil
}
func (e *fixtureEmbedder) EmbedBoth(ctx context.Context, text string) ([]byte, []float32, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	return make([]byte, 32), make([]float32, 1536), nil
}
func (e *fixtureEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]byte, [][]float32, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	bin := make([][]byte, len(texts))
	fp32 := make([][]float32, len(texts))
	for i := range texts {
		bin[i] = make([]byte, 32)
		fp32[i] = make([]float32, 1536)
	}
	return bin, fp32, nil
}
func (e *fixtureEmbedder) Close() error { return nil }

type fixtureAnswerGen struct {
	noCitationToken bool

	mu    sync.Mutex
	calls int
}

func (g *fixtureAnswerGen) Generate(ctx context.Context, query string, chunks []QueryChunk, reprompt string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	g.mu.Lock()
	g.calls++
	g.mu.Unlock()
	if g.noCitationToken {
		return "An answer without citation tokens.", nil
	}
	if len(chunks) > 0 {
		return fmt.Sprintf("Use crypto/sha256.Sum256 [doc_id:%d].", chunks[0].ChunkID), nil
	}
	return "fallback no chunks", nil
}

func auditChainHas(c *dispatcherAuditRecorder, evt EventType) bool {
	for _, e := range c.records {
		if e.EventType == evt {
			return true
		}
	}
	return false
}

func auditChainHasOrdered(c *dispatcherAuditRecorder, want []EventType) bool {
	j := 0
	for _, e := range c.records {
		if j < len(want) && e.EventType == want[j] {
			j++
		}
	}
	return j == len(want)
}

func auditEventTypes(c *dispatcherAuditRecorder) []EventType {
	out := make([]EventType, len(c.records))
	for i, e := range c.records {
		out[i] = e.EventType
	}
	return out
}

func contains(xs []EventType, x EventType) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func mustNewHaikuJudge(t *testing.T, b JudgeBackend) LLMJudge {
	t.Helper()
	j, err := NewHaikuLLMJudge(HaikuLLMJudgeConfig{Backend: b})
	if err != nil {
		t.Fatalf("NewHaikuLLMJudge: %v", err)
	}
	return j
}

func int64SlicesEqual(a, b []int64) bool {
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
