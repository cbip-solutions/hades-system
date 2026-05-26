package worker_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/workforce/queue"
	"github.com/cbip-solutions/hades-system/internal/workforce/subprocess"
	"github.com/cbip-solutions/hades-system/internal/workforce/worker"
)

type fakePersistentManager struct {
	mu                  sync.Mutex
	acquireCalls        int
	spawnEphemeralCalls int
	releaseCalls        int
	lastSpec            worker.WorkerSpec
	lastWorktreePath    string
	session             subprocess.Session
	acquireErr          error
}

func newFakePersistentManager(s subprocess.Session) *fakePersistentManager {
	return &fakePersistentManager{session: s}
}

func (f *fakePersistentManager) AcquirePersistent(ctx context.Context, spec worker.WorkerSpec, worktreePath string) (subprocess.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.acquireCalls++
	f.lastSpec = spec
	f.lastWorktreePath = worktreePath
	if f.acquireErr != nil {
		return nil, f.acquireErr
	}
	return f.session, nil
}

func (f *fakePersistentManager) SpawnEphemeral(ctx context.Context, spec worker.WorkerSpec, worktreePath string) (subprocess.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.spawnEphemeralCalls++
	f.lastSpec = spec
	f.lastWorktreePath = worktreePath
	if f.acquireErr != nil {
		return nil, f.acquireErr
	}
	return f.session, nil
}

func (f *fakePersistentManager) Release(s subprocess.Session) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.releaseCalls++
	return nil
}

type alwaysDoneChild struct{ res worker.RunResult }

func (c alwaysDoneChild) Run(ctx context.Context, req worker.RunRequest) (worker.RunResult, error) {
	return c.res, nil
}

type alwaysFailChild struct{ reason string }

func (c alwaysFailChild) Run(ctx context.Context, req worker.RunRequest) (worker.RunResult, error) {
	return worker.RunResult{Success: false, FailureReason: c.reason}, errors.New(c.reason)
}

type recordingFactory struct {
	mu      sync.Mutex
	child   worker.Worker
	calls   int
	parents []string
	specs   []worker.WorkerSpec
}

func (f *recordingFactory) NewChild(parentTaskID queue.TaskID, spec worker.WorkerSpec, worktreePath string) (worker.Worker, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.parents = append(f.parents, string(parentTaskID))
	f.specs = append(f.specs, spec)
	return f.child, nil
}

type errorFactory struct{ err error }

func (f errorFactory) NewChild(parentTaskID queue.TaskID, spec worker.WorkerSpec, worktreePath string) (worker.Worker, error) {
	return nil, f.err
}

type teamLeadFixture struct {
	spec    worker.WorkerSpec
	stl     *fakeSharedTaskList
	cpq     *fakeCheckpointQueue
	fpq     *fakeFixPromptQueue
	session *fakeSession
	cfg     worker.DoctrineConfig
	manager *fakePersistentManager
	factory worker.WorkerFactory
}

func newTeamLeadFixture(t *testing.T, planJSON string) *teamLeadFixture {
	t.Helper()
	plannerInbound := []subprocess.Message{
		{
			Kind:    subprocess.MessageKindResult,
			ID:      "p-1",
			Method:  "done",
			Payload: json.RawMessage(`{"stop_reason":"end_turn","input_tokens":10,"output_tokens":20,"text":` + jsonString(planJSON) + `}`),
		},
	}
	session := newFakeSession("tid-tl", plannerInbound...)
	spec, err := worker.NewSpec(worker.SpecOptions{
		ID:             "team-lead-1",
		Variant:        worker.VariantTeamLead,
		TaskTier:       worker.TierComplex,
		ModelClass:     "tier-teamlead",
		Tools:          []string{"research_dispatch"},
		Quota:          worker.Quota{MaxTokens: 100000, MaxCostUSD: 5.0, MaxDuration: 30 * time.Minute},
		RecoveryPolicy: worker.RecoveryDoctrineBound,
		DoctrineName:   "max-scope",
		ProjectID:      "internal-platform-x",
	})
	if err != nil {
		t.Fatalf("NewSpec: %v", err)
	}
	return &teamLeadFixture{
		spec:    spec,
		stl:     newFakeSharedTaskList(),
		cpq:     newFakeCheckpointQueue(),
		fpq:     newFakeFixPromptQueue(),
		session: session,
		cfg:     fakeDoctrineConfig("", 30*time.Second),
		manager: newFakePersistentManager(session),
	}
}

