// internal/research/ecosystem/abstention_test.go
//
// Tests for AbstentionPolicy (Bayesian μ−λσ per-ecosystem-tuned).
// Spec §2.7 Q7=A Layer 3; inv-zen-196 (per-ecosystem λ tunable).
//
// Coverage target: ≥90% (security/correctness-critical; this is the
// final answer-gate — false negatives leak unverified content).

package ecosystem

import (
	"context"
	"errors"
	"math"
	"strings"
	"sync"
	"testing"
)

func TestAbstention_HighConfidence_NoAbstain(t *testing.T) {
	policy := newTestAbstentionPolicy(t, defaultPerEcoLambda())
	scores := []float64{0.95, 0.92, 0.91, 0.89, 0.88}
	decision := policy.ShouldAbstain(context.Background(), EcoGo, scores)
	if decision.Abstain {
		t.Errorf("high-confidence cluster must NOT abstain: %+v", decision)
	}
	if decision.Mean < 0.88 || decision.Mean > 0.93 {
		t.Errorf("mean computation off: %.3f", decision.Mean)
	}
	if decision.Ecosystem != EcoGo {
		t.Errorf("decision must record ecosystem: got %q", decision.Ecosystem)
	}
}

func TestAbstention_LowConfidence_Abstain(t *testing.T) {
	policy := newTestAbstentionPolicy(t, defaultPerEcoLambda())

	scores := []float64{-0.05, -0.03, -0.02, -0.01, -0.005}
	decision := policy.ShouldAbstain(context.Background(), EcoGo, scores)
	if !decision.Abstain {
		t.Errorf("low-confidence cluster must abstain: %+v", decision)
	}
	if decision.Threshold > 0 {
		t.Errorf("threshold μ−λσ should be ≤0 here: %.4f", decision.Threshold)
	}
	if decision.Reason == "" {
		t.Errorf("abstain reason must be populated")
	}
	if !strings.Contains(decision.Reason, "<") {
		t.Errorf("reason must show comparison: %q", decision.Reason)
	}
}

func TestAbstention_PerEcosystemLambda_NpmHigherThreshold(t *testing.T) {
	policy := newTestAbstentionPolicy(t, defaultPerEcoLambda())

	scores := []float64{0.6, 0.45, 0.40, 0.35, 0.30}
	goDecision := policy.ShouldAbstain(context.Background(), EcoGo, scores)
	npmDecision := policy.ShouldAbstain(context.Background(), EcoTypeScript, scores)
	if goDecision.Threshold <= npmDecision.Threshold {
		t.Errorf("Go λ (0.3) should produce higher μ−λσ than TS λ (0.8): go=%.4f ts=%.4f",
			goDecision.Threshold, npmDecision.Threshold)
	}
	if goDecision.Lambda != 0.3 {
		t.Errorf("Go λ must be 0.3; got %.2f", goDecision.Lambda)
	}
	if npmDecision.Lambda != 0.8 {
		t.Errorf("TS λ must be 0.8; got %.2f", npmDecision.Lambda)
	}
}

func TestAbstention_DoctrineOverride(t *testing.T) {
	policy := newTestAbstentionPolicy(t, defaultPerEcoLambda())

	scores := []float64{0.6, 0.5, 0.4, 0.3, 0.2}
	base := policy.ShouldAbstain(context.Background(), EcoGo, scores)
	overrides := map[Ecosystem]float64{EcoGo: 5.0}
	overridden := policy.ShouldAbstainWithOverride(context.Background(), EcoGo, scores, overrides)
	if overridden.Lambda != 5.0 {
		t.Errorf("override λ must be 5.0; got %.2f", overridden.Lambda)
	}
	if base.Threshold == overridden.Threshold {
		t.Errorf("override must change threshold")
	}
	if base.Abstain {
		t.Errorf("base λ=0.3 on these scores must NOT abstain: %+v", base)
	}
	if !overridden.Abstain {
		t.Errorf("extreme λ override must trigger abstain on borderline scores: %+v", overridden)
	}
}

