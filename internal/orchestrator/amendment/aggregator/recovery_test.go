package aggregator_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/amendment/aggregator"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

func TestRecoveryAggregatorRecordsWorkerDeathPermanentInfra(t *testing.T) {
	r := aggregator.NewRecovery()
	now := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)

	r.RegisterRule(aggregator.RuleSpec{
		RulePath:        "workforce.recovery.permanent_infra_escalate",
		WindowSessions:  10,
		ThresholdPct:    0.6,
		AnomalousEvents: []eventlog.EventType{eventlog.EvtWorkerDeath},
		PayloadFilter:   aggregator.WorkerFailureClassFilter(aggregator.FailureClassPermanentInfra),
	})

	r.RecordSession(aggregator.SessionContext{
		SessionID:      "s-1",
		Timestamp:      now,
		LastAppliedADR: "ADR-0030",
		EventsThisSession: []eventlog.PayloadEncoder{
			eventlog.WorkerDeath{WorkerID: "w-7", Class: "PERMANENT_INFRA", Reason: "oom_kill"},
		},
	})

	pct, total, last := r.Evaluate("workforce.recovery.permanent_infra_escalate", 10)
	if total != 1 || pct != 0.0 || last != "ADR-0030" {
		t.Errorf("Evaluate=(%v,%d,%q), want (0.0, 1, ADR-0030)", pct, total, last)
	}
}

func TestRecoveryAggregatorWorkerDeathDifferentClassDoesNotMatch(t *testing.T) {
	r := aggregator.NewRecovery()
	now := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)

	r.RegisterRule(aggregator.RuleSpec{
		RulePath:        "workforce.recovery.permanent_infra_escalate",
		WindowSessions:  10,
		ThresholdPct:    0.6,
		AnomalousEvents: []eventlog.EventType{eventlog.EvtWorkerDeath},
		PayloadFilter:   aggregator.WorkerFailureClassFilter(aggregator.FailureClassPermanentInfra),
	})

	for i := 0; i < 4; i++ {
		r.RecordSession(aggregator.SessionContext{
			SessionID:      fmt.Sprintf("s-%d", i),
			Timestamp:      now.Add(time.Duration(i) * time.Hour),
			LastAppliedADR: "ADR-0031",
			EventsThisSession: []eventlog.PayloadEncoder{
				eventlog.WorkerDeath{WorkerID: "w-7", Class: "TRANSIENT_LLM", Reason: "timeout"},
			},
		})
	}

	pct, total, last := r.Evaluate("workforce.recovery.permanent_infra_escalate", 10)
	if total != 4 || pct != 1.0 || last != "ADR-0031" {
		t.Errorf("Evaluate=(%v,%d,%q), want (1.0, 4, ADR-0031) — wrong-class events should NOT be anomaly", pct, total, last)
	}
}

func TestRecoveryAggregatorHRAEscalationCounts(t *testing.T) {
	r := aggregator.NewRecovery()
	now := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)

	r.RegisterRule(aggregator.RuleSpec{
		RulePath:        "hra.escalation.rate_threshold",
		WindowSessions:  20,
		ThresholdPct:    0.85,
		AnomalousEvents: []eventlog.EventType{eventlog.EvtEscalationDecision},
	})

	for i := 0; i < 4; i++ {
		var events []eventlog.PayloadEncoder
		if i%2 == 0 {
			events = []eventlog.PayloadEncoder{eventlog.EscalationDecision{FromLayer: "T", ToLayer: "S"}}
		}
		r.RecordSession(aggregator.SessionContext{
			SessionID:         fmt.Sprintf("s-%d", i),
			Timestamp:         now.Add(time.Duration(i) * time.Hour),
			LastAppliedADR:    "ADR-0032",
			EventsThisSession: events,
		})
	}

	pct, total, last := r.Evaluate("hra.escalation.rate_threshold", 20)
	if total != 4 || pct != 0.5 || last != "ADR-0032" {
		t.Errorf("Evaluate=(%v,%d,%q), want (0.5, 4, ADR-0032)", pct, total, last)
	}
}

func TestRecoveryAggregatorAllFailureClassesViaConvenienceFilter(t *testing.T) {
	r := aggregator.NewRecovery()
	now := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)

	r.RegisterRule(aggregator.RuleSpec{
		RulePath:        "workforce.recovery.any_worker_death",
		WindowSessions:  10,
		ThresholdPct:    0.7,
		AnomalousEvents: []eventlog.EventType{eventlog.EvtWorkerDeath, eventlog.EvtWorkerRedispatched},
		PayloadFilter: aggregator.WorkerFailureClassFilter(
			aggregator.FailureClassTransientLLM,
			aggregator.FailureClassTransientInfra,
			aggregator.FailureClassPermanentTask,
			aggregator.FailureClassPermanentInfra,
		),
	})

	classes := []string{"TRANSIENT_LLM", "TRANSIENT_INFRA", "PERMANENT_TASK", "PERMANENT_INFRA"}
	for i, cls := range classes {
		r.RecordSession(aggregator.SessionContext{
			SessionID:      fmt.Sprintf("s-%d", i),
			Timestamp:      now.Add(time.Duration(i) * time.Hour),
			LastAppliedADR: "ADR-0033",
			EventsThisSession: []eventlog.PayloadEncoder{
				eventlog.WorkerDeath{WorkerID: fmt.Sprintf("w-%d", i), Class: cls, Reason: "x"},
			},
		})
	}

	pct, total, last := r.Evaluate("workforce.recovery.any_worker_death", 10)
	if total != 4 || pct != 0.0 || last != "ADR-0033" {
		t.Errorf("Evaluate=(%v,%d,%q), want (0.0, 4, ADR-0033) — every class should match", pct, total, last)
	}
}

