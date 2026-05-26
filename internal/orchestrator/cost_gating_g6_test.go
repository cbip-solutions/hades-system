package orchestrator_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

type recoveryActuator struct {
	mu                sync.Mutex
	restoreCalls      int
	dropAtDepthCalls  int
	setTierCalls      int
	setParallelism    int
	hardPauseCalls    int
	emergencyOnly     int
	escalateL4Calls   int
	waitConfirmCalls  int
	waitingCalls      int
	lastRestoreReason string
}

func (a *recoveryActuator) DropAtDepth(_ context.Context, _ int) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.dropAtDepthCalls++
	return nil
}
func (a *recoveryActuator) SetTier(_ context.Context, _ int) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.setTierCalls++
	return nil
}
func (a *recoveryActuator) SetParallelism(_ context.Context, _, _ int) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.setParallelism++
	return nil
}
func (a *recoveryActuator) HardPause(_ context.Context, _ string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.hardPauseCalls++
	return nil
}
func (a *recoveryActuator) EmergencyOnlyTier(_ context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.emergencyOnly++
	return nil
}
func (a *recoveryActuator) EscalateL4(_ context.Context, _ map[string]any) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.escalateL4Calls++
	return nil
}
func (a *recoveryActuator) WaitForConfirmation(_ context.Context, _ string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.waitConfirmCalls++
	return nil
}
func (a *recoveryActuator) Waiting(_ context.Context, _ string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.waitingCalls++
	return nil
}
func (a *recoveryActuator) RestoreDefaults(_ context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.restoreCalls++
	return nil
}

func (a *recoveryActuator) RestoreCalls() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.restoreCalls
}

func countByEventType(t *testing.T, log *eventlog.Log, sessionID string, et eventlog.EventType) int {
	t.Helper()
	records, err := log.Query(context.Background(), sessionID, 0)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	n := 0
	for _, rec := range records {
		if rec.EventType == et {
			n++
		}
	}
	return n
}

func recoveryCfg(t *testing.T, log *eventlog.Log, act orchestrator.OrchestratorActuator, fakeClk *clock.Fake, profile string) orchestrator.CostGatingEngineConfig {
	t.Helper()
	prof, err := orchestrator.BuiltinCostProfile(profile)
	if err != nil {
		t.Fatalf("BuiltinCostProfile(%q): %v", profile, err)
	}

	prof.RecoveryStepInterval = 60 * time.Second
	return orchestrator.CostGatingEngineConfig{
		Clock:     fakeClk,
		EventLog:  log,
		Budget:    &costFakeSnapshotReader{},
		Workers:   newCostFakeWorkerSet(true),
		Actuator:  act,
		Profile:   prof,
		Override:  nil,
		PollEvery: 20 * time.Millisecond,
		SessionID: "sess-g6",
		ProjectID: "proj-g6",
	}
}

func waitForCount(t *testing.T, log *eventlog.Log, sessionID string, et eventlog.EventType, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if countByEventType(t, log, sessionID, et) >= n {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("waitForCount: never reached %d events of type %v (have %d)", n, et, countByEventType(t, log, sessionID, et))
}

func recoveryTestCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()

		time.Sleep(20 * time.Millisecond)
	})
	return ctx
}

func TestRecovery_WalksDownwardGradually(t *testing.T) {
	testCtx := recoveryTestCtx(t)
	fakeClk := clock.NewFake(time.Date(2026, 5, 4, 23, 59, 30, 0, time.UTC))
	log := eventlog.NewMemory(clock.Real{})
	act := &recoveryActuator{}
	cfg := recoveryCfg(t, log, act, fakeClk, "max-scope")
	eng, err := orchestrator.NewCostGatingEngine(cfg)
	if err != nil {
		t.Fatalf("NewCostGatingEngine: %v", err)
	}

	if err := eng.Apply(testCtx,
		orchestrator.ThresholdRow{Pct: 90, Action: orchestrator.CostActionReduceParallelism},
		orchestrator.BudgetSnapshot{CumulativeUSD: 90, DailyCapUSD: 100, DoctrineName: "max-scope"}); err != nil {
		t.Fatalf("Apply 90%%: %v", err)
	}

	if err := eng.Apply(testCtx,
		orchestrator.ThresholdRow{Pct: 0, Action: orchestrator.CostActionContinue},
		orchestrator.BudgetSnapshot{CumulativeUSD: 5, DailyCapUSD: 100, DoctrineName: "max-scope"}); err != nil {
		t.Fatalf("Apply Continue: %v", err)
	}

	time.Sleep(20 * time.Millisecond)

	fakeClk.Advance(61 * time.Second)
	waitForCount(t, log, "sess-g6", eventlog.EvtBudgetRecovered, 1)
	time.Sleep(10 * time.Millisecond)

	fakeClk.Advance(61 * time.Second)
	waitForCount(t, log, "sess-g6", eventlog.EvtBudgetRecovered, 2)
	time.Sleep(10 * time.Millisecond)

	fakeClk.Advance(61 * time.Second)
	waitForCount(t, log, "sess-g6", eventlog.EvtBudgetRecovered, 3)
	time.Sleep(10 * time.Millisecond)

	fakeClk.Advance(61 * time.Second)
	waitForCount(t, log, "sess-g6", eventlog.EvtBudgetFullyRecovered, 1)

	if act.RestoreCalls() < 1 {
		t.Errorf("RestoreDefaults not called during recovery walk; got %d calls", act.RestoreCalls())
	}
}