func (f *teamLeadFixture) newTeamLead(t *testing.T) *worker.TeamLead {
	t.Helper()
	tl, err := worker.NewTeamLead(worker.TeamLeadOptions{
		Spec:               f.spec,
		WorktreePath:       "/tmp/wt-tl",
		SubprocessManager:  f.manager,
		SharedTaskList:     f.stl,
		CheckpointQueue:    f.cpq,
		FixPromptQueue:     f.fpq,
		DoctrineConfig:     f.cfg,
		ToolRelay:          worker.NewUnavailableRelay(),
		ChildWorkerFactory: f.factory,
	})
	if err != nil {
		t.Fatalf("NewTeamLead: %v", err)
	}
	return tl
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func (f *teamLeadFixture) enqueueParentTask(t *testing.T, taskID, prompt string) {
	t.Helper()
	err := f.stl.Enqueue(context.Background(), queue.TaskRow{
		TaskID:      queue.TaskID(taskID),
		ProjectID:   f.spec.ProjectID,
		Description: prompt,
		Status:      queue.StatusPending,
	})
	if err != nil {
		t.Fatalf("enqueueParentTask: %v", err)
	}
}

func TestTeamLeadAcquiresPersistentSession(t *testing.T) {
	plan := `{"plan":[{"id":"sub-1","prompt":"do A"}]}`
	f := newTeamLeadFixture(t, plan)
	f.factory = &recordingFactory{child: alwaysDoneChild{res: worker.RunResult{Success: true, FinalStopReason: "end_turn"}}}
	tl := f.newTeamLead(t)
	if tl == nil {
		t.Fatal("nil TeamLead")
	}
	if f.manager.acquireCalls != 1 {
		t.Errorf("AcquirePersistent calls = %d, want 1", f.manager.acquireCalls)
	}
	if f.manager.spawnEphemeralCalls != 0 {
		t.Errorf("SpawnEphemeral calls = %d, want 0", f.manager.spawnEphemeralCalls)
	}
	if f.manager.lastSpec.ID != "team-lead-1" {
		t.Errorf("lastSpec.ID = %q", f.manager.lastSpec.ID)
	}
}

func TestTeamLeadRejectsNonTeamLeadSpec(t *testing.T) {
	plan := `{"plan":[{"id":"x","prompt":"x"}]}`
	f := newTeamLeadFixture(t, plan)
	f.spec.Variant = worker.VariantWorker
	_, err := worker.NewTeamLead(worker.TeamLeadOptions{
		Spec:              f.spec,
		WorktreePath:      "/tmp/wt-tl-bad",
		SubprocessManager: f.manager,
		SharedTaskList:    f.stl,
		CheckpointQueue:   f.cpq,
		FixPromptQueue:    f.fpq,
		DoctrineConfig:    f.cfg,
		ToolRelay:         worker.NewUnavailableRelay(),
	})
	if err == nil {
		t.Fatal("expected error for non-teamlead Spec.Variant")
	}
}

func TestTeamLeadRequiresSubprocessManager(t *testing.T) {
	plan := `{}`
	f := newTeamLeadFixture(t, plan)
	_, err := worker.NewTeamLead(worker.TeamLeadOptions{
		Spec:              f.spec,
		WorktreePath:      "/tmp/wt-tl",
		SubprocessManager: nil,
		SharedTaskList:    f.stl,
		CheckpointQueue:   f.cpq,
		FixPromptQueue:    f.fpq,
		DoctrineConfig:    f.cfg,
		ToolRelay:         worker.NewUnavailableRelay(),
	})
	if err == nil {
		t.Fatal("expected error for nil SubprocessManager")
	}
}

func TestTeamLeadPanicsOnNilWorktree(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on empty worktreePath")
		}
		err, _ := r.(error)
		if !errors.Is(err, worker.ErrNilWorktreePath) {
			t.Fatalf("err = %v, want ErrNilWorktreePath", err)
		}
	}()
	plan := `{}`
	f := newTeamLeadFixture(t, plan)
	_, _ = worker.NewTeamLead(worker.TeamLeadOptions{
		Spec:              f.spec,
		WorktreePath:      "",
		SubprocessManager: f.manager,
		SharedTaskList:    f.stl,
		CheckpointQueue:   f.cpq,
		FixPromptQueue:    f.fpq,
		DoctrineConfig:    f.cfg,
		ToolRelay:         worker.NewUnavailableRelay(),
	})
	t.Fatal("unreachable")
}

func TestTeamLeadPanicsOnNilQueues(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on nil queues")
		}
		err, _ := r.(error)
		if !errors.Is(err, worker.ErrNilQueues) {
			t.Fatalf("err = %v, want ErrNilQueues", err)
		}
	}()
	plan := `{}`
	f := newTeamLeadFixture(t, plan)
	_, _ = worker.NewTeamLead(worker.TeamLeadOptions{
		Spec:              f.spec,
		WorktreePath:      "/tmp/wt-tl",
		SubprocessManager: f.manager,
		SharedTaskList:    nil,
		CheckpointQueue:   f.cpq,
		FixPromptQueue:    f.fpq,
		DoctrineConfig:    f.cfg,
		ToolRelay:         worker.NewUnavailableRelay(),
	})
	t.Fatal("unreachable")
}

func TestTeamLeadPanicsOnNilDoctrineConfig(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic")
		}
		err, _ := r.(error)
		if !errors.Is(err, worker.ErrNilDoctrineConfig) {
			t.Fatalf("err = %v", err)
		}
	}()
	plan := `{}`
	f := newTeamLeadFixture(t, plan)
	_, _ = worker.NewTeamLead(worker.TeamLeadOptions{
		Spec:              f.spec,
		WorktreePath:      "/tmp/wt-tl",
		SubprocessManager: f.manager,
		SharedTaskList:    f.stl,
		CheckpointQueue:   f.cpq,
		FixPromptQueue:    f.fpq,
		DoctrineConfig:    nil,
		ToolRelay:         worker.NewUnavailableRelay(),
	})
	t.Fatal("unreachable")
}

