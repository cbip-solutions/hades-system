package dispatcheradapter_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/daemon/dispatcheradapter"
	"github.com/cbip-solutions/hades-system/internal/doctrine"
	"github.com/cbip-solutions/hades-system/internal/quota"
)

type fakeCostLedger struct {
	used       int64
	cap        int64
	globalUsed int64
	globalCap  int64
	tierUsed   map[string]int64
	tierCaps   map[string]int64
	failOn     string
	failErr    error
}

func (f *fakeCostLedger) ProjectUsage(_ context.Context, _ string) (int64, error) {
	if f.failOn == "ProjectUsage" {
		return 0, f.failErr
	}
	return f.used, nil
}

func (f *fakeCostLedger) ProjectCap(_ context.Context, _ string) (int64, error) {
	if f.failOn == "ProjectCap" {
		return 0, f.failErr
	}
	return f.cap, nil
}

func (f *fakeCostLedger) GlobalUsage(_ context.Context) (int64, error) {
	if f.failOn == "GlobalUsage" {
		return 0, f.failErr
	}
	return f.globalUsed, nil
}

func (f *fakeCostLedger) GlobalCap(_ context.Context) (int64, error) {
	if f.failOn == "GlobalCap" {
		return 0, f.failErr
	}
	return f.globalCap, nil
}

func (f *fakeCostLedger) PerTierUsage(_ context.Context) (map[string]int64, error) {
	if f.failOn == "PerTierUsage" {
		return nil, f.failErr
	}
	return f.tierUsed, nil
}

func (f *fakeCostLedger) PerTierCaps(_ context.Context) (map[string]int64, error) {
	if f.failOn == "PerTierCaps" {
		return nil, f.failErr
	}
	return f.tierCaps, nil
}

type fakeOverride struct {
	rows    map[string]quota.Override
	failGet error
}

func (f *fakeOverride) Get(_ context.Context, alias string) (*quota.Override, error) {
	if f.failGet != nil {
		return nil, f.failGet
	}
	r, ok := f.rows[alias]
	if !ok {
		return nil, nil
	}
	return &r, nil
}

func (f *fakeOverride) Set(_ context.Context, alias string, mult float64, exp time.Time, reason string) error {
	if f.rows == nil {
		f.rows = map[string]quota.Override{}
	}
	f.rows[alias] = quota.Override{Alias: alias, Multiplier: mult, ExpiresAt: exp, Reason: reason}
	return nil
}

func (f *fakeOverride) Reset(_ context.Context, alias string) error {
	delete(f.rows, alias)
	return nil
}

func (f *fakeOverride) List(_ context.Context) ([]quota.Override, error) {
	out := []quota.Override{}
	for _, v := range f.rows {
		out = append(out, v)
	}
	return out, nil
}

func TestBuildPreFlightDepsFullPath(t *testing.T) {
	ctx := context.Background()
	cl := &fakeCostLedger{
		used:       5000,
		cap:        10000,
		globalUsed: 50000,
		globalCap:  100000,
		tierUsed:   map[string]int64{"paygo": 3000},
		tierCaps:   map[string]int64{"paygo": 5000},
	}
	ov := &fakeOverride{rows: map[string]quota.Override{}}
	wfq := quota.NewWfqQueue(map[string]quota.Weight{"a": 1.0})
	thr := quota.DoctrineDefaults(doctrine.NameDefault)

	deps, err := dispatcheradapter.BuildPreFlightDeps(ctx, "a", doctrine.NameDefault, "paygo", thr, cl, ov, wfq)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if deps.Used != 5000 {
		t.Errorf("Used = %d, want 5000", deps.Used)
	}
	if deps.Cap != 10000 {
		t.Errorf("Cap = %d, want 10000", deps.Cap)
	}
	if deps.GlobalUsed != 50000 {
		t.Errorf("GlobalUsed = %d, want 50000", deps.GlobalUsed)
	}
	if deps.GlobalCap != 100000 {
		t.Errorf("GlobalCap = %d, want 100000", deps.GlobalCap)
	}
	if deps.RequestTier != "paygo" {
		t.Errorf("RequestTier = %q, want paygo", deps.RequestTier)
	}
	if deps.PerTierCaps["paygo"] != 5000 {
		t.Errorf("paygo cap = %d, want 5000", deps.PerTierCaps["paygo"])
	}
	if deps.PerTierUsed["paygo"] != 3000 {
		t.Errorf("paygo used = %d, want 3000", deps.PerTierUsed["paygo"])
	}
	if deps.Wfq != wfq {
		t.Error("Wfq pointer mismatch")
	}
	if deps.Now == nil {
		t.Error("Now should default to time.Now")
	}
	if deps.CongestionThreshold != 0 {
		t.Errorf("CongestionThreshold = %d, want 0 (defaults to DefaultStarveDepthThreshold downstream)", deps.CongestionThreshold)
	}
}

