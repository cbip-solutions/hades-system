package orchestrator_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

type fakeBudgetReader struct{}

func (fakeBudgetReader) Snapshot(_ context.Context) (orchestrator.BudgetSnapshot, error) {
	return orchestrator.BudgetSnapshot{}, nil
}

type fakeWorkerSet struct {
	ch chan struct{}
}

func newFakeWorkerSet(ready bool) *fakeWorkerSet {
	ch := make(chan struct{})
	if ready {
		close(ch)
	}
	return &fakeWorkerSet{ch: ch}
}

func (f *fakeWorkerSet) WaitAtomicBoundary(_ context.Context) <-chan struct{} { return f.ch }

func (f *fakeWorkerSet) signalAtomicBoundary() {
	select {
	case <-f.ch:

	default:
		close(f.ch)
	}
}

type fakeActuator struct{}

func (fakeActuator) DropAtDepth(_ context.Context, _ int) error            { return nil }
func (fakeActuator) SetTier(_ context.Context, _ int) error                { return nil }
func (fakeActuator) SetParallelism(_ context.Context, _, _ int) error      { return nil }
func (fakeActuator) HardPause(_ context.Context, _ string) error           { return nil }
func (fakeActuator) EmergencyOnlyTier(_ context.Context) error             { return nil }
func (fakeActuator) EscalateL4(_ context.Context, _ map[string]any) error  { return nil }
func (fakeActuator) WaitForConfirmation(_ context.Context, _ string) error { return nil }
func (fakeActuator) Waiting(_ context.Context, _ string) error             { return nil }
func (fakeActuator) RestoreDefaults(_ context.Context) error               { return nil }

type fakeCostAppender struct{}

func (fakeCostAppender) Append(_ context.Context, _ eventlog.Event) (int64, error) { return 0, nil }

func TestLoadThresholdTable_MaxScopeDoctrine(t *testing.T) {
	prof, err := orchestrator.BuiltinCostProfile("max-scope")
	if err != nil {
		t.Fatalf("BuiltinCostProfile: %v", err)
	}
	tab, err := orchestrator.LoadThresholdTable(prof, nil)
	if err != nil {
		t.Fatalf("LoadThresholdTable: %v", err)
	}
	want := []orchestrator.ThresholdRow{
		{Pct: 60, Action: orchestrator.CostActionDropL3Strategic},
		{Pct: 80, Action: orchestrator.CostActionTierDegradeL2},
		{Pct: 90, Action: orchestrator.CostActionReduceParallelism},
		{Pct: 100, Action: orchestrator.CostActionHardPause},
		{Pct: orchestrator.PctPAYG, Action: orchestrator.CostActionEmergencyOnlyTier},
	}
	if len(tab) != len(want) {
		t.Fatalf("rows: got %d want %d (%+v)", len(tab), len(want), tab)
	}
	for i, row := range tab {
		if row != want[i] {
			t.Errorf("row[%d]: got %+v want %+v", i, row, want[i])
		}
	}
}

func TestLoadThresholdTable_DefaultDoctrine(t *testing.T) {
	prof, err := orchestrator.BuiltinCostProfile("default")
	if err != nil {
		t.Fatalf("BuiltinCostProfile: %v", err)
	}
	tab, err := orchestrator.LoadThresholdTable(prof, nil)
	if err != nil {
		t.Fatalf("LoadThresholdTable: %v", err)
	}
	want := []orchestrator.ThresholdRow{
		{Pct: 60, Action: orchestrator.CostActionContinue},
		{Pct: 80, Action: orchestrator.CostActionTierDegradeL1L2},
		{Pct: 90, Action: orchestrator.CostActionHardPause},
		{Pct: 100, Action: orchestrator.CostActionHardPause},
		{Pct: orchestrator.PctPAYG, Action: orchestrator.CostActionHardPause},
	}
	if len(tab) != len(want) {
		t.Fatalf("rows: got %d want %d (%+v)", len(tab), len(want), tab)
	}
	for i, row := range tab {
		if row != want[i] {
			t.Errorf("row[%d]: got %+v want %+v", i, row, want[i])
		}
	}
}

