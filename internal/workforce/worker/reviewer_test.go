package worker_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/workforce/queue"
	"github.com/cbip-solutions/hades-system/internal/workforce/subprocess"
	"github.com/cbip-solutions/hades-system/internal/workforce/worker"
)

func makeReviewerSpec(t *testing.T, variant worker.Variant, id string) worker.WorkerSpec {
	t.Helper()
	spec, err := worker.NewSpec(worker.SpecOptions{
		ID:             id,
		Variant:        variant,
		TaskTier:       worker.TierComplex,
		ModelClass:     "tier-reviewer",
		Tools:          []string{"audit_review"},
		Quota:          worker.Quota{MaxTokens: 200000, MaxCostUSD: 5.0, MaxDuration: 30 * time.Minute},
		RecoveryPolicy: worker.RecoveryDoctrineBound,
		DoctrineName:   "max-scope",
		ProjectID:      "internal-platform-x",
	})
	if err != nil {
		t.Fatalf("NewSpec: %v", err)
	}
	return spec
}

func reviewerVerdictPayload(verdict string) []byte {
	return []byte(`{"stop_reason":"end_turn","input_tokens":50,"output_tokens":30,"text":"{\"verdict\":\"` + verdict + `\",\"concerns\":[\"missing edge-case test\"]}"}`)
}

func TestReviewerL2EphemeralLifecycle(t *testing.T) {
	session := newFakeSession("tid-rev-l2",
		subprocess.Message{
			Kind: subprocess.MessageKindResult, ID: "r-1", Method: "done",
			Payload: reviewerVerdictPayload("clean"),
		},
	)
	mgr := newFakePersistentManager(session)
	spec := makeReviewerSpec(t, worker.VariantReviewerL2, "reviewer-l2-1")
	rev, err := worker.NewReviewer(worker.ReviewerOptions{
		Spec:              spec,
		WorktreePath:      "/tmp/wt-rev-l2",
		SubprocessManager: mgr,
		SharedTaskList:    newFakeSharedTaskList(),
		CheckpointQueue:   newFakeCheckpointQueue(),
		FixPromptQueue:    newFakeFixPromptQueue(),
		DoctrineConfig:    fakeDoctrineConfig("", 30*time.Second),
		ToolRelay:         worker.NewUnavailableRelay(),
	})
	if err != nil {
		t.Fatalf("NewReviewer: %v", err)
	}
	if rev == nil {
		t.Fatal("nil Reviewer")
	}
	defer rev.Close()

	if mgr.spawnEphemeralCalls != 1 {
		t.Errorf("L2 must use SpawnEphemeral; got spawnEphemeralCalls=%d acquireCalls=%d",
			mgr.spawnEphemeralCalls, mgr.acquireCalls)
	}
	if mgr.acquireCalls != 0 {
		t.Errorf("L2 must NOT AcquirePersistent; got %d", mgr.acquireCalls)
	}
}

func TestReviewerL3PersistentLifecycle(t *testing.T) {
	session := newFakeSession("tid-rev-l3",
		subprocess.Message{
			Kind: subprocess.MessageKindResult, ID: "r-1", Method: "done",
			Payload: reviewerVerdictPayload("clean"),
		},
	)
	mgr := newFakePersistentManager(session)
	spec := makeReviewerSpec(t, worker.VariantReviewerL3, "reviewer-l3-1")
	rev, err := worker.NewReviewer(worker.ReviewerOptions{
		Spec:              spec,
		WorktreePath:      "/tmp/wt-rev-l3",
		SubprocessManager: mgr,
		SharedTaskList:    newFakeSharedTaskList(),
		CheckpointQueue:   newFakeCheckpointQueue(),
		FixPromptQueue:    newFakeFixPromptQueue(),
		DoctrineConfig:    fakeDoctrineConfig("", 30*time.Second),
		ToolRelay:         worker.NewUnavailableRelay(),
	})
	if err != nil {
		t.Fatalf("NewReviewer: %v", err)
	}
	defer rev.Close()
	if mgr.acquireCalls != 1 {
		t.Errorf("L3 must use AcquirePersistent; got %d", mgr.acquireCalls)
	}
	if mgr.spawnEphemeralCalls != 0 {
		t.Errorf("L3 must NOT SpawnEphemeral; got %d", mgr.spawnEphemeralCalls)
	}
}

func TestReviewerL4PersistentLifecycle(t *testing.T) {
	session := newFakeSession("tid-rev-l4",
		subprocess.Message{
			Kind: subprocess.MessageKindResult, ID: "r-1", Method: "done",
			Payload: reviewerVerdictPayload("clean"),
		},
	)
	mgr := newFakePersistentManager(session)
	spec := makeReviewerSpec(t, worker.VariantReviewerL4, "reviewer-l4-1")
	rev, err := worker.NewReviewer(worker.ReviewerOptions{
		Spec:              spec,
		WorktreePath:      "/tmp/wt-rev-l4",
		SubprocessManager: mgr,
		SharedTaskList:    newFakeSharedTaskList(),
		CheckpointQueue:   newFakeCheckpointQueue(),
		FixPromptQueue:    newFakeFixPromptQueue(),
		DoctrineConfig:    fakeDoctrineConfig("", 30*time.Second),
		ToolRelay:         worker.NewUnavailableRelay(),
	})
	if err != nil {
		t.Fatalf("NewReviewer: %v", err)
	}
	defer rev.Close()
	if mgr.acquireCalls != 1 {
		t.Errorf("L4 must use AcquirePersistent; got %d", mgr.acquireCalls)
	}
}

func TestReviewerRejectsNonReviewerSpec(t *testing.T) {
	session := newFakeSession("tid-rev-bad")
	mgr := newFakePersistentManager(session)
	spec := makeReviewerSpec(t, worker.VariantWorker, "x")
	_, err := worker.NewReviewer(worker.ReviewerOptions{
		Spec:              spec,
		WorktreePath:      "/tmp/wt-rev-bad",
		SubprocessManager: mgr,
		SharedTaskList:    newFakeSharedTaskList(),
		CheckpointQueue:   newFakeCheckpointQueue(),
		FixPromptQueue:    newFakeFixPromptQueue(),
		DoctrineConfig:    fakeDoctrineConfig("", 30*time.Second),
		ToolRelay:         worker.NewUnavailableRelay(),
	})
	if err == nil {
		t.Fatal("expected error for non-reviewer Spec.Variant")
	}
}

func TestReviewerRequiresSubprocessManager(t *testing.T) {
	spec := makeReviewerSpec(t, worker.VariantReviewerL2, "r")
	_, err := worker.NewReviewer(worker.ReviewerOptions{
		Spec:              spec,
		WorktreePath:      "/tmp/wt-rev-x",
		SubprocessManager: nil,
		SharedTaskList:    newFakeSharedTaskList(),
		CheckpointQueue:   newFakeCheckpointQueue(),
		FixPromptQueue:    newFakeFixPromptQueue(),
		DoctrineConfig:    fakeDoctrineConfig("", 30*time.Second),
		ToolRelay:         worker.NewUnavailableRelay(),
	})
	if err == nil {
		t.Fatal("expected error for nil SubprocessManager")
	}
}

func TestReviewerPanicsOnNilWorktree(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on empty worktreePath")
		}
	}()
	session := newFakeSession("tid-rev-nw")
	mgr := newFakePersistentManager(session)
	spec := makeReviewerSpec(t, worker.VariantReviewerL2, "r")
	_, _ = worker.NewReviewer(worker.ReviewerOptions{
		Spec: spec, WorktreePath: "",
		SubprocessManager: mgr, SharedTaskList: newFakeSharedTaskList(),
		CheckpointQueue: newFakeCheckpointQueue(), FixPromptQueue: newFakeFixPromptQueue(),
		DoctrineConfig: fakeDoctrineConfig("", 30*time.Second),
		ToolRelay:      worker.NewUnavailableRelay(),
	})
	t.Fatal("unreachable")
}

