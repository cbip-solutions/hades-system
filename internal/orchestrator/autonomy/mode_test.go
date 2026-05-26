package autonomy_test

import (
	"testing"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/autonomy"
)

func ptr[T any](v T) *T { return &v }

func TestResolve_DoctrineDefault_MaxScope(t *testing.T) {
	got := autonomy.Resolve(autonomy.ResolveInput{
		Doctrine: "max-scope",
	})
	if got.Mode != autonomy.ModeSemi {
		t.Fatalf("max-scope default: want semi, got %v", got.Mode)
	}
	if got.Source != autonomy.SourceDoctrineDefault {
		t.Fatalf("source: want doctrine-default, got %v", got.Source)
	}
}

func TestResolve_DoctrineDefault_Default(t *testing.T) {
	got := autonomy.Resolve(autonomy.ResolveInput{Doctrine: "default"})
	if got.Mode != autonomy.ModeManual || got.Source != autonomy.SourceDoctrineDefault {
		t.Fatalf("default doctrine: want manual/doctrine-default, got %v/%v", got.Mode, got.Source)
	}
}

func TestResolve_ProjectConfig_OverridesDoctrine(t *testing.T) {
	got := autonomy.Resolve(autonomy.ResolveInput{
		Doctrine:      "default",
		ProjectConfig: ptr(autonomy.ModeSemi),
	})
	if got.Mode != autonomy.ModeSemi || got.Source != autonomy.SourceProjectConfig {
		t.Fatalf("project config override: got %v/%v", got.Mode, got.Source)
	}
}

func TestResolve_BuildFlag_OverridesProjectConfig(t *testing.T) {
	got := autonomy.Resolve(autonomy.ResolveInput{
		Doctrine:      "default",
		ProjectConfig: ptr(autonomy.ModeSemi),
		BuildFlag:     ptr(autonomy.ModeFull),
	})
	if got.Mode != autonomy.ModeFull || got.Source != autonomy.SourceBuildFlag {
		t.Fatalf("build flag override: got %v/%v", got.Mode, got.Source)
	}
}

func TestResolve_CapaFirewall_HardGuard_IgnoresProjectConfig(t *testing.T) {
	got := autonomy.Resolve(autonomy.ResolveInput{
		Doctrine:      "capa-firewall",
		ProjectConfig: ptr(autonomy.ModeFull),
	})
	if got.Mode != autonomy.ModeManual {
		t.Fatalf("capa-firewall must force manual; got %v", got.Mode)
	}
	if got.Source != autonomy.SourceCapaFirewallGuard {
		t.Fatalf("source must be capa-firewall-guard; got %v", got.Source)
	}
	if got.RejectedOverride == nil {
		t.Fatalf("expected RejectedOverride to record the attempted override")
	}
	if got.RejectedOverride.AttemptedMode != autonomy.ModeFull {
		t.Fatalf("AttemptedMode: want full, got %v", got.RejectedOverride.AttemptedMode)
	}
	if got.RejectedOverride.AttemptedFrom != autonomy.SourceProjectConfig {
		t.Fatalf("AttemptedFrom: want project-config, got %v", got.RejectedOverride.AttemptedFrom)
	}
}

func TestResolve_CapaFirewall_HardGuard_IgnoresBuildFlag(t *testing.T) {
	got := autonomy.Resolve(autonomy.ResolveInput{
		Doctrine:  "capa-firewall",
		BuildFlag: ptr(autonomy.ModeFull),
	})
	if got.Mode != autonomy.ModeManual || got.Source != autonomy.SourceCapaFirewallGuard {
		t.Fatalf("capa-firewall + build flag: got %v/%v", got.Mode, got.Source)
	}
	if got.RejectedOverride == nil || got.RejectedOverride.AttemptedFrom != autonomy.SourceBuildFlag {
		t.Fatalf("RejectedOverride should record build-flag attempt; got %+v", got.RejectedOverride)
	}
}

func TestResolve_CapaFirewall_HardGuard_BothLayers_RecordsHigherPrecedence(t *testing.T) {
	got := autonomy.Resolve(autonomy.ResolveInput{
		Doctrine:      "capa-firewall",
		ProjectConfig: ptr(autonomy.ModeSemi),
		BuildFlag:     ptr(autonomy.ModeFull),
	})
	if got.RejectedOverride.AttemptedFrom != autonomy.SourceBuildFlag {
		t.Fatalf("RejectedOverride must reflect highest-precedence attempt; got %v", got.RejectedOverride.AttemptedFrom)
	}
	if got.RejectedOverride.AttemptedMode != autonomy.ModeFull {
		t.Fatalf("RejectedOverride mode: want full, got %v", got.RejectedOverride.AttemptedMode)
	}
}