func TestLoadThresholdTable_CapaFirewallDoctrine(t *testing.T) {
	prof, err := orchestrator.BuiltinCostProfile("capa-firewall")
	if err != nil {
		t.Fatalf("BuiltinCostProfile: %v", err)
	}
	tab, err := orchestrator.LoadThresholdTable(prof, nil)
	if err != nil {
		t.Fatalf("LoadThresholdTable: %v", err)
	}
	want := []orchestrator.ThresholdRow{
		{Pct: 60, Action: orchestrator.CostActionEscalateL4},
		{Pct: 80, Action: orchestrator.CostActionWaitingForConfirmation},
		{Pct: 90, Action: orchestrator.CostActionWaiting},
		{Pct: 100, Action: orchestrator.CostActionHardPause},
		{Pct: orchestrator.PctPAYG, Action: orchestrator.CostActionHardPause},
	}
	if len(tab) != len(want) {
		t.Fatalf("rows: got %d want %d (%+v)", len(tab), len(want), tab)
	}
	for i, row := range tab {
		if row != want[i] {
			t.Errorf("row[%d]: got %+v want %+v", i, row, want[i])
		}
	}
}

func TestLoadThresholdTable_RejectsUnknownAction(t *testing.T) {
	prof, err := orchestrator.BuiltinCostProfile("max-scope")
	if err != nil {
		t.Fatalf("BuiltinCostProfile: %v", err)
	}

	mut := make(map[string]string, len(prof.Actions))
	for k, v := range prof.Actions {
		mut[k] = v
	}
	mut["80"] = "nuke_orbit"
	prof.Actions = mut

	_, err = orchestrator.LoadThresholdTable(prof, nil)
	if !errors.Is(err, orchestrator.ErrUnknownCostAction) {
		t.Fatalf("want ErrUnknownCostAction, got %v", err)
	}
}

func TestLoadThresholdTable_RejectsLoosenedProjectOverride(t *testing.T) {
	prof, err := orchestrator.BuiltinCostProfile("capa-firewall")
	if err != nil {
		t.Fatalf("BuiltinCostProfile: %v", err)
	}

	override := &orchestrator.ProjectOverride{
		ActionsByPct: map[string]string{"80": "continue"},
	}
	_, err = orchestrator.LoadThresholdTable(prof, override)
	if !errors.Is(err, orchestrator.ErrTightenOnlyViolation) {
		t.Fatalf("want ErrTightenOnlyViolation, got %v", err)
	}
}

func TestLoadThresholdTable_AllowsTightenedProjectOverride(t *testing.T) {
	prof, err := orchestrator.BuiltinCostProfile("default")
	if err != nil {
		t.Fatalf("BuiltinCostProfile: %v", err)
	}

	override := &orchestrator.ProjectOverride{
		ActionsByPct: map[string]string{"60": "hard_pause"},
	}
	tab, err := orchestrator.LoadThresholdTable(prof, override)
	if err != nil {
		t.Fatalf("LoadThresholdTable: %v", err)
	}
	if tab[0].Pct != 60 {
		t.Fatalf("first row pct: got %d want 60", tab[0].Pct)
	}
	if tab[0].Action != orchestrator.CostActionHardPause {
		t.Fatalf("first row action: got %q want %q",
			tab[0].Action, orchestrator.CostActionHardPause)
	}
}

func TestLoadThresholdTable_RejectsUnknownOverrideAction(t *testing.T) {
	prof, err := orchestrator.BuiltinCostProfile("default")
	if err != nil {
		t.Fatalf("BuiltinCostProfile: %v", err)
	}
	override := &orchestrator.ProjectOverride{
		ActionsByPct: map[string]string{"80": "summon_kraken"},
	}
	_, err = orchestrator.LoadThresholdTable(prof, override)
	if !errors.Is(err, orchestrator.ErrUnknownCostAction) {
		t.Fatalf("want ErrUnknownCostAction, got %v", err)
	}
}

