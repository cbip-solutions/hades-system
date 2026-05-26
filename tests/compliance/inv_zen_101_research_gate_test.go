// tests/compliance/inv_zen_101_research_gate_test.go
//
// inv-zen-101 — Research gate: depth/width decisions require a prior
// ResearchCompleted event in the active session.
//
// Compliance witness: end-to-end validation through orchestrator.RunStage4
// public surface. Two cases:
//
//  1. Without prior ResearchCompleted: RunStage4 must return
//     ErrResearchGateNotPassed, state must revert to StateIdle,
//     EvtOrchestratorStopped{outcome:research_gate_failed} must be the
//     last event, and Dispatcher.Dispatch must never be invoked.
//
//  2. With ResearchCompleted pre-seeded: RunStage4 must proceed,
//     Dispatcher.Dispatch must fire exactly once.
//
// This file is the canonical enrollment record for make verify-invariants.
// If this test is deleted or its assertions weakened without a
// corresponding ADR, the verify-inv-zen-101 Makefile target will fail
// the static file-presence check.
//
// Adaptations from plan template (canonical Phase A/B/C truth):
//   - No statemachine subpackage: bare *orchestrator.StateMachine via
//     orchestrator.NewStateMachine (no statemachine.New(initial)).
//   - No worktreepool.NewFake: fakePool defined locally (same shape as
//     the orchestrator_test.go fakePool but compliance-package-local).
//   - eventlog.NewMemory returns *eventlog.Log (concrete); satisfies
//     both eventlog.Appender and eventlogQuerier internally.
//   - Query is 3-arg: Query(ctx, sessionID, since int64).
//   - Returned slice is []eventlog.Record with EventType field (not Type).
//   - eventlog.Decode returns value type (not pointer); type-switch uses
//     eventlog.OrchestratorStopped (value form, not pointer).
//
// If this test fails, the research gate has been disabled or bypassed.
// Do NOT update this file to match new behaviour — surface to operator.
// Either the spec must be amended (ADR-track) or the code must be
// reverted. See spec §6.3 inv-zen-101, ADR-0006.
package compliance_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/worktreepool"
)

type noopDispatcher struct {
	called int
}

func (d *noopDispatcher) Dispatch(_ context.Context, req orchestrator.DispatchRequest) (orchestrator.DispatchResult, error) {
	d.called++
	return orchestrator.DispatchResult{
		WorkersSpawned: req.Width,
		Completed:      req.Width,
	}, nil
}

func (d *noopDispatcher) Shutdown(_ context.Context) error { return nil }

type compFakePool struct{}

func (p *compFakePool) Lease(_ context.Context) (*worktreepool.Worktree, error) {
	return &worktreepool.Worktree{}, nil
}

func (p *compFakePool) Release(_ context.Context, _ *worktreepool.Worktree) error {
	return nil
}

func (p *compFakePool) PruneOrphans(_ context.Context) (worktreepool.PruneReport, error) {
	return worktreepool.PruneReport{}, nil
}

func (p *compFakePool) Close(_ context.Context) error { return nil }

type compFakeSpec struct {
	phases, tasks, parallel int
}

func (s *compFakeSpec) Phases() int                   { return s.phases }
func (s *compFakeSpec) TaskCount() int                { return s.tasks }
func (s *compFakeSpec) ParallelizableUpperBound() int { return s.parallel }
func (s *compFakeSpec) DependencyDAG() any            { return nil }

func newOrchestratorForCompliance(t *testing.T, sessionID string) (*orchestrator.Orchestrator, *eventlog.Log, *noopDispatcher) {
	t.Helper()

	fc := clock.NewFake(time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC))
	memLog := eventlog.NewMemory(fc)
	sm := orchestrator.NewStateMachine(memLog, fc, sessionID, "demo")
	pool := &compFakePool{}
	disp := &noopDispatcher{}

	gate, err := orchestrator.NewResearchGate(memLog)
	if err != nil {
		t.Fatalf("NewResearchGate: %v", err)
	}

	orch, err := orchestrator.New(orchestrator.Config{
		Clock:        fc,
		EventLog:     memLog,
		StateMachine: sm,
		Pool:         pool,
		Dispatcher:   disp,
		Research:     gate,
		PoolCapacity: 8,
		SessionID:    sessionID,
		ProjectID:    "demo",
	})
	if err != nil {
		t.Fatalf("orchestrator.New: %v", err)
	}
	if err := orch.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return orch, memLog, disp
}

