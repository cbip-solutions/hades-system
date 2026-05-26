package orchestrator

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/worktreepool"
)

type failingAppender struct {
	inner   eventlog.Appender
	failOn  int
	calls   int
	failErr error
}

func (f *failingAppender) Append(ctx context.Context, ev eventlog.Event) (int64, error) {
	f.calls++
	if f.failOn > 0 && f.calls == f.failOn {
		return 0, f.failErr
	}
	return f.inner.Append(ctx, ev)
}

type memoryGate struct{}

func (memoryGate) Check(_ context.Context, _ string) error { return nil }

type memoryDispatcher struct {
	shutdownCalls int
}

func (m *memoryDispatcher) Dispatch(_ context.Context, _ DispatchRequest) (DispatchResult, error) {
	return DispatchResult{}, nil
}
func (m *memoryDispatcher) Shutdown(_ context.Context) error {
	m.shutdownCalls++
	return nil
}

type memoryPool struct{}

func (memoryPool) Lease(_ context.Context) (*worktreepool.Worktree, error) {
	return nil, worktreepool.ErrPoolExhausted
}
func (memoryPool) Release(_ context.Context, _ *worktreepool.Worktree) error { return nil }
func (memoryPool) PruneOrphans(_ context.Context) (worktreepool.PruneReport, error) {
	return worktreepool.PruneReport{}, nil
}
func (memoryPool) Close(_ context.Context) error { return nil }

type internalSpec struct {
	taskCount     int
	parallelUpper int
}

func (s internalSpec) Phases() int                   { return 1 }
func (s internalSpec) TaskCount() int                { return s.taskCount }
func (s internalSpec) ParallelizableUpperBound() int { return s.parallelUpper }
func (s internalSpec) DependencyDAG() any            { return nil }

const (
	internalSession = "session-internal"
	internalProject = "project-internal"
)

func newInternalOrchestrator(t *testing.T, app eventlog.Appender, disp Dispatcher) *Orchestrator {
	t.Helper()
	clk := clock.NewFake(time.Date(2026, time.April, 30, 12, 0, 0, 0, time.UTC))
	sm := NewStateMachine(app, clk, internalSession, internalProject)
	o, err := New(Config{
		Clock:        clk,
		EventLog:     app,
		StateMachine: sm,
		Pool:         memoryPool{},
		Dispatcher:   disp,
		Research:     memoryGate{},
		SessionID:    internalSession,
		ProjectID:    internalProject,
		PoolCapacity: 4,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := o.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return o
}

func internalBuildRequest() BuildRequest {
	return BuildRequest{
		SessionID: internalSession,
		ProjectID: internalProject,
		Doctrine:  "max-scope",
		Spec:      internalSpec{taskCount: 4, parallelUpper: 4},
		Autonomy:  "autonomous",
	}
}

func TestRunStage4_AppendStartedEventFails(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, time.April, 30, 12, 0, 0, 0, time.UTC))
	inner := eventlog.NewMemory(clk)
	failErr := errors.New("disk full")
	app := &failingAppender{inner: inner, failOn: 1, failErr: failErr}
	disp := &memoryDispatcher{}
	o := newInternalOrchestrator(t, app, disp)

	err := o.RunStage4(context.Background(), internalBuildRequest())
	if !errors.Is(err, failErr) {
		t.Fatalf("RunStage4: want disk-full chain, got %v", err)
	}
	if !strings.Contains(err.Error(), "append started") {
		t.Fatalf("error should name failing boundary: %v", err)
	}
	if o.State() != StateIdle {
		t.Fatalf("state must remain Idle on early Append failure, got %v", o.State())
	}
}

func TestRunStage4_AppendDepthWidthFails(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, time.April, 30, 12, 0, 0, 0, time.UTC))
	inner := eventlog.NewMemory(clk)
	failErr := errors.New("eventlog write quota exceeded")

	app := &failingAppender{inner: inner, failOn: 3, failErr: failErr}
	disp := &memoryDispatcher{}
	o := newInternalOrchestrator(t, app, disp)

	err := o.RunStage4(context.Background(), internalBuildRequest())
	if !errors.Is(err, failErr) {
		t.Fatalf("RunStage4: want quota-exceeded chain, got %v", err)
	}
	if !strings.Contains(err.Error(), "append depth/width") {
		t.Fatalf("error should name failing boundary: %v", err)
	}

	if o.State() != StateIdle {
		t.Fatalf("state = %v after unwind, want Idle", o.State())
	}
}

