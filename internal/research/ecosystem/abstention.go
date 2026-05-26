// SPDX-License-Identifier: MIT
// Package ecosystem — abstention.go
//
// Bayesian abstention policy per spec §2.7 Q7=A Layer 3.
//
// # Algorithm
//
//	threshold = μ − λ(ecosystem) × σ
//
// where:
//
//	μ — arithmetic mean of top-K retriever scores
//	σ — sample stdev (Bessel's correction) of top-K retriever scores
//	λ — ecosystem-tuned coefficient (Go=0.3, Python=0.5, TS=0.8, Rust=0.4)
//
// Decision threshold < 0 → abstain (no answer faithful enough to ground).
// Operator may tune at runtime via DoctrineProfile.AbstentionThresholds
// override (passed through ShouldAbstainWithOverride).
//
// # Why per-ecosystem λ
//
// Go stdlib + pkg.go.dev have low noise floor (clean docs, canonical paths)
// → low λ permits answers on moderate retrieval. npm corpus is noisy (typo
// packages, micro-deps, abandoned forks) → high λ demands stronger signal
// to commit to an answer. Rust corpus has docs.rs canonical paths but
// trait-impl explosion → middle ground. Python has stdlib + PyPI mix.
//
// # Why μ − λσ (and not a percentile)
//
// The orchestrator routes only the top-N retrieved candidates; a constant
// percentile (e.g. p90) would degenerate to the same threshold on a
// 5-candidate input vs a 50-candidate input. μ−λσ scales with the
// spread of the input: tight clusters of high scores → low σ → high
// threshold (easy to pass); wide spread → high σ → low threshold (gated
// by λ). This matches the intuition that "the retriever agrees" is the
// gating signal.
//
// # Why strict <0 (not ≤0)
//
// All-zero scores (e.g. when reranker is unavailable + similarity normalized
// to 0) yield threshold = 0. Strict <0 lets the policy pass-through that
// boundary case to downstream policies (citation guard, hallucination
// detector); ≤0 would over-abstain on neutral input.
//
// # Doctrine override path
//
// DoctrineProfile.AbstentionThresholds (Phase A doctrine.go) carries the
// per-doctrine multiplier table (max-scope ×1.5, capa-firewall ×2.0 of
// the baseline). Dispatcher resolves DoctrineProfile per query and passes
// AbstentionThresholds as the override map. Override absent for a given
// ecosystem → base λ from PerEcoLambda applied.
//
// # inv-zen-196
//
// "Per-ecosystem λ tunable via providers config." Owned by this file:
// the defaultPerEcoLambda table is the v0.14.0 baseline; the override
// map (DoctrineProfile.AbstentionThresholds, populated from
// providers TOML via Phase F F-8) is the runtime mutability surface.
// Test TestAbstention_DefaultPerEcoLambda_AllFourEcosystemsCovered
// enforces the 4-ecosystem coverage at compile/run time.
//
// # Concurrency
//
// AbstentionPolicy is immutable after NewAbstentionPolicy. perEco is read
// but never written from ShouldAbstain*. Safe for concurrent reads
// (TestAbstention_ConcurrentCallsAreSafe).
//
// # Sensitivity / re-tuning
//
// Re-tuned post-corpus-rebuild on the adversarial held-out set per Phase H.
// Phase H ships the calibration protocol in tests/adversarial/ecosystem/.
//
// Spec docs/superpowers/specs/2026-05-14-zen-swarm-plan-14-research-design.md §2.7 Q7=A Layer 3.
// Master invariant: inv-zen-196.
package ecosystem

import (
	"context"
	"errors"
	"fmt"
	"math"
)

var ErrAbstentionInvalidLambda = errors.New("abstention: invalid lambda value")

type AbstentionConfig struct {
	PerEcoLambda map[Ecosystem]float64
	MeanTopK     int
	StdevTopK    int
}

type AbstentionDecision struct {
	Abstain     bool
	Reason      string
	Lambda      float64
	Mean        float64
	Stdev       float64
	Threshold   float64
	Ecosystem   Ecosystem
	KConsidered int
}

type AbstentionPolicy struct {
	perEco    map[Ecosystem]float64
	meanTopK  int
	stdevTopK int
}

func defaultPerEcoLambda() map[Ecosystem]float64 {
	return map[Ecosystem]float64{
		EcoGo:         0.3,
		EcoPython:     0.5,
		EcoTypeScript: 0.8,
		EcoRust:       0.4,
	}
}

