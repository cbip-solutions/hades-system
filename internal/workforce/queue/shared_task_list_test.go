package queue_test

import (
	"context"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/workforce/queue"
)

func TestStatusString(t *testing.T) {
	cases := []struct {
		s    queue.Status
		want string
	}{
		{queue.StatusPending, "pending"},
		{queue.StatusInProgress, "in_progress"},
		{queue.StatusReview, "review"},
		{queue.StatusDone, "done"},
		{queue.StatusFailed, "failed"},
	}
	for _, c := range cases {
		if got := c.s.String(); got != c.want {
			t.Errorf("Status(%d).String() = %q, want %q", int(c.s), got, c.want)
		}
	}
}

func TestParseStatus(t *testing.T) {
	cases := []struct {
		in  string
		ok  bool
		out queue.Status
	}{
		{"pending", true, queue.StatusPending},
		{"in_progress", true, queue.StatusInProgress},
		{"review", true, queue.StatusReview},
		{"done", true, queue.StatusDone},
		{"failed", true, queue.StatusFailed},
		{"unknown", false, 0},
		{"", false, 0},
	}
	for _, c := range cases {
		got, err := queue.ParseStatus(c.in)
		if c.ok {
			if err != nil {
				t.Errorf("ParseStatus(%q) err=%v, want ok", c.in, err)
			}
			if got != c.out {
				t.Errorf("ParseStatus(%q) = %v, want %v", c.in, got, c.out)
			}
		} else {
			if err == nil {
				t.Errorf("ParseStatus(%q) err=nil, want error", c.in)
			}
		}
	}
}

type fakeSharedTaskList struct {
	rows map[queue.TaskID]*queue.TaskRow
}

var _ queue.SharedTaskList = (*fakeSharedTaskList)(nil)

func newFake() *fakeSharedTaskList {
	return &fakeSharedTaskList{rows: make(map[queue.TaskID]*queue.TaskRow)}
}

func (f *fakeSharedTaskList) Enqueue(_ context.Context, row queue.TaskRow) error {
	cp := row
	f.rows[row.TaskID] = &cp
	return nil
}

func (f *fakeSharedTaskList) Claim(_ context.Context, taskID queue.TaskID, threadID string) error {
	r, ok := f.rows[taskID]
	if !ok {
		return queue.ErrTaskNotFound
	}
	if r.Status != queue.StatusPending {
		return queue.ErrTaskNotPending
	}
	r.Status = queue.StatusInProgress
	r.ThreadID = threadID
	return nil
}

func (f *fakeSharedTaskList) Advance(_ context.Context, taskID queue.TaskID, newStatus queue.Status) error {
	r, ok := f.rows[taskID]
	if !ok {
		return queue.ErrTaskNotFound
	}
	if !queue.IsValidTransition(r.Status, newStatus) {
		return queue.ErrInvalidTransition
	}
	r.Status = newStatus
	return nil
}

func (f *fakeSharedTaskList) Get(_ context.Context, taskID queue.TaskID) (queue.TaskRow, error) {
	r, ok := f.rows[taskID]
	if !ok {
		return queue.TaskRow{}, queue.ErrTaskNotFound
	}
	return *r, nil
}

func (f *fakeSharedTaskList) ListByStatus(_ context.Context, projectID string, status queue.Status) ([]queue.TaskRow, error) {
	var out []queue.TaskRow
	for _, r := range f.rows {
		if r.ProjectID == projectID && r.Status == status {
			cp := *r
			out = append(out, cp)
		}
	}
	return out, nil
}

func (f *fakeSharedTaskList) ByThread(_ context.Context, threadID string) ([]queue.TaskRow, error) {
	var out []queue.TaskRow
	for _, r := range f.rows {
		if r.ThreadID == threadID {
			cp := *r
			out = append(out, cp)
		}
	}
	return out, nil
}

