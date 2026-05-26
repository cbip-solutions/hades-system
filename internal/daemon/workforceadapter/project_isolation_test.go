package workforceadapter_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/daemon/workforceadapter"
	"github.com/cbip-solutions/hades-system/internal/workforce/queue"
)

func TestProjectIDIsolation_NoCrossProjectLeak(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	stl := workforceadapter.NewSharedTaskList(s)

	rowA := queue.TaskRow{
		TaskID:    "shared-id",
		ProjectID: "proj-A",
		Title:     "A-task",
		Status:    queue.StatusPending,
		CreatedAt: time.Now().UTC(),
	}
	if err := stl.Enqueue(ctx, rowA); err != nil {
		t.Fatalf("Enqueue A: %v", err)
	}

	rowB := queue.TaskRow{
		TaskID:    "shared-id",
		ProjectID: "proj-B",
		Title:     "B-task",
		Status:    queue.StatusPending,
		CreatedAt: time.Now().UTC(),
	}
	if err := stl.Enqueue(ctx, rowB); err != nil {
		t.Fatalf("Enqueue B: %v", err)
	}

	stlA := stl.ScopedTo("proj-A")
	stlB := stl.ScopedTo("proj-B")

	gotA, err := stlA.Get(ctx, "shared-id")
	if err != nil {
		t.Fatalf("ScopedTo(A).Get: %v", err)
	}
	if gotA.Title != "A-task" {
		t.Errorf("Scoped(A).Get: title = %q, want A-task", gotA.Title)
	}
	if gotA.ProjectID != "proj-A" {
		t.Errorf("Scoped(A).Get: ProjectID = %q, want proj-A", gotA.ProjectID)
	}

	gotB, err := stlB.Get(ctx, "shared-id")
	if err != nil {
		t.Fatalf("ScopedTo(B).Get: %v", err)
	}
	if gotB.Title != "B-task" {
		t.Errorf("Scoped(B).Get: title = %q, want B-task", gotB.Title)
	}
	if gotB.ProjectID != "proj-B" {
		t.Errorf("Scoped(B).Get: ProjectID = %q, want proj-B", gotB.ProjectID)
	}

	if err := stlA.Claim(ctx, "shared-id", "thr-A1"); err != nil {
		t.Fatalf("ScopedTo(A).Claim: %v", err)
	}

	gotB2, _ := stlB.Get(ctx, "shared-id")
	if gotB2.Status != queue.StatusPending {
		t.Errorf("Scoped(B) row status = %v, want pending (claim must not leak)", gotB2.Status)
	}

	gotA2, _ := stlA.Get(ctx, "shared-id")
	if gotA2.Status != queue.StatusInProgress {
		t.Errorf("Scoped(A) row status = %v, want in_progress", gotA2.Status)
	}
	if gotA2.ThreadID != "thr-A1" {
		t.Errorf("Scoped(A) ThreadID = %q, want thr-A1", gotA2.ThreadID)
	}

	if err := stlA.Advance(ctx, "shared-id", queue.StatusReview); err != nil {
		t.Fatalf("ScopedTo(A).Advance: %v", err)
	}
	gotB3, _ := stlB.Get(ctx, "shared-id")
	if gotB3.Status != queue.StatusPending {
		t.Errorf("after A advance, Scoped(B) status = %v, want pending", gotB3.Status)
	}

	gotA3, _ := stlA.Get(ctx, "shared-id")
	if gotA3.Status != queue.StatusReview {
		t.Errorf("Scoped(A) status after Advance = %v, want review", gotA3.Status)
	}
}

func TestProjectIDIsolation_GetNotFoundInOtherScope(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	stl := workforceadapter.NewSharedTaskList(s)

	_ = stl.Enqueue(ctx, queue.TaskRow{
		TaskID:    "only-A",
		ProjectID: "proj-A",
		Status:    queue.StatusPending,
		CreatedAt: time.Now().UTC(),
	})

	stlB := stl.ScopedTo("proj-B")
	if _, err := stlB.Get(ctx, "only-A"); !errors.Is(err, queue.ErrTaskNotFound) {
		t.Errorf("Scoped(B).Get on A-only task = %v, want ErrTaskNotFound", err)
	}
	if err := stlB.Claim(ctx, "only-A", "thr"); !errors.Is(err, queue.ErrTaskNotFound) {
		t.Errorf("Scoped(B).Claim on A-only task = %v, want ErrTaskNotFound", err)
	}
	if err := stlB.Advance(ctx, "only-A", queue.StatusDone); !errors.Is(err, queue.ErrTaskNotFound) {
		t.Errorf("Scoped(B).Advance on A-only task = %v, want ErrTaskNotFound", err)
	}
}