func TestRunStage4_RecordStoppedHonoursCancelledCtx(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, time.April, 30, 12, 0, 0, 0, time.UTC))
	inner := eventlog.NewMemory(clk)
	disp := &memoryDispatcher{}
	o := newInternalOrchestrator(t, inner, disp)

	// Build a request whose ConfirmationCallback cancels the lifecycle
	// ctx and returns a denial error; recordStopped is called next with
	// the cancelled ctx — the fallback to context.Background MUST NOT
	// drop the audit row.
	ctx, cancel := context.WithCancel(context.Background())
	denyErr := errors.New("operator denied")
	req := internalBuildRequest()
	req.ConfirmationCallback = func(_ context.Context, _ DispatchDecisionEvent) error {
		cancel()
		return denyErr
	}

	err := o.RunStage4(ctx, req)
	if !errors.Is(err, denyErr) {
		t.Fatalf("RunStage4: want deny chain, got %v", err)
	}

	recs, qErr := inner.Query(context.Background(), internalSession, 0)
	if qErr != nil {
		t.Fatalf("Query: %v", qErr)
	}
	found := false
	for _, r := range recs {
		if r.EventType == eventlog.EvtOrchestratorStopped {
			decoded, derr := eventlog.Decode(r.EventType, r.Payload)
			if derr != nil {
				t.Fatalf("decode: %v", derr)
			}
			stopped, ok := decoded.(eventlog.OrchestratorStopped)
			if !ok {
				t.Fatalf("decoded %T", decoded)
			}
			if stopped.Outcome == "confirmation_denied" {
				found = true
				break
			}
		}
	}
	if !found {
		t.Fatalf("missing OrchestratorStopped(confirmation_denied) row even with cancelled ctx")
	}
}

func TestRunStage4_DispatchBeginTransitionFails(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, time.April, 30, 12, 0, 0, 0, time.UTC))
	inner := eventlog.NewMemory(clk)
	failErr := errors.New("transient log error")
	app := &failingAppender{inner: inner, failOn: 4, failErr: failErr}
	disp := &memoryDispatcher{}
	o := newInternalOrchestrator(t, app, disp)

	err := o.RunStage4(context.Background(), internalBuildRequest())
	if !errors.Is(err, failErr) {
		t.Fatalf("RunStage4: want failErr chain, got %v", err)
	}
	if disp.shutdownCalls != 0 {
		t.Fatalf("Dispatcher.Shutdown should not be called (Dispatch never reached): %d calls", disp.shutdownCalls)
	}

	assertInternalOrchestratorStopped(t, inner, internalSession, "transition_failed")

	if o.State() != StateIdle {
		t.Fatalf("post-unwind state = %v, want Idle", o.State())
	}
}

func TestRunStage4_SuccessTransitionFails(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, time.April, 30, 12, 0, 0, 0, time.UTC))
	inner := eventlog.NewMemory(clk)
	failErr := errors.New("disk full mid-success")
	app := &failingAppender{inner: inner, failOn: 5, failErr: failErr}
	disp := &memoryDispatcher{}
	o := newInternalOrchestrator(t, app, disp)

	err := o.RunStage4(context.Background(), internalBuildRequest())
	if !errors.Is(err, failErr) {
		t.Fatalf("RunStage4: want failErr chain, got %v", err)
	}

	assertInternalOrchestratorStopped(t, inner, internalSession, "transition_failed")

	if o.State() != StateIdle {
		t.Fatalf("post-unwind state = %v, want Idle", o.State())
	}

	if disp.shutdownCalls != 1 {
		t.Fatalf("Dispatcher.Shutdown calls = %d, want 1", disp.shutdownCalls)
	}
}

func assertInternalOrchestratorStopped(t *testing.T, log *eventlog.Log, sessionID string, wantOutcome string) {
	t.Helper()
	recs, err := log.Query(context.Background(), sessionID, 0)
	if err != nil {
		t.Fatalf("assertInternalOrchestratorStopped: Query: %v", err)
	}
	for i := len(recs) - 1; i >= 0; i-- {
		if recs[i].EventType != eventlog.EvtOrchestratorStopped {
			continue
		}
		decoded, derr := eventlog.Decode(recs[i].EventType, recs[i].Payload)
		if derr != nil {
			t.Fatalf("assertInternalOrchestratorStopped: decode: %v", derr)
		}
		stopped, ok := decoded.(eventlog.OrchestratorStopped)
		if !ok {
			t.Fatalf("assertInternalOrchestratorStopped: decoded %T, want eventlog.OrchestratorStopped", decoded)
		}
		if stopped.Outcome != wantOutcome {
			t.Fatalf("OrchestratorStopped.Outcome = %q, want %q", stopped.Outcome, wantOutcome)
		}
		return
	}
	t.Fatalf("assertInternalOrchestratorStopped: no EvtOrchestratorStopped row with outcome %q in event log", wantOutcome)
}

