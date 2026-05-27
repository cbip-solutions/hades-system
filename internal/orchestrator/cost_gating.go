// SPDX-License-Identifier: MIT
// Package orchestrator — cost-gating engine.
//
// G-1 ships:
// - CostProfile value type + 3 canonical built-in fixtures (max-scope,
// default, capa-firewall) per spec §1 Q9 D
// - LoadThresholdTable(profile, override?) — closed-vocab validation,
// per-project tighten-only override, sort by Pct ascending with
// PctPAYG last
// - CostGatingEngine struct + NewCostGatingEngine constructor with
// dep-validation + defaults (pollEvery=500ms; atomTimeout=30s)
// - Forward-declared interfaces consumed by G-2/G-3/G-4:
// BudgetSnapshotReader, WorkerSet, OrchestratorActuator
// - inv-hades-092 compile anchor (atomicityGuardEnforced); runtime
// enforcement lands in G-4
//
// Adaptation notes vs plan code:
// - release doctrine package is interface-based (Doctrine interface),
// not value-typed (Profile struct). (TOML loader for the
// cost_degradation stanza) hasn't shipped. owns its own
// CostProfile + BuiltinCostProfile fixtures matching spec §1 Q9 D
// verbatim. When ships, BuiltinCostProfile is replaced by
// a TOML loader returning the same shape; engine code stays put.
// - Type renamed from "Action" to "CostAction" for prefix consistency
// with the CostAction* constants.
// - ThresholdRow.Action field name aligned to test assertion shape
// (`{Pct, Action}`).
// - Constructor takes a *Config struct (recovery / heartbeat / etc.
// pattern) and validates non-nil deps + non-empty session/project
// IDs, returning wrapped ErrInvalidConfig per package convention.
package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

var jsonUnmarshal = json.Unmarshal

var _ = atomicityGuardEnforced

var atomicityGuardEnforced = struct{}{}

type Pct int

const PctPAYG Pct = -1

type CostAction string

const (
	CostActionContinue CostAction = "continue"

	CostActionDropL3Strategic CostAction = "drop_l3_strategic"

	CostActionTierDegradeL2 CostAction = "tier_degrade_l2"

	CostActionTierDegradeL1L2 CostAction = "tier_degrade_l1_l2"

	CostActionReduceParallelism CostAction = "reduce_parallelism"

	CostActionHardPause CostAction = "hard_pause"

	CostActionEmergencyOnlyTier CostAction = "emergency_only_tier"

	CostActionEscalateL4 CostAction = "escalate_l4"

	CostActionWaitingForConfirmation CostAction = "waiting_for_confirmation"

	CostActionWaiting CostAction = "waiting"
)

// knownActions is the closed-vocab membership set for closed-vocabulary
// validation. MUST stay in sync with severityRank (one entry per action).
var knownActions = map[CostAction]struct{}{
	CostActionContinue:               {},
	CostActionDropL3Strategic:        {},
	CostActionTierDegradeL2:          {},
	CostActionTierDegradeL1L2:        {},
	CostActionReduceParallelism:      {},
	CostActionHardPause:              {},
	CostActionEmergencyOnlyTier:      {},
	CostActionEscalateL4:             {},
	CostActionWaitingForConfirmation: {},
	CostActionWaiting:                {},
}

var severityRank = map[CostAction]int{
	CostActionContinue:               0,
	CostActionEscalateL4:             1,
	CostActionDropL3Strategic:        2,
	CostActionTierDegradeL2:          3,
	CostActionTierDegradeL1L2:        4,
	CostActionReduceParallelism:      5,
	CostActionEmergencyOnlyTier:      6,
	CostActionWaitingForConfirmation: 7,
	CostActionWaiting:                8,
	CostActionHardPause:              9,
}

type ThresholdRow struct {
	Pct    Pct
	Action CostAction
}

type ProjectOverride struct {
	ActionsByPct map[string]string
}

