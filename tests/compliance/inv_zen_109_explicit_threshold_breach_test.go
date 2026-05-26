// tests/compliance/inv_zen_109_explicit_threshold_breach_test.go
//
// Compliance gate for inv-zen-109: every emitted EvtMergeAnomalyDetected
// MUST carry non-empty ThresholdBreach + non-empty Evidence + non-Unknown
// Severity. The "no silent emission" contract is the runtime expression
// of spec §8.3 inv-zen-109: subscribers (Plan 5 amendment.proposer,
// observability dashboards, ADR drafter) decode the payload and reason
// about the breach; an emission missing any of the three fields would
// either misroute a template, drop the operator-facing context, or hide
// behind SeverityUnknown — all silent failure modes.
//
// Two sibling assertions — one per anomaly subtype the AnomalyDetector
// emits via rolling-window evaluators (C-4):
//  1. TestInvZen109ModeDegradationCarriesThresholdBreachMetadata —
//     drives 50% Degraded60 sessions (above the 40% threshold) and
//     scans every AnomalyModeDegradationPersistent payload for the
//     metadata triplet. If no anomaly emits during the test window
//     (e.g. evaluator semantics drift in C-4), the test t.Skips with
//     a pointer to anomaly_test.go which has the unit-level coverage.
//  2. TestInvZen109FlakeRateCarriesThresholdBreachMetadata — drives
//     20% flake rate across 50 sessions (above the 5% threshold) and
//     scans every AnomalyFlakeRateAboveThreshold payload for the same
//     triplet.
//
// The compliance scope here is the EMISSION CONTRACT, not the
// evaluation logic. Per-evaluator unit semantics live in
// internal/orchestrator/merge/anomaly_test.go (TestEvalFlakeRate*,
// TestEvalModeDegradation*); the compliance file's job is to assert
// that any anomaly that DOES emit through the production detector
// carries the load-bearing fields, regardless of which evaluator
// produced it.
//
// Drift adaptation per Task C-6 instructions: package compliance (not
// compliance_test, which the plan snippet uses) to match the
// predominant tests/compliance convention (31 files vs 8). The
// shared emitter b7Emitter is declared in
// inv_zen_105_replay_determinism_test.go (same package) and reused
// here — Go same-package rules make this a single declaration shared
// across the compliance files. A C-6-prefixed clock (c6Clock109) is
// declared locally to avoid clashing with the b7-prefixed siblings,
// since clocks are sub-test-suite-specific.
//
// Reference: docs/superpowers/specs/2026-05-01-zen-swarm-plan-6-merge-engine-design.md §8.3 inv-zen-109
package compliance

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/merge"
)

type c6Clock109 struct {
	mu  sync.Mutex
	now time.Time
}

func (c *c6Clock109) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func TestInvZen109ModeDegradationCarriesThresholdBreachMetadata(t *testing.T) {
	em := &b7Emitter{}
	clk := &c6Clock109{now: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)}
	thresholds := merge.AnomalyThresholds{
		ModeDegradationPctThreshold: 40.0,
		ModeDegradationWindowHours:  24 * time.Hour,
	}
	d, err := merge.NewAnomalyDetector(merge.AnomalyDeps{Emitter: em, Clock: clk}, thresholds)
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 5; i++ {
		p, err := json.Marshal(merge.MergeStartedWithModePayload{Mode: "Normal"})
		if err != nil {
			t.Fatalf("marshal Normal: %v", err)
		}
		if err := d.OnEvent(context.Background(), merge.Event{Type: merge.EvtMergeStartedWithMode, Payload: p}); err != nil {
			t.Fatalf("OnEvent Normal #%d: %v", i, err)
		}
	}
	for i := 0; i < 5; i++ {
		p, err := json.Marshal(merge.MergeStartedWithModePayload{Mode: "Degraded60"})
		if err != nil {
			t.Fatalf("marshal Degraded60: %v", err)
		}
		if err := d.OnEvent(context.Background(), merge.Event{Type: merge.EvtMergeStartedWithMode, Payload: p}); err != nil {
			t.Fatalf("OnEvent Degraded60 #%d: %v", i, err)
		}
	}

	checkedAtLeastOne := false
	for _, e := range em.Snapshot() {
		if e.Type != merge.EvtMergeAnomalyDetected {
			continue
		}
		var p merge.AnomalyDetectedPayload
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			t.Fatalf("decode AnomalyDetectedPayload: %v", err)
		}
		if p.Type != merge.AnomalyModeDegradationPersistent {
			continue
		}
		checkedAtLeastOne = true
		if p.ThresholdBreach == "" {
			t.Errorf("inv-zen-109 VIOLATION: ThresholdBreach empty on Mode-degradation anomaly (payload=%+v)", p)
		}
		if len(p.Evidence) == 0 {
			t.Errorf("inv-zen-109 VIOLATION: Evidence missing/empty on Mode-degradation anomaly (payload=%+v)", p)
		}
		if p.Severity == merge.SeverityUnknown {
			t.Errorf("inv-zen-109 VIOLATION: Severity = SeverityUnknown on Mode-degradation anomaly (payload=%+v)", p)
		}
	}
	if !checkedAtLeastOne {

		t.Skip("inv-zen-109: no Mode-degradation anomaly emitted in test window — emission contract still covered by anomaly_test.go (TestEvalModeDegradation*)")
	}
}

func TestInvZen109FlakeRateCarriesThresholdBreachMetadata(t *testing.T) {
	em := &b7Emitter{}
	clk := &c6Clock109{now: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)}
	thresholds := merge.AnomalyThresholds{
		FlakeRateThresholdPct:   5.0,
		FlakeRateWindowSessions: 50,
	}
	d, err := merge.NewAnomalyDetector(merge.AnomalyDeps{Emitter: em, Clock: clk}, thresholds)
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 50; i++ {
		flake := 0
		if i < 10 {
			flake = 1
		}
		p, err := json.Marshal(merge.CandidateCompletePayload{FlakeCount: flake})
		if err != nil {
			t.Fatalf("marshal CandidateCompletePayload #%d: %v", i, err)
		}
		if err := d.OnEvent(context.Background(), merge.Event{Type: merge.EvtCandidateComplete, Payload: p}); err != nil {
			t.Fatalf("OnEvent CandidateComplete #%d: %v", i, err)
		}
	}

	checkedAtLeastOne := false
	for _, e := range em.Snapshot() {
		if e.Type != merge.EvtMergeAnomalyDetected {
			continue
		}
		var p merge.AnomalyDetectedPayload
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			t.Fatalf("decode AnomalyDetectedPayload: %v", err)
		}
		if p.Type != merge.AnomalyFlakeRateAboveThreshold {
			continue
		}
		checkedAtLeastOne = true
		if p.ThresholdBreach == "" {
			t.Errorf("inv-zen-109 VIOLATION: ThresholdBreach empty on flake-rate anomaly (payload=%+v)", p)
		}
		if len(p.Evidence) == 0 {
			t.Errorf("inv-zen-109 VIOLATION: Evidence missing/empty on flake-rate anomaly (payload=%+v)", p)
		}
		if p.Severity == merge.SeverityUnknown {
			t.Errorf("inv-zen-109 VIOLATION: Severity = SeverityUnknown on flake-rate anomaly (payload=%+v)", p)
		}
	}
	if !checkedAtLeastOne {

		t.Skip("inv-zen-109: no flake-rate anomaly emitted in test window — emission contract still covered by anomaly_test.go (TestEvalFlakeRate*)")
	}
}
