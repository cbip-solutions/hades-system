package gate_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/workforce/gate"
)

type noopGatePersist struct {
	state gate.State
}

func (n *noopGatePersist) LoadState(_ context.Context) (gate.State, error) {
	return n.state, nil
}
func (n *noopGatePersist) SaveState(_ context.Context, s gate.State, _ string) error {
	n.state = s
	return nil
}

func TestInitialStateIsRunning(t *testing.T) {
	p := &noopGatePersist{}
	g, err := gate.NewOperatorGate(context.Background(), p)
	if err != nil {
		t.Fatalf("NewOperatorGate: %v", err)
	}
	if got := g.State(); got != gate.StateRunning {
		t.Errorf("initial State = %v, want %v", got, gate.StateRunning)
	}
}

func TestPauseDescriptive(t *testing.T) {
	p := &noopGatePersist{}
	g, _ := gate.NewOperatorGate(context.Background(), p)

	if err := g.Pause(context.Background(), gate.PauseDescriptive, "operator asked"); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	if got := g.State(); got != gate.StatePausedDescriptive {
		t.Errorf("State = %v, want StatePausedDescriptive", got)
	}
	if !g.IsPaused(gate.ScopeWorkerDispatch) {
		t.Error("IsPaused(ScopeWorkerDispatch) = false, want true")
	}
	if !g.IsPaused(gate.ScopeLLMPreCall) {
		t.Error("IsPaused(ScopeLLMPreCall) = false, want true")
	}
	if !g.IsPaused(gate.ScopeAfterCommit) {
		t.Error("IsPaused(ScopeAfterCommit) = false, want true")
	}
}

func TestPauseQuiet(t *testing.T) {
	p := &noopGatePersist{}
	g, _ := gate.NewOperatorGate(context.Background(), p)

	_ = g.Pause(context.Background(), gate.PauseQuiet, "anomaly z>4")
	if got := g.State(); got != gate.StatePausedQuiet {
		t.Errorf("State = %v, want StatePausedQuiet", got)
	}
	if !g.IsPaused(gate.ScopeWorkerDispatch) {
		t.Error("IsPaused should be true for quiet pause")
	}
}

func TestPauseAfterApply(t *testing.T) {
	p := &noopGatePersist{}
	g, _ := gate.NewOperatorGate(context.Background(), p)

	_ = g.Pause(context.Background(), gate.PauseAfterApply, "before merge review")
	if got := g.State(); got != gate.StatePausedAfterApply {
		t.Errorf("State = %v, want StatePausedAfterApply", got)
	}
}

func TestResume(t *testing.T) {
	p := &noopGatePersist{}
	g, _ := gate.NewOperatorGate(context.Background(), p)

	_ = g.Pause(context.Background(), gate.PauseDescriptive, "test")
	if err := g.Resume(context.Background()); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if got := g.State(); got != gate.StateRunning {
		t.Errorf("State after Resume = %v, want StateRunning", got)
	}
	if g.IsPaused(gate.ScopeWorkerDispatch) {
		t.Error("IsPaused after Resume should be false")
	}
}

func TestResumeFromRunningIsNoOp(t *testing.T) {
	p := &noopGatePersist{}
	g, _ := gate.NewOperatorGate(context.Background(), p)

	if err := g.Resume(context.Background()); err != nil {
		t.Fatalf("Resume from running: %v", err)
	}
	if got := g.State(); got != gate.StateRunning {
		t.Errorf("State = %v, want StateRunning", got)
	}
}

func TestPauseFromPausedReplacesMode(t *testing.T) {

	p := &noopGatePersist{}
	g, _ := gate.NewOperatorGate(context.Background(), p)

	_ = g.Pause(context.Background(), gate.PauseDescriptive, "first")
	_ = g.Pause(context.Background(), gate.PauseQuiet, "second")
	if got := g.State(); got != gate.StatePausedQuiet {
		t.Errorf("State = %v, want StatePausedQuiet", got)
	}
}

func TestIsPausedScopes(t *testing.T) {
	p := &noopGatePersist{}
	g, _ := gate.NewOperatorGate(context.Background(), p)

	if g.IsPaused(gate.ScopeWorkerDispatch) || g.IsPaused(gate.ScopeLLMPreCall) || g.IsPaused(gate.ScopeAfterCommit) {
		t.Error("IsPaused must be false while running")
	}

	_ = g.Pause(context.Background(), gate.PauseDescriptive, "test")
	for _, scope := range []gate.Scope{
		gate.ScopeWorkerDispatch,
		gate.ScopeLLMPreCall,
		gate.ScopeAfterCommit,
	} {
		if !g.IsPaused(scope) {
			t.Errorf("IsPaused(%v) = false, want true while paused", scope)
		}
	}
}

