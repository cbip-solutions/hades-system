package main

import (
	"testing"

	"github.com/cbip-solutions/hades-system/internal/caronte/coordinated"
)

func makeConsumers(n int) []coordinated.ConsumerRef {
	out := make([]coordinated.ConsumerRef, n)
	for i := 0; i < n; i++ {
		out[i] = coordinated.ConsumerRef{Repo: "r", CallID: "c"}
	}
	return out
}

func TestDefaultPolicyOracle_LockedReturnsSurface(t *testing.T) {
	o := newDefaultPolicyOracle(fakeDoctrineResolver{p: lockedTestPolicy{}})
	got := o.Decision(coordinated.ContractBreakage{AffectedConsumers: makeConsumers(1)})
	if got != coordinated.ModeSurface {
		t.Fatalf("locked workspace must surface; got %v", got)
	}
}

func TestDefaultPolicyOracle_LargeFanoutReturnsSurface(t *testing.T) {
	o := newDefaultPolicyOracle(fakeDoctrineResolver{p: permissiveTestPolicy{}})
	got := o.Decision(coordinated.ContractBreakage{AffectedConsumers: makeConsumers(6)})
	if got != coordinated.ModeSurface {
		t.Fatalf("large fan-out (>5) must surface; got %v", got)
	}
}

func TestDefaultPolicyOracle_SmallFanoutUnlockedReturnsAutonomy(t *testing.T) {
	o := newDefaultPolicyOracle(fakeDoctrineResolver{p: permissiveTestPolicy{}})
	got := o.Decision(coordinated.ContractBreakage{AffectedConsumers: makeConsumers(3)})
	if got != coordinated.ModeAutonomy {
		t.Fatalf("small fan-out + unlocked must autonomy; got %v", got)
	}
}

func TestDefaultPolicyOracle_BoundaryAtFive(t *testing.T) {
	o := newDefaultPolicyOracle(fakeDoctrineResolver{p: permissiveTestPolicy{}})
	got := o.Decision(coordinated.ContractBreakage{AffectedConsumers: makeConsumers(5)})
	if got != coordinated.ModeAutonomy {
		t.Fatalf("exactly 5 consumers must autonomy (>5 is the cutoff); got %v", got)
	}
}

func TestDefaultPolicyOracle_NilDoctrineDefensiveSurface(t *testing.T) {
	o := newDefaultPolicyOracle(nil)
	got := o.Decision(coordinated.ContractBreakage{AffectedConsumers: makeConsumers(2)})
	if got != coordinated.ModeSurface {
		t.Fatalf("nil doctrine resolver must surface (defense-in-depth); got %v", got)
	}
}

func TestDefaultPolicyOracle_NilPolicyDefensiveSurface(t *testing.T) {
	o := newDefaultPolicyOracle(fakeDoctrineResolver{p: nil})
	got := o.Decision(coordinated.ContractBreakage{AffectedConsumers: makeConsumers(1)})
	if got != coordinated.ModeAutonomy {
		t.Fatalf("nil policy is treated as unlocked (no PrivacyLocked call); small fan-out → autonomy; got %v", got)
	}
}

func TestDefaultPolicyOracle_EmptyConsumersIsAutonomy(t *testing.T) {
	o := newDefaultPolicyOracle(fakeDoctrineResolver{p: permissiveTestPolicy{}})
	got := o.Decision(coordinated.ContractBreakage{AffectedConsumers: nil})
	if got != coordinated.ModeAutonomy {
		t.Fatalf("empty consumers + permissive doctrine → autonomy; got %v", got)
	}
}
