package orchestrator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

type internalAppender struct {
	events []eventlog.Event
}

func (a *internalAppender) Append(_ context.Context, e eventlog.Event) (int64, error) {
	a.events = append(a.events, e)
	return int64(len(a.events)), nil
}

func TestTransition_FromRecoveringRejectedRuntime(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, time.April, 30, 0, 0, 0, 0, time.UTC))
	app := &internalAppender{}
	sm := NewStateMachine(app, clk, "s", "p")

	sm.current = StateRecoveringFromReplay

	err := sm.Transition(context.Background(), StateIdle, "skip-recovery")
	if !errors.Is(err, ErrIllegalTransition) {
		t.Fatalf("err=%v, want ErrIllegalTransition", err)
	}
	if sm.current != StateRecoveringFromReplay {
		t.Errorf("current mutated on illegal transition: %v", sm.current)
	}
	if len(app.events) != 0 {
		t.Errorf("events emitted on illegal transition: %d", len(app.events))
	}
}

func TestTransition_FromUnknownStateRejected(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, time.April, 30, 0, 0, 0, 0, time.UTC))
	app := &internalAppender{}
	sm := NewStateMachine(app, clk, "s", "p")

	sm.current = State(99)

	err := sm.Transition(context.Background(), StateInitializing, "synthetic")
	if !errors.Is(err, ErrIllegalTransition) {
		t.Fatalf("err=%v, want ErrIllegalTransition (no-entry branch)", err)
	}
	if sm.current != State(99) {
		t.Errorf("current mutated on illegal transition: %v", sm.current)
	}
	if len(app.events) != 0 {
		t.Errorf("events emitted on illegal transition: %d", len(app.events))
	}
}
