package orchestrator_test

import (
	"sync"
	"testing"

	orch "github.com/cbip-solutions/hades-system/internal/orchestrator"
)

func TestEvaluate_Mandatory(t *testing.T) {
	p := orch.NewConfirmationPolicy(map[orch.DecisionClass]orch.Threshold{
		orch.DecisionBudgetBreach:                  orch.ThresholdHigh,
		orch.DecisionSpecAmendmentProposal:         orch.ThresholdHigh,
		orch.DecisionInvariantViolation:            orch.ThresholdHigh,
		orch.DecisionArchitecturalReviewEscalation: orch.ThresholdHigh,
	}, false)

	for _, cls := range []orch.DecisionClass{
		orch.DecisionBudgetBreach,
		orch.DecisionSpecAmendmentProposal,
		orch.DecisionInvariantViolation,
		orch.DecisionArchitecturalReviewEscalation,
	} {
		got := p.Evaluate(cls, orch.DecisionEvent{Class: cls})
		if got != orch.ConfirmationActionMandatoryPause {
			t.Errorf("Evaluate(%v) = %v, want ConfirmationActionMandatoryPause", cls, got)
		}
	}
}

func TestEvaluate_OptionalDisabled(t *testing.T) {
	p := orch.NewConfirmationPolicy(map[orch.DecisionClass]orch.Threshold{
		orch.DecisionBudgetBreach: orch.ThresholdMedium,
	}, false)

	got := p.Evaluate(orch.DecisionBudgetBreach, orch.DecisionEvent{Class: orch.DecisionBudgetBreach})
	if got != orch.ConfirmationActionContinue {
		t.Errorf("Evaluate(medium, optional=false) = %v, want ConfirmationActionContinue", got)
	}
}

func TestEvaluate_OptionalEnabled(t *testing.T) {
	p := orch.NewConfirmationPolicy(map[orch.DecisionClass]orch.Threshold{
		orch.DecisionBudgetBreach: orch.ThresholdMedium,
	}, true)

	got := p.Evaluate(orch.DecisionBudgetBreach, orch.DecisionEvent{Class: orch.DecisionBudgetBreach})
	if got != orch.ConfirmationActionOptionalPause {
		t.Errorf("Evaluate(medium, optional=true) = %v, want ConfirmationActionOptionalPause", got)
	}
}

func TestEvaluate_LowAndIgnore(t *testing.T) {
	p := orch.NewConfirmationPolicy(map[orch.DecisionClass]orch.Threshold{
		orch.DecisionBudgetBreach:          orch.ThresholdLow,
		orch.DecisionSpecAmendmentProposal: orch.ThresholdIgnore,
	}, true)

	if got := p.Evaluate(orch.DecisionBudgetBreach, orch.DecisionEvent{}); got != orch.ConfirmationActionContinue {
		t.Errorf("low → %v, want ConfirmationActionContinue", got)
	}
	if got := p.Evaluate(orch.DecisionSpecAmendmentProposal, orch.DecisionEvent{}); got != orch.ConfirmationActionContinue {
		t.Errorf("ignore → %v, want ConfirmationActionContinue", got)
	}
}

func TestEvaluate_UnknownClass_DefaultsToMandatory(t *testing.T) {

	p := orch.NewConfirmationPolicy(map[orch.DecisionClass]orch.Threshold{}, false)
	got := p.Evaluate(orch.DecisionInvariantViolation, orch.DecisionEvent{Class: orch.DecisionInvariantViolation})
	if got != orch.ConfirmationActionMandatoryPause {
		t.Errorf("unknown class default = %v, want ConfirmationActionMandatoryPause", got)
	}
}

func TestEvaluate_UnknownThresholdValue_DefaultsToMandatory(t *testing.T) {

	p := orch.NewConfirmationPolicy(map[orch.DecisionClass]orch.Threshold{
		orch.DecisionBudgetBreach: orch.Threshold("garbage"),
	}, false)

	got := p.Evaluate(orch.DecisionBudgetBreach, orch.DecisionEvent{Class: orch.DecisionBudgetBreach})
	if got != orch.ConfirmationActionMandatoryPause {
		t.Errorf("unknown threshold value → %v, want ConfirmationActionMandatoryPause", got)
	}
}

