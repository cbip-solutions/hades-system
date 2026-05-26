package orchestrator_test

import (
	"context"
	"errors"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/orchestrator"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

type costG3FakeActuator struct {
	dropAtDepthCalls     int
	setTierCalls         int
	setParallelismCalls  int
	hardPauseCalls       int
	emergencyOnlyTier    int
	escalateL4Calls      int
	waitForConfirmCalls  int
	waitingCalls         int
	restoreDefaultsCalls int

	dropAtDepthArg     int
	setTierArg         int
	setParallelismArgs struct{ depth, width int }
	hardPauseReason    string
	escalateL4Payload  map[string]any
	waitForConfirmID   string
	waitingReason      string

	dropAtDepthErr     error
	setTierErr         error
	setParallelismErr  error
	hardPauseErr       error
	emergencyTierErr   error
	escalateL4Err      error
	waitForConfirmErr  error
	waitingErr         error
	restoreDefaultsErr error
}

func (f *costG3FakeActuator) DropAtDepth(ctx context.Context, layer int) error {
	f.dropAtDepthCalls++
	f.dropAtDepthArg = layer
	return f.dropAtDepthErr
}

func (f *costG3FakeActuator) SetTier(ctx context.Context, maxTier int) error {
	f.setTierCalls++
	f.setTierArg = maxTier
	return f.setTierErr
}

func (f *costG3FakeActuator) SetParallelism(ctx context.Context, depthCap int, widthCap int) error {
	f.setParallelismCalls++
	f.setParallelismArgs.depth = depthCap
	f.setParallelismArgs.width = widthCap
	return f.setParallelismErr
}

func (f *costG3FakeActuator) HardPause(ctx context.Context, reason string) error {
	f.hardPauseCalls++
	f.hardPauseReason = reason
	return f.hardPauseErr
}

func (f *costG3FakeActuator) EmergencyOnlyTier(ctx context.Context) error {
	f.emergencyOnlyTier++
	return f.emergencyTierErr
}

func (f *costG3FakeActuator) EscalateL4(ctx context.Context, payload map[string]any) error {
	f.escalateL4Calls++
	f.escalateL4Payload = payload
	return f.escalateL4Err
}

func (f *costG3FakeActuator) WaitForConfirmation(ctx context.Context, decisionID string) error {
	f.waitForConfirmCalls++
	f.waitForConfirmID = decisionID
	return f.waitForConfirmErr
}

func (f *costG3FakeActuator) Waiting(ctx context.Context, reason string) error {
	f.waitingCalls++
	f.waitingReason = reason
	return f.waitingErr
}

func (f *costG3FakeActuator) RestoreDefaults(ctx context.Context) error {
	f.restoreDefaultsCalls++
	return f.restoreDefaultsErr
}

func TestApplyAction_Continue_CallsRestoreDefaults(t *testing.T) {
	act := &costG3FakeActuator{}
	if err := orchestrator.ApplyAction(context.Background(), act, orchestrator.CostActionContinue, nil); err != nil {
		t.Fatalf("ApplyAction: %v", err)
	}
	if act.restoreDefaultsCalls != 1 {
		t.Errorf("RestoreDefaults call count: got %d want 1", act.restoreDefaultsCalls)
	}
}

func TestApplyAction_DropL3Strategic_CallsDropAtDepth3(t *testing.T) {
	act := &costG3FakeActuator{}
	if err := orchestrator.ApplyAction(context.Background(), act, orchestrator.CostActionDropL3Strategic, nil); err != nil {
		t.Fatalf("ApplyAction: %v", err)
	}
	if act.dropAtDepthCalls != 1 {
		t.Errorf("DropAtDepth call count: got %d want 1", act.dropAtDepthCalls)
	}
	if act.dropAtDepthArg != 3 {
		t.Errorf("DropAtDepth arg: got %d want 3", act.dropAtDepthArg)
	}
}

func TestApplyAction_TierDegradeL2_CallsSetTier2(t *testing.T) {
	act := &costG3FakeActuator{}
	if err := orchestrator.ApplyAction(context.Background(), act, orchestrator.CostActionTierDegradeL2, nil); err != nil {
		t.Fatalf("ApplyAction: %v", err)
	}
	if act.setTierCalls != 1 {
		t.Errorf("SetTier call count: got %d want 1", act.setTierCalls)
	}
	if act.setTierArg != 2 {
		t.Errorf("SetTier arg: got %d want 2", act.setTierArg)
	}
}

func TestApplyAction_TierDegradeL1L2_CallsSetTier1(t *testing.T) {
	act := &costG3FakeActuator{}
	if err := orchestrator.ApplyAction(context.Background(), act, orchestrator.CostActionTierDegradeL1L2, nil); err != nil {
		t.Fatalf("ApplyAction: %v", err)
	}
	if act.setTierCalls != 1 {
		t.Errorf("SetTier call count: got %d want 1", act.setTierCalls)
	}
	if act.setTierArg != 1 {
		t.Errorf("SetTier arg: got %d want 1", act.setTierArg)
	}
}

func TestApplyAction_ReduceParallelism_CallsSetParallelism(t *testing.T) {
	act := &costG3FakeActuator{}
	if err := orchestrator.ApplyAction(context.Background(), act, orchestrator.CostActionReduceParallelism, nil); err != nil {
		t.Fatalf("ApplyAction: %v", err)
	}
	if act.setParallelismCalls != 1 {
		t.Errorf("SetParallelism call count: got %d want 1", act.setParallelismCalls)
	}
	if act.setParallelismArgs.depth != 1 || act.setParallelismArgs.width != 2 {
		t.Errorf("SetParallelism args: got {%d,%d} want {1,2}",
			act.setParallelismArgs.depth, act.setParallelismArgs.width)
	}
}

func TestApplyAction_HardPause_CallsHardPause(t *testing.T) {
	act := &costG3FakeActuator{}
	ctx := &orchestrator.CostActionContext{Reason: "cap-100"}
	if err := orchestrator.ApplyAction(context.Background(), act, orchestrator.CostActionHardPause, ctx); err != nil {
		t.Fatalf("ApplyAction: %v", err)
	}
	if act.hardPauseCalls != 1 {
		t.Errorf("HardPause call count: got %d want 1", act.hardPauseCalls)
	}
	if act.hardPauseReason != "cap-100" {
		t.Errorf("HardPause reason: got %q want cap-100", act.hardPauseReason)
	}
}

func TestApplyAction_EmergencyOnlyTier_CallsEmergencyOnlyTier(t *testing.T) {
	act := &costG3FakeActuator{}
	if err := orchestrator.ApplyAction(context.Background(), act, orchestrator.CostActionEmergencyOnlyTier, nil); err != nil {
		t.Fatalf("ApplyAction: %v", err)
	}
	if act.emergencyOnlyTier != 1 {
		t.Errorf("EmergencyOnlyTier call count: got %d want 1", act.emergencyOnlyTier)
	}
}

func TestApplyAction_EscalateL4_CallsEscalateL4WithPayload(t *testing.T) {
	act := &costG3FakeActuator{}
	ctx := &orchestrator.CostActionContext{Reason: "payg-escalation"}
	snap := orchestrator.BudgetSnapshot{CumulativeUSD: 150, DailyCapUSD: 100}
	ctx.Snapshot = snap
	if err := orchestrator.ApplyAction(context.Background(), act, orchestrator.CostActionEscalateL4, ctx); err != nil {
		t.Fatalf("ApplyAction: %v", err)
	}
	if act.escalateL4Calls != 1 {
		t.Errorf("EscalateL4 call count: got %d want 1", act.escalateL4Calls)
	}
	if act.escalateL4Payload == nil {
		t.Fatal("EscalateL4 payload: got nil")
	}
	if reason, ok := act.escalateL4Payload["reason"].(string); !ok || reason != "payg-escalation" {
		t.Errorf("EscalateL4 payload reason: got %v want payg-escalation", act.escalateL4Payload["reason"])
	}
}

func TestApplyAction_WaitingForConfirmation_CallsWaitForConfirmation(t *testing.T) {
	act := &costG3FakeActuator{}
	ctx := &orchestrator.CostActionContext{DecisionID: "dec-12345"}
	if err := orchestrator.ApplyAction(context.Background(), act, orchestrator.CostActionWaitingForConfirmation, ctx); err != nil {
		t.Fatalf("ApplyAction: %v", err)
	}
	if act.waitForConfirmCalls != 1 {
		t.Errorf("WaitForConfirmation call count: got %d want 1", act.waitForConfirmCalls)
	}
	if act.waitForConfirmID != "dec-12345" {
		t.Errorf("WaitForConfirmation id: got %q want dec-12345", act.waitForConfirmID)
	}
}

func TestApplyAction_Waiting_CallsWaiting(t *testing.T) {
	act := &costG3FakeActuator{}
	ctx := &orchestrator.CostActionContext{Reason: "waiting-for-capacity"}
	if err := orchestrator.ApplyAction(context.Background(), act, orchestrator.CostActionWaiting, ctx); err != nil {
		t.Fatalf("ApplyAction: %v", err)
	}
	if act.waitingCalls != 1 {
		t.Errorf("Waiting call count: got %d want 1", act.waitingCalls)
	}
	if act.waitingReason != "waiting-for-capacity" {
		t.Errorf("Waiting reason: got %q want waiting-for-capacity", act.waitingReason)
	}
}

func TestApplyAction_NilContext_DefaultsApplied(t *testing.T) {
	act := &costG3FakeActuator{}

	if err := orchestrator.ApplyAction(context.Background(), act, orchestrator.CostActionHardPause, nil); err != nil {
		t.Fatalf("ApplyAction with nil context: %v", err)
	}
	if act.hardPauseCalls != 1 {
		t.Errorf("HardPause not called")
	}
	if act.hardPauseReason != "" {
		t.Errorf("HardPause reason with nil context: got %q want empty", act.hardPauseReason)
	}
}

func TestApplyAction_UnknownAction_ReturnsError(t *testing.T) {
	act := &costG3FakeActuator{}
	err := orchestrator.ApplyAction(context.Background(), act, orchestrator.CostAction("unknown_action"), nil)
	if err == nil {
		t.Fatal("ApplyAction with unknown action: want error, got nil")
	}

	if act.dropAtDepthCalls+act.setTierCalls+act.setParallelismCalls+
		act.hardPauseCalls+act.emergencyOnlyTier+act.escalateL4Calls+
		act.waitForConfirmCalls+act.waitingCalls+act.restoreDefaultsCalls > 0 {
		t.Error("unknown action: actuator method was called despite error")
	}
}

func TestApplyAction_ActuatorError_Propagates(t *testing.T) {
	act := &costG3FakeActuator{}
	act.hardPauseErr = errors.New("actuator: pause failed")
	err := orchestrator.ApplyAction(context.Background(), act, orchestrator.CostActionHardPause, nil)
	if err == nil {
		t.Fatal("ApplyAction: want error from actuator, got nil")
	}
	if !errors.Is(err, act.hardPauseErr) {
		t.Errorf("ApplyAction error: got %v want %v", err, act.hardPauseErr)
	}
}

func TestEngine_Apply_RecordsCurrentRowOnSuccess(t *testing.T) {
	snap := &costFakeSnapshotReader{}
	snap.set(orchestrator.BudgetSnapshot{CumulativeUSD: 60, DailyCapUSD: 100})
	log := eventlog.NewMemory(clock.Real{})
	cfg := g2Cfg(t, snap, log, clock.Real{}, "max-scope")
	cfg.Actuator = &costG3FakeActuator{}
	eng, _ := orchestrator.NewCostGatingEngine(cfg)

	row := orchestrator.ThresholdRow{Pct: 60, Action: orchestrator.CostActionDropL3Strategic}
	if err := eng.Apply(context.Background(), row, snap.snap); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	stored, ok := orchestrator.GetCurrentRowForTest(eng)
	if !ok {
		t.Fatal("Apply should record currentRow on success")
	}
	if stored.Pct != 60 || stored.Action != orchestrator.CostActionDropL3Strategic {
		t.Errorf("Apply recorded: got %+v want {Pct:60 Action:drop_l3_strategic}", stored)
	}
}

func TestEngine_Apply_SkipsCurrentRowOnError(t *testing.T) {
	snap := &costFakeSnapshotReader{}
	snap.set(orchestrator.BudgetSnapshot{CumulativeUSD: 60, DailyCapUSD: 100})
	log := eventlog.NewMemory(clock.Real{})
	cfg := g2Cfg(t, snap, log, clock.Real{}, "max-scope")

	act := &costG3FakeActuator{}
	act.dropAtDepthErr = errors.New("orchestrator: drop failed")
	cfg.Actuator = act
	eng, _ := orchestrator.NewCostGatingEngine(cfg)

	row := orchestrator.ThresholdRow{Pct: 60, Action: orchestrator.CostActionDropL3Strategic}
	err := eng.Apply(context.Background(), row, snap.snap)
	if err == nil {
		t.Fatal("Apply should propagate actuator error")
	}

	_, ok := orchestrator.GetCurrentRowForTest(eng)
	if ok {
		t.Fatal("Apply should not update currentRow on error")
	}
}
