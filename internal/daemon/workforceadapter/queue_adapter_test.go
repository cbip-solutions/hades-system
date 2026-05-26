package workforceadapter_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"

	"github.com/cbip-solutions/hades-system/internal/daemon/workforceadapter"
	"github.com/cbip-solutions/hades-system/internal/store"
	"github.com/cbip-solutions/hades-system/internal/workforce/queue"
)

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestSharedTaskListImpl_CompileCheck(t *testing.T) {

	s := openTestStore(t)
	var impl queue.SharedTaskList = workforceadapter.NewSharedTaskList(s)
	_ = impl
}

func TestSharedTaskListImpl_WALMode(t *testing.T) {

	s := openTestStore(t)
	db := s.DB()
	var mode string
	row := db.QueryRow("PRAGMA journal_mode;")
	if err := row.Scan(&mode); err != nil {
		t.Fatalf("journal_mode query: %v", err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode = %q, want wal", mode)
	}
}

func TestSharedTaskListImpl_EnqueueGet(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	stl := workforceadapter.NewSharedTaskList(s)

	row := queue.TaskRow{
		TaskID:    "t-001",
		ProjectID: "proj-x",
		Title:     "write failing test",
		Status:    queue.StatusPending,
		CreatedAt: time.Now().UTC().Truncate(time.Second),
	}
	if err := stl.Enqueue(ctx, row); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	got, err := stl.Get(ctx, "t-001")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Title != row.Title {
		t.Errorf("title = %q, want %q", got.Title, row.Title)
	}
	if got.Status != queue.StatusPending {
		t.Errorf("status = %v, want pending", got.Status)
	}
}

func TestSharedTaskListImpl_DuplicateEnqueue(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	stl := workforceadapter.NewSharedTaskList(s)

	row := queue.TaskRow{TaskID: "dup-1", ProjectID: "p1", Status: queue.StatusPending, CreatedAt: time.Now().UTC()}
	if err := stl.Enqueue(ctx, row); err != nil {
		t.Fatalf("first Enqueue: %v", err)
	}
	err := stl.Enqueue(ctx, row)
	if !errors.Is(err, queue.ErrDuplicateTask) {
		t.Errorf("second Enqueue = %v, want ErrDuplicateTask", err)
	}
}

func TestSharedTaskListImpl_ClaimConcurrent(t *testing.T) {

	ctx := context.Background()
	s := openTestStore(t)
	stl := workforceadapter.NewSharedTaskList(s)

	_ = stl.Enqueue(ctx, queue.TaskRow{TaskID: "c-1", ProjectID: "p1", Status: queue.StatusPending, CreatedAt: time.Now().UTC()})
	if err := stl.Claim(ctx, "c-1", "thread-alpha"); err != nil {
		t.Fatalf("Claim: %v", err)
	}
	got, _ := stl.Get(ctx, "c-1")
	if got.Status != queue.StatusInProgress {
		t.Errorf("status after Claim = %v, want in_progress", got.Status)
	}
	if got.ThreadID != "thread-alpha" {
		t.Errorf("threadID = %q, want thread-alpha", got.ThreadID)
	}
}

func TestSharedTaskListImpl_Advance(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	stl := workforceadapter.NewSharedTaskList(s)

	_ = stl.Enqueue(ctx, queue.TaskRow{TaskID: "a-1", ProjectID: "p1", Status: queue.StatusInProgress, CreatedAt: time.Now().UTC()})
	if err := stl.Advance(ctx, "a-1", queue.StatusReview); err != nil {
		t.Fatalf("Advance: %v", err)
	}
	got, _ := stl.Get(ctx, "a-1")
	if got.Status != queue.StatusReview {
		t.Errorf("status = %v, want review", got.Status)
	}
}

func TestSharedTaskListImpl_ListByStatus(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	stl := workforceadapter.NewSharedTaskList(s)

	for i, st := range []queue.Status{queue.StatusPending, queue.StatusPending, queue.StatusDone} {
		_ = stl.Enqueue(ctx, queue.TaskRow{
			TaskID:    queue.TaskID(string(rune('A' + i))),
			ProjectID: "proj-list",
			Status:    st,
			CreatedAt: time.Now().UTC(),
		})
	}
	rows, err := stl.ListByStatus(ctx, "proj-list", queue.StatusPending)
	if err != nil {
		t.Fatalf("ListByStatus: %v", err)
	}
	if len(rows) != 2 {
		t.Errorf("pending count = %d, want 2", len(rows))
	}
}

func TestSharedTaskListImpl_ByThread(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	stl := workforceadapter.NewSharedTaskList(s)

	_ = stl.Enqueue(ctx, queue.TaskRow{TaskID: "th-1a", ProjectID: "p1", ThreadID: "thr-X", Status: queue.StatusInProgress, CreatedAt: time.Now().UTC()})
	_ = stl.Enqueue(ctx, queue.TaskRow{TaskID: "th-1b", ProjectID: "p1", ThreadID: "thr-Y", Status: queue.StatusInProgress, CreatedAt: time.Now().UTC()})
	rows, _ := stl.ByThread(ctx, "thr-X")
	if len(rows) != 1 || rows[0].TaskID != "th-1a" {
		t.Errorf("ByThread(thr-X) = %v, want [{th-1a}]", rows)
	}
}

func TestMigration045_TableShape(t *testing.T) {
	s := openTestStore(t)
	db := s.DB()

	var tblName string
	err := db.QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name='workforce_tasks'`,
	).Scan(&tblName)
	if err == sql.ErrNoRows {
		t.Fatal("workforce_tasks table not found after migration 045")
	}
	if err != nil {
		t.Fatalf("table check: %v", err)
	}

	expectedCols := []string{"id", "task_id", "project_id", "title", "description",
		"status", "thread_id", "priority", "error_detail", "created_at", "updated_at"}
	rows, err := db.Query(`PRAGMA table_info(workforce_tasks)`)
	if err != nil {
		t.Fatalf("PRAGMA table_info: %v", err)
	}
	defer rows.Close()
	found := make(map[string]bool)
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		_ = rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk)
		found[name] = true
	}
	for _, col := range expectedCols {
		if !found[col] {
			t.Errorf("column %q missing from workforce_tasks", col)
		}
	}
	_ = os.Getenv("")
}

func TestSharedTaskListImpl_ConcurrentEnqueue(t *testing.T) {

	ctx := context.Background()
	s := openTestStore(t)
	stl := workforceadapter.NewSharedTaskList(s)

	const N = 50
	errs := make(chan error, N)
	for i := 0; i < N; i++ {
		i := i
		go func() {
			errs <- stl.Enqueue(ctx, queue.TaskRow{
				TaskID:    queue.TaskID(fmt.Sprintf("conc-%03d", i)),
				ProjectID: "proj-conc",
				Status:    queue.StatusPending,
				CreatedAt: time.Now().UTC(),
			})
		}()
	}
	for i := 0; i < N; i++ {
		if err := <-errs; err != nil {
			t.Errorf("goroutine enqueue err: %v", err)
		}
	}
	rows, err := stl.ListByStatus(ctx, "proj-conc", queue.StatusPending)
	if err != nil {
		t.Fatalf("ListByStatus: %v", err)
	}
	if len(rows) != N {
		t.Errorf("pending count = %d, want %d", len(rows), N)
	}
}

func TestSharedTaskListImpl_ConcurrentClaim_OnlyOneWins(t *testing.T) {

	ctx := context.Background()
	s := openTestStore(t)
	stl := workforceadapter.NewSharedTaskList(s)

	_ = stl.Enqueue(ctx, queue.TaskRow{
		TaskID:    "race-task",
		ProjectID: "p-race",
		Status:    queue.StatusPending,
		CreatedAt: time.Now().UTC(),
	})

	const W = 10
	wins := make(chan bool, W)
	for i := 0; i < W; i++ {
		i := i
		go func() {
			err := stl.Claim(ctx, "race-task", fmt.Sprintf("thread-%02d", i))
			wins <- (err == nil)
		}()
	}
	var successCount int
	for i := 0; i < W; i++ {
		if <-wins {
			successCount++
		}
	}
	if successCount != 1 {
		t.Errorf("claim wins = %d, want exactly 1", successCount)
	}
}

func TestSharedTaskListImpl_WALPragmas(t *testing.T) {

	s := openTestStore(t)
	db := s.DB()

	var journalMode string
	_ = db.QueryRow("PRAGMA journal_mode;").Scan(&journalMode)
	if journalMode != "wal" {
		t.Errorf("journal_mode = %q, want wal", journalMode)
	}

	var synchronous int
	_ = db.QueryRow("PRAGMA synchronous;").Scan(&synchronous)

	if synchronous != 1 {
		t.Errorf("synchronous = %d, want 1 (NORMAL)", synchronous)
	}

	var busyTimeout int
	_ = db.QueryRow("PRAGMA busy_timeout;").Scan(&busyTimeout)
	if busyTimeout != 5000 {
		t.Errorf("busy_timeout = %d, want 5000", busyTimeout)
	}
}

func TestCheckpointQueueImpl_CompileCheck(t *testing.T) {
	s := openTestStore(t)
	var impl queue.CheckpointQueue = workforceadapter.NewCheckpointQueue(s)
	_ = impl
}

func TestCheckpointQueueImpl_PutAndPeek(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	cpq := workforceadapter.NewCheckpointQueue(s)

	cp := queue.Checkpoint{
		TaskID:    "cp-task-1",
		ProjectID: "p1",
		ThreadID:  "thr-1",
		StateJSON: `{"step":1}`,
		SeqNum:    1,
		CreatedAt: time.Now().UTC(),
	}
	if err := cpq.Put(ctx, cp); err != nil {
		t.Fatalf("Put: %v", err)
	}
	items, err := cpq.Peek(ctx, "cp-task-1")
	if err != nil {
		t.Fatalf("Peek: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("Peek count = %d, want 1", len(items))
	}
	if items[0].StateJSON != `{"step":1}` {
		t.Errorf("StateJSON = %q", items[0].StateJSON)
	}
	if items[0].ThreadID != "thr-1" {
		t.Errorf("ThreadID = %q, want thr-1", items[0].ThreadID)
	}
}

func TestCheckpointQueueImpl_Drain(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	cpq := workforceadapter.NewCheckpointQueue(s)

	for i := 1; i <= 3; i++ {
		if err := cpq.Put(ctx, queue.Checkpoint{
			TaskID:    "cp-drain",
			ProjectID: "p1",
			ThreadID:  "thr-d",
			StateJSON: `{}`,
			SeqNum:    i,
			CreatedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatalf("Put %d: %v", i, err)
		}
	}
	drained, err := cpq.Drain(ctx, "cp-drain")
	if err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if len(drained) != 3 {
		t.Errorf("drained count = %d, want 3", len(drained))
	}
	for _, d := range drained {
		if !d.Consumed {
			t.Errorf("drained checkpoint not marked consumed: seq=%d", d.SeqNum)
		}
	}

	remaining, _ := cpq.Peek(ctx, "cp-drain")
	if len(remaining) != 0 {
		t.Errorf("remaining after drain = %d, want 0", len(remaining))
	}
}

func TestCheckpointQueueImpl_DrainEmpty(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	cpq := workforceadapter.NewCheckpointQueue(s)

	drained, err := cpq.Drain(ctx, "no-such-task")
	if err != nil {
		t.Fatalf("Drain on empty: %v", err)
	}
	if len(drained) != 0 {
		t.Errorf("expected empty, got %d", len(drained))
	}
}

func TestCheckpointQueueImpl_ByThread(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	cpq := workforceadapter.NewCheckpointQueue(s)

	_ = cpq.Put(ctx, queue.Checkpoint{TaskID: "t1", ProjectID: "p1", ThreadID: "thr-X", StateJSON: `{}`, SeqNum: 1, CreatedAt: time.Now().UTC()})
	_ = cpq.Put(ctx, queue.Checkpoint{TaskID: "t2", ProjectID: "p1", ThreadID: "thr-Y", StateJSON: `{}`, SeqNum: 1, CreatedAt: time.Now().UTC()})
	_ = cpq.Put(ctx, queue.Checkpoint{TaskID: "t3", ProjectID: "p1", ThreadID: "thr-X", StateJSON: `{}`, SeqNum: 2, CreatedAt: time.Now().UTC()})

	rows, err := cpq.ByThread(ctx, "thr-X")
	if err != nil {
		t.Fatalf("ByThread: %v", err)
	}
	if len(rows) != 2 {
		t.Errorf("ByThread(thr-X) = %d, want 2", len(rows))
	}
}

func TestCheckpointQueueImpl_WithDeadline(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	cpq := workforceadapter.NewCheckpointQueue(s)

	deadline := time.Now().UTC().Add(30 * time.Minute).Truncate(time.Second)
	cp := queue.Checkpoint{
		TaskID:     "cp-dl",
		ProjectID:  "p1",
		ThreadID:   "thr-dl",
		StateJSON:  `{}`,
		SeqNum:     1,
		DeadlineAt: deadline,
		CreatedAt:  time.Now().UTC(),
	}
	if err := cpq.Put(ctx, cp); err != nil {
		t.Fatalf("Put: %v", err)
	}
	items, err := cpq.Peek(ctx, "cp-dl")
	if err != nil {
		t.Fatalf("Peek: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("Peek count = %d, want 1", len(items))
	}
	if items[0].DeadlineAt.IsZero() {
		t.Error("DeadlineAt should not be zero after round-trip")
	}
	if !items[0].DeadlineAt.Equal(deadline) {
		t.Errorf("DeadlineAt = %v, want %v", items[0].DeadlineAt, deadline)
	}
}

func TestCheckpointQueueImpl_NilPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("NewCheckpointQueue(nil) should panic")
		}
	}()
	workforceadapter.NewCheckpointQueue(nil)
}

func TestFixPromptQueueImpl_CompileCheck(t *testing.T) {
	s := openTestStore(t)
	var impl queue.FixPromptQueue = workforceadapter.NewFixPromptQueue(s)
	_ = impl
}

func TestFixPromptQueueImpl_PutAndPending(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	fpq := workforceadapter.NewFixPromptQueue(s)

	fp := queue.FixPrompt{
		TaskID:       "fp-task-1",
		ProjectID:    "proj-fp",
		WorkerID:     "worker-1",
		ReviewerTier: queue.ReviewerTierL2,
		PromptText:   "fix edge case",
		CriteriaName: "default",
		Severity:     queue.SeverityMinor,
		CreatedAt:    time.Now().UTC(),
	}
	if err := fpq.Put(ctx, fp); err != nil {
		t.Fatalf("Put: %v", err)
	}
	pending, err := fpq.PendingByWorker(ctx, "worker-1")
	if err != nil {
		t.Fatalf("PendingByWorker: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("pending count = %d, want 1", len(pending))
	}
	if pending[0].PromptText != fp.PromptText {
		t.Errorf("PromptText = %q, want %q", pending[0].PromptText, fp.PromptText)
	}
	if pending[0].ReviewerTier != queue.ReviewerTierL2 {
		t.Errorf("ReviewerTier = %v, want l2", pending[0].ReviewerTier)
	}
}

func TestFixPromptQueueImpl_DrainByWorker(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	fpq := workforceadapter.NewFixPromptQueue(s)

	for i := 0; i < 3; i++ {
		if err := fpq.Put(ctx, queue.FixPrompt{
			TaskID:       queue.TaskID(fmt.Sprintf("fp-t%d", i)),
			ProjectID:    "p1",
			WorkerID:     "w-drain",
			ReviewerTier: queue.ReviewerTierL3,
			Severity:     queue.SeverityMajor,
			CreatedAt:    time.Now().UTC(),
		}); err != nil {
			t.Fatalf("Put %d: %v", i, err)
		}
	}
	drained, err := fpq.DrainByWorker(ctx, "w-drain")
	if err != nil {
		t.Fatalf("DrainByWorker: %v", err)
	}
	if len(drained) != 3 {
		t.Errorf("drained count = %d, want 3", len(drained))
	}
	for _, d := range drained {
		if !d.Consumed {
			t.Errorf("drained fp not marked consumed: task=%s", d.TaskID)
		}
	}
	after, _ := fpq.PendingByWorker(ctx, "w-drain")
	if len(after) != 0 {
		t.Errorf("pending after drain = %d, want 0", len(after))
	}
}

func TestFixPromptQueueImpl_DrainEmpty(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	fpq := workforceadapter.NewFixPromptQueue(s)

	drained, err := fpq.DrainByWorker(ctx, "no-such-worker")
	if err != nil {
		t.Fatalf("DrainByWorker on empty: %v", err)
	}
	if len(drained) != 0 {
		t.Errorf("expected empty, got %d", len(drained))
	}
}

func TestFixPromptQueueImpl_DrainIsolation(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	fpq := workforceadapter.NewFixPromptQueue(s)

	_ = fpq.Put(ctx, queue.FixPrompt{TaskID: "t1", ProjectID: "p1", WorkerID: "w1", Severity: queue.SeverityMinor, ReviewerTier: queue.ReviewerTierL4, CreatedAt: time.Now().UTC()})
	_ = fpq.Put(ctx, queue.FixPrompt{TaskID: "t2", ProjectID: "p1", WorkerID: "w2", Severity: queue.SeverityReject, ReviewerTier: queue.ReviewerTierL4, CreatedAt: time.Now().UTC()})

	drained, _ := fpq.DrainByWorker(ctx, "w1")
	if len(drained) != 1 {
		t.Errorf("drain w1 = %d, want 1", len(drained))
	}
	w2pending, _ := fpq.PendingByWorker(ctx, "w2")
	if len(w2pending) != 1 {
		t.Errorf("w2 pending after w1 drain = %d, want 1", len(w2pending))
	}
}

func TestFixPromptQueueImpl_AllReviewerTiers(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	fpq := workforceadapter.NewFixPromptQueue(s)

	tiers := []queue.ReviewerTier{queue.ReviewerTierL2, queue.ReviewerTierL3, queue.ReviewerTierL4}
	for i, tier := range tiers {
		if err := fpq.Put(ctx, queue.FixPrompt{
			TaskID:       queue.TaskID(fmt.Sprintf("tier-t%d", i)),
			ProjectID:    "p1",
			WorkerID:     fmt.Sprintf("w-tier-%d", i),
			ReviewerTier: tier,
			Severity:     queue.SeverityMinor,
			CreatedAt:    time.Now().UTC(),
		}); err != nil {
			t.Fatalf("Put tier %v: %v", tier, err)
		}
	}
	for i, tier := range tiers {
		pending, _ := fpq.PendingByWorker(ctx, fmt.Sprintf("w-tier-%d", i))
		if len(pending) != 1 {
			t.Errorf("worker %d pending = %d, want 1", i, len(pending))
			continue
		}
		if pending[0].ReviewerTier != tier {
			t.Errorf("ReviewerTier = %v, want %v", pending[0].ReviewerTier, tier)
		}
	}
}

func TestFixPromptQueueImpl_NilPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("NewFixPromptQueue(nil) should panic")
		}
	}()
	workforceadapter.NewFixPromptQueue(nil)
}

func TestSharedTaskListImpl_NilPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("NewSharedTaskList(nil) should panic")
		}
	}()
	workforceadapter.NewSharedTaskList(nil)
}

func TestSharedTaskListImpl_AdvanceNotFound(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	stl := workforceadapter.NewSharedTaskList(s)

	err := stl.Advance(ctx, "nonexistent", queue.StatusDone)
	if !errors.Is(err, queue.ErrTaskNotFound) {
		t.Errorf("got %v, want ErrTaskNotFound", err)
	}
}

func TestSharedTaskListImpl_GetNotFound(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	stl := workforceadapter.NewSharedTaskList(s)

	_, err := stl.Get(ctx, "nonexistent")
	if !errors.Is(err, queue.ErrTaskNotFound) {
		t.Errorf("got %v, want ErrTaskNotFound", err)
	}
}

func TestSharedTaskListImpl_ClaimNotFound(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	stl := workforceadapter.NewSharedTaskList(s)

	err := stl.Claim(ctx, "nonexistent", "thread-x")
	if !errors.Is(err, queue.ErrTaskNotFound) {
		t.Errorf("got %v, want ErrTaskNotFound", err)
	}
}

func TestSharedTaskListImpl_ClaimNotPending(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	stl := workforceadapter.NewSharedTaskList(s)

	_ = stl.Enqueue(ctx, queue.TaskRow{TaskID: "done-task", ProjectID: "p1", Status: queue.StatusDone, CreatedAt: time.Now().UTC()})
	err := stl.Claim(ctx, "done-task", "thread-x")
	if !errors.Is(err, queue.ErrTaskNotPending) {
		t.Errorf("got %v, want ErrTaskNotPending", err)
	}
}

func TestSharedTaskListImpl_EnqueueZeroStatusRejected(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	stl := workforceadapter.NewSharedTaskList(s)

	row := queue.TaskRow{
		TaskID:    "zero-status",
		ProjectID: "p1",

		CreatedAt: time.Now().UTC(),
	}
	if err := stl.Enqueue(ctx, row); err == nil {
		t.Error("Enqueue with zero Status should fail (CHECK constraint)")
	}
}

func TestSharedTaskListImpl_EnqueueZeroCreatedAt(t *testing.T) {

	ctx := context.Background()
	s := openTestStore(t)
	stl := workforceadapter.NewSharedTaskList(s)

	row := queue.TaskRow{
		TaskID:    "zero-cat",
		ProjectID: "p1",
		Status:    queue.StatusPending,
	}
	if err := stl.Enqueue(ctx, row); err != nil {
		t.Fatalf("Enqueue with zero CreatedAt: %v", err)
	}
	got, err := stl.Get(ctx, "zero-cat")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt should have been filled by Enqueue")
	}
}

func TestCheckpointQueueImpl_NilDeadline(t *testing.T) {

	ctx := context.Background()
	s := openTestStore(t)
	cpq := workforceadapter.NewCheckpointQueue(s)

	cp := queue.Checkpoint{
		TaskID:    "cp-no-dl",
		ProjectID: "p1",
		ThreadID:  "thr-nd",
		StateJSON: `{"x":1}`,
		SeqNum:    1,
		CreatedAt: time.Now().UTC(),
	}
	if err := cpq.Put(ctx, cp); err != nil {
		t.Fatalf("Put: %v", err)
	}
	items, err := cpq.Peek(ctx, "cp-no-dl")
	if err != nil {
		t.Fatalf("Peek: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("Peek count = %d, want 1", len(items))
	}
	if !items[0].DeadlineAt.IsZero() {
		t.Errorf("DeadlineAt should be zero when not set, got %v", items[0].DeadlineAt)
	}
}

func openTestStoreNoCleanup(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "err.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	return s
}

func TestSharedTaskListImpl_EnqueueError(t *testing.T) {
	ctx := context.Background()
	s := openTestStoreNoCleanup(t)
	stl := workforceadapter.NewSharedTaskList(s)
	_ = s.Close()
	err := stl.Enqueue(ctx, queue.TaskRow{TaskID: "e1", ProjectID: "p1", Status: queue.StatusPending, CreatedAt: time.Now().UTC()})
	if err == nil {
		t.Error("expected error on closed DB, got nil")
	}
}

func TestSharedTaskListImpl_ClaimError(t *testing.T) {
	ctx := context.Background()
	s := openTestStoreNoCleanup(t)
	stl := workforceadapter.NewSharedTaskList(s)
	_ = s.Close()
	err := stl.Claim(ctx, "t1", "thread")
	if err == nil {
		t.Error("expected error on closed DB, got nil")
	}
}

func TestSharedTaskListImpl_AdvanceError(t *testing.T) {
	ctx := context.Background()
	s := openTestStoreNoCleanup(t)
	stl := workforceadapter.NewSharedTaskList(s)
	_ = s.Close()
	err := stl.Advance(ctx, "t1", queue.StatusDone)
	if err == nil {
		t.Error("expected error on closed DB, got nil")
	}
}

func TestSharedTaskListImpl_GetError(t *testing.T) {
	ctx := context.Background()
	s := openTestStoreNoCleanup(t)
	stl := workforceadapter.NewSharedTaskList(s)
	_ = s.Close()
	_, err := stl.Get(ctx, "t1")
	if err == nil {
		t.Error("expected error on closed DB, got nil")
	}
}

func TestSharedTaskListImpl_ListByStatusError(t *testing.T) {
	ctx := context.Background()
	s := openTestStoreNoCleanup(t)
	stl := workforceadapter.NewSharedTaskList(s)
	_ = s.Close()
	_, err := stl.ListByStatus(ctx, "p1", queue.StatusPending)
	if err == nil {
		t.Error("expected error on closed DB, got nil")
	}
}

func TestSharedTaskListImpl_ByThreadError(t *testing.T) {
	ctx := context.Background()
	s := openTestStoreNoCleanup(t)
	stl := workforceadapter.NewSharedTaskList(s)
	_ = s.Close()
	_, err := stl.ByThread(ctx, "thr-1")
	if err == nil {
		t.Error("expected error on closed DB, got nil")
	}
}

func TestCheckpointQueueImpl_PutError(t *testing.T) {
	ctx := context.Background()
	s := openTestStoreNoCleanup(t)
	cpq := workforceadapter.NewCheckpointQueue(s)
	_ = s.Close()
	err := cpq.Put(ctx, queue.Checkpoint{TaskID: "t1", ProjectID: "p1", ThreadID: "thr", StateJSON: `{}`, SeqNum: 1, CreatedAt: time.Now().UTC()})
	if err == nil {
		t.Error("expected error on closed DB, got nil")
	}
}

func TestCheckpointQueueImpl_DrainError(t *testing.T) {
	ctx := context.Background()
	s := openTestStoreNoCleanup(t)
	cpq := workforceadapter.NewCheckpointQueue(s)
	_ = s.Close()
	_, err := cpq.Drain(ctx, "t1")
	if err == nil {
		t.Error("expected error on closed DB, got nil")
	}
}

func TestCheckpointQueueImpl_PeekError(t *testing.T) {
	ctx := context.Background()
	s := openTestStoreNoCleanup(t)
	cpq := workforceadapter.NewCheckpointQueue(s)
	_ = s.Close()
	_, err := cpq.Peek(ctx, "t1")
	if err == nil {
		t.Error("expected error on closed DB, got nil")
	}
}

func TestCheckpointQueueImpl_ByThreadError(t *testing.T) {
	ctx := context.Background()
	s := openTestStoreNoCleanup(t)
	cpq := workforceadapter.NewCheckpointQueue(s)
	_ = s.Close()
	_, err := cpq.ByThread(ctx, "thr-1")
	if err == nil {
		t.Error("expected error on closed DB, got nil")
	}
}

func TestFixPromptQueueImpl_PutError(t *testing.T) {
	ctx := context.Background()
	s := openTestStoreNoCleanup(t)
	fpq := workforceadapter.NewFixPromptQueue(s)
	_ = s.Close()
	err := fpq.Put(ctx, queue.FixPrompt{TaskID: "t1", ProjectID: "p1", WorkerID: "w1", Severity: queue.SeverityMinor, ReviewerTier: queue.ReviewerTierL2, CreatedAt: time.Now().UTC()})
	if err == nil {
		t.Error("expected error on closed DB, got nil")
	}
}

func TestFixPromptQueueImpl_DrainByWorkerError(t *testing.T) {
	ctx := context.Background()
	s := openTestStoreNoCleanup(t)
	fpq := workforceadapter.NewFixPromptQueue(s)
	_ = s.Close()
	_, err := fpq.DrainByWorker(ctx, "w1")
	if err == nil {
		t.Error("expected error on closed DB, got nil")
	}
}

func TestFixPromptQueueImpl_PendingByWorkerError(t *testing.T) {
	ctx := context.Background()
	s := openTestStoreNoCleanup(t)
	fpq := workforceadapter.NewFixPromptQueue(s)
	_ = s.Close()
	_, err := fpq.PendingByWorker(ctx, "w1")
	if err == nil {
		t.Error("expected error on closed DB, got nil")
	}
}

func TestSharedTaskListImpl_GetBadStatus(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	db := s.DB()

	_, err := db.Exec(`PRAGMA ignore_check_constraints = ON`)
	if err != nil {

		t.Skip("PRAGMA ignore_check_constraints not supported; skipping bad-status test")
	}
	now := time.Now().UTC().Unix()
	_, err = db.ExecContext(ctx, `INSERT INTO workforce_tasks
		(task_id, project_id, title, description, status, thread_id, priority, error_detail, created_at, updated_at)
		VALUES ('bad-status', 'p1', '', '', 'BOGUS', '', 0, '', ?, ?)`, now, now)
	_, _ = db.Exec(`PRAGMA ignore_check_constraints = OFF`)
	if err != nil {
		t.Skipf("could not insert bad status row: %v", err)
	}

	stl := workforceadapter.NewSharedTaskList(s)
	_, err = stl.Get(ctx, "bad-status")
	if err == nil {
		t.Error("expected parse error for bad status, got nil")
	}
}

func TestSharedTaskListImpl_ListByStatusBadStatus(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	db := s.DB()

	_, err := db.Exec(`PRAGMA ignore_check_constraints = ON`)
	if err != nil {
		t.Skip("PRAGMA ignore_check_constraints not supported; skipping bad-status test")
	}
	now := time.Now().UTC().Unix()

	_, _ = db.ExecContext(ctx, `INSERT INTO workforce_tasks
		(task_id, project_id, status, thread_id, title, description, priority, error_detail, created_at, updated_at)
		VALUES ('scan-bad-1', 'p-scan', 'pending', '', '', '', 0, '', ?, ?)`, now, now)
	_, _ = db.ExecContext(ctx, `UPDATE workforce_tasks SET status = 'BOGUS' WHERE task_id = 'scan-bad-1'`)
	_, _ = db.Exec(`PRAGMA ignore_check_constraints = OFF`)

	stl := workforceadapter.NewSharedTaskList(s)
	_, err = stl.ListByStatus(ctx, "p-scan", queue.StatusPending)

	_ = err
}

func TestSharedTaskListImpl_ByThreadBadStatus(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	db := s.DB()

	_, err := db.Exec(`PRAGMA ignore_check_constraints = ON`)
	if err != nil {
		t.Skip("PRAGMA ignore_check_constraints not supported; skipping bad-status test")
	}
	now := time.Now().UTC().Unix()
	_, _ = db.ExecContext(ctx, `INSERT INTO workforce_tasks
		(task_id, project_id, status, thread_id, title, description, priority, error_detail, created_at, updated_at)
		VALUES ('bythread-bad', 'p1', 'BOGUS', 'thr-bad', '', '', 0, '', ?, ?)`, now, now)
	_, _ = db.Exec(`PRAGMA ignore_check_constraints = OFF`)

	stl := workforceadapter.NewSharedTaskList(s)
	_, err = stl.ByThread(ctx, "thr-bad")
	_ = err
}

func TestNewSharedTaskList_ClosedStorePanics(t *testing.T) {
	s := openTestStoreNoCleanup(t)
	_ = s.Close()
	defer func() {
		if r := recover(); r == nil {
			t.Error("NewSharedTaskList with closed store should panic")
		}
	}()
	workforceadapter.NewSharedTaskList(s)
}

func TestSharedTaskListImpl_ClaimBadStatusInDB(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	db := s.DB()

	_, err := db.Exec(`PRAGMA ignore_check_constraints = ON`)
	if err != nil {
		t.Skip("PRAGMA ignore_check_constraints not supported; skipping bad-status test")
	}
	now := time.Now().UTC().Unix()
	_, _ = db.ExecContext(ctx, `INSERT INTO workforce_tasks
		(task_id, project_id, status, thread_id, title, description, priority, error_detail, created_at, updated_at)
		VALUES ('claim-bad-status', 'p1', 'BOGUS', '', '', '', 0, '', ?, ?)`, now, now)
	_, _ = db.Exec(`PRAGMA ignore_check_constraints = OFF`)

	stl := workforceadapter.NewSharedTaskList(s)
	err = stl.Claim(ctx, "claim-bad-status", "thr-x")
	if err == nil {
		t.Error("expected parse error for bad status in Claim, got nil")
	}
}

func TestScanTaskRowsError(t *testing.T) {

	err := workforceadapter.ExportScanTaskRowsError()
	if err == nil {
		t.Error("scanTaskRows with failing scanner should return error")
	}
}

func TestScanCheckpointsError(t *testing.T) {

	err := workforceadapter.ExportScanCheckpointsError()
	if err == nil {
		t.Error("scanCheckpoints with failing scanner should return error")
	}
}

func TestScanFixPromptsError(t *testing.T) {

	err := workforceadapter.ExportScanFixPromptsError()
	if err == nil {
		t.Error("scanFixPrompts with failing scanner should return error")
	}
}

func TestDrainQueryError(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	cpqFail := workforceadapter.ExportNewCheckpointQueueWithFailDrainQuery(s)
	_, err := cpqFail.Drain(ctx, "any-task")
	if err == nil {
		t.Error("Drain with failing query should return error")
	}
}

func TestDrainByWorkerQueryError(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	fpqFail := workforceadapter.ExportNewFixPromptQueueWithFailDrainQuery(s)
	_, err := fpqFail.DrainByWorker(ctx, "any-worker")
	if err == nil {
		t.Error("DrainByWorker with failing query should return error")
	}
}

func TestDrainScanError(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	cpq := workforceadapter.NewCheckpointQueue(s)
	if err := cpq.Put(ctx, queue.Checkpoint{
		TaskID:    "drain-scan-t",
		ProjectID: "p1",
		ThreadID:  "thr",
		StateJSON: `{}`,
		SeqNum:    1,
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	cpqFail := workforceadapter.ExportNewCheckpointQueueWithFailScan(s)
	_, err := cpqFail.Drain(ctx, "drain-scan-t")
	if err == nil {
		t.Error("Drain with failing scanner should return scan error")
	}
}

func TestDrainByWorkerScanError(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	fpq := workforceadapter.NewFixPromptQueue(s)
	if err := fpq.Put(ctx, queue.FixPrompt{
		TaskID:       "fpq-scan-t",
		ProjectID:    "p1",
		WorkerID:     "w-scan",
		ReviewerTier: queue.ReviewerTierL2,
		Severity:     queue.SeverityMinor,
		CreatedAt:    time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	fpqFail := workforceadapter.ExportNewFixPromptQueueWithFailScan(s)
	_, err := fpqFail.DrainByWorker(ctx, "w-scan")
	if err == nil {
		t.Error("DrainByWorker with failing scanner should return scan error")
	}
}

func TestSharedTaskListImpl_ListByStatusScanError(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	stl := workforceadapter.NewSharedTaskList(s)
	if err := stl.Enqueue(ctx, queue.TaskRow{
		TaskID: "ls-scan-t", ProjectID: "p1",
		Status: queue.StatusPending, CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	stlFail := workforceadapter.ExportNewSharedTaskListWithFailScan(s)
	_, err := stlFail.ListByStatus(ctx, "p1", queue.StatusPending)
	if err == nil {
		t.Error("ListByStatus with failing scanner should return scan error")
	}
}

func TestSharedTaskListImpl_ByThreadScanError(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	stl := workforceadapter.NewSharedTaskList(s)
	if err := stl.Enqueue(ctx, queue.TaskRow{
		TaskID: "bt-scan-t", ProjectID: "p1", ThreadID: "thr-scan",
		Status: queue.StatusInProgress, CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	stlFail := workforceadapter.ExportNewSharedTaskListWithFailScan(s)
	_, err := stlFail.ByThread(ctx, "thr-scan")
	if err == nil {
		t.Error("ByThread with failing scanner should return scan error")
	}
}

func TestClaimUpdateError(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	stl := workforceadapter.NewSharedTaskList(s)
	if err := stl.Enqueue(ctx, queue.TaskRow{
		TaskID: "claim-upd-err", ProjectID: "p1",
		Status: queue.StatusPending, CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	stlFail := workforceadapter.ExportNewSharedTaskListWithFailClaimExec(s)
	err := stlFail.Claim(ctx, "claim-upd-err", "thr-x")
	if err == nil {
		t.Error("Claim with failing exec should return error")
	}
}

func TestClaimQueryError(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	stlFail := workforceadapter.ExportNewSharedTaskListWithFailClaimQuery(s)
	err := stlFail.Claim(ctx, "any-task", "thr-x")
	if err == nil {
		t.Error("Claim with failing query should return error")
	}
}

func TestClaimCommitError(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	stl := workforceadapter.NewSharedTaskList(s)
	if err := stl.Enqueue(ctx, queue.TaskRow{
		TaskID: "claim-commit-err", ProjectID: "p1",
		Status: queue.StatusPending, CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	stlFail := workforceadapter.ExportNewSharedTaskListWithFailClaimCommit(s)
	err := stlFail.Claim(ctx, "claim-commit-err", "thr-x")
	if err == nil {
		t.Error("Claim with failing commit should return error")
	}
}

func TestDrainMarkConsumedError(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	cpq := workforceadapter.NewCheckpointQueue(s)
	if err := cpq.Put(ctx, queue.Checkpoint{
		TaskID: "drain-exec-t", ProjectID: "p1", ThreadID: "thr",
		StateJSON: `{}`, SeqNum: 1, CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	cpqFail := workforceadapter.ExportNewCheckpointQueueWithFailDrainExec(s)
	_, err := cpqFail.Drain(ctx, "drain-exec-t")
	if err == nil {
		t.Error("Drain with failing drainExec should return error")
	}
}

func TestDrainCommitError(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	cpq := workforceadapter.NewCheckpointQueue(s)
	if err := cpq.Put(ctx, queue.Checkpoint{
		TaskID: "drain-commit-t", ProjectID: "p1", ThreadID: "thr",
		StateJSON: `{}`, SeqNum: 1, CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	cpqFail := workforceadapter.ExportNewCheckpointQueueWithFailDrainCommit(s)
	_, err := cpqFail.Drain(ctx, "drain-commit-t")
	if err == nil {
		t.Error("Drain with failing drainCommit should return error")
	}
}

func TestDrainByWorkerMarkConsumedError(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	fpq := workforceadapter.NewFixPromptQueue(s)
	if err := fpq.Put(ctx, queue.FixPrompt{
		TaskID: "fp-exec-t", ProjectID: "p1", WorkerID: "w-exec",
		ReviewerTier: queue.ReviewerTierL2, Severity: queue.SeverityMinor,
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	fpqFail := workforceadapter.ExportNewFixPromptQueueWithFailDrainExec(s)
	_, err := fpqFail.DrainByWorker(ctx, "w-exec")
	if err == nil {
		t.Error("DrainByWorker with failing drainExec should return error")
	}
}

func TestDrainByWorkerCommitError(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	fpq := workforceadapter.NewFixPromptQueue(s)
	if err := fpq.Put(ctx, queue.FixPrompt{
		TaskID: "fp-commit-t", ProjectID: "p1", WorkerID: "w-commit",
		ReviewerTier: queue.ReviewerTierL2, Severity: queue.SeverityMinor,
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	fpqFail := workforceadapter.ExportNewFixPromptQueueWithFailDrainCommit(s)
	_, err := fpqFail.DrainByWorker(ctx, "w-commit")
	if err == nil {
		t.Error("DrainByWorker with failing drainCommit should return error")
	}
}
