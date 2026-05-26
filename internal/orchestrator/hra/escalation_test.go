package hra_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/hra"
)

func TestHandleDisagreement_TacticalEmitsToStrategic(t *testing.T) {
	log := &fakeEventLog{}
	fc := clock.NewFake(time.Unix(1_700_000_000, 0))
	rules := hra.NewEscalationRules(log, fc, "sess-1", "proj-1")

	rules.HandleDisagreement(hra.LayerTactical, hra.Finding{
		Verdict:      "needs_fix",
		Disagreement: true,
		Split:        [2]int{2, 2},
	})

	appends := log.Appends()
	if len(appends) != 1 {
		t.Fatalf("appended = %d, want 1", len(appends))
	}
	got := appends[0]
	if got.Type != eventlog.EvtEscalationDecision {
		t.Errorf("type = %v, want EvtEscalationDecision", got.Type)
	}
	if got.SessionID != "sess-1" {
		t.Errorf("session = %q, want sess-1", got.SessionID)
	}
	if got.ProjectID != "proj-1" {
		t.Errorf("project = %q, want proj-1", got.ProjectID)
	}
	if got.Payload["from"] != "tactical" {
		t.Errorf("from = %v, want tactical", got.Payload["from"])
	}
	if got.Payload["to"] != "strategic" {
		t.Errorf("to = %v, want strategic", got.Payload["to"])
	}
	if got.Payload["class"] != "tactical_disagreement" {
		t.Errorf("class = %v, want tactical_disagreement", got.Payload["class"])
	}
	if got.Payload["verdict"] != "needs_fix" {
		t.Errorf("verdict = %v, want needs_fix", got.Payload["verdict"])
	}
	if got.Payload["disagreement"] != true {
		t.Errorf("disagreement = %v, want true", got.Payload["disagreement"])
	}

	if got.Payload["needs_fix"] != false {
		t.Errorf("needs_fix = %v, want false", got.Payload["needs_fix"])
	}

	split, ok := got.Payload["split"].([]int)
	if !ok {
		t.Fatalf("split type = %T, want []int", got.Payload["split"])
	}
	if len(split) != 2 || split[0] != 2 || split[1] != 2 {
		t.Errorf("split = %v, want [2 2]", split)
	}
	// Tactical-layer escalation MUST NOT bump the L3 counter.
	if esc, _, _ := rules.L3Counters(); esc != 0 {
		t.Errorf("l3 escalations after tactical = %d, want 0", esc)
	}
}

func TestHandleDisagreement_StrategicEscalatesToL4(t *testing.T) {
	log := &fakeEventLog{}
	fc := clock.NewFake(time.Unix(1_700_000_000, 0))
	rules := hra.NewEscalationRules(log, fc, "s", "p")

	for i := 0; i < 3; i++ {
		rules.AckedAtLayer(hra.LayerStrategic)
	}

	rules.HandleDisagreement(hra.LayerStrategic, hra.Finding{
		Verdict:      "needs_fix",
		Disagreement: true,
		Split:        [2]int{1, 1},
	})

	appends := log.Appends()
	if len(appends) != 1 {
		t.Fatalf("appended = %d, want 1", len(appends))
	}
	got := appends[0]
	if got.Payload["from"] != "strategic" {
		t.Errorf("from = %v, want strategic", got.Payload["from"])
	}
	if got.Payload["to"] != "architectural" {
		t.Errorf("to = %v, want architectural", got.Payload["to"])
	}
	if got.Payload["class"] != "strategic_deadlock" {
		t.Errorf("class = %v, want strategic_deadlock", got.Payload["class"])
	}
	if esc, acks, _ := rules.L3Counters(); esc != 1 || acks != 3 {
		t.Errorf("counters = (%d, %d), want (1, 3)", esc, acks)
	}
}

