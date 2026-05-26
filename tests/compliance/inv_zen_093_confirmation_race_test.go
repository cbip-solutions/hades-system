// Copyright 2026 zen-swarm contributors. SPDX-License-Identifier: MIT
//
// tests/compliance/inv_zen_093_confirmation_race_test.go
//
// inv-zen-093 (race-safety, spec §3.5):
//
//	Every concurrent HandleAck call carrying the SAME EventID must
//	result in AT MOST ONE successful ack; all others must return
//	ErrConfirmationStale. This enforces the single-pending-request
//	invariant: once an ack is committed, h.pending is cleared so any
//	subsequent or concurrent ack targeting the same EventID finds an
//	empty pending slot and receives ErrConfirmationStale.
//
// Tests:
//  1. TestInvZen093_ConcurrentAcksAtMostOneSucceeds — 64 goroutines
//     race to ack the same EventID; exactly 1 succeeds.
//  2. TestInvZen093_StaleEventIDRejected — replayed ack (same EventID
//     after first ack committed) returns ErrConfirmationStale.
//
// No build tags: default `go test ./tests/compliance/` runs these.
package compliance_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	orch "github.com/cbip-solutions/hades-system/internal/orchestrator"
	"github.com/cbip-solutions/hades-system/internal/workforce/gate"
)

const (
	complianceTestSession = "session-compliance-f6"
	complianceTestProject = "project-compliance-f6"
)

func newComplianceHandler(pol *orch.ConfirmationPolicy) *orch.ConfirmationHandler {
	sm := newComplianceFakeSM(orch.StateRunning)
	ap := newComplianceFakeAppender()
	g := newComplianceFakeGate()
	return orch.NewConfirmationHandler(pol, sm, ap, g, complianceTestSession, complianceTestProject)
}

func TestInvZen093_ConcurrentAcksAtMostOneSucceeds(t *testing.T) {
	pol := orch.NewConfirmationPolicy(map[orch.DecisionClass]orch.Threshold{
		orch.DecisionInvariantViolation: orch.ThresholdHigh,
	}, false)
	sm := newComplianceFakeSM(orch.StateRunning)
	ap := newComplianceFakeAppender()
	g := newComplianceFakeGate()
	h := orch.NewConfirmationHandler(pol, sm, ap, g, complianceTestSession, complianceTestProject)

	out, err := h.RequestConfirmation(context.Background(), orch.RequestConfirmationInput{
		Class: orch.DecisionInvariantViolation,
	})
	if err != nil {
		t.Fatalf("RequestConfirmation: %v", err)
	}

	const goroutines = 64
	var successCount atomic.Int32
	var unexpectedErrs atomic.Int32
	var wg sync.WaitGroup
	wg.Add(goroutines)
	start := make(chan struct{})

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			<-start
			ackErr := h.HandleAck(context.Background(), orch.AckInput{
				EventID:  out.EventID,
				Operator: orch.OperatorIdentity{UID: 501},
			})
			if ackErr == nil {
				successCount.Add(1)
			} else if !errors.Is(ackErr, orch.ErrConfirmationStale) {
				t.Errorf("unexpected error from HandleAck: %v", ackErr)
				unexpectedErrs.Add(1)
			}
		}()
	}

	close(start)
	wg.Wait()

	if n := successCount.Load(); n != 1 {
		t.Errorf("inv-zen-093 violated: %d concurrent acks succeeded; want exactly 1", n)
	}
	if unexpectedErrs.Load() != 0 {
		t.Errorf("inv-zen-093: unexpected non-stale errors from concurrent acks")
	}

	if g.State() != gate.StateRunning {
		t.Errorf("gate state after concurrent acks = %v, want StateRunning", g.State())
	}

	if sm.Current() != orch.StateRunning {
		t.Errorf("state after concurrent acks = %v, want Running", sm.Current())
	}
}

func TestInvZen093_StaleEventIDRejected(t *testing.T) {
	pol := orch.NewConfirmationPolicy(map[orch.DecisionClass]orch.Threshold{
		orch.DecisionInvariantViolation: orch.ThresholdHigh,
	}, false)
	sm := newComplianceFakeSM(orch.StateRunning)
	ap := newComplianceFakeAppender()
	g := newComplianceFakeGate()
	h := orch.NewConfirmationHandler(pol, sm, ap, g, complianceTestSession, complianceTestProject)

	out, err := h.RequestConfirmation(context.Background(), orch.RequestConfirmationInput{
		Class: orch.DecisionInvariantViolation,
	})
	if err != nil {
		t.Fatalf("RequestConfirmation: %v", err)
	}

	if err := h.HandleAck(context.Background(), orch.AckInput{
		EventID:  out.EventID,
		Operator: orch.OperatorIdentity{UID: 501},
	}); err != nil {
		t.Fatalf("first HandleAck: %v", err)
	}

	replayErr := h.HandleAck(context.Background(), orch.AckInput{
		EventID:  out.EventID,
		Operator: orch.OperatorIdentity{UID: 501},
	})
	if !errors.Is(replayErr, orch.ErrConfirmationStale) {
		t.Errorf("replayed ack: err = %v, want ErrConfirmationStale", replayErr)
	}
}
