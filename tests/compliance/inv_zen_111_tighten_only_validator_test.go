// tests/compliance/inv_zen_111_tighten_only_validator_test.go
//
// Compliance gate for inv-zen-111: doctrine bounds matrix is TIGHTEN-only
// at the project layer. Per spec §8.3 inv-zen-111, project-level overrides
// to ScoringConfig MUST be allowed only in the tighter direction:
//
//   - BetaPatchSizePenalty: tighter = HIGHER  (override < base → REJECT)
//   - GammaFlakePenalty:    tighter = HIGHER  (override < base → REJECT)
//   - AlphaReviewerWeight:  NEUTRAL — doctrine philosophy lever; either
//     direction OK
//
// This is the load-bearing safety contract behind the per-project
// override surface (Phase F TOML loader). Without it, a project could
// silently weaken the global doctrine matrix — directly contradicting
// the max-scope/no-defer/no-tech-debt project doctrine that the bounds
// matrix encodes. The compliance test here is the public-API-surface
// expression of the rule: the merge package's exported
// ValidateTightenOnly + ErrLooseAttemptRejected must stay aligned with
// the documented direction-of-tightening per axis, and the rejection
// error must name the offending field so operators see WHICH axis a
// project override violated.
//
// Three sibling assertions:
//  1. TestInvZen111ValidateTightenOnlyAcceptsTightening — five legal
//     directions (equal, β tighten, γ tighten, α neutral lower/higher)
//     all return nil.
//  2. TestInvZen111ValidateTightenOnlyRejectsLoosen — two illegal
//     directions (β loosen, γ loosen) return ErrLooseAttemptRejected
//     with the offending field name embedded in the error message.
//  3. TestInvZen111ValidateTightenOnlyAlphaIsNeutral — α at six
//     spread-apart values (0, 0.5, 1, 1.5, 5, 100) all return nil
//     against the same base, confirming the philosophy-lever semantics.
//
// Note (per C-2 implementer pattern): we use the canonical
// AlphaReviewerWeight / BetaPatchSizePenalty / GammaFlakePenalty field
// names rather than the plan snippet's `Alpha` / `Beta` / `Gamma`
// shorthand. The shorthand was a draft-time abbreviation; the production
// type uses the full names.
//
// Drift adaptation per Task C-6 instructions: package compliance (not
// compliance_test, which the plan snippet uses) to match the
// predominant tests/compliance convention (31 files vs 8). No emitter
// or clock is needed for these tests — ValidateTightenOnly is a pure
// function over ScoringConfig values.
//
// Reference: docs/superpowers/specs/2026-05-01-zen-swarm-plan-6-merge-engine-design.md §8.3 inv-zen-111
package compliance

import (
	"errors"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/merge"
)

func TestInvZen111ValidateTightenOnlyAcceptsTightening(t *testing.T) {
	base := merge.ScoringConfig{
		AlphaReviewerWeight:  1.0,
		BetaPatchSizePenalty: 0.0,
		GammaFlakePenalty:    2.0,
	}
	cases := []merge.ScoringConfig{

		{AlphaReviewerWeight: 1.0, BetaPatchSizePenalty: 0.0, GammaFlakePenalty: 2.0},

		{AlphaReviewerWeight: 1.0, BetaPatchSizePenalty: 0.5, GammaFlakePenalty: 2.0},

		{AlphaReviewerWeight: 1.0, BetaPatchSizePenalty: 0.0, GammaFlakePenalty: 5.0},

		{AlphaReviewerWeight: 0.5, BetaPatchSizePenalty: 0.0, GammaFlakePenalty: 2.0},

		{AlphaReviewerWeight: 1.5, BetaPatchSizePenalty: 0.0, GammaFlakePenalty: 2.0},
	}
	for i, c := range cases {
		if err := merge.ValidateTightenOnly(base, c); err != nil {
			t.Errorf("inv-zen-111 VIOLATION: case %d (%+v): ValidateTightenOnly returned err = %v on accepted direction (want nil)",
				i, c, err)
		}
	}
}

func TestInvZen111ValidateTightenOnlyRejectsLoosen(t *testing.T) {
	base := merge.ScoringConfig{
		AlphaReviewerWeight:  1.0,
		BetaPatchSizePenalty: 0.5,
		GammaFlakePenalty:    2.0,
	}
	cases := []struct {
		override merge.ScoringConfig
		field    string
	}{
		{
			merge.ScoringConfig{AlphaReviewerWeight: 1.0, BetaPatchSizePenalty: 0.0, GammaFlakePenalty: 2.0},
			"BetaPatchSizePenalty",
		},
		{
			merge.ScoringConfig{AlphaReviewerWeight: 1.0, BetaPatchSizePenalty: 0.5, GammaFlakePenalty: 1.0},
			"GammaFlakePenalty",
		},
	}
	for i, c := range cases {
		err := merge.ValidateTightenOnly(base, c.override)
		if !errors.Is(err, merge.ErrLooseAttemptRejected) {
			t.Errorf("inv-zen-111 VIOLATION: case %d (%+v): err = %v want wrapped ErrLooseAttemptRejected",
				i, c.override, err)
			continue
		}
		if !strings.Contains(err.Error(), c.field) {
			t.Errorf("inv-zen-111 VIOLATION: case %d (%+v): error %q does not name field %q",
				i, c.override, err.Error(), c.field)
		}
	}
}

func TestInvZen111ValidateTightenOnlyAlphaIsNeutral(t *testing.T) {
	base := merge.ScoringConfig{AlphaReviewerWeight: 1.0}
	for _, v := range []float64{0.0, 0.5, 1.0, 1.5, 5.0, 100.0} {
		o := merge.ScoringConfig{AlphaReviewerWeight: v}
		if err := merge.ValidateTightenOnly(base, o); err != nil {
			t.Errorf("inv-zen-111 VIOLATION: AlphaReviewerWeight=%v rejected (expected neutral): %v", v, err)
		}
	}
}
