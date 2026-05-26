package amendment_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/amendment"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/amendment/aggregator"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

type fakeReverter struct {
	mu      sync.Mutex
	calls   []fakeRevertCall
	failErr error
}

type fakeRevertCall struct {
	ADRID  int
	Reason string
}

func (f *fakeReverter) AutoRevert(_ context.Context, adrID int, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fakeRevertCall{ADRID: adrID, Reason: reason})
	return f.failErr
}

func (f *fakeReverter) snapshot() []fakeRevertCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]fakeRevertCall, len(f.calls))
	copy(out, f.calls)
	return out
}

type telemetryFakeEmitter struct {
	mu     sync.Mutex
	events []eventlog.PayloadEncoder
}

func (f *telemetryFakeEmitter) EmitDoctrineEvent(_ context.Context, e eventlog.PayloadEncoder) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, e)
	return nil
}

func (f *telemetryFakeEmitter) snapshot() []eventlog.PayloadEncoder {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]eventlog.PayloadEncoder, len(f.events))
	copy(out, f.events)
	return out
}

type fakeCapaProbe struct {
	bound bool
}

func (f fakeCapaProbe) IsCapaFirewall(_ string) bool { return f.bound }

type fakePolicy struct {
	d time.Duration
}

func (f fakePolicy) CooldownFor(_, _ string) time.Duration { return f.d }

func TestTelemetrySubscriberDispatchesAutoRevertOnBreach(t *testing.T) {
	ctx := context.Background()
	rev := &fakeReverter{}
	em := &telemetryFakeEmitter{}
	clk := clock.NewFake(time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC))

	costAgg := aggregator.NewCost()
	rulePath := "autonomy.cost_degradation.soft_check_usd"
	costAgg.RegisterRule(aggregator.RuleSpec{
		RulePath:        rulePath,
		WindowSessions:  5,
		ThresholdPct:    0.7,
		AnomalousEvents: []eventlog.EventType{eventlog.EvtBudgetDegradationApplied},
	})

	for i := 0; i < 5; i++ {
		costAgg.RecordSession(aggregator.SessionContext{
			SessionID:         fmt.Sprintf("s-%d", i),
			Timestamp:         clk.Now().Add(time.Duration(i) * time.Hour),
			LastAppliedADR:    "ADR-0024",
			EventsThisSession: []eventlog.PayloadEncoder{eventlog.BudgetDegradationApplied{ThresholdPct: 60, Action: "drop_l3_strategic"}},
		})
	}

	ts := amendment.NewTelemetrySubscriber(amendment.TelemetrySubscriberConfig{
		Cost:     costAgg,
		Reverter: rev,
		Emitter:  em,
		Clock:    clk,
		Policy:   fakePolicy{d: 24 * time.Hour},
	})

	dispatched, err := ts.EvaluateProject(ctx, "internal-platform-x")
	if err != nil {
		t.Fatalf("EvaluateProject: %v", err)
	}
	if dispatched != 1 {
		t.Errorf("dispatched=%d, want 1", dispatched)
	}

	calls := rev.snapshot()
	if len(calls) != 1 {
		t.Fatalf("AutoRevert call count = %d, want 1; calls=%+v", len(calls), calls)
	}
	if calls[0].ADRID != 24 {
		t.Errorf("AutoRevert ADRID = %d, want 24 (inv-zen-141 attribution)", calls[0].ADRID)
	}

	events := em.snapshot()
	var found *eventlog.DoctrineAutonomousReverted
	for _, ev := range events {
		if x, ok := ev.(eventlog.DoctrineAutonomousReverted); ok {
			found = &x
			break
		}
	}
	if found == nil {
		t.Fatalf("expected DoctrineAutonomousReverted event, none found in %+v", events)
	}
	if found.RulePath != rulePath || found.TelemetryCategory != "cost" {
		t.Errorf("event mismatch: rulepath=%q category=%q", found.RulePath, found.TelemetryCategory)
	}
}

