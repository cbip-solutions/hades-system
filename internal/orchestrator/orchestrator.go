// SPDX-License-Identifier: MIT
// internal/orchestrator/orchestrator.go
//
// Package orchestrator owns the autonomous-orchestrator state machine
// , the depth/width decision (depth.go,
// D-4/D-5), the dispatcher integration with workforce
// (dispatcher.go, D-3), and the public RunStage4 entry point used by
// the daemon HTTP layer (D-2).
//
// The Orchestrator is the load-bearing supervisor of It owns
// one of each substrate primitive (clock, eventlog, state machine,
// worktree pool, dispatcher, research gate) by composition — it never
// reaches into them via package globals. New is pure construction
// (no I/O, no goroutines). Init is the lifecycle hook that validates
// dependencies and primes long-lived state. After Init returns nil the
// orchestrator is ready for RunStage4.
//
// D-2 implements the §3.1 RunStage4 lifecycle:
//
// 1. validate BuildRequest, then check Initialized() (fail-fast)
// 2. emit EvtOrchestratorStarted
// 3. Idle → Initializing
// 4. ResearchGate.Check (invariant enforcement; failure unwinds
// gracefully to Idle and returns ErrResearchGateNotPassed)
// 5. DecideWidth + DecideDepth (depth.go scaffolding;
// D-4/D-5 wholesale-replace with §5.3.2 formula)
// 6. emit EvtDepthWidthDecided
// 7. ConfirmationCallback (operator gate; deny unwinds to Idle)
// 8. Initializing → Running BEFORE blocking on Dispatch (so
// recovery sees Running rather than Initializing on crash)
// 9. Dispatcher.Dispatch (block until completion)
// 10. on success → Running → Idle, emit EvtOrchestratorStopped(success)
// 11. on dispatch error → Running → Aborting → Idle, with cleanup
// Dispatcher.Shutdown, emit EvtOrchestratorStopped(dispatch_failed)
//
// Boundaries
// - invariant: this package NEVER imports internal/store. Persistence
// flows through internal/daemon/orchestratoradapter.
// - invariant: this package NEVER imports internal/workforce/queue
// directly. workforce.Manager is wired via the Dispatcher
// interface (D-3) so eventlog (durable) ⊥ queue (transient) stays
// a clean separation.
//
// Sanctioned scaffolding remaining after D-2:
// - DecideWidth + DecideDepth (depth.go) — D-4/D-5 wholesale-replace
// with the spec §5.3.2 min-over-5-factors / ceil(log_W(N)) formulas.
// - Dispatcher concrete impl (dispatcher.go, D-3) — the interface is
// consumed here; tests inject a fake.
// - ResearchGate concrete impl (D-6) — same pattern.
// - Shutdown (this file) — D-7 wholesale-replace with graceful drain.
package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/worktreepool"
)

// Spec is the build-spec contract RunStage4 consumes. The full
// implementation lives outside this package ( phase boundaries
// keep Spec abstract here so the orchestrator never reaches into the
// daemon's domain types). The four methods are the minimal slice the
// orchestrator needs to size depth/width and propagate the request to
// Dispatcher.
//
// - Phases reports the number of plan phases. Used for depth-budget
// telemetry; not yet load-bearing in D-2 scaffolding but stable in
// the eventual D-4/D-5 §5.3.2 formula.
// - TaskCount reports the number of leaf tasks. Drives the
// DecideDepth log_W(N) calculation.
// - ParallelizableUpperBound returns the DAG-derived upper bound on
// concurrent task execution. MUST be ≥ 1; a value ≤ 0 yields
// orchestrator.ErrZeroWidth from DecideWidth (spec §5.3.2: any zero
// factor in the min-over-5-factors formula short-circuits to "no
// parallelism feasible"). Spec implementations should clamp to 1
// for trivially-serial DAGs.
// - DependencyDAG returns the dependency graph; consumed by D-3
// dispatch sequencing ( narrows the return type when the
// workforce.Manager surface is wired).
type Spec interface {
	Phases() int
	TaskCount() int
	ParallelizableUpperBound() int
	DependencyDAG() any
}

type DispatchDecisionEvent struct {
	Class   string
	Payload map[string]any
}

