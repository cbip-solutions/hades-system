// SPDX-License-Identifier: MIT
// internal/orchestrator/recovery.go
package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

type FailureClass int

const (
	FailureTransientLLM FailureClass = iota + 1

	FailureTransientInfra

	FailurePermanentTask

	FailurePermanentInfra
)

func (f FailureClass) String() string {
	switch f {
	case FailureTransientLLM:
		return "TRANSIENT_LLM"
	case FailureTransientInfra:
		return "TRANSIENT_INFRA"
	case FailurePermanentTask:
		return "PERMANENT_TASK"
	case FailurePermanentInfra:
		return "PERMANENT_INFRA"
	default:
		return "UNKNOWN"
	}
}

var (
	ErrHeartbeatTimeout = errors.New("worker/reviewer heartbeat timeout")

	ErrAllTiersDown = errors.New("all dispatch tiers exhausted (no LLM reachable)")
)

type HTTPStatusError struct {
	Code     int
	Endpoint string
}

func (e *HTTPStatusError) Error() string {
	return e.Endpoint + ": http " + strconv.Itoa(e.Code)
}

type LLMCallError struct{ Inner error }

func (e *LLMCallError) Error() string { return "llm call: " + e.Inner.Error() }
func (e *LLMCallError) Unwrap() error { return e.Inner }

type WorkerSubprocessError struct {
	Reason   string
	ExitCode int
}

func (e *WorkerSubprocessError) Error() string {
	return "worker subprocess: " + e.Reason
}

type QueueWriteError struct{ Inner error }

func (e *QueueWriteError) Error() string { return "queue write: " + e.Inner.Error() }
func (e *QueueWriteError) Unwrap() error { return e.Inner }

type DiskFullError struct{ Path string }

func (e *DiskFullError) Error() string { return "disk full at " + e.Path }

type RepoCorruptError struct{ Repo string }

func (e *RepoCorruptError) Error() string { return "repo corrupt: " + e.Repo }

type AuditWriteError struct{ Inner error }

func (e *AuditWriteError) Error() string { return "audit write: " + e.Inner.Error() }
func (e *AuditWriteError) Unwrap() error { return e.Inner }

func Classify(err error) FailureClass {

	if err == nil {
		return FailurePermanentInfra
	}

	if errors.Is(err, ErrHeartbeatTimeout) {
		return FailureTransientInfra
	}

	if errors.Is(err, ErrAllTiersDown) {
		return FailurePermanentInfra
	}

	var hse *HTTPStatusError
	if errors.As(err, &hse) {
		switch {
		case hse.Code >= 500 && hse.Code < 600:
			return FailureTransientLLM
		case hse.Code == 429:
			return FailureTransientLLM
		case hse.Code == 401 || hse.Code == 403:
			return FailurePermanentInfra
		case hse.Code >= 400 && hse.Code < 500:
			return FailurePermanentTask
		}
	}

	var llmErr *LLMCallError
	if errors.As(err, &llmErr) {
		if errors.Is(llmErr.Inner, context.DeadlineExceeded) {
			return FailureTransientLLM
		}
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return FailureTransientLLM
	}

	var wse *WorkerSubprocessError
	if errors.As(err, &wse) {
		return FailureTransientInfra
	}

	var qwe *QueueWriteError
	if errors.As(err, &qwe) {
		return FailureTransientInfra
	}

	var dfe *DiskFullError
	if errors.As(err, &dfe) {
		return FailurePermanentInfra
	}

	var rce *RepoCorruptError
	if errors.As(err, &rce) {
		return FailurePermanentInfra
	}

	var awe *AuditWriteError
	if errors.As(err, &awe) {
		return FailurePermanentInfra
	}

	return FailurePermanentInfra
}

type RecoveryAction int

const (
	RecoveryActionRedispatchSameTier RecoveryAction = iota + 1

	RecoveryActionRedispatchNextTier

	RecoveryActionEscalateL4

	RecoveryActionSkipTask

	RecoveryActionWaitForConfirmation

	RecoveryActionHardPause
)

func (a RecoveryAction) String() string {
	switch a {
	case RecoveryActionRedispatchSameTier:
		return "redispatch_same_tier"
	case RecoveryActionRedispatchNextTier:
		return "redispatch_next_tier"
	case RecoveryActionEscalateL4:
		return "escalate_l4"
	case RecoveryActionSkipTask:
		return "skip_task"
	case RecoveryActionWaitForConfirmation:
		return "wait_for_confirmation"
	case RecoveryActionHardPause:
		return "hard_pause"
	default:
		return "unknown"
	}
}

type WorkerDeathInput struct {
	TaskID      string
	WorkerID    string
	Err         error
	TierIndex   int
	CausalChain []string
}

type Decision struct {
	Action       RecoveryAction
	Class        FailureClass
	NewTierIndex int
	RetryCount   int
	Reason       string
}

type DoctrineView interface {
	Name() string
	TransientLLMRetries() int
	TransientInfraRetries() int
	PermanentAfterNRetries() int
	OnExhaustAction(class FailureClass) RecoveryAction
	TierFallbackPolicy() TierFallbackPolicy
}