func TestTelemetrySubscriberSuppressesUnderCapaFirewall(t *testing.T) {
	ctx := context.Background()
	rev := &fakeReverter{}
	em := &telemetryFakeEmitter{}
	clk := clock.NewFake(time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC))

	costAgg := aggregator.NewCost()
	rulePath := "autonomy.cost_degradation.soft_check_usd"
	costAgg.RegisterRule(aggregator.RuleSpec{
		RulePath:        rulePath,
		WindowSessions:  5,
		ThresholdPct:    0.7,
		AnomalousEvents: []eventlog.EventType{eventlog.EvtBudgetDegradationApplied},
	})
	for i := 0; i < 5; i++ {
		costAgg.RecordSession(aggregator.SessionContext{
			SessionID:         fmt.Sprintf("s-%d", i),
			Timestamp:         clk.Now().Add(time.Duration(i) * time.Hour),
			LastAppliedADR:    "ADR-0024",
			EventsThisSession: []eventlog.PayloadEncoder{eventlog.BudgetDegradationApplied{ThresholdPct: 60}},
		})
	}

	ts := amendment.NewTelemetrySubscriber(amendment.TelemetrySubscriberConfig{
		Cost:         costAgg,
		Reverter:     rev,
		Emitter:      em,
		Clock:        clk,
		CapaFirewall: fakeCapaProbe{bound: true},
	})

	dispatched, err := ts.EvaluateProject(ctx, "secure-project")
	if err != nil {
		t.Fatalf("EvaluateProject: %v", err)
	}
	if dispatched != 0 {
		t.Errorf("dispatched=%d, want 0 (capa-firewall hard guard)", dispatched)
	}

	if calls := rev.snapshot(); len(calls) != 0 {
		t.Fatalf("AutoRevert called under capa-firewall (inv-zen-100 violated): %+v", calls)
	}
}

// TestTelemetrySubscriberSkipsBelowMinSessions verifies Q13 C threshold
// rule: revert MUST NOT fire if total_sessions < revert_window_sessions.
func TestTelemetrySubscriberSkipsBelowMinSessions(t *testing.T) {
	ctx := context.Background()
	rev := &fakeReverter{}
	em := &telemetryFakeEmitter{}
	clk := clock.NewFake(time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC))

	costAgg := aggregator.NewCost()
	rulePath := "autonomy.cost_degradation.soft_check_usd"
	costAgg.RegisterRule(aggregator.RuleSpec{
		RulePath:        rulePath,
		WindowSessions:  20,
		ThresholdPct:    0.7,
		AnomalousEvents: []eventlog.EventType{eventlog.EvtBudgetDegradationApplied},
	})
	for i := 0; i < 3; i++ {
		costAgg.RecordSession(aggregator.SessionContext{
			SessionID:         fmt.Sprintf("s-%d", i),
			Timestamp:         clk.Now().Add(time.Duration(i) * time.Hour),
			LastAppliedADR:    "ADR-0024",
			EventsThisSession: []eventlog.PayloadEncoder{eventlog.BudgetDegradationApplied{ThresholdPct: 80}},
		})
	}

	ts := amendment.NewTelemetrySubscriber(amendment.TelemetrySubscriberConfig{
		Cost:     costAgg,
		Reverter: rev,
		Emitter:  em,
		Clock:    clk,
	})

	dispatched, _ := ts.EvaluateProject(ctx, "internal-platform-x")
	if dispatched != 0 {
		t.Errorf("dispatched=%d, want 0 (below min sessions)", dispatched)
	}
	if calls := rev.snapshot(); len(calls) != 0 {
		t.Fatalf("AutoRevert called below min-window (Q13 C violated): %+v", calls)
	}
}

