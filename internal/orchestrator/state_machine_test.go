package orchestrator_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/cbip-solutions/hades-system/internal/orchestrator"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

type fakeAppender struct {
	mu     sync.Mutex
	events []eventlog.Event
	nextID int64
}

func (f *fakeAppender) Append(_ context.Context, e eventlog.Event) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	f.events = append(f.events, e)
	return f.nextID, nil
}

func (f *fakeAppender) snapshot() []eventlog.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]eventlog.Event, len(f.events))
	copy(out, f.events)
	return out
}

type errAppender struct{ err error }

func (e *errAppender) Append(_ context.Context, _ eventlog.Event) (int64, error) {
	return 0, e.err
}

func newFakeClock() *clock.Fake {
	return clock.NewFake(time.Date(2026, time.April, 30, 0, 0, 0, 0, time.UTC))
}

const (
	testSessionID = "session-abc"
	testProjectID = "project-xyz"
)

func TestStateString(t *testing.T) {
	cases := []struct {
		s    orchestrator.State
		want string
	}{
		{orchestrator.StateIdle, "idle"},
		{orchestrator.StateInitializing, "initializing"},
		{orchestrator.StateRunning, "running"},
		{orchestrator.StateWaitingForConfirmation, "waiting_for_confirmation"},
		{orchestrator.StateDegradedTier, "degraded_tier"},
		{orchestrator.StateHardPaused, "hard_paused"},
		{orchestrator.StateRecoveringFromReplay, "recovering_from_replay"},
		{orchestrator.StateEmergencyTier, "emergency_tier"},
		{orchestrator.StateAborting, "aborting"},
	}
	for _, c := range cases {
		if got := c.s.String(); got != c.want {
			t.Errorf("State(%d).String() = %q, want %q", int(c.s), got, c.want)
		}
	}
}

func TestStateString_InvalidFallback(t *testing.T) {

	var zero orchestrator.State
	if got := zero.String(); got != "state(0)" {
		t.Errorf("zero State.String() = %q, want %q", got, "state(0)")
	}
	if got := orchestrator.State(255).String(); got != "state(255)" {
		t.Errorf("State(255).String() = %q, want %q", got, "state(255)")
	}
}

func TestParseState(t *testing.T) {
	cases := []struct {
		in  string
		ok  bool
		out orchestrator.State
	}{
		{"idle", true, orchestrator.StateIdle},
		{"initializing", true, orchestrator.StateInitializing},
		{"running", true, orchestrator.StateRunning},
		{"waiting_for_confirmation", true, orchestrator.StateWaitingForConfirmation},
		{"degraded_tier", true, orchestrator.StateDegradedTier},
		{"hard_paused", true, orchestrator.StateHardPaused},
		{"recovering_from_replay", true, orchestrator.StateRecoveringFromReplay},
		{"emergency_tier", true, orchestrator.StateEmergencyTier},
		{"aborting", true, orchestrator.StateAborting},
		{"", false, 0},
		{"unknown", false, 0},
		{"IDLE", false, 0},
	}
	for _, c := range cases {
		got, err := orchestrator.ParseState(c.in)
		if c.ok {
			if err != nil {
				t.Errorf("ParseState(%q) err=%v, want ok", c.in, err)
			}
			if got != c.out {
				t.Errorf("ParseState(%q) = %v, want %v", c.in, got, c.out)
			}
		} else {
			if err == nil {
				t.Errorf("ParseState(%q) err=nil, want error", c.in)
			}
			if !errors.Is(err, orchestrator.ErrUnknownState) {
				t.Errorf("ParseState(%q) err=%v, want wrap of ErrUnknownState", c.in, err)
			}
		}
	}
}

func TestTransitionTableHas28ValidTransitions(t *testing.T) {
	count := 0
	for _, tos := range orchestrator.TransitionTable {
		count += len(tos)
	}
	if count != 28 {
		t.Errorf("TransitionTable has %d transitions, want 28 (per spec §1 Q6 D inv-zen-091)", count)
	}
}

func TestStateTransitionType(t *testing.T) {
	tr := orchestrator.StateTransition{
		From:   orchestrator.StateIdle,
		To:     orchestrator.StateInitializing,
		Reason: "boot",
	}
	if tr.From != orchestrator.StateIdle || tr.To != orchestrator.StateInitializing || tr.Reason != "boot" {
		t.Errorf("StateTransition fields = %+v, want idle→initializing reason=boot", tr)
	}
}

