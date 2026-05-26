package worker_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/workforce/queue"
	"github.com/cbip-solutions/hades-system/internal/workforce/subprocess"
	"github.com/cbip-solutions/hades-system/internal/workforce/worker"
)

type fixture struct {
	spec    worker.WorkerSpec
	stl     *fakeSharedTaskList
	cpq     *fakeCheckpointQueue
	fpq     *fakeFixPromptQueue
	session *fakeSession
	cfg     worker.DoctrineConfig
}

func newFixture(t *testing.T, inbound ...subprocess.Message) *fixture {
	t.Helper()
	spec, err := worker.NewSpec(worker.SpecOptions{
		ID:             "spec-medium-1",
		Variant:        worker.VariantWorker,
		TaskTier:       worker.TierMedium,
		ModelClass:     "tier-medium",
		Tools:          []string{"research_dispatch"},
		Quota:          worker.Quota{MaxTokens: 100000, MaxCostUSD: 10.0, MaxDuration: 5 * time.Minute},
		RecoveryPolicy: worker.RecoveryAutoRespawn,
		DoctrineName:   "max-scope",
		ProjectID:      "internal-platform-x",
	})
	if err != nil {
		t.Fatalf("NewSpec: %v", err)
	}
	return &fixture{
		spec:    spec,
		stl:     newFakeSharedTaskList(),
		cpq:     newFakeCheckpointQueue(),
		fpq:     newFakeFixPromptQueue(),
		session: newFakeSession("tid-test", inbound...),
		cfg:     fakeDoctrineConfig("", 30*time.Second),
	}
}

func (f *fixture) newWorker(t *testing.T) *worker.OpenClaudeWorker {
	t.Helper()
	return worker.NewOpenClaudeWorker(worker.OpenClaudeWorkerOptions{
		Spec:            f.spec,
		WorktreePath:    "/tmp/wt-test",
		Session:         f.session,
		SharedTaskList:  f.stl,
		CheckpointQueue: f.cpq,
		FixPromptQueue:  f.fpq,
		DoctrineConfig:  f.cfg,
		ToolRelay:       worker.NewUnavailableRelay(),
	})
}

func (f *fixture) enqueueTask(t *testing.T, taskID, prompt string) {
	t.Helper()
	err := f.stl.Enqueue(context.Background(), queue.TaskRow{
		TaskID:      queue.TaskID(taskID),
		ProjectID:   f.spec.ProjectID,
		Title:       "test",
		Description: prompt,
		Status:      queue.StatusPending,
	})
	if err != nil {
		t.Fatalf("enqueueTask: %v", err)
	}
}

func doneMsg() subprocess.Message {
	return subprocess.Message{
		Kind:    subprocess.MessageKindResult,
		ID:      "done-1",
		Method:  "done",
		Payload: json.RawMessage(`{"stop_reason":"end_turn","input_tokens":10,"output_tokens":20}`),
	}
}

func TestNewOpenClaudeWorkerPanicsOnNilWorktree(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("inv-zen-087: NewOpenClaudeWorker did not panic on empty worktreePath")
		}
		err, ok := r.(error)
		if !ok {
			t.Fatalf("recovered non-error panic: %T %v", r, r)
		}
		if !errors.Is(err, worker.ErrNilWorktreePath) {
			t.Fatalf("recovered err = %v, want errors.Is(ErrNilWorktreePath)", err)
		}
	}()

	f := newFixture(t)
	_ = worker.NewOpenClaudeWorker(worker.OpenClaudeWorkerOptions{
		Spec:            f.spec,
		WorktreePath:    "",
		Session:         f.session,
		SharedTaskList:  f.stl,
		CheckpointQueue: f.cpq,
		FixPromptQueue:  f.fpq,
		DoctrineConfig:  f.cfg,
		ToolRelay:       worker.NewUnavailableRelay(),
	})
	t.Fatal("unreachable: NewOpenClaudeWorker must panic before this line")
}

func TestNewOpenClaudeWorkerPanicsOnWhitespaceWorktree(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on whitespace-only worktreePath")
		}
		err, _ := r.(error)
		if !errors.Is(err, worker.ErrNilWorktreePath) {
			t.Fatalf("err = %v, want ErrNilWorktreePath", err)
		}
	}()
	f := newFixture(t)
	_ = worker.NewOpenClaudeWorker(worker.OpenClaudeWorkerOptions{
		Spec: f.spec, WorktreePath: "   ", Session: f.session,
		SharedTaskList: f.stl, CheckpointQueue: f.cpq, FixPromptQueue: f.fpq,
		DoctrineConfig: f.cfg, ToolRelay: worker.NewUnavailableRelay(),
	})
	t.Fatal("unreachable")
}

func TestNewOpenClaudeWorkerPanicsOnNilSession(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on nil Session")
		}
		err, _ := r.(error)
		if !errors.Is(err, worker.ErrNilSession) {
			t.Fatalf("err = %v, want ErrNilSession", err)
		}
	}()
	f := newFixture(t)
	_ = worker.NewOpenClaudeWorker(worker.OpenClaudeWorkerOptions{
		Spec: f.spec, WorktreePath: "/tmp/wt", Session: nil,
		SharedTaskList: f.stl, CheckpointQueue: f.cpq, FixPromptQueue: f.fpq,
		DoctrineConfig: f.cfg, ToolRelay: worker.NewUnavailableRelay(),
	})
	t.Fatal("unreachable")
}

func TestNewOpenClaudeWorkerPanicsOnNilQueues(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(o *worker.OpenClaudeWorkerOptions)
	}{
		{"nil_stl", func(o *worker.OpenClaudeWorkerOptions) { o.SharedTaskList = nil }},
		{"nil_cpq", func(o *worker.OpenClaudeWorkerOptions) { o.CheckpointQueue = nil }},
		{"nil_fpq", func(o *worker.OpenClaudeWorkerOptions) { o.FixPromptQueue = nil }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatal("expected panic")
				}
				err, _ := r.(error)
				if !errors.Is(err, worker.ErrNilQueues) {
					t.Fatalf("err = %v, want ErrNilQueues", err)
				}
			}()
			f := newFixture(t)
			opts := worker.OpenClaudeWorkerOptions{
				Spec: f.spec, WorktreePath: "/tmp/wt", Session: f.session,
				SharedTaskList: f.stl, CheckpointQueue: f.cpq, FixPromptQueue: f.fpq,
				DoctrineConfig: f.cfg, ToolRelay: worker.NewUnavailableRelay(),
			}
			c.mutate(&opts)
			_ = worker.NewOpenClaudeWorker(opts)
			t.Fatal("unreachable")
		})
	}
}

