// SPDX-License-Identifier: MIT
package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/cbip-solutions/hades-system/internal/workforce/queue"
	"github.com/cbip-solutions/hades-system/internal/workforce/subprocess"
)

type AggregationInput struct {
	WindowID        string
	FromLevel       string
	ToLevel         string
	StartTime       time.Time
	EndTime         time.Time
	Summary         string
	AnchorTaskIDs   []queue.TaskID
	AnchorWorkerIDs []string
}

type AmendmentProposal struct {
	ID        string
	WindowID  string
	Area      string
	Change    string
	Rationale string
	EmittedAt time.Time
}

// AmendmentProposalEmitter is the small surface L4 reviewers use.
// ships InMemoryAmendmentEmitter for testing + bootstrap.
//
// Concurrency implementations MUST be safe for concurrent Emit +
// Proposals calls (multiple L4 reviewers may write simultaneously).
type AmendmentProposalEmitter interface {
	Emit(ctx context.Context, p AmendmentProposal) error
	Proposals() []AmendmentProposal
}

type InMemoryAmendmentEmitter struct {
	mu    sync.Mutex
	store []AmendmentProposal
}

func NewInMemoryAmendmentEmitter() *InMemoryAmendmentEmitter {
	return &InMemoryAmendmentEmitter{}
}

func (e *InMemoryAmendmentEmitter) Emit(_ context.Context, p AmendmentProposal) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if p.EmittedAt.IsZero() {
		p.EmittedAt = time.Now().UTC()
	}
	e.store = append(e.store, p)
	return nil
}

func (e *InMemoryAmendmentEmitter) Proposals() []AmendmentProposal {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]AmendmentProposal, len(e.store))
	copy(out, e.store)
	return out
}

type Reviewer struct {
	spec WorkerSpec

	manager SubprocessAcquirer

	session subprocess.Session

	stl queue.SharedTaskList

	cpq queue.CheckpointQueue

	fpq queue.FixPromptQueue

	amendments AmendmentProposalEmitter

	llm *OpenClaudeWorker
}

type ReviewerOptions struct {
	Spec                     WorkerSpec
	WorktreePath             string
	SubprocessManager        SubprocessAcquirer
	SharedTaskList           queue.SharedTaskList
	CheckpointQueue          queue.CheckpointQueue
	FixPromptQueue           queue.FixPromptQueue
	DoctrineConfig           DoctrineConfig
	ToolRelay                ToolRelay
	AmendmentProposalEmitter AmendmentProposalEmitter
}

