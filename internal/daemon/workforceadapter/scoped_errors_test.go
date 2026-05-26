package workforceadapter_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/daemon/workforceadapter"
	"github.com/cbip-solutions/hades-system/internal/workforce/queue"
)

func TestScopedSharedTaskList_ClaimBeginTxError(t *testing.T) {
	ctx := context.Background()
	s := openTestStoreNoCleanup(t)
	stl := workforceadapter.NewSharedTaskList(s).ScopedTo("p1")
	_ = s.Close()
	if err := stl.Claim(ctx, "any", "thr"); err == nil {
		t.Error("expected error on closed DB")
	}
}

func TestScopedSharedTaskList_ClaimNotFound(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	stl := workforceadapter.NewSharedTaskList(s).ScopedTo("p1")
	if err := stl.Claim(ctx, "missing", "thr"); !errors.Is(err, queue.ErrTaskNotFound) {
		t.Errorf("got %v, want ErrTaskNotFound", err)
	}
}

func TestScopedSharedTaskList_ClaimQueryError(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	stl := workforceadapter.ExportNewScopedSharedTaskListWithFailClaimQuery(s, "p1")
	if err := stl.Claim(ctx, "any", "thr"); err == nil {
		t.Error("expected query injection error")
	}
}

func TestScopedSharedTaskList_ClaimExecError(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	stl := workforceadapter.NewSharedTaskList(s).ScopedTo("p1")
	if err := stl.Enqueue(ctx, queue.TaskRow{
		TaskID: "claim-exec", ProjectID: "p1", Status: queue.StatusPending, CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	stlFail := workforceadapter.ExportNewScopedSharedTaskListWithFailClaimExec(s, "p1")
	if err := stlFail.Claim(ctx, "claim-exec", "thr"); err == nil {
		t.Error("expected exec injection error")
	}
}

func TestScopedSharedTaskList_ClaimCommitError(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	stl := workforceadapter.NewSharedTaskList(s).ScopedTo("p1")
	if err := stl.Enqueue(ctx, queue.TaskRow{
		TaskID: "claim-com", ProjectID: "p1", Status: queue.StatusPending, CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	stlFail := workforceadapter.ExportNewScopedSharedTaskListWithFailClaimCommit(s, "p1")
	if err := stlFail.Claim(ctx, "claim-com", "thr"); err == nil {
		t.Error("expected commit injection error")
	}
}

func TestScopedSharedTaskList_ClaimNotPending(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	stl := workforceadapter.NewSharedTaskList(s).ScopedTo("p1")
	_ = stl.Enqueue(ctx, queue.TaskRow{
		TaskID: "done-1", ProjectID: "p1", Status: queue.StatusDone, CreatedAt: time.Now().UTC(),
	})
	if err := stl.Claim(ctx, "done-1", "thr"); !errors.Is(err, queue.ErrTaskNotPending) {
		t.Errorf("got %v, want ErrTaskNotPending", err)
	}
}

func TestScopedSharedTaskList_ClaimBadStatusInDB(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	db := s.DB()
	if _, err := db.Exec(`PRAGMA ignore_check_constraints = ON`); err != nil {
		t.Skip("PRAGMA ignore_check_constraints not supported")
	}
	now := time.Now().UTC().Unix()
	if _, err := db.ExecContext(ctx, `INSERT INTO workforce_tasks
		(task_id, project_id, status, thread_id, title, description, priority, error_detail, created_at, updated_at)
		VALUES ('bad-claim', 'p1', 'BOGUS', '', '', '', 0, '', ?, ?)`, now, now); err != nil {
		t.Skipf("could not inject bad status: %v", err)
	}
	_, _ = db.Exec(`PRAGMA ignore_check_constraints = OFF`)

	stl := workforceadapter.NewSharedTaskList(s).ScopedTo("p1")
	if err := stl.Claim(ctx, "bad-claim", "thr"); err == nil {
		t.Error("expected parse error for bad status")
	}
}

func TestScopedSharedTaskList_AdvanceBeginTxError(t *testing.T) {
	ctx := context.Background()
	s := openTestStoreNoCleanup(t)
	stl := workforceadapter.NewSharedTaskList(s).ScopedTo("p1")
	_ = s.Close()
	if err := stl.Advance(ctx, "any", queue.StatusDone); err == nil {
		t.Error("expected error on closed DB")
	}
}

func TestScopedSharedTaskList_AdvanceNotFound(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	stl := workforceadapter.NewSharedTaskList(s).ScopedTo("p1")
	if err := stl.Advance(ctx, "missing", queue.StatusReview); !errors.Is(err, queue.ErrTaskNotFound) {
		t.Errorf("got %v, want ErrTaskNotFound", err)
	}
}

func TestScopedSharedTaskList_AdvanceBadStatusInDB(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	db := s.DB()
	if _, err := db.Exec(`PRAGMA ignore_check_constraints = ON`); err != nil {
		t.Skip("PRAGMA ignore_check_constraints not supported")
	}
	now := time.Now().UTC().Unix()
	if _, err := db.ExecContext(ctx, `INSERT INTO workforce_tasks
		(task_id, project_id, status, thread_id, title, description, priority, error_detail, created_at, updated_at)
		VALUES ('bad-adv', 'p1', 'BOGUS', '', '', '', 0, '', ?, ?)`, now, now); err != nil {
		t.Skipf("could not inject: %v", err)
	}
	_, _ = db.Exec(`PRAGMA ignore_check_constraints = OFF`)

	stl := workforceadapter.NewSharedTaskList(s).ScopedTo("p1")
	if err := stl.Advance(ctx, "bad-adv", queue.StatusDone); err == nil {
		t.Error("expected parse error")
	}
}

func TestScopedSharedTaskList_GetError(t *testing.T) {
	ctx := context.Background()
	s := openTestStoreNoCleanup(t)
	stl := workforceadapter.NewSharedTaskList(s).ScopedTo("p1")
	_ = s.Close()
	if _, err := stl.Get(ctx, "any"); err == nil {
		t.Error("expected error on closed DB")
	}
}

func TestScopedSharedTaskList_GetBadStatusInDB(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	db := s.DB()
	if _, err := db.Exec(`PRAGMA ignore_check_constraints = ON`); err != nil {
		t.Skip("PRAGMA ignore_check_constraints not supported")
	}
	now := time.Now().UTC().Unix()
	if _, err := db.ExecContext(ctx, `INSERT INTO workforce_tasks
		(task_id, project_id, status, thread_id, title, description, priority, error_detail, created_at, updated_at)
		VALUES ('bad-get', 'p1', 'BOGUS', '', '', '', 0, '', ?, ?)`, now, now); err != nil {
		t.Skipf("could not inject: %v", err)
	}
	_, _ = db.Exec(`PRAGMA ignore_check_constraints = OFF`)

	stl := workforceadapter.NewSharedTaskList(s).ScopedTo("p1")
	if _, err := stl.Get(ctx, "bad-get"); err == nil {
		t.Error("expected parse error")
	}
}

func TestScopedSharedTaskList_ListByStatusError(t *testing.T) {
	ctx := context.Background()
	s := openTestStoreNoCleanup(t)
	stl := workforceadapter.NewSharedTaskList(s).ScopedTo("p1")
	_ = s.Close()
	if _, err := stl.ListByStatus(ctx, "p1", queue.StatusPending); err == nil {
		t.Error("expected error on closed DB")
	}
}

func TestScopedSharedTaskList_ListByStatusScanError(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	stl := workforceadapter.NewSharedTaskList(s).ScopedTo("p1")
	_ = stl.Enqueue(ctx, queue.TaskRow{
		TaskID: "ls-1", ProjectID: "p1", Status: queue.StatusPending, CreatedAt: time.Now().UTC(),
	})
	stlFail := workforceadapter.ExportNewScopedSharedTaskListWithFailScan(s, "p1")
	if _, err := stlFail.ListByStatus(ctx, "p1", queue.StatusPending); err == nil {
		t.Error("expected scan error")
	}
}

func TestScopedSharedTaskList_ByThreadError(t *testing.T) {
	ctx := context.Background()
	s := openTestStoreNoCleanup(t)
	stl := workforceadapter.NewSharedTaskList(s).ScopedTo("p1")
	_ = s.Close()
	if _, err := stl.ByThread(ctx, "thr"); err == nil {
		t.Error("expected error on closed DB")
	}
}

func TestScopedSharedTaskList_ByThreadScanError(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	stl := workforceadapter.NewSharedTaskList(s).ScopedTo("p1")
	_ = stl.Enqueue(ctx, queue.TaskRow{
		TaskID: "bt-1", ProjectID: "p1", ThreadID: "thr-bt",
		Status: queue.StatusInProgress, CreatedAt: time.Now().UTC(),
	})
	stlFail := workforceadapter.ExportNewScopedSharedTaskListWithFailScan(s, "p1")
	if _, err := stlFail.ByThread(ctx, "thr-bt"); err == nil {
		t.Error("expected scan error")
	}
}

func TestScopedCheckpoint_DrainBeginError(t *testing.T) {
	ctx := context.Background()
	s := openTestStoreNoCleanup(t)
	cpq := workforceadapter.NewCheckpointQueue(s).ScopedTo("p1")
	_ = s.Close()
	if _, err := cpq.Drain(ctx, "any"); err == nil {
		t.Error("expected error on closed DB")
	}
}

func TestScopedCheckpoint_DrainQueryError(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	cpq := workforceadapter.ExportNewScopedCheckpointQueueWithFailDrainQuery(s, "p1")
	if _, err := cpq.Drain(ctx, "any"); err == nil {
		t.Error("expected query injection error")
	}
}

func TestScopedCheckpoint_DrainScanError(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	cpq := workforceadapter.NewCheckpointQueue(s).ScopedTo("p1")
	_ = cpq.Put(ctx, queue.Checkpoint{
		TaskID: "scan-cp", ProjectID: "p1", ThreadID: "thr",
		StateJSON: `{}`, SeqNum: 1, CreatedAt: time.Now().UTC(),
	})
	cpqFail := workforceadapter.ExportNewScopedCheckpointQueueWithFailScan(s, "p1")
	if _, err := cpqFail.Drain(ctx, "scan-cp"); err == nil {
		t.Error("expected scan injection error")
	}
}

func TestScopedCheckpoint_DrainExecError(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	cpq := workforceadapter.NewCheckpointQueue(s).ScopedTo("p1")
	_ = cpq.Put(ctx, queue.Checkpoint{
		TaskID: "exec-cp", ProjectID: "p1", ThreadID: "thr",
		StateJSON: `{}`, SeqNum: 1, CreatedAt: time.Now().UTC(),
	})
	cpqFail := workforceadapter.ExportNewScopedCheckpointQueueWithFailDrainExec(s, "p1")
	if _, err := cpqFail.Drain(ctx, "exec-cp"); err == nil {
		t.Error("expected exec injection error")
	}
}

func TestScopedCheckpoint_DrainCommitError(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	cpq := workforceadapter.NewCheckpointQueue(s).ScopedTo("p1")
	_ = cpq.Put(ctx, queue.Checkpoint{
		TaskID: "com-cp", ProjectID: "p1", ThreadID: "thr",
		StateJSON: `{}`, SeqNum: 1, CreatedAt: time.Now().UTC(),
	})
	cpqFail := workforceadapter.ExportNewScopedCheckpointQueueWithFailDrainCommit(s, "p1")
	if _, err := cpqFail.Drain(ctx, "com-cp"); err == nil {
		t.Error("expected commit injection error")
	}
}

func TestScopedCheckpoint_DrainEmpty(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	cpq := workforceadapter.NewCheckpointQueue(s).ScopedTo("p1")

	got, err := cpq.Drain(ctx, "no-such")
	if err != nil {
		t.Fatalf("Drain empty: %v", err)
	}
	if got == nil {
		t.Error("Drain empty returned nil; want non-nil empty slice")
	}
	if len(got) != 0 {
		t.Errorf("Drain empty len = %d, want 0", len(got))
	}
}

func TestScopedCheckpoint_PeekError(t *testing.T) {
	ctx := context.Background()
	s := openTestStoreNoCleanup(t)
	cpq := workforceadapter.NewCheckpointQueue(s).ScopedTo("p1")
	_ = s.Close()
	if _, err := cpq.Peek(ctx, "any"); err == nil {
		t.Error("expected error on closed DB")
	}
}

func TestScopedCheckpoint_ByThreadError(t *testing.T) {
	ctx := context.Background()
	s := openTestStoreNoCleanup(t)
	cpq := workforceadapter.NewCheckpointQueue(s).ScopedTo("p1")
	_ = s.Close()
	if _, err := cpq.ByThread(ctx, "thr"); err == nil {
		t.Error("expected error on closed DB")
	}
}

func TestScopedFixPrompt_DrainBeginError(t *testing.T) {
	ctx := context.Background()
	s := openTestStoreNoCleanup(t)
	fpq := workforceadapter.NewFixPromptQueue(s).ScopedTo("p1")
	_ = s.Close()
	if _, err := fpq.DrainByWorker(ctx, "w"); err == nil {
		t.Error("expected error on closed DB")
	}
}

func TestScopedFixPrompt_DrainQueryError(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	fpq := workforceadapter.ExportNewScopedFixPromptQueueWithFailDrainQuery(s, "p1")
	if _, err := fpq.DrainByWorker(ctx, "w"); err == nil {
		t.Error("expected query injection error")
	}
}

func TestScopedFixPrompt_DrainScanError(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	fpq := workforceadapter.NewFixPromptQueue(s).ScopedTo("p1")
	_ = fpq.Put(ctx, queue.FixPrompt{
		TaskID: "fp-scan", ProjectID: "p1", WorkerID: "w-scan",
		ReviewerTier: queue.ReviewerTierL2, Severity: queue.SeverityMinor,
		CreatedAt: time.Now().UTC(),
	})
	fpqFail := workforceadapter.ExportNewScopedFixPromptQueueWithFailScan(s, "p1")
	if _, err := fpqFail.DrainByWorker(ctx, "w-scan"); err == nil {
		t.Error("expected scan injection error")
	}
}

func TestScopedFixPrompt_DrainExecError(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	fpq := workforceadapter.NewFixPromptQueue(s).ScopedTo("p1")
	_ = fpq.Put(ctx, queue.FixPrompt{
		TaskID: "fp-exec", ProjectID: "p1", WorkerID: "w-exec",
		ReviewerTier: queue.ReviewerTierL2, Severity: queue.SeverityMinor,
		CreatedAt: time.Now().UTC(),
	})
	fpqFail := workforceadapter.ExportNewScopedFixPromptQueueWithFailDrainExec(s, "p1")
	if _, err := fpqFail.DrainByWorker(ctx, "w-exec"); err == nil {
		t.Error("expected exec injection error")
	}
}

func TestScopedFixPrompt_DrainCommitError(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	fpq := workforceadapter.NewFixPromptQueue(s).ScopedTo("p1")
	_ = fpq.Put(ctx, queue.FixPrompt{
		TaskID: "fp-com", ProjectID: "p1", WorkerID: "w-com",
		ReviewerTier: queue.ReviewerTierL2, Severity: queue.SeverityMinor,
		CreatedAt: time.Now().UTC(),
	})
	fpqFail := workforceadapter.ExportNewScopedFixPromptQueueWithFailDrainCommit(s, "p1")
	if _, err := fpqFail.DrainByWorker(ctx, "w-com"); err == nil {
		t.Error("expected commit injection error")
	}
}

func TestScopedFixPrompt_DrainEmpty(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	fpq := workforceadapter.NewFixPromptQueue(s).ScopedTo("p1")

	got, err := fpq.DrainByWorker(ctx, "no-such")
	if err != nil {
		t.Fatalf("DrainByWorker empty: %v", err)
	}
	if got == nil {
		t.Error("DrainByWorker empty returned nil; want non-nil empty slice")
	}
	if len(got) != 0 {
		t.Errorf("DrainByWorker empty len = %d, want 0", len(got))
	}
}

func TestScopedFixPrompt_PendingError(t *testing.T) {
	ctx := context.Background()
	s := openTestStoreNoCleanup(t)
	fpq := workforceadapter.NewFixPromptQueue(s).ScopedTo("p1")
	_ = s.Close()
	if _, err := fpq.PendingByWorker(ctx, "w"); err == nil {
		t.Error("expected error on closed DB")
	}
}
