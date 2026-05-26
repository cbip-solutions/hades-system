package main

import (
	"context"
	"errors"
	"testing"

	caronte "github.com/cbip-solutions/hades-system/internal/caronte"
	"github.com/cbip-solutions/hades-system/internal/caronte/evolution"
	"github.com/cbip-solutions/hades-system/internal/caronte/intent"
	"github.com/cbip-solutions/hades-system/internal/caronte/semantic"
	"github.com/cbip-solutions/hades-system/internal/daemon/mcpgateway"
	"github.com/cbip-solutions/hades-system/internal/mcp/research"
)

type fakeCaronteEngine struct{}

func (fakeCaronteEngine) CodeGraph(_ context.Context, _, _ string) (research.CodeGraphResult, error) {
	return research.CodeGraphResult{}, nil
}
func (fakeCaronteEngine) Context(_ context.Context, _, _ string) (caronte.ContextResult, error) {
	return caronte.ContextResult{}, nil
}
func (fakeCaronteEngine) BlastRadius(_ context.Context, _ string, _, _ []string) (evolution.RiskScore, error) {
	return evolution.RiskScore{}, nil
}
func (fakeCaronteEngine) GetWhy(_ context.Context, _, _ string) (intent.WhyAnswer, error) {
	return intent.WhyAnswer{}, nil
}
func (fakeCaronteEngine) GetImplementations(_ context.Context, _, _ string) ([]semantic.Implementation, error) {
	return nil, nil
}
func (fakeCaronteEngine) TraceCallPath(_ context.Context, _ string, _ int, _ string) ([]semantic.CallPathHop, error) {
	return nil, nil
}
func (fakeCaronteEngine) GetCoChange(_ context.Context, _, _ string) ([]caronte.CoChangePeer, error) {
	return nil, nil
}
func (fakeCaronteEngine) GetHealth(_ context.Context, _ string) (caronte.HealthReport, error) {
	return caronte.HealthReport{}, nil
}
func (fakeCaronteEngine) GetArchitecture(_ context.Context, _ string) (caronte.ArchitectureReport, error) {
	return caronte.ArchitectureReport{}, nil
}
func (fakeCaronteEngine) Wiki(_ context.Context, _, _ string) (caronte.WikiDoc, error) {
	return caronte.WikiDoc{}, nil
}
func (fakeCaronteEngine) Close() error { return nil }

func (fakeCaronteEngine) GetContract(_ context.Context, _, _ string) (caronte.ContractPayload, error) {
	return caronte.ContractPayload{}, nil
}
func (fakeCaronteEngine) GetConsumers(_ context.Context, _, _ string) (caronte.ConsumerList, error) {
	return caronte.ConsumerList{}, nil
}
func (fakeCaronteEngine) GetBreakingChanges(_ context.Context, _ string, _ int64) ([]caronte.BreakingChangePayload, error) {
	return nil, nil
}
func (fakeCaronteEngine) TraceAPICall(_ context.Context, _, _ string) (caronte.APICallTrace, error) {
	return caronte.APICallTrace{}, nil
}
func (fakeCaronteEngine) GetWorkspace(_ context.Context, _ string) (caronte.WorkspaceSnapshot, error) {
	return caronte.WorkspaceSnapshot{}, nil
}
func (fakeCaronteEngine) FederationHealth(_ context.Context, _ string) (caronte.FederationHealthReport, error) {
	return caronte.FederationHealthReport{}, nil
}
func (fakeCaronteEngine) ContractDiff(_ context.Context, _ string, _ int64) (caronte.ContractDiff, error) {
	return caronte.ContractDiff{}, nil
}
func (fakeCaronteEngine) GetWhyBreakingChange(_ context.Context, _ string) (caronte.WhyBreakingChange, error) {
	return caronte.WhyBreakingChange{}, nil
}

var _ mcpgateway.CaronteEngine = fakeCaronteEngine{}

func TestBuildDispatcherWithCaronteRegistersAllCaronteTools(t *testing.T) {
	deps := mcpgatewayDeps{
		caronte: fakeCaronteEngine{},
		audit:   mcpgateway.NopAuditEmitter(),
		rbacCfg: defaultRBACConfig(),
	}
	d, err := buildDispatcher(deps)
	if err != nil {
		t.Fatalf("buildDispatcher with caronte: %v", err)
	}
	defer d.Close()

	caronteCount := 0
	for _, e := range d.ListTools() {
		if e.Name.Subsystem() == "caronte" {
			caronteCount++
		}
	}
	const plan19Count, plan20Count = 11, 8
	if got, want := caronteCount, plan19Count+plan20Count; got < want {
		t.Errorf("caronte tool count after cutover = %d; want >= %d (Plan 19 K's %d + Plan 20 I's %d)",
			got, want, plan19Count, plan20Count)
	}
}

// TestBuildDispatcherNilCaronteReturnsBootstrapRequired verifies the
// bootstrap-required posture (Q5=A generalised for caronte): a nil
// deps.caronte MUST cause buildDispatcher to return an error wrapping
// ErrCaronteBootstrapRequired. The daemon refuses to start.
func TestBuildDispatcherNilCaronteReturnsBootstrapRequired(t *testing.T) {
	deps := mcpgatewayDeps{
		caronte: nil,
		audit:   mcpgateway.NopAuditEmitter(),
		rbacCfg: defaultRBACConfig(),
	}
	_, err := buildDispatcher(deps)
	if err == nil {
		t.Fatal("buildDispatcher nil caronte: nil err; expected ErrCaronteBootstrapRequired")
	}
	if !errors.Is(err, mcpgateway.ErrCaronteBootstrapRequired) {
		t.Errorf("error = %v; want chain containing ErrCaronteBootstrapRequired", err)
	}
}

func TestBuildDispatcherCaronteToolsHaveCaronteSubsystem(t *testing.T) {
	deps := mcpgatewayDeps{
		caronte: fakeCaronteEngine{},
		audit:   mcpgateway.NopAuditEmitter(),
		rbacCfg: defaultRBACConfig(),
	}
	d, err := buildDispatcher(deps)
	if err != nil {
		t.Fatalf("buildDispatcher: %v", err)
	}
	defer d.Close()

	sawCaronte := false
	for _, e := range d.ListTools() {
		if e.Name.Subsystem() == "caronte" {
			sawCaronte = true
			if e.Meta.Description == "" {
				t.Errorf("tool %q has empty description", e.Name)
			}
		}
		if e.Name.Subsystem() == "gitnexus" {
			t.Errorf("tool %q registered under removed 'gitnexus' segment", e.Name)
		}
	}
	if !sawCaronte {
		t.Error("no tools registered under the caronte segment")
	}
}

func TestBuildDispatcherCaronteDispatcherCloseSafe(t *testing.T) {
	deps := mcpgatewayDeps{
		caronte: fakeCaronteEngine{},
		audit:   mcpgateway.NopAuditEmitter(),
		rbacCfg: defaultRBACConfig(),
	}
	d, err := buildDispatcher(deps)
	if err != nil {
		t.Fatalf("buildDispatcher: %v", err)
	}

	if err := d.Close(); err != nil {

		_ = err
	}
}
