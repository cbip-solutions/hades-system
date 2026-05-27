// go:build property
//go:build property
// +build property

// Package property — p13_mcp_smart_default_deterministic_test.go (Plan
// 13 IMPORTANT 7 missing-tests completion).
//
// Property: onboard.GetDefaults(kind) is deterministic — invoking N times
// with the same kind produces byte-identical WizardDefaults output. The
// catalog-driven MCP selections + LLM provider + doctrine + audit
// retention are stable; no map-iteration randomness leaks.
//
// Per spec §7.3 + Q7=D smart-default contract: defaults are derived
// from the curated MCP catalog + spec-mandated baseline; they MUST be
// reproducible across runs for operator trust + CI determinism.
//
// Build tag `property` excludes from default CI.
package property

import (
	"reflect"
	"testing"
	"testing/quick"

	"github.com/cbip-solutions/hades-system/internal/onboard"
)

func TestProperty_MCPSmartDefault_Deterministic(t *testing.T) {
	cfg := &quick.Config{MaxCount: 30}
	err := quick.Check(func(kindIdx uint8) bool {

		kinds := []onboard.WizardKind{
			onboard.WizardKindGlobal,
			onboard.WizardKindGreenfield,
			onboard.WizardKindBrownfield,
		}
		kind := kinds[int(kindIdx)%len(kinds)]
		first := onboard.GetDefaults(kind)
		for i := 0; i < 5; i++ {
			again := onboard.GetDefaults(kind)
			if !reflect.DeepEqual(first, again) {
				t.Errorf("kind=%v iteration=%d: defaults differ between calls", kind, i)
				return false
			}
		}
		return true
	}, cfg)
	if err != nil {
		t.Fatalf("determinism property failed: %v", err)
	}
}

func TestProperty_MCPSmartDefault_GreenfieldNonEmpty(t *testing.T) {
	d := onboard.GetDefaults(onboard.WizardKindGreenfield)
	if len(d.MCPSelections) == 0 {
		t.Errorf("greenfield MCPSelections empty; want Tier 1+2 baseline (spec §7.3)")
	}

	hasCtld := false
	for _, mcp := range d.MCPSelections {
		if mcp == "zen-swarm-ctld" {
			hasCtld = true
			break
		}
	}
	if !hasCtld {
		t.Errorf("MCPSelections = %v; want 'zen-swarm-ctld' (Tier 1 mandatory)", d.MCPSelections)
	}
}

func TestProperty_MCPSmartDefault_UnknownKindReturnsZero(t *testing.T) {
	d := onboard.GetDefaults(onboard.WizardKindUnknown)
	zero := onboard.WizardDefaults{}
	if !reflect.DeepEqual(d, zero) {
		t.Errorf("GetDefaults(Unknown) = %+v; want zero WizardDefaults", d)
	}
}