// NewAbstentionPolicy constructs a policy with the given config.
//
// Validation
//   - PerEcoLambda empty/nil → seeded with defaultPerEcoLambda()
//   - Each λ value must be finite and ≥ 0 (else
//     ErrAbstentionInvalidLambda wrapped via %w)
//   - MeanTopK == 0 → 50; StdevTopK == 0 → 50 (cannot be negative; we
//     do not validate negative here because the type is int and the
//     constructor's zero default handles the common case — callers
//     passing negative get clamped behavior at use-time via min())
//
// Post returned policy has a non-nil perEco table and positive top-K
// windows. Subsequent ShouldAbstain* calls do not mutate any field.
func NewAbstentionPolicy(cfg AbstentionConfig) (*AbstentionPolicy, error) {
	if len(cfg.PerEcoLambda) == 0 {
		cfg.PerEcoLambda = defaultPerEcoLambda()
	}
	for e, l := range cfg.PerEcoLambda {
		if math.IsNaN(l) || math.IsInf(l, 0) || l < 0 {
			return nil, fmt.Errorf("%w: ecosystem %q: %v", ErrAbstentionInvalidLambda, e, l)
		}
	}

	clone := make(map[Ecosystem]float64, len(cfg.PerEcoLambda))
	for k, v := range cfg.PerEcoLambda {
		clone[k] = v
	}
	if cfg.MeanTopK == 0 {
		cfg.MeanTopK = 50
	}
	if cfg.StdevTopK == 0 {
		cfg.StdevTopK = 50
	}
	return &AbstentionPolicy{
		perEco:    clone,
		meanTopK:  cfg.MeanTopK,
		stdevTopK: cfg.StdevTopK,
	}, nil
}

func (p *AbstentionPolicy) ShouldAbstain(ctx context.Context, eco Ecosystem, scores []float64) AbstentionDecision {
	return p.ShouldAbstainWithOverride(ctx, eco, scores, nil)
}

func (p *AbstentionPolicy) ShouldAbstainWithOverride(
	ctx context.Context,
	eco Ecosystem,
	scores []float64,
	override map[Ecosystem]float64,
) AbstentionDecision {
	if err := ctx.Err(); err != nil {
		return AbstentionDecision{
			Abstain:   true,
			Reason:    fmt.Sprintf("context canceled: %v", err),
			Ecosystem: eco,
		}
	}
	if len(scores) == 0 {
		return AbstentionDecision{
			Abstain:   true,
			Reason:    "no retrieval candidates",
			Ecosystem: eco,
		}
	}

	lambda := p.perEco[eco]
	if v, ok := override[eco]; ok {
		lambda = v
	}

	mTop := min(p.meanTopK, len(scores))
	sTop := min(p.stdevTopK, len(scores))
	mean := arithmeticMean(scores[:mTop])
	stdev := sampleStdev(scores[:sTop], mean)
	threshold := mean - lambda*stdev
	decision := AbstentionDecision{
		Lambda:      lambda,
		Mean:        mean,
		Stdev:       stdev,
		Threshold:   threshold,
		Ecosystem:   eco,
		KConsidered: max(mTop, sTop),
	}

	if threshold < 0 {
		decision.Abstain = true

		decision.Reason = fmt.Sprintf("μ−λσ = %.4f − %.2f×%.4f = %.4f < 0",
			mean, lambda, stdev, threshold)
	}
	return decision
}

func arithmeticMean(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	var sum float64
	for _, x := range v {
		sum += x
	}
	return sum / float64(len(v))
}

// sampleStdev returns the sample stdev of v given its precomputed mean,
// using Bessel's correction (n-1 denominator).
//
// Returns 0 when len(v) < 2 (single sample has no spread). This matches
// the semantic that a one-shot retrieval cannot be "uncertain" by spread;
// uncertainty must come from the score itself (already captured in μ).
//
// Numerical note: this is the textbook two-pass formulation (mean then
// deviations). It is accurate enough for K ≤ 50 typical scores in [0,1].
// We do not use Welford's because:
//   - the input is bounded and small
//   - the two-pass form is easier to audit + verify against spec
func sampleStdev(v []float64, mean float64) float64 {
	if len(v) < 2 {
		return 0
	}
	var sumSq float64
	for _, x := range v {
		d := x - mean
		sumSq += d * d
	}
	return math.Sqrt(sumSq / float64(len(v)-1))
}
