package orchestrator_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/cbip-solutions/hades-system/internal/orchestrator"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

type trackingDispatcher struct {
	mu            sync.Mutex
	dispatchCalls int
	shutdownCalls int
	dispErr       error

	blockDispatch   bool
	dispatchRelease chan struct{}
}

func newTrackingDispatcher() *trackingDispatcher {
	return &trackingDispatcher{dispatchRelease: make(chan struct{})}
}

func (d *trackingDispatcher) Dispatch(ctx context.Context, _ orchestrator.DispatchRequest) (orchestrator.DispatchResult, error) {
	d.mu.Lock()
	d.dispatchCalls++
	block := d.blockDispatch
	d.mu.Unlock()

	if block {

		select {
		case <-d.dispatchRelease:
		case <-ctx.Done():
			return orchestrator.DispatchResult{}, ctx.Err()
		}
	}
	d.mu.Lock()
	err := d.dispErr
	d.mu.Unlock()
	return orchestrator.DispatchResult{}, err
}

func (d *trackingDispatcher) Shutdown(_ context.Context) error {
	d.mu.Lock()
	d.shutdownCalls++

	if d.blockDispatch {
		select {
		case <-d.dispatchRelease:

		default:
			close(d.dispatchRelease)
		}
	}
	d.mu.Unlock()
	return nil
}

func (d *trackingDispatcher) ShutdownCalls() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.shutdownCalls
}

func newHarnessWithTracker(t *testing.T) (*harness, *trackingDispatcher) {
	t.Helper()
	h := newHarness(t)
	td := newTrackingDispatcher()

	h.disp = nil
	orch, err := orchestrator.New(orchestrator.Config{
		Clock:        h.clk,
		EventLog:     h.memLog,
		StateMachine: orchestrator.NewStateMachine(h.memLog, h.clk, testSessionID, testProjectID),
		Pool:         h.pool,
		Dispatcher:   td,
		Research:     h.gate,
		SessionID:    testSessionID,
		ProjectID:    testProjectID,
		PoolCapacity: 8,
	})
	if err != nil {
		t.Fatalf("orchestrator.New with tracker: %v", err)
	}
	h.orch = orch
	return h, td
}

func TestShutdownIdempotent(t *testing.T) {
	defer goleak.VerifyNone(t)

	h := newHarness(t)
	h.init()

	if err := h.orch.Shutdown(context.Background()); err != nil {
		t.Fatalf("first Shutdown: %v", err)
	}
	if err := h.orch.Shutdown(context.Background()); err != nil {
		t.Fatalf("second Shutdown (idempotent): want nil, got %v", err)
	}
}

func TestShutdownNeverInitializedNoOp(t *testing.T) {
	defer goleak.VerifyNone(t)

	h := newHarness(t)

	if err := h.orch.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown before Init: want nil (no-op), got %v", err)
	}

	if err := h.orch.Shutdown(context.Background()); err != nil {
		t.Fatalf("second Shutdown after never-init: want nil, got %v", err)
	}
}

func TestShutdownForcesIdleAfterDeadline(t *testing.T) {
	defer goleak.VerifyNone(t)

	h, td := newHarnessWithTracker(t)
	h.init()
	h.seedResearchCompleted()

	td.mu.Lock()
	td.blockDispatch = true
	td.mu.Unlock()

	runCtx, runCancel := context.WithCancel(context.Background())
	defer runCancel()

	runDone := make(chan error, 1)
	go func() {
		runDone <- h.orch.RunStage4(runCtx, h.build(defaultSpec(), "max-scope"))
	}()

	deadline := time.Now().Add(2 * time.Second)
	for h.orch.State() != orchestrator.StateRunning {
		if time.Now().After(deadline) {
			t.Fatal("state machine never reached Running within 2s")
		}
		time.Sleep(2 * time.Millisecond)
	}

	runCancel()

	<-runDone

	shutCtx, shutCancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer shutCancel()
	if err := h.orch.Shutdown(shutCtx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	if h.orch.State() != orchestrator.StateIdle {
		t.Fatalf("post-Shutdown state = %v, want Idle", h.orch.State())
	}
}

func TestInitAfterShutdownRejects(t *testing.T) {
	defer goleak.VerifyNone(t)

	h := newHarness(t)
	h.init()

	if err := h.orch.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if err := h.orch.Init(context.Background()); !errors.Is(err, orchestrator.ErrAlreadyShutdown) {
		t.Fatalf("Init after Shutdown: want ErrAlreadyShutdown, got %v", err)
	}
}

func TestShutdownClearsDispatcher(t *testing.T) {
	defer goleak.VerifyNone(t)

	h, td := newHarnessWithTracker(t)
	h.init()

	if err := h.orch.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if got := td.ShutdownCalls(); got != 1 {
		t.Fatalf("Dispatcher.Shutdown calls = %d, want 1", got)
	}

	if err := h.orch.Shutdown(context.Background()); err != nil {
		t.Fatalf("second Shutdown: %v", err)
	}
	if got := td.ShutdownCalls(); got != 1 {
		t.Fatalf("Dispatcher.Shutdown calls after double-Shutdown = %d, want 1 (idempotent guard)", got)
	}
}

func TestShutdownRunStage4InFlight(t *testing.T) {
	defer goleak.VerifyNone(t)

	h, td := newHarnessWithTracker(t)
	h.init()
	h.seedResearchCompleted()

	td.mu.Lock()
	td.blockDispatch = true
	td.mu.Unlock()

	runCtx, runCancel := context.WithCancel(context.Background())
	defer runCancel()
	runDone := make(chan error, 1)
	go func() {
		runDone <- h.orch.RunStage4(runCtx, h.build(defaultSpec(), "max-scope"))
	}()

	waitDeadline := time.Now().Add(2 * time.Second)
	for h.orch.State() != orchestrator.StateRunning {
		if time.Now().After(waitDeadline) {
			t.Fatal("state machine never reached Running within 2s")
		}
		time.Sleep(2 * time.Millisecond)
	}

	runCancel()

	shutCtx, shutCancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer shutCancel()
	if err := h.orch.Shutdown(shutCtx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	select {
	case err := <-runDone:

		if err != nil && !errors.Is(err, context.Canceled) {
			t.Logf("RunStage4 returned non-cancel error (acceptable): %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("RunStage4 goroutine did not drain within 3s")
	}

	if h.orch.State() != orchestrator.StateIdle {
		t.Fatalf("post-Shutdown state = %v, want Idle", h.orch.State())
	}

	recs, err := h.memLog.Query(context.Background(), testSessionID, 0)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	found := false
	for _, r := range recs {
		if r.EventType == eventlog.EvtOrchestratorStopped {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("no EvtOrchestratorStopped in event log after Shutdown")
	}
}
