package orchestrator_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	orch "github.com/cbip-solutions/hades-system/internal/orchestrator"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
	"github.com/cbip-solutions/hades-system/internal/workforce/gate"
)

type confFakeStateMachine struct {
	mu    sync.Mutex
	state orch.State
	allow map[[2]orch.State]bool

	transitionErr error
}

func newConfFakeStateMachine(initial orch.State) *confFakeStateMachine {
	return &confFakeStateMachine{
		state: initial,
		allow: map[[2]orch.State]bool{
			{orch.StateRunning, orch.StateWaitingForConfirmation}:      true,
			{orch.StateWaitingForConfirmation, orch.StateRunning}:      true,
			{orch.StateWaitingForConfirmation, orch.StateAborting}:     true,
			{orch.StateDegradedTier, orch.StateWaitingForConfirmation}: true,
			{orch.StateWaitingForConfirmation, orch.StateDegradedTier}: true,
		},
	}
}

func (f *confFakeStateMachine) Current() orch.State {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state
}

func (f *confFakeStateMachine) Transition(_ context.Context, to orch.State, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.transitionErr != nil {
		return f.transitionErr
	}
	from := f.state
	if !f.allow[[2]orch.State{from, to}] {
		return errors.New("orchestrator/test: illegal transition " + from.String() + "→" + to.String())
	}
	f.state = to
	return nil
}

type confFakeAppender struct {
	mu       sync.Mutex
	counts   map[eventlog.EventType]int
	last     map[eventlog.EventType]eventlog.Event
	appendID int64

	errAfter    int
	calls       int
	errOnAppend error
}

func newConfFakeAppender() *confFakeAppender {
	return &confFakeAppender{
		counts: map[eventlog.EventType]int{},
		last:   map[eventlog.EventType]eventlog.Event{},
	}
}

func (f *confFakeAppender) Append(_ context.Context, ev eventlog.Event) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.errAfter > 0 && f.calls >= f.errAfter && f.errOnAppend != nil {
		return 0, f.errOnAppend
	}
	f.counts[ev.Type]++
	f.last[ev.Type] = ev
	f.appendID++
	return f.appendID, nil
}

func (f *confFakeAppender) Count(t eventlog.EventType) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.counts[t]
}

func (f *confFakeAppender) Last(t eventlog.EventType) eventlog.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.last[t]
}

type confFakeGate struct {
	mu          sync.Mutex
	state       gate.State
	pauseErr    error
	resumeErr   error
	pauseCalls  int
	resumeCalls int
}

func newConfFakeGate(initial gate.State) *confFakeGate {
	return &confFakeGate{state: initial}
}

func (f *confFakeGate) State() gate.State {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state
}

