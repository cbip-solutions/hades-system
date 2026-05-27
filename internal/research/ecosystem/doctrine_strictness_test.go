// internal/research/ecosystem/doctrine_strictness_test.go
//
//
// invariant: Doctrine strictness knob honored — max-scope chains all 8
// events; default chains 4 (Query+Abstain+Verify-failure+Answer); capa-
// firewall chains all 8 + refuses on unverified.
//
// D-9 wired the per-step gates in Dispatcher.Query (LLMJudgeEnabled at
// step 13, RefuseOnUnverified at step 12, CitationMode at step 10,
// AbstentionThresholds at step 9, MaxResults at step 8). D-12 wired the
// emit-level filter in RAGAuditEmitter.shouldEmit. D-13 closes the
// remaining gaps for FULL invariant enforcement:
//
// 1. Per-query audit-emitter profile rebind. D-9 binds the RAGAuditEmitter
// to the `default` profile at NewDispatcher time; D-13 wires per-Query
// rebind via RAGAuditEmitter.SetProfile so the active doctrine's
// filter applies (otherwise max-scope queries silently drop
// EvtRAGRetrieval + EvtRAGCitation).
//
// 2. Verifier SkipStageC per doctrine. Dispatcher.verifyWithDoctrine
// constructs a shadow Verifier with SkipStageC=true for default
// doctrine (latency budget); max-scope + capa-firewall retain full
// 3-stage verify.
//
// 3. Property-shape determinism per doctrine. Each doctrine yields a
// deterministic audit chain for a given happy-path query.
//
// Coverage discipline: per project doctrine `feedback_no_tech_debt.md`,
// security/correctness-critical files require ≥90% per-function
// coverage. invariant governs hallucination-mitigation behaviour — a
// silently-dropped audit event weakens tamper-detection guarantees AND
// downstream replay ( property tests reconstruct queries from
// the audit chain).

package ecosystem