func TestAbstention_EmptyScores_AbstainsWithReason(t *testing.T) {
	policy := newTestAbstentionPolicy(t, defaultPerEcoLambda())
	decision := policy.ShouldAbstain(context.Background(), EcoGo, nil)
	if !decision.Abstain {
		t.Errorf("empty scores must abstain")
	}
	if decision.Reason == "" {
		t.Errorf("Reason must be populated")
	}
	if decision.Ecosystem != EcoGo {
		t.Errorf("decision must record ecosystem even on early-exit: got %q", decision.Ecosystem)
	}
}

func TestAbstention_SingleScore_MeanEqualsScore_StdevZero(t *testing.T) {
	policy := newTestAbstentionPolicy(t, defaultPerEcoLambda())
	scores := []float64{0.5}
	decision := policy.ShouldAbstain(context.Background(), EcoGo, scores)
	if math.Abs(decision.Mean-0.5) > 1e-9 {
		t.Errorf("mean must be 0.5; got %.6f", decision.Mean)
	}
	if math.Abs(decision.Stdev) > 1e-9 {
		t.Errorf("stdev must be 0.0; got %.6f", decision.Stdev)
	}

	if decision.Abstain {
		t.Errorf("single high score must not abstain")
	}
	if decision.KConsidered != 1 {
		t.Errorf("KConsidered must reflect actual slice length: got %d", decision.KConsidered)
	}
}

func TestAbstention_ConstructorRejectsInvalidLambda(t *testing.T) {
	_, err := NewAbstentionPolicy(AbstentionConfig{
		PerEcoLambda: map[Ecosystem]float64{EcoGo: -1.0},
	})
	if err == nil {
		t.Errorf("expected error on negative λ")
	}
	if !errors.Is(err, ErrAbstentionInvalidLambda) {
		t.Errorf("error must wrap ErrAbstentionInvalidLambda; got %v", err)
	}
}

// =============================================================================
// Extended coverage — security/correctness-critical paths (≥90% target)
// =============================================================================

func TestAbstention_ConstructorRejectsNaNLambda(t *testing.T) {
	_, err := NewAbstentionPolicy(AbstentionConfig{
		PerEcoLambda: map[Ecosystem]float64{EcoGo: math.NaN()},
	})
	if err == nil {
		t.Fatal("expected error on NaN λ")
	}
	if !errors.Is(err, ErrAbstentionInvalidLambda) {
		t.Errorf("error must wrap ErrAbstentionInvalidLambda; got %v", err)
	}
}

func TestAbstention_ConstructorRejectsInfLambda(t *testing.T) {
	_, err := NewAbstentionPolicy(AbstentionConfig{
		PerEcoLambda: map[Ecosystem]float64{EcoGo: math.Inf(+1)},
	})
	if err == nil {
		t.Fatal("expected error on +Inf λ")
	}
	if !errors.Is(err, ErrAbstentionInvalidLambda) {
		t.Errorf("error must wrap ErrAbstentionInvalidLambda; got %v", err)
	}
	_, err = NewAbstentionPolicy(AbstentionConfig{
		PerEcoLambda: map[Ecosystem]float64{EcoGo: math.Inf(-1)},
	})
	if err == nil {
		t.Fatal("expected error on -Inf λ")
	}
}

func TestAbstention_ConstructorDefaultsLambdaWhenEmpty(t *testing.T) {

	policy, err := NewAbstentionPolicy(AbstentionConfig{})
	if err != nil {
		t.Fatalf("empty PerEcoLambda must succeed (fills default): %v", err)
	}

	scores := []float64{0.5, 0.4, 0.3, 0.2, 0.1}
	go_ := policy.ShouldAbstain(context.Background(), EcoGo, scores)
	npm := policy.ShouldAbstain(context.Background(), EcoTypeScript, scores)
	if go_.Lambda != 0.3 {
		t.Errorf("default Go λ must be 0.3; got %.2f", go_.Lambda)
	}
	if npm.Lambda != 0.8 {
		t.Errorf("default TS λ must be 0.8; got %.2f", npm.Lambda)
	}
}

func TestAbstention_ConstructorDefaultsTopK(t *testing.T) {

	policy, err := NewAbstentionPolicy(AbstentionConfig{
		PerEcoLambda: defaultPerEcoLambda(),
	})
	if err != nil {
		t.Fatalf("NewAbstentionPolicy: %v", err)
	}
	if policy.meanTopK != 50 {
		t.Errorf("MeanTopK default must be 50; got %d", policy.meanTopK)
	}
	if policy.stdevTopK != 50 {
		t.Errorf("StdevTopK default must be 50; got %d", policy.stdevTopK)
	}
}

