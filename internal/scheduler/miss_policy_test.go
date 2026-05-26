package scheduler_test

import (
	"testing"

	"github.com/cbip-solutions/hades-system/internal/doctrine"
	"github.com/cbip-solutions/hades-system/internal/scheduler"
)

func TestDoctrineMissPolicy_Default(t *testing.T) {
	got := scheduler.DoctrineMissPolicy(doctrine.NameDefault)
	if got != scheduler.MissPolicySkip {
		t.Errorf("DoctrineMissPolicy(default) = %v, want MissPolicySkip", got)
	}
}

func TestDoctrineMissPolicy_MaxScope(t *testing.T) {
	got := scheduler.DoctrineMissPolicy(doctrine.NameMaxScope)
	if got != scheduler.MissPolicyCatchUpBounded {
		t.Errorf("DoctrineMissPolicy(max-scope) = %v, want MissPolicyCatchUpBounded", got)
	}
}

func TestDoctrineMissPolicy_CapaFirewall(t *testing.T) {
	got := scheduler.DoctrineMissPolicy(doctrine.NameCapaFirewall)
	if got != scheduler.MissPolicyNotifyOnly {
		t.Errorf("DoctrineMissPolicy(capa-firewall) = %v, want MissPolicyNotifyOnly", got)
	}
}

func TestDoctrineMissPolicy_UnknownDoctrineFallsBackToSkip(t *testing.T) {
	got := scheduler.DoctrineMissPolicy(doctrine.Name("unknown-doctrine"))
	if got != scheduler.MissPolicySkip {
		t.Errorf("DoctrineMissPolicy(unknown) = %v, want MissPolicySkip (safe default)", got)
	}
}

func TestDoctrineMissPolicy_EmptyDoctrineFallsBackToSkip(t *testing.T) {
	got := scheduler.DoctrineMissPolicy(doctrine.Name(""))
	if got != scheduler.MissPolicySkip {
		t.Errorf("DoctrineMissPolicy(\"\") = %v, want MissPolicySkip (safe default)", got)
	}
}

func TestEffectiveMissPolicy_OverrideWins(t *testing.T) {

	s := &scheduler.Schedule{MissPolicy: scheduler.MissPolicyCoalesce}
	got := scheduler.EffectiveMissPolicy(s, doctrine.NameMaxScope)
	if got != scheduler.MissPolicyCoalesce {
		t.Errorf("EffectiveMissPolicy(coalesce override) = %v, want MissPolicyCoalesce", got)
	}
}

func TestEffectiveMissPolicy_DoctrineWhenZero(t *testing.T) {

	s := &scheduler.Schedule{}
	got := scheduler.EffectiveMissPolicy(s, doctrine.NameMaxScope)
	if got != scheduler.MissPolicyCatchUpBounded {
		t.Errorf("EffectiveMissPolicy(zero, max-scope) = %v, want MissPolicyCatchUpBounded", got)
	}
}

func TestEffectiveMissPolicy_DefaultDoctrineHonoursZero(t *testing.T) {
	s := &scheduler.Schedule{}
	got := scheduler.EffectiveMissPolicy(s, doctrine.NameDefault)
	if got != scheduler.MissPolicySkip {
		t.Errorf("EffectiveMissPolicy(zero, default) = %v, want MissPolicySkip", got)
	}
}

func TestEffectiveMissPolicy_NilScheduleUsesDoctrine(t *testing.T) {
	got := scheduler.EffectiveMissPolicy(nil, doctrine.NameMaxScope)
	if got != scheduler.MissPolicyCatchUpBounded {
		t.Errorf("EffectiveMissPolicy(nil, max-scope) = %v, want MissPolicyCatchUpBounded", got)
	}
	got = scheduler.EffectiveMissPolicy(nil, doctrine.NameDefault)
	if got != scheduler.MissPolicySkip {
		t.Errorf("EffectiveMissPolicy(nil, default) = %v, want MissPolicySkip", got)
	}
	got = scheduler.EffectiveMissPolicy(nil, doctrine.NameCapaFirewall)
	if got != scheduler.MissPolicyNotifyOnly {
		t.Errorf("EffectiveMissPolicy(nil, capa-firewall) = %v, want MissPolicyNotifyOnly", got)
	}
}

func TestEffectiveMissPolicy_NotifyOnlyOverrideOnDefault(t *testing.T) {
	s := &scheduler.Schedule{MissPolicy: scheduler.MissPolicyNotifyOnly}
	got := scheduler.EffectiveMissPolicy(s, doctrine.NameDefault)
	if got != scheduler.MissPolicyNotifyOnly {
		t.Errorf("EffectiveMissPolicy(notify-only override on default) = %v, want MissPolicyNotifyOnly", got)
	}
}

func TestEffectiveMissPolicy_CatchUpBoundedOverrideOnCapaFirewall(t *testing.T) {
	s := &scheduler.Schedule{MissPolicy: scheduler.MissPolicyCatchUpBounded}
	got := scheduler.EffectiveMissPolicy(s, doctrine.NameCapaFirewall)
	if got != scheduler.MissPolicyCatchUpBounded {
		t.Errorf("EffectiveMissPolicy(catchup override on capa-firewall) = %v, want MissPolicyCatchUpBounded", got)
	}
}

func TestEffectiveMissPolicy_UnknownDoctrineUsesSkipForZero(t *testing.T) {
	s := &scheduler.Schedule{}
	got := scheduler.EffectiveMissPolicy(s, doctrine.Name("frob"))

	if got != scheduler.MissPolicySkip {
		t.Errorf("EffectiveMissPolicy(zero, unknown) = %v, want MissPolicySkip", got)
	}
}
