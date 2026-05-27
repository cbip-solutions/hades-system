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
	"github.com/cbip-solutions/hades-system/internal/workforce/gate"
)

func TestConfirmationWatcher_TimedOutPendingRequestAutoDenies(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 5, 27, 9, 0, 0, 0, time.UTC))
	h, sm, ap, g := newConfirmationWatcherHarness(t, clk)

	out, err := h.RequestConfirmation(context.Background(), RequestConfirmationInput{
		Class:   DecisionInvariantViolation,
		Summary: "operator confirmation required",
	})
	if err != nil {
		t.Fatalf("RequestConfirmation: %v", err)
	}
	clk.Advance(11 * time.Minute)

	w, err := NewConfirmationWatcher(ConfirmationWatcherConfig{
		Handler:  h,
		Clock:    clk,
		Interval: time.Minute,
		Timeout:  10 * time.Minute,
	})
	if err != nil {
		t.Fatalf("NewConfirmationWatcher: %v", err)
	}
	if err := w.sweepOnce(context.Background()); err != nil {
		t.Fatalf("sweepOnce: %v", err)
	}

	if sm.Current() != StateAborting {
		t.Fatalf("state = %v, want Aborting after confirmation timeout", sm.Current())
	}
	if g.State() == gate.StateRunning {
		t.Fatalf("gate resumed after timeout deny; want paused for abort tear-down")
	}
	if got := ap.Count(eventlog.EvtOperatorConfirmation); got != 1 {
		t.Fatalf("OperatorConfirmation events = %d, want 1", got)
	}
	last := ap.Last(eventlog.EvtOperatorConfirmation)
	if got, _ := last.Payload["decision"].(string); got != "deny" {
		t.Fatalf("decision = %q, want deny", got)
	}
	if got, _ := last.Payload["rationale"].(string); got != "confirmation timeout after 10m0s" {
		t.Fatalf("rationale = %q, want timeout rationale", got)
	}
	if got, _ := last.Payload["request_seq"].(uint64); got != out.RequestSeq {
		t.Fatalf("request_seq = %d, want %d", got, out.RequestSeq)
	}
	if _, ok := h.pendingForTest(); ok {
		t.Fatal("pending confirmation must be cleared after timeout deny")
	}
}

func TestConfirmationWatcher_PreTimeoutPendingRequestIsLeftIntact(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC))
	h, sm, ap, g := newConfirmationWatcherHarness(t, clk)

	out, err := h.RequestConfirmation(context.Background(), RequestConfirmationInput{
		Class:   DecisionInvariantViolation,
		Summary: "operator confirmation required",
	})
	if err != nil {
		t.Fatalf("RequestConfirmation: %v", err)
	}
	clk.Advance(9 * time.Minute)

	w, err := NewConfirmationWatcher(ConfirmationWatcherConfig{
		Handler:  h,
		Clock:    clk,
		Interval: time.Minute,
		Timeout:  10 * time.Minute,
	})
	if err != nil {
		t.Fatalf("NewConfirmationWatcher: %v", err)
	}
	if err := w.sweepOnce(context.Background()); err != nil {
		t.Fatalf("sweepOnce: %v", err)
	}

	if sm.Current() != StateWaitingForConfirmation {
		t.Fatalf("state = %v, want WaitingForConfirmation before timeout", sm.Current())
	}
	if g.State() != gate.StatePausedDescriptive {
		t.Fatalf("gate = %v, want PausedDescriptive before timeout", g.State())
	}
	if got := ap.Count(eventlog.EvtOperatorConfirmation); got != 0 {
		t.Fatalf("OperatorConfirmation events = %d, want 0 before timeout", got)
	}
	pending, ok := h.pendingForTest()
	if !ok {
		t.Fatal("pending confirmation must stay intact before timeout")
	}
	if pending.eventID != out.EventID {
		t.Fatalf("pending eventID = %d, want %d", pending.eventID, out.EventID)
	}
}

func TestConfirmationWatcher_RunSweepsOnTicker(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 5, 27, 11, 0, 0, 0, time.UTC))
	h, sm, ap, _ := newConfirmationWatcherHarness(t, clk)

	if _, err := h.RequestConfirmation(context.Background(), RequestConfirmationInput{
		Class:   DecisionInvariantViolation,
		Summary: "operator confirmation required",
	}); err != nil {
		t.Fatalf("RequestConfirmation: %v", err)
	}

	w, err := NewConfirmationWatcher(ConfirmationWatcherConfig{
		Handler:  h,
		Clock:    clk,
		Interval: time.Minute,
		Timeout:  10 * time.Minute,
	})
	if err != nil {
		t.Fatalf("NewConfirmationWatcher: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		w.Run(ctx)
	}()

	for i := 0; i < 20 && sm.Current() != StateAborting; i++ {
		clk.Advance(time.Minute)
		if clk.BlockUntilCondition(func() bool {
			return ap.Count(eventlog.EvtOperatorConfirmation) == 1
		}, 50*time.Millisecond) {
			break
		}
	}
	if sm.Current() != StateAborting {
		t.Fatalf("state = %v, want Aborting after ticker-driven timeout", sm.Current())
	}

	cancel()
	if !waitForClosed(done, time.Second) {
		t.Fatal("ConfirmationWatcher.Run did not return after context cancellation")
	}
}