func TestAbstention_ConstructorAcceptsExplicitTopK(t *testing.T) {
	policy, err := NewAbstentionPolicy(AbstentionConfig{
		PerEcoLambda: defaultPerEcoLambda(),
		MeanTopK:     10,
		StdevTopK:    20,
	})
	if err != nil {
		t.Fatalf("NewAbstentionPolicy: %v", err)
	}
	if policy.meanTopK != 10 {
		t.Errorf("MeanTopK override not honored: got %d", policy.meanTopK)
	}
	if policy.stdevTopK != 20 {
		t.Errorf("StdevTopK override not honored: got %d", policy.stdevTopK)
	}
}

func TestAbstention_ContextCanceled_AbstainsImmediately(t *testing.T) {
	policy := newTestAbstentionPolicy(t, defaultPerEcoLambda())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	scores := []float64{0.95, 0.92, 0.91}
	decision := policy.ShouldAbstain(ctx, EcoGo, scores)
	if !decision.Abstain {
		t.Errorf("canceled context must abstain regardless of scores: %+v", decision)
	}
	if !strings.Contains(decision.Reason, "context") {
		t.Errorf("reason must reference context cancellation: %q", decision.Reason)
	}
	if decision.Ecosystem != EcoGo {
		t.Errorf("decision must record ecosystem on cancellation: got %q", decision.Ecosystem)
	}
}

func TestAbstention_NilOverride_UsesBaseLambda(t *testing.T) {
	// Passing nil override map MUST be equivalent to the base ShouldAbstain.
	policy := newTestAbstentionPolicy(t, defaultPerEcoLambda())
	scores := []float64{0.5, 0.4, 0.3, 0.2, 0.1}
	base := policy.ShouldAbstain(context.Background(), EcoGo, scores)
	withNilOverride := policy.ShouldAbstainWithOverride(context.Background(), EcoGo, scores, nil)
	if base.Lambda != withNilOverride.Lambda {
		t.Errorf("nil override must use base λ: base=%.4f override=%.4f", base.Lambda, withNilOverride.Lambda)
	}
	if base.Threshold != withNilOverride.Threshold {
		t.Errorf("nil override must yield identical threshold")
	}
	if base.Abstain != withNilOverride.Abstain {
		t.Errorf("nil override must yield identical decision")
	}
}

func TestAbstention_OverrideEmptyMap_UsesBaseLambda(t *testing.T) {
	// Empty (non-nil) override map MUST also fall back to base λ.
	policy := newTestAbstentionPolicy(t, defaultPerEcoLambda())
	scores := []float64{0.5, 0.4, 0.3, 0.2, 0.1}
	base := policy.ShouldAbstain(context.Background(), EcoGo, scores)
	emptyOverride := map[Ecosystem]float64{}
	withEmpty := policy.ShouldAbstainWithOverride(context.Background(), EcoGo, scores, emptyOverride)
	if base.Lambda != withEmpty.Lambda {
		t.Errorf("empty override map must fall back to base λ: base=%.4f override=%.4f", base.Lambda, withEmpty.Lambda)
	}
}

func TestAbstention_OverrideForDifferentEcosystem_NoEffect(t *testing.T) {
	// Override targeting EcoPython MUST NOT affect EcoGo decision.
	policy := newTestAbstentionPolicy(t, defaultPerEcoLambda())
	scores := []float64{0.5, 0.4, 0.3, 0.2, 0.1}
	base := policy.ShouldAbstain(context.Background(), EcoGo, scores)
	wrongTarget := policy.ShouldAbstainWithOverride(
		context.Background(), EcoGo, scores,
		map[Ecosystem]float64{EcoPython: 99.0},
	)
	if base.Lambda != wrongTarget.Lambda {
		t.Errorf("override for different eco must not affect target eco: base=%.4f got=%.4f",
			base.Lambda, wrongTarget.Lambda)
	}
}

