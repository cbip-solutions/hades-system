//go:build property && cgo

package ecosystem_property_test

import (
	"context"
	"math"
	"testing"
	"testing/quick"

	_ "github.com/mattn/go-sqlite3"
	"github.com/cbip-solutions/hades-system/internal/research/ecosystem"
)

func boundedNonNeg(v, lo, hi float64) float64 {
	x := math.Abs(v)
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return lo
	}
	span := hi - lo
	if span <= 0 {
		return lo
	}
	x = math.Mod(x, span)
	return lo + x
}

func TestAbstention_Property_MonotonicWithLambda(t *testing.T) {
	p, err := ecosystem.NewAbstentionPolicy(ecosystem.AbstentionConfig{})
	if err != nil {
		t.Fatalf("NewAbstentionPolicy: %v", err)
	}
	ctx := context.Background()

	prop := func(s1, s2, s3, s4, s5 float64, lambdaSeed, deltaSeed uint16) bool {

		scores := []float64{
			boundedNonNeg(s1, 0, 1),
			boundedNonNeg(s2, 0, 1),
			boundedNonNeg(s3, 0, 1),
			boundedNonNeg(s4, 0, 1),
			boundedNonNeg(s5, 0, 1),
		}
		lambda1 := float64(lambdaSeed%200) / 100.0
		delta := float64(deltaSeed%100) / 100.0
		lambda2 := lambda1 + delta

		eco := ecosystem.EcoGo

		ov1 := map[ecosystem.Ecosystem]float64{eco: lambda1}
		ov2 := map[ecosystem.Ecosystem]float64{eco: lambda2}

		d1 := p.ShouldAbstainWithOverride(ctx, eco, scores, ov1)
		d2 := p.ShouldAbstainWithOverride(ctx, eco, scores, ov2)

		if d1.Abstain && !d2.Abstain {
			t.Logf("inv-zen-196: monotonicity violated: λ_1=%v abstain=%v λ_2=%v abstain=%v scores=%v",
				lambda1, d1.Abstain, lambda2, d2.Abstain, scores)
			return false
		}
		return true
	}
	cfg := &quick.Config{MaxCount: 2000}
	if err := quick.Check(prop, cfg); err != nil {
		t.Errorf("inv-zen-196: abstention not monotonic in λ: %v", err)
	}
}

func TestAbstention_Property_DefaultLambdaTableMatchesSpec(t *testing.T) {
	p, err := ecosystem.NewAbstentionPolicy(ecosystem.AbstentionConfig{})
	if err != nil {
		t.Fatalf("NewAbstentionPolicy: %v", err)
	}
	ctx := context.Background()

	probe := []float64{0.5}

	wantLambdas := map[ecosystem.Ecosystem]float64{
		ecosystem.EcoGo:         0.3,
		ecosystem.EcoPython:     0.5,
		ecosystem.EcoTypeScript: 0.8,
		ecosystem.EcoRust:       0.4,
	}
	for eco, wantLambda := range wantLambdas {
		got := p.ShouldAbstain(ctx, eco, probe)
		if math.Abs(got.Lambda-wantLambda) > 1e-9 {
			t.Errorf("inv-zen-196: default λ_%s = %v; want %v", eco, got.Lambda, wantLambda)
		}
	}

	if !(wantLambdas[ecosystem.EcoGo] < wantLambdas[ecosystem.EcoRust] &&
		wantLambdas[ecosystem.EcoRust] < wantLambdas[ecosystem.EcoPython] &&
		wantLambdas[ecosystem.EcoPython] < wantLambdas[ecosystem.EcoTypeScript]) {
		t.Errorf("inv-zen-196: default λ ordering wrong: go=%v rust=%v python=%v ts=%v",
			wantLambdas[ecosystem.EcoGo], wantLambdas[ecosystem.EcoRust],
			wantLambdas[ecosystem.EcoPython], wantLambdas[ecosystem.EcoTypeScript])
	}
}

func TestAbstention_Property_HighConfidenceNeverAbstains(t *testing.T) {
	p, err := ecosystem.NewAbstentionPolicy(ecosystem.AbstentionConfig{})
	if err != nil {
		t.Fatalf("NewAbstentionPolicy: %v", err)
	}
	ctx := context.Background()

	scores := []float64{0.99, 0.991, 0.992, 0.989, 0.990}

	for _, eco := range ecosystem.AllEcosystems {
		for _, lambda := range []float64{0.0, 0.3, 0.5, 0.8, 1.0, 1.5, 2.0} {
			ov := map[ecosystem.Ecosystem]float64{eco: lambda}
			d := p.ShouldAbstainWithOverride(ctx, eco, scores, ov)
			if d.Abstain {
				t.Errorf("inv-zen-196: high-μ low-σ unexpectedly abstained: eco=%s λ=%v threshold=%v reason=%s",
					eco, lambda, d.Threshold, d.Reason)
			}
		}
	}
}