var (
	ErrInvalidConfig         = errors.New("orchestrator: invalid config")
	ErrAlreadyInitialized    = errors.New("orchestrator: already initialized")
	ErrNotInitialized        = errors.New("orchestrator: not initialized")
	ErrAlreadyShutdown       = errors.New("orchestrator: already shut down")
	ErrResearchGateNotPassed = errors.New("orchestrator: research gate not passed (inv-zen-101)")
	ErrInvalidBuildRequest   = errors.New("orchestrator: invalid build request")
)

type Dispatcher interface {
	Dispatch(ctx context.Context, req DispatchRequest) (DispatchResult, error)
	Shutdown(ctx context.Context) error
}

type ResearchGate interface {
	Check(ctx context.Context, sessionID string) error
}

type DispatchRequest struct {
	SessionID string
	ProjectID string
	Width     int
	Depth     int
	Doctrine  string
	Spec      Spec
}

type DispatchResult struct {
	WorkersSpawned int
	Completed      int
	Errors         int
	Aborted        int
}

type BuildRequest struct {
	SessionID            string
	ProjectID            string
	Doctrine             string
	Spec                 Spec
	Autonomy             string
	ConfirmationCallback func(ctx context.Context, decision DispatchDecisionEvent) error
}

type Config struct {
	Clock        clock.Clock
	EventLog     eventlog.Appender
	StateMachine *StateMachine
	Pool         worktreepool.Pool
	Dispatcher   Dispatcher
	Research     ResearchGate

	SessionID string

	ProjectID string

	DefaultDoctrine string

	// PoolCapacity is the orchestrator's view of the worktree pool's
	// concurrent-lease ceiling. worktreepool.Pool
	// interface intentionally omits a Capacity() method (the elastic
	// Floor..ElasticMax model is private to the pool implementation),
	// so the supervisor consumes its own configured capacity instead.
	// MUST be >= 1 — New rejects 0 or negative values with
	// ErrInvalidConfig. D-4 wholesale-replaces the consumer of this
	// field with the spec §5.3.2 formula (capacity is one of five
	// width-narrowing factors there).
	PoolCapacity int
}

type Orchestrator struct {
	cfg Config

	mu          sync.Mutex
	initialized bool
	shutdown    bool
	poolPrimed  bool
}

func New(cfg Config) (*Orchestrator, error) {
	if cfg.Clock == nil {
		return nil, fmt.Errorf("%w: clock is nil", ErrInvalidConfig)
	}
	if cfg.EventLog == nil {
		return nil, fmt.Errorf("%w: eventlog is nil", ErrInvalidConfig)
	}
	if cfg.StateMachine == nil {
		return nil, fmt.Errorf("%w: state machine is nil", ErrInvalidConfig)
	}
	if cfg.Pool == nil {
		return nil, fmt.Errorf("%w: worktree pool is nil", ErrInvalidConfig)
	}
	if cfg.Dispatcher == nil {
		return nil, fmt.Errorf("%w: dispatcher is nil", ErrInvalidConfig)
	}
	if cfg.Research == nil {
		return nil, fmt.Errorf("%w: research gate is nil", ErrInvalidConfig)
	}
	if strings.TrimSpace(cfg.SessionID) == "" {
		return nil, fmt.Errorf("%w: session id is empty", ErrInvalidConfig)
	}
	if strings.TrimSpace(cfg.ProjectID) == "" {
		return nil, fmt.Errorf("%w: project id is empty", ErrInvalidConfig)
	}
	if cfg.PoolCapacity < 1 {
		return nil, fmt.Errorf("%w: pool capacity %d (must be >= 1)", ErrInvalidConfig, cfg.PoolCapacity)
	}
	return &Orchestrator{cfg: cfg}, nil
}

func (o *Orchestrator) Init(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.shutdown {
		return ErrAlreadyShutdown
	}
	if o.initialized {
		return ErrAlreadyInitialized
	}
	o.poolPrimed = true
	o.initialized = true
	return nil
}

func (o *Orchestrator) PoolPrimed() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.poolPrimed
}

func (o *Orchestrator) Initialized() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.initialized
}

func (o *Orchestrator) State() State {
	return o.cfg.StateMachine.Current()
}