func TestRunStage4_StartTransitionFails(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, time.April, 30, 12, 0, 0, 0, time.UTC))
	inner := eventlog.NewMemory(clk)
	failErr := errors.New("log write error on start transition")

	app := &failingAppender{inner: inner, failOn: 2, failErr: failErr}
	disp := &memoryDispatcher{}
	o := newInternalOrchestrator(t, app, disp)

	err := o.RunStage4(context.Background(), internalBuildRequest())
	if !errors.Is(err, failErr) {
		t.Fatalf("RunStage4: want failErr chain, got %v", err)
	}

	if disp.shutdownCalls != 0 {
		t.Fatalf("Dispatcher.Shutdown should not be called: %d calls", disp.shutdownCalls)
	}

	assertInternalOrchestratorStopped(t, inner, internalSession, "transition_failed")

	if o.State() != StateIdle {
		t.Fatalf("state = %v, want Idle (transition failed, no state mutation)", o.State())
	}
}

func TestRunStage4_TransitionFailurePropagated(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, time.April, 30, 12, 0, 0, 0, time.UTC))
	inner := eventlog.NewMemory(clk)
	disp := &memoryDispatcher{}
	o := newInternalOrchestrator(t, inner, disp)

	if err := o.cfg.StateMachine.Transition(context.Background(), StateInitializing, "test-prep"); err != nil {
		t.Fatalf("prep transition 1: %v", err)
	}
	if err := o.cfg.StateMachine.Transition(context.Background(), StateRunning, "test-prep"); err != nil {
		t.Fatalf("prep transition 2: %v", err)
	}

	err := o.RunStage4(context.Background(), internalBuildRequest())
	if !errors.Is(err, ErrIllegalTransition) {
		t.Fatalf("RunStage4: want ErrIllegalTransition wrap, got %v", err)
	}
}

func TestRunStage4_ZeroTaskSpecRejected(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, time.April, 30, 12, 0, 0, 0, time.UTC))
	inner := eventlog.NewMemory(clk)
	disp := &memoryDispatcher{}
	o := newInternalOrchestrator(t, inner, disp)

	req := BuildRequest{
		SessionID: internalSession,
		ProjectID: internalProject,
		Doctrine:  "max-scope",
		Spec:      internalSpec{taskCount: 0, parallelUpper: 4},
		Autonomy:  "autonomous",
	}
	err := o.RunStage4(context.Background(), req)
	if !errors.Is(err, ErrInvalidBuildRequest) {
		t.Fatalf("RunStage4(taskCount=0): want ErrInvalidBuildRequest, got %v", err)
	}
	if o.State() != StateIdle {
		t.Fatalf("state = %v after zero-task rejection, want StateIdle", o.State())
	}

	recs, qErr := inner.Query(context.Background(), internalSession, 0)
	if qErr != nil {
		t.Fatalf("Query: %v", qErr)
	}
	for _, r := range recs {
		if r.EventType == eventlog.EvtOrchestratorStarted {
			t.Fatalf("EvtOrchestratorStarted was emitted but must not be when validateBuildRequest fails")
		}
	}
}

func TestRunStage4_DispatchFailureCallsShutdown(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, time.April, 30, 12, 0, 0, 0, time.UTC))
	inner := eventlog.NewMemory(clk)
	disp := &dispatcherFailing{shutdownCalls: 0, dispatchErr: errors.New("worker exited")}
	o := newInternalOrchestrator(t, inner, disp)

	err := o.RunStage4(context.Background(), internalBuildRequest())
	if err == nil {
		t.Fatalf("RunStage4: want error, got nil")
	}
	if disp.shutdownCalls != 1 {
		t.Fatalf("Dispatcher.Shutdown calls = %d, want 1", disp.shutdownCalls)
	}
	if o.State() != StateIdle {
		t.Fatalf("post-unwind state = %v, want Idle", o.State())
	}
}

func TestRunStage4_DecideWidthFailureUnwinds(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, time.April, 30, 12, 0, 0, 0, time.UTC))
	inner := eventlog.NewMemory(clk)
	disp := &memoryDispatcher{}
	o := newInternalOrchestrator(t, inner, disp)

	o.cfg.PoolCapacity = 0

	err := o.RunStage4(context.Background(), internalBuildRequest())
	if !errors.Is(err, ErrZeroWidth) {
		t.Fatalf("RunStage4: want ErrZeroWidth wrap, got %v", err)
	}
	if !strings.Contains(err.Error(), "decide width") {
		t.Fatalf("error should name failing boundary: %v", err)
	}
	if o.State() != StateIdle {
		t.Fatalf("post-unwind state = %v, want Idle", o.State())
	}
}

type dispatcherFailing struct {
	dispatchErr   error
	shutdownCalls int
}

func (d *dispatcherFailing) Dispatch(_ context.Context, _ DispatchRequest) (DispatchResult, error) {
	return DispatchResult{}, d.dispatchErr
}
func (d *dispatcherFailing) Shutdown(_ context.Context) error {
	d.shutdownCalls++
	return nil
}