// TestTelemetrySubscriberSkipsWhenNoLastApplied verifies inv-zen-141:
// if no DoctrineAmendmentApplied is in the window (lastApplied == ""),
// revert MUST NOT fire.
func TestTelemetrySubscriberSkipsWhenNoLastApplied(t *testing.T) {
	ctx := context.Background()
	rev := &fakeReverter{}
	em := &telemetryFakeEmitter{}
	clk := clock.NewFake(time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC))

	costAgg := aggregator.NewCost()
	rulePath := "autonomy.cost_degradation.soft_check_usd"
	costAgg.RegisterRule(aggregator.RuleSpec{
		RulePath:        rulePath,
		WindowSessions:  5,
		ThresholdPct:    0.7,
		AnomalousEvents: []eventlog.EventType{eventlog.EvtBudgetDegradationApplied},
	})
	for i := 0; i < 5; i++ {
		costAgg.RecordSession(aggregator.SessionContext{
			SessionID:         fmt.Sprintf("s-%d", i),
			Timestamp:         clk.Now().Add(time.Duration(i) * time.Hour),
			LastAppliedADR:    "",
			EventsThisSession: []eventlog.PayloadEncoder{eventlog.BudgetDegradationApplied{ThresholdPct: 80}},
		})
	}

	ts := amendment.NewTelemetrySubscriber(amendment.TelemetrySubscriberConfig{
		Cost:     costAgg,
		Reverter: rev,
		Emitter:  em,
		Clock:    clk,
	})

	dispatched, _ := ts.EvaluateProject(ctx, "internal-platform-x")
	if dispatched != 0 {
		t.Errorf("dispatched=%d, want 0 (no attribution candidate per inv-zen-141)", dispatched)
	}
	if calls := rev.snapshot(); len(calls) != 0 {
		t.Fatalf("AutoRevert called without attribution candidate (inv-zen-141 violated): %+v", calls)
	}
}

func TestTelemetrySubscriberSkipsWhenAboveThreshold(t *testing.T) {
	ctx := context.Background()
	rev := &fakeReverter{}
	em := &telemetryFakeEmitter{}
	clk := clock.NewFake(time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC))

	costAgg := aggregator.NewCost()
	rulePath := "autonomy.cost_degradation.soft_check_usd"
	costAgg.RegisterRule(aggregator.RuleSpec{
		RulePath:        rulePath,
		WindowSessions:  5,
		ThresholdPct:    0.5,
		AnomalousEvents: []eventlog.EventType{eventlog.EvtBudgetDegradationApplied},
	})

	for i := 0; i < 5; i++ {
		var events []eventlog.PayloadEncoder
		if i >= 3 {
			events = []eventlog.PayloadEncoder{eventlog.BudgetDegradationApplied{ThresholdPct: 80}}
		}
		costAgg.RecordSession(aggregator.SessionContext{
			SessionID:         fmt.Sprintf("s-%d", i),
			Timestamp:         clk.Now().Add(time.Duration(i) * time.Hour),
			LastAppliedADR:    "ADR-0024",
			EventsThisSession: events,
		})
	}

	ts := amendment.NewTelemetrySubscriber(amendment.TelemetrySubscriberConfig{
		Cost:     costAgg,
		Reverter: rev,
		Emitter:  em,
		Clock:    clk,
	})

	dispatched, _ := ts.EvaluateProject(ctx, "internal-platform-x")
	if dispatched != 0 || len(rev.snapshot()) != 0 {
		t.Errorf("AutoRevert fired despite pct above threshold")
	}
}