func TestBuildPreFlightDepsResolvesOverride(t *testing.T) {
	ctx := context.Background()
	cl := &fakeCostLedger{cap: 100, globalCap: 100}
	expiresAt := time.Now().Add(1 * time.Hour)
	ov := &fakeOverride{rows: map[string]quota.Override{
		"a": {Alias: "a", Multiplier: 3.0, ExpiresAt: expiresAt, Reason: "demo"},
	}}
	wfq := quota.NewWfqQueue(map[string]quota.Weight{"a": 1.0})
	thr := quota.DoctrineDefaults(doctrine.NameDefault)

	deps, err := dispatcheradapter.BuildPreFlightDeps(ctx, "a", doctrine.NameDefault, "", thr, cl, ov, wfq)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if deps.Override == nil {
		t.Fatal("Override = nil; want resolved")
	}
	if deps.Override.Multiplier != 3.0 {
		t.Errorf("Override.Multiplier = %v, want 3.0", deps.Override.Multiplier)
	}
	if deps.Override.Reason != "demo" {
		t.Errorf("Override.Reason = %q, want demo", deps.Override.Reason)
	}
}

func TestBuildPreFlightDepsNoOverride(t *testing.T) {
	ctx := context.Background()
	cl := &fakeCostLedger{cap: 100, globalCap: 100}
	ov := &fakeOverride{rows: map[string]quota.Override{}}
	wfq := quota.NewWfqQueue(map[string]quota.Weight{"a": 1.0})
	thr := quota.DoctrineDefaults(doctrine.NameDefault)

	deps, err := dispatcheradapter.BuildPreFlightDeps(ctx, "a", doctrine.NameDefault, "", thr, cl, ov, wfq)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if deps.Override != nil {
		t.Errorf("Override = %+v, want nil (no override row)", deps.Override)
	}
}

// ----------------------------------------------------------------------------
// Nil-arg validation: each of the three runtime dependencies, if nil,
// MUST trigger a typed error rather than a panic.
// ----------------------------------------------------------------------------

func TestBuildPreFlightDepsRejectsNilDeps(t *testing.T) {
	ctx := context.Background()
	cl := &fakeCostLedger{}
	ov := &fakeOverride{rows: map[string]quota.Override{}}
	wfq := quota.NewWfqQueue(map[string]quota.Weight{})
	thr := quota.DoctrineDefaults(doctrine.NameDefault)

	cases := []struct {
		name string
		cl   dispatcheradapter.CostLedgerReader
		ov   quota.OverrideStore
		wfq  *quota.WfqQueue
	}{
		{"nil ledger", nil, ov, wfq},
		{"nil override store", cl, nil, wfq},
		{"nil wfq", cl, ov, nil},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			_, err := dispatcheradapter.BuildPreFlightDeps(ctx, "a", doctrine.NameDefault, "", thr, c.cl, c.ov, c.wfq)
			if err == nil {
				t.Errorf("Build with %s: want error, got nil", c.name)
			}
		})
	}
}

func TestBuildPreFlightDepsPropagatesLedgerError(t *testing.T) {
	ctx := context.Background()
	thr := quota.DoctrineDefaults(doctrine.NameDefault)
	wfq := quota.NewWfqQueue(map[string]quota.Weight{})

	for _, method := range []string{"ProjectUsage", "ProjectCap", "GlobalUsage", "GlobalCap", "PerTierUsage", "PerTierCaps"} {
		method := method
		t.Run(method, func(t *testing.T) {
			sentinel := errors.New("ledger fail: " + method)
			cl := &fakeCostLedger{failOn: method, failErr: sentinel}
			ov := &fakeOverride{rows: map[string]quota.Override{}}

			_, err := dispatcheradapter.BuildPreFlightDeps(ctx, "a", doctrine.NameDefault, "", thr, cl, ov, wfq)
			if !errors.Is(err, sentinel) {
				t.Errorf("err = %v, want sentinel from %s wrapped", err, method)
			}
		})
	}
}

