// Copyright 2026 zen-swarm contributors. SPDX-License-Identifier: MIT
//
// tests/compliance/orchestrator_fakes_test.go
//
// compliance tests.
//
// Intentionally duplicated from internal/orchestrator/confirmation_handler_test.go
// (confFake*) and internal/orchestrator/confirmation_audit_test.go
// (auditFakeAppender). The compliance suite must build under default tags
// with no special build constraints, and Go disallows importing _test.go
// files across packages. Any future structural change to the orchestrator
// interfaces must be mirrored here.
//
// Interfaces implemented:
//   - orch.StateMachineAPI  → complianceFakeSM
//   - orch.AppenderAPI      → complianceFakeAppender
//   - orch.GateAPI          → complianceFakeGate
package compliance_test

import (
	"context"
	"errors"
	"sync"

	orch "github.com/cbip-solutions/hades-system/internal/orchestrator"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
	"github.com/cbip-solutions/hades-system/internal/workforce/gate"
)

type complianceFakeSM struct {
	mu    sync.Mutex
	state orch.State
	allow map[[2]orch.State]bool
}

func newComplianceFakeSM(initial orch.State) *complianceFakeSM {
	return &complianceFakeSM{
		state: initial,
		allow: map[[2]orch.State]bool{
			{orch.StateRunning, orch.StateWaitingForConfirmation}:      true,
			{orch.StateWaitingForConfirmation, orch.StateRunning}:      true,
			{orch.StateWaitingForConfirmation, orch.StateAborting}:     true,
			{orch.StateDegradedTier, orch.StateWaitingForConfirmation}: true,
			{orch.StateWaitingForConfirmation, orch.StateDegradedTier}: true,
		},
	}
}

func (f *complianceFakeSM) Current() orch.State {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state
}

func (f *complianceFakeSM) Transition(_ context.Context, to orch.State, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	from := f.state
	if !f.allow[[2]orch.State{from, to}] {
		return errors.New("compliance/fake: illegal transition " + from.String() + "→" + to.String())
	}
	f.state = to
	return nil
}

type complianceFakeAppender struct {
	mu     sync.Mutex
	counts map[eventlog.EventType]int
	last   map[eventlog.EventType]map[string]any
	nextID int64
}

func newComplianceFakeAppender() *complianceFakeAppender {
	return &complianceFakeAppender{
		counts: map[eventlog.EventType]int{},
		last:   map[eventlog.EventType]map[string]any{},
	}
}

func (f *complianceFakeAppender) Append(_ context.Context, ev eventlog.Event) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.counts[ev.Type]++

	cp := make(map[string]any, len(ev.Payload))
	for k, v := range ev.Payload {
		cp[k] = v
	}
	f.last[ev.Type] = cp
	f.nextID++
	return f.nextID, nil
}

func (f *complianceFakeAppender) Count(t eventlog.EventType) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.counts[t]
}

func (f *complianceFakeAppender) Last(t eventlog.EventType) map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.last[t]
}

type complianceFakeGate struct {
	mu    sync.Mutex
	state gate.State
}

func newComplianceFakeGate() *complianceFakeGate {
	return &complianceFakeGate{state: gate.StateRunning}
}

func (f *complianceFakeGate) State() gate.State {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state
}

func (f *complianceFakeGate) Pause(_ context.Context, mode gate.PauseMode, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch mode {
	case gate.PauseDescriptive:
		f.state = gate.StatePausedDescriptive
	case gate.PauseQuiet:
		f.state = gate.StatePausedQuiet
	case gate.PauseAfterApply:
		f.state = gate.StatePausedAfterApply
	default:
		return errors.New("compliance/fake: unsupported pause mode")
	}
	return nil
}

func (f *complianceFakeGate) Resume(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.state = gate.StateRunning
	return nil
}