import (
	"context"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

func TestInvZen205_BuiltinProfile_MaxScope(t *testing.T) {
	p, ok := builtinProfiles["max-scope"]
	if !ok {
		t.Fatalf("builtinProfiles must contain max-scope")
	}
	if p.MaxResults != 10 {
		t.Errorf("max-scope MaxResults=%d; want 10", p.MaxResults)
	}
	if !p.LLMJudgeEnabled {
		t.Errorf("max-scope LLMJudgeEnabled=false; want true")
	}
	if p.RefuseOnUnverified {
		t.Errorf("max-scope RefuseOnUnverified=true; want false")
	}
	if p.CitationMode != CitationMandatoryGrammar {
		t.Errorf("max-scope CitationMode=%q; want %q", p.CitationMode, CitationMandatoryGrammar)
	}
	if p.AuditEmissionLevel != AuditAll8Events {
		t.Errorf("max-scope AuditEmissionLevel=%q; want %q",
			p.AuditEmissionLevel, AuditAll8Events)
	}
	wantThresh := map[Ecosystem]float64{
		EcoGo: 0.3, EcoPython: 0.5, EcoTypeScript: 0.8, EcoRust: 0.4,
	}
	for eco, want := range wantThresh {
		if got := p.AbstentionThresholds[eco]; got != want {
			t.Errorf("max-scope λ[%s]=%v; want %v", eco, got, want)
		}
	}
}

func TestInvZen205_BuiltinProfile_Default(t *testing.T) {
	p, ok := builtinProfiles["default"]
	if !ok {
		t.Fatalf("builtinProfiles must contain default")
	}
	if p.MaxResults != 5 {
		t.Errorf("default MaxResults=%d; want 5", p.MaxResults)
	}
	if p.LLMJudgeEnabled {
		t.Errorf("default LLMJudgeEnabled=true; want false")
	}
	if p.RefuseOnUnverified {
		t.Errorf("default RefuseOnUnverified=true; want false")
	}
	if p.AuditEmissionLevel != AuditQueryAbstainVerifyFailureAnswer {
		t.Errorf("default AuditEmissionLevel=%q; want %q",
			p.AuditEmissionLevel, AuditQueryAbstainVerifyFailureAnswer)
	}
	wantThresh := map[Ecosystem]float64{
		EcoGo: 0.45, EcoPython: 0.75, EcoTypeScript: 1.2, EcoRust: 0.6,
	}
	for eco, want := range wantThresh {
		got := p.AbstentionThresholds[eco]
		if absf(got-want) > 1e-9 {
			t.Errorf("default λ[%s]=%v; want %v (×1.5)", eco, got, want)
		}
	}
}

func TestInvZen205_BuiltinProfile_CapaFirewall(t *testing.T) {
	p, ok := builtinProfiles["capa-firewall"]
	if !ok {
		t.Fatalf("builtinProfiles must contain capa-firewall")
	}
	if p.MaxResults != 10 {
		t.Errorf("capa-firewall MaxResults=%d; want 10", p.MaxResults)
	}
	if !p.LLMJudgeEnabled {
		t.Errorf("capa-firewall LLMJudgeEnabled=false; want true")
	}
	if !p.RefuseOnUnverified {
		t.Errorf("capa-firewall RefuseOnUnverified=false; want true")
	}
	if p.CitationMode != CitationMandatoryGrammar {
		t.Errorf("capa-firewall CitationMode=%q; want %q",
			p.CitationMode, CitationMandatoryGrammar)
	}
	if p.AuditEmissionLevel != AuditAll8Events {
		t.Errorf("capa-firewall AuditEmissionLevel=%q; want %q",
			p.AuditEmissionLevel, AuditAll8Events)
	}
	wantThresh := map[Ecosystem]float64{
		EcoGo: 0.6, EcoPython: 1.0, EcoTypeScript: 1.6, EcoRust: 0.8,
	}
	for eco, want := range wantThresh {
		got := p.AbstentionThresholds[eco]
		if absf(got-want) > 1e-9 {
			t.Errorf("capa-firewall λ[%s]=%v; want %v (×2.0)", eco, got, want)
		}
	}
}

func TestInvZen205_LambdaRatios_FrozenContract(t *testing.T) {
	base := builtinProfiles["max-scope"].AbstentionThresholds
	mid := builtinProfiles["default"].AbstentionThresholds
	high := builtinProfiles["capa-firewall"].AbstentionThresholds
	for _, eco := range AllEcosystems {
		if got, want := mid[eco], base[eco]*1.5; absf(got-want) > 1e-9 {
			t.Errorf("default λ[%s]=%v not 1.5× baseline %v (mid drift)",
				eco, got, want)
		}
		if got, want := high[eco], base[eco]*2.0; absf(got-want) > 1e-9 {
			t.Errorf("capa-firewall λ[%s]=%v not 2.0× baseline %v (high drift)",
				eco, got, want)
		}
	}
}

func TestInvZen205_AuditEmitter_SetProfile_RebindsLevel(t *testing.T) {
	chain := NewInMemoryRAGAuditChain()
	initial := builtinProfiles["default"]
	e := NewRAGAuditEmitter(chain, &initial)

	ctx := context.Background()

	seq, err := e.Emit(ctx, eventlog.EvtRAGRetrieval, RAGRetrievalPayload{FusedCount: 1})
	if err != nil {
		t.Fatalf("Emit(EvtRAGRetrieval) under default: %v", err)
	}
	if seq != 0 {
		t.Errorf("under default, EvtRAGRetrieval must short-circuit (seq=0); got seq=%d", seq)
	}
	if got := chain.Len(); got != 0 {
		t.Errorf("under default, chain.Len()=%d after Retrieval emit; want 0", got)
	}

	maxScope := builtinProfiles["max-scope"]
	e.SetProfile(&maxScope)

	seq, err = e.Emit(ctx, eventlog.EvtRAGRetrieval, RAGRetrievalPayload{FusedCount: 1})
	if err != nil {
		t.Fatalf("Emit(EvtRAGRetrieval) under max-scope: %v", err)
	}
	if seq == 0 {
		t.Errorf("under max-scope, EvtRAGRetrieval must emit (seq>0); got seq=0")
	}
	if got := chain.Len(); got != 1 {
		t.Errorf("under max-scope, chain.Len()=%d after Retrieval emit; want 1", got)
	}
}

func TestInvZen205_AuditEmitter_SetProfile_NilPanics(t *testing.T) {
	chain := NewInMemoryRAGAuditChain()
	initial := builtinProfiles["default"]
	e := NewRAGAuditEmitter(chain, &initial)
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("SetProfile(nil): want panic; got none")
		}
	}()
	e.SetProfile(nil)
}