func (o *Orchestrator) RunStage4(ctx context.Context, req BuildRequest) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("orchestrator.RunStage4: ctx cancelled before start: %w", err)
	}
	if err := validateBuildRequest(req); err != nil {
		return err
	}
	if !o.Initialized() {
		return ErrNotInitialized
	}

	startedEv := eventlog.Event{
		Type:      eventlog.EvtOrchestratorStarted,
		SessionID: req.SessionID,
		ProjectID: req.ProjectID,
		Payload: map[string]any{
			"session_id":    req.SessionID,
			"project_id":    req.ProjectID,
			"autonomy_mode": req.Autonomy,
			"doctrine":      req.Doctrine,
		},
	}
	if _, err := o.cfg.EventLog.Append(ctx, startedEv); err != nil {
		return fmt.Errorf("orchestrator.RunStage4: append started: %w", err)
	}

	if err := o.transition(ctx, StateInitializing, "run-stage4-start"); err != nil {

		o.recordStopped(ctx, req, "transition_failed")
		return err
	}

	if gateErr := o.cfg.Research.Check(ctx, req.SessionID); gateErr != nil {
		o.unwind(ctx, req, "research_gate_failed", false)
		return fmt.Errorf("%w: %w", ErrResearchGateNotPassed, gateErr)
	}

	bounds := doctrineBoundsFor(req.Doctrine)
	width, err := DecideWidth(req, o.cfg.PoolCapacity, bounds)
	if err != nil {
		o.unwind(ctx, req, "decide_width_failed", false)
		return fmt.Errorf("orchestrator.RunStage4: decide width: %w", err)
	}
	depth, err := DecideDepth(req.Spec.TaskCount(), width, bounds.MaxDepth)
	if err != nil {
		o.unwind(ctx, req, "decide_depth_failed", false)
		return fmt.Errorf("orchestrator.RunStage4: decide depth: %w", err)
	}

	dwEv := eventlog.Event{
		Type:      eventlog.EvtDepthWidthDecided,
		SessionID: req.SessionID,
		ProjectID: req.ProjectID,
		Payload: map[string]any{
			"depth": depth,
			"width": width,
			"factors": map[string]any{
				"capacity":                   o.cfg.PoolCapacity,
				"doctrine_max_width":         bounds.MaxWidth,
				"doctrine_max_depth":         bounds.MaxDepth,
				"parallelizable_upper_bound": req.Spec.ParallelizableUpperBound(),
				"task_count":                 req.Spec.TaskCount(),
			},
			"rationale": "phase-d2-scaffolding",
		},
	}
	if _, err := o.cfg.EventLog.Append(ctx, dwEv); err != nil {
		o.unwind(ctx, req, "append_depth_width_failed", false)
		return fmt.Errorf("orchestrator.RunStage4: append depth/width: %w", err)
	}

	if req.ConfirmationCallback != nil {
		decision := DispatchDecisionEvent{
			Class: "depth-width",
			Payload: map[string]any{
				"depth": depth,
				"width": width,
			},
		}
		if cbErr := req.ConfirmationCallback(ctx, decision); cbErr != nil {
			o.unwind(ctx, req, "confirmation_denied", false)
			return fmt.Errorf("orchestrator.RunStage4: confirmation denied: %w", cbErr)
		}
	}

	if err := o.transition(ctx, StateRunning, "dispatch-begin"); err != nil {

		o.unwind(ctx, req, "transition_failed", false)
		return err
	}

	dispReq := DispatchRequest{
		SessionID: req.SessionID,
		ProjectID: req.ProjectID,
		Width:     width,
		Depth:     depth,
		Doctrine:  req.Doctrine,
		Spec:      req.Spec,
	}
	if _, dispErr := o.cfg.Dispatcher.Dispatch(ctx, dispReq); dispErr != nil {

		o.unwind(ctx, req, "dispatch_failed", true)
		return fmt.Errorf("orchestrator.RunStage4: dispatch: %w", dispErr)
	}

	if err := o.transition(ctx, StateIdle, "run-stage4-success"); err != nil {

		o.unwind(ctx, req, "transition_failed", true)
		return err
	}
	o.recordStopped(ctx, req, "success")
	return nil
}

