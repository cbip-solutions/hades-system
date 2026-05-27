// SPDX-License-Identifier: MIT
package orchestrator

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

func TestMainLoop_WorkerDeathStreakTransitionsHardPaused(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 5, 27, 13, 0, 0, 0, time.UTC))
	log := eventlog.NewMemory(clk)
	sm := newMainLoopFakeStateMachine(StateRunning)
	loop, err := NewMainLoop(MainLoopConfig{
		Log:       log,
		SM:        sm,
		Clock:     clk,
		SessionID: "session-main-loop-test",
		ProjectID: "project-main-loop-test",
		IdleTick:  time.Minute,
	})
	if err != nil {
		t.Fatalf("NewMainLoop: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		loop.Run(ctx)
	}()
	defer func() {
		cancel()
		if !waitForClosed(done, time.Second) {
			t.Fatal("MainLoop.Run did not return after cancellation")
		}
	}()

	for i := 0; i < 3; i++ {
		if _, err := log.Append(context.Background(), eventlog.Event{
			Type:      eventlog.EvtWorkerDeath,
			SessionID: "session-main-loop-test",
			ProjectID: "project-main-loop-test",
			Payload: map[string]any{
				"worker_id":   "worker",
				"cause":       "heartbeat_timeout",
				"retry_count": i,
			},
		}); err != nil {
			t.Fatalf("Append WorkerDeath: %v", err)
		}
	}

	if !clk.BlockUntilCondition(func() bool {
		return sm.Current() == StateHardPaused
	}, time.Second) {
		t.Fatalf("state = %v, want HardPaused after three worker deaths", sm.Current())
	}
	if got := sm.LastReason(); got != "main_loop:worker_death_streak" {
		t.Fatalf("last transition reason = %q, want main_loop:worker_death_streak", got)
	}
}

func TestMainLoop_HardSubstrateDriftTransitionsHardPaused(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 5, 27, 14, 0, 0, 0, time.UTC))
	log := eventlog.NewMemory(clk)
	sm := newMainLoopFakeStateMachine(StateRunning)
	loop, err := NewMainLoop(MainLoopConfig{
		Log:       log,
		SM:        sm,
		Clock:     clk,
		SessionID: "session-main-loop-test",
		ProjectID: "project-main-loop-test",
	})
	if err != nil {
		t.Fatalf("NewMainLoop: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		loop.Run(ctx)
	}()
	defer func() {
		cancel()
		if !waitForClosed(done, time.Second) {
			t.Fatal("MainLoop.Run did not return after cancellation")
		}
	}()

	if _, err := log.Append(context.Background(), eventlog.Event{
		Type:      eventlog.EvtSubstrateDriftDetected,
		SessionID: "session-main-loop-test",
		ProjectID: "project-main-loop-test",
		Payload: map[string]any{
			"severity": "hard",
			"findings": "attribution leak",
		},
	}); err != nil {
		t.Fatalf("Append SubstrateDriftDetected: %v", err)
	}

	if !clk.BlockUntilCondition(func() bool {
		return sm.Current() == StateHardPaused
	}, time.Second) {
		t.Fatalf("state = %v, want HardPaused after hard drift", sm.Current())
	}
	if got := sm.LastReason(); got != "main_loop:substrate_drift_hard" {
		t.Fatalf("last transition reason = %q, want main_loop:substrate_drift_hard", got)
	}
}

func TestMainLoop_IgnoresBudgetContinueEvents(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 5, 27, 14, 30, 0, 0, time.UTC))
	log := eventlog.NewMemory(clk)
	sm := newMainLoopFakeStateMachine(StateRunning)
	loop, err := NewMainLoop(MainLoopConfig{
		Log:       log,
		SM:        sm,
		Clock:     clk,
		SessionID: "session-main-loop-test",
		ProjectID: "project-main-loop-test",
	})
	if err != nil {
		t.Fatalf("NewMainLoop: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		loop.Run(ctx)
	}()
	defer func() {
		cancel()
		if !waitForClosed(done, time.Second) {
			t.Fatal("MainLoop.Run did not return after cancellation")
		}
	}()

	if _, err := log.Append(context.Background(), eventlog.Event{
		Type:      eventlog.EvtBudgetDegradationApplied,
		SessionID: "session-main-loop-test",
		ProjectID: "project-main-loop-test",
		Payload: map[string]any{
			"action":        "continue",
			"threshold_pct": 0,
		},
	}); err != nil {
		t.Fatalf("Append BudgetDegradationApplied: %v", err)
	}
	clk.Advance(time.Minute)
	if sm.Current() != StateRunning {
		t.Fatalf("state = %v, want Running for budget continue", sm.Current())
	}
}