func TestGateTransitionLog(t *testing.T) {
	p := &noopGatePersist{}
	g, _ := gate.NewOperatorGate(context.Background(), p)

	_ = g.Pause(context.Background(), gate.PauseDescriptive, "reason-A")
	_ = g.Resume(context.Background())

	log := g.TransitionLog()
	if len(log) < 2 {
		t.Fatalf("TransitionLog len = %d, want >= 2", len(log))
	}
	if log[0].ToState != gate.StatePausedDescriptive {
		t.Errorf("log[0].ToState = %v, want StatePausedDescriptive", log[0].ToState)
	}
	if log[0].Reason != "reason-A" {
		t.Errorf("log[0].Reason = %q, want \"reason-A\"", log[0].Reason)
	}
	if log[1].ToState != gate.StateRunning {
		t.Errorf("log[1].ToState = %v, want StateRunning", log[1].ToState)
	}
}

func TestGatePersistSaveCalledOnTransition(t *testing.T) {
	p := &noopGatePersist{}
	g, _ := gate.NewOperatorGate(context.Background(), p)

	_ = g.Pause(context.Background(), gate.PauseQuiet, "z-score")
	if p.state != gate.StatePausedQuiet {
		t.Errorf("persist.state = %v, want StatePausedQuiet", p.state)
	}

	_ = g.Resume(context.Background())
	if p.state != gate.StateRunning {
		t.Errorf("persist.state = %v after Resume, want StateRunning", p.state)
	}
}

func TestGateLoadStateFromPersistOnStart(t *testing.T) {

	p := &noopGatePersist{state: gate.StatePausedDescriptive}
	g, err := gate.NewOperatorGate(context.Background(), p)
	if err != nil {
		t.Fatalf("NewOperatorGate: %v", err)
	}
	if got := g.State(); got != gate.StatePausedDescriptive {
		t.Errorf("restored State = %v, want StatePausedDescriptive", got)
	}
	if !g.IsPaused(gate.ScopeWorkerDispatch) {
		t.Error("IsPaused after restore must be true")
	}
}

func TestGateTransitionTimestamp(t *testing.T) {
	p := &noopGatePersist{}
	g, _ := gate.NewOperatorGate(context.Background(), p)
	before := time.Now().Add(-time.Second)
	_ = g.Pause(context.Background(), gate.PauseDescriptive, "ts-check")
	log := g.TransitionLog()
	if len(log) == 0 {
		t.Fatal("empty transition log")
	}
	if log[0].At.Before(before) {
		t.Errorf("transition At = %v, want >= %v", log[0].At, before)
	}
}

func TestNewOperatorGateNilPersistReturnsErr(t *testing.T) {
	g, err := gate.NewOperatorGate(context.Background(), nil)
	if err == nil {
		t.Fatal("NewOperatorGate(nil) should return error")
	}
	if g != nil {
		t.Errorf("NewOperatorGate(nil) returned non-nil gate %v", g)
	}
	if !errors.Is(err, gate.ErrNilPersist) {
		t.Errorf("NewOperatorGate(nil) error = %v, want errors.Is(ErrNilPersist)", err)
	}
}

func TestPauseUnknownModeReturnsError(t *testing.T) {
	p := &noopGatePersist{}
	g, _ := gate.NewOperatorGate(context.Background(), p)
	err := g.Pause(context.Background(), gate.PauseMode(99), "test")
	if err == nil {
		t.Error("expected error for unknown PauseMode")
	}
}

type errGatePersist struct {
	noopGatePersist
}

func (e *errGatePersist) SaveState(_ context.Context, s gate.State, _ string) error {
	e.state = s
	return fmt.Errorf("injected save error")
}

type errLoadPersist struct{}

func (e *errLoadPersist) LoadState(_ context.Context) (gate.State, error) {
	return "", fmt.Errorf("injected load error")
}
func (e *errLoadPersist) SaveState(_ context.Context, _ gate.State, _ string) error {
	return nil
}

func TestNewOperatorGateLoadStateError(t *testing.T) {
	p := &errLoadPersist{}
	_, err := gate.NewOperatorGate(context.Background(), p)
	if err == nil {
		t.Error("expected error from NewOperatorGate when LoadState fails")
	}
}

func TestPausePersistError(t *testing.T) {
	p := &errGatePersist{}
	g, _ := gate.NewOperatorGate(context.Background(), p)
	err := g.Pause(context.Background(), gate.PauseDescriptive, "test")
	if err == nil {
		t.Error("expected error when persist.SaveState fails during Pause")
	}

	if g.State() != gate.StateRunning {
		t.Errorf("State = %v after Pause persist error, want StateRunning (persist-first)", g.State())
	}
}

