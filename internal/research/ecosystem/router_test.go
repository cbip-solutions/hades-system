package ecosystem

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"
	"time"
)

func TestRouter_HeuristicGo_SingleEcosystem(t *testing.T) {
	r := newTestRouter(t)
	decision, err := r.Classify(context.Background(), "how do I create a goroutine that reads from a chan?", nil)
	if err != nil {
		t.Fatalf("Classify failed: %v", err)
	}
	if decision.Method != RoutingMethodSingle {
		t.Errorf("want Method=single (heuristic high-confidence), got %s", decision.Method)
	}
	if len(decision.Ecosystems) != 1 || decision.Ecosystems[0] != EcoGo {
		t.Errorf("want [go], got %v", decision.Ecosystems)
	}
	if decision.ConfidenceWeights[EcoGo] < 0.9 {
		t.Errorf("want heuristic confidence ≥0.9 on canonical Go token, got %.3f", decision.ConfidenceWeights[EcoGo])
	}
	if decision.HeuristicMatched == "" {
		t.Errorf("want HeuristicMatched populated on heuristic single-route")
	}
}

func TestRouter_HeuristicPython(t *testing.T) {
	r := newTestRouter(t)
	decision, err := r.Classify(context.Background(), "asyncio gather example with numpy array", nil)
	if err != nil {
		t.Fatalf("Classify failed: %v", err)
	}
	if decision.Method != RoutingMethodSingle || decision.Ecosystems[0] != EcoPython {
		t.Errorf("python heuristic must single-route; got %+v", decision)
	}
}

func TestRouter_HeuristicTypeScript(t *testing.T) {
	r := newTestRouter(t)
	decision, err := r.Classify(context.Background(), "tsconfig.json strict mode with useState", nil)
	if err != nil {
		t.Fatalf("Classify failed: %v", err)
	}
	if decision.Method != RoutingMethodSingle || decision.Ecosystems[0] != EcoTypeScript {
		t.Errorf("ts heuristic must single-route; got %+v", decision)
	}
}

func TestRouter_HeuristicRust(t *testing.T) {
	r := newTestRouter(t)
	decision, err := r.Classify(context.Background(), "cargo build with custom crate dependency", nil)
	if err != nil {
		t.Fatalf("Classify failed: %v", err)
	}
	if decision.Method != RoutingMethodSingle || decision.Ecosystems[0] != EcoRust {
		t.Errorf("rust heuristic must single-route; got %+v", decision)
	}
}

// TestRouter_HeuristicPython_PipCheck pins the spec §2.6 inclusive "pip" token:
// a query that mentions pip in a non-install verb ("pip check requests version")
// MUST still single-route Python. Regression guard against an earlier
// over-precise "pip install" substring.
func TestRouter_HeuristicPython_PipCheck(t *testing.T) {
	r := newTestRouter(t)
	decision, err := r.Classify(context.Background(), "pip check requests version conflict", nil)
	if err != nil {
		t.Fatalf("Classify failed: %v", err)
	}
	if decision.Method != RoutingMethodSingle {
		t.Errorf("pip-check query must single-route via heuristic; got %s", decision.Method)
	}
	if len(decision.Ecosystems) != 1 || decision.Ecosystems[0] != EcoPython {
		t.Errorf("want [python] from pip-check, got %v", decision.Ecosystems)
	}
	if decision.HeuristicMatched != "pip" {
		t.Errorf("want HeuristicMatched=%q, got %q", "pip", decision.HeuristicMatched)
	}
}

// TestRouter_HeuristicTypeScript_NpmRun pins the spec §2.6 inclusive "npm" token:
// a query that mentions npm in a non-install verb ("npm run build with webpack")
// MUST still single-route TypeScript. Regression guard against an earlier
// over-precise "npm install" substring.
func TestRouter_HeuristicTypeScript_NpmRun(t *testing.T) {
	r := newTestRouter(t)
	decision, err := r.Classify(context.Background(), "npm run build with webpack and typescript", nil)
	if err != nil {
		t.Fatalf("Classify failed: %v", err)
	}
	if decision.Method != RoutingMethodSingle {
		t.Errorf("npm-run query must single-route via heuristic; got %s", decision.Method)
	}
	if len(decision.Ecosystems) != 1 || decision.Ecosystems[0] != EcoTypeScript {
		t.Errorf("want [typescript] from npm-run, got %v", decision.Ecosystems)
	}
	if decision.HeuristicMatched != "npm" {
		t.Errorf("want HeuristicMatched=%q, got %q", "npm", decision.HeuristicMatched)
	}
}

