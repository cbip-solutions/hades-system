package aggregator_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/amendment/aggregator"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

func TestMergeAggregatorRecordsAnomaly(t *testing.T) {
	m := aggregator.NewMerge()
	now := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)

	rulePath := "merge.scoring.mode_distribution"
	m.RegisterRule(aggregator.RuleSpec{
		RulePath:        rulePath,
		WindowSessions:  10,
		ThresholdPct:    0.7,
		AnomalousEvents: []eventlog.EventType{eventlog.EvtBudgetSnapshotError},
	})

	m.RecordSession(aggregator.SessionContext{
		SessionID:         "s-1",
		Timestamp:         now,
		LastAppliedADR:    "ADR-0027",
		EventsThisSession: []eventlog.PayloadEncoder{eventlog.BudgetSnapshotError{Error: "anomaly"}},
	})

	pct, total, last := m.Evaluate(rulePath, 10)
	if total != 1 || pct != 0.0 || last != "ADR-0027" {
		t.Errorf("Evaluate=(%v,%d,%q), want (0.0, 1, ADR-0027)", pct, total, last)
	}
}

func TestMergeAggregatorRulePerEventTypeFiltering(t *testing.T) {
	m := aggregator.NewMerge()
	now := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)

	m.RegisterRule(aggregator.RuleSpec{
		RulePath:        "merge.mode.degradation_threshold",
		WindowSessions:  10,
		ThresholdPct:    0.6,
		AnomalousEvents: []eventlog.EventType{eventlog.EvtBudgetSnapshotError},
	})

	for i := 0; i < 3; i++ {
		m.RecordSession(aggregator.SessionContext{
			SessionID:         fmt.Sprintf("s-%d", i),
			Timestamp:         now.Add(time.Duration(i) * time.Hour),
			LastAppliedADR:    "ADR-0029",
			EventsThisSession: []eventlog.PayloadEncoder{eventlog.CostThresholdCrossed{ThresholdPct: 80}},
		})
	}

	pct, total, last := m.Evaluate("merge.mode.degradation_threshold", 10)
	if total != 3 || pct != 1.0 || last != "ADR-0029" {
		t.Errorf("Evaluate=(%v,%d,%q), want (1.0, 3, ADR-0029) — wrong-type events should NOT count as anomaly", pct, total, last)
	}
}

func TestMergeAggregatorMixedAnomalyAndPassingPctCalc(t *testing.T) {
	m := aggregator.NewMerge()
	now := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)

	m.RegisterRule(aggregator.RuleSpec{
		RulePath:        "merge.scoring.mode_distribution",
		WindowSessions:  10,
		ThresholdPct:    0.7,
		AnomalousEvents: []eventlog.EventType{eventlog.EvtBudgetSnapshotError},
	})

	for i := 0; i < 10; i++ {
		var events []eventlog.PayloadEncoder
		if i < 3 {
			events = []eventlog.PayloadEncoder{eventlog.BudgetSnapshotError{Error: "x"}}
		}
		m.RecordSession(aggregator.SessionContext{
			SessionID:         fmt.Sprintf("s-%d", i),
			Timestamp:         now.Add(time.Duration(i) * time.Hour),
			LastAppliedADR:    "ADR-0030",
			EventsThisSession: events,
		})
	}

	pct, total, last := m.Evaluate("merge.scoring.mode_distribution", 10)
	if total != 10 || pct != 0.7 || last != "ADR-0030" {
		t.Errorf("Evaluate=(%v,%d,%q), want (0.7, 10, ADR-0030)", pct, total, last)
	}
}

func TestMergeAggregatorEvaluateUnknownRuleReturnsSafeDefaults(t *testing.T) {
	m := aggregator.NewMerge()
	pct, total, last := m.Evaluate("nonexistent.rule", 5)
	if pct != 1.0 || total != 0 || last != "" {
		t.Errorf("Evaluate unknown=(%v,%d,%q), want (1.0, 0, \"\")", pct, total, last)
	}
}

func TestMergeAggregatorRuleSpecsSnapshot(t *testing.T) {
	m := aggregator.NewMerge()
	m.RegisterRule(aggregator.RuleSpec{RulePath: "a", WindowSessions: 5, ThresholdPct: 0.7, AnomalousEvents: []eventlog.EventType{eventlog.EvtBudgetSnapshotError}})
	m.RegisterRule(aggregator.RuleSpec{RulePath: "b", WindowSessions: 5, ThresholdPct: 0.7, AnomalousEvents: []eventlog.EventType{eventlog.EvtCostThresholdCrossed}})
	specs := m.RuleSpecs()
	if len(specs) != 2 {
		t.Errorf("specs len=%d, want 2", len(specs))
	}
}
