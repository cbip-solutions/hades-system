package main

import (
	"context"
	"errors"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/caronte/evolution"
	"github.com/cbip-solutions/hades-system/internal/daemon/mcpgateway"
	orch "github.com/cbip-solutions/hades-system/internal/orchestrator"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/merge"
)

type fakeBlastCore struct{ rs evolution.RiskScore }

func (f fakeBlastCore) blastRadius(_ context.Context, _ string, _, _ []string) (evolution.RiskScore, error) {
	return f.rs, nil
}

func TestOrchVerdictAdapterMapsRiskScore(t *testing.T) {

	var _ orch.BlastRadiusProvider = caronteOrchVerdictAdapter{}

	core := fakeBlastCore{rs: evolution.RiskScore{Score: 0.72, Level: "high", TopAffected: []string{"pkg.A"}}}
	var p orch.BlastRadiusProvider = caronteOrchVerdictAdapter{core: core}
	v, err := p.BlastRadius(context.Background(), "proj", []string{"pkg.A"}, nil)
	if err != nil {
		t.Fatalf("BlastRadius: %v", err)
	}
	if v.Level != "high" || v.Score != 0.72 || len(v.TopAffected) != 1 {
		t.Errorf("orch Verdict = %+v; want high/0.72/[pkg.A]", v)
	}
	if v.TopAffected[0] != "pkg.A" {
		t.Errorf("TopAffected[0] = %q; want pkg.A", v.TopAffected[0])
	}
}

func TestMergeVerdictAdapterMapsRiskScore(t *testing.T) {

	var _ merge.BlastRadiusScorer = caronteMergeVerdictAdapter{}

	core := fakeBlastCore{rs: evolution.RiskScore{Score: 0.4, Level: "medium"}}
	var sc merge.BlastRadiusScorer = caronteMergeVerdictAdapter{core: core}
	v, err := sc.BlastRadius(context.Background(), "proj", nil, []string{"a.go"})
	if err != nil {
		t.Fatalf("BlastRadius: %v", err)
	}
	if v.Level != "medium" || v.Score != 0.4 {
		t.Errorf("merge Verdict = %+v; want medium/0.4", v)
	}
}

func TestOrchVerdictAdapterIsHigh(t *testing.T) {
	core := fakeBlastCore{rs: evolution.RiskScore{Score: 0.85, Level: "high"}}
	p := caronteOrchVerdictAdapter{core: core}
	v, err := p.BlastRadius(context.Background(), "proj", nil, nil)
	if err != nil {
		t.Fatalf("BlastRadius: %v", err)
	}
	if !v.IsHigh() {
		t.Errorf("Verdict.IsHigh() = false; want true for level=%q", v.Level)
	}
}

func TestStaticParamsAccessorReturnsDefaults(t *testing.T) {
	var a evolution.ParamsAccessor = staticParamsAccessor{params: evolution.DefaultParams()}
	got := a.CoChangeParams("any-project")
	want := evolution.DefaultParams()
	if got.WindowDays != want.WindowDays {
		t.Errorf("CoChangeParams.WindowDays = %d; want %d", got.WindowDays, want.WindowDays)
	}
}

func TestStaticParamsAccessorProjectAgnostic(t *testing.T) {
	acc := staticParamsAccessor{params: evolution.DefaultParams()}
	p1 := acc.CoChangeParams("project-one")
	p2 := acc.CoChangeParams("project-two")
	if p1.WindowDays != p2.WindowDays {
		t.Errorf("staticParamsAccessor is not project-agnostic: p1=%+v p2=%+v", p1, p2)
	}
}

func TestNewCaronteSubsystemRegistersCaronteSlot(t *testing.T) {
	prx := mcpgateway.NewCaronteProxy(nil, mcpgateway.NopAuditEmitter())
	sub := newCaronteSubsystem(prx)
	if sub.Name() != "caronte" {
		t.Errorf("subsystem Name() = %q; want caronte", sub.Name())
	}

	const plan19Count, plan20Count = 11, 8
	if got, want := len(sub.Tools()), plan19Count+plan20Count; got < want {
		t.Errorf("subsystem advertises %d tools; want >= %d (Plan 19 K's %d + Plan 20 I's %d)",
			got, want, plan19Count, plan20Count)
	}
	d := mcpgateway.NewDispatcher(mcpgateway.DispatcherConfig{Audit: mcpgateway.NopAuditEmitter()})
	if err := registerSubsystemNamed(d, "caronte", sub); err != nil {
		t.Fatalf("registerSubsystemNamed: %v", err)
	}
	if got, want := len(d.ListTools()), plan19Count+plan20Count; got < want {
		t.Errorf("dispatcher registered %d tools; want >= %d (Plan 19 K's %d + Plan 20 I's %d)",
			got, want, plan19Count, plan20Count)
	}
}

func TestCaronteSubsystemToolNamesUnderCaronte(t *testing.T) {
	prx := mcpgateway.NewCaronteProxy(nil, mcpgateway.NopAuditEmitter())
	sub := newCaronteSubsystem(prx)
	for _, te := range sub.Tools() {
		if te.Name.Subsystem() != "caronte" {
			t.Errorf("tool %q has subsystem %q; want caronte", te.Name, te.Name.Subsystem())
		}
	}
}

func TestEngineBlastCoreAdaptsDelegation(t *testing.T) {

	var _ caronteBlastRadiusCore = engineBlastCore{}

	// Construct adapters over a nil-engine core (J-9 wiring scope).
	// We do NOT call BlastRadius here — that would panic on nil engine.
	// The compile-time check above is the load-bearing assertion.
	_ = caronteOrchVerdictAdapter{core: engineBlastCore{}}
	_ = caronteMergeVerdictAdapter{core: engineBlastCore{}}
}

type errBlastCore struct{ err error }

func (e errBlastCore) blastRadius(_ context.Context, _ string, _, _ []string) (evolution.RiskScore, error) {
	return evolution.RiskScore{}, e.err
}

func TestOrchVerdictAdapterPropagatesError(t *testing.T) {
	sentinel := errors.New("blast-radius unavailable")
	p := caronteOrchVerdictAdapter{core: errBlastCore{err: sentinel}}
	v, err := p.BlastRadius(context.Background(), "proj", nil, nil)
	if !errors.Is(err, sentinel) {
		t.Errorf("BlastRadius error = %v; want sentinel", err)
	}
	if v.Level != "" || v.Score != 0 {
		t.Errorf("BlastRadius verdict on error should be zero, got %+v", v)
	}
}

func TestMergeVerdictAdapterPropagatesError(t *testing.T) {
	sentinel := errors.New("engine closed")
	sc := caronteMergeVerdictAdapter{core: errBlastCore{err: sentinel}}
	v, err := sc.BlastRadius(context.Background(), "proj", nil, nil)
	if !errors.Is(err, sentinel) {
		t.Errorf("BlastRadius error = %v; want sentinel", err)
	}
	if v.Level != "" || v.Score != 0 {
		t.Errorf("BlastRadius verdict on error should be zero, got %+v", v)
	}
}
