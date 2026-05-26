package orchestrator_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/worktreepool"
)

type fakePool struct {
	mu       sync.Mutex
	closed   bool
	leased   int
	leaseErr error
}

func (p *fakePool) Lease(_ context.Context) (*worktreepool.Worktree, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.leaseErr != nil {
		return nil, p.leaseErr
	}
	p.leased++

	return &worktreepool.Worktree{}, nil
}

func (p *fakePool) Release(_ context.Context, _ *worktreepool.Worktree) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.leased > 0 {
		p.leased--
	}
	return nil
}

func (p *fakePool) PruneOrphans(_ context.Context) (worktreepool.PruneReport, error) {
	return worktreepool.PruneReport{}, nil
}

func (p *fakePool) Close(_ context.Context) error {
	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()
	return nil
}

func (p *fakePool) Leased() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.leased
}

type fakeDispatcher struct {
	dispatchCalls int
	resp          orchestrator.DispatchResult
	err           error
}

func (f *fakeDispatcher) Dispatch(_ context.Context, _ orchestrator.DispatchRequest) (orchestrator.DispatchResult, error) {
	f.dispatchCalls++
	return f.resp, f.err
}

func (f *fakeDispatcher) Shutdown(_ context.Context) error { return nil }

type fakeGate struct {
	checkErr error
}

func (f *fakeGate) Check(_ context.Context, _ string) error { return f.checkErr }

func newTestClock() *clock.Fake {
	return clock.NewFake(time.Date(2026, time.April, 30, 12, 0, 0, 0, time.UTC))
}

func newTestOrchestrator(t *testing.T) *orchestrator.Orchestrator {
	t.Helper()
	clk := newTestClock()
	memLog := eventlog.NewMemory(clk)
	sm := orchestrator.NewStateMachine(memLog, clk, testSessionID, testProjectID)
	pool := &fakePool{}
	disp := &fakeDispatcher{}
	gate := &fakeGate{}

	orch, err := orchestrator.New(orchestrator.Config{
		Clock:        clk,
		EventLog:     memLog,
		StateMachine: sm,
		Pool:         pool,
		Dispatcher:   disp,
		Research:     gate,
		SessionID:    testSessionID,
		ProjectID:    testProjectID,
		PoolCapacity: 8,
	})
	if err != nil {
		t.Fatalf("orchestrator.New: %v", err)
	}
	return orch
}

func TestOrchestratorNewRejectsNilDeps(t *testing.T) {
	clk := newTestClock()
	mkLog := func() eventlog.Appender { return eventlog.NewMemory(clk) }
	mkSM := func() *orchestrator.StateMachine {
		return orchestrator.NewStateMachine(mkLog(), clk, testSessionID, testProjectID)
	}
	mkPool := func() worktreepool.Pool { return &fakePool{} }
	mkDisp := func() orchestrator.Dispatcher { return &fakeDispatcher{} }
	mkGate := func() orchestrator.ResearchGate { return &fakeGate{} }

	cases := map[string]orchestrator.Config{
		"nil clock": {
			EventLog: mkLog(), StateMachine: mkSM(), Pool: mkPool(),
			Dispatcher: mkDisp(), Research: mkGate(),
			SessionID: testSessionID, ProjectID: testProjectID,
			PoolCapacity: 8,
		},
		"nil eventlog": {
			Clock: clk, StateMachine: mkSM(), Pool: mkPool(),
			Dispatcher: mkDisp(), Research: mkGate(),
			SessionID: testSessionID, ProjectID: testProjectID,
			PoolCapacity: 8,
		},
		"nil state machine": {
			Clock: clk, EventLog: mkLog(), Pool: mkPool(),
			Dispatcher: mkDisp(), Research: mkGate(),
			SessionID: testSessionID, ProjectID: testProjectID,
			PoolCapacity: 8,
		},
		"nil pool": {
			Clock: clk, EventLog: mkLog(), StateMachine: mkSM(),
			Dispatcher: mkDisp(), Research: mkGate(),
			SessionID: testSessionID, ProjectID: testProjectID,
			PoolCapacity: 8,
		},
		"nil dispatcher": {
			Clock: clk, EventLog: mkLog(), StateMachine: mkSM(), Pool: mkPool(),
			Research:  mkGate(),
			SessionID: testSessionID, ProjectID: testProjectID,
			PoolCapacity: 8,
		},
		"nil research": {
			Clock: clk, EventLog: mkLog(), StateMachine: mkSM(), Pool: mkPool(),
			Dispatcher: mkDisp(),
			SessionID:  testSessionID, ProjectID: testProjectID,
			PoolCapacity: 8,
		},
		"empty session": {
			Clock: clk, EventLog: mkLog(), StateMachine: mkSM(), Pool: mkPool(),
			Dispatcher: mkDisp(), Research: mkGate(),
			ProjectID:    testProjectID,
			PoolCapacity: 8,
		},
		"empty project": {
			Clock: clk, EventLog: mkLog(), StateMachine: mkSM(), Pool: mkPool(),
			Dispatcher: mkDisp(), Research: mkGate(),
			SessionID:    testSessionID,
			PoolCapacity: 8,
		},
		"zero pool capacity": {
			Clock: clk, EventLog: mkLog(), StateMachine: mkSM(), Pool: mkPool(),
			Dispatcher: mkDisp(), Research: mkGate(),
			SessionID: testSessionID, ProjectID: testProjectID,
		},
		"negative pool capacity": {
			Clock: clk, EventLog: mkLog(), StateMachine: mkSM(), Pool: mkPool(),
			Dispatcher: mkDisp(), Research: mkGate(),
			SessionID: testSessionID, ProjectID: testProjectID,
			PoolCapacity: -1,
		},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := orchestrator.New(cfg)
			if !errors.Is(err, orchestrator.ErrInvalidConfig) {
				t.Fatalf("want ErrInvalidConfig wrap, got %v", err)
			}
			if got != nil {
				t.Fatalf("expected nil orchestrator on error, got %#v", got)
			}
		})
	}
}

