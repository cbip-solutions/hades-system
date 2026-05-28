// SPDX-License-Identifier: MIT
// Package orchestrator state machine — HADES design
//
// Defines the 9 supervisor states and the canonical TransitionTable of
// 28 valid transitions per design contract
// every runtime transition is in this table; illegal transitions return
// ErrIllegalTransition without mutating state and without emitting an
// event. Concurrent callers serialize through the embedded mutex.
//
// Architecture leaf package internal to orchestrator/. Imports only
// internal/orchestrator/eventlog (event emission) and
// internal/orchestrator/clock (monotonic timestamps).
// orchestrator.go / dispatcher.go / depth.go siblings will consume
// this file as the supervisor's authoritative state holder.
//
// Invariants
// - invariant: state machine transitions ∈ TransitionTable
// (compile-checked exhaustiveness witnesses + runtime rejection +
// compliance test in tests/compliance/).
// - invariant: this file does NOT import internal/store.
// - invariant: this file does NOT import internal/workforce/queue.
package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

type State uint8

const (
	stateInvalid State = iota

	StateIdle

	StateInitializing

	StateRunning

	StateWaitingForConfirmation

	StateDegradedTier

	StateHardPaused

	StateRecoveringFromReplay

	StateEmergencyTier

	StateAborting
)

func (s State) String() string {
	switch s {
	case StateIdle:
		return "idle"
	case StateInitializing:
		return "initializing"
	case StateRunning:
		return "running"
	case StateWaitingForConfirmation:
		return "waiting_for_confirmation"
	case StateDegradedTier:
		return "degraded_tier"
	case StateHardPaused:
		return "hard_paused"
	case StateRecoveringFromReplay:
		return "recovering_from_replay"
	case StateEmergencyTier:
		return "emergency_tier"
	case StateAborting:
		return "aborting"
	default:
		return fmt.Sprintf("state(%d)", uint8(s))
	}
}

var ErrUnknownState = errors.New("orchestrator: unknown state")

func ParseState(s string) (State, error) {
	switch s {
	case "idle":
		return StateIdle, nil
	case "initializing":
		return StateInitializing, nil
	case "running":
		return StateRunning, nil
	case "waiting_for_confirmation":
		return StateWaitingForConfirmation, nil
	case "degraded_tier":
		return StateDegradedTier, nil
	case "hard_paused":
		return StateHardPaused, nil
	case "recovering_from_replay":
		return StateRecoveringFromReplay, nil
	case "emergency_tier":
		return StateEmergencyTier, nil
	case "aborting":
		return StateAborting, nil
	default:
		return 0, fmt.Errorf("%w: %q", ErrUnknownState, s)
	}
}

// StateTransition is the value type representing a transition. Reason
// is a free-form short string (e.g. "operator-confirmed",
// "cost-degradation-80pct", "panic-recovery"). Reason MUST be
// pre-redacted by the caller — this package emits it verbatim into the
// event-log payload.
type StateTransition struct {
	From   State
	To     State
	Reason string
}