func TestAbstention_UnknownEcosystem_ZeroLambda(t *testing.T) {

	policy, err := NewAbstentionPolicy(AbstentionConfig{
		PerEcoLambda: map[Ecosystem]float64{EcoGo: 0.3},
	})
	if err != nil {
		t.Fatalf("NewAbstentionPolicy: %v", err)
	}
	scores := []float64{0.5, 0.4, 0.3, 0.2, 0.1}
	decision := policy.ShouldAbstain(context.Background(), EcoPython, scores)
	if decision.Lambda != 0 {
		t.Errorf("unknown ecosystem must default λ=0; got %.4f", decision.Lambda)
	}

	if decision.Abstain {
		t.Errorf("with λ=0 and positive μ, must NOT abstain: %+v", decision)
	}
}

func TestAbstention_TopK_LimitsAppliedCorrectly(t *testing.T) {

	policy, err := NewAbstentionPolicy(AbstentionConfig{
		PerEcoLambda: defaultPerEcoLambda(),
		MeanTopK:     2,
		StdevTopK:    2,
	})
	if err != nil {
		t.Fatalf("NewAbstentionPolicy: %v", err)
	}
	scores := []float64{1.0, 1.0, 0.0, 0.0, 0.0}
	decision := policy.ShouldAbstain(context.Background(), EcoGo, scores)
	if math.Abs(decision.Mean-1.0) > 1e-9 {
		t.Errorf("with TopK=2 on [1,1,...], mean must be 1.0; got %.6f", decision.Mean)
	}
	if math.Abs(decision.Stdev) > 1e-9 {
		t.Errorf("with TopK=2 on [1,1,...], stdev must be 0.0; got %.6f", decision.Stdev)
	}
	if decision.KConsidered != 2 {
		t.Errorf("KConsidered must reflect actual TopK used: got %d, want 2", decision.KConsidered)
	}
}

func TestAbstention_TopK_LargerThanScoreSlice(t *testing.T) {

	policy, err := NewAbstentionPolicy(AbstentionConfig{
		PerEcoLambda: defaultPerEcoLambda(),
		MeanTopK:     100,
		StdevTopK:    100,
	})
	if err != nil {
		t.Fatalf("NewAbstentionPolicy: %v", err)
	}
	scores := []float64{0.9, 0.8, 0.7}
	decision := policy.ShouldAbstain(context.Background(), EcoGo, scores)

	if math.Abs(decision.Mean-0.8) > 1e-9 {
		t.Errorf("mean must be 0.8; got %.6f", decision.Mean)
	}
	if decision.KConsidered != 3 {
		t.Errorf("KConsidered must be 3 (capped at slice length): got %d", decision.KConsidered)
	}
}

func TestAbstention_StdevBesselsCorrection(t *testing.T) {

	policy := newTestAbstentionPolicy(t, defaultPerEcoLambda())
	scores := []float64{1.0, 2.0, 3.0}
	decision := policy.ShouldAbstain(context.Background(), EcoGo, scores)
	if math.Abs(decision.Mean-2.0) > 1e-9 {
		t.Errorf("mean must be 2.0; got %.6f", decision.Mean)
	}
	if math.Abs(decision.Stdev-1.0) > 1e-9 {
		t.Errorf("sample stdev must be 1.0 (Bessel correction); got %.6f", decision.Stdev)
	}
}

func TestAbstention_AllZeroScores_AbstainViaThresholdEqualZero(t *testing.T) {

	policy := newTestAbstentionPolicy(t, defaultPerEcoLambda())
	scores := []float64{0.0, 0.0, 0.0, 0.0, 0.0}
	decision := policy.ShouldAbstain(context.Background(), EcoGo, scores)
	if decision.Threshold != 0.0 {
		t.Errorf("threshold must be exactly 0; got %.6f", decision.Threshold)
	}

	if decision.Abstain {
		t.Errorf("threshold==0 must NOT abstain (strict <0): %+v", decision)
	}
}

func TestAbstention_DecisionReason_ContainsNumericFields(t *testing.T) {

	policy := newTestAbstentionPolicy(t, defaultPerEcoLambda())
	scores := []float64{-0.5, -0.4, -0.3}
	decision := policy.ShouldAbstain(context.Background(), EcoGo, scores)
	if !decision.Abstain {
		t.Fatalf("setup must trigger abstain; got %+v", decision)
	}

	for _, want := range []string{"μ", "λ", "σ", "<"} {
		if !strings.Contains(decision.Reason, want) {
			t.Errorf("reason must contain %q for audit trail: %q", want, decision.Reason)
		}
	}
}