func TestProjectIDIsolation_ByThread(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	stl := workforceadapter.NewSharedTaskList(s)

	_ = stl.Enqueue(ctx, queue.TaskRow{
		TaskID: "t-A", ProjectID: "proj-A", ThreadID: "shared-thr",
		Status: queue.StatusInProgress, CreatedAt: time.Now().UTC(),
	})
	_ = stl.Enqueue(ctx, queue.TaskRow{
		TaskID: "t-B", ProjectID: "proj-B", ThreadID: "shared-thr",
		Status: queue.StatusInProgress, CreatedAt: time.Now().UTC(),
	})

	rowsA, _ := stl.ScopedTo("proj-A").ByThread(ctx, "shared-thr")
	if len(rowsA) != 1 || rowsA[0].TaskID != "t-A" {
		t.Errorf("Scoped(A).ByThread = %v, want [{t-A}]", rowsA)
	}
	rowsB, _ := stl.ScopedTo("proj-B").ByThread(ctx, "shared-thr")
	if len(rowsB) != 1 || rowsB[0].TaskID != "t-B" {
		t.Errorf("Scoped(B).ByThread = %v, want [{t-B}]", rowsB)
	}
}

func TestProjectIDIsolation_ListByStatus(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	stl := workforceadapter.NewSharedTaskList(s)

	_ = stl.Enqueue(ctx, queue.TaskRow{TaskID: "t-A1", ProjectID: "proj-A", Status: queue.StatusPending, CreatedAt: time.Now().UTC()})
	_ = stl.Enqueue(ctx, queue.TaskRow{TaskID: "t-A2", ProjectID: "proj-A", Status: queue.StatusPending, CreatedAt: time.Now().UTC()})
	_ = stl.Enqueue(ctx, queue.TaskRow{TaskID: "t-B1", ProjectID: "proj-B", Status: queue.StatusPending, CreatedAt: time.Now().UTC()})

	stlA := stl.ScopedTo("proj-A")
	rowsA, err := stlA.ListByStatus(ctx, "proj-A", queue.StatusPending)
	if err != nil {
		t.Fatalf("Scoped(A).ListByStatus: %v", err)
	}
	if len(rowsA) != 2 {
		t.Errorf("Scoped(A).ListByStatus count = %d, want 2", len(rowsA))
	}
	for _, r := range rowsA {
		if r.ProjectID != "proj-A" {
			t.Errorf("Scoped(A).ListByStatus row ProjectID = %q, want proj-A", r.ProjectID)
		}
	}

	rowsCross, _ := stlA.ListByStatus(ctx, "proj-B", queue.StatusPending)
	if len(rowsCross) != 0 {
		t.Errorf("Scoped(A).ListByStatus(proj-B) = %d rows, want 0 (scope must win)", len(rowsCross))
	}
}