func TestHandleDisagreement_ArchitecturalEscalatesToOperator(t *testing.T) {
	log := &fakeEventLog{}
	fc := clock.NewFake(time.Unix(1_700_000_000, 0))
	rules := hra.NewEscalationRules(log, fc, "s", "p")

	rules.HandleDisagreement(hra.LayerArchitectural, hra.Finding{
		Verdict:  "needs_fix",
		NeedsFix: true,
		Summary:  "inv violation",
	})

	appends := log.Appends()
	if len(appends) != 1 {
		t.Fatalf("appended = %d, want 1", len(appends))
	}
	got := appends[0]
	if got.Payload["from"] != "architectural" {
		t.Errorf("from = %v, want architectural", got.Payload["from"])
	}
	if got.Payload["to"] != "operator" {
		t.Errorf("to = %v, want operator", got.Payload["to"])
	}
	if got.Payload["class"] != "architectural" {
		t.Errorf("class = %v, want architectural", got.Payload["class"])
	}
	if got.Payload["needs_fix"] != true {
		t.Errorf("needs_fix = %v, want true", got.Payload["needs_fix"])
	}
	if got.Payload["summary"] != "inv violation" {
		t.Errorf("summary = %v, want inv violation", got.Payload["summary"])
	}
	// Architectural escalation MUST NOT bump the L3 counter.
	if esc, _, _ := rules.L3Counters(); esc != 0 {
		t.Errorf("l3 escalations after architectural = %d, want 0", esc)
	}
}

func TestHandleDisagreement_RejectsUnknownLayer(t *testing.T) {
	log := &fakeEventLog{}
	fc := clock.NewFake(time.Unix(1_700_000_000, 0))
	rules := hra.NewEscalationRules(log, fc, "s", "p")

	rules.HandleDisagreement(hra.Layer(0), hra.Finding{Verdict: "needs_fix"})
	rules.HandleDisagreement(hra.Layer(99), hra.Finding{Verdict: "needs_fix"})

	if appends := log.Appends(); len(appends) != 0 {
		t.Errorf("unknown-layer disagreement emitted %d events, want 0 (workers/unknown are not reviewers)", len(appends))
	}
}

func TestHandleDisagreement_FixProposalsIncluded(t *testing.T) {
	log := &fakeEventLog{}
	fc := clock.NewFake(time.Unix(1_700_000_000, 0))
	rules := hra.NewEscalationRules(log, fc, "s", "p")

	rules.HandleDisagreement(hra.LayerTactical, hra.Finding{
		Verdict:      "needs_fix",
		Disagreement: true,
		FixProposals: []string{"A", "B"},
	})

	appends := log.Appends()
	if len(appends) != 1 {
		t.Fatalf("appended = %d, want 1", len(appends))
	}
	props, ok := appends[0].Payload["fix_proposals"].([]string)
	if !ok {
		t.Fatalf("fix_proposals type = %T, want []string", appends[0].Payload["fix_proposals"])
	}
	if len(props) != 2 || props[0] != "A" || props[1] != "B" {
		t.Errorf("fix_proposals = %v, want [A B]", props)
	}
}

func TestHandleDisagreement_OmitsEmptySummaryAndFixProposals(t *testing.T) {
	log := &fakeEventLog{}
	fc := clock.NewFake(time.Unix(1_700_000_000, 0))
	rules := hra.NewEscalationRules(log, fc, "s", "p")

	rules.HandleDisagreement(hra.LayerTactical, hra.Finding{
		Verdict:      "needs_fix",
		Disagreement: true,
	})

	appends := log.Appends()
	if len(appends) != 1 {
		t.Fatalf("appended = %d, want 1", len(appends))
	}
	if _, ok := appends[0].Payload["summary"]; ok {
		t.Errorf("summary present in payload but Finding.Summary was empty")
	}
	if _, ok := appends[0].Payload["fix_proposals"]; ok {
		t.Errorf("fix_proposals present in payload but Finding.FixProposals was empty")
	}
}