type TierFallbackPolicy int

const (
	TierFallbackFullChain TierFallbackPolicy = iota + 1

	TierFallbackPartial

	TierFallbackNone
)

type TierChainView interface {
	Len() int

	NextTier(currentIndex int, policy TierFallbackPolicy) (next int, ok bool)
}

type RecoveryEngineConfig struct {
	Doctrine  DoctrineView
	EventLog  *eventlog.Log
	TierChain TierChainView
	Clock     clock.Clock
	ProjectID string
	SessionID string
}

type retryKey struct {
	taskID string
	class  FailureClass
}

type RecoveryEngine struct {
	doc       DoctrineView
	evlog     *eventlog.Log
	tierChain TierChainView
	clk       clock.Clock
	projectID string
	sessionID string

	mu          sync.Mutex
	retries     map[retryKey]int
	cumulative  map[retryKey]int
	corruptHits int
}

func NewRecoveryEngine(cfg RecoveryEngineConfig) (*RecoveryEngine, error) {
	if cfg.Doctrine == nil {
		return nil, fmt.Errorf("%w: doctrine is nil", ErrInvalidConfig)
	}
	if cfg.EventLog == nil {
		return nil, fmt.Errorf("%w: eventlog is nil", ErrInvalidConfig)
	}
	clk := cfg.Clock
	if clk == nil {
		clk = clock.Real{}
	}
	return &RecoveryEngine{
		doc:        cfg.Doctrine,
		evlog:      cfg.EventLog,
		tierChain:  cfg.TierChain,
		clk:        clk,
		projectID:  cfg.ProjectID,
		sessionID:  cfg.SessionID,
		retries:    make(map[retryKey]int),
		cumulative: make(map[retryKey]int),
	}, nil
}

// HandleWorkerDeath classifies the error, applies the per-doctrine retry
// budget, emits EvtWorkerRedispatched (only on retry actions), and returns
// the engine's Decision. The caller's state machine enacts the Decision —
// re-dispatching, transitioning to WAITING_FOR_CONFIRMATION, escalating to
// L4, etc. RecoveryEngine itself never emits terminal events (HardPause,
// EscalateL4, SkipTask, WaitForConfirmation are emitted by the caller).
//
// Lock discipline (review fix-pass): the decision is computed under r.mu
// so concurrent same-(task,class) deaths cannot double-count, but the
// audit Append is emitted OUTSIDE the lock. eventlog.Log.Append
// performs SQLite persistence I/O; holding r.mu across that call would
// serialize every other RecoveryEngine call behind audit-write latency.
// computeDecisionLocked + handleTransientLocked encapsulate the
// must-hold-mu logic.
//
// Audit-trail discipline (D-2/D-3 carry-forward): the EvtWorkerRedispatched
// emission uses context.WithoutCancel(ctx) so a cancelled caller-ctx never
// drops the forensic row. The Decision return value is unaffected by ctx.
//
// Payload schema (review fix-pass): the payload map keys MUST match the
// eventlog.WorkerRedispatched typed-struct json tags so replay
// + release hash-chain consumers can typed-Decode the row without losing
// fields. The contract is pinned by
// TestRecoveryEngine_HandleWorkerDeath_TypedPayloadRoundTrip.
func (r *RecoveryEngine) HandleWorkerDeath(ctx context.Context, in WorkerDeathInput) (Decision, error) {

	r.mu.Lock()
	class := Classify(in.Err)
	dec := r.computeDecisionLocked(class, in)
	r.mu.Unlock()

	if dec.Action == RecoveryActionRedispatchSameTier || dec.Action == RecoveryActionRedispatchNextTier {

		auditCtx := context.WithoutCancel(ctx)
		// Map keys MUST match eventlog.WorkerRedispatched json tags so a
		// typed Decode round-trip yields all fields populated (replay
		// + release hash-chain contract).
		_, _ = r.evlog.Append(auditCtx, eventlog.Event{
			Type:        eventlog.EvtWorkerRedispatched,
			SessionID:   r.sessionID,
			ProjectID:   r.projectID,
			Timestamp:   r.clk.Now(),
			CausalChain: in.CausalChain,
			Payload: map[string]any{
				"task_id":        in.TaskID,
				"worker_id":      in.WorkerID,
				"class":          dec.Class.String(),
				"action":         dec.Action.String(),
				"new_tier_index": dec.NewTierIndex,
				"retry_count":    dec.RetryCount,
				"reason":         dec.Reason,
			},
		})
	}
	return dec, nil
}

