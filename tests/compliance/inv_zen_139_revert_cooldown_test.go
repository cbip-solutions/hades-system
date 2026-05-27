// tests/compliance/inv_zen_139_revert_cooldown_test.go
//
// Compliance gate for invariant invariant: telemetry-driven
// autonomous revert MUST respect per-rule cooldown (default
// max-scope=24h). Within the cooldown window, no additional autonomous
// reverts fire even if every evaluation pass observes a fresh threshold
// breach.
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

func TestInvZen139CooldownSuppressesRapidReRevert(t *testing.T) {
	ctx := context.Background()
	rev := &countingReverter{}
	em := &countingEmitter{}
	clk := clock.NewFake(time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC))
	cd := amendment.NewRevertCooldown()

	costAgg := aggregator.NewCost()
	rulePath := "autonomy.cost_degradation.soft_check_usd"
	costAgg.RegisterRule(aggregator.RuleSpec{
		RulePath:        rulePath,
		WindowSessions:  5,
		ThresholdPct:    0.7,
		AnomalousEvents: []eventlog.EventType{eventlog.EvtBudgetDegradationApplied},
	})

	ts := amendment.NewTelemetrySubscriber(amendment.TelemetrySubscriberConfig{
		Cost:     costAgg,
		Reverter: rev,
		Emitter:  em,
		Clock:    clk,
		Cooldown: cd,
	})

	for pass := 0; pass < 100; pass++ {
		clk.Advance(6 * time.Minute)

		for i := 0; i < 5; i++ {
			costAgg.RecordSession(aggregator.SessionContext{
				SessionID:         fmt.Sprintf("p%d-s%d", pass, i),
				Timestamp:         clk.Now(),
				LastAppliedADR:    "ADR-0024",
				EventsThisSession: []eventlog.PayloadEncoder{eventlog.BudgetDegradationApplied{ThresholdPct: 80}},
			})
		}
		_, _ = ts.EvaluateProject(ctx, "internal-platform-x")
	}

	if got := rev.Count(); got != 1 {
		t.Errorf("inv-zen-139 violated: AutoRevert call count = %d over 100 passes within cooldown, want exactly 1", got)
	}

	if got := em.Suppressed(); got < 50 {
		t.Errorf("DoctrineRevertSuppressedCooldown count = %d over 100 passes, want >= 50 (cooldown should suppress majority)", got)
	}
}

func TestInvZen139CooldownExpiresAfterWindow(t *testing.T) {
	ctx := context.Background()
	rev := &countingReverter{}
	em := &countingEmitter{}
	clk := clock.NewFake(time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC))
	cd := amendment.NewRevertCooldown()

	costAgg := aggregator.NewCost()
	rulePath := "autonomy.cost_degradation.soft_check_usd"
	costAgg.RegisterRule(aggregator.RuleSpec{
		RulePath:        rulePath,
		WindowSessions:  5,
		ThresholdPct:    0.7,
		AnomalousEvents: []eventlog.EventType{eventlog.EvtBudgetDegradationApplied},
	})

	ts := amendment.NewTelemetrySubscriber(amendment.TelemetrySubscriberConfig{
		Cost:     costAgg,
		Reverter: rev,
		Emitter:  em,
		Clock:    clk,
		Cooldown: cd,
	})

	for i := 0; i < 5; i++ {
		costAgg.RecordSession(aggregator.SessionContext{
			SessionID: fmt.Sprintf("a-%d", i), Timestamp: clk.Now(),
			LastAppliedADR:    "ADR-0024",
			EventsThisSession: []eventlog.PayloadEncoder{eventlog.BudgetDegradationApplied{ThresholdPct: 80}},
		})
	}
	_, _ = ts.EvaluateProject(ctx, "internal-platform-x")
	if rev.Count() != 1 {
		t.Fatalf("first pass count=%d, want 1", rev.Count())
	}

	clk.Advance(25 * time.Hour)

	for i := 0; i < 5; i++ {
		costAgg.RecordSession(aggregator.SessionContext{
			SessionID: fmt.Sprintf("b-%d", i), Timestamp: clk.Now(),
			LastAppliedADR:    "ADR-0024",
			EventsThisSession: []eventlog.PayloadEncoder{eventlog.BudgetDegradationApplied{ThresholdPct: 80}},
		})
	}
	_, _ = ts.EvaluateProject(ctx, "internal-platform-x")
	if rev.Count() != 2 {
		t.Errorf("second pass count=%d, want 2 (cooldown should have expired)", rev.Count())
	}
}

type countingReverter struct {
	mu    sync.Mutex
	count int
}

func (c *countingReverter) AutoRevert(_ context.Context, _ int, _ string) error {
	c.mu.Lock()
	c.count++
	c.mu.Unlock()
	return nil
}

func (c *countingReverter) Count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.count
}

type countingEmitter struct {
	mu                     sync.Mutex
	autonomous, suppressed int
}

func (c *countingEmitter) EmitDoctrineEvent(_ context.Context, ev eventlog.PayloadEncoder) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch ev.(type) {
	case eventlog.DoctrineAutonomousReverted:
		c.autonomous++
	case eventlog.DoctrineRevertSuppressedCooldown:
		c.suppressed++
	}
	return nil
}

func (c *countingEmitter) Suppressed() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.suppressed
}