func (f *confFakeGate) Pause(_ context.Context, mode gate.PauseMode, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pauseCalls++
	if f.pauseErr != nil {
		return f.pauseErr
	}
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

func (f *confFakeGate) Resume(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resumeCalls++
	if f.resumeErr != nil {
		return f.resumeErr
	}
	f.state = gate.StateRunning
	return nil
}

const (
	confTestSession = "session-conf-test"
	confTestProject = "project-conf-test"
)

func newHandlerForTest(t *testing.T, p *orch.ConfirmationPolicy, sm orch.StateMachineAPI, ap orch.AppenderAPI, g orch.GateAPI) *orch.ConfirmationHandler {
	t.Helper()
	return orch.NewConfirmationHandler(p, sm, ap, g, confTestSession, confTestProject)
}

func TestRequestConfirmation_HighThreshold_PausesAndAppends(t *testing.T) {
	pol := orch.NewConfirmationPolicy(map[orch.DecisionClass]orch.Threshold{
		orch.DecisionInvariantViolation: orch.ThresholdHigh,
	}, false)
	sm := newConfFakeStateMachine(orch.StateRunning)
	ap := newConfFakeAppender()
	g := newConfFakeGate(gate.StateRunning)

	h := newHandlerForTest(t, pol, sm, ap, g)
	out, err := h.RequestConfirmation(context.Background(), orch.RequestConfirmationInput{
		Class:        orch.DecisionInvariantViolation,
		Summary:      "inv-zen-091 violated",
		Alternatives: []string{"abort", "continue"},
	})
	if err != nil {
		t.Fatalf("RequestConfirmation: %v", err)
	}
	if out.Action != orch.ConfirmationActionMandatoryPause {
		t.Errorf("Action = %v, want ConfirmationActionMandatoryPause", out.Action)
	}
	if out.RequestSeq == 0 {
		t.Error("RequestSeq must be > 0 after mandatory pause")
	}
	if out.EventID == 0 {
		t.Error("EventID must be > 0 after mandatory pause")
	}
	if sm.Current() != orch.StateWaitingForConfirmation {
		t.Errorf("state = %v, want WAITING_FOR_CONFIRMATION", sm.Current())
	}
	if g.State() != gate.StatePausedDescriptive {
		t.Errorf("gate = %v, want PausedDescriptive", g.State())
	}
	if got := ap.Count(eventlog.EvtConfirmationRequested); got != 1 {
		t.Errorf("ConfirmationRequested events = %d, want 1", got)
	}

	last := ap.Last(eventlog.EvtConfirmationRequested)
	if last.SessionID != confTestSession {
		t.Errorf("event SessionID = %q, want %q", last.SessionID, confTestSession)
	}
	if last.ProjectID != confTestProject {
		t.Errorf("event ProjectID = %q, want %q", last.ProjectID, confTestProject)
	}
	if last.Timestamp.IsZero() {
		t.Error("event Timestamp must be non-zero")
	}
	pl, ok := last.Payload["request_seq"]
	if !ok {
		t.Fatalf("Payload missing request_seq; got keys: %v", keysOf(last.Payload))
	}

	if rs, ok := pl.(uint64); ok {
		if rs != out.RequestSeq {
			t.Errorf("Payload request_seq = %d, want %d", rs, out.RequestSeq)
		}
	}
	if got, _ := last.Payload["decision_class"].(string); got != string(orch.DecisionInvariantViolation) {
		t.Errorf("Payload decision_class = %q, want %q", got, string(orch.DecisionInvariantViolation))
	}
	if got, _ := last.Payload["summary"].(string); got != "inv-zen-091 violated" {
		t.Errorf("Payload summary = %q, want %q", got, "inv-zen-091 violated")
	}
}

func TestRequestConfirmation_LowThreshold_DoesNotPause(t *testing.T) {
	pol := orch.NewConfirmationPolicy(map[orch.DecisionClass]orch.Threshold{
		orch.DecisionBudgetBreach: orch.ThresholdLow,
	}, false)
	sm := newConfFakeStateMachine(orch.StateRunning)
	ap := newConfFakeAppender()
	g := newConfFakeGate(gate.StateRunning)

	h := newHandlerForTest(t, pol, sm, ap, g)
	out, err := h.RequestConfirmation(context.Background(), orch.RequestConfirmationInput{
		Class: orch.DecisionBudgetBreach,
	})
	if err != nil {
		t.Fatalf("RequestConfirmation: %v", err)
	}
	if out.Action != orch.ConfirmationActionContinue {
		t.Errorf("Action = %v, want ConfirmationActionContinue", out.Action)
	}
	if out.RequestSeq != 0 {
		t.Errorf("RequestSeq = %d, want 0", out.RequestSeq)
	}
	if out.EventID != 0 {
		t.Errorf("EventID = %d, want 0", out.EventID)
	}
	if sm.Current() != orch.StateRunning {
		t.Error("state must remain RUNNING for low threshold")
	}
	if g.State() != gate.StateRunning {
		t.Error("gate must remain Running for low threshold")
	}
	if ap.Count(eventlog.EvtConfirmationRequested) != 0 {
		t.Error("no ConfirmationRequested expected for ConfirmationActionContinue")
	}
}

func TestRequestConfirmation_OptionalPauseEnabled_TriggersPause(t *testing.T) {
	pol := orch.NewConfirmationPolicy(map[orch.DecisionClass]orch.Threshold{
		orch.DecisionBudgetBreach: orch.ThresholdMedium,
	}, true)
	sm := newConfFakeStateMachine(orch.StateRunning)
	ap := newConfFakeAppender()
	g := newConfFakeGate(gate.StateRunning)

	h := newHandlerForTest(t, pol, sm, ap, g)
	out, err := h.RequestConfirmation(context.Background(), orch.RequestConfirmationInput{
		Class:   orch.DecisionBudgetBreach,
		Summary: "75% budget",
	})
	if err != nil {
		t.Fatalf("RequestConfirmation: %v", err)
	}
	if out.Action != orch.ConfirmationActionOptionalPause {
		t.Errorf("Action = %v, want ConfirmationActionOptionalPause", out.Action)
	}
	if out.RequestSeq == 0 {
		t.Error("RequestSeq must be > 0 after optional pause")
	}
	if sm.Current() != orch.StateWaitingForConfirmation {
		t.Errorf("state = %v, want WAITING_FOR_CONFIRMATION (optional pause still pauses)", sm.Current())
	}
	if g.State() != gate.StatePausedDescriptive {
		t.Errorf("gate = %v, want PausedDescriptive (optional pause still locks gate)", g.State())
	}
	if got := ap.Count(eventlog.EvtConfirmationRequested); got != 1 {
		t.Errorf("ConfirmationRequested events = %d, want 1 (optional still emits)", got)
	}
}

func TestRequestConfirmation_TransitionFails_RollsBackGate(t *testing.T) {
	pol := orch.NewConfirmationPolicy(map[orch.DecisionClass]orch.Threshold{
		orch.DecisionBudgetBreach: orch.ThresholdHigh,
	}, false)

	sm := newConfFakeStateMachine(orch.StateAborting)
	ap := newConfFakeAppender()
	g := newConfFakeGate(gate.StateRunning)

	h := newHandlerForTest(t, pol, sm, ap, g)
	_, err := h.RequestConfirmation(context.Background(), orch.RequestConfirmationInput{
		Class: orch.DecisionBudgetBreach,
	})
	if err == nil {
		t.Fatal("expected error from invalid transition")
	}
	if !errors.Is(err, orch.ErrInvalidTransition) {
		t.Errorf("error chain missing ErrInvalidTransition: %v", err)
	}
	if g.State() != gate.StateRunning {
		t.Errorf("gate must NOT be paused (transition failed before gate.Pause); got %v", g.State())
	}
	if g.pauseCalls != 0 {
		t.Errorf("gate.Pause must not be called when transition fails; got %d calls", g.pauseCalls)
	}
	if sm.Current() != orch.StateAborting {
		t.Errorf("state must remain Aborting on transition failure; got %v", sm.Current())
	}
	if ap.Count(eventlog.EvtConfirmationRequested) != 0 {
		t.Error("no ConfirmationRequested expected when transition fails")
	}
}

func TestRequestConfirmation_GatePauseFails_RollsBackState(t *testing.T) {
	pol := orch.NewConfirmationPolicy(map[orch.DecisionClass]orch.Threshold{
		orch.DecisionBudgetBreach: orch.ThresholdHigh,
	}, false)
	sm := newConfFakeStateMachine(orch.StateRunning)
	ap := newConfFakeAppender()
	g := newConfFakeGate(gate.StateRunning)
	g.pauseErr = errors.New("gate persist failed")

	h := newHandlerForTest(t, pol, sm, ap, g)
	_, err := h.RequestConfirmation(context.Background(), orch.RequestConfirmationInput{
		Class: orch.DecisionBudgetBreach,
	})
	if err == nil {
		t.Fatal("expected error from gate.Pause failure")
	}
	if !errors.Is(err, g.pauseErr) {
		t.Errorf("error chain missing gate.Pause err: %v", err)
	}

	if sm.Current() != orch.StateRunning {
		t.Errorf("state must roll back to Running; got %v", sm.Current())
	}
	if g.State() != gate.StateRunning {
		t.Errorf("gate must remain Running on Pause failure; got %v", g.State())
	}
	if ap.Count(eventlog.EvtConfirmationRequested) != 0 {
		t.Error("no ConfirmationRequested expected when gate.Pause fails")
	}
}

func TestRequestConfirmation_AppendFails_RollsBackGateAndState(t *testing.T) {
	pol := orch.NewConfirmationPolicy(map[orch.DecisionClass]orch.Threshold{
		orch.DecisionBudgetBreach: orch.ThresholdHigh,
	}, false)
	sm := newConfFakeStateMachine(orch.StateRunning)
	ap := newConfFakeAppender()
	ap.errAfter = 1
	ap.errOnAppend = errors.New("disk full")
	g := newConfFakeGate(gate.StateRunning)

	h := newHandlerForTest(t, pol, sm, ap, g)
	_, err := h.RequestConfirmation(context.Background(), orch.RequestConfirmationInput{
		Class: orch.DecisionBudgetBreach,
	})
	if err == nil {
		t.Fatal("expected error from appender failure")
	}
	if !errors.Is(err, ap.errOnAppend) {
		t.Errorf("error chain missing appender err: %v", err)
	}
	if sm.Current() != orch.StateRunning {
		t.Errorf("state must roll back to Running on append failure; got %v", sm.Current())
	}
	if g.State() != gate.StateRunning {
		t.Errorf("gate must roll back to Running on append failure; got %v", g.State())
	}
	if g.resumeCalls != 1 {
		t.Errorf("gate.Resume must be called exactly once on append rollback; got %d", g.resumeCalls)
	}
}

func TestRequestConfirmation_AlreadyPending_ReturnsExistingIDs(t *testing.T) {
	pol := orch.NewConfirmationPolicy(map[orch.DecisionClass]orch.Threshold{
		orch.DecisionInvariantViolation: orch.ThresholdHigh,
	}, false)
	sm := newConfFakeStateMachine(orch.StateRunning)
	ap := newConfFakeAppender()
	g := newConfFakeGate(gate.StateRunning)

	h := newHandlerForTest(t, pol, sm, ap, g)
	out1, err := h.RequestConfirmation(context.Background(), orch.RequestConfirmationInput{
		Class:   orch.DecisionInvariantViolation,
		Summary: "first",
	})
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if out1.RequestSeq == 0 {
		t.Fatal("first call should return RequestSeq > 0")
	}

	out2, err := h.RequestConfirmation(context.Background(), orch.RequestConfirmationInput{
		Class:   orch.DecisionInvariantViolation,
		Summary: "second (should be no-op)",
	})
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if out2.RequestSeq != out1.RequestSeq {
		t.Errorf("second-call RequestSeq = %d, want %d (same as first)", out2.RequestSeq, out1.RequestSeq)
	}
	if out2.EventID != out1.EventID {
		t.Errorf("second-call EventID = %d, want %d (same as first)", out2.EventID, out1.EventID)
	}
	if out2.Action != orch.ConfirmationActionMandatoryPause {
		t.Errorf("second-call Action = %v, want MandatoryPause", out2.Action)
	}

	if got := ap.Count(eventlog.EvtConfirmationRequested); got != 1 {
		t.Errorf("ConfirmationRequested events = %d, want 1 (no double-emit on already-pending)", got)
	}
	if g.pauseCalls != 1 {
		t.Errorf("gate.Pause calls = %d, want 1 (no double-pause on already-pending)", g.pauseCalls)
	}
}

func TestRequestConfirmation_RaceFree(t *testing.T) {
	pol := orch.NewConfirmationPolicy(map[orch.DecisionClass]orch.Threshold{
		orch.DecisionInvariantViolation: orch.ThresholdHigh,
	}, false)
	sm := newConfFakeStateMachine(orch.StateRunning)
	ap := newConfFakeAppender()
	g := newConfFakeGate(gate.StateRunning)

	h := newHandlerForTest(t, pol, sm, ap, g)

	const N = 16
	results := make([]orch.RequestConfirmationOutput, N)
	errs := make([]error, N)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			results[idx], errs[idx] = h.RequestConfirmation(context.Background(), orch.RequestConfirmationInput{
				Class:   orch.DecisionInvariantViolation,
				Summary: "race",
			})
		}(i)
	}
	close(start)
	wg.Wait()

	var winnerSeq uint64
	var winnerEventID int64
	for i, e := range errs {
		if e != nil {
			t.Errorf("call %d: unexpected error %v", i, e)
		}
		if results[i].RequestSeq == 0 {
			t.Errorf("call %d: RequestSeq=0", i)
		}
		if winnerSeq == 0 {
			winnerSeq = results[i].RequestSeq
			winnerEventID = results[i].EventID
			continue
		}
		if results[i].RequestSeq != winnerSeq {
			t.Errorf("call %d: RequestSeq=%d, want all calls to share %d", i, results[i].RequestSeq, winnerSeq)
		}
		if results[i].EventID != winnerEventID {
			t.Errorf("call %d: EventID=%d, want %d", i, results[i].EventID, winnerEventID)
		}
	}

	if got := ap.Count(eventlog.EvtConfirmationRequested); got != 1 {
		t.Errorf("ConfirmationRequested events = %d, want 1 across %d concurrent calls", got, N)
	}
	if g.pauseCalls != 1 {
		t.Errorf("gate.Pause calls = %d, want 1 across %d concurrent calls", g.pauseCalls, N)
	}
}

