package aggregator_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/amendment/aggregator"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

func TestCostAggregatorRecordsBudgetDegradationApplied(t *testing.T) {
	c := aggregator.NewCost()
	now := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)

	c.RegisterRule(aggregator.RuleSpec{
		RulePath:        "autonomy.cost_degradation.soft_check_usd",
		WindowSessions:  20,
		ThresholdPct:    0.7,
		AnomalousEvents: []eventlog.EventType{eventlog.EvtBudgetDegradationApplied},
	})

	c.RecordSession(aggregator.SessionContext{
		SessionID:         "s-1",
		Timestamp:         now,
		LastAppliedADR:    "ADR-0024",
		EventsThisSession: []eventlog.PayloadEncoder{eventlog.BudgetDegradationApplied{ThresholdPct: 60, Action: "drop_l3_strategic"}},
	})

	pct, total, last := c.Evaluate("autonomy.cost_degradation.soft_check_usd", 20)
	if total != 1 {
		t.Errorf("total=%d, want 1", total)
	}
	if pct != 0.0 {
		t.Errorf("pct=%v, want 0.0 (one anomalous session)", pct)
	}
	if last != "ADR-0024" {
		t.Errorf("lastApplied=%q, want ADR-0024", last)
	}
}

func TestCostAggregatorPassingSessionDoesNotPenalize(t *testing.T) {
	c := aggregator.NewCost()
	now := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)

	c.RegisterRule(aggregator.RuleSpec{
		RulePath:        "autonomy.cost_degradation.soft_check_usd",
		WindowSessions:  10,
		ThresholdPct:    0.7,
		AnomalousEvents: []eventlog.EventType{eventlog.EvtBudgetDegradationApplied},
	})

	for i := 0; i < 4; i++ {
		c.RecordSession(aggregator.SessionContext{
			SessionID:         fmt.Sprintf("s-%d", i),
			Timestamp:         now.Add(time.Duration(i) * time.Hour),
			LastAppliedADR:    "ADR-0024",
			EventsThisSession: nil,
		})
	}

	pct, total, last := c.Evaluate("autonomy.cost_degradation.soft_check_usd", 10)
	if total != 4 || pct != 1.0 || last != "ADR-0024" {
		t.Errorf("Evaluate=(%v,%d,%q), want (1.0, 4, ADR-0024)", pct, total, last)
	}
}

func TestCostAggregatorMultipleRulesIndependent(t *testing.T) {
	c := aggregator.NewCost()
	now := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)

	c.RegisterRule(aggregator.RuleSpec{
		RulePath:        "rule.budget",
		WindowSessions:  10,
		ThresholdPct:    0.7,
		AnomalousEvents: []eventlog.EventType{eventlog.EvtBudgetDegradationApplied},
	})

	c.RegisterRule(aggregator.RuleSpec{
		RulePath:        "rule.emergency",
		WindowSessions:  10,
		ThresholdPct:    0.7,
		AnomalousEvents: []eventlog.EventType{eventlog.EvtEmergencyTierActivated},
	})

	c.RecordSession(aggregator.SessionContext{
		SessionID:         "s-1",
		Timestamp:         now,
		LastAppliedADR:    "ADR-A",
		EventsThisSession: []eventlog.PayloadEncoder{eventlog.BudgetDegradationApplied{ThresholdPct: 80}},
	})

	pct1, total1, _ := c.Evaluate("rule.budget", 10)
	pct2, total2, _ := c.Evaluate("rule.emergency", 10)

	if total1 != 1 || pct1 != 0.0 {
		t.Errorf("rule.budget Evaluate=(%v,%d), want (0.0, 1)", pct1, total1)
	}
	if total2 != 1 || pct2 != 1.0 {
		t.Errorf("rule.emergency Evaluate=(%v,%d), want (1.0, 1) — non-matching event-type session counts as passing", pct2, total2)
	}
}

func TestCostAggregatorEvaluateUnknownRuleReturnsSafeDefaults(t *testing.T) {
	c := aggregator.NewCost()
	pct, total, last := c.Evaluate("nonexistent.rule", 5)
	if pct != 1.0 || total != 0 || last != "" {
		t.Errorf("Evaluate unknown=(%v,%d,%q), want (1.0, 0, \"\")", pct, total, last)
	}
}

func TestCostAggregatorRuleSpecsSnapshot(t *testing.T) {
	c := aggregator.NewCost()
	c.RegisterRule(aggregator.RuleSpec{RulePath: "a", WindowSessions: 5, ThresholdPct: 0.7, AnomalousEvents: []eventlog.EventType{eventlog.EvtBudgetDegradationApplied}})
	c.RegisterRule(aggregator.RuleSpec{RulePath: "b", WindowSessions: 5, ThresholdPct: 0.7, AnomalousEvents: []eventlog.EventType{eventlog.EvtEmergencyTierActivated}})
	specs := c.RuleSpecs()
	if len(specs) != 2 {
		t.Errorf("specs len=%d, want 2", len(specs))
	}
}

func TestCostAggregatorRegisterRuleIdempotent(t *testing.T) {
	c := aggregator.NewCost()
	c.RegisterRule(aggregator.RuleSpec{RulePath: "r", WindowSessions: 5, ThresholdPct: 0.7, AnomalousEvents: []eventlog.EventType{eventlog.EvtBudgetDegradationApplied}})
	c.RecordSession(aggregator.SessionContext{
		SessionID: "s-1", Timestamp: time.Now(), LastAppliedADR: "ADR-X",
		EventsThisSession: []eventlog.PayloadEncoder{eventlog.BudgetDegradationApplied{ThresholdPct: 80}},
	})

	c.RegisterRule(aggregator.RuleSpec{RulePath: "r", WindowSessions: 5, ThresholdPct: 0.5, AnomalousEvents: []eventlog.EventType{eventlog.EvtBudgetDegradationApplied}})
	_, total, _ := c.Evaluate("r", 5)
	if total != 1 {
		t.Errorf("RegisterRule clobbered window state; total=%d, want 1", total)
	}
}

func TestSessionMatchesEmptyWhitelistDoesNotMatch(t *testing.T) {
	c := aggregator.NewCost()
	c.RegisterRule(aggregator.RuleSpec{RulePath: "r", WindowSessions: 5, ThresholdPct: 0.7, AnomalousEvents: nil})
	c.RecordSession(aggregator.SessionContext{
		SessionID: "s-1", Timestamp: time.Now(), LastAppliedADR: "ADR-X",
		EventsThisSession: []eventlog.PayloadEncoder{eventlog.BudgetDegradationApplied{ThresholdPct: 80}},
	})
	pct, total, _ := c.Evaluate("r", 5)
	if total != 1 || pct != 1.0 {
		t.Errorf("Evaluate=(%v,%d), want (1.0, 1) — empty whitelist must never flag anomaly", pct, total)
	}
}