func NewReviewer(opts ReviewerOptions) (*Reviewer, error) {
	switch opts.Spec.Variant {
	case VariantReviewerL2, VariantReviewerL3, VariantReviewerL4:

	default:
		return nil, fmt.Errorf("worker: NewReviewer requires Spec.Variant in {reviewer-l2, reviewer-l3, reviewer-l4}, got %v", opts.Spec.Variant)
	}
	if opts.SubprocessManager == nil {
		return nil, errors.New("worker: NewReviewer requires SubprocessManager")
	}
	if strings.TrimSpace(opts.WorktreePath) == "" {
		panic(ErrNilWorktreePath)
	}
	if opts.SharedTaskList == nil || opts.CheckpointQueue == nil || opts.FixPromptQueue == nil {
		panic(ErrNilQueues)
	}
	if opts.DoctrineConfig == nil {
		panic(ErrNilDoctrineConfig)
	}
	if err := opts.Spec.Validate(); err != nil {
		panic(fmt.Errorf("worker: invalid Spec: %w", err))
	}
	relay := opts.ToolRelay
	if relay == nil {
		relay = NewUnavailableRelay()
	}

	emitter := opts.AmendmentProposalEmitter
	if opts.Spec.Variant == VariantReviewerL4 && emitter == nil {
		emitter = NewInMemoryAmendmentEmitter()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var session subprocess.Session
	var err error
	if opts.Spec.Variant.Persistent() {
		session, err = opts.SubprocessManager.AcquirePersistent(ctx, opts.Spec, opts.WorktreePath)
		if err != nil {
			return nil, fmt.Errorf("worker: AcquirePersistent: %w", err)
		}
	} else {
		session, err = opts.SubprocessManager.SpawnEphemeral(ctx, opts.Spec, opts.WorktreePath)
		if err != nil {
			return nil, fmt.Errorf("worker: SpawnEphemeral: %w", err)
		}
	}

	llm := NewOpenClaudeWorker(OpenClaudeWorkerOptions{
		Spec:            opts.Spec,
		WorktreePath:    opts.WorktreePath,
		Session:         session,
		SharedTaskList:  opts.SharedTaskList,
		CheckpointQueue: opts.CheckpointQueue,
		FixPromptQueue:  opts.FixPromptQueue,
		DoctrineConfig:  opts.DoctrineConfig,
		ToolRelay:       relay,
	})

	return &Reviewer{
		spec:       opts.Spec,
		manager:    opts.SubprocessManager,
		session:    session,
		stl:        opts.SharedTaskList,
		cpq:        opts.CheckpointQueue,
		fpq:        opts.FixPromptQueue,
		amendments: emitter,
		llm:        llm,
	}, nil
}

func (r *Reviewer) Close() error {
	return r.manager.Release(r.session)
}

func (r *Reviewer) ReviewCheckpoint(ctx context.Context, taskID queue.TaskID, workerID string) error {
	if r.spec.Variant != VariantReviewerL2 {
		return fmt.Errorf("worker: ReviewCheckpoint is L2-only; this reviewer is %v", r.spec.Variant)
	}
	if strings.TrimSpace(string(taskID)) == "" {
		return errors.New("worker: ReviewCheckpoint requires non-empty taskID")
	}
	if strings.TrimSpace(workerID) == "" {
		return errors.New("worker: ReviewCheckpoint requires non-empty workerID")
	}

	checkpoints, err := r.cpq.Peek(ctx, taskID)
	if err != nil {
		return fmt.Errorf("worker: cpq.Peek: %w", err)
	}

	reviewTaskID := queue.TaskID(string(taskID) + "/review-l2/" + workerID)
	if err := r.stl.Enqueue(ctx, queue.TaskRow{
		TaskID:      reviewTaskID,
		ProjectID:   r.spec.ProjectID,
		Description: "L2 review of " + string(taskID),
		Status:      queue.StatusPending,
	}); err != nil && !errors.Is(err, queue.ErrDuplicateTask) {
		return fmt.Errorf("worker: enqueue review row: %w", err)
	}

	prompt := r.buildL2Prompt(taskID, workerID, checkpoints)
	res, err := r.llm.Run(ctx, RunRequest{TaskID: string(reviewTaskID), Prompt: prompt})
	if err != nil {
		return fmt.Errorf("worker: reviewer LLM: %w", err)
	}
	if !res.Success {
		return fmt.Errorf("worker: reviewer LLM unsuccessful: %s", res.FailureReason)
	}

	verdict, err := parseVerdict(r.llm.LastResponseText())
	if err != nil {
		return fmt.Errorf("worker: parse reviewer verdict: %w", err)
	}
	if verdict.Verdict == "" || verdict.Verdict == "clean" {
		return nil
	}

	severity := mapVerdictToSeverity(verdict.Verdict)
	body := strings.Join(verdict.Concerns, "; ")
	if body == "" {
		body = "(no concerns text)"
	}
	return r.fpq.Put(ctx, queue.FixPrompt{
		TaskID:       taskID,
		ProjectID:    r.spec.ProjectID,
		WorkerID:     workerID,
		ReviewerTier: queue.ReviewerTierL2,
		PromptText:   body,
		CriteriaName: "default",
		Severity:     severity,
	})
}

// ReviewAggregation is the L3 surface. Drives the reviewer LLM with
// the aggregation summary + anchor task references; emits one
// FixPromptQueue row per anchor task addressed to the corresponding
// AnchorWorkerID (paired by index with AnchorTaskIDs).
//
// L3 is persistent: this method may be called many times against the
// same Reviewer; each call drives one LLM round-trip via the long-lived
// subprocess.
//
// Concurrency ReviewAggregation is NOT safe for concurrent invocation
// on the same *Reviewer. The wrapped *OpenClaudeWorker (llm) is
// single-tenant — successive calls MUST be serialised by the caller.
// "Persistent" refers to subprocess lifecycle (the session survives
// across calls), NOT to thread safety.
func (r *Reviewer) ReviewAggregation(ctx context.Context, agg AggregationInput) error {
	if r.spec.Variant != VariantReviewerL3 {
		return fmt.Errorf("worker: ReviewAggregation is L3-only; this reviewer is %v", r.spec.Variant)
	}
	if strings.TrimSpace(agg.WindowID) == "" {
		return errors.New("worker: ReviewAggregation requires non-empty WindowID")
	}

	reviewTaskID := queue.TaskID("review-l3/" + agg.WindowID)
	if err := r.stl.Enqueue(ctx, queue.TaskRow{
		TaskID:      reviewTaskID,
		ProjectID:   r.spec.ProjectID,
		Description: "L3 strategic review of " + agg.WindowID,
		Status:      queue.StatusPending,
	}); err != nil && !errors.Is(err, queue.ErrDuplicateTask) {
		return fmt.Errorf("worker: enqueue review row: %w", err)
	}

	prompt := r.buildL3Prompt(agg)
	res, err := r.llm.Run(ctx, RunRequest{TaskID: string(reviewTaskID), Prompt: prompt})
	if err != nil {
		return fmt.Errorf("worker: reviewer LLM: %w", err)
	}
	if !res.Success {
		return fmt.Errorf("worker: reviewer LLM unsuccessful: %s", res.FailureReason)
	}
	verdict, err := parseVerdict(r.llm.LastResponseText())
	if err != nil {
		return fmt.Errorf("worker: parse reviewer verdict: %w", err)
	}
	if verdict.Verdict == "" || verdict.Verdict == "clean" {
		return nil
	}
	body := strings.Join(verdict.Concerns, "; ")
	if body == "" {
		body = "(no concerns text)"
	}

	for i, taskID := range agg.AnchorTaskIDs {
		var workerID string
		if i < len(agg.AnchorWorkerIDs) {
			workerID = agg.AnchorWorkerIDs[i]
		} else {
			workerID = r.spec.ID
		}
		if err := r.fpq.Put(ctx, queue.FixPrompt{
			TaskID:       taskID,
			ProjectID:    r.spec.ProjectID,
			WorkerID:     workerID,
			ReviewerTier: queue.ReviewerTierL3,
			PromptText:   body,
			CriteriaName: "default",
			Severity:     mapVerdictToSeverity(verdict.Verdict),
		}); err != nil {
			return fmt.Errorf("worker: fpq.Put for anchor %s: %w", taskID, err)
		}
	}
	return nil
}

func (r *Reviewer) ProposeAmendment(ctx context.Context, agg AggregationInput) error {
	if r.spec.Variant != VariantReviewerL4 {
		return fmt.Errorf("worker: ProposeAmendment is L4-only; this reviewer is %v", r.spec.Variant)
	}

	reviewTaskID := queue.TaskID("review-l4/" + agg.WindowID)
	if err := r.stl.Enqueue(ctx, queue.TaskRow{
		TaskID:      reviewTaskID,
		ProjectID:   r.spec.ProjectID,
		Description: "L4 architectural review of " + agg.WindowID,
		Status:      queue.StatusPending,
	}); err != nil && !errors.Is(err, queue.ErrDuplicateTask) {
		return fmt.Errorf("worker: enqueue review row: %w", err)
	}

	prompt := r.buildL4Prompt(agg)
	res, err := r.llm.Run(ctx, RunRequest{TaskID: string(reviewTaskID), Prompt: prompt})
	if err != nil {
		return fmt.Errorf("worker: reviewer LLM: %w", err)
	}
	if !res.Success {
		return fmt.Errorf("worker: reviewer LLM unsuccessful: %s", res.FailureReason)
	}
	amendments, err := parseAmendments(r.llm.LastResponseText())
	if err != nil {
		return fmt.Errorf("worker: parse amendments: %w", err)
	}
	for i, a := range amendments {
		proposal := AmendmentProposal{
			ID:        fmt.Sprintf("%s/amend-%d", agg.WindowID, i+1),
			WindowID:  agg.WindowID,
			Area:      a.Area,
			Change:    a.Change,
			Rationale: a.Rationale,
		}
		if err := r.amendments.Emit(ctx, proposal); err != nil {
			return fmt.Errorf("worker: emit amendment %d: %w", i, err)
		}
	}
	return nil
}

func (r *Reviewer) buildL2Prompt(taskID queue.TaskID, workerID string, checkpoints []queue.Checkpoint) string {
	var b strings.Builder
	b.WriteString("You are an L2 (tactical) reviewer. Review the worker's checkpoint(s) below.\n")
	b.WriteString("Output JSON: {\"verdict\":\"clean|minor|major|reject\",\"concerns\":[\"...\"]}\n\n")
	b.WriteString(fmt.Sprintf("Task: %s\nWorker: %s\n\n", taskID, workerID))
	for i, cp := range checkpoints {
		b.WriteString(fmt.Sprintf("Checkpoint %d (seq=%d): %s\n", i+1, cp.SeqNum, cp.StateJSON))
	}
	return b.String()
}

func (r *Reviewer) buildL3Prompt(agg AggregationInput) string {
	return fmt.Sprintf(
		"You are an L3 (strategic) reviewer. Review the aggregated window summary below.\n"+
			"Output JSON: {\"verdict\":\"clean|minor|major|reject\",\"concerns\":[\"...\"]}\n\n"+
			"Window: %s [%s, %s]\n%s",
		agg.WindowID, agg.StartTime.Format(time.RFC3339), agg.EndTime.Format(time.RFC3339), agg.Summary,
	)
}

func (r *Reviewer) buildL4Prompt(agg AggregationInput) string {
	return fmt.Sprintf(
		"You are an L4 (architectural) reviewer. Review the macro-window summary below.\n"+
			"Output JSON: {\"amendments\":[{\"area\":\"...\",\"change\":\"...\",\"rationale\":\"...\"}]}\n\n"+
			"Window: %s\n%s",
		agg.WindowID, agg.Summary,
	)
}

type reviewerVerdict struct {
	Verdict  string   `json:"verdict"`
	Concerns []string `json:"concerns"`
}

func parseVerdict(text string) (reviewerVerdict, error) {
	var v reviewerVerdict
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end < 0 || end <= start {
		return v, errors.New("verdict JSON not found")
	}
	if err := json.Unmarshal([]byte(text[start:end+1]), &v); err != nil {
		return v, err
	}
	return v, nil
}

type amendmentItem struct {
	Area      string `json:"area"`
	Change    string `json:"change"`
	Rationale string `json:"rationale,omitempty"`
}

type amendmentList struct {
	Amendments []amendmentItem `json:"amendments"`
}

func parseAmendments(text string) ([]amendmentItem, error) {
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end < 0 || end <= start {
		return nil, errors.New("amendments JSON not found")
	}
	var list amendmentList
	if err := json.Unmarshal([]byte(text[start:end+1]), &list); err != nil {
		return nil, err
	}
	return list.Amendments, nil
}

// mapVerdictToSeverity maps the LLM verdict string to a queue.Severity.
// Callers MUST not pass "clean" (caller short-circuits before calling
// this; "clean" → no FixPrompt). Unknown/empty strings default to
// SeverityMinor (conservative — the reviewer flagged something but
// didn't classify; treat as minor).
//
// Verdict aliases (NOT mentioned in the explicit L2/L3/L4 prompt
// contracts, but observed empirically + accepted defensively):
//
// - "strategic" — L3-style verdict the LLM may emit when the
// prompt asks for strategic review; mapped to SeverityMajor
// because strategic concerns escalate the same as major.
// - "architectural" — L4-style verdict the LLM may emit when the
// prompt asks for architectural review; mapped to SeverityMajor
// for the same reason.
//
// The L2/L3/L4 prompts ask for {clean|minor|major|reject}; the
// aliases above absorb LLM verdict drift without surfacing as
// SeverityMinor (which would mute a real strategic/architectural
// concern). When the prompts evolve to formalise these aliases,
// remove them from the alias bucket and add explicit cases.
func mapVerdictToSeverity(verdict string) queue.Severity {
	switch strings.ToLower(strings.TrimSpace(verdict)) {
	case "minor":
		return queue.SeverityMinor
	case "major", "strategic", "architectural":
		return queue.SeverityMajor
	case "reject":
		return queue.SeverityReject
	}
	return queue.SeverityMinor
}