func TestRequestConfirmation_CtxCancelled(t *testing.T) {
	pol := orch.NewConfirmationPolicy(map[orch.DecisionClass]orch.Threshold{
		orch.DecisionInvariantViolation: orch.ThresholdHigh,
	}, false)
	sm := newConfFakeStateMachine(orch.StateRunning)
	ap := newConfFakeAppender()
	g := newConfFakeGate(gate.StateRunning)

	h := newHandlerForTest(t, pol, sm, ap, g)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := h.RequestConfirmation(ctx, orch.RequestConfirmationInput{
		Class: orch.DecisionInvariantViolation,
	})
	if err == nil {
		t.Fatal("expected error from cancelled ctx")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error chain missing context.Canceled: %v", err)
	}

	if sm.Current() != orch.StateRunning {
		t.Errorf("state mutated despite cancelled ctx: %v", sm.Current())
	}
	if g.pauseCalls != 0 {
		t.Errorf("gate.Pause called despite cancelled ctx: %d", g.pauseCalls)
	}
	if ap.Count(eventlog.EvtConfirmationRequested) != 0 {
		t.Error("event emitted despite cancelled ctx")
	}
}

func TestNewConfirmationHandler_PanicOnNil(t *testing.T) {
	validPolicy := orch.NewConfirmationPolicy(nil, false)
	validSM := newConfFakeStateMachine(orch.StateRunning)
	validAP := newConfFakeAppender()
	validG := newConfFakeGate(gate.StateRunning)

	cases := []struct {
		name      string
		build     func() *orch.ConfirmationHandler
		wantPanic string
	}{
		{
			name: "nil policy",
			build: func() *orch.ConfirmationHandler {
				return orch.NewConfirmationHandler(nil, validSM, validAP, validG, "s", "p")
			},
			wantPanic: "policy",
		},
		{
			name: "nil state machine",
			build: func() *orch.ConfirmationHandler {
				return orch.NewConfirmationHandler(validPolicy, nil, validAP, validG, "s", "p")
			},
			wantPanic: "state machine",
		},
		{
			name: "nil appender",
			build: func() *orch.ConfirmationHandler {
				return orch.NewConfirmationHandler(validPolicy, validSM, nil, validG, "s", "p")
			},
			wantPanic: "appender",
		},
		{
			name: "nil gate",
			build: func() *orch.ConfirmationHandler {
				return orch.NewConfirmationHandler(validPolicy, validSM, validAP, nil, "s", "p")
			},
			wantPanic: "gate",
		},
		{
			name: "empty sessionID",
			build: func() *orch.ConfirmationHandler {
				return orch.NewConfirmationHandler(validPolicy, validSM, validAP, validG, "", "p")
			},
			wantPanic: "sessionID",
		},
		{
			name: "empty projectID",
			build: func() *orch.ConfirmationHandler {
				return orch.NewConfirmationHandler(validPolicy, validSM, validAP, validG, "s", "")
			},
			wantPanic: "projectID",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatalf("expected panic for %q, got none", c.name)
				}
				msg, ok := r.(string)
				if !ok {
					t.Fatalf("panic value type = %T, want string", r)
				}
				if !contains(msg, c.wantPanic) {
					t.Errorf("panic message %q missing %q", msg, c.wantPanic)
				}
			}()
			_ = c.build()
		})
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func highPausePolicy() *orch.ConfirmationPolicy {
	return orch.NewConfirmationPolicy(map[orch.DecisionClass]orch.Threshold{
		orch.DecisionInvariantViolation: orch.ThresholdHigh,
	}, false)
}