func TestTeamLeadAcquireError(t *testing.T) {
	plan := `{}`
	f := newTeamLeadFixture(t, plan)
	f.manager.acquireErr = errors.New("subprocess down")
	_, err := worker.NewTeamLead(worker.TeamLeadOptions{
		Spec:              f.spec,
		WorktreePath:      "/tmp/wt-tl",
		SubprocessManager: f.manager,
		SharedTaskList:    f.stl,
		CheckpointQueue:   f.cpq,
		FixPromptQueue:    f.fpq,
		DoctrineConfig:    f.cfg,
		ToolRelay:         worker.NewUnavailableRelay(),
	})
	if err == nil {
		t.Fatal("expected AcquirePersistent error to propagate")
	}
	if !strings.Contains(err.Error(), "subprocess down") {
		t.Errorf("err = %v, want substring 'subprocess down'", err)
	}
}

func TestTeamLeadPlanAndDispatchChildren(t *testing.T) {
	plan := `Some preamble. {"plan":[{"id":"sub-1","prompt":"do A"},{"id":"sub-2","prompt":"do B"}]} trailing.`
	f := newTeamLeadFixture(t, plan)
	rec := &recordingFactory{
		child: alwaysDoneChild{res: worker.RunResult{Success: true, TokensUsed: 5, FinalStopReason: "end_turn"}},
	}
	f.factory = rec
	tl := f.newTeamLead(t)
	f.enqueueParentTask(t, "task-parent-1", "complex objective")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := tl.Run(ctx, worker.RunRequest{TaskID: "task-parent-1", Prompt: "complex objective"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Success {
		t.Errorf("Success=false: %s", res.FailureReason)
	}

	if rec.calls != 2 {
		t.Errorf("factory calls = %d, want 2", rec.calls)
	}

	for _, p := range rec.parents {
		if p != "task-parent-1" {
			t.Errorf("child parent = %q, want task-parent-1", p)
		}
	}

	for _, s := range rec.specs {
		if s.Variant != worker.VariantWorker {
			t.Errorf("child spec.Variant = %v, want VariantWorker", s.Variant)
		}
	}

	children := tl.Children("task-parent-1")
	if len(children) != 2 {
		t.Errorf("Children = %v, want len 2", children)
	}
}

func TestTeamLeadAggregatesResults(t *testing.T) {
	plan := `{"plan":[{"id":"sub-1","prompt":"do A"}]}`
	f := newTeamLeadFixture(t, plan)
	childRes := worker.RunResult{
		Success: true, TokensUsed: 100, CostUSD: 0.5, ToolUseCount: 3,
		Artifacts: []string{"foo.go"}, FinalStopReason: "end_turn",
	}
	f.factory = &recordingFactory{child: alwaysDoneChild{res: childRes}}
	tl := f.newTeamLead(t)
	f.enqueueParentTask(t, "task-parent-agg", "x")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := tl.Run(ctx, worker.RunRequest{TaskID: "task-parent-agg", Prompt: "x"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if res.TokensUsed < 130 {
		t.Errorf("TokensUsed = %d, want >= 130", res.TokensUsed)
	}
	if res.CostUSD < 0.5 {
		t.Errorf("CostUSD = %.4f, want >= 0.5", res.CostUSD)
	}
	if len(res.Artifacts) == 0 {
		t.Error("Artifacts empty after aggregation")
	}
}

func TestTeamLeadChildFailureEmitsFixPrompt(t *testing.T) {
	plan := `{"plan":[{"id":"sub-1","prompt":"will fail"}]}`
	f := newTeamLeadFixture(t, plan)
	f.factory = &recordingFactory{child: alwaysFailChild{reason: "compile error"}}
	tl := f.newTeamLead(t)
	f.enqueueParentTask(t, "task-parent-fail", "x")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, _ := tl.Run(ctx, worker.RunRequest{TaskID: "task-parent-fail", Prompt: "x"})
	if res.Success {
		t.Error("Success=true; expected false on child failure")
	}

	pending, _ := f.fpq.PendingByWorker(context.Background(), f.spec.ID)
	if len(pending) == 0 {

		all := f.fpq.snapshot()
		if len(all) == 0 {
			t.Error("expected FixPromptQueue row emitted for child failure")
		}
	}
}

func TestTeamLeadPlannerEmptyPlanFails(t *testing.T) {
	plan := `{"plan":[]}`
	f := newTeamLeadFixture(t, plan)
	f.factory = &recordingFactory{child: alwaysDoneChild{}}
	tl := f.newTeamLead(t)
	f.enqueueParentTask(t, "task-parent-empty", "x")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := tl.Run(ctx, worker.RunRequest{TaskID: "task-parent-empty", Prompt: "x"})
	if err == nil {
		t.Fatal("expected error on empty plan")
	}
	if res.Success {
		t.Error("Success=true; want false")
	}
}

func TestTeamLeadPlannerMissingJSONFails(t *testing.T) {
	plan := `no json here at all`
	f := newTeamLeadFixture(t, plan)
	f.factory = &recordingFactory{child: alwaysDoneChild{}}
	tl := f.newTeamLead(t)
	f.enqueueParentTask(t, "task-parent-nojson", "x")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := tl.Run(ctx, worker.RunRequest{TaskID: "task-parent-nojson", Prompt: "x"})
	if err == nil {
		t.Fatal("expected error when planner output has no JSON")
	}
}

func TestTeamLeadPlannerMalformedJSONFails(t *testing.T) {
	plan := `prefix {"plan": malformed} suffix`
	f := newTeamLeadFixture(t, plan)
	f.factory = &recordingFactory{child: alwaysDoneChild{}}
	tl := f.newTeamLead(t)
	f.enqueueParentTask(t, "task-parent-bad", "x")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := tl.Run(ctx, worker.RunRequest{TaskID: "task-parent-bad", Prompt: "x"})
	if err == nil {
		t.Fatal("expected error on malformed plan JSON")
	}
}

func TestTeamLeadFactoryError(t *testing.T) {
	plan := `{"plan":[{"id":"sub-1","prompt":"x"}]}`
	f := newTeamLeadFixture(t, plan)
	f.factory = errorFactory{err: errors.New("factory blew up")}
	tl := f.newTeamLead(t)
	f.enqueueParentTask(t, "task-parent-fac", "x")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := tl.Run(ctx, worker.RunRequest{TaskID: "task-parent-fac", Prompt: "x"})
	if err == nil {
		t.Fatal("expected error when factory fails")
	}
	if res.Success {
		t.Error("Success=true; want false")
	}
}

func TestTeamLeadRejectsInvalidRequest(t *testing.T) {
	plan := `{"plan":[]}`
	f := newTeamLeadFixture(t, plan)
	f.factory = &recordingFactory{child: alwaysDoneChild{}}
	tl := f.newTeamLead(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := tl.Run(ctx, worker.RunRequest{TaskID: "", Prompt: "x"}); err == nil {
		t.Error("expected error for empty TaskID")
	}
}

func TestTeamLeadCloseReleasesSession(t *testing.T) {
	plan := `{"plan":[]}`
	f := newTeamLeadFixture(t, plan)
	f.factory = &recordingFactory{child: alwaysDoneChild{}}
	tl := f.newTeamLead(t)
	if err := tl.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if f.manager.releaseCalls != 1 {
		t.Errorf("Release calls = %d, want 1", f.manager.releaseCalls)
	}
}

func TestTeamLeadClaimParentNotFound(t *testing.T) {
	plan := `{"plan":[{"id":"sub-1","prompt":"x"}]}`
	f := newTeamLeadFixture(t, plan)
	f.factory = &recordingFactory{child: alwaysDoneChild{res: worker.RunResult{Success: true, FinalStopReason: "end_turn"}}}
	tl := f.newTeamLead(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := tl.Run(ctx, worker.RunRequest{TaskID: "task-not-enqueued", Prompt: "x"})
	if !errors.Is(err, worker.ErrTaskNotFound) {
		t.Fatalf("err = %v, want ErrTaskNotFound", err)
	}
}

func TestTeamLeadEnqueuePlannerRowError(t *testing.T) {
	plan := `{"plan":[{"id":"sub-1","prompt":"x"}]}`
	f := newTeamLeadFixture(t, plan)
	f.factory = &recordingFactory{child: alwaysDoneChild{}}
	tl := f.newTeamLead(t)
	f.enqueueParentTask(t, "task-eq-err", "x")

	f.stl.enqErr = errors.New("disk write failed")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := tl.Run(ctx, worker.RunRequest{TaskID: "task-eq-err", Prompt: "x"})
	if err == nil {
		t.Fatal("expected Enqueue planner-row error to propagate")
	}
	if !strings.Contains(err.Error(), "disk write failed") {
		t.Errorf("err = %v, want substring 'disk write failed'", err)
	}
}

func TestTeamLeadCloseWithoutSession(t *testing.T) {

	plan := `{"plan":[]}`
	f := newTeamLeadFixture(t, plan)
	f.factory = &recordingFactory{child: alwaysDoneChild{}}
	tl := f.newTeamLead(t)
	if err := tl.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}

	if err := tl.Close(); err != nil {
		t.Errorf("Close (second): %v", err)
	}
}

func TestTeamLeadParsesChildTaskTier(t *testing.T) {
	plan := `{"plan":[{"id":"sub-1","prompt":"x","task_tier":"complex"}]}`
	f := newTeamLeadFixture(t, plan)
	rec := &recordingFactory{child: alwaysDoneChild{res: worker.RunResult{Success: true, FinalStopReason: "end_turn"}}}
	f.factory = rec
	tl := f.newTeamLead(t)
	f.enqueueParentTask(t, "task-tier", "x")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := tl.Run(ctx, worker.RunRequest{TaskID: "task-tier", Prompt: "x"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(rec.specs) == 0 {
		t.Fatal("no children dispatched")
	}
	if rec.specs[0].TaskTier != worker.TierComplex {
		t.Errorf("child TaskTier = %v, want TierComplex (parsed from plan)", rec.specs[0].TaskTier)
	}
}

func TestTeamLeadParsesUnknownChildTaskTierDefaults(t *testing.T) {
	plan := `{"plan":[{"id":"sub-1","prompt":"x","task_tier":"galactic"}]}`
	f := newTeamLeadFixture(t, plan)
	rec := &recordingFactory{child: alwaysDoneChild{res: worker.RunResult{Success: true, FinalStopReason: "end_turn"}}}
	f.factory = rec
	tl := f.newTeamLead(t)
	f.enqueueParentTask(t, "task-tier-unk", "x")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := tl.Run(ctx, worker.RunRequest{TaskID: "task-tier-unk", Prompt: "x"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rec.specs[0].TaskTier != worker.TierMedium {
		t.Errorf("child TaskTier = %v, want TierMedium (unknown fallback)", rec.specs[0].TaskTier)
	}
}

func TestTeamLeadDefaultsNilToolRelay(t *testing.T) {
	plan := `{"plan":[{"id":"sub-1","prompt":"x"}]}`
	f := newTeamLeadFixture(t, plan)
	f.factory = &recordingFactory{child: alwaysDoneChild{res: worker.RunResult{Success: true, FinalStopReason: "end_turn"}}}
	tl, err := worker.NewTeamLead(worker.TeamLeadOptions{
		Spec:               f.spec,
		WorktreePath:       "/tmp/wt-tl-nrelay",
		SubprocessManager:  f.manager,
		SharedTaskList:     f.stl,
		CheckpointQueue:    f.cpq,
		FixPromptQueue:     f.fpq,
		DoctrineConfig:     f.cfg,
		ToolRelay:          nil,
		ChildWorkerFactory: f.factory,
	})
	if err != nil {
		t.Fatalf("NewTeamLead: %v", err)
	}
	if tl == nil {
		t.Fatal("nil TeamLead")
	}
}

func TestTeamLeadPanicsOnInvalidSpec(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on invalid spec")
		}
	}()
	plan := `{}`
	f := newTeamLeadFixture(t, plan)
	bad := f.spec
	bad.ID = ""
	_, _ = worker.NewTeamLead(worker.TeamLeadOptions{
		Spec:               bad,
		WorktreePath:       "/tmp/wt-tl-bad-spec",
		SubprocessManager:  f.manager,
		SharedTaskList:     f.stl,
		CheckpointQueue:    f.cpq,
		FixPromptQueue:     f.fpq,
		DoctrineConfig:     f.cfg,
		ToolRelay:          worker.NewUnavailableRelay(),
		ChildWorkerFactory: nil,
	})
	t.Fatal("unreachable")
}

func TestTeamLeadClaimParentGetError(t *testing.T) {
	plan := `{"plan":[{"id":"sub-1","prompt":"x"}]}`
	plannerInbound := []subprocess.Message{
		{Kind: subprocess.MessageKindResult, ID: "p-1", Method: "done",
			Payload: json.RawMessage(`{"stop_reason":"end_turn","input_tokens":1,"output_tokens":1,"text":` + jsonString(plan) + `}`),
		},
	}
	session := newFakeSession("tid-tl-cge", plannerInbound...)
	spec, _ := worker.NewSpec(worker.SpecOptions{
		ID: "team-lead-cge", Variant: worker.VariantTeamLead,
		TaskTier: worker.TierComplex, ModelClass: "tier-teamlead",
		Tools:          []string{"research_dispatch"},
		Quota:          worker.Quota{MaxTokens: 1000, MaxCostUSD: 1, MaxDuration: time.Minute},
		RecoveryPolicy: worker.RecoveryDoctrineBound,
		DoctrineName:   "max-scope", ProjectID: "internal-platform-x",
	})
	mgr := newFakePersistentManager(session)

	stl := &stlPlannerOKThenError{
		realFake: newFakeSharedTaskList(),
		errOn:    1,
	}
	cpq := newFakeCheckpointQueue()
	fpq := newFakeFixPromptQueue()
	tl, err := worker.NewTeamLead(worker.TeamLeadOptions{
		Spec: spec, WorktreePath: "/tmp/wt-tl-cge",
		SubprocessManager: mgr, SharedTaskList: stl,
		CheckpointQueue: cpq, FixPromptQueue: fpq,
		DoctrineConfig:     fakeDoctrineConfig("", 30*time.Second),
		ToolRelay:          worker.NewUnavailableRelay(),
		ChildWorkerFactory: &recordingFactory{child: alwaysDoneChild{res: worker.RunResult{Success: true}}},
	})
	if err != nil {
		t.Fatalf("NewTeamLead: %v", err)
	}

	_ = stl.realFake.Enqueue(context.Background(), queue.TaskRow{
		TaskID: "task-cge-parent", ProjectID: spec.ProjectID, Status: queue.StatusPending,
	})
	stl.parentID = queue.TaskID("task-cge-parent")
	stl.parentErr = errors.New("transient db error")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = tl.Run(ctx, worker.RunRequest{TaskID: "task-cge-parent", Prompt: "x"})
	if err == nil {
		t.Fatal("expected claimParent.Get error to propagate")
	}
	if !strings.Contains(err.Error(), "transient db error") {
		t.Errorf("err = %v, want substring 'transient db error'", err)
	}
}

func TestTeamLeadParentAlreadyInProgress(t *testing.T) {
	plan := `{"plan":[{"id":"sub-1","prompt":"x"}]}`
	f := newTeamLeadFixture(t, plan)
	rec := &recordingFactory{child: alwaysDoneChild{res: worker.RunResult{Success: true, FinalStopReason: "end_turn"}}}
	f.factory = rec
	tl := f.newTeamLead(t)

	_ = f.stl.Enqueue(context.Background(), queue.TaskRow{
		TaskID:    "task-replay-tl",
		ProjectID: f.spec.ProjectID,
		Status:    queue.StatusInProgress,
		ThreadID:  string(f.session.ThreadID()),
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := tl.Run(ctx, worker.RunRequest{TaskID: "task-replay-tl", Prompt: "x"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Success {
		t.Errorf("Success=false: %s", res.FailureReason)
	}
}

func TestTeamLeadClaimParentRaceClaimError(t *testing.T) {
	plan := `{"plan":[{"id":"sub-1","prompt":"x"}]}`
	f := newTeamLeadFixture(t, plan)
	f.factory = &recordingFactory{child: alwaysDoneChild{res: worker.RunResult{Success: true}}}
	tl := f.newTeamLead(t)
	f.enqueueParentTask(t, "task-race", "x")

	f.stl.claimErr = nil

	stl := &stlNthClaimFails{realFake: f.stl, failOn: 2, err: queue.ErrTaskNotPending}
	tl2, _ := worker.NewTeamLead(worker.TeamLeadOptions{
		Spec: f.spec, WorktreePath: "/tmp/wt-tl-race",
		SubprocessManager: f.manager, SharedTaskList: stl,
		CheckpointQueue: f.cpq, FixPromptQueue: f.fpq,
		DoctrineConfig:     f.cfg,
		ToolRelay:          worker.NewUnavailableRelay(),
		ChildWorkerFactory: f.factory,
	})
	_ = tl
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := tl2.Run(ctx, worker.RunRequest{TaskID: "task-race", Prompt: "x"})
	if !errors.Is(err, worker.ErrTaskAlreadyClaimed) {
		t.Fatalf("err = %v, want ErrTaskAlreadyClaimed", err)
	}
}

func TestTeamLeadClaimParentRaceClaimNotFound(t *testing.T) {
	plan := `{"plan":[{"id":"sub-1","prompt":"x"}]}`
	f := newTeamLeadFixture(t, plan)
	f.factory = &recordingFactory{child: alwaysDoneChild{res: worker.RunResult{Success: true}}}
	f.enqueueParentTask(t, "task-race-nf", "x")
	stl := &stlNthClaimFails{realFake: f.stl, failOn: 2, err: queue.ErrTaskNotFound}
	tl, _ := worker.NewTeamLead(worker.TeamLeadOptions{
		Spec: f.spec, WorktreePath: "/tmp/wt-tl-rnf",
		SubprocessManager: f.manager, SharedTaskList: stl,
		CheckpointQueue: f.cpq, FixPromptQueue: f.fpq,
		DoctrineConfig:     f.cfg,
		ToolRelay:          worker.NewUnavailableRelay(),
		ChildWorkerFactory: f.factory,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := tl.Run(ctx, worker.RunRequest{TaskID: "task-race-nf", Prompt: "x"})
	if !errors.Is(err, worker.ErrTaskNotFound) {
		t.Fatalf("err = %v, want ErrTaskNotFound", err)
	}
}

func TestTeamLeadClaimParentGenericClaimError(t *testing.T) {
	plan := `{"plan":[{"id":"sub-1","prompt":"x"}]}`
	f := newTeamLeadFixture(t, plan)
	f.factory = &recordingFactory{child: alwaysDoneChild{res: worker.RunResult{Success: true}}}
	f.enqueueParentTask(t, "task-race-gen", "x")
	stl := &stlNthClaimFails{realFake: f.stl, failOn: 2, err: errors.New("generic db error")}
	tl, _ := worker.NewTeamLead(worker.TeamLeadOptions{
		Spec: f.spec, WorktreePath: "/tmp/wt-tl-gen",
		SubprocessManager: f.manager, SharedTaskList: stl,
		CheckpointQueue: f.cpq, FixPromptQueue: f.fpq,
		DoctrineConfig:     f.cfg,
		ToolRelay:          worker.NewUnavailableRelay(),
		ChildWorkerFactory: f.factory,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := tl.Run(ctx, worker.RunRequest{TaskID: "task-race-gen", Prompt: "x"})
	if err == nil {
		t.Fatal("expected generic Claim error to propagate")
	}
	if !strings.Contains(err.Error(), "generic db error") {
		t.Errorf("err = %v, want substring 'generic db error'", err)
	}
}

func TestTeamLeadDispatchChildEnqueueError(t *testing.T) {
	plan := `{"plan":[{"id":"sub-1","prompt":"x"}]}`
	f := newTeamLeadFixture(t, plan)
	f.factory = &recordingFactory{child: alwaysDoneChild{res: worker.RunResult{Success: true}}}
	f.enqueueParentTask(t, "task-eq-child-err", "x")

	stl := &stlNthEnqueueFails{realFake: f.stl, failOn: 2, err: errors.New("child enqueue boom")}
	tl, _ := worker.NewTeamLead(worker.TeamLeadOptions{
		Spec: f.spec, WorktreePath: "/tmp/wt-tl-cee",
		SubprocessManager: f.manager, SharedTaskList: stl,
		CheckpointQueue: f.cpq, FixPromptQueue: f.fpq,
		DoctrineConfig:     f.cfg,
		ToolRelay:          worker.NewUnavailableRelay(),
		ChildWorkerFactory: f.factory,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := tl.Run(ctx, worker.RunRequest{TaskID: "task-eq-child-err", Prompt: "x"})
	if err == nil {
		t.Fatal("expected child Enqueue error to propagate")
	}
	if !strings.Contains(err.Error(), "child enqueue boom") {
		t.Errorf("err = %v, want substring 'child enqueue boom'", err)
	}
}

func TestTeamLeadFinalizeParentGetError(t *testing.T) {
	plan := `{"plan":[{"id":"sub-1","prompt":"x"}]}`
	f := newTeamLeadFixture(t, plan)
	f.factory = &recordingFactory{child: alwaysDoneChild{res: worker.RunResult{Success: true}}}
	f.enqueueParentTask(t, "task-fin-err", "x")
	stl := &stlGetSometimesFails{
		realFake: f.stl,

		parentID: queue.TaskID("task-fin-err"),
		failOn:   2,
	}
	tl, _ := worker.NewTeamLead(worker.TeamLeadOptions{
		Spec: f.spec, WorktreePath: "/tmp/wt-tl-fge",
		SubprocessManager: f.manager, SharedTaskList: stl,
		CheckpointQueue: f.cpq, FixPromptQueue: f.fpq,
		DoctrineConfig:     f.cfg,
		ToolRelay:          worker.NewUnavailableRelay(),
		ChildWorkerFactory: f.factory,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := tl.Run(ctx, worker.RunRequest{TaskID: "task-fin-err", Prompt: "x"})

	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Success {
		t.Errorf("Success=false: %s", res.FailureReason)
	}
}

func TestTeamLeadFinalizeParentClaimsPendingRow(t *testing.T) {
	plan := `{"plan":[]}`
	_ = plan

	plan = `{"plan":[{"id":"sub-1","prompt":"x"}]}`
	f := newTeamLeadFixture(t, plan)
	f.factory = &recordingFactory{child: alwaysDoneChild{res: worker.RunResult{Success: true}}}
	f.enqueueParentTask(t, "task-fin-claim", "x")

	stl := &stlClaimNoop{realFake: f.stl, target: queue.TaskID("task-fin-claim")}
	tl, _ := worker.NewTeamLead(worker.TeamLeadOptions{
		Spec: f.spec, WorktreePath: "/tmp/wt-tl-fcp",
		SubprocessManager: f.manager, SharedTaskList: stl,
		CheckpointQueue: f.cpq, FixPromptQueue: f.fpq,
		DoctrineConfig:     f.cfg,
		ToolRelay:          worker.NewUnavailableRelay(),
		ChildWorkerFactory: f.factory,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = tl.Run(ctx, worker.RunRequest{TaskID: "task-fin-claim", Prompt: "x"})

}

func TestTeamLeadMarkParentFailedGetError(t *testing.T) {
	plan := `not json`
	f := newTeamLeadFixture(t, plan)
	f.factory = &recordingFactory{child: alwaysDoneChild{}}
	f.enqueueParentTask(t, "task-mpf-err", "x")

	stl := &stlGetSometimesFails{
		realFake: f.stl,
		parentID: queue.TaskID("task-mpf-err"),
		failOn:   1,
	}
	tl, _ := worker.NewTeamLead(worker.TeamLeadOptions{
		Spec: f.spec, WorktreePath: "/tmp/wt-tl-mpf",
		SubprocessManager: f.manager, SharedTaskList: stl,
		CheckpointQueue: f.cpq, FixPromptQueue: f.fpq,
		DoctrineConfig:     f.cfg,
		ToolRelay:          worker.NewUnavailableRelay(),
		ChildWorkerFactory: f.factory,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := tl.Run(ctx, worker.RunRequest{TaskID: "task-mpf-err", Prompt: "x"})
	if err == nil {
		t.Fatal("expected plan parse error to propagate")
	}
}

func TestParsePlanDoubleBlockUsesOuter(t *testing.T) {

	plan := `{"plan":[{"id":"sub-1","prompt":"first"}]} {"plan":[{"id":"sneaky","prompt":"override"}]}`
	f := newTeamLeadFixture(t, plan)
	f.factory = &recordingFactory{child: alwaysDoneChild{res: worker.RunResult{Success: true}}}
	tl := f.newTeamLead(t)
	f.enqueueParentTask(t, "task-double-block", "x")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := tl.Run(ctx, worker.RunRequest{TaskID: "task-double-block", Prompt: "x"})
	if err == nil {
		t.Fatal("expected double-block plan JSON to fail unmarshal (outer-span semantic)")
	}
	if !strings.Contains(err.Error(), "plan JSON unmarshal") {
		t.Errorf("err = %v, want substring 'plan JSON unmarshal' (fail-loud on doctrine drift)", err)
	}
}

type reasonedFailChild struct{ reason string }

func (c reasonedFailChild) Run(ctx context.Context, req worker.RunRequest) (worker.RunResult, error) {
	return worker.RunResult{Success: false, FailureReason: c.reason}, errors.New(c.reason)
}

type reasonedFailFactory struct {
	mu      sync.Mutex
	reasons []string
	calls   int
}

func (f *reasonedFailFactory) NewChild(parentTaskID queue.TaskID, spec worker.WorkerSpec, worktreePath string) (worker.Worker, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r := f.reasons[f.calls%len(f.reasons)]
	f.calls++
	return reasonedFailChild{reason: r}, nil
}

func TestTeamLeadDispatchSurfacesAllChildErrors(t *testing.T) {
	plan := `{"plan":[{"id":"sub-1","prompt":"first"},{"id":"sub-2","prompt":"second"},{"id":"sub-3","prompt":"third"}]}`
	f := newTeamLeadFixture(t, plan)
	f.factory = &reasonedFailFactory{
		reasons: []string{"alpha-error", "beta-error", "gamma-error"},
	}
	tl := f.newTeamLead(t)
	f.enqueueParentTask(t, "task-multi-fail", "x")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := tl.Run(ctx, worker.RunRequest{TaskID: "task-multi-fail", Prompt: "x"})
	if err == nil {
		t.Fatal("expected aggregated child errors")
	}
	// All three reason strings MUST appear in the joined error message
	// (errors.Join concatenates with newlines).
	msg := err.Error()
	for _, want := range []string{"alpha-error", "beta-error", "gamma-error"} {
		if !strings.Contains(msg, want) {
			t.Errorf("err = %q, missing substring %q (errors.Join must surface every child failure)", msg, want)
		}
	}

	all := f.fpq.snapshot()
	if len(all) != 3 {
		t.Errorf("FixPromptQueue rows = %d, want 3 (one per failed child)", len(all))
	}
}

type cancellingFailChild struct {
	cancel context.CancelFunc
	reason string
}

func (c cancellingFailChild) Run(ctx context.Context, req worker.RunRequest) (worker.RunResult, error) {

	if c.cancel != nil {
		c.cancel()
	}
	return worker.RunResult{Success: false, FailureReason: c.reason}, errors.New(c.reason)
}

func TestTeamLeadFixPromptSurvivesParentCancellation(t *testing.T) {
	plan := `{"plan":[{"id":"sub-1","prompt":"will fail and cancel"}]}`
	f := newTeamLeadFixture(t, plan)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.factory = &recordingFactory{child: cancellingFailChild{
		cancel: cancel,
		reason: "child failed after cancelling parent",
	}}
	tl := f.newTeamLead(t)
	f.enqueueParentTask(t, "task-cancel-fp", "x")
	res, _ := tl.Run(ctx, worker.RunRequest{TaskID: "task-cancel-fp", Prompt: "x"})
	if res.Success {
		t.Error("Success=true; want false (child failed)")
	}

	all := f.fpq.snapshot()
	if len(all) != 1 {
		t.Fatalf("FixPromptQueue snapshot size = %d, want 1 (bgCtx must isolate write from caller cancellation)", len(all))
	}
	got := all[0]
	if got.WorkerID != f.spec.ID {
		t.Errorf("FixPrompt.WorkerID = %q, want %q (TeamLead's own spec)", got.WorkerID, f.spec.ID)
	}
	if got.ReviewerTier != queue.ReviewerTierL2 {
		t.Errorf("FixPrompt.ReviewerTier = %v, want L2", got.ReviewerTier)
	}
	if !strings.Contains(got.PromptText, "child failed after cancelling parent") {
		t.Errorf("FixPrompt.PromptText = %q, want substring 'child failed after cancelling parent'", got.PromptText)
	}
}

func TestTeamLeadPlannerPropagatesError(t *testing.T) {

	plannerInbound := []subprocess.Message{
		{Kind: subprocess.MessageKindError, ID: "p-1", ErrCode: 500, ErrMsg: "planner exploded"},
	}
	session := newFakeSession("tid-tl-err", plannerInbound...)
	spec, _ := worker.NewSpec(worker.SpecOptions{
		ID:             "team-lead-perr",
		Variant:        worker.VariantTeamLead,
		TaskTier:       worker.TierComplex,
		ModelClass:     "tier-teamlead",
		Tools:          []string{"research_dispatch"},
		Quota:          worker.Quota{MaxTokens: 100000, MaxCostUSD: 5.0, MaxDuration: 30 * time.Minute},
		RecoveryPolicy: worker.RecoveryDoctrineBound,
		DoctrineName:   "max-scope",
		ProjectID:      "internal-platform-x",
	})
	mgr := newFakePersistentManager(session)
	stl := newFakeSharedTaskList()
	cpq := newFakeCheckpointQueue()
	fpq := newFakeFixPromptQueue()
	tl, err := worker.NewTeamLead(worker.TeamLeadOptions{
		Spec:               spec,
		WorktreePath:       "/tmp/wt-tl-perr",
		SubprocessManager:  mgr,
		SharedTaskList:     stl,
		CheckpointQueue:    cpq,
		FixPromptQueue:     fpq,
		DoctrineConfig:     fakeDoctrineConfig("", 30*time.Second),
		ToolRelay:          worker.NewUnavailableRelay(),
		ChildWorkerFactory: &recordingFactory{child: alwaysDoneChild{}},
	})
	if err != nil {
		t.Fatalf("NewTeamLead: %v", err)
	}
	_ = stl.Enqueue(context.Background(), queue.TaskRow{
		TaskID: "task-perr", ProjectID: spec.ProjectID, Status: queue.StatusPending,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = tl.Run(ctx, worker.RunRequest{TaskID: "task-perr", Prompt: "x"})
	if err == nil {
		t.Fatal("expected planner error to propagate")
	}
	if !strings.Contains(err.Error(), "planner exploded") {
		t.Errorf("err = %v, want substring 'planner exploded'", err)
	}
}