func TestValidStates_ReturnsCanonicalNine(t *testing.T) {
	got := orchestrator.ValidStates()
	if len(got) != 9 {
		t.Errorf("ValidStates len=%d, want 9", len(got))
	}

	got[0] = orchestrator.StateAborting
	again := orchestrator.ValidStates()
	if again[0] == orchestrator.StateAborting {
		t.Errorf("ValidStates returned non-defensive slice")
	}
}

func TestValidTransitionCount_Is28(t *testing.T) {
	if got := orchestrator.ValidTransitionCount(); got != 28 {
		t.Errorf("ValidTransitionCount = %d, want 28", got)
	}
}

func TestStateMachine_NewStartsInIdle(t *testing.T) {
	sm := orchestrator.NewStateMachine(&fakeAppender{}, newFakeClock(), testSessionID, testProjectID)
	if got := sm.Current(); got != orchestrator.StateIdle {
		t.Errorf("new SM current=%v, want idle", got)
	}
}

func TestStateMachine_NewStateMachine_NilAppenderPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("NewStateMachine(nil app) did not panic")
		}
	}()
	_ = orchestrator.NewStateMachine(nil, newFakeClock(), testSessionID, testProjectID)
}

func TestStateMachine_NewStateMachine_NilClockPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("NewStateMachine(nil clock) did not panic")
		}
	}()
	_ = orchestrator.NewStateMachine(&fakeAppender{}, nil, testSessionID, testProjectID)
}

func TestStateMachine_NewStateMachine_EmptySessionPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("NewStateMachine(empty sessionID) did not panic")
		}
	}()
	_ = orchestrator.NewStateMachine(&fakeAppender{}, newFakeClock(), "", testProjectID)
}

func TestStateMachine_NewStateMachine_EmptyProjectPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("NewStateMachine(empty projectID) did not panic")
		}
	}()
	_ = orchestrator.NewStateMachine(&fakeAppender{}, newFakeClock(), testSessionID, "")
}

func TestStateMachine_LegalTransitionMutatesAndEmits(t *testing.T) {
	app := &fakeAppender{}
	clk := newFakeClock()
	sm := orchestrator.NewStateMachine(app, clk, testSessionID, testProjectID)

	if err := sm.Transition(context.Background(), orchestrator.StateInitializing, "boot"); err != nil {
		t.Fatalf("Transition: %v", err)
	}
	if got := sm.Current(); got != orchestrator.StateInitializing {
		t.Errorf("after legal transition current=%v, want initializing", got)
	}
	evts := app.snapshot()
	if len(evts) != 1 {
		t.Fatalf("appended %d events, want 1", len(evts))
	}
	ev := evts[0]
	if ev.Type != eventlog.EvtOrchestratorStateTransition {
		t.Errorf("event type = %v, want EvtOrchestratorStateTransition", ev.Type)
	}
	if ev.SessionID != testSessionID || ev.ProjectID != testProjectID {
		t.Errorf("event identity = (%s, %s), want (%s, %s)", ev.SessionID, ev.ProjectID, testSessionID, testProjectID)
	}
	if !ev.Timestamp.Equal(clk.Now()) {
		t.Errorf("event timestamp = %v, want clock.Now() = %v", ev.Timestamp, clk.Now())
	}
	if ev.Payload["from"] != orchestrator.StateIdle.String() {
		t.Errorf("payload from = %v, want idle", ev.Payload["from"])
	}
	if ev.Payload["to"] != orchestrator.StateInitializing.String() {
		t.Errorf("payload to = %v, want initializing", ev.Payload["to"])
	}
	if ev.Payload["reason"] != "boot" {
		t.Errorf("payload reason = %v, want boot", ev.Payload["reason"])
	}
}

func TestStateMachine_IllegalTransitionRejected(t *testing.T) {
	app := &fakeAppender{}
	sm := orchestrator.NewStateMachine(app, newFakeClock(), testSessionID, testProjectID)

	err := sm.Transition(context.Background(), orchestrator.StateRunning, "skip-init")
	if !errors.Is(err, orchestrator.ErrIllegalTransition) {
		t.Fatalf("err=%v, want ErrIllegalTransition", err)
	}
	if got := sm.Current(); got != orchestrator.StateIdle {
		t.Errorf("after illegal Transition current=%v, want unchanged idle", got)
	}
	if got := app.snapshot(); len(got) != 0 {
		t.Errorf("illegal transition emitted %d events, want 0", len(got))
	}
}