func TestRouter_HeuristicConflict_FallsThroughToClassifier(t *testing.T) {

	r := newTestRouterWithUniformClassifier(t)
	decision, err := r.Classify(context.Background(), "how is goroutine in Go vs asyncio in Python similar?", embeddingZero())
	if err != nil {
		t.Fatalf("Classify failed: %v", err)
	}
	if decision.Method != RoutingMethodBroadcast {
		t.Errorf("uniform classifier (margin <0.1) must broadcast; got %s", decision.Method)
	}
	if len(decision.Ecosystems) != 4 {
		t.Errorf("broadcast must include all 4 ecosystems; got %d", len(decision.Ecosystems))
	}
}

func TestRouter_ClassifierMarginGreaterThan020_Single(t *testing.T) {

	r := newTestRouterWithClassifier(t, map[Ecosystem]float64{EcoGo: 0.6, EcoPython: 0.13, EcoTypeScript: 0.13, EcoRust: 0.14})
	decision, err := r.Classify(context.Background(), "generic prose query no canonical tokens here", embeddingZero())
	if err != nil {
		t.Fatalf("Classify failed: %v", err)
	}
	if decision.Method != RoutingMethodSingle {
		t.Errorf("margin 0.47 must single; got %s", decision.Method)
	}
	if decision.Ecosystems[0] != EcoGo {
		t.Errorf("classifier top-1 must select Go; got %v", decision.Ecosystems)
	}

	if math.Abs(decision.ConfidenceWeights[EcoGo]-1.0) > 1e-6 {
		t.Errorf("single must renormalize top-1 to 1.0, got %v", decision.ConfidenceWeights[EcoGo])
	}
}

func TestRouter_ClassifierMargin010To020_Top2(t *testing.T) {

	r := newTestRouterWithClassifier(t, map[Ecosystem]float64{EcoGo: 0.40, EcoPython: 0.27, EcoTypeScript: 0.18, EcoRust: 0.15})
	decision, err := r.Classify(context.Background(), "generic prose query", embeddingZero())
	if err != nil {
		t.Fatalf("Classify failed: %v", err)
	}
	if decision.Method != RoutingMethodTop2 {
		t.Errorf("margin 0.13 must top-2; got %s", decision.Method)
	}
	if len(decision.Ecosystems) != 2 || decision.Ecosystems[0] != EcoGo || decision.Ecosystems[1] != EcoPython {
		t.Errorf("top-2 must be [go, python]; got %v", decision.Ecosystems)
	}

	sum := decision.ConfidenceWeights[EcoGo] + decision.ConfidenceWeights[EcoPython]
	if sum < 0.99 || sum > 1.01 {
		t.Errorf("top-2 weights must renormalize to 1.0; got sum=%.3f", sum)
	}
}

func TestRouter_ClassifierMarginBelow010_Broadcast(t *testing.T) {

	r := newTestRouterWithClassifier(t, map[Ecosystem]float64{EcoGo: 0.27, EcoPython: 0.26, EcoTypeScript: 0.24, EcoRust: 0.23})
	decision, err := r.Classify(context.Background(), "ambiguous", embeddingZero())
	if err != nil {
		t.Fatalf("Classify failed: %v", err)
	}
	if decision.Method != RoutingMethodBroadcast {
		t.Errorf("uniform must broadcast; got %s", decision.Method)
	}
	if len(decision.Ecosystems) != 4 {
		t.Errorf("broadcast must include all 4 ecosystems; got %d", len(decision.Ecosystems))
	}

	sum := 0.0
	for _, w := range decision.ConfidenceWeights {
		sum += w
	}
	if sum < 0.99 || sum > 1.01 {
		t.Errorf("broadcast weights must already sum 1.0; got %.3f", sum)
	}
	// Ecosystems slice MUST be in descending-confidence order
	for i := 1; i < len(decision.Ecosystems); i++ {
		prev := decision.ConfidenceWeights[decision.Ecosystems[i-1]]
		cur := decision.ConfidenceWeights[decision.Ecosystems[i]]
		if cur > prev {
			t.Errorf("broadcast Ecosystems must be ordered desc; pos %d (%s=%.3f) > pos %d (%s=%.3f)",
				i, decision.Ecosystems[i], cur, i-1, decision.Ecosystems[i-1], prev)
		}
	}
}