// TransitionTable is the canonical set of 28 valid transitions per
// spec §1 design choice D invariant. The compliance test re-derives this set
// from the spec; do NOT add a transition without first amending the
// spec.
//
// Layout TransitionTable[from][to] = struct{}{} when the move is
// legal.
//
// Composition 28 transitions = 24 non-recovery + 4 recovery exits
// (RECOVERING_FROM_REPLAY → {Running, DegradedTier,
// WaitingForConfirmation, Aborting}). The "{any}→
// RECOVERING_FROM_REPLAY (on daemon restart)" rule from the spec
// belongs to the Replay code path which constructs a fresh
// StateMachine after restart rather than entering RECOVERING via
// Transition; consequently no inbound RECOVERING edges appear in
// this table.
var TransitionTable = map[State]map[State]struct{}{

	StateIdle: {
		StateInitializing: {},
	},

	StateInitializing: {
		StateRunning:  {},
		StateAborting: {},
	},

	StateRunning: {
		StateWaitingForConfirmation: {},
		StateDegradedTier:           {},
		StateHardPaused:             {},
		StateEmergencyTier:          {},
		StateAborting:               {},
		StateIdle:                   {},
	},

	StateWaitingForConfirmation: {
		StateRunning:      {},
		StateDegradedTier: {},
		StateAborting:     {},
	},

	StateDegradedTier: {
		StateRunning:                {},
		StateWaitingForConfirmation: {},
		StateHardPaused:             {},
		StateEmergencyTier:          {},
		StateAborting:               {},
	},

	StateHardPaused: {
		StateWaitingForConfirmation: {},
		StateAborting:               {},
		StateRunning:                {},
	},

	StateEmergencyTier: {
		StateHardPaused:             {},
		StateAborting:               {},
		StateWaitingForConfirmation: {},
	},

	StateRecoveringFromReplay: {
		StateRunning:                {},
		StateDegradedTier:           {},
		StateWaitingForConfirmation: {},
		StateAborting:               {},
	},

	StateAborting: {
		StateIdle: {},
	},
}

var _validStates = []State{
	StateIdle,
	StateInitializing,
	StateRunning,
	StateWaitingForConfirmation,
	StateDegradedTier,
	StateHardPaused,
	StateRecoveringFromReplay,
	StateEmergencyTier,
	StateAborting,
}

var _validTransitions = func() int {
	n := 0
	for _, tos := range TransitionTable {
		n += len(tos)
	}
	return n
}()

func ValidStates() []State {
	out := make([]State, len(_validStates))
	copy(out, _validStates)
	return out
}

func ValidTransitionCount() int {
	return _validTransitions
}

var ErrIllegalTransition = errors.New("orchestrator: illegal state transition")

func IsLegal(from, to State) bool {
	tos, ok := TransitionTable[from]
	if !ok {
		return false
	}
	_, ok = tos[to]
	return ok
}

type StateMachine struct {
	mu        sync.Mutex
	current   State
	app       eventlog.Appender
	clk       clock.Clock
	sessionID string
	projectID string
}

func NewStateMachine(app eventlog.Appender, clk clock.Clock, sessionID, projectID string) *StateMachine {
	if app == nil {
		panic("orchestrator: NewStateMachine: appender must not be nil")
	}
	if clk == nil {
		panic("orchestrator: NewStateMachine: clock must not be nil")
	}
	if sessionID == "" {
		panic("orchestrator: NewStateMachine: sessionID must not be empty")
	}
	if projectID == "" {
		panic("orchestrator: NewStateMachine: projectID must not be empty")
	}
	return &StateMachine{
		current:   StateIdle,
		app:       app,
		clk:       clk,
		sessionID: sessionID,
		projectID: projectID,
	}
}

func (sm *StateMachine) Current() State {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.current
}

func (sm *StateMachine) Transition(ctx context.Context, to State, reason string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("orchestrator: Transition: ctx cancelled before start: %w", err)
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	from := sm.current
	tos, ok := TransitionTable[from]
	if !ok {
		return fmt.Errorf("%w: from=%s to=%s (no entry for current state)", ErrIllegalTransition, from, to)
	}
	if _, ok := tos[to]; !ok {
		return fmt.Errorf("%w: %s→%s", ErrIllegalTransition, from, to)
	}

	ev := eventlog.Event{
		Type:      eventlog.EvtOrchestratorStateTransition,
		SessionID: sm.sessionID,
		ProjectID: sm.projectID,
		Timestamp: sm.clk.Now(),
		Payload: map[string]any{
			"from":   from.String(),
			"to":     to.String(),
			"reason": reason,
		},
	}
	if _, err := sm.app.Append(ctx, ev); err != nil {
		// Rollback do NOT mutate current on appender failure. The
		// supervisor will retry or escalate.
		return fmt.Errorf("orchestrator: append state-transition event: %w", err)
	}
	sm.current = to
	return nil
}
