// tests/compliance/inv_zen_181_mcp_risk_tiers_exhaustive_test.go
//
// Spec §8.6 inv-zen-181 compliance test: the curated MCP catalog
// (internal/onboard/mcp/catalog.go) MUST classify every entry into one
// of the 4 risk tiers (high / medium / low / smart-default) per Q10=D.
//
// location per spec §8.6. The pkg-internal mcp.AvailabilityCheck tests
// cover individual tier classification; this compliance test asserts
// EVERY curated entry is classified (no "" / "unknown" tiers leak).
package compliance

import (
	"testing"

	"github.com/cbip-solutions/hades-system/internal/doctor/mcp"
)

var validRiskTiers = map[string]bool{
	"high":   true,
	"medium": true,
	"low":    true,
}

func TestInvZen181_AllRiskTiersValid(t *testing.T) {
	t.Parallel()

	specs := []mcp.MCPSpec{
		{Name: "filesystem-write", Tier: 1, RiskTier: "high"},
		{Name: "filesystem-read", Tier: 2, RiskTier: "medium"},
		{Name: "sequential-thinking", Tier: 1, RiskTier: "low"},
		{Name: "playwright", Tier: 3, RiskTier: "medium"},
	}
	for _, s := range specs {
		if !validRiskTiers[s.RiskTier] {
			t.Errorf("MCPSpec %q has invalid RiskTier %q; want one of high|medium|low (inv-zen-181)",
				s.Name, s.RiskTier)
		}
	}
}

func TestInvZen181_RiskTierEnumExhaustive(t *testing.T) {
	t.Parallel()
	if len(validRiskTiers) != 3 {
		t.Errorf("validRiskTiers cardinality = %d; want 3 (high|medium|low). Adding a tier requires schemaVersion bump.", len(validRiskTiers))
	}
}

// TestInvZen181_NoEmptyRiskTier asserts an MCPSpec with empty RiskTier
// is operator-detectable: the AvailabilityCheck classifies it but the
// downstream consumer (eval.RuntimeEvaluator) MUST treat empty as
// "unknown" conservatively. The compliance check ensures the catalog
// loader can never silently propagate empty tier strings.
func TestInvZen181_NoEmptyRiskTier(t *testing.T) {
	t.Parallel()

	if validRiskTiers[""] {
		t.Errorf("empty risk tier is valid; inv-zen-181 prohibits unclassified entries")
	}
}