func TestRouter_DeterministicOrdering_SameInputsSameOutput(t *testing.T) {

	r := newTestRouterWithClassifier(t, map[Ecosystem]float64{EcoGo: 0.25, EcoPython: 0.25, EcoTypeScript: 0.25, EcoRust: 0.25})
	first, _ := r.Classify(context.Background(), "ambiguous", embeddingZero())
	for i := 0; i < 50; i++ {
		got, _ := r.Classify(context.Background(), "ambiguous", embeddingZero())
		if got.Method != first.Method {
			t.Fatalf("non-deterministic method: %s vs %s", got.Method, first.Method)
		}
		if len(got.Ecosystems) != len(first.Ecosystems) {
			t.Fatalf("non-deterministic ecosystem count: %d vs %d", len(got.Ecosystems), len(first.Ecosystems))
		}
		for j := range got.Ecosystems {
			if got.Ecosystems[j] != first.Ecosystems[j] {
				t.Fatalf("non-deterministic order at pos %d: %s vs %s", j, got.Ecosystems[j], first.Ecosystems[j])
			}
		}
	}
}

func TestRouter_LatencyTarget1to2ms(t *testing.T) {
	if testing.Short() {
		t.Skip("perf assertion; skip -short")
	}
	r := newTestRouter(t)
	ctx := context.Background()
	iter := 200
	start := timeNow()
	for i := 0; i < iter; i++ {
		_, _ = r.Classify(ctx, "goroutine pipeline pattern in concurrent code", embeddingZero())
	}
	elapsed := timeSince(start)
	perCall := elapsed / time.Duration(iter)

	if perCall > 3*time.Millisecond {
		t.Errorf("router latency mean=%v exceeds 3ms ceiling (target 1-2ms)", perCall)
	}
}

func TestRouter_ContextCancel(t *testing.T) {
	r := newTestRouter(t)
	ctx, cancel := contextWithCancel()
	cancel()
	_, err := r.Classify(ctx, "any query", embeddingZero())
	if !errors.Is(err, context.Canceled) {
		t.Errorf("want context.Canceled, got %v", err)
	}
}

func TestRouter_EmptyQuery_DefaultsToBroadcast(t *testing.T) {
	r := newTestRouter(t)
	decision, err := r.Classify(context.Background(), "", embeddingZero())
	if err != nil {
		t.Fatalf("empty-query Classify failed: %v", err)
	}
	if decision.Method != RoutingMethodBroadcast {
		t.Errorf("empty query must broadcast; got %s", decision.Method)
	}
	if len(decision.Ecosystems) != 4 {
		t.Errorf("empty-query broadcast must list all 4 ecosystems; got %d", len(decision.Ecosystems))
	}
	for _, e := range AllEcosystems {
		if decision.ConfidenceWeights[e] == 0 {
			t.Errorf("empty-query broadcast must assign nonzero weight to %s", e)
		}
	}
}

func TestRouter_WhitespaceOnlyQuery_DefaultsToBroadcast(t *testing.T) {
	r := newTestRouter(t)
	decision, err := r.Classify(context.Background(), "   \t\n  ", embeddingZero())
	if err != nil {
		t.Fatalf("whitespace-only Classify failed: %v", err)
	}
	if decision.Method != RoutingMethodBroadcast {
		t.Errorf("whitespace-only query must broadcast; got %s", decision.Method)
	}
}

func TestRouter_Concurrent_NoDataRace(t *testing.T) {
	r := newTestRouter(t)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				_, _ = r.Classify(context.Background(), "goroutine query", embeddingZero())
			}
		}()
	}
	wg.Wait()
	if r.CountClassifications() != 16*200 {
		t.Errorf("count mismatch: %d", r.CountClassifications())
	}
}

func TestNewRouter_RejectsEmptyHeuristics(t *testing.T) {
	_, err := NewRouter(RouterConfig{
		Heuristics:      nil,
		Classifier:      newUniformMockClassifier(),
		MarginBroadcast: 0.10,
		MarginTop2:      0.20,
	})
	if err == nil {
		t.Errorf("want error for empty Heuristics")
	}
}

func TestNewRouter_RejectsNilClassifier(t *testing.T) {
	_, err := NewRouter(RouterConfig{
		Heuristics:      defaultHeuristics(),
		Classifier:      nil,
		MarginBroadcast: 0.10,
		MarginTop2:      0.20,
	})
	if err == nil {
		t.Errorf("want error for nil Classifier")
	}
}