func TestResumePersistError(t *testing.T) {

	p2 := &errGatePersist{noopGatePersist: noopGatePersist{state: gate.StatePausedQuiet}}
	g2, _ := gate.NewOperatorGate(context.Background(), p2)
	err := g2.Resume(context.Background())
	if err == nil {
		t.Error("expected error when persist.SaveState fails during Resume")
	}

	if g2.State() != gate.StatePausedQuiet {
		t.Errorf("State = %v after Resume persist error, want StatePausedQuiet (persist-first)", g2.State())
	}
}

func TestPausePersistFirst_StateUnchangedOnPersistError(t *testing.T) {
	p := &errGatePersist{noopGatePersist: noopGatePersist{state: gate.StateRunning}}
	g, err := gate.NewOperatorGate(context.Background(), p)
	if err != nil {
		t.Fatalf("NewOperatorGate: %v", err)
	}
	stateBefore := g.State()
	if stateBefore != gate.StateRunning {
		t.Fatalf("precondition: initial state = %v, want StateRunning", stateBefore)
	}
	pErr := g.Pause(context.Background(), gate.PauseDescriptive, "test")
	if pErr == nil {
		t.Fatal("expected persist error from Pause")
	}
	stateAfter := g.State()
	if stateAfter != gate.StateRunning {
		t.Errorf("Pause returned error but state mutated to %v; want unchanged StateRunning (persist-first)", stateAfter)
	}
}

func TestResumePersistFirst_StateUnchangedOnPersistError(t *testing.T) {

	p := &errGatePersist{noopGatePersist: noopGatePersist{state: gate.StatePausedDescriptive}}
	g, err := gate.NewOperatorGate(context.Background(), p)
	if err != nil {
		t.Fatalf("NewOperatorGate: %v", err)
	}
	if g.State() != gate.StatePausedDescriptive {
		t.Fatalf("precondition: initial state = %v, want StatePausedDescriptive", g.State())
	}
	rErr := g.Resume(context.Background())
	if rErr == nil {
		t.Fatal("expected persist error from Resume")
	}
	stateAfter := g.State()
	if stateAfter != gate.StatePausedDescriptive {
		t.Errorf("Resume returned error but state mutated to %v; want unchanged StatePausedDescriptive (persist-first)", stateAfter)
	}
}

func TestIsPaused_AfterApply_ScopeAware(t *testing.T) {
	p := &noopGatePersist{}
	g, err := gate.NewOperatorGate(context.Background(), p)
	if err != nil {
		t.Fatalf("NewOperatorGate: %v", err)
	}

	if err := g.Pause(context.Background(), gate.PauseAfterApply, "before merge review"); err != nil {
		t.Fatalf("Pause: %v", err)
	}

	cases := []struct {
		name  string
		scope gate.Scope
		want  bool
	}{
		{"WorkerDispatch blocks", gate.ScopeWorkerDispatch, true},
		{"LLMPreCall passes", gate.ScopeLLMPreCall, false},
		{"AfterCommit passes", gate.ScopeAfterCommit, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := g.IsPaused(tc.scope); got != tc.want {
				t.Errorf("IsPaused(%v) under StatePausedAfterApply = %v, want %v", tc.scope, got, tc.want)
			}
		})
	}
}

func TestIsPaused_DescriptiveAndQuiet_BlockAllScopes(t *testing.T) {
	for _, mode := range []gate.PauseMode{gate.PauseDescriptive, gate.PauseQuiet} {
		t.Run(fmt.Sprintf("mode=%d", mode), func(t *testing.T) {
			p := &noopGatePersist{}
			g, _ := gate.NewOperatorGate(context.Background(), p)
			_ = g.Pause(context.Background(), mode, "test")
			for _, scope := range []gate.Scope{
				gate.ScopeWorkerDispatch,
				gate.ScopeLLMPreCall,
				gate.ScopeAfterCommit,
			} {
				if !g.IsPaused(scope) {
					t.Errorf("IsPaused(%v) under mode=%d = false, want true", scope, mode)
				}
			}
		})
	}
}

func TestGateLoadStateUnrecognizedDefaultsToRunning(t *testing.T) {

	p := &noopGatePersist{state: gate.State("unknown_state_xyz")}
	g, err := gate.NewOperatorGate(context.Background(), p)
	if err != nil {
		t.Fatalf("NewOperatorGate: %v", err)
	}
	if got := g.State(); got != gate.StateRunning {
		t.Errorf("unrecognised state should default to StateRunning, got %v", got)
	}
}