// CostProfile is the per-doctrine cost-degradation table consumed by
// Three canonical built-ins (BuiltinCostProfile) match spec
// §1 Q9 D verbatim. (when shipped) replaces BuiltinCostProfile
// with a TOML loader returning the same shape — engine code untouched.
//
// Fields
// - DoctrineName: "max-scope" | "default" | "capa-firewall"
// - Actions: threshold-key → CostAction string. Keys MUST be exactly
// {"60","80","90","100","payg"}; missing keys yield ErrMissingCostActionRow.
// Unknown action strings yield ErrUnknownCostAction.
// - AtomicityTimeout: per-doctrine cap on the atomic-boundary wait
// before warn-and-proceed (G-4). Zero falls back to 30s in
// NewCostGatingEngine.
// - RecoveryStepInterval: per-doctrine cadence for gradual restoration
// (G-6). Set to spec-default 60s for all three built-ins.
type CostProfile struct {
	DoctrineName         string
	Actions              map[string]string
	AtomicityTimeout     time.Duration
	RecoveryStepInterval time.Duration
}

type BudgetSnapshot struct {
	CumulativeUSD   float64
	DailyCapUSD     float64
	ProjectedEODUSD float64
	PAYGActive      bool

	ProjectID    string
	DoctrineName string
}

type BudgetSnapshotReader interface {
	Snapshot(ctx context.Context) (BudgetSnapshot, error)
}

type WorkerSet interface {
	WaitAtomicBoundary(ctx context.Context) <-chan struct{}
}

type OrchestratorActuator interface {
	DropAtDepth(ctx context.Context, layer int) error
	SetTier(ctx context.Context, maxTier int) error
	SetParallelism(ctx context.Context, depthCap int, widthCap int) error
	HardPause(ctx context.Context, reason string) error
	EmergencyOnlyTier(ctx context.Context) error
	EscalateL4(ctx context.Context, payload map[string]any) error
	WaitForConfirmation(ctx context.Context, decisionID string) error
	Waiting(ctx context.Context, reason string) error
	RestoreDefaults(ctx context.Context) error
}

// Sentinel errors. All are wrapped with %w by callers; consumers MUST
// use errors.Is for matching. Privacy contract (IMP-3 carry-forward):
// wrapped messages name the offending key/value but never echo
// secret-shaped bytes.
var (
	ErrUnknownCostAction = errors.New("orchestrator: unknown cost-gating action")

	ErrTightenOnlyViolation = errors.New("orchestrator: project override loosens doctrine action (tighten-only enforced)")

	ErrMissingCostActionRow = errors.New("orchestrator: doctrine missing action for threshold")

	ErrUnknownDoctrine = errors.New("orchestrator: unknown doctrine")
)

var thresholdKeyOrder = []string{"60", "80", "90", "100", "payg"}

func BuiltinCostProfile(doctrineName string) (CostProfile, error) {
	switch doctrineName {
	case "max-scope":
		return CostProfile{
			DoctrineName: "max-scope",
			Actions: map[string]string{
				"60":   string(CostActionDropL3Strategic),
				"80":   string(CostActionTierDegradeL2),
				"90":   string(CostActionReduceParallelism),
				"100":  string(CostActionHardPause),
				"payg": string(CostActionEmergencyOnlyTier),
			},
			AtomicityTimeout:     30 * time.Second,
			RecoveryStepInterval: 60 * time.Second,
		}, nil
	case "default":
		return CostProfile{
			DoctrineName: "default",
			Actions: map[string]string{
				"60":   string(CostActionContinue),
				"80":   string(CostActionTierDegradeL1L2),
				"90":   string(CostActionHardPause),
				"100":  string(CostActionHardPause),
				"payg": string(CostActionHardPause),
			},
			AtomicityTimeout:     30 * time.Second,
			RecoveryStepInterval: 60 * time.Second,
		}, nil
	case "capa-firewall":
		return CostProfile{
			DoctrineName: "capa-firewall",
			Actions: map[string]string{
				"60":   string(CostActionEscalateL4),
				"80":   string(CostActionWaitingForConfirmation),
				"90":   string(CostActionWaiting),
				"100":  string(CostActionHardPause),
				"payg": string(CostActionHardPause),
			},
			AtomicityTimeout:     30 * time.Second,
			RecoveryStepInterval: 60 * time.Second,
		}, nil
	}
	return CostProfile{}, fmt.Errorf("%w: %q (want max-scope|default|capa-firewall)",
		ErrUnknownDoctrine, doctrineName)
}

func pctFromKey(k string) Pct {
	if k == "payg" {
		return PctPAYG
	}
	n, err := strconv.Atoi(k)
	if err != nil {
		panic(fmt.Sprintf("orchestrator: pctFromKey: non-numeric threshold key %q (thresholdKeyOrder drift)", k))
	}
	return Pct(n)
}

