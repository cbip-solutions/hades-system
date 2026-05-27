// SPDX-License-Identifier: MIT
package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cbip-solutions/hades-system/internal/workforce/queue"
	"github.com/cbip-solutions/hades-system/internal/workforce/subprocess"
)

// OpenClaudeWorker is the concrete Worker implementation that drives an
// OpenClaude subprocess via the subprocess.Session (stdio
// JSON-RPC). It implements the release spec §3.1 Flow 1 pipeline:
//
// 1. Claim a SharedTaskList row (status: pending → in_progress)
// 2. Drain pending FixPromptQueue rows for this worker
// 3. Build a prompt with the doctrine-reinforcement block (inv-hades-070 hook)
// + drained fix-prompts + the user prompt
// 4. Send the prompt as a single Request frame to OpenClaude
// 5. Loop on Receive:
// - Notification "checkpoint" → CheckpointQueue.Put with deadline-stamping
// (inv-hades-050 hook)
// - Request "tool_use" → route via ToolRelay; send Result/Error back
// - Result "done" / non-tool_use → terminal; advance task to review
// - Error → mark task failed
// 6. Finalize SharedTaskList (success → review, failure → failed)
//
// inv-hades-087 boundary: NewOpenClaudeWorker REQUIRES non-empty
// worktreePath. The Worker is the consumer; release WorktreePool will own
// allocation. Compile-check via constructor signature; runtime check via
// panic at construction with explanatory message.
//
// inv-hades-031 boundary: this struct depends only on internal/workforce/queue
// (interface) + internal/workforce/subprocess (interface) + worker
// package interfaces (DoctrineConfig, ToolRelay). It MUST NOT import
// internal/store directly. Concrete queue/store wiring lives in
// internal/daemon/workforceadapter.
type OpenClaudeWorker struct {
	spec         WorkerSpec
	worktreePath string

	session subprocess.Session

	stl queue.SharedTaskList
	cpq queue.CheckpointQueue
	fpq queue.FixPromptQueue

	cfg   DoctrineConfig
	relay ToolRelay

	lastResponseText string
}

type OpenClaudeWorkerOptions struct {
	Spec WorkerSpec

	WorktreePath string

	Session subprocess.Session

	SharedTaskList  queue.SharedTaskList
	CheckpointQueue queue.CheckpointQueue
	FixPromptQueue  queue.FixPromptQueue

	DoctrineConfig DoctrineConfig

	ToolRelay ToolRelay
}

func NewOpenClaudeWorker(opts OpenClaudeWorkerOptions) *OpenClaudeWorker {
	if strings.TrimSpace(opts.WorktreePath) == "" {
		panic(ErrNilWorktreePath)
	}
	if opts.Session == nil {
		panic(ErrNilSession)
	}
	if opts.SharedTaskList == nil || opts.CheckpointQueue == nil || opts.FixPromptQueue == nil {
		panic(ErrNilQueues)
	}
	if opts.DoctrineConfig == nil {
		panic(ErrNilDoctrineConfig)
	}
	if opts.ToolRelay == nil {
		panic(ErrNilToolRelay)
	}
	if err := opts.Spec.Validate(); err != nil {
		panic(fmt.Errorf("worker: invalid Spec: %w", err))
	}
	return &OpenClaudeWorker{
		spec:         opts.Spec,
		worktreePath: opts.WorktreePath,
		session:      opts.Session,
		stl:          opts.SharedTaskList,
		cpq:          opts.CheckpointQueue,
		fpq:          opts.FixPromptQueue,
		cfg:          opts.DoctrineConfig,
		relay:        opts.ToolRelay,
	}
}

func (w *OpenClaudeWorker) Run(ctx context.Context, req RunRequest) (RunResult, error) {
	if err := req.Validate(); err != nil {
		return RunResult{}, err
	}

	ctx, cancel := context.WithTimeout(ctx, w.spec.Quota.MaxDuration)
	defer cancel()

	taskID := queue.TaskID(req.TaskID)

	if err := w.claimTask(ctx, taskID); err != nil {
		return RunResult{}, err
	}

	prompt, err := w.buildPrompt(ctx, req)
	if err != nil {
		w.markFailed(taskID, fmt.Sprintf("build prompt: %v", err))
		return RunResult{}, fmt.Errorf("worker: build prompt: %w", err)
	}

	if err := w.sendPrompt(ctx, prompt); err != nil {
		w.markFailed(taskID, fmt.Sprintf("send prompt: %v", err))
		return RunResult{}, fmt.Errorf("worker: send prompt: %w", err)
	}

	res, runErr := w.runTurnLoop(ctx, taskID)

	finalizeErr := w.finalizeTask(taskID, res, runErr)

	if runErr != nil {
		return res, runErr
	}
	if finalizeErr != nil {

		res.Success = false
		res.FailureReason = "finalize task: " + finalizeErr.Error()
		return res, finalizeErr
	}
	return res, nil
}