func TestNewOpenClaudeWorkerPanicsOnNilDoctrineConfig(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic")
		}
		err, _ := r.(error)
		if !errors.Is(err, worker.ErrNilDoctrineConfig) {
			t.Fatalf("err = %v, want ErrNilDoctrineConfig", err)
		}
	}()
	f := newFixture(t)
	_ = worker.NewOpenClaudeWorker(worker.OpenClaudeWorkerOptions{
		Spec: f.spec, WorktreePath: "/tmp/wt", Session: f.session,
		SharedTaskList: f.stl, CheckpointQueue: f.cpq, FixPromptQueue: f.fpq,
		DoctrineConfig: nil, ToolRelay: worker.NewUnavailableRelay(),
	})
	t.Fatal("unreachable")
}

func TestNewOpenClaudeWorkerPanicsOnNilToolRelay(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on nil ToolRelay")
		}
		err, _ := r.(error)
		if !errors.Is(err, worker.ErrNilToolRelay) {
			t.Fatalf("err = %v, want ErrNilToolRelay", err)
		}
	}()
	f := newFixture(t)
	_ = worker.NewOpenClaudeWorker(worker.OpenClaudeWorkerOptions{
		Spec: f.spec, WorktreePath: "/tmp/wt", Session: f.session,
		SharedTaskList: f.stl, CheckpointQueue: f.cpq, FixPromptQueue: f.fpq,
		DoctrineConfig: f.cfg, ToolRelay: nil,
	})
	t.Fatal("unreachable")
}

func TestNewOpenClaudeWorkerPanicsOnInvalidSpec(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on invalid spec")
		}
	}()
	f := newFixture(t)
	bad := f.spec
	bad.ID = ""
	_ = worker.NewOpenClaudeWorker(worker.OpenClaudeWorkerOptions{
		Spec: bad, WorktreePath: "/tmp/wt", Session: f.session,
		SharedTaskList: f.stl, CheckpointQueue: f.cpq, FixPromptQueue: f.fpq,
		DoctrineConfig: f.cfg, ToolRelay: worker.NewUnavailableRelay(),
	})
	t.Fatal("unreachable")
}

func TestWorkerInterfaceSatisfied(t *testing.T) {
	var _ worker.Worker = (*worker.OpenClaudeWorker)(nil)
}

