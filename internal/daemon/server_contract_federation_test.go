package daemon

import (
	"context"
	"reflect"
	"testing"
)

type fakeContractFederation struct{}

func (f *fakeContractFederation) ListWorkspaces(_ context.Context) ([]Workspace, error) {
	return nil, nil
}
func (f *fakeContractFederation) GetWorkspace(_ context.Context, _ string) (Workspace, error) {
	return Workspace{}, nil
}
func (f *fakeContractFederation) ListRecentBreakingChanges(_ context.Context, _ string, _ int) ([]BreakingChange, error) {
	return nil, nil
}
func (f *fakeContractFederation) ListWorkspaceMembers(_ context.Context, _ string) ([]Member, error) {
	return nil, nil
}
func (f *fakeContractFederation) GetBreakingChangeWithConsumers(_ context.Context, _ string) (BreakingChange, []BreakingChangeConsumer, error) {
	return BreakingChange{}, nil, nil
}
func (f *fakeContractFederation) Close() error { return nil }

type fakeContractCoordinator struct{}

func (f *fakeContractCoordinator) RecentDispatches(_ context.Context, _ int) ([]DispatchDecision, error) {
	return nil, nil
}

func TestContractFederationNilByDefault(t *testing.T) {
	s := newTestServer(t)
	if s.ContractFederation() != nil {
		t.Error("ContractFederation() non-nil before SetContractFederation")
	}
}

func TestSetContractFederationRoundTrip(t *testing.T) {
	s := newTestServer(t)
	f := &fakeContractFederation{}
	s.SetContractFederation(f)
	got := s.ContractFederation()
	if got == nil {
		t.Fatal("ContractFederation() nil after SetContractFederation")
	}
	if got != f {
		t.Errorf("ContractFederation() returned %v; want %v", got, f)
	}
}

func TestSetContractFederationNilSafe(t *testing.T) {
	s := newTestServer(t)
	f := &fakeContractFederation{}
	s.SetContractFederation(f)
	if s.ContractFederation() == nil {
		t.Fatal("ContractFederation() nil after first Set")
	}
	s.SetContractFederation(nil)
	if s.ContractFederation() != nil {
		t.Error("ContractFederation() non-nil after SetContractFederation(nil)")
	}
}

func TestContractCoordinatorNilByDefault(t *testing.T) {
	s := newTestServer(t)
	if s.ContractCoordinator() != nil {
		t.Error("ContractCoordinator() non-nil before SetContractCoordinator")
	}
}

func TestSetContractCoordinatorRoundTrip(t *testing.T) {
	s := newTestServer(t)
	c := &fakeContractCoordinator{}
	s.SetContractCoordinator(c)
	got := s.ContractCoordinator()
	if got == nil {
		t.Fatal("ContractCoordinator() nil after SetContractCoordinator")
	}
	if got != c {
		t.Errorf("ContractCoordinator() returned %v; want %v", got, c)
	}
}

func TestSetContractCoordinatorNilSafe(t *testing.T) {
	s := newTestServer(t)
	c := &fakeContractCoordinator{}
	s.SetContractCoordinator(c)
	if s.ContractCoordinator() == nil {
		t.Fatal("ContractCoordinator() nil after first Set")
	}
	s.SetContractCoordinator(nil)
	if s.ContractCoordinator() != nil {
		t.Error("ContractCoordinator() non-nil after SetContractCoordinator(nil)")
	}
}

// TestContractFederationForDaemon_InterfaceContractStable pins the
// interface method set: drift here breaks the structural contract that
// the wiring file's compile-time anchor (var _ daemon.ContractFederationForDaemon
// = fedDB) depends on. The list mirrors Stage-0 J-0.7 (Phase A as-shipped
// Wave-1 surface).
//
// Sister-test per [[feedback_sister_test_pattern]] — any method
// addition/removal on ContractFederationForDaemon MUST update this slice
// in lock-step so the assertion stays honest.
func TestContractFederationForDaemon_InterfaceContractStable(t *testing.T) {
	iface := reflect.TypeOf((*ContractFederationForDaemon)(nil)).Elem()
	wantMethods := []string{
		"Close",
		"GetBreakingChangeWithConsumers",
		"GetWorkspace",
		"ListRecentBreakingChanges",
		"ListWorkspaceMembers",
		"ListWorkspaces",
	}
	if got := iface.NumMethod(); got != len(wantMethods) {
		t.Fatalf("ContractFederationForDaemon has %d methods, want %d (%v)",
			got, len(wantMethods), wantMethods)
	}
	for i, want := range wantMethods {
		if got := iface.Method(i).Name; got != want {
			t.Errorf("method[%d] = %q, want %q", i, got, want)
		}
	}
}

func TestContractCoordinatorForDaemon_InterfaceContractStable(t *testing.T) {
	iface := reflect.TypeOf((*ContractCoordinatorForDaemon)(nil)).Elem()
	wantMethods := []string{"RecentDispatches"}
	if got := iface.NumMethod(); got != len(wantMethods) {
		t.Fatalf("ContractCoordinatorForDaemon has %d methods, want %d (%v)",
			got, len(wantMethods), wantMethods)
	}
	for i, want := range wantMethods {
		if got := iface.Method(i).Name; got != want {
			t.Errorf("method[%d] = %q, want %q", i, got, want)
		}
	}
}
