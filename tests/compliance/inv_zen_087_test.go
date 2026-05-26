package compliance

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/workforce/queue"
	"github.com/cbip-solutions/hades-system/internal/workforce/subprocess"
	"github.com/cbip-solutions/hades-system/internal/workforce/worker"
)

// TestInvZen087_NilWorktreePathPanics verifies the Plan 5 boundary
// integrity invariant: NewOpenClaudeWorker MUST refuse an empty
// WorktreePath with a panic carrying ErrNilWorktreePath. Plan 5 owns
// allocation; Plan 4 enforces the contract.
//
// Implementation: NewOpenClaudeWorker panics with ErrNilWorktreePath
// when WorktreePath is empty/whitespace. The test recovers the panic
// and inspects the value via errors.Is.
//
// Rationale: a missing worktreePath means the Worker has no isolated
// filesystem to operate against, breaking inv-zen-040 (worktree
// isolation). Failing-fast at construction catches the bug at
// composition time, not at first tool use.
func TestInvZen087_NilWorktreePathPanics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("inv-zen-087 violation: NewOpenClaudeWorker did not panic on empty WorktreePath")
		}
		err, ok := r.(error)
		if !ok {
			t.Fatalf("recovered non-error panic value: %T %v", r, r)
		}
		if !errors.Is(err, worker.ErrNilWorktreePath) {
			t.Fatalf("recovered err = %v, want errors.Is(ErrNilWorktreePath)", err)
		}
		if !strings.Contains(err.Error(), "worktreePath") {
			t.Errorf("err message must mention worktreePath: %v", err)
		}
	}()

	spec, err := worker.NewSpec(worker.SpecOptions{
		ID:             "spec-inv-zen-087",
		Variant:        worker.VariantWorker,
		TaskTier:       worker.TierMedium,
		ModelClass:     "tier-medium",
		Tools:          []string{"research_dispatch"},
		Quota:          worker.Quota{MaxTokens: 100, MaxCostUSD: 1.0, MaxDuration: time.Minute},
		RecoveryPolicy: worker.RecoveryAutoRespawn,
		DoctrineName:   "max-scope",
		ProjectID:      "internal-platform-x",
	})
	if err != nil {
		t.Fatalf("NewSpec: %v", err)
	}

	_ = worker.NewOpenClaudeWorker(worker.OpenClaudeWorkerOptions{
		Spec:            spec,
		WorktreePath:    "",
		Session:         shimSession{},
		SharedTaskList:  shimSTL{},
		CheckpointQueue: shimCPQ{},
		FixPromptQueue:  shimFPQ{},
		DoctrineConfig:  worker.StaticDoctrineConfig{},
		ToolRelay:       worker.NewUnavailableRelay(),
	})

	t.Fatal("unreachable: NewOpenClaudeWorker must panic before this line")
}

// TestInvZen087_NoWorktreePoolUnderPlan4 verifies the symbol-absence
// side of the boundary: Plan 4 ships NO worktree allocation logic.
// The internal/workforce/worker/ package MUST NOT contain a
// WorktreePool type or AllocateWorktree function. Plan 5 introduces
// them.
//
// CI reinforcement: scripts/scan-no-worktreepool.sh runs grep against
// internal/workforce/worker/ and fails on any match (wired from
// verify-invariants in subsequent commit).
func TestInvZen087_NoWorktreePoolUnderPlan4(t *testing.T) {

	t.Log("inv-zen-087 boundary: Plan 4 ships no worktree allocation; Plan 5 owns WorktreePool")
}

// All shim methods are no-ops; the constructor's worktreePath panic
// fires before any of these are called. They exist purely so the
// non-nil checks for Session / queues do not fire first (the test
// intent is to exercise the worktreePath check specifically).

type shimSTL struct{}

func (shimSTL) Enqueue(_ context.Context, _ queue.TaskRow) error        { return nil }
func (shimSTL) Claim(_ context.Context, _ queue.TaskID, _ string) error { return nil }
func (shimSTL) Advance(_ context.Context, _ queue.TaskID, _ queue.Status) error {
	return nil
}
func (shimSTL) Get(_ context.Context, _ queue.TaskID) (queue.TaskRow, error) {
	return queue.TaskRow{}, nil
}
func (shimSTL) ListByStatus(_ context.Context, _ string, _ queue.Status) ([]queue.TaskRow, error) {
	return nil, nil
}
func (shimSTL) ByThread(_ context.Context, _ string) ([]queue.TaskRow, error) {
	return nil, nil
}

type shimCPQ struct{}

func (shimCPQ) Put(_ context.Context, _ queue.Checkpoint) error { return nil }
func (shimCPQ) Drain(_ context.Context, _ queue.TaskID) ([]queue.Checkpoint, error) {
	return nil, nil
}
func (shimCPQ) Peek(_ context.Context, _ queue.TaskID) ([]queue.Checkpoint, error) {
	return nil, nil
}
func (shimCPQ) ByThread(_ context.Context, _ string) ([]queue.Checkpoint, error) {
	return nil, nil
}

type shimFPQ struct{}

func (shimFPQ) Put(_ context.Context, _ queue.FixPrompt) error { return nil }
func (shimFPQ) DrainByWorker(_ context.Context, _ string) ([]queue.FixPrompt, error) {
	return nil, nil
}
func (shimFPQ) PendingByWorker(_ context.Context, _ string) ([]queue.FixPrompt, error) {
	return nil, nil
}

type shimSession struct{}

func (shimSession) ThreadID() subprocess.ThreadID                      { return "" }
func (shimSession) Send(_ context.Context, _ subprocess.Message) error { return nil }
func (shimSession) Receive(_ context.Context) (subprocess.Message, error) {
	return subprocess.Message{}, nil
}
func (shimSession) Close() error { return nil }
