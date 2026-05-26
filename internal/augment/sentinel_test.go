package augment

import "testing"

func TestSentinelsReturnNil(t *testing.T) {
	if err := budgetGateRequired(); err != nil {
		t.Errorf("budgetGateRequired: %v (must be nil)", err)
	}
	if err := capaFirewallAugmentDisabled(); err != nil {
		t.Errorf("capaFirewallAugmentDisabled: %v (must be nil)", err)
	}
	if err := aggregatorPrivacyFilterRequired(); err != nil {
		t.Errorf("aggregatorPrivacyFilterRequired: %v (must be nil)", err)
	}
}
