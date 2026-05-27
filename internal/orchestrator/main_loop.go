// SPDX-License-Identifier: MIT
package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

const (
	DefaultMainLoopIdleTick = 5 * time.Second
	mainLoopBufferSize      = 256
	mainLoopDeathThreshold  = 3
)

var ErrMainLoopInvalidConfig = errors.New("orchestrator: main loop invalid config")

type MainLoopConfig struct {
	Log          *eventlog.Log
	Subscription eventlog.Subscription
	SM           StateMachineAPI
	Clock        clock.Clock
	SessionID    string
	ProjectID    string
	IdleTick     time.Duration
}

type MainLoop struct {
	sm       StateMachineAPI
	clk      clock.Clock
	idleTick time.Duration
	sub      eventlog.Subscription

	deathStreak atomic.Int32
}

func NewMainLoop(cfg MainLoopConfig) (*MainLoop, error) {
	if cfg.Log == nil && cfg.Subscription == nil {
		return nil, fmt.Errorf("%w: log or subscription is required", ErrMainLoopInvalidConfig)
	}
	if cfg.SM == nil {
		return nil, fmt.Errorf("%w: state machine is nil", ErrMainLoopInvalidConfig)
	}
	if cfg.SessionID == "" {
		return nil, fmt.Errorf("%w: session id is empty", ErrMainLoopInvalidConfig)
	}
	if cfg.ProjectID == "" {
		return nil, fmt.Errorf("%w: project id is empty", ErrMainLoopInvalidConfig)
	}
	if cfg.IdleTick < 0 {
		return nil, fmt.Errorf("%w: idle tick is negative", ErrMainLoopInvalidConfig)
	}
	if cfg.Clock == nil {
		cfg.Clock = clock.Real{}
	}
	if cfg.IdleTick == 0 {
		cfg.IdleTick = DefaultMainLoopIdleTick
	}
	sub := cfg.Subscription
	if sub == nil {
		sub = cfg.Log.Subscribe(eventlog.Filter{
			Types:     MainLoopEventTypes(),
			ProjectID: cfg.ProjectID,
		}, mainLoopBufferSize)
	}
	return &MainLoop{
		sm:       cfg.SM,
		clk:      cfg.Clock,
		idleTick: cfg.IdleTick,
		sub:      sub,
	}, nil
}

func MainLoopEventTypes() []eventlog.EventType {
	return []eventlog.EventType{
		eventlog.EvtWorkerDeath,
		eventlog.EvtSubstrateDriftDetected,
		eventlog.EvtOrchestratorStopped,
		eventlog.EvtBudgetDegradationApplied,
		eventlog.EvtEmergencyTierActivated,
	}
}

func (m *MainLoop) Run(ctx context.Context) {
	ticker := m.clk.NewTicker(m.idleTick)
	defer ticker.Stop()
	defer m.sub.Close()
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.sub.Done():
			return
		case rec := <-m.sub.Events():
			m.handleRecord(ctx, rec)
		case <-ticker.C():
		}
	}
}

func (m *MainLoop) handleRecord(ctx context.Context, rec eventlog.Record) {
	switch rec.EventType {
	case eventlog.EvtWorkerDeath:
		if m.deathStreak.Add(1) >= mainLoopDeathThreshold {
			_ = m.sm.Transition(ctx, StateHardPaused, "main_loop:worker_death_streak")
		}
	case eventlog.EvtSubstrateDriftDetected:
		m.deathStreak.Store(0)
		m.handleDrift(ctx, rec)
	case eventlog.EvtBudgetDegradationApplied:
		m.deathStreak.Store(0)
		m.handleBudgetDegradation(ctx, rec)
	case eventlog.EvtEmergencyTierActivated:
		m.deathStreak.Store(0)
		_ = m.sm.Transition(ctx, StateEmergencyTier, "main_loop:emergency_tier")
	case eventlog.EvtOrchestratorStopped:
		m.deathStreak.Store(0)
	default:
		m.deathStreak.Store(0)
	}
}

func (m *MainLoop) handleBudgetDegradation(ctx context.Context, rec eventlog.Record) {
	decoded, err := eventlog.Decode(rec.EventType, rec.Payload)
	if err != nil {
		return
	}
	budget, ok := decoded.(eventlog.BudgetDegradationApplied)
	if !ok {
		return
	}
	if budget.Action == "" || budget.Action == "continue" {
		return
	}
	_ = m.sm.Transition(ctx, StateDegradedTier, "main_loop:budget_degradation")
}

func (m *MainLoop) handleDrift(ctx context.Context, rec eventlog.Record) {
	decoded, err := eventlog.Decode(rec.EventType, rec.Payload)
	if err != nil {
		return
	}
	drift, ok := decoded.(eventlog.SubstrateDriftDetected)
	if !ok {
		return
	}
	if strings.EqualFold(drift.Severity, "hard") {
		_ = m.sm.Transition(ctx, StateHardPaused, "main_loop:substrate_drift_hard")
	}
}