func TestLoadThresholdTable_MissingThresholdRow(t *testing.T) {

	prof := orchestrator.CostProfile{
		DoctrineName: "max-scope",
		Actions: map[string]string{
			"60":   "drop_l3_strategic",
			"80":   "tier_degrade_l2",
			"100":  "hard_pause",
			"payg": "emergency_only_tier",
		},
		AtomicityTimeout:     30 * time.Second,
		RecoveryStepInterval: 60 * time.Second,
	}
	_, err := orchestrator.LoadThresholdTable(prof, nil)
	if !errors.Is(err, orchestrator.ErrMissingCostActionRow) {
		t.Fatalf("want ErrMissingCostActionRow, got %v", err)
	}
}

func TestBuiltinCostProfile_UnknownDoctrine(t *testing.T) {
	_, err := orchestrator.BuiltinCostProfile("garbage")
	if !errors.Is(err, orchestrator.ErrUnknownDoctrine) {
		t.Fatalf("want ErrUnknownDoctrine, got %v", err)
	}
}

func TestBuiltinCostProfile_AllThreeDoctrines_HaveDefaults(t *testing.T) {
	for _, name := range []string{"max-scope", "default", "capa-firewall"} {
		prof, err := orchestrator.BuiltinCostProfile(name)
		if err != nil {
			t.Fatalf("BuiltinCostProfile(%q): %v", name, err)
		}
		if prof.DoctrineName != name {
			t.Errorf("DoctrineName: got %q want %q", prof.DoctrineName, name)
		}
		if prof.AtomicityTimeout != 30*time.Second {
			t.Errorf("%s AtomicityTimeout: got %v want 30s", name, prof.AtomicityTimeout)
		}
		if prof.RecoveryStepInterval != 60*time.Second {
			t.Errorf("%s RecoveryStepInterval: got %v want 60s", name, prof.RecoveryStepInterval)
		}
		if len(prof.Actions) != 5 {
			t.Errorf("%s Actions: got %d entries want 5", name, len(prof.Actions))
		}
	}
}

func goodCfg(t *testing.T) orchestrator.CostGatingEngineConfig {
	t.Helper()
	prof, err := orchestrator.BuiltinCostProfile("max-scope")
	if err != nil {
		t.Fatalf("BuiltinCostProfile: %v", err)
	}
	return orchestrator.CostGatingEngineConfig{
		Clock:     clock.Real{},
		EventLog:  fakeCostAppender{},
		Budget:    fakeBudgetReader{},
		Workers:   newFakeWorkerSet(true),
		Actuator:  fakeActuator{},
		Profile:   prof,
		Override:  nil,
		PollEvery: 500 * time.Millisecond,
		SessionID: "sess-1",
		ProjectID: "proj-1",
	}
}

