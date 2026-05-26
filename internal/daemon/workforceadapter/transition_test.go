package workforceadapter_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/daemon/workforceadapter"
	"github.com/cbip-solutions/hades-system/internal/workforce/queue"
)

func TestAdvance_RejectsInvalidTransition_PendingToDoneDirect(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	stl := workforceadapter.NewSharedTaskList(s)

	_ = stl.Enqueue(ctx, queue.TaskRow{
		TaskID: "t1", ProjectID: "p1",
		Status: queue.StatusPending, CreatedAt: time.Now().UTC(),
	})

	if err := stl.Advance(ctx, "t1", queue.StatusDone); !errors.Is(err, queue.ErrInvalidTransition) {
		t.Errorf("got %v, want ErrInvalidTransition for pending→done", err)
	}
}

func TestAdvance_RejectsInvalidTransition_DoneToPending(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	stl := workforceadapter.NewSharedTaskList(s)

	_ = stl.Enqueue(ctx, queue.TaskRow{
		TaskID: "t-done", ProjectID: "p1",
		Status: queue.StatusPending, CreatedAt: time.Now().UTC(),
	})
	if err := stl.Claim(ctx, "t-done", "thr"); err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if err := stl.Advance(ctx, "t-done", queue.StatusReview); err != nil {
		t.Fatalf("Advance to review: %v", err)
	}
	if err := stl.Advance(ctx, "t-done", queue.StatusDone); err != nil {
		t.Fatalf("Advance to done: %v", err)
	}

	if err := stl.Advance(ctx, "t-done", queue.StatusPending); !errors.Is(err, queue.ErrInvalidTransition) {
		t.Errorf("got %v, want ErrInvalidTransition for done→pending", err)
	}
}

func TestAdvance_RejectsInvalidTransition_FailedToInProgress(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	stl := workforceadapter.NewSharedTaskList(s)

	_ = stl.Enqueue(ctx, queue.TaskRow{
		TaskID: "t-fail", ProjectID: "p1",
		Status: queue.StatusPending, CreatedAt: time.Now().UTC(),
	})
	if err := stl.Claim(ctx, "t-fail", "thr"); err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if err := stl.Advance(ctx, "t-fail", queue.StatusFailed); err != nil {
		t.Fatalf("Advance to failed: %v", err)
	}

	if err := stl.Advance(ctx, "t-fail", queue.StatusInProgress); !errors.Is(err, queue.ErrInvalidTransition) {
		t.Errorf("got %v, want ErrInvalidTransition for failed→in_progress", err)
	}
}

func TestAdvance_AcceptsValidTransitions(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	stl := workforceadapter.NewSharedTaskList(s)

	_ = stl.Enqueue(ctx, queue.TaskRow{
		TaskID: "t-walk", ProjectID: "p1",
		Status: queue.StatusPending, CreatedAt: time.Now().UTC(),
	})
	if err := stl.Claim(ctx, "t-walk", "thr"); err != nil {
		t.Fatalf("Claim (pending→in_progress): %v", err)
	}
	if err := stl.Advance(ctx, "t-walk", queue.StatusReview); err != nil {
		t.Errorf("Advance in_progress→review: %v", err)
	}
	if err := stl.Advance(ctx, "t-walk", queue.StatusDone); err != nil {
		t.Errorf("Advance review→done: %v", err)
	}

	got, _ := stl.Get(ctx, "t-walk")
	if got.Status != queue.StatusDone {
		t.Errorf("final status = %v, want done", got.Status)
	}
}

func TestAdvance_ReviewBackToInProgress(t *testing.T) {

	ctx := context.Background()
	s := openTestStore(t)
	stl := workforceadapter.NewSharedTaskList(s)

	_ = stl.Enqueue(ctx, queue.TaskRow{
		TaskID: "t-back", ProjectID: "p1",
		Status: queue.StatusPending, CreatedAt: time.Now().UTC(),
	})
	_ = stl.Claim(ctx, "t-back", "thr")
	_ = stl.Advance(ctx, "t-back", queue.StatusReview)
	if err := stl.Advance(ctx, "t-back", queue.StatusInProgress); err != nil {
		t.Errorf("Advance review→in_progress (send-back): %v", err)
	}
}

func TestScopedAdvance_RejectsInvalidTransition(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	stl := workforceadapter.NewSharedTaskList(s).ScopedTo("p1")

	_ = stl.Enqueue(ctx, queue.TaskRow{
		TaskID: "ts1", ProjectID: "p1",
		Status: queue.StatusPending, CreatedAt: time.Now().UTC(),
	})

	if err := stl.Advance(ctx, "ts1", queue.StatusDone); !errors.Is(err, queue.ErrInvalidTransition) {
		t.Errorf("Scoped: got %v, want ErrInvalidTransition", err)
	}
}

func TestAdvance_BeginTxError(t *testing.T) {
	ctx := context.Background()
	s := openTestStoreNoCleanup(t)
	stl := workforceadapter.NewSharedTaskList(s)
	_ = s.Close()
	if err := stl.Advance(ctx, "any", queue.StatusDone); err == nil {
		t.Error("expected begin-tx error on closed DB")
	}
}

