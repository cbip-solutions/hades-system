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

type trackingActuator struct {
	hardPauseErr error
	setTierErr   error

	hardPauseCalls int
	setTierCalls   int
	lastReason     string
	lastTier       int
}

func (a *trackingActuator) DropAtDepth(_ context.Context, _ int) error { return nil }
func (a *trackingActuator) SetTier(_ context.Context, t int) error {
	a.setTierCalls++
	a.lastTier = t
	return a.setTierErr
}
func (a *trackingActuator) SetParallelism(_ context.Context, _, _ int) error { return nil }
func (a *trackingActuator) HardPause(_ context.Context, r string) error {
	a.hardPauseCalls++
	a.lastReason = r
	return a.hardPauseErr
}
func (a *trackingActuator) EmergencyOnlyTier(_ context.Context) error            { return nil }
func (a *trackingActuator) EscalateL4(_ context.Context, _ map[string]any) error { return nil }
func (a *trackingActuator) WaitForConfirmation(_ context.Context, _ string) error {
	return nil
}
func (a *trackingActuator) Waiting(_ context.Context, _ string) error { return nil }
func (a *trackingActuator) RestoreDefaults(_ context.Context) error   { return nil }

type neverSignalingWorkerSet struct{}

func (neverSignalingWorkerSet) WaitAtomicBoundary(_ context.Context) <-chan struct{} {
	return make(chan struct{})
}

func g5Cfg(t *testing.T, log *eventlog.Log, act orchestrator.OrchestratorActuator, ws orchestrator.WorkerSet, profile string) orchestrator.CostGatingEngineConfig {
	t.Helper()
	prof, err := orchestrator.BuiltinCostProfile(profile)
	if err != nil {
		t.Fatalf("BuiltinCostProfile(%q): %v", profile, err)
	}
	return orchestrator.CostGatingEngineConfig{
		Clock:     clock.NewFake(time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)),
		EventLog:  log,
		Budget:    &costFakeSnapshotReader{},
		Workers:   ws,
		Actuator:  act,
		Profile:   prof,
		Override:  nil,
		PollEvery: 20 * time.Millisecond,
		SessionID: "sess-g5",
		ProjectID: "proj-g5",
	}
}

func firstDegradationApplied(t *testing.T, log *eventlog.Log) *eventlog.BudgetDegradationApplied {
	t.Helper()
	records, err := log.Query(context.Background(), "sess-g5", 0)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	for _, rec := range records {
		if rec.EventType != eventlog.EvtBudgetDegradationApplied {
			continue
		}
		dec, derr := eventlog.Decode(rec.EventType, rec.Payload)
		if derr != nil {
			t.Fatalf("Decode: %v", derr)
		}
		bda, ok := dec.(eventlog.BudgetDegradationApplied)
		if !ok {
			t.Fatalf("Decode type = %T, want BudgetDegradationApplied", dec)
		}
		return &bda
	}
	return nil
}

func degradationCount(t *testing.T, log *eventlog.Log) int {
	t.Helper()
	records, err := log.Query(context.Background(), "sess-g5", 0)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	n := 0
	for _, rec := range records {
		if rec.EventType == eventlog.EvtBudgetDegradationApplied {
			n++
		}
	}
	return n
}

func allDegradations(t *testing.T, log *eventlog.Log) []eventlog.BudgetDegradationApplied {
	t.Helper()
	records, err := log.Query(context.Background(), "sess-g5", 0)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	var out []eventlog.BudgetDegradationApplied
	for _, rec := range records {
		if rec.EventType != eventlog.EvtBudgetDegradationApplied {
			continue
		}
		dec, derr := eventlog.Decode(rec.EventType, rec.Payload)
		if derr != nil {
			t.Fatalf("Decode: %v", derr)
		}
		out = append(out, dec.(eventlog.BudgetDegradationApplied))
	}
	return out
}

