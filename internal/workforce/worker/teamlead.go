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

// SubprocessAcquirer is the small surface TeamLead + Reviewer use for
// session lifecycle. *subprocess.Manager satisfies this
// interface (with one trivial adapter that translates worker.WorkerSpec
// → subprocess.WorkerSpecRef; see internal/daemon/workforceadapter).
//
// Concurrency implementations MUST be safe for concurrent calls from
// multiple TeamLead/Reviewer constructors.
type SubprocessAcquirer interface {
	AcquirePersistent(ctx context.Context, spec WorkerSpec, worktreePath string) (subprocess.Session, error)

	SpawnEphemeral(ctx context.Context, spec WorkerSpec, worktreePath string) (subprocess.Session, error)

	Release(s subprocess.Session) error
}

// WorkerFactory creates child Workers on TeamLead's behalf. HADES design's
// orchestrator wires the production factory; tests provide fakes.
//
// Concurrency implementations MUST be safe for concurrent NewChild
// calls (TeamLead dispatches children in parallel goroutines).
type WorkerFactory interface {
	NewChild(parentTaskID queue.TaskID, spec WorkerSpec, worktreePath string) (Worker, error)
}

type TeamLead struct {
	spec WorkerSpec

	worktreePath string

	manager SubprocessAcquirer

	session subprocess.Session

	stl queue.SharedTaskList

	fpq queue.FixPromptQueue

	childFactory WorkerFactory

	planner *OpenClaudeWorker

	mu               sync.Mutex
	parentToChildren map[queue.TaskID][]queue.TaskID
}

type TeamLeadOptions struct {
	Spec               WorkerSpec
	WorktreePath       string
	SubprocessManager  SubprocessAcquirer
	SharedTaskList     queue.SharedTaskList
	CheckpointQueue    queue.CheckpointQueue
	FixPromptQueue     queue.FixPromptQueue
	DoctrineConfig     DoctrineConfig
	ToolRelay          ToolRelay
	ChildWorkerFactory WorkerFactory
}

