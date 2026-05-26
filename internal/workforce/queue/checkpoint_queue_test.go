package queue_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/workforce/queue"
)

type fakeCheckpointQueue struct {
	items []queue.Checkpoint
}

var _ queue.CheckpointQueue = (*fakeCheckpointQueue)(nil)

func (f *fakeCheckpointQueue) Put(_ context.Context, cp queue.Checkpoint) error {
	f.items = append(f.items, cp)
	return nil
}

func (f *fakeCheckpointQueue) Drain(_ context.Context, taskID queue.TaskID) ([]queue.Checkpoint, error) {
	var out []queue.Checkpoint
	var remaining []queue.Checkpoint
	for _, cp := range f.items {
		if cp.TaskID == taskID && !cp.Consumed {
			cp.Consumed = true
			out = append(out, cp)
		} else {
			remaining = append(remaining, cp)
		}
	}
	f.items = remaining
	return out, nil
}

func (f *fakeCheckpointQueue) Peek(_ context.Context, taskID queue.TaskID) ([]queue.Checkpoint, error) {
	var out []queue.Checkpoint
	for _, cp := range f.items {
		if cp.TaskID == taskID && !cp.Consumed {
			out = append(out, cp)
		}
	}
	return out, nil
}

func (f *fakeCheckpointQueue) ByThread(_ context.Context, threadID string) ([]queue.Checkpoint, error) {
	var out []queue.Checkpoint
	for _, cp := range f.items {
		if cp.ThreadID == threadID {
			out = append(out, cp)
		}
	}
	return out, nil
}

func TestFakeCheckpointQueue_PutAndPeek(t *testing.T) {
	ctx := context.Background()
	f := &fakeCheckpointQueue{}

	cp := queue.Checkpoint{
		TaskID:    "t-cp-1",
		ProjectID: "p1",
		ThreadID:  "thr-1",
		StateJSON: `{"step":1}`,
		SeqNum:    1,
		CreatedAt: time.Now().UTC(),
	}
	if err := f.Put(ctx, cp); err != nil {
		t.Fatalf("Put: %v", err)
	}
	items, err := f.Peek(ctx, "t-cp-1")
	if err != nil {
		t.Fatalf("Peek: %v", err)
	}
	if len(items) != 1 {
		t.Errorf("Peek count = %d, want 1", len(items))
	}
	if items[0].StateJSON != `{"step":1}` {
		t.Errorf("StateJSON = %q", items[0].StateJSON)
	}
}

func TestFakeCheckpointQueue_DrainConsumed(t *testing.T) {
	ctx := context.Background()
	f := &fakeCheckpointQueue{}
	for i := 1; i <= 3; i++ {
		_ = f.Put(ctx, queue.Checkpoint{
			TaskID:    "t-drain",
			ProjectID: "p1",
			ThreadID:  "thr-d",
			StateJSON: `{}`,
			SeqNum:    i,
			CreatedAt: time.Now().UTC(),
		})
	}
	drained, _ := f.Drain(ctx, "t-drain")
	if len(drained) != 3 {
		t.Errorf("drain count = %d, want 3", len(drained))
	}

	remaining, _ := f.Peek(ctx, "t-drain")
	if len(remaining) != 0 {
		t.Errorf("remaining after drain = %d, want 0", len(remaining))
	}
}

func TestFakeCheckpointQueue_Deadline(t *testing.T) {

	cp := queue.Checkpoint{
		TaskID:     "t-dl",
		ProjectID:  "p1",
		ThreadID:   "thr-dl",
		StateJSON:  `{}`,
		SeqNum:     1,
		DeadlineAt: time.Now().UTC().Add(30 * time.Minute),
		CreatedAt:  time.Now().UTC(),
	}
	if cp.DeadlineAt.IsZero() {
		t.Error("DeadlineAt should not be zero when set")
	}
}

func TestFakeCheckpointQueue_ByThread(t *testing.T) {
	ctx := context.Background()
	f := &fakeCheckpointQueue{}
	for i, thr := range []string{"thr-A", "thr-B", "thr-A"} {
		_ = f.Put(ctx, queue.Checkpoint{
			TaskID:    queue.TaskID(fmt.Sprintf("tc-%d", i)),
			ProjectID: "p1",
			ThreadID:  thr,
			StateJSON: `{}`,
			SeqNum:    i,
			CreatedAt: time.Now().UTC(),
		})
	}
	rows, _ := f.ByThread(ctx, "thr-A")
	if len(rows) != 2 {
		t.Errorf("ByThread(thr-A) count = %d, want 2", len(rows))
	}
}