func TestProjectIDIsolation_Checkpoint_DrainPeekByThread(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	cpq := workforceadapter.NewCheckpointQueue(s)

	_ = cpq.Put(ctx, queue.Checkpoint{
		TaskID: "shared-task", ProjectID: "proj-A", ThreadID: "thr-1",
		StateJSON: `{"who":"A"}`, SeqNum: 1, CreatedAt: time.Now().UTC(),
	})
	_ = cpq.Put(ctx, queue.Checkpoint{
		TaskID: "shared-task", ProjectID: "proj-B", ThreadID: "thr-1",
		StateJSON: `{"who":"B"}`, SeqNum: 1, CreatedAt: time.Now().UTC(),
	})

	cpqA := cpq.ScopedTo("proj-A")
	peekA, err := cpqA.Peek(ctx, "shared-task")
	if err != nil {
		t.Fatalf("Scoped(A).Peek: %v", err)
	}
	if len(peekA) != 1 || peekA[0].StateJSON != `{"who":"A"}` {
		t.Errorf("Scoped(A).Peek leak: got %+v", peekA)
	}

	drainA, err := cpqA.Drain(ctx, "shared-task")
	if err != nil {
		t.Fatalf("Scoped(A).Drain: %v", err)
	}
	if len(drainA) != 1 {
		t.Errorf("Scoped(A).Drain count = %d, want 1", len(drainA))
	}

	cpqB := cpq.ScopedTo("proj-B")
	peekB, _ := cpqB.Peek(ctx, "shared-task")
	if len(peekB) != 1 || peekB[0].StateJSON != `{"who":"B"}` {
		t.Errorf("Scoped(B) state polluted by Scoped(A).Drain: %+v", peekB)
	}

	btA, _ := cpqA.ByThread(ctx, "thr-1")
	for _, c := range btA {
		if c.ProjectID != "proj-A" {
			t.Errorf("Scoped(A).ByThread leak: ProjectID=%q", c.ProjectID)
		}
	}
}

func TestScopedSharedTaskList_EnqueueProjectMismatch(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	stl := workforceadapter.NewSharedTaskList(s).ScopedTo("proj-A")

	err := stl.Enqueue(ctx, queue.TaskRow{
		TaskID:    "mismatch-1",
		ProjectID: "proj-X",
		Status:    queue.StatusPending,
		CreatedAt: time.Now().UTC(),
	})
	if !errors.Is(err, queue.ErrProjectIDMismatch) {
		t.Errorf("got %v, want ErrProjectIDMismatch", err)
	}
}

func TestScopedSharedTaskList_EnqueueAutoFillsProject(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	stl := workforceadapter.NewSharedTaskList(s).ScopedTo("proj-fill")

	err := stl.Enqueue(ctx, queue.TaskRow{
		TaskID:    "fill-1",
		Status:    queue.StatusPending,
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	got, _ := stl.Get(ctx, "fill-1")
	if got.ProjectID != "proj-fill" {
		t.Errorf("ProjectID = %q, want proj-fill (auto-fill)", got.ProjectID)
	}
}

func TestScopedSharedTaskList_PanicEmptyProject(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("ScopedTo(\"\") should panic")
		}
	}()
	s := openTestStore(t)
	workforceadapter.NewSharedTaskList(s).ScopedTo("")
}

func TestScopedSharedTaskList_ProjectIDGetter(t *testing.T) {
	s := openTestStore(t)
	scoped := workforceadapter.NewSharedTaskList(s).ScopedTo("proj-getter")
	if scoped.ProjectID() != "proj-getter" {
		t.Errorf("ProjectID() = %q, want proj-getter", scoped.ProjectID())
	}
}

func TestScopedCheckpointQueue_PutProjectMismatch(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	cpq := workforceadapter.NewCheckpointQueue(s).ScopedTo("proj-A")

	err := cpq.Put(ctx, queue.Checkpoint{
		TaskID:    "cp-mis-1",
		ProjectID: "proj-X",
		ThreadID:  "thr",
		StateJSON: `{}`,
		SeqNum:    1,
		CreatedAt: time.Now().UTC(),
	})
	if !errors.Is(err, queue.ErrProjectIDMismatch) {
		t.Errorf("got %v, want ErrProjectIDMismatch", err)
	}
}

func TestScopedCheckpointQueue_PutAutoFillsProject(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	cpq := workforceadapter.NewCheckpointQueue(s).ScopedTo("proj-cp-fill")

	err := cpq.Put(ctx, queue.Checkpoint{
		TaskID: "cp-fill-1", ThreadID: "thr",
		StateJSON: `{}`, SeqNum: 1, CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	peek, _ := cpq.Peek(context.Background(), "cp-fill-1")
	if len(peek) != 1 || peek[0].ProjectID != "proj-cp-fill" {
		t.Errorf("Peek after auto-fill = %+v", peek)
	}
}

func TestScopedCheckpointQueue_PanicEmptyProject(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("CheckpointQueue ScopedTo(\"\") should panic")
		}
	}()
	s := openTestStore(t)
	workforceadapter.NewCheckpointQueue(s).ScopedTo("")
}

