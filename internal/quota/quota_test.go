package quota

import (
	"errors"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/doctrine"
)

func TestDoctrineDefaultsMaxScope(t *testing.T) {
	got := DoctrineDefaults(doctrine.NameMaxScope)
	if got.SoftCapPct != 80 {
		t.Errorf("SoftCapPct = %d, want 80", got.SoftCapPct)
	}
	if got.HardCapPct != 100 {
		t.Errorf("HardCapPct = %d, want 100", got.HardCapPct)
	}
	if got.Mode != ModeWarnOnly {
		t.Errorf("Mode = %v, want ModeWarnOnly", got.Mode)
	}
}

func TestDoctrineDefaultsDefault(t *testing.T) {
	got := DoctrineDefaults(doctrine.NameDefault)
	if got.SoftCapPct != 80 {
		t.Errorf("SoftCapPct = %d, want 80", got.SoftCapPct)
	}
	if got.HardCapPct != 100 {
		t.Errorf("HardCapPct = %d, want 100", got.HardCapPct)
	}
	if got.Mode != ModeSoftHard {
		t.Errorf("Mode = %v, want ModeSoftHard", got.Mode)
	}
}

func TestDoctrineDefaultsCapaFirewall(t *testing.T) {
	got := DoctrineDefaults(doctrine.NameCapaFirewall)
	if got.SoftCapPct != 80 {
		t.Errorf("SoftCapPct = %d, want 80 (Pulido §3.5 keeps soft warning at 80%%)", got.SoftCapPct)
	}
	if got.HardCapPct != 95 {
		t.Errorf("HardCapPct = %d, want 95 (capa-firewall extra margin)", got.HardCapPct)
	}
	if got.Mode != ModeExtraMargin {
		t.Errorf("Mode = %v, want ModeExtraMargin", got.Mode)
	}
}

func TestDoctrineDefaultsUnknownDoctrineFallsBackToDefault(t *testing.T) {
	got := DoctrineDefaults(doctrine.Name("unknown-doctrine-xyz"))
	want := DoctrineDefaults(doctrine.NameDefault)
	if got != want {
		t.Errorf("DoctrineDefaults(unknown) = %+v, want %+v (default fallback)", got, want)
	}
}

func TestDoctrineConstantsMatchInternalDoctrineNames(t *testing.T) {

	cases := []struct {
		got  doctrine.Name
		want string
	}{
		{doctrine.NameMaxScope, "max-scope"},
		{doctrine.NameDefault, "default"},
		{doctrine.NameCapaFirewall, "capa-firewall"},
	}
	for _, c := range cases {
		if string(c.got) != c.want {
			t.Errorf("doctrine.Name = %q, want %q", string(c.got), c.want)
		}
	}
}

func TestDoctrineMatrixSentinelReturnsErr(t *testing.T) {

	err := quotaDoctrineMatrixSentinel()
	if !errors.Is(err, ErrDoctrineMatrixAnchor) {
		t.Errorf("quotaDoctrineMatrixSentinel returned %v, want ErrDoctrineMatrixAnchor", err)
	}
}

func TestModeStringStable(t *testing.T) {
	cases := []struct {
		mode Mode
		want string
	}{
		{ModeWarnOnly, "warn-only"},
		{ModeSoftHard, "soft-hard"},
		{ModeExtraMargin, "extra-margin"},
		{Mode(99), "unknown"},
	}
	for _, c := range cases {
		if got := c.mode.String(); got != c.want {
			t.Errorf("Mode(%d).String() = %q, want %q", int(c.mode), got, c.want)
		}
	}
}

func TestResolveThresholdsNoOverrideUsesDoctrine(t *testing.T) {
	got := ResolveThresholds("internal-platform-x", doctrine.NameMaxScope, nil)
	want := DoctrineDefaults(doctrine.NameMaxScope)
	if got != want {
		t.Errorf("ResolveThresholds(no override) = %+v, want %+v", got, want)
	}
}

func TestResolveThresholdsValidOverrideWins(t *testing.T) {
	override := &ProjectQuotaOverride{SoftCapPct: 50, HardCapPct: 90}
	got := ResolveThresholds("internal-platform-x", doctrine.NameDefault, override)
	if got.SoftCapPct != 50 {
		t.Errorf("SoftCapPct = %d, want 50 (override wins)", got.SoftCapPct)
	}
	if got.HardCapPct != 90 {
		t.Errorf("HardCapPct = %d, want 90 (override wins)", got.HardCapPct)
	}

	if got.Mode != ModeSoftHard {
		t.Errorf("Mode = %v, want ModeSoftHard (doctrine-bound)", got.Mode)
	}
}

func TestResolveThresholdsCapaFirewallModePreserved(t *testing.T) {

	override := &ProjectQuotaOverride{SoftCapPct: 70, HardCapPct: 100}
	got := ResolveThresholds("internal-platform-x", doctrine.NameCapaFirewall, override)
	if got.SoftCapPct != 70 || got.HardCapPct != 100 {
		t.Errorf("override pcts ignored: %+v", got)
	}
	if got.Mode != ModeExtraMargin {
		t.Errorf("Mode = %v, want ModeExtraMargin (capa-firewall doctrine-bound)", got.Mode)
	}
}