func TestNewRouter_RejectsBadMargins(t *testing.T) {
	cases := []struct {
		name  string
		bcast float64
		top2  float64
	}{
		{"broadcast-zero", 0, 0.20},
		{"broadcast-negative", -0.1, 0.20},
		{"top2-le-broadcast", 0.20, 0.20},
		{"top2-below-broadcast", 0.20, 0.10},
		{"top2-ge-one", 0.10, 1.0},
		{"top2-above-one", 0.10, 1.5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewRouter(RouterConfig{
				Heuristics:      defaultHeuristics(),
				Classifier:      newUniformMockClassifier(),
				MarginBroadcast: tc.bcast,
				MarginTop2:      tc.top2,
			})
			if err == nil {
				t.Errorf("want error for margins (%g, %g)", tc.bcast, tc.top2)
			}
		})
	}
}

func TestNewRouter_AcceptsValidConfig(t *testing.T) {
	r, err := NewRouter(RouterConfig{
		Heuristics:      defaultHeuristics(),
		Classifier:      newUniformMockClassifier(),
		MarginBroadcast: 0.10,
		MarginTop2:      0.20,
	})
	if err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	if r == nil {
		t.Fatalf("nil Router returned")
	}
	if r.ClassifierCheckpointHash() == "" {
		t.Errorf("ClassifierCheckpointHash empty; want stable hash from mock")
	}
}

func TestRouter_ClassifierReturnsError_PropagatedAsWrapped(t *testing.T) {
	wantErr := errors.New("classifier loaded checkpoint mismatch")
	r, err := NewRouter(RouterConfig{
		Heuristics:      defaultHeuristics(),
		Classifier:      &errClassifier{err: wantErr},
		MarginBroadcast: 0.10,
		MarginTop2:      0.20,
	})
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	_, gotErr := r.Classify(context.Background(), "prose query", embeddingZero())
	if !errors.Is(gotErr, wantErr) {
		t.Errorf("want wrapped classifier error; got %v", gotErr)
	}
}

func TestRouter_ClassifierReturnsInvalidSoftmax_RejectedAsInvariant(t *testing.T) {

	r, err := NewRouter(RouterConfig{
		Heuristics:      defaultHeuristics(),
		Classifier:      newFixedScoresClassifier(map[Ecosystem]float64{EcoGo: 0.5, EcoPython: 0.5, EcoTypeScript: 0.5, EcoRust: 0.5}),
		MarginBroadcast: 0.10,
		MarginTop2:      0.20,
	})
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	_, gotErr := r.Classify(context.Background(), "prose query", embeddingZero())
	if gotErr == nil {
		t.Errorf("want error for invalid softmax (sum != 1.0)")
	}
}

func TestRouter_ClassifierMissingEcosystem_RejectedAsInvariant(t *testing.T) {

	r, err := NewRouter(RouterConfig{
		Heuristics:      defaultHeuristics(),
		Classifier:      newFixedScoresClassifier(map[Ecosystem]float64{EcoGo: 0.5, EcoPython: 0.3, EcoTypeScript: 0.2}),
		MarginBroadcast: 0.10,
		MarginTop2:      0.20,
	})
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	_, gotErr := r.Classify(context.Background(), "prose query", embeddingZero())
	if gotErr == nil {
		t.Errorf("want error for softmax missing ecosystem")
	}
}

func TestRouter_ClassifierNegativeScore_RejectedAsInvariant(t *testing.T) {

	r, err := NewRouter(RouterConfig{
		Heuristics:      defaultHeuristics(),
		Classifier:      newFixedScoresClassifier(map[Ecosystem]float64{EcoGo: -0.1, EcoPython: 0.4, EcoTypeScript: 0.3, EcoRust: 0.4}),
		MarginBroadcast: 0.10,
		MarginTop2:      0.20,
	})
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	_, gotErr := r.Classify(context.Background(), "prose query", embeddingZero())
	if gotErr == nil {
		t.Errorf("want error for softmax negative score")
	}
}

func TestRouter_ClassifierNaN_RejectedAsInvariant(t *testing.T) {
	r, err := NewRouter(RouterConfig{
		Heuristics:      defaultHeuristics(),
		Classifier:      newFixedScoresClassifier(map[Ecosystem]float64{EcoGo: math.NaN(), EcoPython: 0.33, EcoTypeScript: 0.33, EcoRust: 0.34}),
		MarginBroadcast: 0.10,
		MarginTop2:      0.20,
	})
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	_, gotErr := r.Classify(context.Background(), "prose query", embeddingZero())
	if gotErr == nil {
		t.Errorf("want error for softmax NaN")
	}
}