func TestApply_EmitsBudgetDegradationAppliedWithAttribution(t *testing.T) {
	log := eventlog.NewMemory(clock.Real{})
	act := &trackingActuator{}
	ws := newCostFakeWorkerSet(true)
	cfg := g5Cfg(t, log, act, ws, "max-scope")
	eng, err := orchestrator.NewCostGatingEngine(cfg)
	if err != nil {
		t.Fatalf("NewCostGatingEngine: %v", err)
	}

	snap := orchestrator.BudgetSnapshot{
		CumulativeUSD:   85,
		DailyCapUSD:     100,
		ProjectedEODUSD: 110,
		PAYGActive:      false,
		ProjectID:       "internal-platform-x",
		DoctrineName:    "max-scope",
	}
	row := orchestrator.ThresholdRow{Pct: 80, Action: orchestrator.CostActionTierDegradeL2}
	if err := eng.Apply(context.Background(), row, snap); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	bda := firstDegradationApplied(t, log)
	if bda == nil {
		t.Fatal("BudgetDegradationApplied not emitted")
	}
	if bda.ThresholdPct != 80 {
		t.Errorf("ThresholdPct = %d, want 80", bda.ThresholdPct)
	}
	if bda.Action != "tier_degrade_l2" {
		t.Errorf("Action = %q, want tier_degrade_l2", bda.Action)
	}
	if bda.PriorAction != "continue" {
		t.Errorf("PriorAction = %q, want continue (first-time apply)", bda.PriorAction)
	}
	if bda.Doctrine != "max-scope" {
		t.Errorf("Doctrine = %q, want max-scope", bda.Doctrine)
	}
	if bda.ProjectID != "internal-platform-x" {
		t.Errorf("ProjectID = %q, want internal-platform-x", bda.ProjectID)
	}
	if bda.CumulativeUSD != 85.0 {
		t.Errorf("CumulativeUSD = %v, want 85", bda.CumulativeUSD)
	}
	if bda.DailyCapUSD != 100.0 {
		t.Errorf("DailyCapUSD = %v, want 100", bda.DailyCapUSD)
	}
	if bda.ProjectedEODUSD != 110.0 {
		t.Errorf("ProjectedEODUSD = %v, want 110", bda.ProjectedEODUSD)
	}
	if bda.PAYGActive {
		t.Errorf("PAYGActive = true, want false")
	}

	if act.setTierCalls != 1 || act.lastTier != 2 {
		t.Errorf("SetTier calls = %d (lastTier=%d), want 1 call with tier=2", act.setTierCalls, act.lastTier)
	}
}

func TestApply_PriorActionTracksTransitions(t *testing.T) {
	log := eventlog.NewMemory(clock.Real{})
	act := &trackingActuator{}
	ws := newCostFakeWorkerSet(true)
	cfg := g5Cfg(t, log, act, ws, "max-scope")
	eng, _ := orchestrator.NewCostGatingEngine(cfg)

	snap := orchestrator.BudgetSnapshot{DoctrineName: "max-scope", DailyCapUSD: 100}

	row1 := orchestrator.ThresholdRow{Pct: 60, Action: orchestrator.CostActionDropL3Strategic}
	snap.CumulativeUSD = 60
	if err := eng.Apply(context.Background(), row1, snap); err != nil {
		t.Fatalf("Apply 1: %v", err)
	}

	row2 := orchestrator.ThresholdRow{Pct: 80, Action: orchestrator.CostActionTierDegradeL2}
	snap.CumulativeUSD = 80
	if err := eng.Apply(context.Background(), row2, snap); err != nil {
		t.Fatalf("Apply 2: %v", err)
	}

	emitted := allDegradations(t, log)
	if len(emitted) != 2 {
		t.Fatalf("len(emitted) = %d, want 2", len(emitted))
	}
	if emitted[0].PriorAction != "continue" {
		t.Errorf("event 0 PriorAction = %q, want continue (first-time)", emitted[0].PriorAction)
	}
	if emitted[1].PriorAction != "drop_l3_strategic" {
		t.Errorf("event 1 PriorAction = %q, want drop_l3_strategic (after first apply)", emitted[1].PriorAction)
	}
}

func TestApply_PAYGActivation_AttributionRecorded(t *testing.T) {
	log := eventlog.NewMemory(clock.Real{})
	act := &trackingActuator{}
	ws := newCostFakeWorkerSet(true)
	cfg := g5Cfg(t, log, act, ws, "max-scope")
	eng, _ := orchestrator.NewCostGatingEngine(cfg)

	row1 := orchestrator.ThresholdRow{Pct: 60, Action: orchestrator.CostActionDropL3Strategic}
	snap1 := orchestrator.BudgetSnapshot{CumulativeUSD: 60, DailyCapUSD: 100, DoctrineName: "max-scope"}
	if err := eng.Apply(context.Background(), row1, snap1); err != nil {
		t.Fatalf("Apply 1: %v", err)
	}

	row2 := orchestrator.ThresholdRow{Pct: orchestrator.PctPAYG, Action: orchestrator.CostActionEmergencyOnlyTier}
	snap2 := orchestrator.BudgetSnapshot{CumulativeUSD: 5, DailyCapUSD: 100, PAYGActive: true, DoctrineName: "max-scope"}
	if err := eng.Apply(context.Background(), row2, snap2); err != nil {
		t.Fatalf("Apply 2: %v", err)
	}

	emitted := allDegradations(t, log)
	if len(emitted) != 2 {
		t.Fatalf("len(emitted) = %d, want 2", len(emitted))
	}
	if !emitted[1].PAYGActive {
		t.Errorf("event 1 PAYGActive = false, want true")
	}
	if emitted[1].PriorAction != "drop_l3_strategic" {
		t.Errorf("event 1 PriorAction = %q, want drop_l3_strategic", emitted[1].PriorAction)
	}
	if emitted[1].Action != "emergency_only_tier" {
		t.Errorf("event 1 Action = %q, want emergency_only_tier", emitted[1].Action)
	}
}