func TestStateMachine_AppenderErrorPropagated(t *testing.T) {
	want := errors.New("boom")
	sm := orchestrator.NewStateMachine(&errAppender{err: want}, newFakeClock(), testSessionID, testProjectID)
	err := sm.Transition(context.Background(), orchestrator.StateInitializing, "boot")
	if !errors.Is(err, want) {
		t.Fatalf("err=%v, want appender err", err)
	}

	if got := sm.Current(); got != orchestrator.StateIdle {
		t.Errorf("after appender err current=%v, want unchanged idle", got)
	}
}

func TestIsLegal_TableMatchesTransitionTable(t *testing.T) {
	for from, tos := range orchestrator.TransitionTable {
		for to := range tos {
			if !orchestrator.IsLegal(from, to) {
				t.Errorf("IsLegal(%s, %s) = false, want true (in TransitionTable)", from, to)
			}
		}
	}
}

func TestIsLegal_RejectsIllegal(t *testing.T) {
	cases := []struct {
		from, to orchestrator.State
	}{
		{orchestrator.StateIdle, orchestrator.StateRunning},
		{orchestrator.StateAborting, orchestrator.StateRunning},
		{orchestrator.StateIdle, orchestrator.StateAborting},
	}
	for _, c := range cases {
		if orchestrator.IsLegal(c.from, c.to) {
			t.Errorf("IsLegal(%s, %s) = true, want false", c.from, c.to)
		}
	}
}

func TestIsLegal_UnknownFromStateReturnsFalse(t *testing.T) {

	if orchestrator.IsLegal(orchestrator.State(99), orchestrator.StateIdle) {
		t.Errorf("IsLegal(State(99), idle) = true, want false (no row)")
	}
}

func TestIsLegal_PureNoSideEffects(t *testing.T) {
	app := &fakeAppender{}
	sm := orchestrator.NewStateMachine(app, newFakeClock(), testSessionID, testProjectID)
	_ = orchestrator.IsLegal(orchestrator.StateRunning, orchestrator.StateAborting)
	if got := sm.Current(); got != orchestrator.StateIdle {
		t.Errorf("IsLegal mutated state: current=%v, want idle", got)
	}
	if got := app.snapshot(); len(got) != 0 {
		t.Errorf("IsLegal emitted %d events, want 0", len(got))
	}
}

func TestStateMachine_ConcurrentTransitionSafety(t *testing.T) {
	const N = 64
	app := &fakeAppender{}
	sm := orchestrator.NewStateMachine(app, newFakeClock(), testSessionID, testProjectID)

	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		wins   int
		losses int
	)
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			err := sm.Transition(context.Background(), orchestrator.StateInitializing, "race")
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				wins++
			} else if errors.Is(err, orchestrator.ErrIllegalTransition) {
				losses++
			} else {
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()

	if wins != 1 {
		t.Errorf("wins=%d, want 1 (exactly-one-winner under contention)", wins)
	}
	if losses != N-1 {
		t.Errorf("losses=%d, want %d", losses, N-1)
	}
	if got := sm.Current(); got != orchestrator.StateInitializing {
		t.Errorf("final current=%v, want initializing", got)
	}
	if got := len(app.snapshot()); got != 1 {
		t.Errorf("appended events=%d, want 1", got)
	}
}

func TestStateMachine_SequentialChain(t *testing.T) {
	app := &fakeAppender{}
	sm := orchestrator.NewStateMachine(app, newFakeClock(), testSessionID, testProjectID)

	chain := []struct {
		to     orchestrator.State
		reason string
	}{
		{orchestrator.StateInitializing, "boot"},
		{orchestrator.StateRunning, "ready"},
		{orchestrator.StateDegradedTier, "cost-80pct"},
		{orchestrator.StateRunning, "cost-recovery"},
		{orchestrator.StateAborting, "operator-stop"},
		{orchestrator.StateIdle, "cleanup-done"},
	}
	for _, step := range chain {
		if err := sm.Transition(context.Background(), step.to, step.reason); err != nil {
			t.Fatalf("Transition→%v: %v", step.to, err)
		}
	}
	if got := sm.Current(); got != orchestrator.StateIdle {
		t.Errorf("end state=%v, want idle", got)
	}
	if got := len(app.snapshot()); got != len(chain) {
		t.Errorf("events=%d, want %d", got, len(chain))
	}
}