// computeDecisionLocked dispatches by FailureClass to compute the
// Decision the caller's state machine will enact. The PERMANENT_INFRA /
// PERMANENT_TASK cases short-circuit; transient classes go through
// handleTransientLocked which applies the per-doctrine retry budget.
//
// Caller MUST hold r.mu. The "Locked" suffix is the Go convention
// signalling "must be called with the receiver's mutex held".
func (r *RecoveryEngine) computeDecisionLocked(class FailureClass, in WorkerDeathInput) Decision {
	dec := Decision{Class: class, NewTierIndex: in.TierIndex}
	switch class {
	case FailurePermanentInfra:
		dec.Action = RecoveryActionHardPause
		dec.Reason = "permanent_infra: " + errString(in.Err)
	case FailurePermanentTask:
		dec.Action = r.doc.OnExhaustAction(class)
		dec.Reason = "permanent_task: " + errString(in.Err)
	case FailureTransientLLM, FailureTransientInfra:
		dec = r.handleTransientLocked(class, in)
	}
	return dec
}

// handleTransientLocked applies the per-doctrine retry budget for
// TRANSIENT_LLM and TRANSIENT_INFRA classes. Tier fallback is reserved
// for TRANSIENT_LLM (TRANSIENT_INFRA stays on the same tier across the
// budget; the infra problem is local, not tier-specific).
//
// Two counters drive this function (see RecoveryEngine docstring):
// - r.retries[key]: PER-TIER counter. Resets to 0 on
// RedispatchNextTier so each tier walks its own budget.
// - r.cumulative[key]: NEVER-RESET counter. Drives the reclassify-
// permanent gate so sustained transient failure across the tier
// chain eventually flips to PERMANENT_TASK once cumulative count
// reaches doctrine.permanent_after_n_retries.
//
// Decision.RetryCount surfaces the CUMULATIVE counter (the operator-
// facing audit value should reflect actual progression through the
// retry envelope, not the per-tier walk).
//
// Caller MUST hold r.mu.
func (r *RecoveryEngine) handleTransientLocked(class FailureClass, in WorkerDeathInput) Decision {
	key := retryKey{taskID: in.TaskID, class: class}
	r.retries[key]++
	r.cumulative[key]++
	count := r.retries[key]
	totalCount := r.cumulative[key]

	budget := r.doc.TransientLLMRetries()
	if class == FailureTransientInfra {
		budget = r.doc.TransientInfraRetries()
	}

	dec := Decision{Class: class, NewTierIndex: in.TierIndex, RetryCount: totalCount}

	if count <= budget {
		dec.Action = RecoveryActionRedispatchSameTier
		dec.Reason = "within_budget"
		return dec
	}

	if class == FailureTransientLLM {
		policy := r.doc.TierFallbackPolicy()
		if policy != TierFallbackNone && r.tierChain != nil {
			if next, ok := r.tierChain.NextTier(in.TierIndex, policy); ok {

				r.retries[key] = 0
				dec.Action = RecoveryActionRedispatchNextTier
				dec.NewTierIndex = next
				dec.Reason = "tier_fallback"
				return dec
			}
		}
	}

	if totalCount >= r.doc.PermanentAfterNRetries() {
		dec.Class = FailurePermanentTask
		dec.Action = r.doc.OnExhaustAction(FailurePermanentTask)
		dec.Reason = "reclassified_permanent_task"
		return dec
	}

	dec.Action = r.doc.OnExhaustAction(class)
	dec.Reason = "exhausted_no_fallback"
	return dec
}

func (r *RecoveryEngine) OnCorruption(_ context.Context) RecoveryAction {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.corruptHits++
	if r.corruptHits > 5 {
		return RecoveryActionHardPause
	}
	return RecoveryActionRedispatchSameTier
}

func errString(err error) string {
	if err == nil {
		return "<nil>"
	}
	return err.Error()
}

func (r *RecoveryEngine) LastAssignmentFor(ctx context.Context, workerID string) string {
	if workerID == "" {
		return ""
	}
	records, err := r.evlog.Query(ctx, r.sessionID, 0)
	if err != nil {

		return ""
	}

	for i := len(records) - 1; i >= 0; i-- {
		rec := records[i]
		if rec.EventType != eventlog.EvtWorkerDispatched {
			continue
		}
		dec, err := eventlog.Decode(rec.EventType, rec.Payload)
		if err != nil {

			continue
		}

		wd := dec.(eventlog.WorkerDispatched)
		if wd.WorkerID == workerID {
			return wd.TaskID
		}
	}
	return ""
}

func AdaptTierChain(tiers []string, partialStopBefore int) TierChainView {
	chainLen := len(tiers)

	stop := partialStopBefore
	if stop <= 0 || stop > chainLen {
		stop = chainLen
	}
	return &tierChainAdapter{
		tiers:             tiers,
		partialStopBefore: stop,
	}
}

type tierChainAdapter struct {
	tiers             []string
	partialStopBefore int
}

func (a *tierChainAdapter) Len() int { return len(a.tiers) }

func (a *tierChainAdapter) NextTier(current int, policy TierFallbackPolicy) (int, bool) {
	if len(a.tiers) == 0 {
		return 0, false
	}
	switch policy {
	case TierFallbackNone:
		return current, false
	case TierFallbackFullChain:
		next := current + 1
		if next < len(a.tiers) {
			return next, true
		}
		return current, false
	case TierFallbackPartial:
		next := current + 1
		if next < a.partialStopBefore {
			return next, true
		}
		return current, false
	default:

		return current, false
	}
}