// LoadThresholdTable reads the per-doctrine action map from the
// profile, applies an optional per-project tighten-only override,
// validates the closed action vocabulary, and returns rows sorted by
// Pct ascending with PctPAYG last.
//
// Validation order (fail-fast):
// 1. Each canonical key in thresholdKeyOrder MUST be present in
// p.Actions; missing → wrapped ErrMissingCostActionRow.
// 2. If override has an entry for the key, the override value MUST
// parse to a known CostAction; unknown → wrapped ErrUnknownCostAction.
// 3. The override's severityRank MUST be ≥ the doctrine's
// severityRank; lower → wrapped ErrTightenOnlyViolation.
// 4. The final action (after override resolution) MUST be a member of
// knownActions; unknown → wrapped ErrUnknownCostAction.
//
// Sort ascending by Pct numeric value; PctPAYG last (sentinel branch).
func LoadThresholdTable(p CostProfile, ov *ProjectOverride) ([]ThresholdRow, error) {
	if p.Actions == nil {
		return nil, fmt.Errorf("%w: doctrine=%q (Actions map is nil)",
			ErrMissingCostActionRow, p.DoctrineName)
	}
	rows := make([]ThresholdRow, 0, len(thresholdKeyOrder))
	for _, k := range thresholdKeyOrder {
		raw, ok := p.Actions[k]
		if !ok {
			return nil, fmt.Errorf("%w: doctrine=%q key=%q",
				ErrMissingCostActionRow, p.DoctrineName, k)
		}
		act := CostAction(raw)
		if _, known := knownActions[act]; !known {
			return nil, fmt.Errorf("%w: %q (doctrine=%q key=%q)",
				ErrUnknownCostAction, raw, p.DoctrineName, k)
		}

		if ov != nil && ov.ActionsByPct != nil {
			if oraw, ok := ov.ActionsByPct[k]; ok {
				oact := CostAction(oraw)
				if _, oknown := knownActions[oact]; !oknown {
					return nil, fmt.Errorf("%w: %q (override key=%q)",
						ErrUnknownCostAction, oraw, k)
				}
				if severityRank[oact] < severityRank[act] {
					return nil, fmt.Errorf(
						"%w: doctrine=%s pct=%s doctrine_action=%s project_action=%s",
						ErrTightenOnlyViolation, p.DoctrineName, k, act, oact)
				}
				act = oact
			}
		}
		row := ThresholdRow{Action: act, Pct: pctFromKey(k)}
		rows = append(rows, row)
	}

	sort.SliceStable(rows, func(i, j int) bool {
		return thresholdLess(rows[i].Pct, rows[j].Pct)
	})
	return rows, nil
}

func thresholdLess(a, b Pct) bool {
	if a == PctPAYG {
		return false
	}
	if b == PctPAYG {
		return true
	}
	return a < b
}

type CostGatingEngine struct {
	clk         clock.Clock
	emitter     eventlog.Appender
	budget      BudgetSnapshotReader
	workers     WorkerSet
	actuator    OrchestratorActuator
	profile     CostProfile
	override    *ProjectOverride
	pollEvery   time.Duration
	atomTimeout time.Duration
	sessionID   string
	projectID   string

	mu             sync.Mutex
	table          []ThresholdRow
	currentRow     *ThresholdRow
	stoppedCh      chan struct{}
	recoveryActive bool
}

type CostGatingEngineConfig struct {
	Clock     clock.Clock
	EventLog  eventlog.Appender
	Budget    BudgetSnapshotReader
	Workers   WorkerSet
	Actuator  OrchestratorActuator
	Profile   CostProfile
	Override  *ProjectOverride
	PollEvery time.Duration
	SessionID string
	ProjectID string
}