func TestStateMachine_TwelveIllegalTransitions(t *testing.T) {
	cases := []struct {
		name  string
		setup []orchestrator.State
		from  orchestrator.State
		to    orchestrator.State
	}{

		{"idle_to_running_skips_init", nil, orchestrator.StateIdle, orchestrator.StateRunning},

		{"idle_to_aborting_no_session", nil, orchestrator.StateIdle, orchestrator.StateAborting},

		{"idle_to_degraded_tier", nil, orchestrator.StateIdle, orchestrator.StateDegradedTier},

		{"idle_to_emergency_tier", nil, orchestrator.StateIdle, orchestrator.StateEmergencyTier},

		{"initializing_to_degraded", []orchestrator.State{orchestrator.StateInitializing}, orchestrator.StateInitializing, orchestrator.StateDegradedTier},

		{"initializing_to_idle_no_abort", []orchestrator.State{orchestrator.StateInitializing}, orchestrator.StateInitializing, orchestrator.StateIdle},

		{"running_to_initializing", []orchestrator.State{orchestrator.StateInitializing, orchestrator.StateRunning}, orchestrator.StateRunning, orchestrator.StateInitializing},

		{"aborting_to_running_terminal_escape", []orchestrator.State{orchestrator.StateInitializing, orchestrator.StateRunning, orchestrator.StateAborting}, orchestrator.StateAborting, orchestrator.StateRunning},

		{"aborting_to_degraded_terminal_escape", []orchestrator.State{orchestrator.StateInitializing, orchestrator.StateRunning, orchestrator.StateAborting}, orchestrator.StateAborting, orchestrator.StateDegradedTier},

		{"recovering_to_idle_skip_islegal", nil, orchestrator.StateRecoveringFromReplay, orchestrator.StateIdle},

		{"emergency_to_running_direct", []orchestrator.State{orchestrator.StateInitializing, orchestrator.StateRunning, orchestrator.StateEmergencyTier}, orchestrator.StateEmergencyTier, orchestrator.StateRunning},

		{"hard_paused_to_degraded_direct", []orchestrator.State{orchestrator.StateInitializing, orchestrator.StateRunning, orchestrator.StateHardPaused}, orchestrator.StateHardPaused, orchestrator.StateDegradedTier},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {

			if c.name == "recovering_to_idle_skip_islegal" {
				if orchestrator.IsLegal(c.from, c.to) {
					t.Fatalf("IsLegal(%s, %s) = true, want false", c.from, c.to)
				}
				return
			}
			app := &fakeAppender{}
			sm := orchestrator.NewStateMachine(app, newFakeClock(), testSessionID, testProjectID)

			for _, step := range c.setup {
				if err := sm.Transition(context.Background(), step, "setup"); err != nil {
					t.Fatalf("setup chain to %v: %v", step, err)
				}
			}
			before := len(app.snapshot())
			err := sm.Transition(context.Background(), c.to, "adversarial-illegal")
			if !errors.Is(err, orchestrator.ErrIllegalTransition) {
				t.Fatalf("err=%v, want ErrIllegalTransition", err)
			}
			if got := sm.Current(); got != c.from {
				t.Errorf("current=%v, want unchanged %v", got, c.from)
			}
			if got := len(app.snapshot()); got != before {
				t.Errorf("events appended on illegal attempt: %d → %d", before, got)
			}
		})
	}
}

func TestStateMachine_CtxCancelledAtEntry(t *testing.T) {
	app := &fakeAppender{}
	sm := orchestrator.NewStateMachine(app, newFakeClock(), testSessionID, testProjectID)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := sm.Transition(ctx, orchestrator.StateInitializing, "boot")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v, want context.Canceled", err)
	}
	if got := sm.Current(); got != orchestrator.StateIdle {
		t.Errorf("after cancelled ctx current=%v, want unchanged idle", got)
	}
	if got := app.snapshot(); len(got) != 0 {
		t.Errorf("cancelled ctx emitted %d events, want 0", len(got))
	}
}