func TestTelemetrySubscriberDispatchPropagatesReverterError(t *testing.T) {
	ctx := context.Background()
	rev := &fakeReverter{failErr: errors.New("git revert failed")}
	em := &telemetryFakeEmitter{}
	clk := clock.NewFake(time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC))

	costAgg := aggregator.NewCost()
	rulePath := "autonomy.cost_degradation.soft_check_usd"
	costAgg.RegisterRule(aggregator.RuleSpec{
		RulePath:        rulePath,
		WindowSessions:  5,
		ThresholdPct:    0.7,
		AnomalousEvents: []eventlog.EventType{eventlog.EvtBudgetDegradationApplied},
	})
	for i := 0; i < 5; i++ {
		costAgg.RecordSession(aggregator.SessionContext{
			SessionID:         fmt.Sprintf("s-%d", i),
			Timestamp:         clk.Now().Add(time.Duration(i) * time.Hour),
			LastAppliedADR:    "ADR-0024",
			EventsThisSession: []eventlog.PayloadEncoder{eventlog.BudgetDegradationApplied{ThresholdPct: 80}},
		})
	}

	ts := amendment.NewTelemetrySubscriber(amendment.TelemetrySubscriberConfig{
		Cost:     costAgg,
		Reverter: rev,
		Emitter:  em,
		Clock:    clk,
	})

	dispatched, _ := ts.EvaluateProject(ctx, "internal-platform-x")
	if dispatched != 0 {
		t.Errorf("dispatched=%d, want 0 (revert err treated as undispatched)", dispatched)
	}
	if calls := rev.snapshot(); len(calls) != 1 {
		t.Errorf("AutoRevert call count=%d, want 1 (the failed attempt)", len(calls))
	}

	events := em.snapshot()
	var foundFailed bool
	for _, ev := range events {
		if x, ok := ev.(eventlog.DoctrineAmendmentApplyFailed); ok && x.Stage == "auto-revert" {
			foundFailed = true
			break
		}
	}
	if !foundFailed {
		t.Errorf("expected DoctrineAmendmentApplyFailed event with Stage=auto-revert; got %+v", events)
	}
}

func TestTelemetrySubscriberMalformedADRIDEmitsParseFailure(t *testing.T) {
	ctx := context.Background()
	rev := &fakeReverter{}
	em := &telemetryFakeEmitter{}
	clk := clock.NewFake(time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC))

	costAgg := aggregator.NewCost()
	rulePath := "autonomy.cost_degradation.soft_check_usd"
	costAgg.RegisterRule(aggregator.RuleSpec{
		RulePath:        rulePath,
		WindowSessions:  5,
		ThresholdPct:    0.7,
		AnomalousEvents: []eventlog.EventType{eventlog.EvtBudgetDegradationApplied},
	})
	for i := 0; i < 5; i++ {
		costAgg.RecordSession(aggregator.SessionContext{
			SessionID:         fmt.Sprintf("s-%d", i),
			Timestamp:         clk.Now().Add(time.Duration(i) * time.Hour),
			LastAppliedADR:    "BOGUS-ID-NOT-ADR-FORMAT",
			EventsThisSession: []eventlog.PayloadEncoder{eventlog.BudgetDegradationApplied{ThresholdPct: 80}},
		})
	}

	ts := amendment.NewTelemetrySubscriber(amendment.TelemetrySubscriberConfig{
		Cost:     costAgg,
		Reverter: rev,
		Emitter:  em,
		Clock:    clk,
	})

	dispatched, _ := ts.EvaluateProject(ctx, "internal-platform-x")
	if dispatched != 0 || len(rev.snapshot()) != 0 {
		t.Errorf("dispatched on malformed ADR ID")
	}
	events := em.snapshot()
	var foundFailed bool
	for _, ev := range events {
		if x, ok := ev.(eventlog.DoctrineAmendmentApplyFailed); ok && x.Stage == "auto-revert-parse" {
			foundFailed = true
			break
		}
	}
	if !foundFailed {
		t.Errorf("expected DoctrineAmendmentApplyFailed event with Stage=auto-revert-parse; got %+v", events)
	}
}