func NewTeamLead(opts TeamLeadOptions) (*TeamLead, error) {
	if opts.Spec.Variant != VariantTeamLead {
		return nil, fmt.Errorf("worker: NewTeamLead requires Spec.Variant=teamlead, got %v", opts.Spec.Variant)
	}
	if opts.SubprocessManager == nil {
		return nil, errors.New("worker: NewTeamLead requires SubprocessManager")
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

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	session, err := opts.SubprocessManager.AcquirePersistent(ctx, opts.Spec, opts.WorktreePath)
	if err != nil {
		return nil, fmt.Errorf("worker: AcquirePersistent: %w", err)
	}

	planner := NewOpenClaudeWorker(OpenClaudeWorkerOptions{
		Spec:            opts.Spec,
		WorktreePath:    opts.WorktreePath,
		Session:         session,
		SharedTaskList:  opts.SharedTaskList,
		CheckpointQueue: opts.CheckpointQueue,
		FixPromptQueue:  opts.FixPromptQueue,
		DoctrineConfig:  opts.DoctrineConfig,
		ToolRelay:       relay,
	})
	return &TeamLead{
		spec:             opts.Spec,
		worktreePath:     opts.WorktreePath,
		manager:          opts.SubprocessManager,
		session:          session,
		stl:              opts.SharedTaskList,
		fpq:              opts.FixPromptQueue,
		childFactory:     opts.ChildWorkerFactory,
		planner:          planner,
		parentToChildren: map[queue.TaskID][]queue.TaskID{},
	}, nil
}

func (t *TeamLead) Run(ctx context.Context, req RunRequest) (RunResult, error) {
	if err := req.Validate(); err != nil {
		return RunResult{}, err
	}
	taskID := queue.TaskID(req.TaskID)

	plannerTaskID := queue.TaskID(string(taskID) + "/planner")
	if err := t.stl.Enqueue(ctx, queue.TaskRow{
		TaskID:      plannerTaskID,
		ProjectID:   t.spec.ProjectID,
		Description: req.Prompt,
		Status:      queue.StatusPending,
	}); err != nil && !errors.Is(err, queue.ErrDuplicateTask) {
		return RunResult{}, fmt.Errorf("worker: enqueue planner row: %w", err)
	}

	planRes, err := t.planner.Run(ctx, RunRequest{
		TaskID: string(plannerTaskID),
		Prompt: t.buildPlannerPrompt(req.Prompt),
	})
	if err != nil {

		t.markParentFailed(taskID, fmt.Sprintf("planner: %v", err))
		return planRes, err
	}

	plannerSent := t.planner.LastResponseText()
	plan, err := parsePlan(plannerSent)
	if err != nil {
		t.markParentFailed(taskID, fmt.Sprintf("plan parse: %v", err))
		return RunResult{Success: false, FailureReason: err.Error()}, fmt.Errorf("worker: plan parse: %w", err)
	}

	if err := t.claimParent(ctx, taskID); err != nil {
		return RunResult{}, err
	}
	childResults, dispatchErr := t.dispatchChildren(ctx, taskID, plan)

	res := t.aggregate(planRes, childResults, dispatchErr)
	t.finalizeParent(taskID, res)
	if dispatchErr != nil {
		return res, dispatchErr
	}
	return res, nil
}

func (t *TeamLead) Children(parentTaskID string) []queue.TaskID {
	t.mu.Lock()
	defer t.mu.Unlock()
	src := t.parentToChildren[queue.TaskID(parentTaskID)]
	out := make([]queue.TaskID, len(src))
	copy(out, src)
	return out
}

// Close releases the persistent session. Persistent sessions stay
// alive across Close unless the TTL evictor fires; calling Release is
// a hint to the SubprocessManager that the operator no longer needs
// this session.
//
// The constructor guarantees both t.session and t.manager are non-nil
// (AcquirePersistent must succeed before construction returns), so we
// do not re-check here.
func (t *TeamLead) Close() error {
	return t.manager.Release(t.session)
}

type childPlanItem struct {
	ID       string `json:"id"`
	Prompt   string `json:"prompt"`
	TaskTier string `json:"task_tier,omitempty"`
}

type childPlan struct {
	Plan []childPlanItem `json:"plan"`
}

func (t *TeamLead) buildPlannerPrompt(objective string) string {
	return fmt.Sprintf(
		"You are the team lead. Decompose the following objective into N "+
			"child tasks. Output valid JSON with shape "+
			`{"plan":[{"id":"sub-1","prompt":"...","task_tier":"medium"}]}`+
			".\n\nObjective: %s",
		objective,
	)
}

// parsePlan extracts the JSON plan object from the planner LLM's
// response text. Tolerates surrounding prose (the LLM may emit a
// preamble) by slicing from the FIRST '{' to the LAST '}' — the
// outer block.
//
// Contract assumption (locked by TestParsePlanDoubleBlockUsesOuter):
// the planner LLM is doctrine-bound to emit at most one top-level
// JSON object. If two adjacent {...}{...} blocks appear (LLM
// misbehaviour), this extractor takes the OUTER span (from the
// first '{' to the last '}'), which json.Unmarshal then rejects as
// malformed — fail-loud rather than silently picking either block.
//
// We do NOT use a streaming JSON tokenizer here because the doctrine
// reinforcement block (invariant) explicitly forbids the LLM from
// emitting embedded code-fence JSON or stray braces inside prose;
// adding a tokenizer would mask doctrine drift instead of surfacing
// it. The adversarial test locks this semantic so a future
// "tolerant" refactor must justify the doctrine relaxation first.
func parsePlan(text string) (childPlan, error) {
	var p childPlan
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end < 0 || end <= start {
		return p, errors.New("worker: plan JSON not found in planner response")
	}
	if err := json.Unmarshal([]byte(text[start:end+1]), &p); err != nil {
		return p, fmt.Errorf("worker: plan JSON unmarshal: %w", err)
	}
	if len(p.Plan) == 0 {
		return p, errors.New("worker: planner returned empty plan")
	}
	return p, nil
}

func (t *TeamLead) claimParent(ctx context.Context, parentID queue.TaskID) error {
	row, err := t.stl.Get(ctx, parentID)
	if err != nil {
		if errors.Is(err, queue.ErrTaskNotFound) {
			return ErrTaskNotFound
		}
		return fmt.Errorf("worker: claimParent.Get: %w", err)
	}
	if row.Status == queue.StatusInProgress {
		return nil
	}
	if err := t.stl.Claim(ctx, parentID, t.session.ThreadID().String()); err != nil {
		if errors.Is(err, queue.ErrTaskNotFound) {
			return ErrTaskNotFound
		}
		if errors.Is(err, queue.ErrTaskNotPending) {
			return ErrTaskAlreadyClaimed
		}
		return fmt.Errorf("worker: claimParent.Claim: %w", err)
	}
	return nil
}

func (t *TeamLead) dispatchChildren(ctx context.Context, parentID queue.TaskID, plan childPlan) ([]RunResult, error) {
	results := make([]RunResult, len(plan.Plan))
	errs := make([]error, len(plan.Plan))
	var wg sync.WaitGroup

	for i, item := range plan.Plan {

		childTier := TierMedium
		if item.TaskTier != "" {
			if parsed, perr := ParseTaskTier(item.TaskTier); perr == nil {
				childTier = parsed
			}
		}
		childSpec := t.spec

		childSpec.Tools = append([]string(nil), t.spec.Tools...)
		childSpec.ID = fmt.Sprintf("%s-child-%s", t.spec.ID, item.ID)
		childSpec.Variant = VariantWorker
		childSpec.TaskTier = childTier

		childTaskID := queue.TaskID(fmt.Sprintf("%s/%s", parentID, item.ID))
		if err := t.stl.Enqueue(ctx, queue.TaskRow{
			TaskID:      childTaskID,
			ProjectID:   t.spec.ProjectID,
			Description: item.Prompt,
			Status:      queue.StatusPending,
		}); err != nil && !errors.Is(err, queue.ErrDuplicateTask) {
			errs[i] = fmt.Errorf("Enqueue child %s: %w", item.ID, err)
			continue
		}

		t.recordChild(parentID, childTaskID)

		child, err := t.childFactory.NewChild(parentID, childSpec, t.worktreePath)
		if err != nil {
			errs[i] = fmt.Errorf("NewChild %s: %w", item.ID, err)
			t.emitChildFixPrompt(ctx, childTaskID, childSpec.ID, item.ID, err)
			continue
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			res, runErr := child.Run(ctx, RunRequest{
				TaskID: string(childTaskID),
				Prompt: item.Prompt,
			})
			results[i] = res
			if runErr != nil {
				errs[i] = runErr
				t.emitChildFixPrompt(ctx, childTaskID, childSpec.ID, item.ID, runErr)
			}
		}()
	}
	wg.Wait()

	return results, errors.Join(errs...)
}

// emitChildFixPrompt writes a FixPromptQueue row addressed to the
// TeamLead's own spec ID so the next planner iteration sees the
// failure context. Severity is l2 (tactical) — a single child failure
// is recoverable; aggregated failures escalate to l3/l4 via
// AggregationStream.
//
// The caller's ctx is intentionally ignored (signature `_ context.Context`):
// the goroutine in dispatchChildren may run past the caller's
// cancellation, and we MUST land the FixPromptQueue write so the
// upstream worker sees the failure on the next iteration. A fresh
// background context with a 5s budget mirrors finalizeTask + markFailed
// (the SQL-backed FixPromptQueue impl honours ctx via its database
// driver, so passing the cancelled caller ctx would silently drop the
// fix-prompt — see TestTeamLeadFixPromptSurvivesParentCancellation).
func (t *TeamLead) emitChildFixPrompt(_ context.Context, childTaskID queue.TaskID, childSpecID, planItemID string, err error) {
	bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = t.fpq.Put(bgCtx, queue.FixPrompt{
		TaskID:       childTaskID,
		ProjectID:    t.spec.ProjectID,
		WorkerID:     t.spec.ID,
		ReviewerTier: queue.ReviewerTierL2,
		PromptText:   fmt.Sprintf("child %s (spec=%s) failed: %v", planItemID, childSpecID, err),
		CriteriaName: "default",
		Severity:     queue.SeverityMajor,
	})
}

func (t *TeamLead) recordChild(parentID, childID queue.TaskID) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.parentToChildren[parentID] = append(t.parentToChildren[parentID], childID)
}