func TestAckedAtLayer_OnlyStrategicCounted(t *testing.T) {
	log := &fakeEventLog{}
	fc := clock.NewFake(time.Unix(1_700_000_000, 0))
	rules := hra.NewEscalationRules(log, fc, "s", "p")

	rules.AckedAtLayer(hra.LayerTactical)
	if _, acks, _ := rules.L3Counters(); acks != 0 {
		t.Errorf("acks after tactical ack = %d, want 0", acks)
	}

	rules.AckedAtLayer(hra.LayerStrategic)
	if _, acks, _ := rules.L3Counters(); acks != 1 {
		t.Errorf("acks after strategic ack = %d, want 1", acks)
	}

	rules.AckedAtLayer(hra.LayerArchitectural)
	if _, acks, _ := rules.L3Counters(); acks != 1 {
		t.Errorf("acks after architectural ack = %d, want 1 (architectural ignored)", acks)
	}

	if appends := log.Appends(); len(appends) != 0 {
		t.Errorf("AckedAtLayer emitted %d events, want 0", len(appends))
	}
}

func TestL3Counters_RatioCalculation(t *testing.T) {
	log := &fakeEventLog{}
	fc := clock.NewFake(time.Unix(1_700_000_000, 0))

	cases := []struct {
		name     string
		escs     int
		acks     int
		wantEsc  int64
		wantAcks int64
		wantR    float64
	}{
		{"empty", 0, 0, 0, 0, 0},
		{"one-esc-no-ack", 1, 0, 1, 0, 1.0},
		{"one-esc-three-acks", 1, 3, 1, 3, 0.25},
		{"five-and-five", 5, 5, 5, 5, 0.5},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rules := hra.NewEscalationRules(log, fc, "s", "p")
			for i := 0; i < tc.escs; i++ {
				rules.HandleDisagreement(hra.LayerStrategic, hra.Finding{
					Verdict:      "needs_fix",
					Disagreement: true,
				})
			}
			for i := 0; i < tc.acks; i++ {
				rules.AckedAtLayer(hra.LayerStrategic)
			}
			esc, acks, ratio := rules.L3Counters()
			if esc != tc.wantEsc || acks != tc.wantAcks {
				t.Errorf("counters = (%d, %d), want (%d, %d)", esc, acks, tc.wantEsc, tc.wantAcks)
			}
			if ratio != tc.wantR {
				t.Errorf("ratio = %v, want %v", ratio, tc.wantR)
			}
		})
	}
}

func TestCooldown_DefaultThirtySeconds(t *testing.T) {
	log := &fakeEventLog{}
	fc := clock.NewFake(time.Unix(1_700_000_000, 0))
	rules := hra.NewEscalationRules(log, fc, "s", "p")

	if got := rules.Cooldown(); got != 30*time.Second {
		t.Errorf("Cooldown() = %v, want 30s", got)
	}
}

func TestHandleDisagreement_RaceFreeUnderConcurrent(t *testing.T) {
	log := &fakeEventLog{}
	fc := clock.NewFake(time.Unix(1_700_000_000, 0))
	rules := hra.NewEscalationRules(log, fc, "s", "p")

	const N = 100

	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rules.HandleDisagreement(hra.LayerTactical, hra.Finding{
				Verdict:      "needs_fix",
				Disagreement: true,
			})
		}()
	}
	wg.Wait()
	if esc, _, _ := rules.L3Counters(); esc != 0 {
		t.Errorf("l3 escalations after %d tactical = %d, want 0", N, esc)
	}
	if got := len(log.Appends()); got != N {
		t.Errorf("appends after %d tactical = %d, want %d", N, got, N)
	}

	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rules.HandleDisagreement(hra.LayerStrategic, hra.Finding{
				Verdict:      "needs_fix",
				Disagreement: true,
			})
		}()
	}
	wg.Wait()
	if esc, _, _ := rules.L3Counters(); esc != int64(N) {
		t.Errorf("l3 escalations after %d strategic = %d, want %d", N, esc, N)
	}
	if got := len(log.Appends()); got != 2*N {
		t.Errorf("total appends = %d, want %d", got, 2*N)
	}
}

