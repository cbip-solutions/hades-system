package autonomy_test

import (
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/autonomy"
)

func TestApplyOverride_TightenAllowed(t *testing.T) {
	cases := []struct {
		base, override, want autonomy.Tier
	}{

		{autonomy.TierSoft, autonomy.TierHard, autonomy.TierHard},

		{autonomy.TierInformational, autonomy.TierHard, autonomy.TierHard},

		{autonomy.TierInformational, autonomy.TierSoft, autonomy.TierSoft},

		{autonomy.TierHard, autonomy.TierHard, autonomy.TierHard},
		{autonomy.TierSoft, autonomy.TierSoft, autonomy.TierSoft},
		{autonomy.TierInformational, autonomy.TierInformational, autonomy.TierInformational},
	}
	for _, c := range cases {
		if got := autonomy.ApplyOverride(c.base, c.override); got != c.want {
			t.Errorf("ApplyOverride(%v, %v): want %v got %v", c.base, c.override, c.want, got)
		}
	}
}

func TestApplyOverride_ZeroOverrideIsNoOp(t *testing.T) {
	for _, base := range []autonomy.Tier{autonomy.TierInformational, autonomy.TierSoft, autonomy.TierHard} {
		if got := autonomy.ApplyOverride(base, 0); got != base {
			t.Errorf("ApplyOverride(%v, 0): want %v got %v", base, base, got)
		}
	}
}

func TestApplyOverride_WeakerOverrideKeepsBaseline(t *testing.T) {

	cases := []struct {
		base, override autonomy.Tier
	}{
		{autonomy.TierHard, autonomy.TierSoft},
		{autonomy.TierHard, autonomy.TierInformational},
		{autonomy.TierSoft, autonomy.TierInformational},
	}
	for _, c := range cases {
		if got := autonomy.ApplyOverride(c.base, c.override); got != c.base {
			t.Errorf("ApplyOverride(%v, %v) must keep baseline; got %v", c.base, c.override, got)
		}
	}
}

func TestValidateOverrides_LooseningRejected(t *testing.T) {

	for _, doctrine := range autonomy.AllDoctrineNames() {
		for _, weaker := range []autonomy.Tier{autonomy.TierSoft, autonomy.TierInformational} {
			err := autonomy.ValidateOverrides(doctrine, map[string]autonomy.Tier{
				autonomy.CheckResearchMCPUp: weaker,
			})
			if err == nil {
				t.Errorf("doctrine=%s, override hard→%s: expected loosening error", doctrine, weaker)
				continue
			}
			if !strings.Contains(err.Error(), "tighten-only") {
				t.Errorf("loosening error must mention tighten-only; got %v", err)
			}
		}
	}

	err := autonomy.ValidateOverrides("default", map[string]autonomy.Tier{
		autonomy.CheckCaronteIndexCurrency: autonomy.TierInformational,
	})
	if err == nil {
		t.Fatalf("soft→informational must be rejected")
	}
}

func TestValidateOverrides_TightenAccepted(t *testing.T) {

	overrides := map[string]autonomy.Tier{
		autonomy.CheckCaronteIndexCurrency: autonomy.TierHard,
		autonomy.CheckSystemStateTOML:      autonomy.TierHard,
	}
	if err := autonomy.ValidateOverrides("default", overrides); err != nil {
		t.Fatalf("tighten must be accepted: %v", err)
	}
}

func TestValidateOverrides_NoOpOverrideAccepted(t *testing.T) {

	overrides := map[string]autonomy.Tier{
		autonomy.CheckResearchMCPUp: autonomy.TierHard,
	}
	for _, d := range autonomy.AllDoctrineNames() {
		if err := autonomy.ValidateOverrides(d, overrides); err != nil {
			t.Errorf("doctrine=%s: no-op override must be accepted; got %v", d, err)
		}
	}
}

func TestValidateOverrides_TightenInformationalToSoftAccepted(t *testing.T) {

	overrides := map[string]autonomy.Tier{
		autonomy.CheckAmendmentDryRunApproved: autonomy.TierSoft,
	}
	if err := autonomy.ValidateOverrides("default", overrides); err != nil {
		t.Fatalf("informational→soft must be accepted: %v", err)
	}
}

func TestValidateOverrides_UnknownCheckRejected(t *testing.T) {
	overrides := map[string]autonomy.Tier{"no_such_check": autonomy.TierHard}
	err := autonomy.ValidateOverrides("default", overrides)
	if err == nil {
		t.Fatalf("unknown check name must be rejected")
	}
	if !strings.Contains(err.Error(), "no_such_check") {
		t.Errorf("error must cite the offending check; got %v", err)
	}
}

func TestValidateOverrides_UnknownDoctrineRejected(t *testing.T) {
	overrides := map[string]autonomy.Tier{autonomy.CheckResearchMCPUp: autonomy.TierHard}
	err := autonomy.ValidateOverrides("no-such", overrides)
	if err == nil {
		t.Fatalf("unknown doctrine must be rejected")
	}
}

func TestValidateOverrides_InvalidTierValueRejected(t *testing.T) {
	overrides := map[string]autonomy.Tier{autonomy.CheckResearchMCPUp: autonomy.Tier(99)}
	err := autonomy.ValidateOverrides("default", overrides)
	if err == nil {
		t.Fatalf("invalid tier value must be rejected")
	}
	if !strings.Contains(err.Error(), "invalid tier value") {
		t.Errorf("error must cite invalid tier; got %v", err)
	}
}

func TestValidateOverrides_EmptyMapAccepted(t *testing.T) {
	if err := autonomy.ValidateOverrides("default", nil); err != nil {
		t.Errorf("nil overrides must be accepted: %v", err)
	}
	if err := autonomy.ValidateOverrides("default", map[string]autonomy.Tier{}); err != nil {
		t.Errorf("empty overrides must be accepted: %v", err)
	}
}