func TestNewCostGatingEngine_NilDeps_Errors(t *testing.T) {
	t.Run("nil clock", func(t *testing.T) {
		cfg := goodCfg(t)
		cfg.Clock = nil
		_, err := orchestrator.NewCostGatingEngine(cfg)
		if !errors.Is(err, orchestrator.ErrInvalidConfig) {
			t.Fatalf("want ErrInvalidConfig, got %v", err)
		}
	})
	t.Run("nil eventlog", func(t *testing.T) {
		cfg := goodCfg(t)
		cfg.EventLog = nil
		_, err := orchestrator.NewCostGatingEngine(cfg)
		if !errors.Is(err, orchestrator.ErrInvalidConfig) {
			t.Fatalf("want ErrInvalidConfig, got %v", err)
		}
	})
	t.Run("nil budget", func(t *testing.T) {
		cfg := goodCfg(t)
		cfg.Budget = nil
		_, err := orchestrator.NewCostGatingEngine(cfg)
		if !errors.Is(err, orchestrator.ErrInvalidConfig) {
			t.Fatalf("want ErrInvalidConfig, got %v", err)
		}
	})
	t.Run("nil workers", func(t *testing.T) {
		cfg := goodCfg(t)
		cfg.Workers = nil
		_, err := orchestrator.NewCostGatingEngine(cfg)
		if !errors.Is(err, orchestrator.ErrInvalidConfig) {
			t.Fatalf("want ErrInvalidConfig, got %v", err)
		}
	})
	t.Run("nil actuator", func(t *testing.T) {
		cfg := goodCfg(t)
		cfg.Actuator = nil
		_, err := orchestrator.NewCostGatingEngine(cfg)
		if !errors.Is(err, orchestrator.ErrInvalidConfig) {
			t.Fatalf("want ErrInvalidConfig, got %v", err)
		}
	})
	t.Run("empty session id", func(t *testing.T) {
		cfg := goodCfg(t)
		cfg.SessionID = ""
		_, err := orchestrator.NewCostGatingEngine(cfg)
		if !errors.Is(err, orchestrator.ErrInvalidConfig) {
			t.Fatalf("want ErrInvalidConfig, got %v", err)
		}
	})
	t.Run("empty project id", func(t *testing.T) {
		cfg := goodCfg(t)
		cfg.ProjectID = ""
		_, err := orchestrator.NewCostGatingEngine(cfg)
		if !errors.Is(err, orchestrator.ErrInvalidConfig) {
			t.Fatalf("want ErrInvalidConfig, got %v", err)
		}
	})
}

func TestNewCostGatingEngine_HappyPath(t *testing.T) {
	cfg := goodCfg(t)
	eng, err := orchestrator.NewCostGatingEngine(cfg)
	if err != nil {
		t.Fatalf("NewCostGatingEngine: %v", err)
	}
	if eng == nil {
		t.Fatal("engine is nil")
	}
}

func TestNewCostGatingEngine_InvalidProfile_PropagatesError(t *testing.T) {
	cfg := goodCfg(t)
	mut := make(map[string]string, len(cfg.Profile.Actions))
	for k, v := range cfg.Profile.Actions {
		mut[k] = v
	}
	mut["80"] = "nuke_orbit"
	cfg.Profile.Actions = mut

	_, err := orchestrator.NewCostGatingEngine(cfg)
	if !errors.Is(err, orchestrator.ErrUnknownCostAction) {
		t.Fatalf("want ErrUnknownCostAction (wrapped), got %v", err)
	}
}

type costG4TestActuator struct {
	dropAtDepthCalls int
	dropAtDepthArg   int
	hardPauseCalls   int
	hardPauseReason  string
}

func (f *costG4TestActuator) DropAtDepth(_ context.Context, layer int) error {
	f.dropAtDepthCalls++
	f.dropAtDepthArg = layer
	return nil
}
func (f *costG4TestActuator) SetTier(_ context.Context, _ int) error           { return nil }
func (f *costG4TestActuator) SetParallelism(_ context.Context, _, _ int) error { return nil }
func (f *costG4TestActuator) HardPause(_ context.Context, reason string) error {
	f.hardPauseCalls++
	f.hardPauseReason = reason
	return nil
}
func (f *costG4TestActuator) EmergencyOnlyTier(_ context.Context) error             { return nil }
func (f *costG4TestActuator) EscalateL4(_ context.Context, _ map[string]any) error  { return nil }
func (f *costG4TestActuator) WaitForConfirmation(_ context.Context, _ string) error { return nil }
func (f *costG4TestActuator) Waiting(_ context.Context, _ string) error             { return nil }
func (f *costG4TestActuator) RestoreDefaults(_ context.Context) error               { return nil }

