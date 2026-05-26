// Copyright 2026 zen-swarm contributors. SPDX-License-Identifier: MIT
//
// tests/compliance/inv_zen_092_cost_atomicity_test.go
//
// compliance test. End-to-end runtime certification of the spec §1 Q9 D
// guarantee that NO state-mutating cost-gating action fires while a worker
// holds an open atomic unit (commit boundary not yet signaled).
//
// Three assertions:
//   1. Zero-degradation-during-atomic-unit: with WorkerSet never signaling
//      and clock advanced under the atomicity_timeout, NO state-mutating
//      actuator method may fire (only RestoreDefaults is permitted —
//      that's the recovery / Continue path which deliberately bypasses
//      the guard per G-5).
//   2. Timeout-deterministic: advancing the fake clock by exactly the
//      doctrine atomicity_timeout (default 30s) causes the
//      EvtCostGatingAtomicityTimeout warn event to fire and Apply to
//      proceed (warn-and-proceed pattern).
//   3. Post-commit-applies: when WorkerSet.SignalAtomicBoundary fires,
//      the queued Apply unblocks and the actuator method is invoked.
//
// Default build tags (no //go:build compliance) — make verify-invariants
// runs these alongside inv-zen-101/093/099 per the established pattern.

package compliance_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

type cgComplianceActuator struct {
	mu             sync.Mutex
	mutationCalls  int
	setParallelism int
	restoreCalls   int
}

func (a *cgComplianceActuator) DropAtDepth(_ context.Context, _ int) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.mutationCalls++
	return nil
}
func (a *cgComplianceActuator) SetTier(_ context.Context, _ int) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.mutationCalls++
	return nil
}
func (a *cgComplianceActuator) SetParallelism(_ context.Context, _, _ int) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.mutationCalls++
	a.setParallelism++
	return nil
}
func (a *cgComplianceActuator) HardPause(_ context.Context, _ string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.mutationCalls++
	return nil
}
func (a *cgComplianceActuator) EmergencyOnlyTier(_ context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.mutationCalls++
	return nil
}
func (a *cgComplianceActuator) EscalateL4(_ context.Context, _ map[string]any) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.mutationCalls++
	return nil
}
func (a *cgComplianceActuator) WaitForConfirmation(_ context.Context, _ string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.mutationCalls++
	return nil
}
func (a *cgComplianceActuator) Waiting(_ context.Context, _ string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.mutationCalls++
	return nil
}
func (a *cgComplianceActuator) RestoreDefaults(_ context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.restoreCalls++
	return nil
}

func (a *cgComplianceActuator) MutationCalls() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.mutationCalls
}
func (a *cgComplianceActuator) SetParallelismCalls() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.setParallelism
}

type cgComplianceWorkerSet struct {
	mu       sync.Mutex
	boundary chan struct{}
}

func newCGComplianceWorkerSet() *cgComplianceWorkerSet {
	return &cgComplianceWorkerSet{boundary: make(chan struct{})}
}

func (w *cgComplianceWorkerSet) WaitAtomicBoundary(_ context.Context) <-chan struct{} {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.boundary
}

func (w *cgComplianceWorkerSet) SignalAtomicBoundary() {
	w.mu.Lock()
	defer w.mu.Unlock()
	select {
	case <-w.boundary:
	default:
		close(w.boundary)
	}
}

type cgComplianceBudget struct {
	mu   sync.Mutex
	snap orchestrator.BudgetSnapshot
}

func (b *cgComplianceBudget) Snapshot(_ context.Context) (orchestrator.BudgetSnapshot, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.snap, nil
}

func (b *cgComplianceBudget) Set(s orchestrator.BudgetSnapshot) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.snap = s
}

func cgComplianceCfg(t *testing.T, fakeClk *clock.Fake, log *eventlog.Log, act orchestrator.OrchestratorActuator, ws orchestrator.WorkerSet, bud orchestrator.BudgetSnapshotReader) orchestrator.CostGatingEngineConfig {
	t.Helper()
	prof, err := orchestrator.BuiltinCostProfile("max-scope")
	if err != nil {
		t.Fatalf("BuiltinCostProfile: %v", err)
	}
	return orchestrator.CostGatingEngineConfig{
		Clock:     fakeClk,
		EventLog:  log,
		Budget:    bud,
		Workers:   ws,
		Actuator:  act,
		Profile:   prof,
		Override:  nil,
		PollEvery: 100 * time.Millisecond,
		SessionID: "sess-inv092",
		ProjectID: "proj-inv092",
	}
}

func hasEvent(t *testing.T, log *eventlog.Log, et eventlog.EventType) bool {
	t.Helper()
	records, err := log.Query(context.Background(), "sess-inv092", 0)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	for _, rec := range records {
		if rec.EventType == et {
			return true
		}
	}
	return false
}