func TestIsL3Deadlock_BelowThresholdReturnsFalse(t *testing.T) {
	log := &fakeEventLog{}
	fc := clock.NewFake(time.Unix(1_700_000_000, 0))
	rules := hra.NewEscalationRules(log, fc, "s", "p")

	rules.HandleDisagreement(hra.LayerStrategic, hra.Finding{
		Verdict:      "needs_fix",
		Disagreement: true,
	})
	for i := 0; i < 4; i++ {
		rules.AckedAtLayer(hra.LayerStrategic)
	}

	if got := rules.IsL3Deadlock(0.5); got {
		t.Errorf("IsL3Deadlock(0.5) = true, want false (ratio 0.2 < 0.5)")
	}
}

func TestIsL3Deadlock_AboveThresholdReturnsTrue(t *testing.T) {
	log := &fakeEventLog{}
	fc := clock.NewFake(time.Unix(1_700_000_000, 0))
	rules := hra.NewEscalationRules(log, fc, "s", "p")

	for i := 0; i < 5; i++ {
		rules.HandleDisagreement(hra.LayerStrategic, hra.Finding{
			Verdict:      "needs_fix",
			Disagreement: true,
		})
	}
	rules.AckedAtLayer(hra.LayerStrategic)

	if got := rules.IsL3Deadlock(0.5); !got {
		t.Errorf("IsL3Deadlock(0.5) = false, want true (ratio ≈0.833 ≥ 0.5)")
	}
}

func TestIsL3Deadlock_NoEventsReturnsFalse(t *testing.T) {
	log := &fakeEventLog{}
	fc := clock.NewFake(time.Unix(1_700_000_000, 0))
	rules := hra.NewEscalationRules(log, fc, "s", "p")

	if got := rules.IsL3Deadlock(0.5); got {
		t.Errorf("IsL3Deadlock(0.5) on fresh rules = true, want false (vacuous)")
	}
}

func TestIsL3Deadlock_ZeroEscalationsReturnsFalse(t *testing.T) {
	log := &fakeEventLog{}
	fc := clock.NewFake(time.Unix(1_700_000_000, 0))
	rules := hra.NewEscalationRules(log, fc, "s", "p")

	for i := 0; i < 5; i++ {
		rules.AckedAtLayer(hra.LayerStrategic)
	}

	if got := rules.IsL3Deadlock(0.5); got {
		t.Errorf("IsL3Deadlock(0.5) with only-acks = true, want false (esc==0 short-circuit)")
	}
}

func TestIsL3Deadlock_AtThresholdReturnsTrue(t *testing.T) {
	log := &fakeEventLog{}
	fc := clock.NewFake(time.Unix(1_700_000_000, 0))
	rules := hra.NewEscalationRules(log, fc, "s", "p")

	rules.HandleDisagreement(hra.LayerStrategic, hra.Finding{
		Verdict:      "needs_fix",
		Disagreement: true,
	})
	rules.AckedAtLayer(hra.LayerStrategic)

	if got := rules.IsL3Deadlock(0.5); !got {
		t.Errorf("IsL3Deadlock(0.5) at exact threshold = false, want true (boundary inclusive)")
	}
}

func TestHandleDisagreement_StrategicUpgradesClassWhenDeadlocked(t *testing.T) {
	log := &fakeEventLog{}
	fc := clock.NewFake(time.Unix(1_700_000_000, 0))
	rules := hra.NewEscalationRules(log, fc, "s", "p")

	for i := 0; i < 3; i++ {
		rules.HandleDisagreement(hra.LayerStrategic, hra.Finding{
			Verdict:      "needs_fix",
			Disagreement: true,
		})
	}

	appends := log.Appends()
	if len(appends) != 3 {
		t.Fatalf("appended = %d, want 3", len(appends))
	}
	last := appends[len(appends)-1]
	if last.Payload["class"] != "strategic_persistent_deadlock" {
		t.Errorf("last class = %v, want strategic_persistent_deadlock", last.Payload["class"])
	}

	for i, ev := range appends {
		if ev.Payload["class"] != "strategic_persistent_deadlock" {
			t.Errorf("call %d class = %v, want strategic_persistent_deadlock", i, ev.Payload["class"])
		}
	}
}