func TestOrchestratorNewHappyPath(t *testing.T) {
	orch := newTestOrchestrator(t)
	if orch == nil {
		t.Fatalf("expected non-nil orchestrator")
	}
	if orch.State() != orchestrator.StateIdle {
		t.Fatalf("expected fresh orchestrator at StateIdle, got %v", orch.State())
	}
	if orch.PoolPrimed() {
		t.Fatalf("expected PoolPrimed=false before Init")
	}
	if orch.Initialized() {
		t.Fatalf("expected Initialized=false before Init")
	}
}

func TestOrchestratorInitHappyPath(t *testing.T) {
	orch := newTestOrchestrator(t)
	if err := orch.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if !orch.Initialized() {
		t.Fatalf("expected Initialized=true after Init")
	}
	if !orch.PoolPrimed() {
		t.Fatalf("expected PoolPrimed=true after Init")
	}
}

func TestOrchestratorInitIdempotent(t *testing.T) {
	orch := newTestOrchestrator(t)
	if err := orch.Init(context.Background()); err != nil {
		t.Fatalf("first Init: %v", err)
	}
	if err := orch.Init(context.Background()); !errors.Is(err, orchestrator.ErrAlreadyInitialized) {
		t.Fatalf("second Init: want ErrAlreadyInitialized, got %v", err)
	}
}

func TestOrchestratorInitRejectsCancelledContext(t *testing.T) {
	orch := newTestOrchestrator(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := orch.Init(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Init with cancelled ctx: want context.Canceled, got %v", err)
	}

	if orch.Initialized() {
		t.Fatalf("expected Initialized=false after cancelled-ctx Init")
	}
	if err := orch.Init(context.Background()); err != nil {
		t.Fatalf("retry Init: %v", err)
	}
}

func TestRunStage4HappyPath(t *testing.T) {
	h := newHarness(t)
	h.init()
	h.seedResearchCompleted()

	if err := h.orch.RunStage4(context.Background(), h.build(defaultSpec(), "max-scope")); err != nil {
		t.Fatalf("RunStage4: %v", err)
	}
	if h.orch.State() != orchestrator.StateIdle {
		t.Fatalf("end state = %v, want Idle", h.orch.State())
	}
	if h.disp.dispatchCalls != 1 {
		t.Fatalf("dispatch calls = %d, want 1", h.disp.dispatchCalls)
	}
	h.assertEventOrder(
		eventlog.EvtResearchCompleted,
		eventlog.EvtOrchestratorStarted,
		eventlog.EvtOrchestratorStateTransition,
		eventlog.EvtDepthWidthDecided,
		eventlog.EvtOrchestratorStateTransition,
		eventlog.EvtOrchestratorStateTransition,
		eventlog.EvtOrchestratorStopped,
	)
	h.assertOrchestratorStopped("success")
}

func TestRunStage4ResearchGateFailed(t *testing.T) {
	h := newHarness(t)
	h.init()
	// Do NOT seed EvtResearchCompleted; force gate to fail.
	gateErr := errors.New("no completed research event found")
	h.gate.checkErr = gateErr

	err := h.orch.RunStage4(context.Background(), h.build(defaultSpec(), "max-scope"))
	if err == nil {
		t.Fatalf("RunStage4: want error, got nil")
	}
	if !errors.Is(err, orchestrator.ErrResearchGateNotPassed) {
		t.Fatalf("RunStage4: want ErrResearchGateNotPassed wrap, got %v", err)
	}
	if !errors.Is(err, gateErr) {
		t.Fatalf("RunStage4: want gate error chain preserved, got %v", err)
	}
	if h.orch.State() != orchestrator.StateIdle {
		t.Fatalf("end state = %v, want Idle", h.orch.State())
	}
	if h.disp.dispatchCalls != 0 {
		t.Fatalf("dispatch must not run on gate failure, got %d calls", h.disp.dispatchCalls)
	}
	h.assertOrchestratorStopped("research_gate_failed")
}

func TestRunStage4ContextCancellation(t *testing.T) {
	h := newHarness(t)
	h.init()
	h.seedResearchCompleted()
	h.disp.err = context.Canceled

	err := h.orch.RunStage4(context.Background(), h.build(defaultSpec(), "max-scope"))
	if err == nil {
		t.Fatalf("RunStage4: want error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RunStage4: want context.Canceled chain, got %v", err)
	}
	if h.orch.State() != orchestrator.StateIdle {
		t.Fatalf("end state = %v, want Idle (post-Aborting cleanup)", h.orch.State())
	}
	h.assertOrchestratorStopped("dispatch_failed")
}

func TestRunStage4InvalidBuildRequest(t *testing.T) {
	h := newHarness(t)
	h.init()
	spec := defaultSpec()

	cases := map[string]orchestrator.BuildRequest{
		"empty session": {
			ProjectID: testProjectID,
			Doctrine:  "max-scope",
			Spec:      spec,
		},
		"empty project": {
			SessionID: testSessionID,
			Doctrine:  "max-scope",
			Spec:      spec,
		},
		"empty doctrine": {
			SessionID: testSessionID,
			ProjectID: testProjectID,
			Spec:      spec,
		},
		"nil spec": {
			SessionID: testSessionID,
			ProjectID: testProjectID,
			Doctrine:  "max-scope",
		},
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			err := h.orch.RunStage4(context.Background(), req)
			if !errors.Is(err, orchestrator.ErrInvalidBuildRequest) {
				t.Fatalf("want ErrInvalidBuildRequest wrap, got %v", err)
			}
			if h.orch.State() != orchestrator.StateIdle {
				t.Fatalf("state must remain Idle on validation failure, got %v", h.orch.State())
			}
		})
	}
}