func TestFakeSharedTaskList_EnqueueAndGet(t *testing.T) {
	ctx := context.Background()
	f := newFake()

	row := queue.TaskRow{
		TaskID:    queue.TaskID("task-1"),
		ProjectID: "proj-a",
		Title:     "implement X",
		Status:    queue.StatusPending,
		CreatedAt: time.Now().UTC(),
	}
	if err := f.Enqueue(ctx, row); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	got, err := f.Get(ctx, row.TaskID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Title != row.Title {
		t.Errorf("got title %q, want %q", got.Title, row.Title)
	}
}

func TestFakeSharedTaskList_Claim(t *testing.T) {
	ctx := context.Background()
	f := newFake()

	row := queue.TaskRow{TaskID: "t1", ProjectID: "p1", Status: queue.StatusPending, CreatedAt: time.Now().UTC()}
	_ = f.Enqueue(ctx, row)

	if err := f.Claim(ctx, "t1", "thread-abc"); err != nil {
		t.Fatalf("Claim: %v", err)
	}
	got, _ := f.Get(ctx, "t1")
	if got.Status != queue.StatusInProgress {
		t.Errorf("after Claim status = %v, want in_progress", got.Status)
	}
	if got.ThreadID != "thread-abc" {
		t.Errorf("after Claim threadID = %q, want thread-abc", got.ThreadID)
	}
}

func TestFakeSharedTaskList_ClaimNotPending(t *testing.T) {
	ctx := context.Background()
	f := newFake()
	row := queue.TaskRow{TaskID: "t2", ProjectID: "p1", Status: queue.StatusDone, CreatedAt: time.Now().UTC()}
	_ = f.Enqueue(ctx, row)
	err := f.Claim(ctx, "t2", "thread-x")
	if err != queue.ErrTaskNotPending {
		t.Errorf("got %v, want ErrTaskNotPending", err)
	}
}

func TestFakeSharedTaskList_Advance(t *testing.T) {
	ctx := context.Background()
	f := newFake()
	row := queue.TaskRow{TaskID: "t3", ProjectID: "p1", Status: queue.StatusInProgress, CreatedAt: time.Now().UTC()}
	_ = f.Enqueue(ctx, row)
	if err := f.Advance(ctx, "t3", queue.StatusReview); err != nil {
		t.Fatalf("Advance: %v", err)
	}
	got, _ := f.Get(ctx, "t3")
	if got.Status != queue.StatusReview {
		t.Errorf("after Advance status = %v, want review", got.Status)
	}
}

func TestFakeSharedTaskList_ListByStatus(t *testing.T) {
	ctx := context.Background()
	f := newFake()
	for i, s := range []queue.Status{queue.StatusPending, queue.StatusPending, queue.StatusDone} {
		_ = f.Enqueue(ctx, queue.TaskRow{
			TaskID:    queue.TaskID(string(rune('A' + i))),
			ProjectID: "p1",
			Status:    s,
			CreatedAt: time.Now().UTC(),
		})
	}
	pending, _ := f.ListByStatus(ctx, "p1", queue.StatusPending)
	if len(pending) != 2 {
		t.Errorf("pending count = %d, want 2", len(pending))
	}
}

func TestFakeSharedTaskList_ByThread(t *testing.T) {
	ctx := context.Background()
	f := newFake()
	_ = f.Enqueue(ctx, queue.TaskRow{TaskID: "x1", ProjectID: "p1", ThreadID: "th-1", Status: queue.StatusInProgress, CreatedAt: time.Now().UTC()})
	_ = f.Enqueue(ctx, queue.TaskRow{TaskID: "x2", ProjectID: "p1", ThreadID: "th-2", Status: queue.StatusInProgress, CreatedAt: time.Now().UTC()})
	rows, _ := f.ByThread(ctx, "th-1")
	if len(rows) != 1 || rows[0].TaskID != "x1" {
		t.Errorf("ByThread(th-1) = %v, want [{x1}]", rows)
	}
}

func TestFakeSharedTaskList_GetNotFound(t *testing.T) {
	ctx := context.Background()
	f := newFake()
	_, err := f.Get(ctx, "nonexistent")
	if err != queue.ErrTaskNotFound {
		t.Errorf("got %v, want ErrTaskNotFound", err)
	}
}

func TestStatusStringUnknown(t *testing.T) {

	var s queue.Status = 99
	got := s.String()
	if got == "" || got == "pending" {
		t.Errorf("unknown Status.String() = %q, want non-empty non-canonical string", got)
	}
}

func TestDurableQueueOpened(t *testing.T) {

	if !queue.DurableQueueOpened() {
		t.Error("DurableQueueOpened() should return true")
	}
}

func TestIsValidTransition_AllowedEdges(t *testing.T) {
	allowed := []struct{ from, to queue.Status }{
		{queue.StatusPending, queue.StatusInProgress},
		{queue.StatusPending, queue.StatusFailed},
		{queue.StatusInProgress, queue.StatusReview},
		{queue.StatusInProgress, queue.StatusFailed},
		{queue.StatusReview, queue.StatusDone},
		{queue.StatusReview, queue.StatusFailed},
		{queue.StatusReview, queue.StatusInProgress},
	}
	for _, c := range allowed {
		if !queue.IsValidTransition(c.from, c.to) {
			t.Errorf("expected %v→%v allowed", c.from, c.to)
		}
	}
}

func TestIsValidTransition_DisallowedEdges(t *testing.T) {
	disallowed := []struct{ from, to queue.Status }{
		{queue.StatusPending, queue.StatusReview},
		{queue.StatusPending, queue.StatusDone},
		{queue.StatusPending, queue.StatusPending},
		{queue.StatusInProgress, queue.StatusPending},
		{queue.StatusInProgress, queue.StatusInProgress},
		{queue.StatusInProgress, queue.StatusDone},
		{queue.StatusReview, queue.StatusReview},
		{queue.StatusReview, queue.StatusPending},
		{queue.StatusDone, queue.StatusPending},
		{queue.StatusDone, queue.StatusInProgress},
		{queue.StatusDone, queue.StatusReview},
		{queue.StatusDone, queue.StatusDone},
		{queue.StatusDone, queue.StatusFailed},
		{queue.StatusFailed, queue.StatusPending},
		{queue.StatusFailed, queue.StatusInProgress},
		{queue.StatusFailed, queue.StatusReview},
		{queue.StatusFailed, queue.StatusDone},
		{queue.StatusFailed, queue.StatusFailed},
	}
	for _, c := range disallowed {
		if queue.IsValidTransition(c.from, c.to) {
			t.Errorf("expected %v→%v disallowed", c.from, c.to)
		}
	}
}

func TestIsValidTransition_UnknownStatus(t *testing.T) {

	if queue.IsValidTransition(queue.Status(99), queue.StatusDone) {
		t.Error("unknown current status should be rejected")
	}
	if queue.IsValidTransition(queue.Status(0), queue.StatusPending) {
		t.Error("zero current status should be rejected")
	}
}