func TestReviewerPanicsOnNilQueues(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on nil queues")
		}
	}()
	session := newFakeSession("tid-rev-nq")
	mgr := newFakePersistentManager(session)
	spec := makeReviewerSpec(t, worker.VariantReviewerL2, "r")
	_, _ = worker.NewReviewer(worker.ReviewerOptions{
		Spec: spec, WorktreePath: "/tmp/wt",
		SubprocessManager: mgr, SharedTaskList: nil,
		CheckpointQueue: newFakeCheckpointQueue(), FixPromptQueue: newFakeFixPromptQueue(),
		DoctrineConfig: fakeDoctrineConfig("", 30*time.Second),
		ToolRelay:      worker.NewUnavailableRelay(),
	})
	t.Fatal("unreachable")
}

func TestReviewerPanicsOnNilDoctrineConfig(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on nil DoctrineConfig")
		}
	}()
	session := newFakeSession("tid-rev-nc")
	mgr := newFakePersistentManager(session)
	spec := makeReviewerSpec(t, worker.VariantReviewerL2, "r")
	_, _ = worker.NewReviewer(worker.ReviewerOptions{
		Spec: spec, WorktreePath: "/tmp/wt",
		SubprocessManager: mgr, SharedTaskList: newFakeSharedTaskList(),
		CheckpointQueue: newFakeCheckpointQueue(), FixPromptQueue: newFakeFixPromptQueue(),
		DoctrineConfig: nil, ToolRelay: worker.NewUnavailableRelay(),
	})
	t.Fatal("unreachable")
}

func TestReviewerAcquireError(t *testing.T) {
	session := newFakeSession("tid-rev-ae")
	mgr := newFakePersistentManager(session)
	mgr.acquireErr = errors.New("subprocess down")
	spec := makeReviewerSpec(t, worker.VariantReviewerL3, "r")
	_, err := worker.NewReviewer(worker.ReviewerOptions{
		Spec: spec, WorktreePath: "/tmp/wt",
		SubprocessManager: mgr, SharedTaskList: newFakeSharedTaskList(),
		CheckpointQueue: newFakeCheckpointQueue(), FixPromptQueue: newFakeFixPromptQueue(),
		DoctrineConfig: fakeDoctrineConfig("", 30*time.Second),
		ToolRelay:      worker.NewUnavailableRelay(),
	})
	if err == nil {
		t.Fatal("expected AcquirePersistent error to propagate")
	}
}

func TestReviewerSpawnEphemeralError(t *testing.T) {
	session := newFakeSession("tid-rev-see")
	mgr := newFakePersistentManager(session)
	mgr.acquireErr = errors.New("spawn failed")
	spec := makeReviewerSpec(t, worker.VariantReviewerL2, "r")
	_, err := worker.NewReviewer(worker.ReviewerOptions{
		Spec: spec, WorktreePath: "/tmp/wt",
		SubprocessManager: mgr, SharedTaskList: newFakeSharedTaskList(),
		CheckpointQueue: newFakeCheckpointQueue(), FixPromptQueue: newFakeFixPromptQueue(),
		DoctrineConfig: fakeDoctrineConfig("", 30*time.Second),
		ToolRelay:      worker.NewUnavailableRelay(),
	})
	if err == nil {
		t.Fatal("expected SpawnEphemeral error to propagate")
	}
}