func TestRunHappyPath(t *testing.T) {
	f := newFixture(t, doneMsg())
	f.enqueueTask(t, "task-1", "implement feature X")
	w := f.newWorker(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := w.Run(ctx, worker.RunRequest{TaskID: "task-1", Prompt: "implement feature X"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Success {
		t.Errorf("Success=false, FailureReason=%q", res.FailureReason)
	}
	if res.FinalStopReason != "end_turn" {
		t.Errorf("FinalStopReason = %q, want end_turn", res.FinalStopReason)
	}
	if res.TokensUsed != 30 {
		t.Errorf("TokensUsed = %d, want 30", res.TokensUsed)
	}

	row, _ := f.stl.Get(context.Background(), queue.TaskID("task-1"))
	if row.Status != queue.StatusReview {
		t.Errorf("row.Status = %q, want review (Plan 4 worker advances to review; reviewers advance to done)", row.Status)
	}
}

func TestRunDoctrineReinforcementInjected(t *testing.T) {
	f := newFixture(t, doneMsg())
	f.enqueueTask(t, "task-doctrine", "do X")
	w := f.newWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := w.Run(ctx, worker.RunRequest{TaskID: "task-doctrine", Prompt: "do X"}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	sent := f.session.sentSnapshot()
	if len(sent) == 0 {
		t.Fatal("no messages sent")
	}
	first := sent[0]
	if first.Kind != subprocess.MessageKindRequest {
		t.Errorf("first.Kind = %v, want Request", first.Kind)
	}
	if first.Method != "prompt" {
		t.Errorf("first.Method = %q, want prompt", first.Method)
	}

	var body struct {
		Prompt string `json:"prompt"`
	}
	if err := json.Unmarshal(first.Payload, &body); err != nil {
		t.Fatalf("payload not JSON: %v", err)
	}
	if !strings.Contains(body.Prompt, "[doctrine: max-scope]") {
		t.Errorf("prompt missing doctrine marker: %q", body.Prompt)
	}
	if !strings.Contains(body.Prompt, "do X") {
		t.Errorf("prompt missing user instruction: %q", body.Prompt)
	}
}

func TestRunWithConfiguredTemplate(t *testing.T) {
	f := newFixture(t, doneMsg())
	f.cfg = fakeDoctrineConfig("MAX-SCOPE-FULL-CONTENT-HERE", 30*time.Second)
	f.enqueueTask(t, "task-template", "do Y")
	w := f.newWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := w.Run(ctx, worker.RunRequest{TaskID: "task-template", Prompt: "do Y"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	sent := f.session.sentSnapshot()
	var body struct {
		Prompt string `json:"prompt"`
	}
	_ = json.Unmarshal(sent[0].Payload, &body)
	if !strings.Contains(body.Prompt, "MAX-SCOPE-FULL-CONTENT-HERE") {
		t.Errorf("prompt missing rendered template: %q", body.Prompt)
	}
	if !strings.Contains(body.Prompt, "[doctrine: max-scope]") {
		t.Errorf("prompt missing audit marker even with rendered template: %q", body.Prompt)
	}
}

func TestRunRejectsInvalidRequest(t *testing.T) {
	f := newFixture(t)
	w := f.newWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := w.Run(ctx, worker.RunRequest{TaskID: "", Prompt: "x"}); err == nil {
		t.Error("expected error for empty TaskID")
	}
	if _, err := w.Run(ctx, worker.RunRequest{TaskID: "x", Prompt: ""}); err == nil {
		t.Error("expected error for empty Prompt")
	}
}

func TestRunTaskNotFound(t *testing.T) {
	f := newFixture(t)
	w := f.newWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := w.Run(ctx, worker.RunRequest{TaskID: "nonexistent", Prompt: "x"})
	if !errors.Is(err, worker.ErrTaskNotFound) {
		t.Fatalf("err = %v, want ErrTaskNotFound", err)
	}
}

func TestRunRejectsAlreadyClaimedTask(t *testing.T) {
	f := newFixture(t)

	_ = f.stl.Enqueue(context.Background(), queue.TaskRow{
		TaskID: queue.TaskID("task-claimed"), ProjectID: f.spec.ProjectID,
		Status: queue.StatusInProgress, ThreadID: "other-thread",
	})
	w := f.newWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := w.Run(ctx, worker.RunRequest{TaskID: "task-claimed", Prompt: "x"})
	if !errors.Is(err, worker.ErrTaskAlreadyClaimed) {
		t.Fatalf("err = %v, want ErrTaskAlreadyClaimed", err)
	}
}

func TestRunPropagatesClaimError(t *testing.T) {
	f := newFixture(t)
	f.enqueueTask(t, "task-claim-err", "x")
	f.stl.setNextClaimErr(errors.New("disk down"))
	w := f.newWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := w.Run(ctx, worker.RunRequest{TaskID: "task-claim-err", Prompt: "x"})
	if err == nil {
		t.Fatal("expected error on Claim failure")
	}
	if !strings.Contains(err.Error(), "disk down") {
		t.Errorf("err = %v, want substring 'disk down'", err)
	}
}

func TestRunHonoursCtxCancel(t *testing.T) {
	f := newFixture(t)
	f.session.setRecvBlock(true)
	f.enqueueTask(t, "task-cancel", "do X")
	w := f.newWorker(t)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	res, err := w.Run(ctx, worker.RunRequest{TaskID: "task-cancel", Prompt: "do X"})
	if err == nil {
		t.Fatal("expected error on ctx cancel")
	}
	if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want context.Canceled or DeadlineExceeded", err)
	}
	if res.Success {
		t.Error("Success must be false on cancel")
	}
	row, _ := f.stl.Get(context.Background(), queue.TaskID("task-cancel"))
	if row.Status != queue.StatusFailed {
		t.Errorf("row.Status = %q, want failed", row.Status)
	}
}

func TestRunQuotaExceeded(t *testing.T) {
	f := newFixture(t)

	f.spec.Quota.MaxTokens = 5

	f.session = newFakeSession("tid-test",
		subprocess.Message{
			Kind:    subprocess.MessageKindNotification,
			Method:  "token_usage",
			Payload: json.RawMessage(`{"input_tokens":50,"output_tokens":50}`),
		},
		doneMsg(),
	)
	f.enqueueTask(t, "task-quota", "x")
	w := f.newWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := w.Run(ctx, worker.RunRequest{TaskID: "task-quota", Prompt: "x"})
	if !errors.Is(err, worker.ErrQuotaExceeded) {
		t.Fatalf("err = %v, want ErrQuotaExceeded", err)
	}
	row, _ := f.stl.Get(context.Background(), queue.TaskID("task-quota"))
	if row.Status != queue.StatusFailed {
		t.Errorf("row.Status = %q, want failed", row.Status)
	}
}

func TestRunSessionErrorMarksFailed(t *testing.T) {
	f := newFixture(t,
		subprocess.Message{
			Kind:    subprocess.MessageKindError,
			ID:      "err-1",
			ErrCode: 500,
			ErrMsg:  "subprocess panic",
		},
	)
	f.enqueueTask(t, "task-err", "x")
	w := f.newWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := w.Run(ctx, worker.RunRequest{TaskID: "task-err", Prompt: "x"})
	if err == nil {
		t.Fatal("expected error from session Error frame")
	}
	if !strings.Contains(err.Error(), "subprocess panic") {
		t.Errorf("err = %v, want substring 'subprocess panic'", err)
	}
	row, _ := f.stl.Get(context.Background(), queue.TaskID("task-err"))
	if row.Status != queue.StatusFailed {
		t.Errorf("row.Status = %q, want failed", row.Status)
	}
}

func TestRunSessionClosedReturnsError(t *testing.T) {

	f := newFixture(t)
	f.enqueueTask(t, "task-closed", "x")
	w := f.newWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := w.Run(ctx, worker.RunRequest{TaskID: "task-closed", Prompt: "x"})
	if err == nil {
		t.Fatal("expected error on premature ErrSessionClosed")
	}
	row, _ := f.stl.Get(context.Background(), queue.TaskID("task-closed"))
	if row.Status != queue.StatusFailed {
		t.Errorf("row.Status = %q, want failed", row.Status)
	}
}

func TestRunToolUseRouted(t *testing.T) {
	f := newFixture(t,
		subprocess.Message{
			Kind:    subprocess.MessageKindRequest,
			ID:      "tu-1",
			Method:  "tool_use",
			Payload: json.RawMessage(`{"name":"research_dispatch","input":{"q":"go MCP"}}`),
		},
		doneMsg(),
	)
	f.enqueueTask(t, "task-tu", "research X")
	relay := newFakeToolRelay(`{"results":[{"url":"https://example.com"}]}`, nil)
	w := worker.NewOpenClaudeWorker(worker.OpenClaudeWorkerOptions{
		Spec:            f.spec,
		WorktreePath:    "/tmp/wt-tu",
		Session:         f.session,
		SharedTaskList:  f.stl,
		CheckpointQueue: f.cpq,
		FixPromptQueue:  f.fpq,
		DoctrineConfig:  f.cfg,
		ToolRelay:       relay,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := w.Run(ctx, worker.RunRequest{TaskID: "task-tu", Prompt: "research X"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Success {
		t.Errorf("Success=false: %s", res.FailureReason)
	}
	if res.ToolUseCount != 1 {
		t.Errorf("ToolUseCount = %d, want 1", res.ToolUseCount)
	}
	if relay.callCount() != 1 {
		t.Errorf("relay calls = %d, want 1", relay.callCount())
	}

	sent := f.session.sentSnapshot()
	var found bool
	for _, m := range sent {
		if m.Kind == subprocess.MessageKindResult && m.ID == "tu-1" {
			if !strings.Contains(string(m.Payload), "example.com") {
				t.Errorf("Result payload missing relay output: %s", m.Payload)
			}
			found = true
		}
	}
	if !found {
		t.Error("worker did not send Result frame for tool_use tu-1")
	}
}

func TestRunToolNotInWhitelist(t *testing.T) {
	f := newFixture(t,
		subprocess.Message{
			Kind:    subprocess.MessageKindRequest,
			ID:      "tu-deny",
			Method:  "tool_use",
			Payload: json.RawMessage(`{"name":"ssh_exec","input":{"host":"vps","cmd":"id"}}`),
		},
		doneMsg(),
	)
	f.enqueueTask(t, "task-deny", "x")
	relay := newFakeToolRelay(`{}`, nil)
	w := worker.NewOpenClaudeWorker(worker.OpenClaudeWorkerOptions{
		Spec:            f.spec,
		WorktreePath:    "/tmp/wt-deny",
		Session:         f.session,
		SharedTaskList:  f.stl,
		CheckpointQueue: f.cpq,
		FixPromptQueue:  f.fpq,
		DoctrineConfig:  f.cfg,
		ToolRelay:       relay,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := w.Run(ctx, worker.RunRequest{TaskID: "task-deny", Prompt: "x"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if relay.callCount() != 0 {
		t.Errorf("relay must NOT be called for non-whitelisted tool, got %d", relay.callCount())
	}

	sent := f.session.sentSnapshot()
	var found bool
	for _, m := range sent {
		if m.Kind == subprocess.MessageKindError && m.ID == "tu-deny" {
			if !strings.Contains(m.ErrMsg, "tool_not_in_whitelist") {
				t.Errorf("Error.ErrMsg = %q, want substring 'tool_not_in_whitelist'", m.ErrMsg)
			}
			found = true
		}
	}
	if !found {
		t.Error("worker did not send Error frame for non-whitelisted tool_use")
	}
}

func TestRunToolRelayError(t *testing.T) {
	f := newFixture(t,
		subprocess.Message{
			Kind:    subprocess.MessageKindRequest,
			ID:      "tu-err",
			Method:  "tool_use",
			Payload: json.RawMessage(`{"name":"research_dispatch","input":{}}`),
		},
		doneMsg(),
	)
	f.enqueueTask(t, "task-relay-err", "x")
	relay := newFakeToolRelay("", errors.New("backend timeout"))
	w := worker.NewOpenClaudeWorker(worker.OpenClaudeWorkerOptions{
		Spec:            f.spec,
		WorktreePath:    "/tmp/wt-rerr",
		Session:         f.session,
		SharedTaskList:  f.stl,
		CheckpointQueue: f.cpq,
		FixPromptQueue:  f.fpq,
		DoctrineConfig:  f.cfg,
		ToolRelay:       relay,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := w.Run(ctx, worker.RunRequest{TaskID: "task-relay-err", Prompt: "x"})
	if err != nil {
		t.Fatalf("relay error must NOT propagate; got %v", err)
	}
	if !res.Success {
		t.Errorf("Success=false: %s", res.FailureReason)
	}

	sent := f.session.sentSnapshot()
	var found bool
	for _, m := range sent {
		if m.Kind == subprocess.MessageKindError && m.ID == "tu-err" {
			if !strings.Contains(m.ErrMsg, "backend timeout") {
				t.Errorf("Error.ErrMsg = %q, want substring 'backend timeout'", m.ErrMsg)
			}
			found = true
		}
	}
	if !found {
		t.Error("worker did not forward relay error as Error frame")
	}
}

func TestRunMalformedToolUsePayload(t *testing.T) {
	f := newFixture(t,
		subprocess.Message{
			Kind:    subprocess.MessageKindRequest,
			ID:      "tu-bad",
			Method:  "tool_use",
			Payload: json.RawMessage(`not json`),
		},
		doneMsg(),
	)
	f.enqueueTask(t, "task-bad-payload", "x")
	w := f.newWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := w.Run(ctx, worker.RunRequest{TaskID: "task-bad-payload", Prompt: "x"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	sent := f.session.sentSnapshot()
	var found bool
	for _, m := range sent {
		if m.Kind == subprocess.MessageKindError && m.ID == "tu-bad" {
			found = true
		}
	}
	if !found {
		t.Error("worker did not send Error frame for malformed tool_use payload")
	}
}

func TestRunUnknownToolFamily(t *testing.T) {
	f := newFixture(t,
		subprocess.Message{
			Kind:    subprocess.MessageKindRequest,
			ID:      "tu-unk",
			Method:  "tool_use",
			Payload: json.RawMessage(`{"name":"weather_forecast","input":{"city":"Madrid"}}`),
		},
		doneMsg(),
	)
	f.spec.Tools = append(f.spec.Tools, "weather_forecast")

	if err := f.spec.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	f.enqueueTask(t, "task-unk", "x")
	w := f.newWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := w.Run(ctx, worker.RunRequest{TaskID: "task-unk", Prompt: "x"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	sent := f.session.sentSnapshot()
	var found bool
	for _, m := range sent {
		if m.Kind == subprocess.MessageKindError && m.ID == "tu-unk" {
			if !strings.Contains(m.ErrMsg, "unknown_tool_family") {
				t.Errorf("ErrMsg = %q, want substring 'unknown_tool_family'", m.ErrMsg)
			}
			found = true
		}
	}
	if !found {
		t.Error("worker did not send Error frame for unknown tool family")
	}
}

func TestRunRoutesSshExec(t *testing.T) {
	f := newFixture(t,
		subprocess.Message{
			Kind:    subprocess.MessageKindRequest,
			ID:      "tu-ssh",
			Method:  "tool_use",
			Payload: json.RawMessage(`{"name":"ssh_exec","input":{"host":"vps","cmd":"id"}}`),
		},
		doneMsg(),
	)
	f.spec.Tools = append(f.spec.Tools, "ssh_exec")
	f.enqueueTask(t, "task-ssh", "x")
	relay := newFakeToolRelay(`{"stdout":"uid=0"}`, nil)
	w := worker.NewOpenClaudeWorker(worker.OpenClaudeWorkerOptions{
		Spec:            f.spec,
		WorktreePath:    "/tmp/wt-ssh",
		Session:         f.session,
		SharedTaskList:  f.stl,
		CheckpointQueue: f.cpq,
		FixPromptQueue:  f.fpq,
		DoctrineConfig:  f.cfg,
		ToolRelay:       relay,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := w.Run(ctx, worker.RunRequest{TaskID: "task-ssh", Prompt: "x"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if relay.callCount() != 1 {
		t.Errorf("relay calls = %d, want 1", relay.callCount())
	}
}

func TestRunRoutesAuditReview(t *testing.T) {
	f := newFixture(t,
		subprocess.Message{
			Kind:    subprocess.MessageKindRequest,
			ID:      "tu-audit",
			Method:  "tool_use",
			Payload: json.RawMessage(`{"name":"audit_review","input":{"task_id":"x"}}`),
		},
		doneMsg(),
	)
	f.spec.Tools = append(f.spec.Tools, "audit_review")
	f.enqueueTask(t, "task-audit", "x")
	relay := newFakeToolRelay(`{"verdict":"clean"}`, nil)
	w := worker.NewOpenClaudeWorker(worker.OpenClaudeWorkerOptions{
		Spec: f.spec, WorktreePath: "/tmp/wt-audit", Session: f.session,
		SharedTaskList: f.stl, CheckpointQueue: f.cpq, FixPromptQueue: f.fpq,
		DoctrineConfig: f.cfg, ToolRelay: relay,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := w.Run(ctx, worker.RunRequest{TaskID: "task-audit", Prompt: "x"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if relay.callCount() != 1 {
		t.Errorf("relay calls = %d, want 1", relay.callCount())
	}
}

func TestRunRoutesBudget(t *testing.T) {
	f := newFixture(t,
		subprocess.Message{
			Kind:    subprocess.MessageKindRequest,
			ID:      "tu-budget",
			Method:  "tool_use",
			Payload: json.RawMessage(`{"name":"budget_pre_call","input":{}}`),
		},
		doneMsg(),
	)
	f.spec.Tools = append(f.spec.Tools, "budget_pre_call")
	f.enqueueTask(t, "task-budget", "x")
	relay := newFakeToolRelay(`{"allowed":true}`, nil)
	w := worker.NewOpenClaudeWorker(worker.OpenClaudeWorkerOptions{
		Spec: f.spec, WorktreePath: "/tmp/wt-budget", Session: f.session,
		SharedTaskList: f.stl, CheckpointQueue: f.cpq, FixPromptQueue: f.fpq,
		DoctrineConfig: f.cfg, ToolRelay: relay,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := w.Run(ctx, worker.RunRequest{TaskID: "task-budget", Prompt: "x"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if relay.callCount() != 1 {
		t.Errorf("relay calls = %d, want 1", relay.callCount())
	}
}

func TestRunCheckpointEmitted(t *testing.T) {
	beforeRun := time.Now().UTC()
	f := newFixture(t,
		subprocess.Message{
			Kind:    subprocess.MessageKindNotification,
			Method:  "checkpoint",
			Payload: json.RawMessage(`{"state":"red","seq":1}`),
		},
		doneMsg(),
	)
	f.enqueueTask(t, "task-cp", "tdd")
	w := f.newWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := w.Run(ctx, worker.RunRequest{TaskID: "task-cp", Prompt: "tdd"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.CheckpointIDs) != 1 {
		t.Fatalf("CheckpointIDs = %d, want 1", len(res.CheckpointIDs))
	}

	cps := f.cpq.snapshot()
	if len(cps) != 1 {
		t.Fatalf("checkpoint rows = %d, want 1", len(cps))
	}
	cp := cps[0]
	if cp.TaskID != queue.TaskID("task-cp") {
		t.Errorf("cp.TaskID = %q", cp.TaskID)
	}
	if cp.ThreadID != "tid-test" {
		t.Errorf("cp.ThreadID = %q, want tid-test (Session.ThreadID)", cp.ThreadID)
	}
	if cp.ProjectID != f.spec.ProjectID {
		t.Errorf("cp.ProjectID = %q, want %q", cp.ProjectID, f.spec.ProjectID)
	}
	if cp.DeadlineAt.IsZero() {
		t.Fatal("inv-zen-050: cp.DeadlineAt is zero; must be deadline-stamped")
	}
	if !cp.DeadlineAt.After(beforeRun) {
		t.Errorf("cp.DeadlineAt %v must be after run start %v", cp.DeadlineAt, beforeRun)
	}
	wantMin := beforeRun.Add(20 * time.Second)
	wantMax := beforeRun.Add(40 * time.Second)
	if cp.DeadlineAt.Before(wantMin) || cp.DeadlineAt.After(wantMax) {
		t.Errorf("cp.DeadlineAt %v not in [%v, %v] for 30s deadline",
			cp.DeadlineAt, wantMin, wantMax)
	}
	if cp.SeqNum != 1 {
		t.Errorf("cp.SeqNum = %d, want 1", cp.SeqNum)
	}
	if string(cp.StateJSON) == "" {
		t.Error("cp.StateJSON must be populated")
	}
}

func TestRunMultipleCheckpointsOrdered(t *testing.T) {
	f := newFixture(t,
		subprocess.Message{
			Kind: subprocess.MessageKindNotification, Method: "checkpoint",
			Payload: json.RawMessage(`{"i":1}`),
		},
		subprocess.Message{
			Kind: subprocess.MessageKindNotification, Method: "checkpoint",
			Payload: json.RawMessage(`{"i":2}`),
		},
		subprocess.Message{
			Kind: subprocess.MessageKindNotification, Method: "checkpoint",
			Payload: json.RawMessage(`{"i":3}`),
		},
		doneMsg(),
	)
	f.enqueueTask(t, "task-cp-multi", "x")
	w := f.newWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := w.Run(ctx, worker.RunRequest{TaskID: "task-cp-multi", Prompt: "x"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.CheckpointIDs) != 3 {
		t.Fatalf("CheckpointIDs = %d, want 3", len(res.CheckpointIDs))
	}

	cps := f.cpq.snapshot()
	if len(cps) != 3 {
		t.Fatalf("checkpoint rows = %d, want 3", len(cps))
	}

	for i, cp := range cps {
		if cp.SeqNum != i+1 {
			t.Errorf("cps[%d].SeqNum = %d, want %d", i, cp.SeqNum, i+1)
		}
	}
}

func TestRunCheckpointPutFailureMarksFailed(t *testing.T) {
	f := newFixture(t,
		subprocess.Message{
			Kind: subprocess.MessageKindNotification, Method: "checkpoint",
			Payload: json.RawMessage(`{"x":1}`),
		},
		doneMsg(),
	)
	f.enqueueTask(t, "task-cp-fail", "x")
	f.cpq.setFailNextPut(errors.New("disk full"))
	w := f.newWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := w.Run(ctx, worker.RunRequest{TaskID: "task-cp-fail", Prompt: "x"})
	if err == nil {
		t.Fatal("expected error on CheckpointQueue.Put failure")
	}
	if !strings.Contains(err.Error(), "disk full") {
		t.Errorf("err = %v, want substring 'disk full'", err)
	}
	row, _ := f.stl.Get(context.Background(), queue.TaskID("task-cp-fail"))
	if row.Status != queue.StatusFailed {
		t.Errorf("row.Status = %q, want failed", row.Status)
	}
}

func TestRunMalformedCheckpointPayload(t *testing.T) {
	f := newFixture(t,
		subprocess.Message{
			Kind: subprocess.MessageKindNotification, Method: "checkpoint",
			Payload: json.RawMessage(`not json`),
		},
		doneMsg(),
	)
	f.enqueueTask(t, "task-cp-bad", "x")
	w := f.newWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := w.Run(ctx, worker.RunRequest{TaskID: "task-cp-bad", Prompt: "x"})
	if err == nil {
		t.Fatal("expected error on malformed checkpoint payload")
	}
	row, _ := f.stl.Get(context.Background(), queue.TaskID("task-cp-bad"))
	if row.Status != queue.StatusFailed {
		t.Errorf("row.Status = %q, want failed", row.Status)
	}
}

func TestRunPromptIdempotentReClaim(t *testing.T) {
	f := newFixture(t, doneMsg())

	_ = f.stl.Enqueue(context.Background(), queue.TaskRow{
		TaskID:    queue.TaskID("task-replay"),
		ProjectID: f.spec.ProjectID,
		Status:    queue.StatusInProgress,
		ThreadID:  string(f.session.ThreadID()),
	})
	w := f.newWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := w.Run(ctx, worker.RunRequest{TaskID: "task-replay", Prompt: "x"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Success {
		t.Errorf("Success=false: %s", res.FailureReason)
	}
}

func TestRunPropagatesGetError(t *testing.T) {
	f := newFixture(t)
	w := worker.NewOpenClaudeWorker(worker.OpenClaudeWorkerOptions{
		Spec:            f.spec,
		WorktreePath:    "/tmp/wt-get-err",
		Session:         f.session,
		SharedTaskList:  &fakeStlBadGet{err: errors.New("disk read")},
		CheckpointQueue: f.cpq,
		FixPromptQueue:  f.fpq,
		DoctrineConfig:  f.cfg,
		ToolRelay:       worker.NewUnavailableRelay(),
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := w.Run(ctx, worker.RunRequest{TaskID: "x", Prompt: "x"})
	if err == nil {
		t.Fatal("expected Get error to propagate")
	}
	if !strings.Contains(err.Error(), "disk read") {
		t.Errorf("err = %v, want substring 'disk read'", err)
	}
}

func TestRunPropagatesClaimNotFound(t *testing.T) {
	f := newFixture(t)
	f.enqueueTask(t, "task-claim-nf", "x")
	f.stl.setNextClaimErr(queue.ErrTaskNotFound)
	w := f.newWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := w.Run(ctx, worker.RunRequest{TaskID: "task-claim-nf", Prompt: "x"})
	if !errors.Is(err, worker.ErrTaskNotFound) {
		t.Fatalf("err = %v, want ErrTaskNotFound", err)
	}
}

func TestRunPropagatesClaimNotPending(t *testing.T) {
	f := newFixture(t)
	f.enqueueTask(t, "task-claim-np", "x")
	f.stl.setNextClaimErr(queue.ErrTaskNotPending)
	w := f.newWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := w.Run(ctx, worker.RunRequest{TaskID: "task-claim-np", Prompt: "x"})
	if !errors.Is(err, worker.ErrTaskAlreadyClaimed) {
		t.Fatalf("err = %v, want ErrTaskAlreadyClaimed", err)
	}
}

func TestRunFixPromptDrainError(t *testing.T) {
	f := newFixture(t)
	f.enqueueTask(t, "task-drain-err", "x")
	w := worker.NewOpenClaudeWorker(worker.OpenClaudeWorkerOptions{
		Spec:            f.spec,
		WorktreePath:    "/tmp/wt-drain-err",
		Session:         f.session,
		SharedTaskList:  f.stl,
		CheckpointQueue: f.cpq,
		FixPromptQueue:  &fakeFpqBadDrain{err: errors.New("fp drain failed")},
		DoctrineConfig:  f.cfg,
		ToolRelay:       worker.NewUnavailableRelay(),
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := w.Run(ctx, worker.RunRequest{TaskID: "task-drain-err", Prompt: "x"})
	if err == nil {
		t.Fatal("expected build prompt error to propagate")
	}
	if !strings.Contains(err.Error(), "fp drain failed") {
		t.Errorf("err = %v, want substring 'fp drain failed'", err)
	}
	row, _ := f.stl.Get(context.Background(), queue.TaskID("task-drain-err"))
	if row.Status != queue.StatusFailed {
		t.Errorf("row.Status = %q, want failed", row.Status)
	}
}

func TestRunSendPromptError(t *testing.T) {
	f := newFixture(t)
	f.enqueueTask(t, "task-send-err", "x")

	_ = f.session.Close()
	w := f.newWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := w.Run(ctx, worker.RunRequest{TaskID: "task-send-err", Prompt: "x"})
	if err == nil {
		t.Fatal("expected send prompt error")
	}
	if !errors.Is(err, subprocess.ErrSessionClosed) {
		t.Errorf("err = %v, want errors.Is(ErrSessionClosed)", err)
	}
}

func TestRunCtxCancelledBeforeReceive(t *testing.T) {
	f := newFixture(t)
	f.session.setRecvBlock(true)
	f.enqueueTask(t, "task-pre-cancel", "x")
	w := f.newWorker(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := w.Run(ctx, worker.RunRequest{TaskID: "task-pre-cancel", Prompt: "x"})
	if err == nil {
		t.Fatal("expected ctx error")
	}
	if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want ctx.Canceled or DeadlineExceeded", err)
	}
}

func TestRunMalformedResultPayload(t *testing.T) {
	f := newFixture(t,
		subprocess.Message{
			Kind:    subprocess.MessageKindResult,
			ID:      "r-bad",
			Method:  "done",
			Payload: json.RawMessage(`not json`),
		},
	)
	f.enqueueTask(t, "task-result-bad", "x")
	w := f.newWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := w.Run(ctx, worker.RunRequest{TaskID: "task-result-bad", Prompt: "x"})
	if err == nil {
		t.Fatal("expected parse result error")
	}
	if !strings.Contains(err.Error(), "parse result") {
		t.Errorf("err = %v, want substring 'parse result'", err)
	}
}

func TestRunResultEmptyPayloadIsEndTurn(t *testing.T) {
	f := newFixture(t,
		subprocess.Message{
			Kind:    subprocess.MessageKindResult,
			ID:      "r-empty",
			Method:  "done",
			Payload: json.RawMessage{},
		},
	)
	f.enqueueTask(t, "task-result-empty", "x")
	w := f.newWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := w.Run(ctx, worker.RunRequest{TaskID: "task-result-empty", Prompt: "x"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Success {
		t.Errorf("Success=false; empty Result payload should be end_turn")
	}
	if res.FinalStopReason != "end_turn" {
		t.Errorf("FinalStopReason = %q, want end_turn", res.FinalStopReason)
	}
}

func TestRunResultMaxTokensTerminalNotSuccess(t *testing.T) {
	f := newFixture(t,
		subprocess.Message{
			Kind:    subprocess.MessageKindResult,
			ID:      "r-max",
			Method:  "done",
			Payload: json.RawMessage(`{"stop_reason":"max_tokens","input_tokens":1,"output_tokens":1}`),
		},
	)
	f.enqueueTask(t, "task-result-max", "x")
	w := f.newWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := w.Run(ctx, worker.RunRequest{TaskID: "task-result-max", Prompt: "x"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Success {
		t.Error("Success=true; max_tokens should not be success")
	}
	if res.FinalStopReason != "max_tokens" {
		t.Errorf("FinalStopReason = %q, want max_tokens", res.FinalStopReason)
	}
}

func TestRunComputeCheckpointDeadlineFallback(t *testing.T) {
	f := newFixture(t,
		subprocess.Message{
			Kind: subprocess.MessageKindNotification, Method: "checkpoint",
			Payload: json.RawMessage(`{"state":{"x":1},"seq":1}`),
		},
		doneMsg(),
	)

	f.cfg = zeroDeadlineConfig{}
	beforeRun := time.Now().UTC()
	f.enqueueTask(t, "task-cp-fb", "x")
	w := f.newWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := w.Run(ctx, worker.RunRequest{TaskID: "task-cp-fb", Prompt: "x"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.CheckpointIDs) != 1 {
		t.Fatalf("CheckpointIDs = %d, want 1", len(res.CheckpointIDs))
	}
	cps := f.cpq.snapshot()
	cp := cps[0]
	wantMin := beforeRun.Add(20 * time.Second)
	wantMax := beforeRun.Add(40 * time.Second)
	if cp.DeadlineAt.Before(wantMin) || cp.DeadlineAt.After(wantMax) {
		t.Errorf("cp.DeadlineAt %v not in [%v, %v] for fallback (30s default)",
			cp.DeadlineAt, wantMin, wantMax)
	}
}

// TestRunFinalizeAdvanceErrorPropagates is the I-7 regression: when
// finalizeTask's Advance to StatusReview fails (disk full, FK
// violation, transition conflict against a concurrently-mutated row),
// Run MUST surface the error so Plan 5 orchestrator can detect that
// the SharedTaskList row never reached the terminal state — without
// this, the row would stay stuck in_progress despite the worker
// reporting Success=true, and the orchestrator would never dispatch
// L2 reviewers against it.
//
// Pre-fix, finalizeTask discarded the Advance error via `_ =`; the
// caller's Run treated it as a no-op success.
func TestRunFinalizeAdvanceErrorPropagates(t *testing.T) {
	f := newFixture(t, doneMsg())
	f.enqueueTask(t, "task-fin-adv-err", "x")

	f.stl.setNextAdvanceErr(errors.New("disk full"))
	w := f.newWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := w.Run(ctx, worker.RunRequest{TaskID: "task-fin-adv-err", Prompt: "x"})
	if err == nil {
		t.Fatal("expected Advance failure to propagate")
	}
	if !strings.Contains(err.Error(), "disk full") {
		t.Errorf("err = %v, want substring 'disk full'", err)
	}
	if res.Success {
		t.Error("Success=true; want false on finalize Advance failure")
	}
	if !strings.Contains(res.FailureReason, "finalize task") {
		t.Errorf("FailureReason = %q, want substring 'finalize task'", res.FailureReason)
	}
}

// TestRunResultQuotaExceeded is the I-6 regression: a terminal Result
// frame whose accumulated tokens (input + output) exceed the spec's
// MaxTokens MUST surface as ErrQuotaExceeded, not as Success=true.
// Without the explicit check inside the Result branch, the loop
// returns immediately on the terminal frame (line `return res, nil`)
// before reaching the post-switch quota check, and a token-overrun
// run silently reports success — defeating the cost-runaway prevention
// the Quota struct exists to enforce.
func TestRunResultQuotaExceeded(t *testing.T) {
	f := newFixture(t,

		subprocess.Message{
			Kind:    subprocess.MessageKindResult,
			ID:      "r-quota",
			Method:  "done",
			Payload: json.RawMessage(`{"stop_reason":"end_turn","input_tokens":10,"output_tokens":10}`),
		},
	)
	f.spec.Quota.MaxTokens = 5
	f.enqueueTask(t, "task-result-quota", "x")
	w := f.newWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := w.Run(ctx, worker.RunRequest{TaskID: "task-result-quota", Prompt: "x"})
	if !errors.Is(err, worker.ErrQuotaExceeded) {
		t.Fatalf("err = %v, want ErrQuotaExceeded", err)
	}
	if res.Success {
		t.Error("Success=true; want false on quota exceeded")
	}
	if res.FailureReason == "" {
		t.Error("FailureReason empty; expected populated")
	}
	if !strings.Contains(res.FailureReason, "MaxTokens") {
		t.Errorf("FailureReason = %q, want substring 'MaxTokens'", res.FailureReason)
	}
	row, _ := f.stl.Get(context.Background(), queue.TaskID("task-result-quota"))
	if row.Status != queue.StatusFailed {
		t.Errorf("row.Status = %q, want failed (quota exceeded)", row.Status)
	}
}

func TestRunUnknownMessageKindFails(t *testing.T) {
	f := newFixture(t,

		subprocess.Message{
			Kind:    subprocess.MessageKind(99),
			ID:      "unk-kind",
			Method:  "future-frame",
			Payload: json.RawMessage(`{}`),
		},
		doneMsg(),
	)
	f.enqueueTask(t, "task-unk-kind", "x")
	w := f.newWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := w.Run(ctx, worker.RunRequest{TaskID: "task-unk-kind", Prompt: "x"})
	if err == nil {
		t.Fatal("expected error on unknown MessageKind")
	}
	if !strings.Contains(err.Error(), "unknown MessageKind") {
		t.Errorf("err = %v, want substring 'unknown MessageKind'", err)
	}
	if res.Success {
		t.Error("Success=true; expected false on unknown MessageKind")
	}
	if res.FailureReason == "" {
		t.Error("FailureReason empty; expected populated")
	}
	row, _ := f.stl.Get(context.Background(), queue.TaskID("task-unk-kind"))
	if row.Status != queue.StatusFailed {
		t.Errorf("row.Status = %q, want failed", row.Status)
	}
}

func TestRunUnknownRequestMethod(t *testing.T) {
	f := newFixture(t,
		subprocess.Message{
			Kind:    subprocess.MessageKindRequest,
			ID:      "r-unk",
			Method:  "ping",
			Payload: json.RawMessage(`{}`),
		},
		doneMsg(),
	)
	f.enqueueTask(t, "task-unk-method", "x")
	w := f.newWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := w.Run(ctx, worker.RunRequest{TaskID: "task-unk-method", Prompt: "x"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	sent := f.session.sentSnapshot()
	var found bool
	for _, m := range sent {
		if m.Kind == subprocess.MessageKindError && m.ID == "r-unk" {
			if !strings.Contains(m.ErrMsg, "unknown_request_method") {
				t.Errorf("ErrMsg = %q, want substring 'unknown_request_method'", m.ErrMsg)
			}
			found = true
		}
	}
	if !found {
		t.Error("worker did not send Error frame for unknown request method")
	}
}

func TestRunToolUseSendErrorPropagates(t *testing.T) {
	relay := newFakeToolRelay(`{"ok":true}`, nil)
	tu := subprocess.Message{
		Kind:    subprocess.MessageKindRequest,
		ID:      "tu-send-err",
		Method:  "tool_use",
		Payload: json.RawMessage(`{"name":"research_dispatch","input":{}}`),
	}
	sess := &fakeSessionFailingSecondSend{
		threadID: subprocess.ThreadID("tid-fail"),
		inbound:  []subprocess.Message{tu, doneMsg()},
	}
	f := newFixture(t)
	f.enqueueTask(t, "task-tu-send-err", "x")
	w := worker.NewOpenClaudeWorker(worker.OpenClaudeWorkerOptions{
		Spec:            f.spec,
		WorktreePath:    "/tmp/wt-tu-se",
		Session:         sess,
		SharedTaskList:  f.stl,
		CheckpointQueue: f.cpq,
		FixPromptQueue:  f.fpq,
		DoctrineConfig:  f.cfg,
		ToolRelay:       relay,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := w.Run(ctx, worker.RunRequest{TaskID: "task-tu-send-err", Prompt: "x"})
	if err == nil {
		t.Fatal("expected Send-after-relay error to propagate")
	}
	if !errors.Is(err, subprocess.ErrSessionClosed) {
		t.Errorf("err = %v, want errors.Is(ErrSessionClosed)", err)
	}
}

func TestRunFixPromptQueueDrained(t *testing.T) {
	f := newFixture(t, doneMsg())

	_ = f.fpq.Put(context.Background(), queue.FixPrompt{
		TaskID:       queue.TaskID("task-fp"),
		ProjectID:    f.spec.ProjectID,
		WorkerID:     f.spec.ID,
		ReviewerTier: queue.ReviewerTierL2,
		PromptText:   "missing edge-case test for nil input",
		CriteriaName: "default",
		Severity:     queue.SeverityMinor,
	})
	f.enqueueTask(t, "task-fp", "implement Y")
	w := f.newWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := w.Run(ctx, worker.RunRequest{TaskID: "task-fp", Prompt: "implement Y"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	sent := f.session.sentSnapshot()
	var body struct {
		Prompt string `json:"prompt"`
	}
	_ = json.Unmarshal(sent[0].Payload, &body)
	if !strings.Contains(body.Prompt, "missing edge-case test") {
		t.Errorf("prompt missing fix-prompt content: %q", body.Prompt)
	}
}

func ExampleMakeCheckpointID() {
	id := worker.MakeCheckpointID("internal-platform-x", queue.TaskID("task-42"), 7)
	fmt.Println(id)

}

func TestMakeCheckpointIDDeterministic(t *testing.T) {
	a := worker.MakeCheckpointID("p1", queue.TaskID("t1"), 3)
	b := worker.MakeCheckpointID("p1", queue.TaskID("t1"), 3)
	if a != b {
		t.Errorf("MakeCheckpointID not deterministic: %q != %q", a, b)
	}
	want := "p1/t1/seq=3"
	if a != want {
		t.Errorf("MakeCheckpointID = %q, want %q (format is contract for Plan 5)", a, want)
	}
}