func TestAbstention_OverridePreservesPolicyState(t *testing.T) {

	policy := newTestAbstentionPolicy(t, defaultPerEcoLambda())
	originalGoLambda := policy.perEco[EcoGo]
	_ = policy.ShouldAbstainWithOverride(
		context.Background(), EcoGo, []float64{0.5}, map[Ecosystem]float64{EcoGo: 99.0},
	)
	if policy.perEco[EcoGo] != originalGoLambda {
		t.Errorf("override leaked into base policy: was %.4f now %.4f",
			originalGoLambda, policy.perEco[EcoGo])
	}

	subsequent := policy.ShouldAbstain(context.Background(), EcoGo, []float64{0.5})
	if subsequent.Lambda != originalGoLambda {
		t.Errorf("subsequent call λ leaked: got %.4f want %.4f", subsequent.Lambda, originalGoLambda)
	}
}

func TestAbstention_ConcurrentCallsAreSafe(t *testing.T) {

	policy := newTestAbstentionPolicy(t, defaultPerEcoLambda())
	scores := []float64{0.5, 0.4, 0.3, 0.2, 0.1}
	var wg sync.WaitGroup
	const N = 100
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func(i int) {
			defer wg.Done()
			eco := AllEcosystems[i%len(AllEcosystems)]
			decision := policy.ShouldAbstain(context.Background(), eco, scores)
			if decision.Ecosystem != eco {
				t.Errorf("concurrent decision corrupted: want %q got %q", eco, decision.Ecosystem)
			}
		}(i)
	}
	wg.Wait()
}

func TestAbstention_ErrAbstentionInvalidLambdaIsSentinel(t *testing.T) {

	wrapped := ErrAbstentionInvalidLambda
	if !errors.Is(wrapped, ErrAbstentionInvalidLambda) {
		t.Errorf("ErrAbstentionInvalidLambda must satisfy errors.Is reflexively")
	}
	if ErrAbstentionInvalidLambda.Error() == "" {
		t.Errorf("ErrAbstentionInvalidLambda.Error() must be non-empty")
	}
}

func TestAbstention_DefaultPerEcoLambda_AllFourEcosystemsCovered(t *testing.T) {

	defaults := defaultPerEcoLambda()
	for _, eco := range AllEcosystems {
		if _, ok := defaults[eco]; !ok {
			t.Errorf("inv-zen-196: defaultPerEcoLambda missing entry for %q", eco)
		}
	}
	if len(defaults) != len(AllEcosystems) {
		t.Errorf("default λ table has %d entries; AllEcosystems has %d — drift",
			len(defaults), len(AllEcosystems))
	}
}

func TestAbstention_ArithmeticMean_EmptyInputReturnsZero(t *testing.T) {

	if got := arithmeticMean(nil); got != 0 {
		t.Errorf("arithmeticMean(nil) must return 0; got %.6f", got)
	}
	if got := arithmeticMean([]float64{}); got != 0 {
		t.Errorf("arithmeticMean([]) must return 0; got %.6f", got)
	}
}

func TestAbstention_DefaultLambdaValuesMatchSpec(t *testing.T) {

	defaults := defaultPerEcoLambda()
	cases := map[Ecosystem]float64{
		EcoGo:         0.3,
		EcoPython:     0.5,
		EcoTypeScript: 0.8,
		EcoRust:       0.4,
	}
	for eco, want := range cases {
		if got := defaults[eco]; got != want {
			t.Errorf("inv-zen-196 default λ for %q: want %.2f got %.2f", eco, want, got)
		}
	}
}

func newTestAbstentionPolicy(t *testing.T, perEcoLambda map[Ecosystem]float64) *AbstentionPolicy {
	t.Helper()
	p, err := NewAbstentionPolicy(AbstentionConfig{
		PerEcoLambda: perEcoLambda,
		MeanTopK:     50,
		StdevTopK:    50,
	})
	if err != nil {
		t.Fatalf("NewAbstentionPolicy: %v", err)
	}
	return p
}