func setupPendingRequest(t *testing.T, h *orch.ConfirmationHandler) orch.RequestConfirmationOutput {
	t.Helper()
	out, err := h.RequestConfirmation(context.Background(), orch.RequestConfirmationInput{
		Class:   orch.DecisionInvariantViolation,
		Summary: "test pause",
	})
	if err != nil {
		t.Fatalf("RequestConfirmation (setup): %v", err)
	}
	if out.EventID == 0 {
		t.Fatal("setup: EventID must be > 0")
	}
	return out
}

func TestHandleAck_HappyPath(t *testing.T) {
	sm := newConfFakeStateMachine(orch.StateRunning)
	ap := newConfFakeAppender()
	g := newConfFakeGate(gate.StateRunning)
	h := newHandlerForTest(t, highPausePolicy(), sm, ap, g)

	out := setupPendingRequest(t, h)

	op := orch.OperatorIdentity{UID: 1000, Reason: "approved"}
	err := h.HandleAck(context.Background(), orch.AckInput{
		EventID:   out.EventID,
		Rationale: "LGTM",
		Operator:  op,
	})
	if err != nil {
		t.Fatalf("HandleAck: %v", err)
	}
	if sm.Current() != orch.StateRunning {
		t.Errorf("state = %v, want Running after ack", sm.Current())
	}
	if g.State() != gate.StateRunning {
		t.Errorf("gate = %v, want Running after ack", g.State())
	}
	if got := ap.Count(eventlog.EvtOperatorConfirmation); got != 1 {
		t.Errorf("OperatorConfirmation events = %d, want 1", got)
	}

	last := ap.Last(eventlog.EvtOperatorConfirmation)
	if last.SessionID != confTestSession {
		t.Errorf("event SessionID = %q, want %q", last.SessionID, confTestSession)
	}
	raw, _ := last.Payload["_typed_payload"]
	if typed, ok := raw.(eventlog.OperatorConfirmation); ok {
		if typed.Decision != "ack" {
			t.Errorf("payload Decision = %q, want \"ack\"", typed.Decision)
		}
		if typed.OperatorUID != op.UID {
			t.Errorf("payload OperatorUID = %d, want %d", typed.OperatorUID, op.UID)
		}
	}

	if dec, _ := last.Payload["decision"].(string); dec != "ack" {
		t.Errorf("Payload[decision] = %q, want \"ack\"", dec)
	}
}