func NewCostGatingEngine(cfg CostGatingEngineConfig) (*CostGatingEngine, error) {
	if cfg.Clock == nil {
		return nil, fmt.Errorf("%w: clock is nil", ErrInvalidConfig)
	}
	if cfg.EventLog == nil {
		return nil, fmt.Errorf("%w: eventlog is nil", ErrInvalidConfig)
	}
	if cfg.Budget == nil {
		return nil, fmt.Errorf("%w: budget reader is nil", ErrInvalidConfig)
	}
	if cfg.Workers == nil {
		return nil, fmt.Errorf("%w: worker set is nil", ErrInvalidConfig)
	}
	if cfg.Actuator == nil {
		return nil, fmt.Errorf("%w: actuator is nil", ErrInvalidConfig)
	}
	if cfg.SessionID == "" {
		return nil, fmt.Errorf("%w: session id is empty", ErrInvalidConfig)
	}
	if cfg.ProjectID == "" {
		return nil, fmt.Errorf("%w: project id is empty", ErrInvalidConfig)
	}
	tab, err := LoadThresholdTable(cfg.Profile, cfg.Override)
	if err != nil {
		return nil, err
	}
	pollEvery := cfg.PollEvery
	if pollEvery == 0 {
		pollEvery = 500 * time.Millisecond
	}
	atomTimeout := cfg.Profile.AtomicityTimeout
	if atomTimeout == 0 {
		atomTimeout = 30 * time.Second
	}
	return &CostGatingEngine{
		clk:         cfg.Clock,
		emitter:     cfg.EventLog,
		budget:      cfg.Budget,
		workers:     cfg.Workers,
		actuator:    cfg.Actuator,
		profile:     cfg.Profile,
		override:    cfg.Override,
		pollEvery:   pollEvery,
		atomTimeout: atomTimeout,
		sessionID:   cfg.SessionID,
		projectID:   cfg.ProjectID,
		table:       tab,
		stoppedCh:   make(chan struct{}),
	}, nil
}

func (e *CostGatingEngine) Evaluate(s BudgetSnapshot) ThresholdRow {

	if s.PAYGActive {
		for _, r := range e.table {
			if r.Pct == PctPAYG {
				return r
			}
		}

		return ThresholdRow{Pct: 0, Action: CostActionContinue}
	}

	if s.DailyCapUSD <= 0 {
		return ThresholdRow{Pct: 0, Action: CostActionContinue}
	}
	cum := s.CumulativeUSD
	if cum < 0 {
		cum = 0
	}
	pct := int((cum / s.DailyCapUSD) * 100)

	var match ThresholdRow
	matched := false
	for _, r := range e.table {
		if r.Pct == PctPAYG {
			break
		}
		if int(r.Pct) <= pct {
			match = r
			matched = true
		}
	}
	if !matched {
		return ThresholdRow{Pct: 0, Action: CostActionContinue}
	}
	return match
}

// Run is the evaluator goroutine. Polls BudgetSnapshotReader.Snapshot
// on pollEvery cadence, computes the active threshold via Evaluate,
// and on transition (current row changed) calls Apply. Blocks until
// ctx is cancelled. Closes stoppedCh on return so callers can wait
// deterministically via Stopped().
//
// Single-instance contract: Run MUST NOT be called concurrently on the
// same engine. The stoppedCh is single-use; a second Run would
// double-close. The orchestrator boot path calls Run exactly once.
//
// Failure handling:
// - Snapshot returns error → emit EvtBudgetSnapshotError via
// context.WithoutCancel(ctx) so the audit row survives caller
// cancellation. Skip Apply for this tick; loop continues.
// - Apply returns error → emit EvtBudgetDegradationFailed (also
// WithoutCancel). Do NOT update currentRow so the next same-snapshot
// tick retries Apply. This makes Apply effectively at-least-once.
//
// The ticker fires at pollEvery (default 500ms; <1s is a hard
// requirement for sub-second cap-cross detection per spec §1 Q9).
func (e *CostGatingEngine) Run(ctx context.Context) {
	defer close(e.stoppedCh)
	tick := e.clk.NewTicker(e.pollEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C():
			e.tickOnce(ctx)
		}
	}
}

func (e *CostGatingEngine) tickOnce(ctx context.Context) {
	snap, err := e.budget.Snapshot(ctx)
	if err != nil {
		e.emitSnapshotError(ctx, err)
		return
	}
	row := e.Evaluate(snap)
	e.mu.Lock()
	same := e.currentRow != nil && *e.currentRow == row
	e.mu.Unlock()
	if same {
		return
	}
	if applyErr := e.Apply(ctx, row, snap); applyErr != nil {
		e.emitApplyFailed(ctx, row, applyErr)
		return
	}
}