func TestTelemetrySubscriberCooldownSuppressesRevert(t *testing.T) {
	ctx := context.Background()
	rev := &fakeReverter{}
	em := &telemetryFakeEmitter{}
	clk := clock.NewFake(time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC))

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
		Policy:   fakePolicy{d: 24 * time.Hour},
	})

	for i := 0; i < 5; i++ {
		costAgg.RecordSession(aggregator.SessionContext{
			SessionID:         fmt.Sprintf("s-%d", i),
			Timestamp:         clk.Now().Add(time.Duration(i) * time.Hour),
			LastAppliedADR:    "ADR-0024",
			EventsThisSession: []eventlog.PayloadEncoder{eventlog.BudgetDegradationApplied{ThresholdPct: 80}},
		})
	}
	if dispatched, _ := ts.EvaluateProject(ctx, "internal-platform-x"); dispatched != 1 {
		t.Fatalf("first pass dispatched=%d, want 1", dispatched)
	}
	if calls := len(rev.snapshot()); calls != 1 {
		t.Fatalf("first pass calls=%d, want 1", calls)
	}

	clk.Advance(time.Hour)

	for i := 5; i < 10; i++ {
		costAgg.RecordSession(aggregator.SessionContext{
			SessionID:         fmt.Sprintf("s-%d", i),
			Timestamp:         clk.Now().Add(time.Duration(i) * time.Hour),
			LastAppliedADR:    "ADR-0024",
			EventsThisSession: []eventlog.PayloadEncoder{eventlog.BudgetDegradationApplied{ThresholdPct: 80}},
		})
	}
	if dispatched, _ := ts.EvaluateProject(ctx, "internal-platform-x"); dispatched != 0 {
		t.Errorf("second pass dispatched=%d, want 0 (cooldown suppression)", dispatched)
	}
	if calls := len(rev.snapshot()); calls != 1 {
		t.Errorf("calls=%d, want 1 (cooldown should suppress second attempt)", calls)
	}

	events := em.snapshot()
	var found *eventlog.DoctrineRevertSuppressedCooldown
	for _, ev := range events {
		if x, ok := ev.(eventlog.DoctrineRevertSuppressedCooldown); ok {
			found = &x
			break
		}
	}
	if found == nil {
		t.Fatalf("expected DoctrineRevertSuppressedCooldown event")
	}
	if found.RulePath != rulePath || found.ADRID != "ADR-0024" {
		t.Errorf("event mismatch: rule=%q adr=%q", found.RulePath, found.ADRID)
	}

	if found.CooldownRemainingHours < 22.5 || found.CooldownRemainingHours > 23.5 {
		t.Errorf("CooldownRemainingHours=%v, want ~23h", found.CooldownRemainingHours)
	}
}

func TestParseADRIDHappyPaths(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"ADR-0024", 24},
		{"ADR-1", 1},
		{"24", 24},
		{"ADR-9999", 9999},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got, err := amendment.ParseADRIDForTest(c.in)
			if err != nil {
				t.Fatalf("ParseADRID: %v", err)
			}
			if got != c.want {
				t.Errorf("got=%d, want %d", got, c.want)
			}
		})
	}
}

func TestParseADRIDFailures(t *testing.T) {
	for _, in := range []string{"", "ADR-", "ADR-X", "ADR-12X", "BOGUS", "ADR-12-X"} {
		if _, err := amendment.ParseADRIDForTest(in); err == nil {
			t.Errorf("expected error for malformed ADR ID %q", in)
		}
	}
}

type fakeCooldownTracker struct {
	mu   sync.Mutex
	last map[string]struct {
		when     time.Time
		cooldown time.Duration
	}
}

func newFakeCooldownTracker() *fakeCooldownTracker {
	return &fakeCooldownTracker{last: map[string]struct {
		when     time.Time
		cooldown time.Duration
	}{}}
}

func (f *fakeCooldownTracker) LastRevertedAt(rulePath string) (time.Time, time.Duration, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if e, ok := f.last[rulePath]; ok {
		return e.when, e.cooldown, true
	}
	return time.Time{}, 0, false
}