func TestResolve_CapaFirewall_NoOverride_NoRejection(t *testing.T) {
	got := autonomy.Resolve(autonomy.ResolveInput{Doctrine: "capa-firewall"})
	if got.Mode != autonomy.ModeManual || got.Source != autonomy.SourceCapaFirewallGuard {
		t.Fatalf("capa-firewall vanilla: got %v/%v", got.Mode, got.Source)
	}
	if got.RejectedOverride != nil {
		t.Fatalf("no override attempted, RejectedOverride must be nil; got %+v", got.RejectedOverride)
	}
}

func TestResolve_CapaFirewall_ManualOverride_NoRejection(t *testing.T) {

	got := autonomy.Resolve(autonomy.ResolveInput{
		Doctrine:      "capa-firewall",
		BuildFlag:     ptr(autonomy.ModeManual),
		ProjectConfig: ptr(autonomy.ModeManual),
	})
	if got.Mode != autonomy.ModeManual || got.Source != autonomy.SourceCapaFirewallGuard {
		t.Fatalf("capa-firewall + manual overrides: got %v/%v", got.Mode, got.Source)
	}
	if got.RejectedOverride != nil {
		t.Fatalf("manual override on capa-firewall is not a rejected override; got %+v", got.RejectedOverride)
	}
}

func TestResolve_UnknownDoctrine_FailsClosed(t *testing.T) {
	got := autonomy.Resolve(autonomy.ResolveInput{Doctrine: "no-such-doctrine"})
	if got.Mode != autonomy.ModeManual {
		t.Fatalf("unknown doctrine fails closed to manual; got %v", got.Mode)
	}
	if got.Source != autonomy.SourceDoctrineDefault {
		t.Fatalf("source: got %v", got.Source)
	}
}

func TestResolve_DoctrineWhitespaceTolerated(t *testing.T) {
	got := autonomy.Resolve(autonomy.ResolveInput{Doctrine: "  Capa-Firewall  "})
	if got.Source != autonomy.SourceCapaFirewallGuard {
		t.Fatalf("doctrine name should be trimmed + case-insensitive; got source %v", got.Source)
	}
}

func TestModeString(t *testing.T) {
	cases := []struct {
		m    autonomy.Mode
		want string
	}{
		{autonomy.ModeManual, "manual"},
		{autonomy.ModeSemi, "semi"},
		{autonomy.ModeFull, "full"},
	}
	for _, c := range cases {
		if c.m.String() != c.want {
			t.Fatalf("Mode.String %v: want %q got %q", c.m, c.want, c.m.String())
		}
	}

	var bogus autonomy.Mode = 99
	if got := bogus.String(); got != "mode(99)" {
		t.Fatalf("Mode.String unknown: want mode(99), got %q", got)
	}
}

func TestSourceString(t *testing.T) {
	cases := []struct {
		s    autonomy.Source
		want string
	}{
		{autonomy.SourceDoctrineDefault, "doctrine-default"},
		{autonomy.SourceProjectConfig, "project-config"},
		{autonomy.SourceBuildFlag, "build-flag"},
		{autonomy.SourceCapaFirewallGuard, "capa-firewall-guard"},
	}
	for _, c := range cases {
		if c.s.String() != c.want {
			t.Fatalf("Source.String %v: want %q got %q", c.s, c.want, c.s.String())
		}
	}
	var bogus autonomy.Source = 99
	if got := bogus.String(); got != "source(99)" {
		t.Fatalf("Source.String unknown: want source(99), got %q", got)
	}
}

func TestParseMode(t *testing.T) {
	for _, c := range []struct {
		in   string
		want autonomy.Mode
		ok   bool
	}{
		{"manual", autonomy.ModeManual, true},
		{"semi", autonomy.ModeSemi, true},
		{"full", autonomy.ModeFull, true},
		{"FULL", autonomy.ModeFull, true},
		{"  Semi  ", autonomy.ModeSemi, true},
		{"", 0, false},
		{"god-mode", 0, false},
	} {
		got, err := autonomy.ParseMode(c.in)
		if c.ok && err != nil {
			t.Fatalf("ParseMode(%q): unexpected err %v", c.in, err)
		}
		if !c.ok && err == nil {
			t.Fatalf("ParseMode(%q): expected error", c.in)
		}
		if c.ok && got != c.want {
			t.Fatalf("ParseMode(%q): got %v want %v", c.in, got, c.want)
		}
	}
}
