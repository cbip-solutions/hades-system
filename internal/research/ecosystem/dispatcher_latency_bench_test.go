package ecosystem

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"
)

type benchSetup struct {
	once                sync.Once
	happyDispatcher     *Dispatcher
	broadcastDispatcher *Dispatcher
	emptyDispatcher     *Dispatcher
	maxScopeDispatcher  *Dispatcher
}

var benchSetupSingleton benchSetup

func (s *benchSetup) init(tb testing.TB) {
	tb.Helper()
	s.once.Do(func() {

		s.happyDispatcher = newBenchDispatcher(tb, benchDispatcherCfg{
			classifierScores: map[Ecosystem]float64{
				EcoGo: 0.85, EcoPython: 0.05, EcoTypeScript: 0.05, EcoRust: 0.05,
			},
			aggregatorFactory: fixtureAggregators,
		})

		s.broadcastDispatcher = newBenchDispatcher(tb, benchDispatcherCfg{
			classifierScores: map[Ecosystem]float64{
				EcoGo: 0.25, EcoPython: 0.25, EcoTypeScript: 0.25, EcoRust: 0.25,
			},
			aggregatorFactory: fixtureAggregators,
		})

		s.emptyDispatcher = newBenchDispatcher(tb, benchDispatcherCfg{
			classifierScores: map[Ecosystem]float64{
				EcoGo: 0.85, EcoPython: 0.05, EcoTypeScript: 0.05, EcoRust: 0.05,
			},
			aggregatorFactory: emptyAggregatorsAllEco,
		})

		s.maxScopeDispatcher = newBenchDispatcher(tb, benchDispatcherCfg{
			classifierScores: map[Ecosystem]float64{
				EcoGo: 0.85, EcoPython: 0.05, EcoTypeScript: 0.05, EcoRust: 0.05,
			},
			aggregatorFactory: fixtureAggregators,
			llmJudgeBackend:   &fakeJudgeBackend{response: `{"acceptable": true}`},
		})
	})
}

type benchDispatcherCfg struct {
	classifierScores  map[Ecosystem]float64
	aggregatorFactory func() map[Ecosystem]aggregatorAdapter
	llmJudgeBackend   *fakeJudgeBackend
}

func newBenchDispatcher(tb testing.TB, cfg benchDispatcherCfg) *Dispatcher {
	tb.Helper()

	router, err := NewRouter(RouterConfig{
		Heuristics:      defaultHeuristics(),
		Classifier:      newFixedScoresClassifier(cfg.classifierScores),
		MarginBroadcast: 0.10,
		MarginTop2:      0.20,
	})
	if err != nil {
		tb.Fatalf("NewRouter: %v", err)
	}

	reranker, err := NewBGEReRankerV2M3(BGEConfig{
		Backend:      BGEBackendMock,
		MaxLatencyMs: 300,
	})
	if err != nil {
		tb.Fatalf("NewBGEReRankerV2M3: %v", err)
	}

	symIdx := newFakeSymbolIndex(map[Ecosystem]map[string]string{
		EcoGo: {"crypto/sha256.Sum256": "func Sum256(data []byte) [Size]byte"},
	})
	verifier, err := NewVerifier(VerifierConfig{
		SymbolIndex:   symIdx,
		LiveCmdRunner: nil,
		LRUSize:       1024,
		LRUTTL:        24 * time.Hour,
		Clock:         benchSystemClock{},
	})
	if err != nil {
		tb.Fatalf("NewVerifier: %v", err)
	}

	abstention, err := NewAbstentionPolicy(AbstentionConfig{
		PerEcoLambda: defaultPerEcoLambda(),
	})
	if err != nil {
		tb.Fatalf("NewAbstentionPolicy: %v", err)
	}

	auditChain := newRecordingChain()

	var llmJudge LLMJudge
	if cfg.llmJudgeBackend != nil {
		llmJudge, err = NewHaikuLLMJudge(HaikuLLMJudgeConfig{
			Backend: cfg.llmJudgeBackend,
		})
		if err != nil {
			tb.Fatalf("NewHaikuLLMJudge: %v", err)
		}
	}

	d, err := NewDispatcher(Options{
		Embedder:         newFixtureEmbedder(),
		Reranker:         reranker,
		Router:           router,
		Verifier:         verifier,
		AbstentionPolicy: abstention,
		LLMJudge:         llmJudge,
		AuditChain:       auditChain,
	})
	if err != nil {
		tb.Fatalf("NewDispatcher: %v", err)
	}

	d.doctrineResolver = &fixtureDoctrineResolver{}
	d.auditEmitter = NewRAGAuditEmitter(auditChain, &DoctrineProfile{
		Name: "max-scope", AuditEmissionLevel: AuditAll8Events,
	})
	d.aggregators = cfg.aggregatorFactory()
	d.answerGenerator = &fixtureAnswerGen{}
	d.versionDetector = &fixtureVersionDet{}
	return d
}