func TestRecovery_CapaFirewallHoldsAtWaitingForConfirmation(t *testing.T) {
	testCtx := recoveryTestCtx(t)
	_ = testCtx
	fakeClk := clock.NewFake(time.Date(2026, 5, 4, 23, 59, 30, 0, time.UTC))
	log := eventlog.NewMemory(clock.Real{})
	act := &recoveryActuator{}
	cfg := recoveryCfg(t, log, act, fakeClk, "capa-firewall")
	eng, _ := orchestrator.NewCostGatingEngine(cfg)

	_ = eng.Apply(testCtx,
		orchestrator.ThresholdRow{Pct: 80, Action: orchestrator.CostActionWaitingForConfirmation},
		orchestrator.BudgetSnapshot{CumulativeUSD: 80, DailyCapUSD: 100, DoctrineName: "capa-firewall"})

	if err := eng.Apply(testCtx,
		orchestrator.ThresholdRow{Pct: 0, Action: orchestrator.CostActionContinue},
		orchestrator.BudgetSnapshot{CumulativeUSD: 0, DailyCapUSD: 100, DoctrineName: "capa-firewall"}); err != nil {
		t.Fatalf("Apply Continue: %v", err)
	}

	waitForCount(t, log, "sess-g6", eventlog.EvtBudgetRecoveryHeld, 1)

	fakeClk.Advance(120 * time.Second)
	time.Sleep(20 * time.Millisecond)
	if countByEventType(t, log, "sess-g6", eventlog.EvtBudgetFullyRecovered) > 0 {
		t.Fatal("capa-firewall must NOT auto-recover from WAITING_*; operator confirmation required")
	}
}

func TestRecovery_CapaFirewallHoldsAtWaiting(t *testing.T) {

	testCtx := recoveryTestCtx(t)
	_ = testCtx
	fakeClk := clock.NewFake(time.Date(2026, 5, 4, 23, 59, 30, 0, time.UTC))
	log := eventlog.NewMemory(clock.Real{})
	act := &recoveryActuator{}
	cfg := recoveryCfg(t, log, act, fakeClk, "capa-firewall")
	eng, _ := orchestrator.NewCostGatingEngine(cfg)

	_ = eng.Apply(testCtx,
		orchestrator.ThresholdRow{Pct: 90, Action: orchestrator.CostActionWaiting},
		orchestrator.BudgetSnapshot{CumulativeUSD: 90, DailyCapUSD: 100, DoctrineName: "capa-firewall"})

	_ = eng.Apply(testCtx,
		orchestrator.ThresholdRow{Pct: 0, Action: orchestrator.CostActionContinue},
		orchestrator.BudgetSnapshot{CumulativeUSD: 0, DailyCapUSD: 100, DoctrineName: "capa-firewall"})

	waitForCount(t, log, "sess-g6", eventlog.EvtBudgetRecoveryHeld, 1)

	fakeClk.Advance(120 * time.Second)
	time.Sleep(20 * time.Millisecond)
	if countByEventType(t, log, "sess-g6", eventlog.EvtBudgetFullyRecovered) > 0 {
		t.Fatal("capa-firewall must hold at WAITING")
	}
}

func TestRecovery_NoRecoveryWalkWhenAlreadyAtBaseline(t *testing.T) {

	testCtx := recoveryTestCtx(t)
	_ = testCtx
	fakeClk := clock.NewFake(time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC))
	log := eventlog.NewMemory(clock.Real{})
	act := &recoveryActuator{}
	cfg := recoveryCfg(t, log, act, fakeClk, "max-scope")
	eng, _ := orchestrator.NewCostGatingEngine(cfg)

	if err := eng.Apply(testCtx,
		orchestrator.ThresholdRow{Pct: 0, Action: orchestrator.CostActionContinue},
		orchestrator.BudgetSnapshot{CumulativeUSD: 5, DailyCapUSD: 100, DoctrineName: "max-scope"}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	fakeClk.Advance(180 * time.Second)
	time.Sleep(20 * time.Millisecond)
	if countByEventType(t, log, "sess-g6", eventlog.EvtBudgetRecovered) > 0 {
		t.Error("BudgetRecovered emitted from baseline (no degradation)")
	}
	if countByEventType(t, log, "sess-g6", eventlog.EvtBudgetFullyRecovered) > 0 {
		t.Error("BudgetFullyRecovered emitted from baseline (no degradation)")
	}
}