func TestApply_NoEmissionOnActuatorError(t *testing.T) {
	log := eventlog.NewMemory(clock.Real{})
	wantErr := errors.New("actuator failure")
	act := &trackingActuator{setTierErr: wantErr}
	ws := newCostFakeWorkerSet(true)
	cfg := g5Cfg(t, log, act, ws, "max-scope")
	eng, _ := orchestrator.NewCostGatingEngine(cfg)

	row := orchestrator.ThresholdRow{Pct: 80, Action: orchestrator.CostActionTierDegradeL2}
	snap := orchestrator.BudgetSnapshot{CumulativeUSD: 80, DailyCapUSD: 100, DoctrineName: "max-scope"}
	if err := eng.Apply(context.Background(), row, snap); !errors.Is(err, wantErr) {
		t.Fatalf("Apply err = %v, want chain to actuator failure", err)
	}
	if degradationCount(t, log) != 0 {
		t.Errorf("BudgetDegradationApplied emitted on actuator error; want 0")
	}
}

func TestApply_AuditEmissionSurvivesCtxCancel(t *testing.T) {

	log := eventlog.NewMemory(clock.Real{})
	act := &trackingActuator{}
	ws := newCostFakeWorkerSet(true)
	cfg := g5Cfg(t, log, act, ws, "max-scope")
	eng, _ := orchestrator.NewCostGatingEngine(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	row := orchestrator.ThresholdRow{Pct: 0, Action: orchestrator.CostActionContinue}
	snap := orchestrator.BudgetSnapshot{CumulativeUSD: 5, DailyCapUSD: 100, DoctrineName: "max-scope"}
	if err := eng.Apply(ctx, row, snap); err != nil {
		t.Fatalf("Apply Continue with cancelled ctx: %v", err)
	}
	if degradationCount(t, log) != 1 {
		t.Errorf("audit not emitted under cancelled ctx (WithoutCancel pin); got %d, want 1", degradationCount(t, log))
	}
}

func TestApply_TypedPayloadRoundTrip(t *testing.T) {
	log := eventlog.NewMemory(clock.Real{})
	act := &trackingActuator{}
	ws := newCostFakeWorkerSet(true)
	cfg := g5Cfg(t, log, act, ws, "max-scope")
	eng, _ := orchestrator.NewCostGatingEngine(cfg)

	row := orchestrator.ThresholdRow{Pct: 100, Action: orchestrator.CostActionHardPause}
	snap := orchestrator.BudgetSnapshot{
		CumulativeUSD:   105,
		DailyCapUSD:     100,
		ProjectedEODUSD: 130,
		PAYGActive:      true,
		ProjectID:       "internal-platform-x",
		DoctrineName:    "max-scope",
	}
	if err := eng.Apply(context.Background(), row, snap); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	bda := firstDegradationApplied(t, log)
	if bda == nil {
		t.Fatal("event missing")
	}

	if bda.ThresholdPct != 100 || bda.Action != "hard_pause" ||
		bda.PriorAction != "continue" || bda.Doctrine != "max-scope" ||
		bda.ProjectID != "internal-platform-x" || bda.CumulativeUSD != 105 ||
		bda.DailyCapUSD != 100 || bda.ProjectedEODUSD != 130 || !bda.PAYGActive {
		t.Errorf("typed roundtrip incomplete: %+v", *bda)
	}
}

func TestApply_ContinueAction_SkipsAtomicityGuard(t *testing.T) {
	log := eventlog.NewMemory(clock.Real{})
	act := &trackingActuator{}
	ws := neverSignalingWorkerSet{}
	cfg := g5Cfg(t, log, act, ws, "max-scope")
	eng, _ := orchestrator.NewCostGatingEngine(cfg)

	row := orchestrator.ThresholdRow{Pct: 0, Action: orchestrator.CostActionContinue}
	snap := orchestrator.BudgetSnapshot{CumulativeUSD: 5, DailyCapUSD: 100, DoctrineName: "max-scope"}

	done := make(chan error, 1)
	go func() { done <- eng.Apply(context.Background(), row, snap) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Apply Continue: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Apply Continue blocked on atomicity guard (must skip for Continue)")
	}

	bda := firstDegradationApplied(t, log)
	if bda == nil {
		t.Fatal("BudgetDegradationApplied not emitted for Continue")
	}
	if bda.Action != "continue" || bda.PriorAction != "continue" {
		t.Errorf("Continue attribution wrong: action=%q prior=%q", bda.Action, bda.PriorAction)
	}
}