type benchSystemClock struct{}

func (benchSystemClock) Now() time.Time { return time.Now() }

func BenchmarkDispatcher_Query_HappyPath(b *testing.B) {
	benchSetupSingleton.init(b)
	ctx := context.Background()
	req := QueryRequest{
		Query:    "how do I create a SHA-256 hash in Go?",
		Doctrine: "default",
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := benchSetupSingleton.happyDispatcher.Query(ctx, req)
		if err != nil {
			b.Fatalf("Query: %v", err)
		}
	}
}

func BenchmarkDispatcher_Query_BroadcastFanOut(b *testing.B) {
	benchSetupSingleton.init(b)
	ctx := context.Background()
	req := QueryRequest{
		Query:    "ambiguous cross-eco broadcast benchmark query",
		Doctrine: "default",
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := benchSetupSingleton.broadcastDispatcher.Query(ctx, req)
		if err != nil {
			b.Fatalf("Query: %v", err)
		}
	}
}

func BenchmarkDispatcher_Query_AbstainPath(b *testing.B) {
	benchSetupSingleton.init(b)
	ctx := context.Background()
	req := QueryRequest{
		Query:    "abstain-path benchmark query",
		Doctrine: "default",
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := benchSetupSingleton.emptyDispatcher.Query(ctx, req)
		if err != nil {
			b.Fatalf("Query: %v", err)
		}
	}
}

func BenchmarkDispatcher_Query_DefaultDoctrine(b *testing.B) {
	benchSetupSingleton.init(b)
	ctx := context.Background()
	req := QueryRequest{
		Query:    "benchmark query",
		Doctrine: "default",
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := benchSetupSingleton.happyDispatcher.Query(ctx, req)
		if err != nil {
			b.Fatalf("Query: %v", err)
		}
	}
}

func BenchmarkDispatcher_Query_MaxScopeDoctrine(b *testing.B) {
	benchSetupSingleton.init(b)
	ctx := context.Background()
	req := QueryRequest{
		Query:    "benchmark query",
		Doctrine: "max-scope",
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := benchSetupSingleton.maxScopeDispatcher.Query(ctx, req)
		if err != nil {
			b.Fatalf("Query: %v", err)
		}
	}
}

func TestBenchmarkLatencyBudget_P50P95(t *testing.T) {
	if testing.Short() {
		t.Skip("perf; skip -short")
	}

	d := newBenchDispatcher(t, benchDispatcherCfg{
		classifierScores: map[Ecosystem]float64{
			EcoGo: 0.85, EcoPython: 0.05, EcoTypeScript: 0.05, EcoRust: 0.05,
		},
		aggregatorFactory: fixtureAggregators,
	})
	const trials = 200
	durations := make([]time.Duration, trials)
	for i := 0; i < trials; i++ {
		start := time.Now()
		_, err := d.Query(context.Background(), QueryRequest{
			Query:    "latency budget probe",
			Doctrine: "default",
		})
		if err != nil {
			t.Fatalf("trial %d: %v", i, err)
		}
		durations[i] = time.Since(start)
	}
	p50 := percentileDurationBench(durations, 0.50)
	p95 := percentileDurationBench(durations, 0.95)
	t.Logf("dispatcher latency budget probe: p50=%v p95=%v over %d trials", p50, p95, trials)
	if p50 > 350*time.Millisecond {
		t.Errorf("spec §4.7 latency: P50 %v > 350ms budget", p50)
	}
	if p95 > 700*time.Millisecond {
		t.Errorf("spec §4.7 latency: P95 %v > 700ms budget", p95)
	}
}

func percentileDurationBench(d []time.Duration, p float64) time.Duration {
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