func TestMainLoop_IgnoresCrossProjectEvents(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 5, 27, 15, 0, 0, 0, time.UTC))
	log := eventlog.NewMemory(clk)
	sm := newMainLoopFakeStateMachine(StateRunning)
	loop, err := NewMainLoop(MainLoopConfig{
		Log:       log,
		SM:        sm,
		Clock:     clk,
		SessionID: "session-main-loop-test",
		ProjectID: "project-main-loop-test",
	})
	if err != nil {
		t.Fatalf("NewMainLoop: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		loop.Run(ctx)
	}()
	defer func() {
		cancel()
		if !waitForClosed(done, time.Second) {
			t.Fatal("MainLoop.Run did not return after cancellation")
		}
	}()

	for i := 0; i < 3; i++ {
		if _, err := log.Append(context.Background(), eventlog.Event{
			Type:      eventlog.EvtWorkerDeath,
			SessionID: "session-main-loop-test",
			ProjectID: "other-project",
			Payload:   map[string]any{"worker_id": "worker"},
		}); err != nil {
			t.Fatalf("Append WorkerDeath: %v", err)
		}
	}
	clk.Advance(time.Minute)
	if sm.Current() != StateRunning {
		t.Fatalf("state = %v, want Running for cross-project events", sm.Current())
	}
}

func TestNewMainLoopRejectsInvalidConfig(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 5, 27, 16, 0, 0, 0, time.UTC))
	log := eventlog.NewMemory(clk)
	sm := newMainLoopFakeStateMachine(StateRunning)

	tests := []struct {
		name string
		cfg  MainLoopConfig
	}{
		{name: "nil log", cfg: MainLoopConfig{SM: sm, Clock: clk, SessionID: "s", ProjectID: "p"}},
		{name: "nil state machine", cfg: MainLoopConfig{Log: log, Clock: clk, SessionID: "s", ProjectID: "p"}},
		{name: "empty session", cfg: MainLoopConfig{Log: log, SM: sm, Clock: clk, ProjectID: "p"}},
		{name: "empty project", cfg: MainLoopConfig{Log: log, SM: sm, Clock: clk, SessionID: "s"}},
		{name: "negative idle tick", cfg: MainLoopConfig{Log: log, SM: sm, Clock: clk, SessionID: "s", ProjectID: "p", IdleTick: -time.Second}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewMainLoop(tc.cfg)
			if !errors.Is(err, ErrMainLoopInvalidConfig) {
				t.Fatalf("err = %v, want ErrMainLoopInvalidConfig", err)
			}
		})
	}
}

type mainLoopFakeStateMachine struct {
	mu     sync.Mutex
	state  State
	reason string
}

func newMainLoopFakeStateMachine(initial State) *mainLoopFakeStateMachine {
	return &mainLoopFakeStateMachine{state: initial}
}

func (f *mainLoopFakeStateMachine) Current() State {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state
}

func (f *mainLoopFakeStateMachine) LastReason() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reason
}

func (f *mainLoopFakeStateMachine) Transition(_ context.Context, to State, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	allowed := map[[2]State]bool{
		{StateRunning, StateHardPaused}:                  true,
		{StateRunning, StateDegradedTier}:                true,
		{StateDegradedTier, StateHardPaused}:             true,
		{StateWaitingForConfirmation, StateAborting}:     true,
		{StateWaitingForConfirmation, StateDegradedTier}: true,
	}
	if !allowed[[2]State{f.state, to}] {
		return errors.New("orchestrator/test: illegal main-loop transition")
	}
	f.state = to
	f.reason = reason
	return nil
}