func TestRecovery_EventPayloadAttribution(t *testing.T) {

	testCtx := recoveryTestCtx(t)
	fakeClk := clock.NewFake(time.Date(2026, 5, 4, 23, 59, 30, 0, time.UTC))
	log := eventlog.NewMemory(clock.Real{})
	act := &recoveryActuator{}
	cfg := recoveryCfg(t, log, act, fakeClk, "max-scope")
	eng, _ := orchestrator.NewCostGatingEngine(cfg)

	_ = eng.Apply(testCtx,
		orchestrator.ThresholdRow{Pct: 90, Action: orchestrator.CostActionReduceParallelism},
		orchestrator.BudgetSnapshot{CumulativeUSD: 90, DailyCapUSD: 100, DoctrineName: "max-scope", ProjectID: "internal-platform-x"})

	_ = eng.Apply(testCtx,
		orchestrator.ThresholdRow{Pct: 0, Action: orchestrator.CostActionContinue},
		orchestrator.BudgetSnapshot{CumulativeUSD: 5, DailyCapUSD: 100, DoctrineName: "max-scope", ProjectID: "internal-platform-x"})
	time.Sleep(20 * time.Millisecond)

	fakeClk.Advance(61 * time.Second)
	waitForCount(t, log, "sess-g6", eventlog.EvtBudgetRecovered, 1)

	records, _ := log.Query(context.Background(), "sess-g6", 0)
	var first *eventlog.BudgetRecovered
	for _, rec := range records {
		if rec.EventType != eventlog.EvtBudgetRecovered {
			continue
		}
		dec, _ := eventlog.Decode(rec.EventType, rec.Payload)
		v := dec.(eventlog.BudgetRecovered)
		first = &v
		break
	}
	if first == nil {
		t.Fatal("no BudgetRecovered emitted")
	}
	if first.UndoneAction != "reduce_parallelism" {
		t.Errorf("UndoneAction = %q, want reduce_parallelism", first.UndoneAction)
	}
	if first.NextAction != "tier_degrade_l2" {
		t.Errorf("NextAction = %q, want tier_degrade_l2 (next-lower from 90 = 80)", first.NextAction)
	}
	if first.NextPct != 80 {
		t.Errorf("NextPct = %d, want 80", first.NextPct)
	}
	if first.Doctrine != "max-scope" {
		t.Errorf("Doctrine = %q, want max-scope", first.Doctrine)
	}
	if first.ProjectID != "internal-platform-x" {
		t.Errorf("ProjectID = %q, want internal-platform-x", first.ProjectID)
	}
}

func TestRecovery_ConcurrentTriggers_SingleWalkOnly(t *testing.T) {

	testCtx := recoveryTestCtx(t)
	fakeClk := clock.NewFake(time.Date(2026, 5, 4, 23, 59, 30, 0, time.UTC))
	log := eventlog.NewMemory(clock.Real{})
	act := &recoveryActuator{}
	cfg := recoveryCfg(t, log, act, fakeClk, "max-scope")
	eng, _ := orchestrator.NewCostGatingEngine(cfg)

	_ = eng.Apply(testCtx,
		orchestrator.ThresholdRow{Pct: 90, Action: orchestrator.CostActionReduceParallelism},
		orchestrator.BudgetSnapshot{CumulativeUSD: 90, DailyCapUSD: 100, DoctrineName: "max-scope"})

	_ = eng.Apply(testCtx,
		orchestrator.ThresholdRow{Pct: 0, Action: orchestrator.CostActionContinue},
		orchestrator.BudgetSnapshot{CumulativeUSD: 5, DailyCapUSD: 100, DoctrineName: "max-scope"})
	_ = eng.Apply(testCtx,
		orchestrator.ThresholdRow{Pct: 0, Action: orchestrator.CostActionContinue},
		orchestrator.BudgetSnapshot{CumulativeUSD: 5, DailyCapUSD: 100, DoctrineName: "max-scope"})
	time.Sleep(20 * time.Millisecond)

	for i := 0; i < 5; i++ {
		fakeClk.Advance(61 * time.Second)
		time.Sleep(20 * time.Millisecond)
	}
	waitForCount(t, log, "sess-g6", eventlog.EvtBudgetFullyRecovered, 1)

	if got := countByEventType(t, log, "sess-g6", eventlog.EvtBudgetFullyRecovered); got != 1 {
		t.Errorf("BudgetFullyRecovered count = %d, want 1 (single recovery walk)", got)
	}
}