func TestApply_AtomicReadyImmediate(t *testing.T) {
	fakeClk := clock.NewFake(time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC))
	log := eventlog.NewMemory(fakeClk)
	snap := &costFakeSnapshotReader{}
	snap.set(orchestrator.BudgetSnapshot{CumulativeUSD: 100, DailyCapUSD: 100})

	ws := newFakeWorkerSet(true)
	prof, _ := orchestrator.BuiltinCostProfile("max-scope")

	act := &costG4TestActuator{}
	cfg := orchestrator.CostGatingEngineConfig{
		Clock:     fakeClk,
		EventLog:  log,
		Budget:    snap,
		Workers:   ws,
		Actuator:  act,
		Profile:   prof,
		Override:  nil,
		PollEvery: 500 * time.Millisecond,
		SessionID: "sess-g4",
		ProjectID: "proj-g4",
	}
	eng, err := orchestrator.NewCostGatingEngine(cfg)
	if err != nil {
		t.Fatalf("NewCostGatingEngine: %v", err)
	}

	if err := eng.Apply(context.Background(),
		orchestrator.ThresholdRow{Pct: 100, Action: orchestrator.CostActionHardPause},
		orchestrator.BudgetSnapshot{CumulativeUSD: 100, DailyCapUSD: 100}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if act.hardPauseCalls != 1 {
		t.Fatalf("HardPause not applied: calls=%d", act.hardPauseCalls)
	}

	recs, _ := log.Query(context.Background(), "sess-g4", 0)
	for _, rec := range recs {
		if rec.EventType == eventlog.EvtCostGatingAtomicityTimeout {
			t.Fatalf("warn event must not fire when boundary already ready")
		}
	}
}

func TestApply_WaitsForAtomicBoundary(t *testing.T) {
	fakeClk := clock.NewFake(time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC))
	log := eventlog.NewMemory(fakeClk)
	snap := &costFakeSnapshotReader{}
	snap.set(orchestrator.BudgetSnapshot{CumulativeUSD: 60, DailyCapUSD: 100})

	ws := newFakeWorkerSet(false)
	prof, _ := orchestrator.BuiltinCostProfile("max-scope")

	act := &costG4TestActuator{}
	cfg := orchestrator.CostGatingEngineConfig{
		Clock:     fakeClk,
		EventLog:  log,
		Budget:    snap,
		Workers:   ws,
		Actuator:  act,
		Profile:   prof,
		Override:  nil,
		PollEvery: 500 * time.Millisecond,
		SessionID: "sess-g4",
		ProjectID: "proj-g4",
	}
	eng, err := orchestrator.NewCostGatingEngine(cfg)
	if err != nil {
		t.Fatalf("NewCostGatingEngine: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- eng.Apply(context.Background(),
			orchestrator.ThresholdRow{Pct: 60, Action: orchestrator.CostActionDropL3Strategic},
			orchestrator.BudgetSnapshot{CumulativeUSD: 60, DailyCapUSD: 100})
	}()

	// Worker still mid-atomic-unit: action MUST NOT have fired yet.
	fakeClk.Advance(5 * time.Second)
	if act.dropAtDepthCalls != 0 {
		t.Fatalf("action fired before commit boundary: calls=%d", act.dropAtDepthCalls)
	}

	ws.signalAtomicBoundary()
	if err := <-done; err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if act.dropAtDepthCalls != 1 || act.dropAtDepthArg != 3 {
		t.Fatalf("action did not apply post-boundary: calls=%d, arg=%d", act.dropAtDepthCalls, act.dropAtDepthArg)
	}
}

