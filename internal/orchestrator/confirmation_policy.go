// SPDX-License-Identifier: MIT
// Package orchestrator provides the autonomous-build supervisor.
//
// confirmation_policy.go implements Q6 D's per-doctrine threshold engine.
// It is stateless and side-effect free: callers (confirmation_handler.go)
// translate ConfirmationAction values into state-machine transitions and event-log
// appends. The threshold map is loaded from doctrine TOML by
//
// Invariants
// - inv-hades-093 (race-safety) is enforced in confirmation_handler.go;
// this file's purity makes that enforcement straightforward.
// - Unknown DecisionClass defaults to mandatory pause (defense-in-depth).
package orchestrator

type DecisionClass string

const (
	DecisionBudgetBreach DecisionClass = "budget_breach"

	DecisionSpecAmendmentProposal DecisionClass = "spec_amendment_proposal"

	DecisionInvariantViolation DecisionClass = "invariant_violation"

	DecisionArchitecturalReviewEscalation DecisionClass = "architectural_review_escalation"

	DecisionHighBlastRadius DecisionClass = "high_blast_radius"
)

type Threshold string

const (
	ThresholdHigh Threshold = "high"

	ThresholdMedium Threshold = "medium"

	ThresholdLow Threshold = "low"

	ThresholdIgnore Threshold = "ignore"
)

type ConfirmationAction int

const (
	ConfirmationActionContinue ConfirmationAction = iota

	ConfirmationActionOptionalPause

	ConfirmationActionMandatoryPause
)

type DecisionEvent struct {
	Class    DecisionClass
	Severity string
	Summary  string
}

type ConfirmationPolicy struct {
	thresholds      map[DecisionClass]Threshold
	optionalEnabled bool
}

func NewConfirmationPolicy(thresholds map[DecisionClass]Threshold, optionalEnabled bool) *ConfirmationPolicy {
	cp := &ConfirmationPolicy{
		thresholds:      make(map[DecisionClass]Threshold, len(thresholds)),
		optionalEnabled: optionalEnabled,
	}
	for k, v := range thresholds {
		cp.thresholds[k] = v
	}
	return cp
}

func (cp *ConfirmationPolicy) Evaluate(class DecisionClass, _ DecisionEvent) ConfirmationAction {
	t, ok := cp.thresholds[class]
	if !ok {
		return ConfirmationActionMandatoryPause
	}
	switch t {
	case ThresholdHigh:
		return ConfirmationActionMandatoryPause
	case ThresholdMedium:
		if cp.optionalEnabled {
			return ConfirmationActionOptionalPause
		}
		return ConfirmationActionContinue
	case ThresholdLow, ThresholdIgnore:
		return ConfirmationActionContinue
	default:

		return ConfirmationActionMandatoryPause
	}
}