func TestRecoveryAggregatorWorkerRedispatchedWithClassFilter(t *testing.T) {
	r := aggregator.NewRecovery()
	now := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)

	r.RegisterRule(aggregator.RuleSpec{
		RulePath:        "workforce.recovery.transient_llm_only",
		WindowSessions:  5,
		ThresholdPct:    0.5,
		AnomalousEvents: []eventlog.EventType{eventlog.EvtWorkerRedispatched},
		PayloadFilter:   aggregator.WorkerFailureClassFilter(aggregator.FailureClassTransientLLM),
	})

	r.RecordSession(aggregator.SessionContext{
		SessionID:      "s-1",
		Timestamp:      now,
		LastAppliedADR: "ADR-0034",
		EventsThisSession: []eventlog.PayloadEncoder{
			eventlog.WorkerRedispatched{TaskID: "t-1", WorkerID: "w-1", Class: "TRANSIENT_LLM", Action: "redispatch_same_tier"},
		},
	})

	pct, total, _ := r.Evaluate("workforce.recovery.transient_llm_only", 5)
	if total != 1 || pct != 0.0 {
		t.Errorf("Evaluate=(%v,%d), want (0.0, 1)", pct, total)
	}
}

func TestRecoveryAggregatorPredicatePassThroughForOtherTypes(t *testing.T) {
	r := aggregator.NewRecovery()
	now := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)

	r.RegisterRule(aggregator.RuleSpec{
		RulePath:        "workforce.recovery.mixed",
		WindowSessions:  5,
		ThresholdPct:    0.5,
		AnomalousEvents: []eventlog.EventType{eventlog.EvtWorkerDeath, eventlog.EvtEscalationDecision},
		PayloadFilter:   aggregator.WorkerFailureClassFilter(aggregator.FailureClassPermanentInfra),
	})

	r.RecordSession(aggregator.SessionContext{
		SessionID:      "s-1",
		Timestamp:      now,
		LastAppliedADR: "ADR-0035",
		EventsThisSession: []eventlog.PayloadEncoder{
			eventlog.EscalationDecision{FromLayer: "T", ToLayer: "S"},
		},
	})

	pct, total, _ := r.Evaluate("workforce.recovery.mixed", 5)
	if total != 1 || pct != 0.0 {
		t.Errorf("Evaluate=(%v,%d), want (0.0, 1) — predicate must pass-through for EscalationDecision", pct, total)
	}
}

func TestRecoveryAggregatorEvaluateUnknownRuleReturnsSafeDefaults(t *testing.T) {
	r := aggregator.NewRecovery()
	pct, total, last := r.Evaluate("nonexistent.rule", 5)
	if pct != 1.0 || total != 0 || last != "" {
		t.Errorf("Evaluate unknown=(%v,%d,%q), want (1.0, 0, \"\")", pct, total, last)
	}
}

func TestRecoveryAggregatorRuleSpecsSnapshot(t *testing.T) {
	r := aggregator.NewRecovery()
	r.RegisterRule(aggregator.RuleSpec{RulePath: "a", WindowSessions: 5, ThresholdPct: 0.7, AnomalousEvents: []eventlog.EventType{eventlog.EvtWorkerDeath}})
	r.RegisterRule(aggregator.RuleSpec{RulePath: "b", WindowSessions: 5, ThresholdPct: 0.7, AnomalousEvents: []eventlog.EventType{eventlog.EvtEscalationDecision}})
	specs := r.RuleSpecs()
	if len(specs) != 2 {
		t.Errorf("specs len=%d, want 2", len(specs))
	}
}

func TestAllFailureClassesReturnsAllFour(t *testing.T) {
	classes := aggregator.AllFailureClasses()
	if len(classes) != 4 {
		t.Errorf("AllFailureClasses len=%d, want 4", len(classes))
	}
	seen := map[aggregator.FailureClass]bool{}
	for _, c := range classes {
		seen[c] = true
	}
	for _, expected := range []aggregator.FailureClass{
		aggregator.FailureClassTransientLLM, aggregator.FailureClassTransientInfra,
		aggregator.FailureClassPermanentTask, aggregator.FailureClassPermanentInfra,
	} {
		if !seen[expected] {
			t.Errorf("AllFailureClasses missing %s", expected)
		}
	}
}

func TestDefaultRecoveryAnomalyEventTypesIncludesAllFour(t *testing.T) {
	types := aggregator.DefaultRecoveryAnomalyEventTypes()
	if len(types) != 4 {
		t.Errorf("DefaultRecoveryAnomalyEventTypes len=%d, want 4", len(types))
	}
}