func TestNewConfirmationPolicy_CopiesInputMap(t *testing.T) {
	// Constructor must copy the input map so that caller mutations do not
	// affect the policy's behavior. This is a load-bearing property for
	// safe concurrent access.
	inputMap := map[orch.DecisionClass]orch.Threshold{
		orch.DecisionBudgetBreach: orch.ThresholdHigh,
	}
	p := orch.NewConfirmationPolicy(inputMap, false)

	inputMap[orch.DecisionBudgetBreach] = orch.ThresholdLow
	inputMap[orch.DecisionInvariantViolation] = orch.ThresholdHigh

	got := p.Evaluate(orch.DecisionBudgetBreach, orch.DecisionEvent{Class: orch.DecisionBudgetBreach})
	if got != orch.ConfirmationActionMandatoryPause {
		t.Errorf("after caller mutation, Evaluate(BudgetBreach) = %v, want ConfirmationActionMandatoryPause", got)
	}

	got2 := p.Evaluate(orch.DecisionInvariantViolation, orch.DecisionEvent{})
	if got2 != orch.ConfirmationActionMandatoryPause {
		t.Errorf("after caller mutation, Evaluate(InvariantViolation) = %v, want ConfirmationActionMandatoryPause", got2)
	}
}

func TestDecisionHighBlastRadiusPausesInAutonomy(t *testing.T) {
	pol := orch.NewConfirmationPolicy(map[orch.DecisionClass]orch.Threshold{
		orch.DecisionHighBlastRadius: orch.ThresholdHigh,
	}, false)
	got := pol.Evaluate(orch.DecisionHighBlastRadius, orch.DecisionEvent{Class: orch.DecisionHighBlastRadius})
	if got != orch.ConfirmationActionMandatoryPause {
		t.Errorf("Evaluate(high_blast_radius@high) = %v; want ConfirmationActionMandatoryPause", got)
	}
}

func TestDecisionHighBlastRadiusUnmappedStillPauses(t *testing.T) {
	pol := orch.NewConfirmationPolicy(map[orch.DecisionClass]orch.Threshold{}, false)
	got := pol.Evaluate(orch.DecisionHighBlastRadius, orch.DecisionEvent{Class: orch.DecisionHighBlastRadius})
	if got != orch.ConfirmationActionMandatoryPause {
		t.Errorf("Evaluate(unmapped high_blast_radius) = %v; want MandatoryPause (defense-in-depth)", got)
	}
}

func TestDecisionHighBlastRadiusValue(t *testing.T) {
	if orch.DecisionHighBlastRadius != "high_blast_radius" {
		t.Errorf("DecisionHighBlastRadius = %q; want \"high_blast_radius\"", orch.DecisionHighBlastRadius)
	}
}

func TestEvaluate_RaceFree(t *testing.T) {

	p := orch.NewConfirmationPolicy(map[orch.DecisionClass]orch.Threshold{
		orch.DecisionBudgetBreach:                  orch.ThresholdHigh,
		orch.DecisionSpecAmendmentProposal:         orch.ThresholdMedium,
		orch.DecisionInvariantViolation:            orch.ThresholdLow,
		orch.DecisionArchitecturalReviewEscalation: orch.ThresholdIgnore,
	}, true)

	var wg sync.WaitGroup
	numGoroutines := 100
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			classes := []orch.DecisionClass{
				orch.DecisionBudgetBreach,
				orch.DecisionSpecAmendmentProposal,
				orch.DecisionInvariantViolation,
				orch.DecisionArchitecturalReviewEscalation,
			}
			for _, cls := range classes {
				evt := orch.DecisionEvent{
					Class:    cls,
					Severity: "test",
					Summary:  "goroutine " + string(rune(idx)),
				}
				_ = p.Evaluate(cls, evt)
			}
		}(i)
	}

	wg.Wait()

}