func (e *CostGatingEngine) emitSnapshotError(ctx context.Context, err error) {
	auditCtx := context.WithoutCancel(ctx)
	payload, _ := payloadOf(eventlog.BudgetSnapshotError{Error: err.Error()})
	_, _ = e.emitter.Append(auditCtx, eventlog.Event{
		Type:      eventlog.EvtBudgetSnapshotError,
		SessionID: e.sessionID,
		ProjectID: e.projectID,
		Timestamp: e.clk.Now(),
		Payload:   payload,
	})
}

func (e *CostGatingEngine) emitApplyFailed(ctx context.Context, row ThresholdRow, err error) {
	auditCtx := context.WithoutCancel(ctx)
	payload, _ := payloadOf(eventlog.BudgetDegradationFailed{
		Action: string(row.Action),
		Error:  err.Error(),
	})
	_, _ = e.emitter.Append(auditCtx, eventlog.Event{
		Type:      eventlog.EvtBudgetDegradationFailed,
		SessionID: e.sessionID,
		ProjectID: e.projectID,
		Timestamp: e.clk.Now(),
		Payload:   payload,
	})
}

func payloadOf(enc eventlog.PayloadEncoder) (map[string]any, error) {
	raw, err := enc.Payload()
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if uerr := jsonUnmarshalToMap(raw, &m); uerr != nil {
		return nil, uerr
	}
	return m, nil
}

func jsonUnmarshalToMap(raw []byte, m *map[string]any) error {
	return jsonUnmarshal(raw, m)
}

func (e *CostGatingEngine) Stopped() <-chan struct{} {
	return e.stoppedCh
}

func (e *CostGatingEngine) Apply(ctx context.Context, row ThresholdRow, snap BudgetSnapshot) error {

	if row.Action != CostActionContinue {
		if err := e.waitAtomicBoundary(ctx); err != nil {
			return err
		}
	}
	c := &CostActionContext{
		Reason:    fmt.Sprintf("cost-gating threshold pct=%v action=%s", row.Pct, row.Action),
		Snapshot:  snap,
		Threshold: row,
	}
	if err := ApplyAction(ctx, e.actuator, row.Action, c); err != nil {
		return err
	}

	e.mu.Lock()
	var prior CostAction = CostActionContinue
	var priorDegradation *ThresholdRow
	if e.currentRow != nil {
		prior = e.currentRow.Action
		if e.currentRow.Action != CostActionContinue {
			rowCopy := *e.currentRow
			priorDegradation = &rowCopy
		}
	}
	e.currentRow = &row
	e.mu.Unlock()

	payload, perr := payloadOf(eventlog.BudgetDegradationApplied{
		ThresholdPct:    int(row.Pct),
		Action:          string(row.Action),
		PriorAction:     string(prior),
		Doctrine:        snap.DoctrineName,
		ProjectID:       snap.ProjectID,
		CumulativeUSD:   snap.CumulativeUSD,
		DailyCapUSD:     snap.DailyCapUSD,
		ProjectedEODUSD: snap.ProjectedEODUSD,
		PAYGActive:      snap.PAYGActive,
	})
	if perr != nil {
		return fmt.Errorf("orchestrator: marshal BudgetDegradationApplied: %w", perr)
	}
	auditCtx := context.WithoutCancel(ctx)
	if _, aerr := e.emitter.Append(auditCtx, eventlog.Event{
		Type:      eventlog.EvtBudgetDegradationApplied,
		SessionID: e.sessionID,
		ProjectID: e.projectID,
		Timestamp: e.clk.Now(),
		Payload:   payload,
	}); aerr != nil {
		return aerr
	}

	if row.Action == CostActionContinue && priorDegradation != nil {
		e.triggerRecovery(ctx, snap, *priorDegradation)
	}
	return nil
}

var recoveryHoldActions = map[CostAction]struct{}{
	CostActionWaitingForConfirmation: {},
	CostActionWaiting:                {},
}