func (w *OpenClaudeWorker) claimTask(ctx context.Context, taskID queue.TaskID) error {
	row, err := w.stl.Get(ctx, taskID)
	if err != nil {
		if errors.Is(err, queue.ErrTaskNotFound) {
			return ErrTaskNotFound
		}
		return fmt.Errorf("worker: SharedTaskList.Get: %w", err)
	}

	if row.Status == queue.StatusInProgress && row.ThreadID != w.session.ThreadID().String() {
		return ErrTaskAlreadyClaimed
	}

	if row.Status == queue.StatusInProgress && row.ThreadID == w.session.ThreadID().String() {
		return nil
	}
	if err := w.stl.Claim(ctx, taskID, w.session.ThreadID().String()); err != nil {
		if errors.Is(err, queue.ErrTaskNotFound) {
			return ErrTaskNotFound
		}
		if errors.Is(err, queue.ErrTaskNotPending) {
			return ErrTaskAlreadyClaimed
		}
		return fmt.Errorf("worker: SharedTaskList.Claim: %w", err)
	}
	return nil
}

func (w *OpenClaudeWorker) buildPrompt(ctx context.Context, req RunRequest) (string, error) {
	var b strings.Builder

	template := w.cfg.ReinforcementTemplate(w.spec.DoctrineName)
	marker := "[doctrine: " + w.spec.DoctrineName + "]"
	if !strings.Contains(template, marker) {

		b.WriteString(marker)
		b.WriteString("\n")
	}
	b.WriteString(template)
	b.WriteString("\n\n")

	pending, err := w.fpq.DrainByWorker(ctx, w.spec.ID)
	if err != nil {
		return "", fmt.Errorf("FixPromptQueue.DrainByWorker: %w", err)
	}
	for _, fp := range pending {
		b.WriteString("FIX-PROMPT (")
		b.WriteString(fp.ReviewerTier.String())
		b.WriteString("/")
		b.WriteString(fp.Severity.String())
		b.WriteString("):\n")
		b.WriteString(fp.PromptText)
		b.WriteString("\n\n")
	}

	b.WriteString(req.Prompt)
	return b.String(), nil
}

func (w *OpenClaudeWorker) sendPrompt(ctx context.Context, prompt string) error {
	payload, _ := json.Marshal(struct {
		Prompt string `json:"prompt"`
	}{Prompt: prompt})
	msg := subprocess.Message{
		Kind:     subprocess.MessageKindRequest,
		ID:       "prompt-1",
		ThreadID: w.session.ThreadID(),
		Method:   "prompt",
		Payload:  payload,
	}
	return w.session.Send(ctx, msg)
}