func TestBuildPreFlightDepsPropagatesOverrideError(t *testing.T) {
	ctx := context.Background()
	thr := quota.DoctrineDefaults(doctrine.NameDefault)
	wfq := quota.NewWfqQueue(map[string]quota.Weight{})
	cl := &fakeCostLedger{cap: 100, globalCap: 100}
	sentinel := errors.New("override store down")
	ov := &fakeOverride{rows: map[string]quota.Override{}, failGet: sentinel}

	_, err := dispatcheradapter.BuildPreFlightDeps(ctx, "a", doctrine.NameDefault, "", thr, cl, ov, wfq)
	if !errors.Is(err, sentinel) {
		t.Errorf("err = %v, want sentinel", err)
	}
}

func TestBuildPreFlightDepsEmptyTierMaps(t *testing.T) {
	ctx := context.Background()
	cl := &fakeCostLedger{cap: 100, globalCap: 100, tierUsed: nil, tierCaps: nil}
	ov := &fakeOverride{rows: map[string]quota.Override{}}
	wfq := quota.NewWfqQueue(map[string]quota.Weight{})
	thr := quota.DoctrineDefaults(doctrine.NameDefault)

	deps, err := dispatcheradapter.BuildPreFlightDeps(ctx, "a", doctrine.NameDefault, "", thr, cl, ov, wfq)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if deps.PerTierCaps == nil {
		t.Error("PerTierCaps nil; want empty map")
	}
	if deps.PerTierUsed == nil {
		t.Error("PerTierUsed nil; want empty map")
	}
	if len(deps.PerTierCaps) != 0 {
		t.Errorf("PerTierCaps non-empty: %v", deps.PerTierCaps)
	}
	if len(deps.PerTierUsed) != 0 {
		t.Errorf("PerTierUsed non-empty: %v", deps.PerTierUsed)
	}
}

func TestBuildPreFlightDepsThresholdsPassThrough(t *testing.T) {
	ctx := context.Background()
	cl := &fakeCostLedger{cap: 100, globalCap: 100}
	ov := &fakeOverride{rows: map[string]quota.Override{}}
	wfq := quota.NewWfqQueue(map[string]quota.Weight{})

	thr := quota.DoctrineDefaults(doctrine.NameMaxScope)
	deps, err := dispatcheradapter.BuildPreFlightDeps(ctx, "a", doctrine.NameMaxScope, "", thr, cl, ov, wfq)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if deps.Thresholds.Mode != thr.Mode {
		t.Errorf("Thresholds.Mode = %v, want %v", deps.Thresholds.Mode, thr.Mode)
	}
	if deps.Thresholds.SoftCapPct != thr.SoftCapPct {
		t.Errorf("Thresholds.SoftCapPct = %d, want %d", deps.Thresholds.SoftCapPct, thr.SoftCapPct)
	}
	if deps.Thresholds.HardCapPct != thr.HardCapPct {
		t.Errorf("Thresholds.HardCapPct = %d, want %d", deps.Thresholds.HardCapPct, thr.HardCapPct)
	}
}

func TestBuildPreFlightDepsZeroCapsPassThrough(t *testing.T) {
	ctx := context.Background()
	cl := &fakeCostLedger{cap: 0, globalCap: 0}
	ov := &fakeOverride{rows: map[string]quota.Override{}}
	wfq := quota.NewWfqQueue(map[string]quota.Weight{})
	thr := quota.DoctrineDefaults(doctrine.NameDefault)

	deps, err := dispatcheradapter.BuildPreFlightDeps(ctx, "a", doctrine.NameDefault, "", thr, cl, ov, wfq)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if deps.Cap != 0 {
		t.Errorf("Cap = %d, want 0 passed through", deps.Cap)
	}
	if deps.GlobalCap != 0 {
		t.Errorf("GlobalCap = %d, want 0 passed through", deps.GlobalCap)
	}
}