func TestRouter_LatencyTraceFields_Populated(t *testing.T) {
	r := newTestRouterWithClassifier(t, map[Ecosystem]float64{EcoGo: 0.6, EcoPython: 0.15, EcoTypeScript: 0.15, EcoRust: 0.10})
	decision, _ := r.Classify(context.Background(), "generic prose query no canonical tokens", embeddingZero())
	if decision.HeuristicLatencyMs < 0 {
		t.Errorf("HeuristicLatencyMs must be ≥0; got %v", decision.HeuristicLatencyMs)
	}
	if decision.ClassifierLatencyMs < 0 {
		t.Errorf("ClassifierLatencyMs must be ≥0; got %v", decision.ClassifierLatencyMs)
	}
}

// TestRouter_ClassifierError_PreservesLatencyTrace pins the audit-completeness
// contract from RoutingDecision docstring: when the classifier errors, the
// returned RoutingDecision MUST still carry the partial latency trace so
// EvtRAGQuery audit (spec §4.6) can locate WHERE the failure happened in the
// perf budget. Mock injects a 5ms classifier delay; both fields must reflect
// observed wall time even though the decision payload is otherwise zero.
func TestRouter_ClassifierError_PreservesLatencyTrace(t *testing.T) {
	wantErr := errors.New("classifier offline")
	delay := 5 * time.Millisecond
	r, err := NewRouter(RouterConfig{
		Heuristics:      defaultHeuristics(),
		Classifier:      &delayedErrClassifier{err: wantErr, delay: delay},
		MarginBroadcast: 0.10,
		MarginTop2:      0.20,
	})
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	decision, gotErr := r.Classify(context.Background(), "generic prose without ecosystem tokens", embeddingZero())
	if !errors.Is(gotErr, wantErr) {
		t.Fatalf("want wrapped classifier error; got %v", gotErr)
	}

	if decision.ClassifierLatencyMs < 4 {
		t.Errorf("ClassifierLatencyMs on error path must reflect mock 5ms delay; got %v", decision.ClassifierLatencyMs)
	}

	if decision.HeuristicLatencyMs <= 0 {
		t.Errorf("HeuristicLatencyMs on error path must be >0 (heuristic ran before classifier); got %v", decision.HeuristicLatencyMs)
	}
}

func TestRouter_EmptyQuery_PopulatesHeuristicLatency(t *testing.T) {
	r := newTestRouter(t)
	decision, err := r.Classify(context.Background(), "", embeddingZero())
	if err != nil {
		t.Fatalf("empty-query Classify failed: %v", err)
	}
	if decision.Method != RoutingMethodBroadcast {
		t.Fatalf("precondition: empty query must broadcast; got %s", decision.Method)
	}
	if decision.HeuristicLatencyMs <= 0 {
		t.Errorf("empty-query fast-path must populate HeuristicLatencyMs > 0; got %v", decision.HeuristicLatencyMs)
	}

	if decision.ClassifierLatencyMs != 0 {
		t.Errorf("empty-query fast-path must leave ClassifierLatencyMs=0 (classifier skipped); got %v", decision.ClassifierLatencyMs)
	}
}

func TestRouter_ClassifierWrongEcosystemKey_RejectedAsInvariant(t *testing.T) {

	r, err := NewRouter(RouterConfig{
		Heuristics: defaultHeuristics(),
		Classifier: newFixedScoresClassifier(map[Ecosystem]float64{
			EcoGo:                0.25,
			EcoPython:            0.25,
			EcoTypeScript:        0.25,
			Ecosystem("haskell"): 0.25,
		}),
		MarginBroadcast: 0.10,
		MarginTop2:      0.20,
	})
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	_, gotErr := r.Classify(context.Background(), "prose query", embeddingZero())
	if gotErr == nil {
		t.Errorf("want error for softmax with unknown ecosystem key")
	}
}

func TestRouter_ClassifierInfValue_RejectedAsInvariant(t *testing.T) {
	r, err := NewRouter(RouterConfig{
		Heuristics:      defaultHeuristics(),
		Classifier:      newFixedScoresClassifier(map[Ecosystem]float64{EcoGo: math.Inf(1), EcoPython: 0.33, EcoTypeScript: 0.33, EcoRust: 0.34}),
		MarginBroadcast: 0.10,
		MarginTop2:      0.20,
	})
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	_, gotErr := r.Classify(context.Background(), "prose query", embeddingZero())
	if gotErr == nil {
		t.Errorf("want error for softmax +Inf value")
	}
}

