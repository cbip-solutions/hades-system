package queue_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/workforce/queue"
)

type fakeFixPromptQueue struct {
	items []queue.FixPrompt
}

var _ queue.FixPromptQueue = (*fakeFixPromptQueue)(nil)

func (f *fakeFixPromptQueue) Put(_ context.Context, fp queue.FixPrompt) error {
	f.items = append(f.items, fp)
	return nil
}

func (f *fakeFixPromptQueue) DrainByWorker(_ context.Context, workerID string) ([]queue.FixPrompt, error) {
	var out []queue.FixPrompt
	var remaining []queue.FixPrompt
	for _, fp := range f.items {
		if fp.WorkerID == workerID && !fp.Consumed {
			fp.Consumed = true
			out = append(out, fp)
		} else {
			remaining = append(remaining, fp)
		}
	}
	f.items = remaining
	return out, nil
}

func (f *fakeFixPromptQueue) PendingByWorker(_ context.Context, workerID string) ([]queue.FixPrompt, error) {
	var out []queue.FixPrompt
	for _, fp := range f.items {
		if fp.WorkerID == workerID && !fp.Consumed {
			out = append(out, fp)
		}
	}
	return out, nil
}

func TestFakeFixPromptQueue_PutAndPending(t *testing.T) {
	ctx := context.Background()
	f := &fakeFixPromptQueue{}

	fp := queue.FixPrompt{
		TaskID:       "task-fp-1",
		ProjectID:    "proj-fp",
		WorkerID:     "worker-alpha",
		ReviewerTier: queue.ReviewerTierL2,
		PromptText:   "missing test for edge case X",
		CriteriaName: "default",
		Severity:     queue.SeverityMinor,
		CreatedAt:    time.Now().UTC(),
	}
	if err := f.Put(ctx, fp); err != nil {
		t.Fatalf("Put: %v", err)
	}
	pending, err := f.PendingByWorker(ctx, "worker-alpha")
	if err != nil {
		t.Fatalf("PendingByWorker: %v", err)
	}
	if len(pending) != 1 {
		t.Errorf("pending count = %d, want 1", len(pending))
	}
	if pending[0].PromptText != fp.PromptText {
		t.Errorf("prompt = %q, want %q", pending[0].PromptText, fp.PromptText)
	}
}

func TestFakeFixPromptQueue_DrainByWorker(t *testing.T) {
	ctx := context.Background()
	f := &fakeFixPromptQueue{}

	for i := 0; i < 3; i++ {
		_ = f.Put(ctx, queue.FixPrompt{
			TaskID:       queue.TaskID(fmt.Sprintf("t%d", i)),
			ProjectID:    "p1",
			WorkerID:     "w-drain",
			ReviewerTier: queue.ReviewerTierL3,
			Severity:     queue.SeverityMajor,
			CreatedAt:    time.Now().UTC(),
		})
	}
	drained, err := f.DrainByWorker(ctx, "w-drain")
	if err != nil {
		t.Fatalf("DrainByWorker: %v", err)
	}
	if len(drained) != 3 {
		t.Errorf("drained count = %d, want 3", len(drained))
	}
	after, _ := f.PendingByWorker(ctx, "w-drain")
	if len(after) != 0 {
		t.Errorf("pending after drain = %d, want 0", len(after))
	}
}

func TestFakeFixPromptQueue_DrainIsolation(t *testing.T) {

	ctx := context.Background()
	f := &fakeFixPromptQueue{}
	_ = f.Put(ctx, queue.FixPrompt{TaskID: "t1", ProjectID: "p1", WorkerID: "w1", Severity: queue.SeverityMinor, CreatedAt: time.Now().UTC()})
	_ = f.Put(ctx, queue.FixPrompt{TaskID: "t2", ProjectID: "p1", WorkerID: "w2", Severity: queue.SeverityMinor, CreatedAt: time.Now().UTC()})

	drained, _ := f.DrainByWorker(ctx, "w1")
	if len(drained) != 1 {
		t.Errorf("drain w1 = %d, want 1", len(drained))
	}
	w2pending, _ := f.PendingByWorker(ctx, "w2")
	if len(w2pending) != 1 {
		t.Errorf("w2 pending after w1 drain = %d, want 1", len(w2pending))
	}
}

func TestSeverityString(t *testing.T) {
	cases := []struct {
		s    queue.Severity
		want string
	}{
		{queue.SeverityMinor, "minor"},
		{queue.SeverityMajor, "major"},
		{queue.SeverityReject, "reject"},
	}
	for _, c := range cases {
		if got := c.s.String(); got != c.want {
			t.Errorf("Severity(%d).String() = %q, want %q", int(c.s), got, c.want)
		}
	}
}

func TestReviewerTierString(t *testing.T) {
	cases := []struct {
		rt   queue.ReviewerTier
		want string
	}{
		{queue.ReviewerTierL2, "l2"},
		{queue.ReviewerTierL3, "l3"},
		{queue.ReviewerTierL4, "l4"},
	}
	for _, c := range cases {
		if got := c.rt.String(); got != c.want {
			t.Errorf("ReviewerTier(%d).String() = %q, want %q", int(c.rt), got, c.want)
		}
	}
}

func TestReviewerTierStringUnknown(t *testing.T) {
	var rt queue.ReviewerTier = 99
	got := rt.String()
	if got == "" || got == "l2" {
		t.Errorf("unknown ReviewerTier.String() = %q, want non-empty non-canonical string", got)
	}
}

func TestSeverityStringUnknown(t *testing.T) {
	var s queue.Severity = 99
	got := s.String()
	if got == "" || got == "minor" {
		t.Errorf("unknown Severity.String() = %q, want non-empty non-canonical string", got)
	}
}

func TestParseReviewerTier(t *testing.T) {
	cases := []struct {
		in   string
		ok   bool
		want queue.ReviewerTier
	}{
		{"l2", true, queue.ReviewerTierL2},
		{"l3", true, queue.ReviewerTierL3},
		{"l4", true, queue.ReviewerTierL4},
		{"l99", false, 0},
		{"", false, 0},
	}
	for _, c := range cases {
		got, err := queue.ParseReviewerTier(c.in)
		if c.ok {
			if err != nil {
				t.Errorf("ParseReviewerTier(%q) err=%v, want ok", c.in, err)
			}
			if got != c.want {
				t.Errorf("ParseReviewerTier(%q) = %v, want %v", c.in, got, c.want)
			}
		} else {
			if err == nil {
				t.Errorf("ParseReviewerTier(%q) err=nil, want error", c.in)
			}
		}
	}
}

func TestParseSeverity(t *testing.T) {
	cases := []struct {
		in   string
		ok   bool
		want queue.Severity
	}{
		{"minor", true, queue.SeverityMinor},
		{"major", true, queue.SeverityMajor},
		{"reject", true, queue.SeverityReject},
		{"BOGUS", false, 0},
		{"", false, 0},
	}
	for _, c := range cases {
		got, err := queue.ParseSeverity(c.in)
		if c.ok {
			if err != nil {
				t.Errorf("ParseSeverity(%q) err=%v, want ok", c.in, err)
			}
			if got != c.want {
				t.Errorf("ParseSeverity(%q) = %v, want %v", c.in, got, c.want)
			}
		} else {
			if err == nil {
				t.Errorf("ParseSeverity(%q) err=nil, want error", c.in)
			}
		}
	}
}