func TestInvZen101_RunStage4WithoutResearchRefused(t *testing.T) {
	const sessionID = "sess-inv101-fail"
	orch, memLog, disp := newOrchestratorForCompliance(t, sessionID)

	req := orchestrator.BuildRequest{
		SessionID: sessionID,
		ProjectID: "demo",
		Spec:      &compFakeSpec{phases: 2, tasks: 8, parallel: 8},
		Doctrine:  "max-scope",
	}

	err := orch.RunStage4(context.Background(), req)
	if !errors.Is(err, orchestrator.ErrResearchGateNotPassed) {
		t.Fatalf("inv-zen-101 violated: want ErrResearchGateNotPassed, got %v", err)
	}

	if got := orch.State(); got != orchestrator.StateIdle {
		t.Fatalf("inv-zen-101: post-failure state = %v, want StateIdle", got)
	}

	if disp.called != 0 {
		t.Fatalf("inv-zen-101 violated: Dispatcher.Dispatch invoked %d times despite gate failure", disp.called)
	}

	records, qErr := memLog.Query(context.Background(), sessionID, 0)
	if qErr != nil {
		t.Fatalf("Query: %v", qErr)
	}
	if len(records) == 0 {
		t.Fatalf("inv-zen-101: eventlog empty for session %s", sessionID)
	}

	var stoppedRec *eventlog.Record
	for i := range records {
		if records[i].EventType == eventlog.EvtOrchestratorStopped {
			stoppedRec = &records[i]
			break
		}
	}
	if stoppedRec == nil {
		t.Fatalf("inv-zen-101: no OrchestratorStopped event found in session %s", sessionID)
	}

	decoded, decErr := eventlog.Decode(stoppedRec.EventType, stoppedRec.Payload)
	if decErr != nil {

		var m map[string]any
		if uerr := json.Unmarshal(stoppedRec.Payload, &m); uerr != nil {
			t.Fatalf("Decode (%v) + Unmarshal failed: %v", decErr, uerr)
		}
		outcome, _ := m["outcome"].(string)
		if outcome != "research_gate_failed" {
			t.Fatalf("outcome = %q, want research_gate_failed", outcome)
		}
	} else {

		stopped, ok := decoded.(eventlog.OrchestratorStopped)
		if !ok {
			t.Fatalf("decoded type = %T, want eventlog.OrchestratorStopped", decoded)
		}
		if stopped.Outcome != "research_gate_failed" {
			t.Fatalf("decoded outcome = %q, want research_gate_failed", stopped.Outcome)
		}
	}

	for _, rec := range records {
		if rec.EventType == eventlog.EvtWorkerDispatched {
			t.Fatalf("inv-zen-101 violated: WorkerDispatched emitted before gate cleared")
		}
	}
}

func TestInvZen101_RunStage4WithResearchProceeds(t *testing.T) {
	const sessionID = "sess-inv101-pass"
	orch, memLog, disp := newOrchestratorForCompliance(t, sessionID)

	_, seedErr := memLog.Append(context.Background(), eventlog.Event{
		Type:      eventlog.EvtResearchCompleted,
		SessionID: sessionID,
		ProjectID: "demo",
		Timestamp: time.Date(2026, 4, 30, 11, 59, 0, 0, time.UTC),
		Payload: map[string]any{
			"findings_summary": "compliance-test seed",
			"cost_usd":         0.0,
		},
	})
	if seedErr != nil {
		t.Fatalf("seed ResearchCompleted: %v", seedErr)
	}

	req := orchestrator.BuildRequest{
		SessionID: sessionID,
		ProjectID: "demo",
		Spec:      &compFakeSpec{phases: 1, tasks: 1, parallel: 1},
		Doctrine:  "max-scope",
	}
	if runErr := orch.RunStage4(context.Background(), req); runErr != nil {
		t.Fatalf("RunStage4 with research: %v", runErr)
	}

	if disp.called != 1 {
		t.Fatalf("inv-zen-101: Dispatch must run exactly once when gate passed; called=%d", disp.called)
	}
}
