// SPDX-License-Identifier: MIT
// Package orchestrator confirmation_handler — release Tasks F-2 + F-3.
//
// ConfirmationHandler is the stateful coordinator wiring
// ConfirmationPolicy (F-1) + StateMachine + eventlog
// + release OperatorGate. RequestConfirmation evaluates the policy, and
// on a mandatory or optional pause verdict transitions
// RUNNING → WAITING_FOR_CONFIRMATION, locks the operator gate
// (PauseDescriptive), and emits an EvtConfirmationRequested event so
// downstream operator UI + release hash-chain replay can observe the
// pause.
//
// Race-safety (invariant spirit): all three side effects (state
// transition, gate Pause, eventlog Append) execute under h.mu so that
// concurrent RequestConfirmation calls see a single pendingRequest at a
// time. Subsequent callers that arrive while a request is pending
// receive the existing RequestSeq + EventID without producing duplicate
// state mutations or events. The full ack/deny race-safety surface
// (RequestSeq + EventID match validation, ErrConfirmationStale) lands in
// Task F-3.
//
// Partial-failure rollback: each side effect is paired with a deferred
// reverse operation so a leaked Pause cannot hang the orchestrator if
// the eventlog Append fails after the gate is locked. The deferred
// rollback path uses context.Background() (or a never-cancelling ctx)
// so a cancelled caller-ctx does not block cleanup.
//
// Invariants
// - invariant (race-safety): single pending request guarded by
// h.pending under h.mu (full enforcement in F-3 ack/deny).
// - invariant (boundary): this file does NOT import internal/store;
// it depends only on eventlog + workforce/gate.
// - Privacy IMP-3 carry-forward: Summary + Alternatives strings are
// emitted verbatim into the audit-trail event payload; callers MUST
// pre-redact secrets before passing them in.
package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
	"github.com/cbip-solutions/hades-system/internal/workforce/gate"
)

// ErrInvalidTransition is returned by RequestConfirmation (and, in F-3,
// HandleAck/HandleDeny) when the underlying StateMachine rejects the
// requested transition (e.g., the supervisor is already in StateAborting
// and Aborting → WaitingForConfirmation is not in TransitionTable).
// Callers that catch this error MUST NOT retry without first checking
// the supervisor state — a persistent ErrInvalidTransition usually means
// the operator already invoked Abort or the orchestrator is already in
// the requested target state.
var ErrInvalidTransition = errors.New("orchestrator: invalid state transition")

var ErrConfirmationStale = errors.New("orchestrator: stale confirmation")

type StateMachineAPI interface {
	Current() State
	Transition(ctx context.Context, to State, reason string) error
}

type AppenderAPI interface {
	Append(ctx context.Context, ev eventlog.Event) (int64, error)
}

type GateAPI interface {
	Pause(ctx context.Context, mode gate.PauseMode, reason string) error
	Resume(ctx context.Context) error
	State() gate.State
}

type RequestConfirmationInput struct {
	Class        DecisionClass
	Summary      string
	Alternatives []string
}

type RequestConfirmationOutput struct {
	Action     ConfirmationAction
	RequestSeq uint64
	EventID    int64
}

type pendingRequest struct {
	seq           uint64
	eventID       int64
	class         DecisionClass
	previousState State
	requestedAt   time.Time
}

type ConfirmationHandler struct {
	policy    *ConfirmationPolicy
	sm        StateMachineAPI
	ap        AppenderAPI
	g         GateAPI
	sessionID string
	projectID string

	mu      sync.Mutex
	seq     atomic.Uint64
	pending *pendingRequest
	now     func() time.Time
}

func NewConfirmationHandler(p *ConfirmationPolicy, sm StateMachineAPI, ap AppenderAPI, g GateAPI, sessionID, projectID string) *ConfirmationHandler {
	if p == nil {
		panic("orchestrator: NewConfirmationHandler: policy must not be nil")
	}
	if sm == nil {
		panic("orchestrator: NewConfirmationHandler: state machine must not be nil")
	}
	if ap == nil {
		panic("orchestrator: NewConfirmationHandler: appender must not be nil")
	}
	if g == nil {
		panic("orchestrator: NewConfirmationHandler: gate must not be nil")
	}
	if sessionID == "" {
		panic("orchestrator: NewConfirmationHandler: sessionID must not be empty")
	}
	if projectID == "" {
		panic("orchestrator: NewConfirmationHandler: projectID must not be empty")
	}
	return &ConfirmationHandler{
		policy:    p,
		sm:        sm,
		ap:        ap,
		g:         g,
		sessionID: sessionID,
		projectID: projectID,
		now:       time.Now,
	}
}