func TestResolveThresholdsMaxScopeModePreserved(t *testing.T) {

	override := &ProjectQuotaOverride{SoftCapPct: 60, HardCapPct: 95}
	got := ResolveThresholds("internal-platform-x", doctrine.NameMaxScope, override)
	if got.SoftCapPct != 60 || got.HardCapPct != 95 {
		t.Errorf("override pcts ignored: %+v", got)
	}
	if got.Mode != ModeWarnOnly {
		t.Errorf("Mode = %v, want ModeWarnOnly (max-scope doctrine-bound)", got.Mode)
	}
}

func TestResolveThresholdsInvalidOverrideFallsBack(t *testing.T) {
	cases := []struct {
		name     string
		override ProjectQuotaOverride
	}{
		{"soft greater than hard", ProjectQuotaOverride{SoftCapPct: 90, HardCapPct: 80}},
		{"soft below 1", ProjectQuotaOverride{SoftCapPct: 0, HardCapPct: 100}},
		{"hard above 100", ProjectQuotaOverride{SoftCapPct: 80, HardCapPct: 150}},
		{"both zero", ProjectQuotaOverride{SoftCapPct: 0, HardCapPct: 0}},
		{"negative soft", ProjectQuotaOverride{SoftCapPct: -10, HardCapPct: 100}},
		{"negative hard", ProjectQuotaOverride{SoftCapPct: 50, HardCapPct: -1}},
		{"soft above 100", ProjectQuotaOverride{SoftCapPct: 101, HardCapPct: 100}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ResolveThresholds("internal-platform-x", doctrine.NameDefault, &c.override)
			want := DoctrineDefaults(doctrine.NameDefault)
			if got != want {
				t.Errorf("ResolveThresholds(%+v) = %+v, want %+v (fallback)", c.override, got, want)
			}
		})
	}
}

func TestResolveThresholdsSoftEqualsHardAllowed(t *testing.T) {

	override := &ProjectQuotaOverride{SoftCapPct: 90, HardCapPct: 90}
	got := ResolveThresholds("internal-platform-x", doctrine.NameDefault, override)
	if got.SoftCapPct != 90 || got.HardCapPct != 90 {
		t.Errorf("soft=hard not honoured: %+v", got)
	}
	if got.Mode != ModeSoftHard {
		t.Errorf("Mode = %v, want ModeSoftHard (doctrine-bound)", got.Mode)
	}
}

func TestResolveThresholdsBoundaryValues(t *testing.T) {

	cases := []struct {
		name     string
		override ProjectQuotaOverride
	}{
		{"soft=1 hard=1", ProjectQuotaOverride{SoftCapPct: 1, HardCapPct: 1}},
		{"soft=1 hard=100", ProjectQuotaOverride{SoftCapPct: 1, HardCapPct: 100}},
		{"soft=100 hard=100", ProjectQuotaOverride{SoftCapPct: 100, HardCapPct: 100}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ResolveThresholds("internal-platform-x", doctrine.NameDefault, &c.override)
			if got.SoftCapPct != c.override.SoftCapPct {
				t.Errorf("SoftCapPct = %d, want %d", got.SoftCapPct, c.override.SoftCapPct)
			}
			if got.HardCapPct != c.override.HardCapPct {
				t.Errorf("HardCapPct = %d, want %d", got.HardCapPct, c.override.HardCapPct)
			}
		})
	}
}

func TestResolveThresholdsUnknownDoctrineFallsBack(t *testing.T) {

	override := &ProjectQuotaOverride{SoftCapPct: 60, HardCapPct: 90}
	got := ResolveThresholds("typo-project", doctrine.Name("unknown-doctrine-xyz"), override)
	if got.SoftCapPct != 60 || got.HardCapPct != 90 {
		t.Errorf("override pcts ignored on unknown doctrine: %+v", got)
	}

	if got.Mode != ModeSoftHard {
		t.Errorf("Mode = %v, want ModeSoftHard (unknown→default fallback)", got.Mode)
	}
}

func TestClassifyUsageBelowSoft(t *testing.T) {
	thr := DoctrineDefaults(doctrine.NameDefault)
	got := ClassifyUsage(50, 100, thr)
	if got != CapStatusOK {
		t.Errorf("ClassifyUsage(50/100) = %v, want CapStatusOK", got)
	}
}

func TestClassifyUsageAtSoftCap(t *testing.T) {
	thr := DoctrineDefaults(doctrine.NameDefault)
	got := ClassifyUsage(80, 100, thr)
	if got != CapStatusSoftWarn {
		t.Errorf("ClassifyUsage(80/100) = %v, want CapStatusSoftWarn", got)
	}
}