func TestApply_AtomicTimeoutEmitsWarnAndProceeds(t *testing.T) {

	realClk := clock.Real{}
	log := eventlog.NewMemory(realClk)
	snap := &costFakeSnapshotReader{}
	snap.set(orchestrator.BudgetSnapshot{CumulativeUSD: 60, DailyCapUSD: 100})

	ws := newFakeWorkerSet(false)
	prof, _ := orchestrator.BuiltinCostProfile("max-scope")
	prof.AtomicityTimeout = 100 * time.Millisecond

	act := &costG4TestActuator{}
	cfg := orchestrator.CostGatingEngineConfig{
		Clock:     realClk,
		EventLog:  log,
		Budget:    snap,
		Workers:   ws,
		Actuator:  act,
		Profile:   prof,
		Override:  nil,
		PollEvery: 500 * time.Millisecond,
		SessionID: "sess-g4",
		ProjectID: "proj-g4",
	}
	eng, err := orchestrator.NewCostGatingEngine(cfg)
	if err != nil {
		t.Fatalf("NewCostGatingEngine: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- eng.Apply(context.Background(),
			orchestrator.ThresholdRow{Pct: 60, Action: orchestrator.CostActionDropL3Strategic},
			orchestrator.BudgetSnapshot{CumulativeUSD: 60, DailyCapUSD: 100})
	}()

	if err := <-done; err != nil {
		t.Fatalf("Apply on timeout must proceed not error: %v", err)
	}
	if act.dropAtDepthCalls != 1 || act.dropAtDepthArg != 3 {
		t.Fatalf("action must apply post-timeout (warn-and-proceed): calls=%d, arg=%d", act.dropAtDepthCalls, act.dropAtDepthArg)
	}

	recs, _ := log.Query(context.Background(), "sess-g4", 0)
	found := false
	for _, rec := range recs {
		if rec.EventType == eventlog.EvtCostGatingAtomicityTimeout {
			found = true

			decoded, err := eventlog.Decode(rec.EventType, rec.Payload)
			if err != nil {
				t.Fatalf("Decode timeout event: %v", err)
			}
			payload := decoded.(eventlog.CostGatingAtomicityTimeout)
			if payload.TimeoutSec != 0.1 {
				t.Errorf("TimeoutSec: got %v, want 0.1", payload.TimeoutSec)
			}
			break
		}
	}
	if !found {
		t.Fatalf("CostGatingAtomicityTimeout warn event missing")
	}
}

func TestApply_CtxCancelledDuringWait(t *testing.T) {
	fakeClk := clock.NewFake(time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC))
	log := eventlog.NewMemory(fakeClk)
	snap := &costFakeSnapshotReader{}
	snap.set(orchestrator.BudgetSnapshot{CumulativeUSD: 60, DailyCapUSD: 100})

	ws := newFakeWorkerSet(false)
	prof, _ := orchestrator.BuiltinCostProfile("max-scope")

	act := &costG4TestActuator{}
	cfg := orchestrator.CostGatingEngineConfig{
		Clock:     fakeClk,
		EventLog:  log,
		Budget:    snap,
		Workers:   ws,
		Actuator:  act,
		Profile:   prof,
		Override:  nil,
		PollEvery: 500 * time.Millisecond,
		SessionID: "sess-g4",
		ProjectID: "proj-g4",
	}
	eng, err := orchestrator.NewCostGatingEngine(cfg)
	if err != nil {
		t.Fatalf("NewCostGatingEngine: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- eng.Apply(ctx,
			orchestrator.ThresholdRow{Pct: 60, Action: orchestrator.CostActionDropL3Strategic},
			orchestrator.BudgetSnapshot{CumulativeUSD: 60, DailyCapUSD: 100})
	}()

	time.Sleep(1 * time.Millisecond)
	cancel()

	if err := <-done; err == nil {
		t.Fatalf("Apply on ctx cancel must return error, got nil")
	}
	if act.dropAtDepthCalls != 0 {
		t.Fatalf("action must not apply on ctx cancel: calls=%d", act.dropAtDepthCalls)
	}

	recs, _ := log.Query(context.Background(), "sess-g4", 0)
	for _, rec := range recs {
		if rec.EventType == eventlog.EvtCostGatingAtomicityTimeout {
			t.Fatalf("warn event must not fire on context cancellation")
		}
	}
}