func TestHandleDeny_TransitionsToAborting(t *testing.T) {
	sm := newConfFakeStateMachine(orch.StateRunning)
	ap := newConfFakeAppender()
	g := newConfFakeGate(gate.StateRunning)
	h := newHandlerForTest(t, highPausePolicy(), sm, ap, g)

	out := setupPendingRequest(t, h)

	err := h.HandleDeny(context.Background(), orch.DenyInput{
		EventID:   out.EventID,
		Rationale: "rejected",
		Operator:  orch.OperatorIdentity{UID: 0},
	})
	if err != nil {
		t.Fatalf("HandleDeny: %v", err)
	}
	if sm.Current() != orch.StateAborting {
		t.Errorf("state = %v, want Aborting after deny", sm.Current())
	}

	if g.State() == gate.StateRunning {
		t.Errorf("gate must NOT be resumed after deny; got Running")
	}
	if g.resumeCalls != 0 {
		t.Errorf("gate.Resume calls = %d, want 0 after deny", g.resumeCalls)
	}
	if got := ap.Count(eventlog.EvtOperatorConfirmation); got != 1 {
		t.Errorf("OperatorConfirmation events = %d, want 1", got)
	}
	if dec, _ := ap.Last(eventlog.EvtOperatorConfirmation).Payload["decision"].(string); dec != "deny" {
		t.Errorf("Payload[decision] = %q, want \"deny\"", dec)
	}
}