func (t *TeamLead) aggregate(planRes RunResult, childResults []RunResult, dispatchErr error) RunResult {
	res := RunResult{
		Success:         planRes.Success,
		TokensUsed:      planRes.TokensUsed,
		CostUSD:         planRes.CostUSD,
		ToolUseCount:    planRes.ToolUseCount,
		CheckpointIDs:   append([]string(nil), planRes.CheckpointIDs...),
		Artifacts:       append([]string(nil), planRes.Artifacts...),
		FinalStopReason: planRes.FinalStopReason,
	}
	for _, c := range childResults {
		res.TokensUsed += c.TokensUsed
		res.CostUSD += c.CostUSD
		res.ToolUseCount += c.ToolUseCount
		res.CheckpointIDs = append(res.CheckpointIDs, c.CheckpointIDs...)
		res.Artifacts = append(res.Artifacts, c.Artifacts...)
		if !c.Success {
			res.Success = false
			if res.FailureReason == "" {
				res.FailureReason = c.FailureReason
			}
		}
	}
	if dispatchErr != nil {
		res.Success = false
		if res.FailureReason == "" {
			res.FailureReason = dispatchErr.Error()
		}
	}
	return res
}

func (t *TeamLead) finalizeParent(parentID queue.TaskID, res RunResult) {
	bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	row, err := t.stl.Get(bgCtx, parentID)
	if err != nil {
		return
	}
	target := queue.StatusReview
	if !res.Success {
		target = queue.StatusFailed
	}

	if row.Status == queue.StatusPending {
		_ = t.stl.Claim(bgCtx, parentID, t.session.ThreadID().String())
		row, _ = t.stl.Get(bgCtx, parentID)
	}
	if row.Status == queue.StatusInProgress {
		_ = t.stl.Advance(bgCtx, parentID, target)
	}
}

func (t *TeamLead) markParentFailed(parentID queue.TaskID, _ string) {
	bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	row, err := t.stl.Get(bgCtx, parentID)
	if err != nil {
		return
	}
	if row.Status == queue.StatusPending || row.Status == queue.StatusInProgress {
		_ = t.stl.Advance(bgCtx, parentID, queue.StatusFailed)
	}
}