// TestInvZen092_NoDegradationDuringAtomicUnit asserts that while a worker
// holds an open atomic unit (boundary never signals) and the clock has
// not yet crossed the atomicity_timeout, the engine MUST NOT fire any
// state-mutating actuator method.
func TestInvZen092_NoDegradationDuringAtomicUnit(t *testing.T) {
	fakeClk := clock.NewFake(time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC))
	log := eventlog.NewMemory(clock.Real{})
	act := &cgComplianceActuator{}
	ws := newCGComplianceWorkerSet()
	bud := &cgComplianceBudget{snap: orchestrator.BudgetSnapshot{
		CumulativeUSD: 90, DailyCapUSD: 100, DoctrineName: "max-scope",
	}}
	eng, err := orchestrator.NewCostGatingEngine(cgComplianceCfg(t, fakeClk, log, act, ws, bud))
	if err != nil {
		t.Fatalf("NewCostGatingEngine: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- eng.Apply(context.Background(),
			orchestrator.ThresholdRow{Pct: 90, Action: orchestrator.CostActionReduceParallelism},
			bud.snap)
	}()

	time.Sleep(20 * time.Millisecond)
	fakeClk.Advance(29 * time.Second)

	if act.MutationCalls() != 0 {
		t.Fatalf("inv-zen-092 violated: %d state-mutating actuator calls during open atomic unit (cumulative ≤30s)", act.MutationCalls())
	}

	ws.SignalAtomicBoundary()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Apply did not unblock after SignalAtomicBoundary")
	}
}

func TestInvZen092_TimeoutDeterministic(t *testing.T) {
	fakeClk := clock.NewFake(time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC))
	log := eventlog.NewMemory(clock.Real{})
	act := &cgComplianceActuator{}
	ws := newCGComplianceWorkerSet()
	bud := &cgComplianceBudget{snap: orchestrator.BudgetSnapshot{
		CumulativeUSD: 90, DailyCapUSD: 100, DoctrineName: "max-scope",
	}}
	eng, _ := orchestrator.NewCostGatingEngine(cgComplianceCfg(t, fakeClk, log, act, ws, bud))

	done := make(chan error, 1)
	go func() {
		done <- eng.Apply(context.Background(),
			orchestrator.ThresholdRow{Pct: 90, Action: orchestrator.CostActionReduceParallelism},
			bud.snap)
	}()
	time.Sleep(20 * time.Millisecond)

	fakeClk.Advance(29 * time.Second)
	if hasEvent(t, log, eventlog.EvtCostGatingAtomicityTimeout) {
		t.Fatal("inv-zen-092 violated: warn fired before 30s timeout boundary")
	}

	fakeClk.Advance(2 * time.Second)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Apply timeout path returned error (want nil for warn-and-proceed): %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Apply did not return after timeout boundary crossed")
	}
	if !hasEvent(t, log, eventlog.EvtCostGatingAtomicityTimeout) {
		t.Fatal("inv-zen-092 violated: warn not emitted at 30s timeout boundary")
	}
	// Warn-and-proceed: action MUST have applied.
	if act.SetParallelismCalls() != 1 {
		t.Fatalf("warn-and-proceed: expected 1 SetParallelism call after timeout; got %d", act.SetParallelismCalls())
	}
}

func TestInvZen092_PostCommitAppliesOnNextTick(t *testing.T) {
	fakeClk := clock.NewFake(time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC))
	log := eventlog.NewMemory(clock.Real{})
	act := &cgComplianceActuator{}
	ws := newCGComplianceWorkerSet()
	bud := &cgComplianceBudget{snap: orchestrator.BudgetSnapshot{
		CumulativeUSD: 90, DailyCapUSD: 100, DoctrineName: "max-scope",
	}}
	eng, _ := orchestrator.NewCostGatingEngine(cgComplianceCfg(t, fakeClk, log, act, ws, bud))

	done := make(chan error, 1)
	go func() {
		done <- eng.Apply(context.Background(),
			orchestrator.ThresholdRow{Pct: 90, Action: orchestrator.CostActionReduceParallelism},
			bud.snap)
	}()
	time.Sleep(20 * time.Millisecond)

	fakeClk.Advance(5 * time.Second)
	if act.MutationCalls() != 0 {
		t.Fatalf("inv-zen-092 violated: action applied before commit boundary signal (calls=%d)", act.MutationCalls())
	}

	ws.SignalAtomicBoundary()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Apply post-commit: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Apply did not unblock after SignalAtomicBoundary")
	}
	if act.SetParallelismCalls() != 1 {
		t.Fatalf("expected 1 SetParallelism call post-commit; got %d", act.SetParallelismCalls())
	}

	if hasEvent(t, log, eventlog.EvtCostGatingAtomicityTimeout) {
		t.Fatal("warn fired despite boundary signal before timeout")
	}
}