func (o *Orchestrator) transition(ctx context.Context, to State, reason string) error {
	if err := o.cfg.StateMachine.Transition(ctx, to, reason); err != nil {
		return fmt.Errorf("orchestrator: transition to %s: %w", to, err)
	}
	return nil
}

func (o *Orchestrator) unwind(ctx context.Context, req BuildRequest, outcome string, fromRunning bool) {
	o.recordStopped(ctx, req, outcome)
	if fromRunning {
		_ = o.cfg.Dispatcher.Shutdown(context.Background())
		_ = o.transition(ctx, StateAborting, outcome+"-running-unwind")
	} else {
		_ = o.transition(ctx, StateAborting, outcome+"-unwind")
	}
	_ = o.transition(ctx, StateIdle, outcome+"-cleanup")
}

func (o *Orchestrator) recordStopped(ctx context.Context, req BuildRequest, outcome string) {
	ev := eventlog.Event{
		Type:      eventlog.EvtOrchestratorStopped,
		SessionID: req.SessionID,
		ProjectID: req.ProjectID,
		Payload: map[string]any{
			"outcome": outcome,
		},
	}

	appendCtx := ctx
	if appendCtx.Err() != nil {
		appendCtx = context.Background()
	}
	_, _ = o.cfg.EventLog.Append(appendCtx, ev)
}

func validateBuildRequest(req BuildRequest) error {
	if strings.TrimSpace(req.SessionID) == "" {
		return fmt.Errorf("%w: session id is empty", ErrInvalidBuildRequest)
	}
	if strings.TrimSpace(req.ProjectID) == "" {
		return fmt.Errorf("%w: project id is empty", ErrInvalidBuildRequest)
	}
	if strings.TrimSpace(req.Doctrine) == "" {
		return fmt.Errorf("%w: doctrine is empty", ErrInvalidBuildRequest)
	}
	if req.Spec == nil {
		return fmt.Errorf("%w: spec is nil", ErrInvalidBuildRequest)
	}
	if req.Spec.TaskCount() <= 0 {
		return fmt.Errorf("%w: task_count must be > 0", ErrInvalidBuildRequest)
	}
	return nil
}

func (o *Orchestrator) Shutdown(ctx context.Context) error {
	o.mu.Lock()
	if !o.initialized && !o.shutdown {

		o.shutdown = true
		o.mu.Unlock()
		return nil
	}
	if o.shutdown {

		o.mu.Unlock()
		return nil
	}
	o.mu.Unlock()

	_ = o.cfg.Dispatcher.Shutdown(ctx)

	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
waitLoop:
	for {
		if o.cfg.StateMachine.Current() == StateIdle {
			break waitLoop
		}
		select {
		case <-ctx.Done():

			from := o.cfg.StateMachine.Current()
			o.forceUnwindToIdle(context.Background(), "forced_shutdown")

			_, _ = o.cfg.EventLog.Append(context.Background(), eventlog.Event{
				Type:      eventlog.EvtOrchestratorStopped,
				SessionID: o.cfg.SessionID,
				ProjectID: o.cfg.ProjectID,
				Timestamp: o.cfg.Clock.Now(),
				Payload: map[string]any{
					"outcome": "forced_shutdown",
					"from":    from.String(),
				},
			})
			break waitLoop
		case <-tick.C:

		}
	}

	o.mu.Lock()
	o.shutdown = true

	o.mu.Unlock()
	return nil
}

func (o *Orchestrator) forceUnwindToIdle(ctx context.Context, outcome string) {
	switch current := o.cfg.StateMachine.Current(); current {
	case StateIdle:

		return
	case StateRunning, StateInitializing:

		_ = o.cfg.StateMachine.Transition(ctx, StateAborting, outcome+"-force-abort")
		_ = o.cfg.StateMachine.Transition(ctx, StateIdle, outcome+"-force-idle")
	case StateAborting:
		_ = o.cfg.StateMachine.Transition(ctx, StateIdle, outcome+"-force-idle")
	default:

		_ = o.cfg.StateMachine.Transition(ctx, StateAborting, outcome+"-force-abort")
		_ = o.cfg.StateMachine.Transition(ctx, StateIdle, outcome+"-force-idle")
	}
}