func (e *CostGatingEngine) triggerRecovery(ctx context.Context, snap BudgetSnapshot, priorDegradation ThresholdRow) {
	e.mu.Lock()
	if e.recoveryActive {
		// Already walking; do not spawn a second goroutine.
		e.mu.Unlock()
		return
	}
	e.mu.Unlock()

	if _, hold := recoveryHoldActions[priorDegradation.Action]; hold {
		auditCtx := context.WithoutCancel(ctx)
		payload, _ := payloadOf(eventlog.BudgetRecoveryHeld{
			HeldAction: string(priorDegradation.Action),
			Reason:     "capa-firewall: operator confirmation required to release",
			Doctrine:   snap.DoctrineName,
			ProjectID:  snap.ProjectID,
		})
		_, _ = e.emitter.Append(auditCtx, eventlog.Event{
			Type:      eventlog.EvtBudgetRecoveryHeld,
			SessionID: e.sessionID,
			ProjectID: e.projectID,
			Timestamp: e.clk.Now(),
			Payload:   payload,
		})
		return
	}

	e.mu.Lock()
	e.recoveryActive = true
	e.mu.Unlock()

	stepInterval := e.profile.RecoveryStepInterval
	if stepInterval <= 0 {
		stepInterval = 60 * time.Second
	}
	go e.recoveryWalk(ctx, snap, priorDegradation, stepInterval)
}

func (e *CostGatingEngine) recoveryWalk(ctx context.Context, snap BudgetSnapshot, priorDegradation ThresholdRow, step time.Duration) {
	defer func() {
		e.mu.Lock()
		e.recoveryActive = false
		e.mu.Unlock()
	}()

	walkRow := priorDegradation

	for {
		timer := e.clk.NewTimer(step)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C():
		}

		if walkRow.Action == CostActionContinue {
			auditCtx := context.WithoutCancel(ctx)
			payload, _ := payloadOf(eventlog.BudgetFullyRecovered{
				Doctrine:  snap.DoctrineName,
				ProjectID: snap.ProjectID,
			})
			_, _ = e.emitter.Append(auditCtx, eventlog.Event{
				Type:      eventlog.EvtBudgetFullyRecovered,
				SessionID: e.sessionID,
				ProjectID: e.projectID,
				Timestamp: e.clk.Now(),
				Payload:   payload,
			})
			return
		}

		if _, hold := recoveryHoldActions[walkRow.Action]; hold {
			auditCtx := context.WithoutCancel(ctx)
			payload, _ := payloadOf(eventlog.BudgetRecoveryHeld{
				HeldAction: string(walkRow.Action),
				Reason:     "capa-firewall: operator confirmation required to release",
				Doctrine:   snap.DoctrineName,
				ProjectID:  snap.ProjectID,
			})
			_, _ = e.emitter.Append(auditCtx, eventlog.Event{
				Type:      eventlog.EvtBudgetRecoveryHeld,
				SessionID: e.sessionID,
				ProjectID: e.projectID,
				Timestamp: e.clk.Now(),
				Payload:   payload,
			})
			return
		}

		next := ThresholdRow{Pct: 0, Action: CostActionContinue}
		for i := len(e.table) - 1; i >= 0; i-- {
			if e.table[i].Pct == PctPAYG {
				continue
			}
			if e.table[i].Pct < walkRow.Pct {
				next = e.table[i]
				break
			}
		}

		auditCtx := context.WithoutCancel(ctx)
		payload, _ := payloadOf(eventlog.BudgetRecovered{
			UndoneAction: string(walkRow.Action),
			NextAction:   string(next.Action),
			NextPct:      int(next.Pct),
			Doctrine:     snap.DoctrineName,
			ProjectID:    snap.ProjectID,
		})
		_, _ = e.emitter.Append(auditCtx, eventlog.Event{
			Type:      eventlog.EvtBudgetRecovered,
			SessionID: e.sessionID,
			ProjectID: e.projectID,
			Timestamp: e.clk.Now(),
			Payload:   payload,
		})

		_ = ApplyAction(ctx, e.actuator, next.Action, &CostActionContext{
			Reason:    "cost-gating recovery walk",
			Snapshot:  snap,
			Threshold: next,
		})
		walkRow = next
	}
}

func (e *CostGatingEngine) waitAtomicBoundary(ctx context.Context) error {
	boundary := e.workers.WaitAtomicBoundary(ctx)
	timer := e.clk.NewTimer(e.atomTimeout)
	defer timer.Stop()
	select {
	case <-boundary:
		return nil
	case <-timer.C():

		auditCtx := context.WithoutCancel(ctx)
		payload, _ := payloadOf(eventlog.CostGatingAtomicityTimeout{TimeoutSec: e.atomTimeout.Seconds()})
		_, _ = e.emitter.Append(auditCtx, eventlog.Event{
			Type:      eventlog.EvtCostGatingAtomicityTimeout,
			SessionID: e.sessionID,
			ProjectID: e.projectID,
			Timestamp: e.clk.Now(),
			Payload:   payload,
		})
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