func TestAdvance_QueryError(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	stl := workforceadapter.ExportNewSharedTaskListWithFailAdvanceQuery(s)
	if err := stl.Advance(ctx, "any", queue.StatusDone); err == nil {
		t.Error("expected query injection error")
	}
}

func TestAdvance_ExecError(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	stl := workforceadapter.NewSharedTaskList(s)
	_ = stl.Enqueue(ctx, queue.TaskRow{
		TaskID: "exec-err", ProjectID: "p1",
		Status: queue.StatusInProgress, CreatedAt: time.Now().UTC(),
	})
	stlFail := workforceadapter.ExportNewSharedTaskListWithFailAdvanceExec(s)
	if err := stlFail.Advance(ctx, "exec-err", queue.StatusReview); err == nil {
		t.Error("expected exec injection error")
	}
}

func TestAdvance_ZeroAffected(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	stl := workforceadapter.NewSharedTaskList(s)
	_ = stl.Enqueue(ctx, queue.TaskRow{
		TaskID: "zero-aff", ProjectID: "p1",
		Status: queue.StatusInProgress, CreatedAt: time.Now().UTC(),
	})
	stlFail := workforceadapter.ExportNewSharedTaskListWithAdvanceZeroAffected(s)
	if err := stlFail.Advance(ctx, "zero-aff", queue.StatusReview); !errors.Is(err, queue.ErrTaskNotFound) {
		t.Errorf("got %v, want ErrTaskNotFound on zero affected", err)
	}
}

func TestAdvance_CommitError(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	stl := workforceadapter.NewSharedTaskList(s)
	_ = stl.Enqueue(ctx, queue.TaskRow{
		TaskID: "commit-err", ProjectID: "p1",
		Status: queue.StatusInProgress, CreatedAt: time.Now().UTC(),
	})
	stlFail := workforceadapter.ExportNewSharedTaskListWithFailAdvanceCommit(s)
	if err := stlFail.Advance(ctx, "commit-err", queue.StatusReview); err == nil {
		t.Error("expected commit injection error")
	}
}

func TestScopedAdvance_QueryError(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	stl := workforceadapter.ExportNewScopedSharedTaskListWithFailAdvanceQuery(s, "p1")
	if err := stl.Advance(ctx, "any", queue.StatusDone); err == nil {
		t.Error("expected query injection error")
	}
}

func TestScopedAdvance_ExecError(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	stl := workforceadapter.NewSharedTaskList(s).ScopedTo("p1")
	_ = stl.Enqueue(ctx, queue.TaskRow{
		TaskID: "s-exec-err", ProjectID: "p1",
		Status: queue.StatusInProgress, CreatedAt: time.Now().UTC(),
	})
	stlFail := workforceadapter.ExportNewScopedSharedTaskListWithFailAdvanceExec(s, "p1")
	if err := stlFail.Advance(ctx, "s-exec-err", queue.StatusReview); err == nil {
		t.Error("expected scoped exec injection error")
	}
}

func TestScopedAdvance_ZeroAffected(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	stl := workforceadapter.NewSharedTaskList(s).ScopedTo("p1")
	_ = stl.Enqueue(ctx, queue.TaskRow{
		TaskID: "s-zero-aff", ProjectID: "p1",
		Status: queue.StatusInProgress, CreatedAt: time.Now().UTC(),
	})
	stlFail := workforceadapter.ExportNewScopedSharedTaskListWithAdvanceZeroAffected(s, "p1")
	if err := stlFail.Advance(ctx, "s-zero-aff", queue.StatusReview); !errors.Is(err, queue.ErrTaskNotFound) {
		t.Errorf("got %v, want ErrTaskNotFound on zero affected", err)
	}
}

func TestScopedAdvance_CommitError(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	stl := workforceadapter.NewSharedTaskList(s).ScopedTo("p1")
	_ = stl.Enqueue(ctx, queue.TaskRow{
		TaskID: "s-commit-err", ProjectID: "p1",
		Status: queue.StatusInProgress, CreatedAt: time.Now().UTC(),
	})
	stlFail := workforceadapter.ExportNewScopedSharedTaskListWithFailAdvanceCommit(s, "p1")
	if err := stlFail.Advance(ctx, "s-commit-err", queue.StatusReview); err == nil {
		t.Error("expected scoped commit injection error")
	}
}

func TestAdvance_BadStatusInDB(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	db := s.DB()

	if _, err := db.Exec(`PRAGMA ignore_check_constraints = ON`); err != nil {
		t.Skip("PRAGMA ignore_check_constraints not supported")
	}
	now := time.Now().UTC().Unix()
	if _, err := db.ExecContext(ctx, `INSERT INTO workforce_tasks
		(task_id, project_id, status, thread_id, title, description, priority, error_detail, created_at, updated_at)
		VALUES ('adv-bad', 'p1', 'BOGUS', '', '', '', 0, '', ?, ?)`, now, now); err != nil {
		t.Skipf("could not inject: %v", err)
	}
	_, _ = db.Exec(`PRAGMA ignore_check_constraints = OFF`)

	stl := workforceadapter.NewSharedTaskList(s)
	if err := stl.Advance(ctx, "adv-bad", queue.StatusDone); err == nil {
		t.Error("expected parse-status error")
	}
}