func (h *ConfirmationHandler) RequestConfirmation(ctx context.Context, in RequestConfirmationInput) (RequestConfirmationOutput, error) {
	if err := ctx.Err(); err != nil {
		return RequestConfirmationOutput{}, fmt.Errorf("orchestrator: RequestConfirmation: ctx cancelled before start: %w", err)
	}

	action := h.policy.Evaluate(in.Class, DecisionEvent{Class: in.Class, Summary: in.Summary})
	if action == ConfirmationActionContinue {
		return RequestConfirmationOutput{Action: ConfirmationActionContinue}, nil
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.pending != nil {
		return RequestConfirmationOutput{
			Action:     action,
			RequestSeq: h.pending.seq,
			EventID:    h.pending.eventID,
		}, nil
	}

	previous := h.sm.Current()

	transitionReason := "confirmation_requested:" + string(in.Class)
	if err := h.sm.Transition(ctx, StateWaitingForConfirmation, transitionReason); err != nil {
		return RequestConfirmationOutput{}, fmt.Errorf("%w: %w", ErrInvalidTransition, err)
	}

	rolledBack := false
	defer func() {
		if !rolledBack {
			return
		}
		cleanupCtx := context.WithoutCancel(ctx)

		_ = h.sm.Transition(cleanupCtx, previous, "confirmation_rollback:"+string(in.Class))
	}()

	gateReason := fmt.Sprintf("confirmation: %s", in.Class)
	if err := h.g.Pause(ctx, gate.PauseDescriptive, gateReason); err != nil {
		rolledBack = true
		return RequestConfirmationOutput{}, fmt.Errorf("orchestrator: gate pause: %w", err)
	}

	seq := h.seq.Add(1)
	payload := eventlog.ConfirmationRequested{
		EventID:       fmt.Sprintf("req-%d", seq),
		DecisionClass: string(in.Class),
		RequestSeq:    seq,
		Summary:       in.Summary,
		Alternatives:  in.Alternatives,
	}

	eventID, err := h.ap.Append(ctx, eventlog.Event{
		Type:      eventlog.EvtConfirmationRequested,
		SessionID: h.sessionID,
		ProjectID: h.projectID,
		Timestamp: h.now(),
		Payload: map[string]any{
			"event_id":       payload.EventID,
			"decision_class": payload.DecisionClass,
			"request_seq":    payload.RequestSeq,
			"summary":        payload.Summary,
			"alternatives":   payload.Alternatives,
		},
	})
	if err != nil {

		cleanupCtx := context.WithoutCancel(ctx)
		_ = h.g.Resume(cleanupCtx)
		rolledBack = true
		return RequestConfirmationOutput{}, fmt.Errorf("orchestrator: eventlog append: %w", err)
	}

	h.pending = &pendingRequest{
		seq:           seq,
		eventID:       eventID,
		class:         in.Class,
		previousState: previous,
		requestedAt:   h.now(),
	}

	return RequestConfirmationOutput{
		Action:     action,
		RequestSeq: seq,
		EventID:    eventID,
	}, nil
}

type OperatorIdentity struct {
	UID    int
	Reason string
}

type AckInput struct {
	EventID   int64
	Rationale string
	Operator  OperatorIdentity
}

type DenyInput struct {
	EventID   int64
	Rationale string
	Operator  OperatorIdentity
}

func (h *ConfirmationHandler) HandleAck(ctx context.Context, in AckInput) error {
	return h.handle(ctx, in.EventID, in.Rationale, in.Operator, true)
}

func (h *ConfirmationHandler) HandleDeny(ctx context.Context, in DenyInput) error {
	return h.handle(ctx, in.EventID, in.Rationale, in.Operator, false)
}

func (h *ConfirmationHandler) handle(ctx context.Context, eventID int64, rationale string, op OperatorIdentity, ack bool) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.pending == nil || h.pending.eventID != eventID {
		return ErrConfirmationStale
	}

	pending := h.pending

	decision := "deny"
	target := StateAborting
	transitionReason := "confirmation_deny"
	if ack {
		decision = "ack"
		target = pending.previousState
		transitionReason = "confirmation_ack"
	}

	_, err := h.ap.Append(ctx, eventlog.Event{
		Type:      eventlog.EvtOperatorConfirmation,
		SessionID: h.sessionID,
		ProjectID: h.projectID,
		Timestamp: h.now(),
		Payload: map[string]any{
			"event_id":     fmt.Sprintf("req-%d", pending.seq),
			"decision":     decision,
			"rationale":    rationale,
			"request_seq":  pending.seq,
			"operator_uid": op.UID,
		},
	})
	if err != nil {

		return fmt.Errorf("orchestrator: HandleAck/Deny append: %w", err)
	}

	if err := h.sm.Transition(ctx, target, transitionReason); err != nil {
		return fmt.Errorf("%w: handle transition: %w", ErrInvalidTransition, err)
	}

	if ack {
		if err := h.g.Resume(ctx); err != nil {

			return fmt.Errorf("orchestrator: gate resume: %w", err)
		}
	}

	h.pending = nil
	return nil
}