func TestReviewerL2ReviewCheckpointEmitsFixPrompt(t *testing.T) {
	session := newFakeSession("tid-rev-l2-rcp",
		subprocess.Message{
			Kind: subprocess.MessageKindResult, ID: "r-1", Method: "done",
			Payload: reviewerVerdictPayload("minor"),
		},
	)
	mgr := newFakePersistentManager(session)
	stl := newFakeSharedTaskList()
	cpq := newFakeCheckpointQueue()
	fpq := newFakeFixPromptQueue()
	spec := makeReviewerSpec(t, worker.VariantReviewerL2, "reviewer-l2-rcp")
	rev, err := worker.NewReviewer(worker.ReviewerOptions{
		Spec: spec, WorktreePath: "/tmp/wt-rev-l2-rcp",
		SubprocessManager: mgr, SharedTaskList: stl,
		CheckpointQueue: cpq, FixPromptQueue: fpq,
		DoctrineConfig: fakeDoctrineConfig("", 30*time.Second),
		ToolRelay:      worker.NewUnavailableRelay(),
	})
	if err != nil {
		t.Fatalf("NewReviewer: %v", err)
	}
	defer rev.Close()

	cpRow := queue.Checkpoint{
		TaskID:     "task-cp-r1",
		ProjectID:  spec.ProjectID,
		ThreadID:   "wkr-thread-1",
		StateJSON:  `{"diff":"+ test"}`,
		SeqNum:     1,
		DeadlineAt: time.Now().UTC().Add(30 * time.Second),
	}
	if err := cpq.Put(context.Background(), cpRow); err != nil {
		t.Fatalf("cpq.Put: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := rev.ReviewCheckpoint(ctx, "task-cp-r1", "wkr-spec-1"); err != nil {
		t.Fatalf("ReviewCheckpoint: %v", err)
	}

	pending, _ := fpq.PendingByWorker(context.Background(), "wkr-spec-1")
	if len(pending) != 1 {
		t.Fatalf("FixPromptQueue rows for wkr-spec-1 = %d, want 1", len(pending))
	}
	got := pending[0]
	if got.ReviewerTier != queue.ReviewerTierL2 {
		t.Errorf("ReviewerTier = %v, want L2", got.ReviewerTier)
	}
	if got.TaskID != queue.TaskID("task-cp-r1") {
		t.Errorf("TaskID = %q", got.TaskID)
	}
}

func TestReviewerL2ReviewCheckpointCleanVerdictNoFixPrompt(t *testing.T) {
	session := newFakeSession("tid-rev-l2-clean",
		subprocess.Message{
			Kind: subprocess.MessageKindResult, ID: "r-1", Method: "done",
			Payload: []byte(`{"stop_reason":"end_turn","input_tokens":1,"output_tokens":1,"text":"{\"verdict\":\"clean\"}"}`),
		},
	)
	mgr := newFakePersistentManager(session)
	stl := newFakeSharedTaskList()
	cpq := newFakeCheckpointQueue()
	fpq := newFakeFixPromptQueue()
	spec := makeReviewerSpec(t, worker.VariantReviewerL2, "reviewer-l2-clean")
	rev, _ := worker.NewReviewer(worker.ReviewerOptions{
		Spec: spec, WorktreePath: "/tmp/wt-rev-l2-cl",
		SubprocessManager: mgr, SharedTaskList: stl,
		CheckpointQueue: cpq, FixPromptQueue: fpq,
		DoctrineConfig: fakeDoctrineConfig("", 30*time.Second),
		ToolRelay:      worker.NewUnavailableRelay(),
	})
	defer rev.Close()
	_ = cpq.Put(context.Background(), queue.Checkpoint{
		TaskID: "task-clean", ProjectID: spec.ProjectID, ThreadID: "t",
		StateJSON: `{}`, SeqNum: 1, DeadlineAt: time.Now().UTC().Add(30 * time.Second),
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := rev.ReviewCheckpoint(ctx, "task-clean", "wkr-clean"); err != nil {
		t.Fatalf("ReviewCheckpoint: %v", err)
	}
	pending, _ := fpq.PendingByWorker(context.Background(), "wkr-clean")
	if len(pending) != 0 {
		t.Errorf("clean verdict emitted %d FixPrompts; want 0", len(pending))
	}
}

func TestReviewerL2ReviewCheckpointEmptyTaskID(t *testing.T) {
	rev := buildEphemeralReviewer(t)
	defer rev.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rev.ReviewCheckpoint(ctx, "", "wkr"); err == nil {
		t.Fatal("expected error for empty taskID")
	}
}

func TestReviewerL2ReviewCheckpointEmptyWorkerID(t *testing.T) {
	rev := buildEphemeralReviewer(t)
	defer rev.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rev.ReviewCheckpoint(ctx, "task-x", ""); err == nil {
		t.Fatal("expected error for empty workerID")
	}
}

func TestReviewerL2ReviewAggregationRejected(t *testing.T) {
	rev := buildEphemeralReviewer(t)
	defer rev.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := rev.ReviewAggregation(ctx, worker.AggregationInput{WindowID: "x"})
	if err == nil {
		t.Fatal("expected ReviewAggregation to reject L2 (L3+ only)")
	}
}

func TestReviewerL2ProposeAmendmentRejected(t *testing.T) {
	rev := buildEphemeralReviewer(t)
	defer rev.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := rev.ProposeAmendment(ctx, worker.AggregationInput{WindowID: "x"})
	if err == nil {
		t.Fatal("expected ProposeAmendment to reject non-L4")
	}
}

func TestReviewerL3ReviewAggregationEmitsStrategicFixPrompts(t *testing.T) {
	session := newFakeSession("tid-rev-l3-agg",
		subprocess.Message{
			Kind: subprocess.MessageKindResult, ID: "r-1", Method: "done",
			Payload: []byte(`{"stop_reason":"end_turn","input_tokens":500,"output_tokens":80,"text":"{\"verdict\":\"strategic\",\"concerns\":[\"tech debt in module X\"]}"}`),
		},
	)
	mgr := newFakePersistentManager(session)
	stl := newFakeSharedTaskList()
	cpq := newFakeCheckpointQueue()
	fpq := newFakeFixPromptQueue()
	spec := makeReviewerSpec(t, worker.VariantReviewerL3, "reviewer-l3-agg")
	rev, _ := worker.NewReviewer(worker.ReviewerOptions{
		Spec: spec, WorktreePath: "/tmp/wt-rev-l3-agg",
		SubprocessManager: mgr, SharedTaskList: stl,
		CheckpointQueue: cpq, FixPromptQueue: fpq,
		DoctrineConfig: fakeDoctrineConfig("", 30*time.Second),
		ToolRelay:      worker.NewUnavailableRelay(),
	})
	defer rev.Close()

	agg := worker.AggregationInput{
		WindowID: "window-1", FromLevel: "L2", ToLevel: "L3",
		StartTime: time.Now().UTC().Add(-5 * time.Minute),
		EndTime:   time.Now().UTC(),
		Summary:   "5 checkpoints reviewed; 3 minor concerns escalated",
		AnchorTaskIDs: []queue.TaskID{
			"task-1", "task-2", "task-3",
		},
		AnchorWorkerIDs: []string{"wkr-1", "wkr-2", "wkr-3"},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := rev.ReviewAggregation(ctx, agg); err != nil {
		t.Fatalf("ReviewAggregation: %v", err)
	}
	for i, taskID := range agg.AnchorTaskIDs {
		workerID := agg.AnchorWorkerIDs[i]
		rows, _ := fpq.PendingByWorker(context.Background(), workerID)
		if len(rows) == 0 {
			t.Errorf("no FixPrompt emitted for anchor task %s / worker %s", taskID, workerID)
			continue
		}
		if rows[0].ReviewerTier != queue.ReviewerTierL3 {
			t.Errorf("anchor %s: ReviewerTier = %v, want L3", taskID, rows[0].ReviewerTier)
		}
		if rows[0].TaskID != taskID {
			t.Errorf("anchor %d: TaskID = %q, want %q", i, rows[0].TaskID, taskID)
		}
	}
}

func TestReviewerL3LLMErrorPropagates(t *testing.T) {
	session := newFakeSession("tid-rev-l3-err",
		subprocess.Message{Kind: subprocess.MessageKindError, ID: "r-1", ErrCode: 500, ErrMsg: "l3 crashed"},
	)
	mgr := newFakePersistentManager(session)
	spec := makeReviewerSpec(t, worker.VariantReviewerL3, "reviewer-l3-err")
	rev, _ := worker.NewReviewer(worker.ReviewerOptions{
		Spec: spec, WorktreePath: "/tmp/wt-rev-l3-err",
		SubprocessManager: mgr, SharedTaskList: newFakeSharedTaskList(),
		CheckpointQueue: newFakeCheckpointQueue(), FixPromptQueue: newFakeFixPromptQueue(),
		DoctrineConfig: fakeDoctrineConfig("", 30*time.Second),
		ToolRelay:      worker.NewUnavailableRelay(),
	})
	defer rev.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := rev.ReviewAggregation(ctx, worker.AggregationInput{WindowID: "w-err"})
	if err == nil {
		t.Fatal("expected reviewer LLM error to propagate")
	}
}

func TestReviewerL4LLMErrorPropagates(t *testing.T) {
	session := newFakeSession("tid-rev-l4-err",
		subprocess.Message{Kind: subprocess.MessageKindError, ID: "r-1", ErrCode: 500, ErrMsg: "l4 crashed"},
	)
	mgr := newFakePersistentManager(session)
	spec := makeReviewerSpec(t, worker.VariantReviewerL4, "reviewer-l4-err")
	rev, _ := worker.NewReviewer(worker.ReviewerOptions{
		Spec: spec, WorktreePath: "/tmp/wt-rev-l4-err",
		SubprocessManager: mgr, SharedTaskList: newFakeSharedTaskList(),
		CheckpointQueue: newFakeCheckpointQueue(), FixPromptQueue: newFakeFixPromptQueue(),
		DoctrineConfig: fakeDoctrineConfig("", 30*time.Second),
		ToolRelay:      worker.NewUnavailableRelay(),
	})
	defer rev.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := rev.ProposeAmendment(ctx, worker.AggregationInput{WindowID: "w-err"})
	if err == nil {
		t.Fatal("expected reviewer LLM error to propagate")
	}
}

func TestReviewerL2EmptyConcerns(t *testing.T) {
	payload := []byte(`{"stop_reason":"end_turn","input_tokens":1,"output_tokens":1,"text":"{\"verdict\":\"minor\",\"concerns\":[]}"}`)
	session := newFakeSession("tid-rev-l2-ec",
		subprocess.Message{Kind: subprocess.MessageKindResult, ID: "r-1", Method: "done", Payload: payload},
	)
	mgr := newFakePersistentManager(session)
	cpq := newFakeCheckpointQueue()
	fpq := newFakeFixPromptQueue()
	spec := makeReviewerSpec(t, worker.VariantReviewerL2, "reviewer-l2-ec")
	rev, _ := worker.NewReviewer(worker.ReviewerOptions{
		Spec: spec, WorktreePath: "/tmp/wt-rev-l2-ec",
		SubprocessManager: mgr, SharedTaskList: newFakeSharedTaskList(),
		CheckpointQueue: cpq, FixPromptQueue: fpq,
		DoctrineConfig: fakeDoctrineConfig("", 30*time.Second),
		ToolRelay:      worker.NewUnavailableRelay(),
	})
	defer rev.Close()
	_ = cpq.Put(context.Background(), queue.Checkpoint{
		TaskID: "task-ec", ProjectID: spec.ProjectID, ThreadID: "t",
		StateJSON: `{}`, SeqNum: 1, DeadlineAt: time.Now().UTC().Add(30 * time.Second),
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rev.ReviewCheckpoint(ctx, "task-ec", "wkr-ec"); err != nil {
		t.Fatalf("ReviewCheckpoint: %v", err)
	}
	pending, _ := fpq.PendingByWorker(context.Background(), "wkr-ec")
	if len(pending) != 1 {
		t.Fatalf("expected 1 FixPrompt; got %d", len(pending))
	}
	if !strings.Contains(pending[0].PromptText, "no concerns") {
		t.Errorf("PromptText = %q, want substring 'no concerns'", pending[0].PromptText)
	}
}

func TestReviewerL3EmptyConcerns(t *testing.T) {
	payload := []byte(`{"stop_reason":"end_turn","input_tokens":1,"output_tokens":1,"text":"{\"verdict\":\"strategic\",\"concerns\":[]}"}`)
	session := newFakeSession("tid-rev-l3-ec",
		subprocess.Message{Kind: subprocess.MessageKindResult, ID: "r-1", Method: "done", Payload: payload},
	)
	mgr := newFakePersistentManager(session)
	fpq := newFakeFixPromptQueue()
	spec := makeReviewerSpec(t, worker.VariantReviewerL3, "reviewer-l3-ec")
	rev, _ := worker.NewReviewer(worker.ReviewerOptions{
		Spec: spec, WorktreePath: "/tmp/wt-rev-l3-ec",
		SubprocessManager: mgr, SharedTaskList: newFakeSharedTaskList(),
		CheckpointQueue: newFakeCheckpointQueue(), FixPromptQueue: fpq,
		DoctrineConfig: fakeDoctrineConfig("", 30*time.Second),
		ToolRelay:      worker.NewUnavailableRelay(),
	})
	defer rev.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := rev.ReviewAggregation(ctx, worker.AggregationInput{
		WindowID: "w-ec", AnchorTaskIDs: []queue.TaskID{"a-1"}, AnchorWorkerIDs: []string{"wkr-ec"},
	})
	if err != nil {
		t.Fatalf("ReviewAggregation: %v", err)
	}
	pending, _ := fpq.PendingByWorker(context.Background(), "wkr-ec")
	if len(pending) != 1 {
		t.Fatalf("expected 1 FixPrompt; got %d", len(pending))
	}
	if !strings.Contains(pending[0].PromptText, "no concerns") {
		t.Errorf("PromptText = %q, want substring 'no concerns'", pending[0].PromptText)
	}
}

func TestReviewerL3RejectsEmptyWindowID(t *testing.T) {
	rev := buildPersistentReviewer(t, worker.VariantReviewerL3, "reviewer-l3-ew")
	defer rev.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := rev.ReviewAggregation(ctx, worker.AggregationInput{WindowID: ""})
	if err == nil {
		t.Fatal("expected error for empty WindowID")
	}
}

func TestReviewerL3CheckpointReviewRejected(t *testing.T) {
	rev := buildPersistentReviewer(t, worker.VariantReviewerL3, "reviewer-l3-cp")
	defer rev.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := rev.ReviewCheckpoint(ctx, "task-x", "wkr")
	if err == nil {
		t.Fatal("expected ReviewCheckpoint to reject L3 (L2 only)")
	}
}

func TestReviewerL4ProposeAmendmentEmitsProposal(t *testing.T) {
	session := newFakeSession("tid-rev-l4-prop",
		subprocess.Message{
			Kind: subprocess.MessageKindResult, ID: "r-1", Method: "done",
			Payload: []byte(`{"stop_reason":"end_turn","input_tokens":1000,"output_tokens":200,"text":"{\"amendments\":[{\"area\":\"workforce.subprocess\",\"change\":\"increase TTL to 16h\",\"rationale\":\"stable contexts\"}]}"}`),
		},
	)
	mgr := newFakePersistentManager(session)
	stl := newFakeSharedTaskList()
	cpq := newFakeCheckpointQueue()
	fpq := newFakeFixPromptQueue()
	emitter := worker.NewInMemoryAmendmentEmitter()
	spec := makeReviewerSpec(t, worker.VariantReviewerL4, "reviewer-l4-prop")
	rev, _ := worker.NewReviewer(worker.ReviewerOptions{
		Spec: spec, WorktreePath: "/tmp/wt-rev-l4-prop",
		SubprocessManager: mgr, SharedTaskList: stl,
		CheckpointQueue: cpq, FixPromptQueue: fpq,
		DoctrineConfig:           fakeDoctrineConfig("", 30*time.Second),
		ToolRelay:                worker.NewUnavailableRelay(),
		AmendmentProposalEmitter: emitter,
	})
	defer rev.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	agg := worker.AggregationInput{
		WindowID: "macro-1", FromLevel: "L3", ToLevel: "L4",
		Summary: "5 strategic concerns clustered around subprocess TTL",
	}
	if err := rev.ProposeAmendment(ctx, agg); err != nil {
		t.Fatalf("ProposeAmendment: %v", err)
	}
	props := emitter.Proposals()
	if len(props) == 0 {
		t.Fatal("no amendment proposal emitted")
	}
	if !strings.Contains(props[0].Area, "workforce.subprocess") {
		t.Errorf("Area = %q, want substring 'workforce.subprocess'", props[0].Area)
	}
	if !strings.Contains(props[0].Change, "16h") {
		t.Errorf("Change = %q, want substring '16h'", props[0].Change)
	}
}

func TestReviewerL4DefaultsInMemoryEmitter(t *testing.T) {

	session := newFakeSession("tid-rev-l4-def",
		subprocess.Message{
			Kind: subprocess.MessageKindResult, ID: "r-1", Method: "done",
			Payload: []byte(`{"stop_reason":"end_turn","input_tokens":1,"output_tokens":1,"text":"{\"amendments\":[{\"area\":\"x\",\"change\":\"y\"}]}"}`),
		},
	)
	mgr := newFakePersistentManager(session)
	spec := makeReviewerSpec(t, worker.VariantReviewerL4, "reviewer-l4-def")
	rev, err := worker.NewReviewer(worker.ReviewerOptions{
		Spec: spec, WorktreePath: "/tmp/wt-rev-l4-def",
		SubprocessManager: mgr, SharedTaskList: newFakeSharedTaskList(),
		CheckpointQueue: newFakeCheckpointQueue(), FixPromptQueue: newFakeFixPromptQueue(),
		DoctrineConfig: fakeDoctrineConfig("", 30*time.Second),
		ToolRelay:      worker.NewUnavailableRelay(),
	})
	if err != nil {
		t.Fatalf("NewReviewer: %v", err)
	}
	defer rev.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rev.ProposeAmendment(ctx, worker.AggregationInput{WindowID: "x"}); err != nil {
		t.Fatalf("ProposeAmendment: %v", err)
	}

}

func TestReviewerL3ProposeAmendmentRejected(t *testing.T) {
	rev := buildPersistentReviewer(t, worker.VariantReviewerL3, "r")
	defer rev.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := rev.ProposeAmendment(ctx, worker.AggregationInput{WindowID: "x"})
	if err == nil {
		t.Fatal("expected ProposeAmendment to reject L3 (L4 only)")
	}
}

func TestReviewerL2LLMErrorPropagates(t *testing.T) {
	session := newFakeSession("tid-rev-l2-err",
		subprocess.Message{Kind: subprocess.MessageKindError, ID: "r-1", ErrCode: 500, ErrMsg: "reviewer subprocess crashed"},
	)
	mgr := newFakePersistentManager(session)
	stl := newFakeSharedTaskList()
	cpq := newFakeCheckpointQueue()
	fpq := newFakeFixPromptQueue()
	spec := makeReviewerSpec(t, worker.VariantReviewerL2, "reviewer-l2-err")
	rev, _ := worker.NewReviewer(worker.ReviewerOptions{
		Spec: spec, WorktreePath: "/tmp/wt-rev-l2-err",
		SubprocessManager: mgr, SharedTaskList: stl,
		CheckpointQueue: cpq, FixPromptQueue: fpq,
		DoctrineConfig: fakeDoctrineConfig("", 30*time.Second),
		ToolRelay:      worker.NewUnavailableRelay(),
	})
	defer rev.Close()
	_ = cpq.Put(context.Background(), queue.Checkpoint{
		TaskID: "task-err", ProjectID: spec.ProjectID, ThreadID: "t",
		StateJSON: `{}`, SeqNum: 1, DeadlineAt: time.Now().UTC().Add(30 * time.Second),
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := rev.ReviewCheckpoint(ctx, "task-err", "wkr-err")
	if err == nil {
		t.Fatal("expected reviewer LLM error to propagate")
	}
	if !strings.Contains(err.Error(), "reviewer subprocess crashed") {
		t.Errorf("err = %v, want substring 'reviewer subprocess crashed'", err)
	}
}

func TestReviewerL2ReviewCheckpointMalformedVerdict(t *testing.T) {
	session := newFakeSession("tid-rev-l2-mv",
		subprocess.Message{
			Kind: subprocess.MessageKindResult, ID: "r-1", Method: "done",
			Payload: []byte(`{"stop_reason":"end_turn","input_tokens":1,"output_tokens":1,"text":"not json"}`),
		},
	)
	mgr := newFakePersistentManager(session)
	stl := newFakeSharedTaskList()
	cpq := newFakeCheckpointQueue()
	fpq := newFakeFixPromptQueue()
	spec := makeReviewerSpec(t, worker.VariantReviewerL2, "reviewer-l2-mv")
	rev, _ := worker.NewReviewer(worker.ReviewerOptions{
		Spec: spec, WorktreePath: "/tmp/wt-rev-l2-mv",
		SubprocessManager: mgr, SharedTaskList: stl,
		CheckpointQueue: cpq, FixPromptQueue: fpq,
		DoctrineConfig: fakeDoctrineConfig("", 30*time.Second),
		ToolRelay:      worker.NewUnavailableRelay(),
	})
	defer rev.Close()
	_ = cpq.Put(context.Background(), queue.Checkpoint{
		TaskID: "task-mv", ProjectID: spec.ProjectID, ThreadID: "t",
		StateJSON: `{}`, SeqNum: 1, DeadlineAt: time.Now().UTC().Add(30 * time.Second),
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rev.ReviewCheckpoint(ctx, "task-mv", "wkr-mv"); err == nil {
		t.Fatal("expected error on malformed verdict JSON")
	}
}

func buildEphemeralReviewer(t *testing.T) *worker.Reviewer {
	t.Helper()
	session := newFakeSession("tid-rev-h", subprocess.Message{
		Kind: subprocess.MessageKindResult, ID: "r-1", Method: "done",
		Payload: reviewerVerdictPayload("clean"),
	})
	mgr := newFakePersistentManager(session)
	spec := makeReviewerSpec(t, worker.VariantReviewerL2, "reviewer-l2-h")
	rev, err := worker.NewReviewer(worker.ReviewerOptions{
		Spec: spec, WorktreePath: "/tmp/wt-rev-l2-h",
		SubprocessManager: mgr, SharedTaskList: newFakeSharedTaskList(),
		CheckpointQueue: newFakeCheckpointQueue(), FixPromptQueue: newFakeFixPromptQueue(),
		DoctrineConfig: fakeDoctrineConfig("", 30*time.Second),
		ToolRelay:      worker.NewUnavailableRelay(),
	})
	if err != nil {
		t.Fatalf("buildEphemeralReviewer: %v", err)
	}
	return rev
}

func buildPersistentReviewer(t *testing.T, variant worker.Variant, id string) *worker.Reviewer {
	t.Helper()
	session := newFakeSession("tid-rev-h", subprocess.Message{
		Kind: subprocess.MessageKindResult, ID: "r-1", Method: "done",
		Payload: reviewerVerdictPayload("clean"),
	})
	mgr := newFakePersistentManager(session)
	spec := makeReviewerSpec(t, variant, id)
	rev, err := worker.NewReviewer(worker.ReviewerOptions{
		Spec: spec, WorktreePath: "/tmp/wt-rev-p-h",
		SubprocessManager: mgr, SharedTaskList: newFakeSharedTaskList(),
		CheckpointQueue: newFakeCheckpointQueue(), FixPromptQueue: newFakeFixPromptQueue(),
		DoctrineConfig: fakeDoctrineConfig("", 30*time.Second),
		ToolRelay:      worker.NewUnavailableRelay(),
	})
	if err != nil {
		t.Fatalf("buildPersistentReviewer: %v", err)
	}
	return rev
}

var _ = json.Valid

func TestMapVerdictToSeverityViaReviewerL2(t *testing.T) {
	cases := []struct {
		verdict       string
		wantSeverity  queue.Severity
		wantNoPrompts bool
	}{
		{"clean", 0, true},
		{"minor", queue.SeverityMinor, false},
		{"major", queue.SeverityMajor, false},
		{"reject", queue.SeverityReject, false},

		{"weird-verdict", queue.SeverityMinor, false},
	}
	for _, c := range cases {
		t.Run(c.verdict, func(t *testing.T) {
			payload := []byte(`{"stop_reason":"end_turn","input_tokens":1,"output_tokens":1,"text":"{\"verdict\":\"` + c.verdict + `\",\"concerns\":[\"x\"]}"}`)
			session := newFakeSession("tid-rev-mv-"+c.verdict,
				subprocess.Message{Kind: subprocess.MessageKindResult, ID: "r-1", Method: "done", Payload: payload},
			)
			mgr := newFakePersistentManager(session)
			cpq := newFakeCheckpointQueue()
			fpq := newFakeFixPromptQueue()
			spec := makeReviewerSpec(t, worker.VariantReviewerL2, "reviewer-l2-mv-"+c.verdict)
			rev, _ := worker.NewReviewer(worker.ReviewerOptions{
				Spec: spec, WorktreePath: "/tmp/wt-rev-mv-" + c.verdict,
				SubprocessManager: mgr, SharedTaskList: newFakeSharedTaskList(),
				CheckpointQueue: cpq, FixPromptQueue: fpq,
				DoctrineConfig: fakeDoctrineConfig("", 30*time.Second),
				ToolRelay:      worker.NewUnavailableRelay(),
			})
			defer rev.Close()
			_ = cpq.Put(context.Background(), queue.Checkpoint{
				TaskID: queue.TaskID("task-mv-" + c.verdict), ProjectID: spec.ProjectID, ThreadID: "t",
				StateJSON: `{}`, SeqNum: 1, DeadlineAt: time.Now().UTC().Add(30 * time.Second),
			})
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := rev.ReviewCheckpoint(ctx, queue.TaskID("task-mv-"+c.verdict), "wkr-mv"); err != nil {
				t.Fatalf("ReviewCheckpoint: %v", err)
			}
			pending, _ := fpq.PendingByWorker(context.Background(), "wkr-mv")
			if c.wantNoPrompts {
				if len(pending) != 0 {
					t.Errorf("verdict %q: emitted %d prompts; want 0", c.verdict, len(pending))
				}
				return
			}
			if len(pending) != 1 {
				t.Fatalf("verdict %q: emitted %d prompts; want 1", c.verdict, len(pending))
			}
			if pending[0].Severity != c.wantSeverity {
				t.Errorf("verdict %q: severity = %v, want %v", c.verdict, pending[0].Severity, c.wantSeverity)
			}
		})
	}
}

type fakeCpqBadPeek struct{ err error }

func (f *fakeCpqBadPeek) Put(_ context.Context, _ queue.Checkpoint) error { return nil }
func (f *fakeCpqBadPeek) Drain(_ context.Context, _ queue.TaskID) ([]queue.Checkpoint, error) {
	return nil, nil
}
func (f *fakeCpqBadPeek) Peek(_ context.Context, _ queue.TaskID) ([]queue.Checkpoint, error) {
	return nil, f.err
}
func (f *fakeCpqBadPeek) ByThread(_ context.Context, _ string) ([]queue.Checkpoint, error) {
	return nil, nil
}

func TestReviewerL2PeekError(t *testing.T) {
	session := newFakeSession("tid-rev-pe",
		subprocess.Message{Kind: subprocess.MessageKindResult, ID: "r-1", Method: "done", Payload: reviewerVerdictPayload("clean")},
	)
	mgr := newFakePersistentManager(session)
	spec := makeReviewerSpec(t, worker.VariantReviewerL2, "reviewer-l2-pe")
	rev, _ := worker.NewReviewer(worker.ReviewerOptions{
		Spec: spec, WorktreePath: "/tmp/wt-rev-pe",
		SubprocessManager: mgr, SharedTaskList: newFakeSharedTaskList(),
		CheckpointQueue: &fakeCpqBadPeek{err: errors.New("peek failed")},
		FixPromptQueue:  newFakeFixPromptQueue(),
		DoctrineConfig:  fakeDoctrineConfig("", 30*time.Second),
		ToolRelay:       worker.NewUnavailableRelay(),
	})
	defer rev.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := rev.ReviewCheckpoint(ctx, "task-pe", "wkr-pe")
	if err == nil {
		t.Fatal("expected Peek error to propagate")
	}
	if !strings.Contains(err.Error(), "peek failed") {
		t.Errorf("err = %v, want substring 'peek failed'", err)
	}
}

type fakeStlBadEnqueue struct {
	err  error
	real *fakeSharedTaskList
}

func (f *fakeStlBadEnqueue) Enqueue(_ context.Context, _ queue.TaskRow) error { return f.err }
func (f *fakeStlBadEnqueue) Claim(ctx context.Context, id queue.TaskID, t string) error {
	return f.real.Claim(ctx, id, t)
}
func (f *fakeStlBadEnqueue) Advance(ctx context.Context, id queue.TaskID, st queue.Status) error {
	return f.real.Advance(ctx, id, st)
}
func (f *fakeStlBadEnqueue) Get(ctx context.Context, id queue.TaskID) (queue.TaskRow, error) {
	return f.real.Get(ctx, id)
}
func (f *fakeStlBadEnqueue) ListByStatus(ctx context.Context, p string, st queue.Status) ([]queue.TaskRow, error) {
	return f.real.ListByStatus(ctx, p, st)
}
func (f *fakeStlBadEnqueue) ByThread(ctx context.Context, t string) ([]queue.TaskRow, error) {
	return f.real.ByThread(ctx, t)
}

func TestReviewerL2EnqueueReviewRowError(t *testing.T) {
	session := newFakeSession("tid-rev-eq",
		subprocess.Message{Kind: subprocess.MessageKindResult, ID: "r-1", Method: "done", Payload: reviewerVerdictPayload("clean")},
	)
	mgr := newFakePersistentManager(session)
	spec := makeReviewerSpec(t, worker.VariantReviewerL2, "reviewer-l2-eq")
	cpq := newFakeCheckpointQueue()
	rev, _ := worker.NewReviewer(worker.ReviewerOptions{
		Spec: spec, WorktreePath: "/tmp/wt-rev-eq",
		SubprocessManager: mgr,
		SharedTaskList:    &fakeStlBadEnqueue{err: errors.New("enq fail"), real: newFakeSharedTaskList()},
		CheckpointQueue:   cpq,
		FixPromptQueue:    newFakeFixPromptQueue(),
		DoctrineConfig:    fakeDoctrineConfig("", 30*time.Second),
		ToolRelay:         worker.NewUnavailableRelay(),
	})
	defer rev.Close()
	_ = cpq.Put(context.Background(), queue.Checkpoint{
		TaskID: "task-eq", ProjectID: spec.ProjectID, ThreadID: "t", StateJSON: `{}`, SeqNum: 1,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := rev.ReviewCheckpoint(ctx, "task-eq", "wkr-eq")
	if err == nil {
		t.Fatal("expected Enqueue error to propagate")
	}
}

func TestReviewerL3EnqueueReviewRowError(t *testing.T) {
	session := newFakeSession("tid-rev-l3-eq",
		subprocess.Message{Kind: subprocess.MessageKindResult, ID: "r-1", Method: "done", Payload: reviewerVerdictPayload("clean")},
	)
	mgr := newFakePersistentManager(session)
	spec := makeReviewerSpec(t, worker.VariantReviewerL3, "reviewer-l3-eq")
	rev, _ := worker.NewReviewer(worker.ReviewerOptions{
		Spec: spec, WorktreePath: "/tmp/wt-rev-l3-eq",
		SubprocessManager: mgr,
		SharedTaskList:    &fakeStlBadEnqueue{err: errors.New("enq fail"), real: newFakeSharedTaskList()},
		CheckpointQueue:   newFakeCheckpointQueue(),
		FixPromptQueue:    newFakeFixPromptQueue(),
		DoctrineConfig:    fakeDoctrineConfig("", 30*time.Second),
		ToolRelay:         worker.NewUnavailableRelay(),
	})
	defer rev.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := rev.ReviewAggregation(ctx, worker.AggregationInput{WindowID: "w"})
	if err == nil {
		t.Fatal("expected Enqueue error to propagate")
	}
}

func TestReviewerL4EnqueueReviewRowError(t *testing.T) {
	session := newFakeSession("tid-rev-l4-eq",
		subprocess.Message{Kind: subprocess.MessageKindResult, ID: "r-1", Method: "done", Payload: reviewerVerdictPayload("clean")},
	)
	mgr := newFakePersistentManager(session)
	spec := makeReviewerSpec(t, worker.VariantReviewerL4, "reviewer-l4-eq")
	rev, _ := worker.NewReviewer(worker.ReviewerOptions{
		Spec: spec, WorktreePath: "/tmp/wt-rev-l4-eq",
		SubprocessManager: mgr,
		SharedTaskList:    &fakeStlBadEnqueue{err: errors.New("enq fail"), real: newFakeSharedTaskList()},
		CheckpointQueue:   newFakeCheckpointQueue(),
		FixPromptQueue:    newFakeFixPromptQueue(),
		DoctrineConfig:    fakeDoctrineConfig("", 30*time.Second),
		ToolRelay:         worker.NewUnavailableRelay(),
	})
	defer rev.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := rev.ProposeAmendment(ctx, worker.AggregationInput{WindowID: "w"})
	if err == nil {
		t.Fatal("expected Enqueue error to propagate")
	}
}

func reviewerNonEndTurnPayload() []byte {
	return []byte(`{"stop_reason":"max_tokens","input_tokens":1,"output_tokens":1,"text":"{\"verdict\":\"x\"}"}`)
}

func TestReviewerL2LLMUnsuccessful(t *testing.T) {
	session := newFakeSession("tid-rev-l2-uns",
		subprocess.Message{Kind: subprocess.MessageKindResult, ID: "r-1", Method: "done", Payload: reviewerNonEndTurnPayload()},
	)
	mgr := newFakePersistentManager(session)
	cpq := newFakeCheckpointQueue()
	spec := makeReviewerSpec(t, worker.VariantReviewerL2, "reviewer-l2-uns")
	rev, _ := worker.NewReviewer(worker.ReviewerOptions{
		Spec: spec, WorktreePath: "/tmp/wt-rev-uns",
		SubprocessManager: mgr, SharedTaskList: newFakeSharedTaskList(),
		CheckpointQueue: cpq, FixPromptQueue: newFakeFixPromptQueue(),
		DoctrineConfig: fakeDoctrineConfig("", 30*time.Second),
		ToolRelay:      worker.NewUnavailableRelay(),
	})
	defer rev.Close()
	_ = cpq.Put(context.Background(), queue.Checkpoint{
		TaskID: "task-uns", ProjectID: spec.ProjectID, ThreadID: "t", StateJSON: `{}`, SeqNum: 1,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := rev.ReviewCheckpoint(ctx, "task-uns", "wkr-uns")
	if err == nil {
		t.Fatal("expected unsuccessful LLM result to error")
	}
}

func TestReviewerL3LLMUnsuccessful(t *testing.T) {
	session := newFakeSession("tid-rev-l3-uns",
		subprocess.Message{Kind: subprocess.MessageKindResult, ID: "r-1", Method: "done", Payload: reviewerNonEndTurnPayload()},
	)
	mgr := newFakePersistentManager(session)
	spec := makeReviewerSpec(t, worker.VariantReviewerL3, "reviewer-l3-uns")
	rev, _ := worker.NewReviewer(worker.ReviewerOptions{
		Spec: spec, WorktreePath: "/tmp/wt-rev-l3-uns",
		SubprocessManager: mgr, SharedTaskList: newFakeSharedTaskList(),
		CheckpointQueue: newFakeCheckpointQueue(), FixPromptQueue: newFakeFixPromptQueue(),
		DoctrineConfig: fakeDoctrineConfig("", 30*time.Second),
		ToolRelay:      worker.NewUnavailableRelay(),
	})
	defer rev.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := rev.ReviewAggregation(ctx, worker.AggregationInput{WindowID: "w"})
	if err == nil {
		t.Fatal("expected unsuccessful LLM result to error")
	}
}

func TestReviewerL4LLMUnsuccessful(t *testing.T) {
	session := newFakeSession("tid-rev-l4-uns",
		subprocess.Message{Kind: subprocess.MessageKindResult, ID: "r-1", Method: "done", Payload: reviewerNonEndTurnPayload()},
	)
	mgr := newFakePersistentManager(session)
	spec := makeReviewerSpec(t, worker.VariantReviewerL4, "reviewer-l4-uns")
	rev, _ := worker.NewReviewer(worker.ReviewerOptions{
		Spec: spec, WorktreePath: "/tmp/wt-rev-l4-uns",
		SubprocessManager: mgr, SharedTaskList: newFakeSharedTaskList(),
		CheckpointQueue: newFakeCheckpointQueue(), FixPromptQueue: newFakeFixPromptQueue(),
		DoctrineConfig: fakeDoctrineConfig("", 30*time.Second),
		ToolRelay:      worker.NewUnavailableRelay(),
	})
	defer rev.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := rev.ProposeAmendment(ctx, worker.AggregationInput{WindowID: "w"})
	if err == nil {
		t.Fatal("expected unsuccessful LLM result to error")
	}
}

func TestReviewerL3VerdictParseError(t *testing.T) {
	payload := []byte(`{"stop_reason":"end_turn","input_tokens":1,"output_tokens":1,"text":"no json at all"}`)
	session := newFakeSession("tid-rev-l3-vp",
		subprocess.Message{Kind: subprocess.MessageKindResult, ID: "r-1", Method: "done", Payload: payload},
	)
	mgr := newFakePersistentManager(session)
	spec := makeReviewerSpec(t, worker.VariantReviewerL3, "reviewer-l3-vp")
	rev, _ := worker.NewReviewer(worker.ReviewerOptions{
		Spec: spec, WorktreePath: "/tmp/wt-rev-l3-vp",
		SubprocessManager: mgr, SharedTaskList: newFakeSharedTaskList(),
		CheckpointQueue: newFakeCheckpointQueue(), FixPromptQueue: newFakeFixPromptQueue(),
		DoctrineConfig: fakeDoctrineConfig("", 30*time.Second),
		ToolRelay:      worker.NewUnavailableRelay(),
	})
	defer rev.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := rev.ReviewAggregation(ctx, worker.AggregationInput{WindowID: "w"})
	if err == nil {
		t.Fatal("expected verdict parse error")
	}
}

func TestReviewerL4AmendmentsParseError(t *testing.T) {
	payload := []byte(`{"stop_reason":"end_turn","input_tokens":1,"output_tokens":1,"text":"no json at all"}`)
	session := newFakeSession("tid-rev-l4-pe",
		subprocess.Message{Kind: subprocess.MessageKindResult, ID: "r-1", Method: "done", Payload: payload},
	)
	mgr := newFakePersistentManager(session)
	spec := makeReviewerSpec(t, worker.VariantReviewerL4, "reviewer-l4-pe")
	rev, _ := worker.NewReviewer(worker.ReviewerOptions{
		Spec: spec, WorktreePath: "/tmp/wt-rev-l4-pe",
		SubprocessManager: mgr, SharedTaskList: newFakeSharedTaskList(),
		CheckpointQueue: newFakeCheckpointQueue(), FixPromptQueue: newFakeFixPromptQueue(),
		DoctrineConfig: fakeDoctrineConfig("", 30*time.Second),
		ToolRelay:      worker.NewUnavailableRelay(),
	})
	defer rev.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := rev.ProposeAmendment(ctx, worker.AggregationInput{WindowID: "w"})
	if err == nil {
		t.Fatal("expected amendments parse error")
	}
}

func TestReviewerL3CleanVerdictNoFixPrompts(t *testing.T) {
	payload := []byte(`{"stop_reason":"end_turn","input_tokens":1,"output_tokens":1,"text":"{\"verdict\":\"clean\"}"}`)
	session := newFakeSession("tid-rev-l3-clean",
		subprocess.Message{Kind: subprocess.MessageKindResult, ID: "r-1", Method: "done", Payload: payload},
	)
	mgr := newFakePersistentManager(session)
	fpq := newFakeFixPromptQueue()
	spec := makeReviewerSpec(t, worker.VariantReviewerL3, "reviewer-l3-clean")
	rev, _ := worker.NewReviewer(worker.ReviewerOptions{
		Spec: spec, WorktreePath: "/tmp/wt-rev-l3-clean",
		SubprocessManager: mgr, SharedTaskList: newFakeSharedTaskList(),
		CheckpointQueue: newFakeCheckpointQueue(), FixPromptQueue: fpq,
		DoctrineConfig: fakeDoctrineConfig("", 30*time.Second),
		ToolRelay:      worker.NewUnavailableRelay(),
	})
	defer rev.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := rev.ReviewAggregation(ctx, worker.AggregationInput{
		WindowID:      "w-clean",
		AnchorTaskIDs: []queue.TaskID{"a-1"},
	})
	if err != nil {
		t.Fatalf("ReviewAggregation: %v", err)
	}
	if len(fpq.snapshot()) != 0 {
		t.Errorf("clean verdict emitted FixPrompts; want 0")
	}
}

func TestReviewerL3MissingAnchorWorkerIDsDefaultsToReviewerSpecID(t *testing.T) {
	payload := []byte(`{"stop_reason":"end_turn","input_tokens":1,"output_tokens":1,"text":"{\"verdict\":\"strategic\",\"concerns\":[\"x\"]}"}`)
	session := newFakeSession("tid-rev-l3-mw",
		subprocess.Message{Kind: subprocess.MessageKindResult, ID: "r-1", Method: "done", Payload: payload},
	)
	mgr := newFakePersistentManager(session)
	fpq := newFakeFixPromptQueue()
	spec := makeReviewerSpec(t, worker.VariantReviewerL3, "reviewer-l3-mw")
	rev, _ := worker.NewReviewer(worker.ReviewerOptions{
		Spec: spec, WorktreePath: "/tmp/wt-rev-l3-mw",
		SubprocessManager: mgr, SharedTaskList: newFakeSharedTaskList(),
		CheckpointQueue: newFakeCheckpointQueue(), FixPromptQueue: fpq,
		DoctrineConfig: fakeDoctrineConfig("", 30*time.Second),
		ToolRelay:      worker.NewUnavailableRelay(),
	})
	defer rev.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := rev.ReviewAggregation(ctx, worker.AggregationInput{
		WindowID:        "w-mw",
		AnchorTaskIDs:   []queue.TaskID{"a-1", "a-2"},
		AnchorWorkerIDs: []string{},
	})
	if err != nil {
		t.Fatalf("ReviewAggregation: %v", err)
	}
	pending, _ := fpq.PendingByWorker(context.Background(), spec.ID)
	if len(pending) != 2 {
		t.Errorf("Defaulted to spec.ID = %d FixPrompts; want 2", len(pending))
	}
}

type fakeFpqBadPut struct{ err error }

func (f *fakeFpqBadPut) Put(_ context.Context, _ queue.FixPrompt) error { return f.err }
func (f *fakeFpqBadPut) DrainByWorker(_ context.Context, _ string) ([]queue.FixPrompt, error) {
	return nil, nil
}
func (f *fakeFpqBadPut) PendingByWorker(_ context.Context, _ string) ([]queue.FixPrompt, error) {
	return nil, nil
}

func TestReviewerL3FpqPutError(t *testing.T) {
	payload := []byte(`{"stop_reason":"end_turn","input_tokens":1,"output_tokens":1,"text":"{\"verdict\":\"strategic\",\"concerns\":[\"x\"]}"}`)
	session := newFakeSession("tid-rev-l3-fpe",
		subprocess.Message{Kind: subprocess.MessageKindResult, ID: "r-1", Method: "done", Payload: payload},
	)
	mgr := newFakePersistentManager(session)
	spec := makeReviewerSpec(t, worker.VariantReviewerL3, "reviewer-l3-fpe")
	rev, _ := worker.NewReviewer(worker.ReviewerOptions{
		Spec: spec, WorktreePath: "/tmp/wt-rev-l3-fpe",
		SubprocessManager: mgr, SharedTaskList: newFakeSharedTaskList(),
		CheckpointQueue: newFakeCheckpointQueue(),
		FixPromptQueue:  &fakeFpqBadPut{err: errors.New("fpq put failed")},
		DoctrineConfig:  fakeDoctrineConfig("", 30*time.Second),
		ToolRelay:       worker.NewUnavailableRelay(),
	})
	defer rev.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := rev.ReviewAggregation(ctx, worker.AggregationInput{
		WindowID: "w-fpe", AnchorTaskIDs: []queue.TaskID{"a-1"}, AnchorWorkerIDs: []string{"wkr-1"},
	})
	if err == nil {
		t.Fatal("expected fpq.Put error to propagate")
	}
}

type errEmitter struct{ err error }

func (e *errEmitter) Emit(_ context.Context, _ worker.AmendmentProposal) error { return e.err }
func (e *errEmitter) Proposals() []worker.AmendmentProposal                    { return nil }

func TestReviewerL4EmitError(t *testing.T) {
	payload := []byte(`{"stop_reason":"end_turn","input_tokens":1,"output_tokens":1,"text":"{\"amendments\":[{\"area\":\"x\",\"change\":\"y\"}]}"}`)
	session := newFakeSession("tid-rev-l4-emit",
		subprocess.Message{Kind: subprocess.MessageKindResult, ID: "r-1", Method: "done", Payload: payload},
	)
	mgr := newFakePersistentManager(session)
	spec := makeReviewerSpec(t, worker.VariantReviewerL4, "reviewer-l4-emit")
	rev, _ := worker.NewReviewer(worker.ReviewerOptions{
		Spec: spec, WorktreePath: "/tmp/wt-rev-l4-emit",
		SubprocessManager: mgr, SharedTaskList: newFakeSharedTaskList(),
		CheckpointQueue: newFakeCheckpointQueue(), FixPromptQueue: newFakeFixPromptQueue(),
		DoctrineConfig:           fakeDoctrineConfig("", 30*time.Second),
		ToolRelay:                worker.NewUnavailableRelay(),
		AmendmentProposalEmitter: &errEmitter{err: errors.New("emit failed")},
	})
	defer rev.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := rev.ProposeAmendment(ctx, worker.AggregationInput{WindowID: "w-emit"})
	if err == nil {
		t.Fatal("expected emit error to propagate")
	}
}

func TestReviewerPanicsOnInvalidSpec(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on invalid spec")
		}
	}()
	session := newFakeSession("tid-rev-bad-spec")
	mgr := newFakePersistentManager(session)
	spec := makeReviewerSpec(t, worker.VariantReviewerL2, "x")
	spec.ID = ""
	_, _ = worker.NewReviewer(worker.ReviewerOptions{
		Spec: spec, WorktreePath: "/tmp/wt",
		SubprocessManager: mgr, SharedTaskList: newFakeSharedTaskList(),
		CheckpointQueue: newFakeCheckpointQueue(), FixPromptQueue: newFakeFixPromptQueue(),
		DoctrineConfig: fakeDoctrineConfig("", 30*time.Second),
		ToolRelay:      worker.NewUnavailableRelay(),
	})
	t.Fatal("unreachable")
}

func TestReviewerDefaultsNilToolRelay(t *testing.T) {
	session := newFakeSession("tid-rev-nrelay",
		subprocess.Message{Kind: subprocess.MessageKindResult, ID: "r-1", Method: "done", Payload: reviewerVerdictPayload("clean")},
	)
	mgr := newFakePersistentManager(session)
	spec := makeReviewerSpec(t, worker.VariantReviewerL2, "reviewer-l2-nrelay")
	rev, err := worker.NewReviewer(worker.ReviewerOptions{
		Spec: spec, WorktreePath: "/tmp/wt-rev-nrelay",
		SubprocessManager: mgr, SharedTaskList: newFakeSharedTaskList(),
		CheckpointQueue: newFakeCheckpointQueue(), FixPromptQueue: newFakeFixPromptQueue(),
		DoctrineConfig: fakeDoctrineConfig("", 30*time.Second),
		ToolRelay:      nil,
	})
	if err != nil {
		t.Fatalf("NewReviewer: %v", err)
	}
	defer rev.Close()
}

func TestReviewerL3VerdictMalformedJSONInsideBraces(t *testing.T) {
	payload := []byte(`{"stop_reason":"end_turn","input_tokens":1,"output_tokens":1,"text":"prefix {malformed json} suffix"}`)
	session := newFakeSession("tid-rev-l3-mj",
		subprocess.Message{Kind: subprocess.MessageKindResult, ID: "r-1", Method: "done", Payload: payload},
	)
	mgr := newFakePersistentManager(session)
	spec := makeReviewerSpec(t, worker.VariantReviewerL3, "reviewer-l3-mj")
	rev, _ := worker.NewReviewer(worker.ReviewerOptions{
		Spec: spec, WorktreePath: "/tmp/wt-rev-l3-mj",
		SubprocessManager: mgr, SharedTaskList: newFakeSharedTaskList(),
		CheckpointQueue: newFakeCheckpointQueue(), FixPromptQueue: newFakeFixPromptQueue(),
		DoctrineConfig: fakeDoctrineConfig("", 30*time.Second),
		ToolRelay:      worker.NewUnavailableRelay(),
	})
	defer rev.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := rev.ReviewAggregation(ctx, worker.AggregationInput{WindowID: "w"})
	if err == nil {
		t.Fatal("expected verdict unmarshal error")
	}
}

func TestReviewerL4AmendmentsMalformedJSONInsideBraces(t *testing.T) {
	payload := []byte(`{"stop_reason":"end_turn","input_tokens":1,"output_tokens":1,"text":"prefix {malformed} suffix"}`)
	session := newFakeSession("tid-rev-l4-mj",
		subprocess.Message{Kind: subprocess.MessageKindResult, ID: "r-1", Method: "done", Payload: payload},
	)
	mgr := newFakePersistentManager(session)
	spec := makeReviewerSpec(t, worker.VariantReviewerL4, "reviewer-l4-mj")
	rev, _ := worker.NewReviewer(worker.ReviewerOptions{
		Spec: spec, WorktreePath: "/tmp/wt-rev-l4-mj",
		SubprocessManager: mgr, SharedTaskList: newFakeSharedTaskList(),
		CheckpointQueue: newFakeCheckpointQueue(), FixPromptQueue: newFakeFixPromptQueue(),
		DoctrineConfig: fakeDoctrineConfig("", 30*time.Second),
		ToolRelay:      worker.NewUnavailableRelay(),
	})
	defer rev.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := rev.ProposeAmendment(ctx, worker.AggregationInput{WindowID: "w"})
	if err == nil {
		t.Fatal("expected amendments unmarshal error")
	}
}