func (w *OpenClaudeWorker) runTurnLoop(ctx context.Context, taskID queue.TaskID) (RunResult, error) {
	res := RunResult{}
	for {

		select {
		case <-ctx.Done():
			res.FailureReason = "ctx done before receive"
			return res, ctx.Err()
		default:
		}

		msg, err := w.session.Receive(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				res.FailureReason = err.Error()
				return res, err
			}
			res.FailureReason = fmt.Sprintf("session.Receive: %v", err)
			return res, fmt.Errorf("worker: session.Receive: %w", err)
		}

		switch msg.Kind {
		case subprocess.MessageKindError:
			reason := fmt.Sprintf("subprocess error: code=%d msg=%q", msg.ErrCode, msg.ErrMsg)
			res.FailureReason = reason
			return res, errors.New(reason)
		case subprocess.MessageKindNotification:
			if err := w.handleNotification(ctx, msg, taskID, &res); err != nil {
				res.FailureReason = err.Error()
				return res, err
			}
		case subprocess.MessageKindRequest:
			if err := w.handleToolUseRequest(ctx, msg); err != nil {

				res.FailureReason = err.Error()
				return res, err
			}
			res.ToolUseCount++
		case subprocess.MessageKindResult:

			done, text, err := parseResult(msg.Payload, &res)
			if err != nil {
				res.FailureReason = fmt.Sprintf("parse result: %v", err)
				return res, fmt.Errorf("worker: parse result: %w", err)
			}
			w.lastResponseText = text
			res.Success = done

			if res.TokensUsed > w.spec.Quota.MaxTokens {
				res.FailureReason = fmt.Sprintf("worker: tokens=%d exceeds Quota.MaxTokens=%d on terminal Result",
					res.TokensUsed, w.spec.Quota.MaxTokens)
				res.Success = false
				return res, ErrQuotaExceeded
			}
			return res, nil
		default:
			// Unknown MessageKind: a forward-compatible frame from a
			// future subprocess revision, or a zero-value frame from a
			// misbehaving subprocess. Either way: fail-loud rather
			// than silently iterate without progress (which would burn
			// the ctx + quota budget without yielding any useful
			// failure signal). When the enum is extended, add an
			// explicit case branch above; do not relax this default.
			reason := fmt.Sprintf("worker: unknown MessageKind %v from session.Receive (forward-compat or zero-value frame)", msg.Kind)
			res.FailureReason = reason
			return res, errors.New(reason)
		}

		if res.TokensUsed > w.spec.Quota.MaxTokens {
			res.FailureReason = fmt.Sprintf("tokens %d exceeds Quota.MaxTokens %d",
				res.TokensUsed, w.spec.Quota.MaxTokens)
			return res, ErrQuotaExceeded
		}
	}
}

func (w *OpenClaudeWorker) handleNotification(ctx context.Context, msg subprocess.Message, taskID queue.TaskID, res *RunResult) error {
	switch msg.Method {
	case "checkpoint":
		var body struct {
			State json.RawMessage `json:"state"`
			Seq   int             `json:"seq"`
		}

		if err := json.Unmarshal(msg.Payload, &body); err != nil {
			return fmt.Errorf("checkpoint payload not JSON: %w", err)
		}
		stateBytes := []byte(body.State)
		if len(stateBytes) == 0 {

			stateBytes = []byte(msg.Payload)
		}
		seq := body.Seq
		if seq == 0 {
			seq = len(res.CheckpointIDs) + 1
		}
		deadline := w.computeCheckpointDeadline()
		cp := queue.Checkpoint{
			TaskID:     taskID,
			ProjectID:  w.spec.ProjectID,
			ThreadID:   w.session.ThreadID().String(),
			StateJSON:  string(stateBytes),
			SeqNum:     seq,
			DeadlineAt: deadline,
		}
		if err := w.cpq.Put(ctx, cp); err != nil {
			return fmt.Errorf("CheckpointQueue.Put: %w", err)
		}

		res.CheckpointIDs = append(res.CheckpointIDs, MakeCheckpointID(w.spec.ProjectID, taskID, seq))
	case "token_usage":
		var body struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		}
		if err := json.Unmarshal(msg.Payload, &body); err == nil {
			res.TokensUsed += body.InputTokens + body.OutputTokens
		}

	}
	return nil
}

func (w *OpenClaudeWorker) computeCheckpointDeadline() time.Time {
	d := w.cfg.CheckpointDeadline(w.spec.DoctrineName)
	if d <= 0 {
		d = DefaultCheckpointDeadline
	}
	return time.Now().UTC().Add(d)
}