func (f *fakeCooldownTracker) MarkReverted(rulePath string, when time.Time, cooldown time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.last[rulePath] = struct {
		when     time.Time
		cooldown time.Duration
	}{when: when, cooldown: cooldown}
}

func TestTelemetrySubscriberMergeAndRecoveryAggregatorsAlsoFire(t *testing.T) {
	ctx := context.Background()
	rev := &fakeReverter{}
	em := &telemetryFakeEmitter{}
	clk := clock.NewFake(time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC))

	costAgg := aggregator.NewCost()
	mergeAgg := aggregator.NewMerge()
	recoveryAgg := aggregator.NewRecovery()

	costAgg.RegisterRule(aggregator.RuleSpec{
		RulePath:        "rule.cost",
		WindowSessions:  3,
		ThresholdPct:    0.5,
		AnomalousEvents: []eventlog.EventType{eventlog.EvtBudgetDegradationApplied},
	})
	mergeAgg.RegisterRule(aggregator.RuleSpec{
		RulePath:        "rule.merge",
		WindowSessions:  3,
		ThresholdPct:    0.5,
		AnomalousEvents: []eventlog.EventType{eventlog.EvtBudgetSnapshotError},
	})
	recoveryAgg.RegisterRule(aggregator.RuleSpec{
		RulePath:        "rule.recovery",
		WindowSessions:  3,
		ThresholdPct:    0.5,
		AnomalousEvents: []eventlog.EventType{eventlog.EvtWorkerDeath},
	})

	for i := 0; i < 3; i++ {
		costAgg.RecordSession(aggregator.SessionContext{
			SessionID: fmt.Sprintf("sc-%d", i), Timestamp: clk.Now().Add(time.Duration(i) * time.Hour),
			LastAppliedADR:    "ADR-100",
			EventsThisSession: []eventlog.PayloadEncoder{eventlog.BudgetDegradationApplied{ThresholdPct: 80}},
		})
		mergeAgg.RecordSession(aggregator.SessionContext{
			SessionID: fmt.Sprintf("sm-%d", i), Timestamp: clk.Now().Add(time.Duration(i) * time.Hour),
			LastAppliedADR:    "ADR-200",
			EventsThisSession: []eventlog.PayloadEncoder{eventlog.BudgetSnapshotError{Error: "x"}},
		})
		recoveryAgg.RecordSession(aggregator.SessionContext{
			SessionID: fmt.Sprintf("sr-%d", i), Timestamp: clk.Now().Add(time.Duration(i) * time.Hour),
			LastAppliedADR:    "ADR-300",
			EventsThisSession: []eventlog.PayloadEncoder{eventlog.WorkerDeath{WorkerID: "w", Class: "PERMANENT_INFRA"}},
		})
	}

	cd := newFakeCooldownTracker()
	ts := amendment.NewTelemetrySubscriber(amendment.TelemetrySubscriberConfig{
		Cost:     costAgg,
		Merge:    mergeAgg,
		Recovery: recoveryAgg,
		Reverter: rev,
		Emitter:  em,
		Clock:    clk,
		Cooldown: cd,
	})

	dispatched, err := ts.EvaluateProject(ctx, "internal-platform-x")
	if err != nil {
		t.Fatalf("EvaluateProject: %v", err)
	}
	if dispatched != 3 {
		t.Errorf("dispatched=%d, want 3 (one per aggregator)", dispatched)
	}

	events := em.snapshot()
	categories := map[string]bool{}
	for _, ev := range events {
		if x, ok := ev.(eventlog.DoctrineAutonomousReverted); ok {
			categories[x.TelemetryCategory] = true
		}
	}
	for _, want := range []string{"cost", "merge", "recovery"} {
		if !categories[want] {
			t.Errorf("missing TelemetryCategory=%q in emitted events", want)
		}
	}
}