func TestRunStage4NotInitialized(t *testing.T) {
	h := newHarness(t)

	err := h.orch.RunStage4(context.Background(), h.build(defaultSpec(), "max-scope"))
	if !errors.Is(err, orchestrator.ErrNotInitialized) {
		t.Fatalf("RunStage4 pre-Init: want ErrNotInitialized, got %v", err)
	}
}

func TestRunStage4ConfirmationCallbackDeny(t *testing.T) {
	h := newHarness(t)
	h.init()
	h.seedResearchCompleted()
	denyErr := errors.New("operator-denied")
	h.confirm = func(_ context.Context, ev orchestrator.DispatchDecisionEvent) error {
		if ev.Class != "depth-width" {
			t.Fatalf("decision class = %q, want depth-width", ev.Class)
		}
		return denyErr
	}

	err := h.orch.RunStage4(context.Background(), h.build(defaultSpec(), "max-scope"))
	if err == nil {
		t.Fatalf("RunStage4: want error, got nil")
	}
	if !errors.Is(err, denyErr) {
		t.Fatalf("RunStage4: want deny-error chain, got %v", err)
	}
	if h.orch.State() != orchestrator.StateIdle {
		t.Fatalf("end state = %v, want Idle", h.orch.State())
	}
	if h.disp.dispatchCalls != 0 {
		t.Fatalf("dispatch must not run on confirmation deny, got %d calls", h.disp.dispatchCalls)
	}
	h.assertOrchestratorStopped("confirmation_denied")
}

func TestRunStage4ContextCancelledBeforeStart(t *testing.T) {
	h := newHarness(t)
	h.init()
	h.seedResearchCompleted()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := h.orch.RunStage4(ctx, h.build(defaultSpec(), "max-scope"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RunStage4 with pre-cancelled ctx: want context.Canceled, got %v", err)
	}
}

func TestRunStage4AllKnownDoctrines(t *testing.T) {

	for _, doctrine := range []string{"max-scope", "capa-firewall", "default"} {
		t.Run(doctrine, func(t *testing.T) {
			h := newHarness(t)
			h.init()
			h.seedResearchCompleted()
			if err := h.orch.RunStage4(context.Background(), h.build(defaultSpec(), doctrine)); err != nil {
				t.Fatalf("RunStage4 doctrine=%q: %v", doctrine, err)
			}
			if h.orch.State() != orchestrator.StateIdle {
				t.Fatalf("end state = %v, want Idle", h.orch.State())
			}
			h.assertOrchestratorStopped("success")
		})
	}
}

func TestRunStage4DefaultDoctrineFallback(t *testing.T) {

	h := newHarness(t)
	h.init()
	h.seedResearchCompleted()
	if err := h.orch.RunStage4(context.Background(), h.build(defaultSpec(), "unrecognised")); err != nil {
		t.Fatalf("RunStage4 with unknown doctrine: %v", err)
	}
	if h.orch.State() != orchestrator.StateIdle {
		t.Fatalf("end state = %v, want Idle", h.orch.State())
	}
}