func TestHandleDisagreement_StrategicNoUpgradeWhenAcksDominate(t *testing.T) {
	log := &fakeEventLog{}
	fc := clock.NewFake(time.Unix(1_700_000_000, 0))
	rules := hra.NewEscalationRules(log, fc, "s", "p")

	for i := 0; i < 5; i++ {
		rules.AckedAtLayer(hra.LayerStrategic)
	}
	rules.HandleDisagreement(hra.LayerStrategic, hra.Finding{
		Verdict:      "needs_fix",
		Disagreement: true,
	})

	appends := log.Appends()
	if len(appends) != 1 {
		t.Fatalf("appended = %d, want 1", len(appends))
	}
	if appends[0].Payload["class"] != "strategic_deadlock" {
		t.Errorf("class = %v, want strategic_deadlock (acks-dominate regime)", appends[0].Payload["class"])
	}
}

func TestHandleDisagreement_TacticalNotAffectedByL3Threshold(t *testing.T) {
	log := &fakeEventLog{}
	fc := clock.NewFake(time.Unix(1_700_000_000, 0))
	rules := hra.NewEscalationRules(log, fc, "s", "p")

	for i := 0; i < 10; i++ {
		rules.HandleDisagreement(hra.LayerStrategic, hra.Finding{
			Verdict:      "needs_fix",
			Disagreement: true,
		})
	}
	if !rules.IsL3Deadlock(0.5) {
		t.Fatalf("precondition: IsL3Deadlock(0.5) = false; setup wrong")
	}

	rules.HandleDisagreement(hra.LayerTactical, hra.Finding{
		Verdict:      "needs_fix",
		Disagreement: true,
	})

	appends := log.Appends()
	last := appends[len(appends)-1]
	if last.Payload["from"] != "tactical" {
		t.Fatalf("last from = %v, want tactical (test setup wrong)", last.Payload["from"])
	}
	if last.Payload["class"] != "tactical_disagreement" {
		t.Errorf("tactical class = %v, want tactical_disagreement (upgrade is strategic-only)",
			last.Payload["class"])
	}
}

func TestIsL3Deadlock_RaceFreeUnderConcurrent(t *testing.T) {
	log := &fakeEventLog{}
	fc := clock.NewFake(time.Unix(1_700_000_000, 0))
	rules := hra.NewEscalationRules(log, fc, "s", "p")

	const N = 100
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			rules.HandleDisagreement(hra.LayerStrategic, hra.Finding{
				Verdict:      "needs_fix",
				Disagreement: true,
			})
		}()
		go func() {
			defer wg.Done()
			rules.AckedAtLayer(hra.LayerStrategic)
		}()
	}
	wg.Wait()

	esc, acks, ratio := rules.L3Counters()
	if esc != int64(N) {
		t.Errorf("l3 escalations = %d, want %d", esc, N)
	}
	if acks != int64(N) {
		t.Errorf("l3 acks = %d, want %d", acks, N)
	}
	if ratio != 0.5 {
		t.Errorf("ratio = %v, want 0.5", ratio)
	}
	if !rules.IsL3Deadlock(0.5) {
		t.Errorf("IsL3Deadlock(0.5) = false, want true at exact threshold")
	}
}

