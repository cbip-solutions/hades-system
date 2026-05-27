// SPDX-License-Identifier: MIT
package v1

import (
	"fmt"
)

type CrossFieldViolation struct {
	InvariantID string
	Detail      string
}

func (e *CrossFieldViolation) Error() string {
	return fmt.Sprintf("doctrine: cross-field invariant %s violated: %s", e.InvariantID, e.Detail)
}

func (e *CrossFieldViolation) Is(target error) bool { return target == ErrValidationFailed }

// validateCrossField runs the v1 cross-field invariant suite. Returns a
// slice of errors (joined by Validate's caller). Order is stable per
// invariant ID for predictable error messages.
//
// Reviewer IMPORTANT #4: this used to be reachable via an exported
// package-var function pointer (var ValidateCrossField), which an external
// caller could replace with a no-op to bypass the cross-field checks.
// crossfield.go and validate.go live in the same package v1 — the
// "import cycle" justification for the indirection was incorrect — so we
// keep the function package-private. Validate() in validate.go calls it
// directly. External callers MUST go through Schema.Validate() to
// exercise these checks; there is no other public surface.
func validateCrossField(s *Schema) []error {
	var errs []error

	// CFI-MergeScoringWeightsSum100 — Merge.ScoringWeights MUST sum to 100.
	sum := s.Merge.ScoringWeights.TestPass + s.Merge.ScoringWeights.LintPass +
		s.Merge.ScoringWeights.Coverage + s.Merge.ScoringWeights.Diff + s.Merge.ScoringWeights.Duration
	if sum != 100 {
		errs = append(errs, &CrossFieldViolation{
			InvariantID: "MergeScoringWeightsSum100",
			Detail:      fmt.Sprintf("Merge.ScoringWeights TestPass+LintPass+Coverage+Diff+Duration = %d; want 100", sum),
		})
	}

	if s.Workforce.Recovery.TransientRetryBudget > 0 && s.Workforce.MaxDepth <= s.Workforce.MinDepth {
		errs = append(errs, &CrossFieldViolation{
			InvariantID: "WorkforceDepthHeadroomForRetries",
			Detail:      fmt.Sprintf("Workforce.Recovery.TransientRetryBudget=%d but MaxDepth=%d <= MinDepth=%d (no headroom for retries)", s.Workforce.Recovery.TransientRetryBudget, s.Workforce.MaxDepth, s.Workforce.MinDepth),
		})
	}

	if s.Autonomy.CostDegradation.HardStopUSD == s.Autonomy.CostDegradation.SoftCheckUSD {
		errs = append(errs, &CrossFieldViolation{
			InvariantID: "CostDegradationHardStopStrictlyGreater",
			Detail:      fmt.Sprintf("Autonomy.CostDegradation.HardStopUSD=%d must be strictly greater than SoftCheckUSD=%d", s.Autonomy.CostDegradation.HardStopUSD, s.Autonomy.CostDegradation.SoftCheckUSD),
		})
	}

	if !isMonotoneFromOne(s.HRA.LayersEnabled) {
		errs = append(errs, &CrossFieldViolation{
			InvariantID: "HRALayersMonotoneFromOne",
			Detail:      fmt.Sprintf("HRA.LayersEnabled %v: must be ascending starting from 1 with no gaps (e.g., [1], [1,2], [1,2,3])", s.HRA.LayersEnabled),
		})
	}

	if s.HadesDayCadence.MorningBriefIfWithinHours > 24 {
		errs = append(errs, &CrossFieldViolation{
			InvariantID: "MorningBriefWithinAtMostOneDay",
			Detail:      fmt.Sprintf("HadesDayCadence.MorningBriefIfWithinHours=%d > 24", s.HadesDayCadence.MorningBriefIfWithinHours),
		})
	}

	if s.HadesDayCadence.EODDigestIfWithinHours > 24 {
		errs = append(errs, &CrossFieldViolation{
			InvariantID: "EODDigestWithinAtMostOneDay",
			Detail:      fmt.Sprintf("HadesDayCadence.EODDigestIfWithinHours=%d > 24", s.HadesDayCadence.EODDigestIfWithinHours),
		})
	}

	capacity := s.Workforce.MaxDepth * s.Workforce.MaxWidthPerLayer
	if capacity > 0 && s.Quota.MaxConcurrentTasks > capacity {
		errs = append(errs, &CrossFieldViolation{
			InvariantID: "QuotaWithinPoolCapacity",
			Detail:      fmt.Sprintf("Quota.MaxConcurrentTasks=%d > Workforce.MaxDepth*MaxWidthPerLayer=%d (oversubscription)", s.Quota.MaxConcurrentTasks, capacity),
		})
	}

	if s.Notifications.QuietHoursStart == s.Notifications.QuietHoursEnd && s.Notifications.QuietHoursStart != "" {
		errs = append(errs, &CrossFieldViolation{
			InvariantID: "QuietHoursStartEndDistinct",
			Detail:      fmt.Sprintf("Notifications.QuietHoursStart and QuietHoursEnd both = %q; must differ", s.Notifications.QuietHoursStart),
		})
	}

	if s.WFQ.OvercommitPolicy == "reject" && s.Quota.MaxConcurrentTasks > 256 {
		errs = append(errs, &CrossFieldViolation{
			InvariantID: "RejectOvercommitImpliesBoundedQuota",
			Detail:      fmt.Sprintf("WFQ.OvercommitPolicy=reject but Quota.MaxConcurrentTasks=%d > 256 (Plan 7 ceiling)", s.Quota.MaxConcurrentTasks),
		})
	}

	if len(s.Gates.TestTiers.Enabled) == 0 {
		errs = append(errs, &CrossFieldViolation{
			InvariantID: "TestTiersNonEmpty",
			Detail:      "Gates.TestTiers.Enabled must contain at least one tier (e.g., 'unit')",
		})
	}

	return errs
}

func isMonotoneFromOne(xs []int) bool {
	for i, x := range xs {
		if x != i+1 {
			return false
		}
	}
	return true
}