func TestHandleAck_StaleEventID_RejectsWithoutSideEffect(t *testing.T) {
	sm := newConfFakeStateMachine(orch.StateRunning)
	ap := newConfFakeAppender()
	g := newConfFakeGate(gate.StateRunning)
	h := newHandlerForTest(t, highPausePolicy(), sm, ap, g)

	out := setupPendingRequest(t, h)
	wrongEventID := out.EventID + 999

	err := h.HandleAck(context.Background(), orch.AckInput{
		EventID:   wrongEventID,
		Rationale: "wrong",
		Operator:  orch.OperatorIdentity{UID: 0},
	})
	if !errors.Is(err, orch.ErrConfirmationStale) {
		t.Errorf("expected ErrConfirmationStale, got: %v", err)
	}

	if sm.Current() != orch.StateWaitingForConfirmation {
		t.Errorf("state = %v, want WaitingForConfirmation (stale ack must not mutate)", sm.Current())
	}

	if g.State() == gate.StateRunning {
		t.Error("gate must NOT be resumed on stale ack")
	}
	if g.resumeCalls != 0 {
		t.Errorf("gate.Resume calls = %d, want 0 on stale ack", g.resumeCalls)
	}

	if got := ap.Count(eventlog.EvtOperatorConfirmation); got != 0 {
		t.Errorf("OperatorConfirmation events = %d, want 0 on stale ack", got)
	}

	err2 := h.HandleAck(context.Background(), orch.AckInput{
		EventID:  out.EventID,
		Operator: orch.OperatorIdentity{UID: 0},
	})
	if err2 != nil {
		t.Errorf("correct ack after stale rejection failed: %v", err2)
	}
}

func TestHandleAck_NoPending_ReturnsStale(t *testing.T) {
	sm := newConfFakeStateMachine(orch.StateRunning)
	ap := newConfFakeAppender()
	g := newConfFakeGate(gate.StateRunning)
	h := newHandlerForTest(t, highPausePolicy(), sm, ap, g)

	err := h.HandleAck(context.Background(), orch.AckInput{
		EventID:   42,
		Rationale: "nobody home",
	})
	if !errors.Is(err, orch.ErrConfirmationStale) {
		t.Errorf("expected ErrConfirmationStale on no-pending ack, got: %v", err)
	}
}

func TestHandleDeny_StaleEventID_RejectsWithoutSideEffect(t *testing.T) {
	sm := newConfFakeStateMachine(orch.StateRunning)
	ap := newConfFakeAppender()
	g := newConfFakeGate(gate.StateRunning)
	h := newHandlerForTest(t, highPausePolicy(), sm, ap, g)

	out := setupPendingRequest(t, h)
	wrongEventID := out.EventID + 999

	err := h.HandleDeny(context.Background(), orch.DenyInput{
		EventID:   wrongEventID,
		Rationale: "wrong",
	})
	if !errors.Is(err, orch.ErrConfirmationStale) {
		t.Errorf("expected ErrConfirmationStale, got: %v", err)
	}
	if sm.Current() != orch.StateWaitingForConfirmation {
		t.Errorf("state = %v, want WaitingForConfirmation (stale deny must not mutate)", sm.Current())
	}
	if g.resumeCalls != 0 {
		t.Errorf("gate.Resume calls = %d, want 0 on stale deny", g.resumeCalls)
	}
	if got := ap.Count(eventlog.EvtOperatorConfirmation); got != 0 {
		t.Errorf("OperatorConfirmation events = %d, want 0 on stale deny", got)
	}
}

func TestHandleAck_AppendFails_PendingIntact(t *testing.T) {
	sm := newConfFakeStateMachine(orch.StateRunning)
	ap := newConfFakeAppender()
	g := newConfFakeGate(gate.StateRunning)
	h := newHandlerForTest(t, highPausePolicy(), sm, ap, g)

	out := setupPendingRequest(t, h)

	ap.errAfter = 2
	ap.errOnAppend = errors.New("disk full")

	err := h.HandleAck(context.Background(), orch.AckInput{
		EventID:   out.EventID,
		Rationale: "try",
	})
	if err == nil {
		t.Fatal("expected error from appender failure")
	}
	if !errors.Is(err, ap.errOnAppend) {
		t.Errorf("error chain missing appender err: %v", err)
	}

	if sm.Current() != orch.StateWaitingForConfirmation {
		t.Errorf("state = %v, want WaitingForConfirmation (pending intact on append failure)", sm.Current())
	}

	ap.errAfter = 0
	ap.errOnAppend = nil
	err2 := h.HandleAck(context.Background(), orch.AckInput{
		EventID:   out.EventID,
		Rationale: "retry",
	})
	if err2 != nil {
		t.Errorf("retry ack after append-failure should succeed: %v", err2)
	}
}

func TestHandleAck_OperatorUID_PropagatesToEvent(t *testing.T) {
	sm := newConfFakeStateMachine(orch.StateRunning)
	ap := newConfFakeAppender()
	g := newConfFakeGate(gate.StateRunning)
	h := newHandlerForTest(t, highPausePolicy(), sm, ap, g)

	out := setupPendingRequest(t, h)

	const wantUID = 501
	err := h.HandleAck(context.Background(), orch.AckInput{
		EventID:   out.EventID,
		Rationale: "uid-test",
		Operator:  orch.OperatorIdentity{UID: wantUID, Reason: "test"},
	})
	if err != nil {
		t.Fatalf("HandleAck: %v", err)
	}

	last := ap.Last(eventlog.EvtOperatorConfirmation)

	if uid, ok := last.Payload["operator_uid"].(int); ok {
		if uid != wantUID {
			t.Errorf("Payload[operator_uid] = %d, want %d", uid, wantUID)
		}
	} else {
		t.Errorf("Payload[operator_uid] missing or wrong type; Payload = %v", last.Payload)
	}
}