func TestHRACoordinator_DefaultWiresEscalationRules(t *testing.T) {
	fake := clock.NewFake(time.Unix(0, 0))
	log := &fakeEventLog{}
	cfg := hra.Config{
		Clock:    fake,
		EventLog: log,
		Context: &fakeCoordinatorContext{
			sessionID: "sess-default-esc",
			projectID: "proj-default-esc",
			doctrine:  "max-scope",
		},
	}
	c, err := hra.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c.SetArchitecturalAggregatorForTest(disagreementAggregator)

	cancel, errCh := startCoord(t, c, log)
	t.Cleanup(func() {
		cancel()
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
			t.Fatalf("Run did not exit after cancel")
		}
	})

	injectArchitecturalEvent(t, log, "proj-default-esc")
	sub := log.subscribeForFilter(eventlog.EvtTacticalAggregation)
	if !fake.BlockUntilCondition(func() bool { return len(sub.events) == 0 }, 2*time.Second) {
		t.Fatalf("architectural cadence did not drain injected events")
	}
	fake.Advance(30*time.Minute + time.Second)

	if !fake.BlockUntilCondition(func() bool {
		return log.appendedOf(eventlog.EvtEscalationDecision) >= 2
	}, 2*time.Second) {
		t.Fatalf("expected ≥ 2 escalation rows under default-wire (got %d)",
			log.appendedOf(eventlog.EvtEscalationDecision))
	}

	var newShape eventlog.Event
	for _, ev := range log.Appends() {
		if ev.Type != eventlog.EvtEscalationDecision {
			continue
		}
		if _, ok := ev.Payload["to"]; ok {
			newShape = ev
			break
		}
	}
	if newShape.Type != eventlog.EvtEscalationDecision {
		t.Fatalf("no EscalationRules-shaped row found; default-wire missing")
	}
	if newShape.Payload["from"] != "architectural" {
		t.Errorf("new-shape from = %v, want architectural", newShape.Payload["from"])
	}
	if newShape.Payload["to"] != "operator" {
		t.Errorf("new-shape to = %v, want operator", newShape.Payload["to"])
	}
	if newShape.Payload["class"] != "architectural" {
		t.Errorf("new-shape class = %v, want architectural", newShape.Payload["class"])
	}
}

func TestHighBlastRadiusEscalatesAtStrategicLayer(t *testing.T) {
	log := &fakeEventLog{}
	fc := clock.NewFake(time.Unix(1_700_000_000, 0))
	r := hra.NewEscalationRules(log, fc, "sess-1", "proj-1")

	before, _, _ := r.L3Counters()
	r.HighBlastRadius(context.Background(), "high", 0.72, []string{"pkg.A", "pkg.B"})
	after, _, _ := r.L3Counters()

	if after != before+1 {
		t.Errorf("L3 escalations = %d; want %d (high blast-radius bumps the strategic counter)", after, before+1)
	}

	var evts []eventlog.Event
	for _, ev := range log.Appends() {
		if ev.Type == eventlog.EvtEscalationDecision {
			evts = append(evts, ev)
		}
	}
	if len(evts) != 1 {
		t.Fatalf("emitted %d escalation events; want 1", len(evts))
	}
	if from := evts[0].Payload["from"]; from != "strategic" {
		t.Errorf("escalation from = %v; want strategic (L3)", from)
	}
	if nf := evts[0].Payload["needs_fix"]; nf != true {
		t.Errorf("needs_fix = %v; want true", nf)
	}
}

func TestHighBlastRadiusNonHighIsNoop(t *testing.T) {
	log := &fakeEventLog{}
	fc := clock.NewFake(time.Unix(1_700_000_000, 0))
	r := hra.NewEscalationRules(log, fc, "sess-1", "proj-1")

	before, _, _ := r.L3Counters()
	r.HighBlastRadius(context.Background(), "medium", 0.5, []string{"pkg.A"})
	r.HighBlastRadius(context.Background(), "low", 0.1, nil)
	after, _, _ := r.L3Counters()

	if after != before {
		t.Errorf("L3 escalations bumped on non-high verdict: %d → %d; want unchanged", before, after)
	}
	var count int
	for _, ev := range log.Appends() {
		if ev.Type == eventlog.EvtEscalationDecision {
			count++
		}
	}
	if count != 0 {
		t.Errorf("emitted %d escalation events on non-high; want 0", count)
	}
}
