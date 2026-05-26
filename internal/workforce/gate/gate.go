// SPDX-License-Identifier: MIT
package gate

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type State string

const (
	StateRunning State = "running"

	StatePausedDescriptive State = "paused_descriptive"

	StatePausedQuiet State = "paused_quiet"

	StatePausedAfterApply State = "paused_after_apply"
)

type PauseMode int

const (
	PauseDescriptive PauseMode = iota

	PauseQuiet

	PauseAfterApply
)

type Scope int

const (
	ScopeWorkerDispatch Scope = iota

	ScopeLLMPreCall

	ScopeAfterCommit
)

type Transition struct {
	At      time.Time
	From    State
	ToState State
	Reason  string
}

type GatePersist interface {
	LoadState(ctx context.Context) (State, error)

	SaveState(ctx context.Context, s State, reason string) error
}

type OperatorGate struct {
	mu      sync.RWMutex
	state   State
	persist GatePersist
	log     []Transition
}

func NewOperatorGate(ctx context.Context, persist GatePersist) (*OperatorGate, error) {
	if persist == nil {
		return nil, ErrNilPersist
	}
	s, err := persist.LoadState(ctx)
	if err != nil {
		return nil, fmt.Errorf("gate.NewOperatorGate: load state: %w", err)
	}

	switch s {
	case StateRunning, StatePausedDescriptive, StatePausedQuiet, StatePausedAfterApply:

	default:
		s = StateRunning
	}
	return &OperatorGate{state: s, persist: persist}, nil
}

var ErrNilPersist = fmt.Errorf("gate.NewOperatorGate: persist is nil")

func (g *OperatorGate) State() State {
	g.mu.RLock()
	s := g.state
	g.mu.RUnlock()
	return s
}

func (g *OperatorGate) IsPaused(scope Scope) bool {
	g.mu.RLock()
	s := g.state
	g.mu.RUnlock()
	switch s {
	case StateRunning:
		return false
	case StatePausedAfterApply:

		return scope == ScopeWorkerDispatch
	default:

		return true
	}
}

func (g *OperatorGate) Pause(ctx context.Context, mode PauseMode, reason string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	from := g.state
	var to State
	switch mode {
	case PauseDescriptive:
		to = StatePausedDescriptive
	case PauseQuiet:
		to = StatePausedQuiet
	case PauseAfterApply:
		to = StatePausedAfterApply
	default:
		return fmt.Errorf("gate.Pause: unknown mode %d", mode)
	}

	if err := g.persist.SaveState(ctx, to, reason); err != nil {
		return fmt.Errorf("gate.Pause: persist: %w", err)
	}
	g.state = to
	g.log = append(g.log, Transition{
		At:      time.Now().UTC(),
		From:    from,
		ToState: to,
		Reason:  reason,
	})
	return nil
}

func (g *OperatorGate) Resume(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	from := g.state
	if from == StateRunning {
		return nil
	}

	if err := g.persist.SaveState(ctx, StateRunning, "resume"); err != nil {
		return fmt.Errorf("gate.Resume: persist: %w", err)
	}
	g.state = StateRunning
	g.log = append(g.log, Transition{
		At:      time.Now().UTC(),
		From:    from,
		ToState: StateRunning,
		Reason:  "resume",
	})
	return nil
}

func (g *OperatorGate) TransitionLog() []Transition {
	g.mu.RLock()
	out := make([]Transition, len(g.log))
	copy(out, g.log)
	g.mu.RUnlock()
	return out
}
