// tests/compliance/inv_zen_141_revert_attribution_test.go
//
// Compliance gate for invariant invariant: telemetry-driven autonomous
// revert MUST target the most-recent DoctrineAmendmentApplied for the
// rule's category within the rolling window. The TelemetrySubscriber's
// LIFO attribution rule prevents reverting a prior amendment when a
// more-recent one is the actual cause of the threshold breach.
package compliance

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/amendment"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/amendment/aggregator"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

// TestInvZen141RevertTargetsLastAppliedADR exercises invariant:
// when multiple DoctrineAmendmentApplied events sit in the rolling
// window, AutoRevert MUST target the MOST-RECENT one (LIFO
// attribution).
func TestInvZen141RevertTargetsLastAppliedADR(t *testing.T) {
	ctx := context.Background()
	rev := &recordingReverter{}
	em := &recordingEmitter{}
	clk := clock.NewFake(time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC))

	costAgg := aggregator.NewCost()
	rulePath := "autonomy.cost_degradation.soft_check_usd"
	costAgg.RegisterRule(aggregator.RuleSpec{
		RulePath:        rulePath,
		WindowSessions:  10,
		ThresholdPct:    0.7,
		AnomalousEvents: []eventlog.EventType{eventlog.EvtBudgetDegradationApplied},
	})

	for i := 0; i < 10; i++ {
		adr := "ADR-0024"
		if i >= 5 {
			adr = "ADR-0025"
		}
		costAgg.RecordSession(aggregator.SessionContext{
			SessionID:         fmt.Sprintf("s-%d", i),
			Timestamp:         clk.Now().Add(time.Duration(i) * time.Hour),
			LastAppliedADR:    adr,
			EventsThisSession: []eventlog.PayloadEncoder{eventlog.BudgetDegradationApplied{ThresholdPct: 80}},
		})
	}

	ts := amendment.NewTelemetrySubscriber(amendment.TelemetrySubscriberConfig{
		Cost:     costAgg,
		Reverter: rev,
		Emitter:  em,
		Clock:    clk,
	})

	dispatched, err := ts.EvaluateProject(ctx, "internal-platform-x")
	if err != nil {
		t.Fatalf("EvaluateProject: %v", err)
	}
	if dispatched != 1 {
		t.Fatalf("dispatched=%d, want 1", dispatched)
	}

	calls := rev.Snapshot()
	if len(calls) != 1 {
		t.Fatalf("AutoRevert calls = %d, want 1", len(calls))
	}
	if calls[0].adr != 25 {
		t.Errorf("inv-zen-141 violated: AutoRevert ADR = %d, want 25 (most-recent in window)", calls[0].adr)
	}
}

type recordingReverter struct {
	mu    sync.Mutex
	calls []recordingRevertCall
}

type recordingRevertCall struct {
	adr    int
	reason string
}

func (r *recordingReverter) AutoRevert(_ context.Context, adr int, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, recordingRevertCall{adr: adr, reason: reason})
	return nil
}

func (r *recordingReverter) Snapshot() []recordingRevertCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]recordingRevertCall, len(r.calls))
	copy(out, r.calls)
	return out
}

type recordingEmitter struct{}

func (recordingEmitter) EmitDoctrineEvent(_ context.Context, _ eventlog.PayloadEncoder) error {
	return nil
}