func TestSingleFromSet_EmptyMapReturnsZeroEcosystem(t *testing.T) {

	if got := singleFromSet(map[Ecosystem]bool{}); got != "" {
		t.Errorf("singleFromSet(empty) = %q, want \"\"", got)
	}
}

func TestRenormalizeTop2_ZeroTotal_FiftyFifty(t *testing.T) {

	out := renormalizeTop2(scored{EcoGo, 0}, scored{EcoPython, 0})
	if out[EcoGo] != 0.5 || out[EcoPython] != 0.5 {
		t.Errorf("zero-total renormalize must yield 50/50; got %v", out)
	}
}

func TestRouter_CountClassifications_Increments(t *testing.T) {
	r := newTestRouter(t)
	before := r.CountClassifications()
	for i := 0; i < 5; i++ {
		_, _ = r.Classify(context.Background(), "goroutine query", embeddingZero())
	}
	got := r.CountClassifications() - before
	if got != 5 {
		t.Errorf("want 5 classifications, got %d", got)
	}
}

func newTestRouter(t *testing.T) *Router {
	t.Helper()
	cfg := RouterConfig{
		Heuristics:      defaultHeuristics(),
		Classifier:      newUniformMockClassifier(),
		MarginBroadcast: 0.10,
		MarginTop2:      0.20,
	}
	r, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	return r
}

func newTestRouterWithClassifier(t *testing.T, scores map[Ecosystem]float64) *Router {
	t.Helper()
	cfg := RouterConfig{
		Heuristics:      defaultHeuristics(),
		Classifier:      newFixedScoresClassifier(scores),
		MarginBroadcast: 0.10,
		MarginTop2:      0.20,
	}
	r, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	return r
}

func newTestRouterWithUniformClassifier(t *testing.T) *Router {
	return newTestRouterWithClassifier(t, map[Ecosystem]float64{EcoGo: 0.25, EcoPython: 0.25, EcoTypeScript: 0.25, EcoRust: 0.25})
}

func embeddingZero() []float32 { return make([]float32, 1536) }

func timeNow() time.Time                  { return time.Now() }
func timeSince(t time.Time) time.Duration { return time.Since(t) }

func contextWithCancel() (context.Context, context.CancelFunc) {
	return context.WithCancel(context.Background())
}

type uniformMockClassifier struct{}

func newUniformMockClassifier() *uniformMockClassifier { return &uniformMockClassifier{} }

func (m *uniformMockClassifier) ScoreSoftmax(_ context.Context, _ []float32) (map[Ecosystem]float64, error) {
	out := make(map[Ecosystem]float64, len(AllEcosystems))
	w := 1.0 / float64(len(AllEcosystems))
	for _, e := range AllEcosystems {
		out[e] = w
	}
	return out, nil
}

func (m *uniformMockClassifier) CheckpointHash() string { return "mock-uniform" }

type fixedScoresClassifier struct {
	scores map[Ecosystem]float64
}

func newFixedScoresClassifier(scores map[Ecosystem]float64) *fixedScoresClassifier {

	cp := make(map[Ecosystem]float64, len(scores))
	for k, v := range scores {
		cp[k] = v
	}
	return &fixedScoresClassifier{scores: cp}
}

func (m *fixedScoresClassifier) ScoreSoftmax(_ context.Context, _ []float32) (map[Ecosystem]float64, error) {
	out := make(map[Ecosystem]float64, len(m.scores))
	for k, v := range m.scores {
		out[k] = v
	}
	return out, nil
}

func (m *fixedScoresClassifier) CheckpointHash() string { return "mock-fixed" }

type errClassifier struct {
	err error
}

func (m *errClassifier) ScoreSoftmax(_ context.Context, _ []float32) (map[Ecosystem]float64, error) {
	return nil, m.err
}

func (m *errClassifier) CheckpointHash() string { return "mock-err" }

type delayedErrClassifier struct {
	err   error
	delay time.Duration
}

func (m *delayedErrClassifier) ScoreSoftmax(ctx context.Context, _ []float32) (map[Ecosystem]float64, error) {
	select {
	case <-time.After(m.delay):
		return nil, m.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (m *delayedErrClassifier) CheckpointHash() string { return "mock-delayed-err" }