func TestScopedCheckpointQueue_ProjectIDGetter(t *testing.T) {
	s := openTestStore(t)
	if got := workforceadapter.NewCheckpointQueue(s).ScopedTo("p-cp").ProjectID(); got != "p-cp" {
		t.Errorf("ProjectID() = %q, want p-cp", got)
	}
}

func TestScopedFixPromptQueue_PutProjectMismatch(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	fpq := workforceadapter.NewFixPromptQueue(s).ScopedTo("proj-A")

	err := fpq.Put(ctx, queue.FixPrompt{
		TaskID: "fp-mis", ProjectID: "proj-X",
		WorkerID: "w1", ReviewerTier: queue.ReviewerTierL2,
		Severity: queue.SeverityMinor, CreatedAt: time.Now().UTC(),
	})
	if !errors.Is(err, queue.ErrProjectIDMismatch) {
		t.Errorf("got %v, want ErrProjectIDMismatch", err)
	}
}

func TestScopedFixPromptQueue_PutAutoFillsProject(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	fpq := workforceadapter.NewFixPromptQueue(s).ScopedTo("proj-fp-fill")

	err := fpq.Put(ctx, queue.FixPrompt{
		TaskID: "fp-fill", WorkerID: "w-fill",
		ReviewerTier: queue.ReviewerTierL2,
		Severity:     queue.SeverityMinor,
		CreatedAt:    time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	pending, _ := fpq.PendingByWorker(ctx, "w-fill")
	if len(pending) != 1 || pending[0].ProjectID != "proj-fp-fill" {
		t.Errorf("Pending after auto-fill = %+v", pending)
	}
}

func TestScopedFixPromptQueue_PanicEmptyProject(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("FixPromptQueue ScopedTo(\"\") should panic")
		}
	}()
	s := openTestStore(t)
	workforceadapter.NewFixPromptQueue(s).ScopedTo("")
}

func TestScopedFixPromptQueue_ProjectIDGetter(t *testing.T) {
	s := openTestStore(t)
	if got := workforceadapter.NewFixPromptQueue(s).ScopedTo("p-fp").ProjectID(); got != "p-fp" {
		t.Errorf("ProjectID() = %q, want p-fp", got)
	}
}

func TestProjectIDIsolation_FixPrompt_DrainPending(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	fpq := workforceadapter.NewFixPromptQueue(s)

	_ = fpq.Put(ctx, queue.FixPrompt{
		TaskID: "fp-A", ProjectID: "proj-A", WorkerID: "worker-1",
		ReviewerTier: queue.ReviewerTierL2, Severity: queue.SeverityMinor,
		PromptText: "A's prompt", CreatedAt: time.Now().UTC(),
	})
	_ = fpq.Put(ctx, queue.FixPrompt{
		TaskID: "fp-B", ProjectID: "proj-B", WorkerID: "worker-1",
		ReviewerTier: queue.ReviewerTierL2, Severity: queue.SeverityMinor,
		PromptText: "B's prompt", CreatedAt: time.Now().UTC(),
	})

	fpqA := fpq.ScopedTo("proj-A")
	pendingA, err := fpqA.PendingByWorker(ctx, "worker-1")
	if err != nil {
		t.Fatalf("Scoped(A).PendingByWorker: %v", err)
	}
	if len(pendingA) != 1 || pendingA[0].PromptText != "A's prompt" {
		t.Errorf("Scoped(A).PendingByWorker leak: %+v", pendingA)
	}

	drainedA, _ := fpqA.DrainByWorker(ctx, "worker-1")
	if len(drainedA) != 1 || drainedA[0].PromptText != "A's prompt" {
		t.Errorf("Scoped(A).DrainByWorker leak: %+v", drainedA)
	}

	fpqB := fpq.ScopedTo("proj-B")
	pendingB, _ := fpqB.PendingByWorker(ctx, "worker-1")
	if len(pendingB) != 1 || pendingB[0].PromptText != "B's prompt" {
		t.Errorf("Scoped(B) state polluted by Scoped(A).DrainByWorker: %+v", pendingB)
	}
}
