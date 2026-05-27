// SPDX-License-Identifier: MIT
// Package augment — compile-time invariant anchors.
//
// Each sentinel is a no-op function that exists solely to anchor a
// production code path to a documented invariant. compliance tests
// grep production source for the sentinel names; if a sentinel is removed
// (or demoted to a test-only file) the compliance test fails.
//
// Three sentinels ship in C-1:
// - budgetGateRequired (inv-hades-167)
// - capaFirewallAugmentDisabled (inv-hades-170)
// - aggregatorPrivacyFilterRequired (inv-hades-171)
//
// All three are invoked from NewPipeline (production code path) to keep
// them reachable. Removing the call would surface in code review +
// compliance-test grep output.

package augment

// budgetGateRequired returns nil. Invoked from NewPipeline to keep the
// inv-hades-167 anchor reachable.
//
// inv-hades-167: every augmentation request MUST pass through BudgetGate.Check
// before any LLM/MCP cost is incurred.
func budgetGateRequired() error {
	return nil
}

func capaFirewallAugmentDisabled() error {
	return nil
}

// aggregatorPrivacyFilterRequired returns nil. Invoked from NewPipeline to
// keep the inv-hades-171 anchor reachable.
//
// inv-hades-171: aggregator queries that cross project boundaries MUST be
// filtered through PrivacyFilter.
func aggregatorPrivacyFilterRequired() error {
	return nil
}
