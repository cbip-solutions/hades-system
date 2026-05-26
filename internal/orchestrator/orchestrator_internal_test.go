package orchestrator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/worktreepool"
)

type internalOrchAppender struct{}

func (internalOrchAppender) Append(_ context.Context, _ eventlog.Event) (int64, error) {
	return 1, nil
}

type internalOrchPool struct{}

func (internalOrchPool) Lease(_ context.Context) (*worktreepool.Worktree, error) {
	return nil, worktreepool.ErrPoolExhausted
}
func (internalOrchPool) Release(_ context.Context, _ *worktreepool.Worktree) error { return nil }
func (internalOrchPool) PruneOrphans(_ context.Context) (worktreepool.PruneReport, error) {
	return worktreepool.PruneReport{}, nil
}
func (internalOrchPool) Close(_ context.Context) error { return nil }

type internalOrchDispatcher struct{}

func (internalOrchDispatcher) Dispatch(_ context.Context, _ DispatchRequest) (DispatchResult, error) {
	return DispatchResult{}, nil
}
func (internalOrchDispatcher) Shutdown(_ context.Context) error { return nil }

type internalOrchGate struct{}

func (internalOrchGate) Check(_ context.Context, _ string) error { return nil }

func TestInit_AfterShutdownReturnsErrAlreadyShutdown(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, time.April, 30, 12, 0, 0, 0, time.UTC))
	app := internalOrchAppender{}
	sm := NewStateMachine(app, clk, "session-internal", "project-internal")

	o, err := New(Config{
		Clock:        clk,
		EventLog:     app,
		StateMachine: sm,
		Pool:         internalOrchPool{},
		Dispatcher:   internalOrchDispatcher{},
		Research:     internalOrchGate{},
		SessionID:    "session-internal",
		ProjectID:    "project-internal",
		PoolCapacity: 4,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	o.mu.Lock()
	o.shutdown = true
	o.mu.Unlock()

	if got := o.Init(context.Background()); !errors.Is(got, ErrAlreadyShutdown) {
		t.Fatalf("Init after shutdown: want ErrAlreadyShutdown, got %v", got)
	}
}