func TestClassifyUsageBetweenSoftAndHard(t *testing.T) {
	thr := DoctrineDefaults(doctrine.NameDefault)
	got := ClassifyUsage(95, 100, thr)
	if got != CapStatusSoftWarn {
		t.Errorf("ClassifyUsage(95/100) = %v, want CapStatusSoftWarn", got)
	}
}

func TestClassifyUsageAtHardCapDefaultDeny(t *testing.T) {
	thr := DoctrineDefaults(doctrine.NameDefault)
	got := ClassifyUsage(100, 100, thr)
	if got != CapStatusHardDeny {
		t.Errorf("ClassifyUsage(100/100, default) = %v, want CapStatusHardDeny", got)
	}
}

func TestClassifyUsageAboveHardCapDefaultDeny(t *testing.T) {
	thr := DoctrineDefaults(doctrine.NameDefault)
	got := ClassifyUsage(150, 100, thr)
	if got != CapStatusHardDeny {
		t.Errorf("ClassifyUsage(150/100, default) = %v, want CapStatusHardDeny", got)
	}
}

func TestClassifyUsageAtHardCapMaxScopeLogOnly(t *testing.T) {
	thr := DoctrineDefaults(doctrine.NameMaxScope)
	got := ClassifyUsage(100, 100, thr)
	if got != CapStatusHardLogOnly {
		t.Errorf("ClassifyUsage(100/100, max-scope) = %v, want CapStatusHardLogOnly (never silently deny)", got)
	}
}

func TestClassifyUsageAboveHardCapMaxScopeStillLogOnly(t *testing.T) {
	thr := DoctrineDefaults(doctrine.NameMaxScope)
	got := ClassifyUsage(200, 100, thr)
	if got != CapStatusHardLogOnly {
		t.Errorf("ClassifyUsage(200/100, max-scope) = %v, want CapStatusHardLogOnly", got)
	}
}

func TestClassifyUsageAtHardCapCapaFirewall(t *testing.T) {
	thr := DoctrineDefaults(doctrine.NameCapaFirewall)
	got := ClassifyUsage(95, 100, thr)
	if got != CapStatusHardDeny {
		t.Errorf("ClassifyUsage(95/100, capa-firewall) = %v, want CapStatusHardDeny (denies at 95)", got)
	}
}

func TestClassifyUsageAt94CapaFirewallStillSoft(t *testing.T) {
	thr := DoctrineDefaults(doctrine.NameCapaFirewall)
	got := ClassifyUsage(94, 100, thr)
	if got != CapStatusSoftWarn {
		t.Errorf("ClassifyUsage(94/100, capa-firewall) = %v, want CapStatusSoftWarn (below 95)", got)
	}
}

func TestClassifyUsageZeroCapNeverDenies(t *testing.T) {

	thr := DoctrineDefaults(doctrine.NameDefault)
	cases := []int64{0, 100, 1000000}
	for _, used := range cases {
		got := ClassifyUsage(used, 0, thr)
		if got != CapStatusOK {
			t.Errorf("ClassifyUsage(%d/0) = %v, want CapStatusOK (no cap)", used, got)
		}
	}
}

func TestClassifyUsageNegativeCapNeverDenies(t *testing.T) {

	thr := DoctrineDefaults(doctrine.NameDefault)
	got := ClassifyUsage(500, -10, thr)
	if got != CapStatusOK {
		t.Errorf("ClassifyUsage(500/-10) = %v, want CapStatusOK (negative cap → no cap)", got)
	}
}

func TestClassifyUsageNegativeUsedTreatedAsZero(t *testing.T) {
	thr := DoctrineDefaults(doctrine.NameDefault)
	got := ClassifyUsage(-50, 100, thr)
	if got != CapStatusOK {
		t.Errorf("ClassifyUsage(-50/100) = %v, want CapStatusOK", got)
	}
}

func TestClassifyUsageJustBelowSoftIsOK(t *testing.T) {

	thr := DoctrineDefaults(doctrine.NameDefault)
	got := ClassifyUsage(79, 100, thr)
	if got != CapStatusOK {
		t.Errorf("ClassifyUsage(79/100) = %v, want CapStatusOK (below 80)", got)
	}
}

func TestClassifyUsageJustBelowHardIsSoftWarn(t *testing.T) {

	thr := DoctrineDefaults(doctrine.NameDefault)
	got := ClassifyUsage(99, 100, thr)
	if got != CapStatusSoftWarn {
		t.Errorf("ClassifyUsage(99/100) = %v, want CapStatusSoftWarn (below 100)", got)
	}
}

func TestCapStatusStringStable(t *testing.T) {
	cases := map[CapStatus]string{
		CapStatusOK:          "ok",
		CapStatusSoftWarn:    "soft-warn",
		CapStatusHardDeny:    "hard-deny",
		CapStatusHardLogOnly: "hard-log-only",
		CapStatus(99):        "unknown",
	}
	for s, want := range cases {
		if got := s.String(); got != want {
			t.Errorf("%v.String() = %q, want %q", int(s), got, want)
		}
	}
}
