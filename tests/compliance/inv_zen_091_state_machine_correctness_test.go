// tests/compliance/inv_zen_091_state_machine_correctness_test.go
//
// invariant — State machine transitions ∈ TransitionTable.
//
// Compliance witness: re-derives the canonical 28 transitions from
// spec §1 Q6 D INDEPENDENTLY of the runtime TransitionTable and
// proves (a) every canonical transition is in TransitionTable,
// (b) every TransitionTable transition is in the canonical set
// (no extras), (c) the runtime Transition method rejects an
// adversarial illegal probe.
//
// This is the third witness for invariant (code: TransitionTable
// + _validTransitions; unit test: TestTransitionTableHas28*; this
// file). Three places to catch one drift.
//
// If this test fails, the state machine has drifted from the spec
// and EITHER the spec must be amended (operator decision, ADR-track)
// OR the code must be reverted. Do NOT silently update this file to
// match new behaviour — surface to operator.
package compliance

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

var canonicalTransitions = []struct{ from, to orchestrator.State }{

	{orchestrator.StateIdle, orchestrator.StateInitializing},

	{orchestrator.StateInitializing, orchestrator.StateRunning},
	{orchestrator.StateInitializing, orchestrator.StateAborting},

	{orchestrator.StateRunning, orchestrator.StateWaitingForConfirmation},
	{orchestrator.StateRunning, orchestrator.StateDegradedTier},
	{orchestrator.StateRunning, orchestrator.StateHardPaused},
	{orchestrator.StateRunning, orchestrator.StateEmergencyTier},
	{orchestrator.StateRunning, orchestrator.StateAborting},
	{orchestrator.StateRunning, orchestrator.StateIdle},

	{orchestrator.StateWaitingForConfirmation, orchestrator.StateRunning},
	{orchestrator.StateWaitingForConfirmation, orchestrator.StateDegradedTier},
	{orchestrator.StateWaitingForConfirmation, orchestrator.StateAborting},

	{orchestrator.StateDegradedTier, orchestrator.StateRunning},
	{orchestrator.StateDegradedTier, orchestrator.StateWaitingForConfirmation},
	{orchestrator.StateDegradedTier, orchestrator.StateHardPaused},
	{orchestrator.StateDegradedTier, orchestrator.StateEmergencyTier},
	{orchestrator.StateDegradedTier, orchestrator.StateAborting},

	{orchestrator.StateHardPaused, orchestrator.StateWaitingForConfirmation},
	{orchestrator.StateHardPaused, orchestrator.StateAborting},
	{orchestrator.StateHardPaused, orchestrator.StateRunning},

	{orchestrator.StateEmergencyTier, orchestrator.StateHardPaused},
	{orchestrator.StateEmergencyTier, orchestrator.StateAborting},
	{orchestrator.StateEmergencyTier, orchestrator.StateWaitingForConfirmation},

	{orchestrator.StateRecoveringFromReplay, orchestrator.StateRunning},
	{orchestrator.StateRecoveringFromReplay, orchestrator.StateDegradedTier},
	{orchestrator.StateRecoveringFromReplay, orchestrator.StateWaitingForConfirmation},
	{orchestrator.StateRecoveringFromReplay, orchestrator.StateAborting},

	{orchestrator.StateAborting, orchestrator.StateIdle},
}

func TestInvZen091_CanonicalSetHasExactly28(t *testing.T) {
	if got := len(canonicalTransitions); got != 28 {
		t.Fatalf("canonicalTransitions len=%d, want 28 (spec §1 Q6 D drift)", got)
	}
	if got := orchestrator.ValidTransitionCount(); got != 28 {
		t.Fatalf("orchestrator.ValidTransitionCount() = %d, want 28", got)
	}
}

func TestInvZen091_TransitionTableMatchesCanonicalExactly(t *testing.T) {
	canonSet := make(map[orchestrator.State]map[orchestrator.State]struct{})
	for _, e := range canonicalTransitions {
		if _, ok := canonSet[e.from]; !ok {
			canonSet[e.from] = make(map[orchestrator.State]struct{})
		}
		canonSet[e.from][e.to] = struct{}{}
	}

	for from, tos := range canonSet {
		for to := range tos {
			if !orchestrator.IsLegal(from, to) {
				t.Errorf("MISSING in TransitionTable: %s→%s (canonical per spec)", from, to)
			}
		}
	}

	for from, tos := range orchestrator.TransitionTable {
		for to := range tos {
			if _, ok := canonSet[from][to]; !ok {
				t.Errorf("EXTRA in TransitionTable: %s→%s (not in spec canonical set)", from, to)
			}
		}
	}
}

func TestInvZen091_RuntimeRejectsIllegalTransition(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, time.April, 30, 0, 0, 0, 0, time.UTC))
	app := &collectingAppender{}
	sm := orchestrator.NewStateMachine(app, clk, "compliance-session", "compliance-project")

	err := sm.Transition(context.Background(), orchestrator.StateAborting, "adversarial")
	if !errors.Is(err, orchestrator.ErrIllegalTransition) {
		t.Fatalf("err=%v, want ErrIllegalTransition", err)
	}
	if sm.Current() != orchestrator.StateIdle {
		t.Errorf("state mutated on illegal transition: %v", sm.Current())
	}
	if len(app.events) != 0 {
		t.Errorf("events emitted on illegal transition: %d", len(app.events))
	}
}

func TestInvZen091_ValidStatesAreNine(t *testing.T) {
	got := orchestrator.ValidStates()
	if len(got) != 9 {
		t.Fatalf("ValidStates len=%d, want 9 (spec §1 Q6 D)", len(got))
	}

	want := map[orchestrator.State]bool{
		orchestrator.StateIdle:                   true,
		orchestrator.StateInitializing:           true,
		orchestrator.StateRunning:                true,
		orchestrator.StateWaitingForConfirmation: true,
		orchestrator.StateDegradedTier:           true,
		orchestrator.StateHardPaused:             true,
		orchestrator.StateRecoveringFromReplay:   true,
		orchestrator.StateEmergencyTier:          true,
		orchestrator.StateAborting:               true,
	}
	seen := make(map[orchestrator.State]bool, len(got))
	for _, s := range got {
		if !want[s] {
			t.Errorf("ValidStates contains non-canonical state %v", s)
		}
		if seen[s] {
			t.Errorf("ValidStates contains duplicate %v", s)
		}
		seen[s] = true
	}
	for s := range want {
		if !seen[s] {
			t.Errorf("ValidStates missing canonical state %v", s)
		}
	}
}

type collectingAppender struct {
	events []eventlog.Event
}

func (c *collectingAppender) Append(_ context.Context, e eventlog.Event) (int64, error) {
	c.events = append(c.events, e)
	return int64(len(c.events)), nil
}