func (w *OpenClaudeWorker) handleToolUseRequest(ctx context.Context, msg subprocess.Message) error {

	if msg.Method != "tool_use" {
		return w.sendError(ctx, msg.ID, 400, fmt.Sprintf("unknown_request_method: %q", msg.Method))
	}

	var body struct {
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(msg.Payload, &body); err != nil {
		return w.sendError(ctx, msg.ID, 400, fmt.Sprintf("malformed_tool_use_payload: %v", err))
	}

	if !w.toolInWhitelist(body.Name) {
		return w.sendError(ctx, msg.ID, 403,
			fmt.Sprintf("tool_not_in_whitelist: %s (spec.Tools=%v)", body.Name, w.spec.Tools))
	}
	if !knownToolFamily(body.Name) {
		return w.sendError(ctx, msg.ID, 400,
			fmt.Sprintf("unknown_tool_family: %s (no router for prefix)", body.Name))
	}

	out, err := w.relay.Dispatch(ctx, body.Name, body.Input)
	if err != nil {
		return w.sendError(ctx, msg.ID, 502, fmt.Sprintf("relay_failed: %v", err))
	}

	resp := subprocess.Message{
		Kind:     subprocess.MessageKindResult,
		ID:       msg.ID,
		ThreadID: w.session.ThreadID(),
		Method:   "tool_result",
		Payload:  out,
	}
	return w.session.Send(ctx, resp)
}

func (w *OpenClaudeWorker) sendError(ctx context.Context, correlationID string, code int, errMsg string) error {
	return w.session.Send(ctx, subprocess.Message{
		Kind:     subprocess.MessageKindError,
		ID:       correlationID,
		ThreadID: w.session.ThreadID(),
		ErrCode:  code,
		ErrMsg:   errMsg,
	})
}

func (w *OpenClaudeWorker) toolInWhitelist(name string) bool {
	for _, t := range w.spec.Tools {
		if t == name {
			return true
		}
	}
	return false
}

func knownToolFamily(name string) bool {
	if name == "ssh_exec" {
		return true
	}
	if strings.HasPrefix(name, "research_") {
		return true
	}
	if strings.HasPrefix(name, "audit_") {
		return true
	}
	if strings.HasPrefix(name, "budget_") || strings.HasPrefix(name, "budget.") {
		return true
	}
	return false
}

func parseResult(payload json.RawMessage, res *RunResult) (done bool, text string, err error) {
	var body struct {
		StopReason   string `json:"stop_reason"`
		InputTokens  int    `json:"input_tokens"`
		OutputTokens int    `json:"output_tokens"`
		Text         string `json:"text"`
	}
	if len(payload) == 0 {

		res.FinalStopReason = "end_turn"
		return true, "", nil
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		return false, "", err
	}
	res.FinalStopReason = body.StopReason
	res.TokensUsed += body.InputTokens + body.OutputTokens
	return body.StopReason == "end_turn", body.Text, nil
}

func (w *OpenClaudeWorker) LastResponseText() string {
	return w.lastResponseText
}

// MakeCheckpointID returns the stable string used as the worker-local
// identifier for a checkpoint emission. release's reviewer dispatcher
// MUST compute the same string deterministically from (projectID,
// taskID, seq) to correlate Drain reads with the upstream worker's
// RunResult.CheckpointIDs slice.
//
// Format "<projectID>/<taskID>/seq=<seq>". The format is stable;
// changing it is a breaking contract + audit downstream.
//
// The queue.CheckpointQueue interface intentionally does not return
// an ID from Put (the SQLite rowid is an implementation detail), so
// this string is the worker layer's correlation handle.
func MakeCheckpointID(projectID string, taskID queue.TaskID, seq int) string {
	return fmt.Sprintf("%s/%s/seq=%d", projectID, taskID, seq)
}

// finalizeTask updates the SharedTaskList row based on the run outcome.
// Success → status=review.
// Failure → status=failed.
//
// SharedTaskList writes use a fresh background context so cancellation
// in the caller's ctx does not prevent the row from reaching a terminal
// state.
//
// Returns the Advance error so Run can surface infrastructure failures
// (disk full, FK violation, transition conflict) to the caller —
// without this, a row could remain stuck in_progress despite Run
// reporting completion, leaving the orchestrator unable to detect that
// the SharedTaskList drift had occurred. Early-exit paths in Run that
// call markFailed do NOT propagate the error (they already have a
// runErr to surface; finalizeTask's error would just shadow it).
func (w *OpenClaudeWorker) finalizeTask(taskID queue.TaskID, res RunResult, runErr error) error {
	bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if runErr == nil && res.Success {

		if err := w.stl.Advance(bgCtx, taskID, queue.StatusReview); err != nil {
			return fmt.Errorf("Advance to review: %w", err)
		}
		return nil
	}
	w.markFailed(taskID, res.FailureReason)
	return nil
}

func (w *OpenClaudeWorker) markFailed(taskID queue.TaskID, reason string) {
	_ = reason
	bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = w.stl.Advance(bgCtx, taskID, queue.StatusFailed)
}
