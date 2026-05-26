package merge_test

import (
	"testing"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/merge"
)

func TestModeStringer(t *testing.T) {
	cases := []struct {
		m    merge.Mode
		want string
	}{
		{merge.ModeNormal, "Normal"},
		{merge.ModeDegraded60, "Degraded60"},
		{merge.ModeDegraded80, "Degraded80"},
		{merge.ModeEmergencyOnly, "EmergencyOnly"},
	}
	for _, c := range cases {
		if got := c.m.String(); got != c.want {
			t.Errorf("Mode(%d).String() = %q want %q", int(c.m), got, c.want)
		}
	}
}

func TestModeStringerUnknown(t *testing.T) {
	if got := merge.Mode(9999).String(); got != "Unknown" {
		t.Errorf("Mode(9999).String() = %q want Unknown", got)
	}
}

func TestTestTierStringer(t *testing.T) {
	cases := []struct {
		tt   merge.TestTier
		want string
	}{
		{merge.TestTierFull, "Full"},
		{merge.TestTierSmoke, "Smoke"},
		{merge.TestTierSmokeFailFast, "SmokeFailFast"},
	}
	for _, c := range cases {
		if got := c.tt.String(); got != c.want {
			t.Errorf("TestTier(%d).String() = %q want %q", int(c.tt), got, c.want)
		}
	}
}

func TestTestTierUnknown(t *testing.T) {
	if got := merge.TestTier(9999).String(); got != "Unknown" {
		t.Errorf("TestTier(9999).String() = %q want Unknown", got)
	}
}

func TestModeForCanonicalConfigs(t *testing.T) {
	cases := []struct {
		m      merge.Mode
		max    int
		tier   merge.TestTier
		flakes int
	}{
		{merge.ModeNormal, 3, merge.TestTierFull, 2},
		{merge.ModeDegraded60, 2, merge.TestTierFull, 1},
		{merge.ModeDegraded80, 1, merge.TestTierSmoke, 0},
		{merge.ModeEmergencyOnly, 1, merge.TestTierSmokeFailFast, 0},
	}
	for _, c := range cases {
		got := merge.ModeFor(c.m)
		if got.MaxCandidates != c.max {
			t.Errorf("ModeFor(%v).MaxCandidates = %d want %d", c.m, got.MaxCandidates, c.max)
		}
		if got.TestTier != c.tier {
			t.Errorf("ModeFor(%v).TestTier = %v want %v", c.m, got.TestTier, c.tier)
		}
		if got.FlakeRerunBudget != c.flakes {
			t.Errorf("ModeFor(%v).FlakeRerunBudget = %d want %d", c.m, got.FlakeRerunBudget, c.flakes)
		}
	}
}

func TestModeForUnknownPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on unknown Mode")
		}
	}()
	_ = merge.ModeFor(merge.Mode(9999))
}

func TestAllModesCovered(t *testing.T) {
	all := merge.AllModes()
	if len(all) != 5 {
		t.Fatalf("AllModes() len = %d want 5", len(all))
	}
	seen := make(map[merge.Mode]bool)
	for _, m := range all {
		if seen[m] {
			t.Errorf("duplicate Mode in AllModes: %v", m)
		}
		seen[m] = true
		_ = merge.ModeFor(m)
	}
}

func TestModeConfigInvariants(t *testing.T) {
	for _, m := range merge.AllModes() {
		cfg := merge.ModeFor(m)
		if cfg.MaxCandidates < 1 || cfg.MaxCandidates > 5 {
			t.Errorf("ModeFor(%v).MaxCandidates=%d outside [1,5]", m, cfg.MaxCandidates)
		}
		if cfg.FlakeRerunBudget < 0 {
			t.Errorf("ModeFor(%v).FlakeRerunBudget=%d negative", m, cfg.FlakeRerunBudget)
		}
	}
}

func TestModeHighRiskConfig(t *testing.T) {
	cfg := merge.ModeFor(merge.ModeHighRisk)
	if cfg.TestTier != merge.TestTierFull {
		t.Errorf("TestTier = %v; want TestTierFull", cfg.TestTier)
	}
	if cfg.MaxCandidates < 3 {
		t.Errorf("MaxCandidates = %d; want ≥ 3 (max parallelism for thorough vetting)", cfg.MaxCandidates)
	}
	if cfg.FlakeRerunBudget <= merge.ModeFor(merge.ModeNormal).FlakeRerunBudget {
		t.Errorf("FlakeRerunBudget = %d; want > ModeNormal's %d (raised)", cfg.FlakeRerunBudget, merge.ModeFor(merge.ModeNormal).FlakeRerunBudget)
	}
}

func TestModeHighRiskInAllModesAndString(t *testing.T) {
	found := false
	for _, m := range merge.AllModes() {
		if m == merge.ModeHighRisk {
			found = true
		}
	}
	if !found {
		t.Error("ModeHighRisk missing from AllModes()")
	}
	if merge.ModeHighRisk.String() != "HighRisk" {
		t.Errorf("ModeHighRisk.String() = %q; want HighRisk", merge.ModeHighRisk.String())
	}
}

func TestEscalateForBlastRadiusOverridesCostPressure(t *testing.T) {

	for _, base := range merge.AllModes() {
		if got := merge.EscalateForBlastRadius(base, "high"); got != merge.ModeHighRisk {
			t.Errorf("EscalateForBlastRadius(%v, high) = %v; want ModeHighRisk (override cost pressure)", base, got)
		}
	}

	if got := merge.EscalateForBlastRadius(merge.ModeDegraded80, "medium"); got != merge.ModeDegraded80 {
		t.Errorf("EscalateForBlastRadius(Degraded80, medium) = %v; want Degraded80 (no override)", got)
	}
	if got := merge.EscalateForBlastRadius(merge.ModeNormal, "low"); got != merge.ModeNormal {
		t.Errorf("EscalateForBlastRadius(Normal, low) = %v; want Normal", got)
	}
}