func TestInvZen205_AuditEmitter_SetProfile_UsesNewProfileForDoctrineLabel(t *testing.T) {
	chain := NewInMemoryRAGAuditChain()
	initial := builtinProfiles["default"]
	e := NewRAGAuditEmitter(chain, &initial)

	rebind := builtinProfiles["max-scope"]
	e.SetProfile(&rebind)

	_, err := e.Emit(context.Background(), eventlog.EvtRAGQuery, RAGQueryPayload{Query: "q"})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if chain.Len() != 1 {
		t.Fatalf("chain.Len()=%d; want 1", chain.Len())
	}
	rec := chain.Get(1)
	if rec == nil {
		t.Fatalf("Get(1) returned nil")
	}
	if rec.Doctrine != "max-scope" {
		t.Errorf("chain doctrine=%q; want %q (rebind not propagated)",
			rec.Doctrine, "max-scope")
	}
}

func TestInvZen205_Dispatcher_AuditEmitterRebindsToActiveDoctrine(t *testing.T) {
	fix := buildHappyPathFixture(t)
	chain := newRecordingChain()

	judgeBackend := &fakeJudgeBackend{response: `{"acceptable": true}`}
	fix.llmJudge = mustNewHaikuJudge(t, judgeBackend)

	d, err := NewDispatcher(Options{
		Embedder:         newFixtureEmbedder(),
		Reranker:         fix.reranker,
		AuditChain:       chain,
		Router:           fix.router,
		Verifier:         fix.verifier,
		AbstentionPolicy: fix.abstention,
		LLMJudge:         fix.llmJudge,
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	d.doctrineResolver = &fixtureDoctrineResolver{}
	d.aggregators = fix.aggregators
	d.answerGenerator = fix.answerGen
	d.versionDetector = fix.versionDet

	_, err = d.Query(context.Background(), QueryRequest{
		Query:    "rebind probe",
		Doctrine: "max-scope",
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}

	have := chainEventTypesFromRecording(chain)
	required := []EventType{
		eventlog.EvtRAGQuery,
		eventlog.EvtRAGRetrieval,
		eventlog.EvtRAGCitation,
		eventlog.EvtRAGVerify,
		eventlog.EvtRAGAnswer,
	}
	for _, evt := range required {
		if !recordingHasEvent(chain, evt) {
			t.Errorf("max-scope chain missing %s (inv-zen-205 rebind failure); have=%v",
				evt.String(), eventTypeNames(have))
		}
	}
}

func TestInvZen205_Dispatcher_AuditEmitterStaysDefaultUnderDefault(t *testing.T) {
	fix := buildHappyPathFixture(t)
	chain := newRecordingChain()

	d, err := NewDispatcher(Options{
		Embedder:         newFixtureEmbedder(),
		Reranker:         fix.reranker,
		AuditChain:       chain,
		Router:           fix.router,
		Verifier:         fix.verifier,
		AbstentionPolicy: fix.abstention,
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	d.doctrineResolver = &fixtureDoctrineResolver{}
	d.aggregators = fix.aggregators
	d.answerGenerator = fix.answerGen
	d.versionDetector = fix.versionDet

	_, err = d.Query(context.Background(), QueryRequest{
		Query:    "filter probe",
		Doctrine: "default",
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}

	if recordingHasEvent(chain, eventlog.EvtRAGRetrieval) {
		t.Errorf("default doctrine must filter EvtRAGRetrieval; chain=%v",
			eventTypeNames(chainEventTypesFromRecording(chain)))
	}
	if recordingHasEvent(chain, eventlog.EvtRAGCitation) {
		t.Errorf("default doctrine must filter EvtRAGCitation; chain=%v",
			eventTypeNames(chainEventTypesFromRecording(chain)))
	}
	if !recordingHasEvent(chain, eventlog.EvtRAGQuery) {
		t.Errorf("default doctrine must emit EvtRAGQuery")
	}
	if !recordingHasEvent(chain, eventlog.EvtRAGAnswer) {
		t.Errorf("default doctrine must emit EvtRAGAnswer")
	}
}

func TestInvZen205_Verifier_SkipStageC_UnderDefaultDoctrine(t *testing.T) {
	fix := buildHappyPathFixture(t)
	rec := &recordingLiveCmdRunner{}
	fix.verifier = newTestVerifierWithRunner(t, newFakeSymbolIndex(nil), rec)
	fix.dispatcher = newDispatcherFromFixture(t, fix)

	_, _ = fix.dispatcher.Query(context.Background(), QueryRequest{
		Query: "verifier-default", Doctrine: "default",
	})
	if rec.calls != 0 {
		t.Errorf("under default doctrine, LiveCmdRunner.Run must NOT be called (SkipStageC=true); calls=%d",
			rec.calls)
	}
}

func TestInvZen205_Verifier_StageC_UnderMaxScopeDoctrine(t *testing.T) {
	fix := buildHappyPathFixture(t)
	rec := &recordingLiveCmdRunner{}
	fix.verifier = newTestVerifierWithRunner(t, newFakeSymbolIndex(nil), rec)
	judgeBackend := &fakeJudgeBackend{response: `{"acceptable": true}`}
	fix.llmJudge = mustNewHaikuJudge(t, judgeBackend)
	fix.dispatcher = newDispatcherFromFixture(t, fix)

	_, _ = fix.dispatcher.Query(context.Background(), QueryRequest{
		Query: "verifier-maxscope", Doctrine: "max-scope",
	})
	if rec.calls == 0 {
		t.Errorf("under max-scope, LiveCmdRunner.Run must be called for unknown symbols; calls=0")
	}
}

func TestInvZen205_Verifier_StageC_UnderCapaFirewallDoctrine(t *testing.T) {
	fix := buildHappyPathFixture(t)
	rec := &recordingLiveCmdRunner{}
	fix.verifier = newTestVerifierWithRunner(t, newFakeSymbolIndex(nil), rec)
	fix.dispatcher = newDispatcherFromFixture(t, fix)
	_, _ = fix.dispatcher.Query(context.Background(), QueryRequest{
		Query:    "verifier-capa",
		Doctrine: "capa-firewall",
	})
	if rec.calls == 0 {
		t.Errorf("under capa-firewall, LiveCmdRunner.Run must be called (refuse decision needs verify result); calls=0")
	}
}

func TestInvZen205_DoctrineKnob_PropertyTest_DeterministicChain(t *testing.T) {
	cases := []struct {
		doctrine string
		want     []EventType
	}{
		{
			doctrine: "max-scope",
			want: []EventType{
				eventlog.EvtRAGQuery,
				eventlog.EvtRAGRetrieval,
				eventlog.EvtRAGCitation,
				eventlog.EvtRAGVerify,
				eventlog.EvtRAGAnswer,
			},
		},
		{
			doctrine: "default",

			want: []EventType{
				eventlog.EvtRAGQuery,
				eventlog.EvtRAGVerify,
				eventlog.EvtRAGAnswer,
			},
		},
		{
			doctrine: "capa-firewall",
			want: []EventType{
				eventlog.EvtRAGQuery,
				eventlog.EvtRAGRetrieval,
				eventlog.EvtRAGCitation,
				eventlog.EvtRAGVerify,
				eventlog.EvtRAGAnswer,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.doctrine, func(t *testing.T) {
			fix := buildHappyPathFixture(t)
			chain := newRecordingChain()
			judgeBackend := &fakeJudgeBackend{response: `{"acceptable": true}`}
			fix.llmJudge = mustNewHaikuJudge(t, judgeBackend)

			d, err := NewDispatcher(Options{
				Embedder:         newFixtureEmbedder(),
				Reranker:         fix.reranker,
				AuditChain:       chain,
				Router:           fix.router,
				Verifier:         fix.verifier,
				AbstentionPolicy: fix.abstention,
				LLMJudge:         fix.llmJudge,
			})
			if err != nil {
				t.Fatalf("NewDispatcher: %v", err)
			}
			d.doctrineResolver = &fixtureDoctrineResolver{}
			d.aggregators = fix.aggregators
			d.answerGenerator = fix.answerGen
			d.versionDetector = fix.versionDet

			_, err = d.Query(context.Background(), QueryRequest{
				Query: "property", Doctrine: tc.doctrine,
			})
			if err != nil {
				t.Fatalf("Query under %s: %v", tc.doctrine, err)
			}
			got := chainEventTypesFromRecording(chain)
			if !equalEventTypeList(got, tc.want) {
				t.Errorf("doctrine=%s chain mismatch:\n  got  = %v\n  want = %v",
					tc.doctrine, eventTypeNames(got), eventTypeNames(tc.want))
			}
		})
	}
}

func TestInvZen205_DoctrineKnob_PropertyTest_DeterministicChain_TwoRuns(t *testing.T) {
	doctrines := []string{"max-scope", "default", "capa-firewall"}
	for _, doctrine := range doctrines {
		t.Run(doctrine, func(t *testing.T) {
			run := func() []EventType {
				fix := buildHappyPathFixture(t)
				chain := newRecordingChain()
				if doctrine == "max-scope" {
					judgeBackend := &fakeJudgeBackend{response: `{"acceptable": true}`}
					fix.llmJudge = mustNewHaikuJudge(t, judgeBackend)
				}
				d, err := NewDispatcher(Options{
					Embedder:         newFixtureEmbedder(),
					Reranker:         fix.reranker,
					AuditChain:       chain,
					Router:           fix.router,
					Verifier:         fix.verifier,
					AbstentionPolicy: fix.abstention,
					LLMJudge:         fix.llmJudge,
				})
				if err != nil {
					t.Fatalf("NewDispatcher: %v", err)
				}
				d.doctrineResolver = &fixtureDoctrineResolver{}
				d.aggregators = fix.aggregators
				d.answerGenerator = fix.answerGen
				d.versionDetector = fix.versionDet
				_, _ = d.Query(context.Background(), QueryRequest{Query: "p", Doctrine: doctrine})
				return chainEventTypesFromRecording(chain)
			}
			first := run()
			second := run()
			if !equalEventTypeList(first, second) {
				t.Errorf("doctrine=%s non-deterministic chain:\n  run1 = %v\n  run2 = %v",
					doctrine, eventTypeNames(first), eventTypeNames(second))
			}
		})
	}
}

func TestInvZen205_CapaFirewall_RefuseChainShape(t *testing.T) {
	fix := buildHappyPathFixture(t)
	chain := newRecordingChain()
	fix.verifier = newTestVerifier(t, newFakeSymbolIndex(nil), nil)

	d, err := NewDispatcher(Options{
		Embedder:         newFixtureEmbedder(),
		Reranker:         fix.reranker,
		AuditChain:       chain,
		Router:           fix.router,
		Verifier:         fix.verifier,
		AbstentionPolicy: fix.abstention,
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	d.doctrineResolver = &fixtureDoctrineResolver{}
	d.aggregators = fix.aggregators
	d.answerGenerator = fix.answerGen
	d.versionDetector = fix.versionDet

	res, err := d.Query(context.Background(), QueryRequest{
		Query: "refuse-shape", Doctrine: "capa-firewall", Strict: false,
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if !res.Abstained {
		t.Errorf("capa-firewall must abstain on unverified; got Abstained=false")
	}
	want := []EventType{
		eventlog.EvtRAGQuery,
		eventlog.EvtRAGRetrieval,
		eventlog.EvtRAGCitation,
		eventlog.EvtRAGVerify,
		eventlog.EvtRAGAbstain,
	}
	got := chainEventTypesFromRecording(chain)
	if !equalEventTypeList(got, want) {
		t.Errorf("capa-firewall refuse chain mismatch:\n  got  = %v\n  want = %v",
			eventTypeNames(got), eventTypeNames(want))
	}
	if recordingHasEvent(chain, eventlog.EvtRAGAnswer) {
		t.Errorf("capa-firewall refuse must not emit EvtRAGAnswer; chain=%v",
			eventTypeNames(got))
	}
	if !strings.Contains(strings.ToLower(res.AbstainReason), "capa-firewall") &&
		!strings.Contains(strings.ToLower(res.AbstainReason), "refuse") {
		t.Errorf("AbstainReason must mention capa-firewall/refuse; got %q", res.AbstainReason)
	}
}

type recordingLiveCmdRunner struct {
	calls int
}

func (r *recordingLiveCmdRunner) Run(ctx context.Context, eco Ecosystem, ref SymbolRef) (liveCmdResult, error) {
	r.calls++
	return liveCmdResult{Exists: false}, nil
}

func newTestVerifierWithRunner(t *testing.T, si SymbolIndexLookup, runner LiveCmdRunner) *Verifier {
	t.Helper()
	v, err := NewVerifier(VerifierConfig{
		SymbolIndex:   si,
		LiveCmdRunner: runner,
	})
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	return v
}

func chainEventTypesFromRecording(c *dispatcherAuditRecorder) []EventType {
	snap := c.Snapshot()
	out := make([]EventType, 0, len(snap))
	for _, r := range snap {
		out = append(out, r.EventType)
	}
	return out
}

func recordingHasEvent(c *dispatcherAuditRecorder, evt EventType) bool {
	for _, r := range c.Snapshot() {
		if r.EventType == evt {
			return true
		}
	}
	return false
}

func equalEventTypeList(a, b []EventType) bool {
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

func eventTypeNames(es []EventType) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.String()
	}
	return out
}
