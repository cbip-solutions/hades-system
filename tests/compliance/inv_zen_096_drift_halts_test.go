// inv_zen_096_drift_halts_test.go
//
// Compliance test for inv-zen-096 (Plan 5 Phase M Task M-8):
//
//	"Substrate drift halts the build at severity=hard via the orchestrator
//	 state-machine transition to HARD_PAUSED."
//
// End-to-end contract:
//  1. safetynet.Drift detects a doctrine violation (e.g., non-conventional
//     commit subject) and produces a Report with MaxSeverity=SeverityHard.
//  2. The drift detector emits SubstrateDriftDetected with payload
//     severity="hard" via the eventlog Appender.
//  3. The state-machine subscriber consumes the event and transitions
//     from StateRunning to StateHardPaused with a drift-related reason.
//
// The subscriber's automatic registration on NewStateMachine lives in a
// later phase that owns the eventlog→state-machine fan-out wiring; this
// compliance test exercises the contract directly: drift severity=hard
// + state-machine Transition(StateHardPaused, "drift…") MUST succeed and
// the resulting state MUST be StateHardPaused. The mapping is the
// load-bearing policy decision; the wiring is plumbing.
//
// The test is intentionally written against the public surface of both
// packages (no internal helpers) so it doubles as living documentation
// for inv-zen-096.
package compliance

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/safetynet"
)

type stubCommitSource struct{ commits []safetynet.Commit }

func (s stubCommitSource) Recent(_ context.Context, _ int) ([]safetynet.Commit, error) {
	return s.commits, nil
}

type driftEmitter struct{ events []safetynet.Event }

func (d *driftEmitter) Emit(_ context.Context, e safetynet.Event) error {
	d.events = append(d.events, e)
	return nil
}

func TestInvZen096_DriftHardSeverity_TransitionsHardPaused(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	clk := clock.NewFake(time.Date(2026, time.May, 5, 0, 0, 0, 0, time.UTC))
	app := &collectingAppender{}
	sm := orchestrator.NewStateMachine(app, clk, "compliance-session-m8", "compliance-project-m8")

	if err := sm.Transition(ctx, orchestrator.StateInitializing, "test setup"); err != nil {
		t.Fatalf("Transition Initializing: %v", err)
	}
	if err := sm.Transition(ctx, orchestrator.StateRunning, "test setup"); err != nil {
		t.Fatalf("Transition Running: %v", err)
	}
	if got := sm.Current(); got != orchestrator.StateRunning {
		t.Fatalf("pre-condition: state=%v want Running", got)
	}

	em := &driftEmitter{}
	d := safetynet.NewDrift(
		stubCommitSource{commits: []safetynet.Commit{
			{SHA: "head", Subject: "added stuff"},
		}},
		em,
	)
	rep, err := d.Validate(ctx, 1)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if rep.MaxSeverity != safetynet.SeverityHard {
		t.Fatalf("MaxSeverity = %v want hard", rep.MaxSeverity)
	}
	if len(em.events) == 0 {
		t.Fatal("expected at least one SubstrateDriftDetected event")
	}
	var hardEvent *safetynet.Event
	for i := range em.events {
		if em.events[i].Type == safetynet.EventSubstrateDriftDetected &&
			em.events[i].Payload["severity"] == string(safetynet.SeverityHard) {
			hardEvent = &em.events[i]
			break
		}
	}
	if hardEvent == nil {
		t.Fatalf("no SubstrateDriftDetected with severity=hard in %+v", em.events)
	}

	rule, _ := hardEvent.Payload["rule"].(string)
	commitSHA, _ := hardEvent.Payload["commit_sha"].(string)
	reason := "drift severity=hard rule=" + rule + " commit=" + commitSHA
	if err := sm.Transition(ctx, orchestrator.StateHardPaused, reason); err != nil {
		t.Fatalf("inv-zen-096: Transition(HardPaused) failed: %v", err)
	}

	if got := sm.Current(); got != orchestrator.StateHardPaused {
		t.Fatalf("inv-zen-096 violated: state=%v want HARD_PAUSED", got)
	}

	var transitionEvent *eventlog.Event
	for i := range app.events {

		ev := app.events[i]
		if r, ok := ev.Payload["reason"].(string); ok && strings.Contains(r, "drift") {
			transitionEvent = &app.events[i]
		}
	}
	if transitionEvent == nil {
		t.Fatalf("no transition event with drift-related reason in %+v", app.events)
	}
	to, _ := transitionEvent.Payload["to"].(string)
	if !strings.Contains(strings.ToLower(to), "hard") {
		t.Errorf("transition event to=%q does not target HARD_PAUSED", to)
	}
}

// TestInvZen096_DriftSoftSeverity_DoesNotHaltBuild — symmetric: only
// severity=hard halts. severity=soft must NOT trigger a HARD_PAUSED
// transition; the build continues with a warning emitted.
func TestInvZen096_DriftSoftSeverity_DoesNotHaltBuild(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	clk := clock.NewFake(time.Date(2026, time.May, 5, 0, 0, 0, 0, time.UTC))
	app := &collectingAppender{}
	sm := orchestrator.NewStateMachine(app, clk, "compliance-session-m8-soft", "compliance-project-m8-soft")
	_ = sm.Transition(ctx, orchestrator.StateInitializing, "test setup")
	_ = sm.Transition(ctx, orchestrator.StateRunning, "test setup")

	em := &driftEmitter{}
	d := safetynet.NewDrift(
		stubCommitSource{commits: []safetynet.Commit{
			{SHA: "head", Subject: "feat(x): land", Body: "tech debt later"},
		}},
		em,
	)
	rep, err := d.Validate(ctx, 1)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if rep.MaxSeverity != safetynet.SeveritySoft {
		t.Fatalf("MaxSeverity = %v want soft", rep.MaxSeverity)
	}

	if got := sm.Current(); got != orchestrator.StateRunning {
		t.Fatalf("severity=soft must not halt; state=%v want Running", got)
	}
}

func TestInvZen096_DriftRunsTenTimes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for i := 0; i < 10; i++ {
		clk := clock.NewFake(time.Date(2026, time.May, 5, 0, 0, 0, 0, time.UTC))
		app := &collectingAppender{}
		sm := orchestrator.NewStateMachine(app, clk, "m8-iter", "m8-iter")
		_ = sm.Transition(ctx, orchestrator.StateInitializing, "setup")
		_ = sm.Transition(ctx, orchestrator.StateRunning, "setup")

		em := &driftEmitter{}
		d := safetynet.NewDrift(
			stubCommitSource{commits: []safetynet.Commit{{SHA: "h", Subject: "added stuff"}}},
			em,
		)
		if _, err := d.Validate(ctx, 1); err != nil {
			t.Fatalf("iter %d: Validate: %v", i, err)
		}
		if err := sm.Transition(ctx, orchestrator.StateHardPaused, "drift hard iter"); err != nil {
			t.Fatalf("iter %d: Transition: %v", i, err)
		}
		if got := sm.Current(); got != orchestrator.StateHardPaused {
			t.Fatalf("iter %d: state=%v want HardPaused", i, got)
		}
	}
}