func TestHandleAck_Race(t *testing.T) {
	sm := newConfFakeStateMachine(orch.StateRunning)
	ap := newConfFakeAppender()
	g := newConfFakeGate(gate.StateRunning)
	h := newHandlerForTest(t, highPausePolicy(), sm, ap, g)

	out := setupPendingRequest(t, h)

	const N = 16
	errs := make([]error, N)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start

			eid := out.EventID
			if idx != 0 {
				eid = out.EventID + 1
			}
			errs[idx] = h.HandleAck(context.Background(), orch.AckInput{
				EventID:  eid,
				Operator: orch.OperatorIdentity{UID: idx},
			})
		}(i)
	}
	close(start)
	wg.Wait()

	successCount := 0
	staleCount := 0
	for i, err := range errs {
		if err == nil {
			successCount++
		} else if errors.Is(err, orch.ErrConfirmationStale) {
			staleCount++
		} else {
			t.Errorf("call %d: unexpected error %v", i, err)
		}
	}
	if successCount != 1 {
		t.Errorf("successCount = %d, want exactly 1", successCount)
	}
	if staleCount != N-1 {
		t.Errorf("staleCount = %d, want %d", staleCount, N-1)
	}
	if sm.Current() != orch.StateRunning {
		t.Errorf("state = %v, want Running after single successful ack", sm.Current())
	}
	if got := ap.Count(eventlog.EvtOperatorConfirmation); got != 1 {
		t.Errorf("OperatorConfirmation events = %d, want exactly 1", got)
	}
}

func TestHandleAck_AfterAck_ReturnsStale(t *testing.T) {
	sm := newConfFakeStateMachine(orch.StateRunning)
	ap := newConfFakeAppender()
	g := newConfFakeGate(gate.StateRunning)
	h := newHandlerForTest(t, highPausePolicy(), sm, ap, g)

	out := setupPendingRequest(t, h)

	if err := h.HandleAck(context.Background(), orch.AckInput{EventID: out.EventID}); err != nil {
		t.Fatalf("first HandleAck: %v", err)
	}

	err := h.HandleAck(context.Background(), orch.AckInput{EventID: out.EventID})
	if !errors.Is(err, orch.ErrConfirmationStale) {
		t.Errorf("expected ErrConfirmationStale on post-ack retry, got: %v", err)
	}
}

func TestHandleAck_AuditEmittedBeforeTransition(t *testing.T) {
	sm := newConfFakeStateMachine(orch.StateRunning)
	ap := newConfFakeAppender()
	g := newConfFakeGate(gate.StateRunning)
	h := newHandlerForTest(t, highPausePolicy(), sm, ap, g)

	out := setupPendingRequest(t, h)

	transErr := errors.New("sm: transition rejected")
	sm.mu.Lock()
	sm.transitionErr = transErr
	sm.mu.Unlock()

	err := h.HandleAck(context.Background(), orch.AckInput{
		EventID:   out.EventID,
		Rationale: "audit-first",
	})
	if err == nil {
		t.Fatal("expected error from transition failure")
	}
	if !errors.Is(err, transErr) {
		t.Errorf("error chain missing transErr: %v", err)
	}

	if got := ap.Count(eventlog.EvtOperatorConfirmation); got != 1 {
		t.Errorf("OperatorConfirmation events = %d, want 1 (append before transition)", got)
	}

	if sm.Current() != orch.StateWaitingForConfirmation {
		t.Errorf("state = %v, want WaitingForConfirmation after transition failure", sm.Current())
	}

	sm.mu.Lock()
	sm.transitionErr = nil
	sm.mu.Unlock()

	err2 := h.HandleAck(context.Background(), orch.AckInput{
		EventID:   out.EventID,
		Rationale: "retry after transition fixed",
	})
	if err2 != nil {
		t.Errorf("retry ack should succeed after transition error cleared: %v", err2)
	}
}