func TestNewConfirmationWatcherRejectsInvalidConfig(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC))
	h, _, _, _ := newConfirmationWatcherHarness(t, clk)

	tests := []struct {
		name string
		cfg  ConfirmationWatcherConfig
	}{
		{name: "nil handler", cfg: ConfirmationWatcherConfig{Clock: clk}},
		{name: "non-positive interval", cfg: ConfirmationWatcherConfig{Handler: h, Clock: clk, Interval: -time.Second}},
		{name: "non-positive timeout", cfg: ConfirmationWatcherConfig{Handler: h, Clock: clk, Timeout: -time.Second}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewConfirmationWatcher(tc.cfg)
			if !errors.Is(err, ErrConfirmationWatcherInvalidConfig) {
				t.Fatalf("err = %v, want ErrConfirmationWatcherInvalidConfig", err)
			}
		})
	}
}

func newConfirmationWatcherHarness(t *testing.T, clk *clock.Fake) (*ConfirmationHandler, *watcherFakeStateMachine, *watcherFakeAppender, *watcherFakeGate) {
	t.Helper()
	sm := newWatcherFakeStateMachine(StateRunning)
	ap := newWatcherFakeAppender()
	g := newWatcherFakeGate(gate.StateRunning)
	h := NewConfirmationHandler(
		NewConfirmationPolicy(map[DecisionClass]Threshold{
			DecisionInvariantViolation: ThresholdHigh,
		}, false),
		sm,
		ap,
		g,
		"session-confirmation-watcher-test",
		"project-confirmation-watcher-test",
	)
	h.now = clk.Now
	return h, sm, ap, g
}

func (h *ConfirmationHandler) pendingForTest() (pendingRequest, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.pending == nil {
		return pendingRequest{}, false
	}
	return *h.pending, true
}

func waitForClosed(ch <-chan struct{}, timeout time.Duration) bool {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ch:
		return true
	case <-timer.C:
		return false
	}
}

type watcherFakeStateMachine struct {
	mu    sync.Mutex
	state State
}

func newWatcherFakeStateMachine(initial State) *watcherFakeStateMachine {
	return &watcherFakeStateMachine{state: initial}
}

func (f *watcherFakeStateMachine) Current() State {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state
}

func (f *watcherFakeStateMachine) Transition(_ context.Context, to State, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	allowed := map[[2]State]bool{
		{StateRunning, StateWaitingForConfirmation}:      true,
		{StateWaitingForConfirmation, StateRunning}:      true,
		{StateWaitingForConfirmation, StateAborting}:     true,
		{StateDegradedTier, StateWaitingForConfirmation}: true,
		{StateWaitingForConfirmation, StateDegradedTier}: true,
	}
	if !allowed[[2]State{f.state, to}] {
		return errors.New("orchestrator/test: illegal watcher transition")
	}
	f.state = to
	return nil
}

type watcherFakeAppender struct {
	mu       sync.Mutex
	counts   map[eventlog.EventType]int
	last     map[eventlog.EventType]eventlog.Event
	appendID int64
}

func newWatcherFakeAppender() *watcherFakeAppender {
	return &watcherFakeAppender{
		counts: map[eventlog.EventType]int{},
		last:   map[eventlog.EventType]eventlog.Event{},
	}
}

func (f *watcherFakeAppender) Append(_ context.Context, ev eventlog.Event) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.counts[ev.Type]++
	f.last[ev.Type] = ev
	f.appendID++
	return f.appendID, nil
}

func (f *watcherFakeAppender) Count(t eventlog.EventType) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.counts[t]
}

func (f *watcherFakeAppender) Last(t eventlog.EventType) eventlog.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.last[t]
}

type watcherFakeGate struct {
	mu    sync.Mutex
	state gate.State
}

func newWatcherFakeGate(initial gate.State) *watcherFakeGate {
	return &watcherFakeGate{state: initial}
}

func (f *watcherFakeGate) Pause(_ context.Context, mode gate.PauseMode, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch mode {
	case gate.PauseDescriptive:
		f.state = gate.StatePausedDescriptive
	case gate.PauseQuiet:
		f.state = gate.StatePausedQuiet
	case gate.PauseAfterApply:
		f.state = gate.StatePausedAfterApply
	default:
		return errors.New("orchestrator/test: unsupported pause mode")
	}
	return nil
}

func (f *watcherFakeGate) Resume(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.state = gate.StateRunning
	return nil
}

func (f *watcherFakeGate) State() gate.State {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state
}
