package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

func orchForUnwindTest(t *testing.T) *Orchestrator {
	t.Helper()
	clk := clock.NewFake(time.Date(2026, time.April, 30, 12, 0, 0, 0, time.UTC))
	log := eventlog.NewMemory(clk)
	sm := NewStateMachine(log, clk, "sess-unwind", "proj-unwind")
	o, err := New(Config{
		Clock:        clk,
		EventLog:     log,
		StateMachine: sm,
		Pool:         memoryPool{},
		Dispatcher:   internalOrchDispatcher{},
		Research:     internalOrchGate{},
		SessionID:    "sess-unwind",
		ProjectID:    "proj-unwind",
		PoolCapacity: 2,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return o
}

func driveToState(t *testing.T, o *Orchestrator, path ...State) {
	t.Helper()
	for _, to := range path {
		if err := o.cfg.StateMachine.Transition(context.Background(), to, "test-drive"); err != nil {
			t.Fatalf("driveToState: Transition(%v): %v", to, err)
		}
	}
}

func TestForceUnwindToIdle_AlreadyIdle(t *testing.T) {
	o := orchForUnwindTest(t)

	if got := o.cfg.StateMachine.Current(); got != StateIdle {
		t.Fatalf("expected StateIdle at start, got %v", got)
	}

	o.forceUnwindToIdle(context.Background(), "noop-test")
	if got := o.cfg.StateMachine.Current(); got != StateIdle {
		t.Fatalf("SM should remain Idle, got %v", got)
	}
}

func TestForceUnwindToIdle_FromAborting(t *testing.T) {
	o := orchForUnwindTest(t)

	driveToState(t, o, StateInitializing, StateAborting)
	if got := o.cfg.StateMachine.Current(); got != StateAborting {
		t.Fatalf("expected StateAborting, got %v", got)
	}

	o.forceUnwindToIdle(context.Background(), "aborting-test")
	if got := o.cfg.StateMachine.Current(); got != StateIdle {
		t.Fatalf("expected StateIdle after forceUnwindToIdle, got %v", got)
	}
}

func TestForceUnwindToIdle_FromDegradedTier(t *testing.T) {
	o := orchForUnwindTest(t)

	driveToState(t, o, StateInitializing, StateRunning, StateDegradedTier)
	if got := o.cfg.StateMachine.Current(); got != StateDegradedTier {
		t.Fatalf("expected StateDegradedTier, got %v", got)
	}

	o.forceUnwindToIdle(context.Background(), "degraded-test")
	if got := o.cfg.StateMachine.Current(); got != StateIdle {
		t.Fatalf("expected StateIdle after forceUnwindToIdle from DegradedTier, got %v", got)
	}
}