func TestHandleAck_ResumeFails_PendingIntact(t *testing.T) {
	sm := newConfFakeStateMachine(orch.StateRunning)
	ap := newConfFakeAppender()
	g := newConfFakeGate(gate.StateRunning)
	h := newHandlerForTest(t, highPausePolicy(), sm, ap, g)

	out := setupPendingRequest(t, h)

	resumeErr := errors.New("resume: persistence failed")
	g.mu.Lock()
	g.resumeErr = resumeErr
	g.mu.Unlock()

	err := h.HandleAck(context.Background(), orch.AckInput{
		EventID:   out.EventID,
		Rationale: "resume-fail-test",
	})
	if err == nil {
		t.Fatal("expected error from gate.Resume failure")
	}
	if !errors.Is(err, resumeErr) {
		t.Errorf("error chain missing resumeErr: %v", err)
	}

	if got := ap.Count(eventlog.EvtOperatorConfirmation); got != 1 {
		t.Errorf("OperatorConfirmation events = %d, want 1 (appended before resume attempt)", got)
	}

	err2 := h.HandleAck(context.Background(), orch.AckInput{
		EventID:   out.EventID,
		Rationale: "probe pending intact",
	})

	if errors.Is(err2, orch.ErrConfirmationStale) {
		t.Errorf("pending was cleared after resume failure; expected it to survive for operator retry")
	}
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

type noopGatePersist struct {
	mu sync.Mutex
	s  gate.State
}

func newNoopGatePersist(initial gate.State) *noopGatePersist {
	return &noopGatePersist{s: initial}
}

func (p *noopGatePersist) LoadState(ctx context.Context) (gate.State, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.s, nil
}

func (p *noopGatePersist) SaveState(ctx context.Context, s gate.State, reason string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.s = s
	return nil
}

func (p *noopGatePersist) CurrentState() gate.State {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.s
}

func TestRequestConfirmation_RealOperatorGate_PauseResumeRoundtrip(t *testing.T) {
	ctx := context.Background()

	persist := newNoopGatePersist(gate.StateRunning)
	realGate, err := gate.NewOperatorGate(ctx, persist)
	if err != nil {
		t.Fatalf("gate.NewOperatorGate: %v", err)
	}

	if realGate.State() != gate.StateRunning {
		t.Fatalf("pre: realGate.State() = %v, want StateRunning", realGate.State())
	}
	if realGate.IsPaused(gate.ScopeWorkerDispatch) {
		t.Fatal("pre: realGate.IsPaused(ScopeWorkerDispatch) must be false when Running")
	}
	if realGate.IsPaused(gate.ScopeLLMPreCall) {
		t.Fatal("pre: realGate.IsPaused(ScopeLLMPreCall) must be false when Running")
	}

	pol := orch.NewConfirmationPolicy(map[orch.DecisionClass]orch.Threshold{
		orch.DecisionSpecAmendmentProposal: orch.ThresholdHigh,
	}, false)
	sm := newConfFakeStateMachine(orch.StateRunning)
	ap := newConfFakeAppender()
	h := newHandlerForTest(t, pol, sm, ap, realGate)

	out, err := h.RequestConfirmation(ctx, orch.RequestConfirmationInput{
		Class:   orch.DecisionSpecAmendmentProposal,
		Summary: "plan amendment",
	})
	if err != nil {
		t.Fatalf("RequestConfirmation: %v", err)
	}
	if out.Action != orch.ConfirmationActionMandatoryPause {
		t.Errorf("Action = %v, want ConfirmationActionMandatoryPause", out.Action)
	}

	if realGate.State() != gate.StatePausedDescriptive {
		t.Errorf("post-RequestConfirmation: realGate.State() = %v, want StatePausedDescriptive", realGate.State())
	}
	if !realGate.IsPaused(gate.ScopeWorkerDispatch) {
		t.Error("post-RequestConfirmation: realGate.IsPaused(ScopeWorkerDispatch) must be true")
	}
	if !realGate.IsPaused(gate.ScopeLLMPreCall) {
		t.Error("post-RequestConfirmation: realGate.IsPaused(ScopeLLMPreCall) must be true")
	}

	if persist.CurrentState() != gate.StatePausedDescriptive {
		t.Errorf("persist.CurrentState() = %v, want StatePausedDescriptive", persist.CurrentState())
	}

	err = h.HandleAck(ctx, orch.AckInput{
		EventID:   out.EventID,
		Rationale: "acked",
		Operator:  orch.OperatorIdentity{UID: 1000},
	})
	if err != nil {
		t.Fatalf("HandleAck: %v", err)
	}

	if realGate.State() != gate.StateRunning {
		t.Errorf("post-HandleAck: realGate.State() = %v, want StateRunning", realGate.State())
	}
	if realGate.IsPaused(gate.ScopeWorkerDispatch) {
		t.Error("post-HandleAck: realGate.IsPaused(ScopeWorkerDispatch) must be false")
	}
	if realGate.IsPaused(gate.ScopeLLMPreCall) {
		t.Error("post-HandleAck: realGate.IsPaused(ScopeLLMPreCall) must be false")
	}

	if persist.CurrentState() != gate.StateRunning {
		t.Errorf("post-HandleAck: persist.CurrentState() = %v, want StateRunning", persist.CurrentState())
	}

	log := realGate.TransitionLog()
	if len(log) < 2 {
		t.Fatalf("realGate.TransitionLog() has %d entries, want at least 2 (pause + resume)", len(log))
	}
	if log[0].From != gate.StateRunning || log[0].ToState != gate.StatePausedDescriptive {
		t.Errorf("log[0]: %v → %v, want Running → PausedDescriptive", log[0].From, log[0].ToState)
	}
	if log[1].From != gate.StatePausedDescriptive || log[1].ToState != gate.StateRunning {
		t.Errorf("log[1]: %v → %v, want PausedDescriptive → Running", log[1].From, log[1].ToState)
	}
}
