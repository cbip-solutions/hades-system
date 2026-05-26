package merge_test

import (
	"context"
	"encoding/json"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/merge"
)

type recordingClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *recordingClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *recordingClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestSeverityStringer(t *testing.T) {
	cases := []struct {
		s    merge.Severity
		want string
	}{
		{merge.SeverityInfo, "Info"},
		{merge.SeverityWarning, "Warning"},
		{merge.SeverityHigh, "High"},
		{merge.SeverityCritical, "Critical"},
	}
	for _, c := range cases {
		if got := c.s.String(); got != c.want {
			t.Errorf("Severity(%d).String() = %q want %q", int(c.s), got, c.want)
		}
	}
}

func TestSeverityUnknown(t *testing.T) {
	if got := merge.Severity(9999).String(); got != "Unknown" {
		t.Errorf("Severity(9999).String() = %q want Unknown", got)
	}
}

func TestAllSeveritiesCoverage(t *testing.T) {
	all := merge.AllSeverities()
	if len(all) != 4 {
		t.Fatalf("AllSeverities len = %d want 4", len(all))
	}
	seen := make(map[merge.Severity]bool)
	for _, s := range all {
		if seen[s] {
			t.Errorf("duplicate %v", s)
		}
		seen[s] = true
	}
}

func TestSeverityIsIntKind(t *testing.T) {
	s := merge.Severity(0)
	if k := reflect.TypeOf(s).Kind(); k != reflect.Int {
		t.Fatalf("Severity.Kind() = %v want Int (inv-zen-110 sibling)", k)
	}
}

func TestNewAnomalyDetectorRejectsMissingDeps(t *testing.T) {
	_, err := merge.NewAnomalyDetector(merge.AnomalyDeps{}, merge.AnomalyThresholds{})
	if err == nil {
		t.Error("NewAnomalyDetector accepted missing Emitter")
	}
}

func TestAnomalyDetectorOnEventIgnoresUnrelated(t *testing.T) {
	em := &recordingEmitter{}
	d, err := merge.NewAnomalyDetector(merge.AnomalyDeps{
		Emitter: em,
	}, merge.DefaultAnomalyThresholds())
	if err != nil {
		t.Fatalf("NewAnomalyDetector: %v", err)
	}

	if err := d.OnEvent(context.Background(), merge.Event{Type: merge.EvtBaselineStarted}); err != nil {
		t.Errorf("OnEvent unrelated returned err = %v want nil", err)
	}
	if got := len(em.Snapshot()); got != 0 {
		t.Errorf("Snapshot len = %d want 0 (unrelated event)", got)
	}
}

func TestAnomalyDetectedPayloadJSONRoundTrip(t *testing.T) {
	in := merge.AnomalyDetectedPayload{
		Type:            merge.AnomalyFlakeRateAboveThreshold,
		Severity:        merge.SeverityHigh,
		ThresholdBreach: "rate > 5%",
		Evidence:        map[string]any{"rate": 0.07, "window_sessions": 100},
		Detail:          "γ scoring penalty consistently activated",
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out merge.AnomalyDetectedPayload
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if in.Type != out.Type {
		t.Errorf("Type drift: %v vs %v", in.Type, out.Type)
	}
	if in.Severity != out.Severity {
		t.Errorf("Severity drift: %v vs %v", in.Severity, out.Severity)
	}
	if in.ThresholdBreach != out.ThresholdBreach {
		t.Errorf("ThresholdBreach drift")
	}
	if in.Detail != out.Detail {
		t.Errorf("Detail drift")
	}

	if len(in.Evidence) != len(out.Evidence) {
		t.Errorf("Evidence key-count drift: %d vs %d", len(in.Evidence), len(out.Evidence))
	}
	for k := range in.Evidence {
		if _, ok := out.Evidence[k]; !ok {
			t.Errorf("Evidence missing key %q after round-trip", k)
		}
	}
}

func TestNoPerAnomalyEventTypeConstants(t *testing.T) {
	all := merge.AllEventTypes()
	for _, et := range all {
		s := et.String()

		if s == "MergeAnomalyDetected" {
			continue
		}

		if contains(s, "Anomaly") {
			t.Errorf("Drift-D VIOLATION: per-anomaly EventType found: %s", s)
		}
	}
}

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (f *fakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *fakeClock) Set(t time.Time) {
	f.mu.Lock()
	f.now = t
	f.mu.Unlock()
}

func (f *fakeClock) Advance(d time.Duration) {
	f.mu.Lock()
	f.now = f.now.Add(d)
	f.mu.Unlock()
}

func newDetector(t *testing.T, em merge.EventEmitter, thresholds merge.AnomalyThresholds, clk merge.AnomalyClock) *merge.AnomalyDetector {
	t.Helper()
	d, err := merge.NewAnomalyDetector(merge.AnomalyDeps{
		Emitter: em, Clock: clk,
	}, thresholds)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func makeCandidateCompleteEvt(t *testing.T, flakeCount int) merge.Event {
	t.Helper()
	p, err := json.Marshal(merge.CandidateCompletePayload{
		CandidateID:    "h1",
		FlakeCount:     flakeCount,
		TestPassCount:  10,
		TestFailCount:  0,
		PatchSizeLines: 30,
	})
	if err != nil {
		t.Fatalf("marshal CandidateCompletePayload: %v", err)
	}
	return merge.Event{Type: merge.EvtCandidateComplete, Payload: p}
}

func makeMergeStartedEvt(t *testing.T, mode string) merge.Event {
	t.Helper()
	p, err := json.Marshal(merge.MergeStartedWithModePayload{
		RequestHash: "abc",
		Mode:        mode,
	})
	if err != nil {
		t.Fatalf("marshal MergeStartedWithModePayload: %v", err)
	}
	return merge.Event{Type: merge.EvtMergeStartedWithMode, Payload: p}
}

func TestEvalFlakeRateBelowThresholdNoEmit(t *testing.T) {
	em := &recordingEmitter{}
	clk := &fakeClock{now: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)}
	thresholds := merge.AnomalyThresholds{
		FlakeRateThresholdPct:   5.0,
		FlakeRateWindowSessions: 100,
	}
	d := newDetector(t, em, thresholds, clk)

	for i := 0; i < 100; i++ {
		flake := 0
		if i < 3 {
			flake = 1
		}
		if err := d.OnEvent(context.Background(), makeCandidateCompleteEvt(t, flake)); err != nil {
			t.Fatalf("OnEvent: %v", err)
		}
	}
	for _, e := range em.Snapshot() {
		if e.Type == merge.EvtMergeAnomalyDetected {
			t.Errorf("flake rate 3%% emitted anomaly (threshold=5%%)")
		}
	}
}

func TestEvalFlakeRateAboveThresholdEmits(t *testing.T) {
	em := &recordingEmitter{}
	clk := &fakeClock{now: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)}
	thresholds := merge.AnomalyThresholds{
		FlakeRateThresholdPct:   5.0,
		FlakeRateWindowSessions: 100,
	}
	d := newDetector(t, em, thresholds, clk)

	for i := 0; i < 100; i++ {
		flake := 0
		if i < 10 {
			flake = 1
		}
		if err := d.OnEvent(context.Background(), makeCandidateCompleteEvt(t, flake)); err != nil {
			t.Fatalf("OnEvent: %v", err)
		}
	}
	saw := false
	for _, e := range em.Snapshot() {
		if e.Type != merge.EvtMergeAnomalyDetected {
			continue
		}
		var p merge.AnomalyDetectedPayload
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			t.Fatalf("unmarshal anomaly payload: %v", err)
		}
		if p.Type != merge.AnomalyFlakeRateAboveThreshold {
			continue
		}
		saw = true
		if p.ThresholdBreach == "" {
			t.Error("ThresholdBreach empty")
		}
		if p.Evidence == nil {
			t.Error("Evidence nil")
		}
		if p.Severity != merge.SeverityHigh {
			t.Errorf("Severity = %v want SeverityHigh", p.Severity)
		}

		for _, k := range []string{"rate", "window_sessions", "flaked_count"} {
			if _, ok := p.Evidence[k]; !ok {
				t.Errorf("Evidence missing key %q", k)
			}
		}
		if p.Detail == "" {
			t.Error("Detail empty")
		}
	}
	if !saw {
		t.Error("flake rate 10% did not emit AnomalyFlakeRateAboveThreshold")
	}
}

func TestEvalFlakeRateWindowSlidesPastThreshold(t *testing.T) {
	em := &recordingEmitter{}
	clk := &fakeClock{now: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)}
	thresholds := merge.AnomalyThresholds{
		FlakeRateThresholdPct:   5.0,
		FlakeRateWindowSessions: 10,
	}
	d := newDetector(t, em, thresholds, clk)

	for i := 0; i < 10; i++ {
		if err := d.OnEvent(context.Background(), makeCandidateCompleteEvt(t, 1)); err != nil {
			t.Fatalf("OnEvent saturation: %v", err)
		}
	}

	for i := 0; i < 10; i++ {
		if err := d.OnEvent(context.Background(), makeCandidateCompleteEvt(t, 0)); err != nil {
			t.Fatalf("OnEvent clean: %v", err)
		}
	}
	emCountBefore := 0
	for _, e := range em.Snapshot() {
		if e.Type == merge.EvtMergeAnomalyDetected {
			emCountBefore++
		}
	}
	// 5 more clean events: window stays at 0%, MUST NOT emit again.
	for i := 0; i < 5; i++ {
		if err := d.OnEvent(context.Background(), makeCandidateCompleteEvt(t, 0)); err != nil {
			t.Fatalf("OnEvent post-slide: %v", err)
		}
	}
	emCountAfter := 0
	for _, e := range em.Snapshot() {
		if e.Type == merge.EvtMergeAnomalyDetected {
			emCountAfter++
		}
	}
	if emCountAfter != emCountBefore {
		t.Errorf("post-saturation clean events emitted new anomalies (before=%d after=%d)", emCountBefore, emCountAfter)
	}
}

func TestEvalModeDegradationBelowThresholdNoEmit(t *testing.T) {
	em := &recordingEmitter{}
	clk := &fakeClock{now: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)}
	thresholds := merge.AnomalyThresholds{
		ModeDegradationPctThreshold: 40.0,
		ModeDegradationWindowHours:  24 * time.Hour,
	}
	d := newDetector(t, em, thresholds, clk)
	for i := 0; i < 10; i++ {
		if err := d.OnEvent(context.Background(), makeMergeStartedEvt(t, "Normal")); err != nil {
			t.Fatalf("OnEvent Normal: %v", err)
		}
	}
	for i := 0; i < 3; i++ {
		if err := d.OnEvent(context.Background(), makeMergeStartedEvt(t, "Degraded60")); err != nil {
			t.Fatalf("OnEvent Degraded60: %v", err)
		}
	}

	for _, e := range em.Snapshot() {
		if e.Type == merge.EvtMergeAnomalyDetected {
			t.Errorf("23%% degradation emitted anomaly (threshold=40%%)")
		}
	}
}

func TestEvalModeDegradationAboveThresholdEmits(t *testing.T) {
	em := &recordingEmitter{}
	clk := &fakeClock{now: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)}
	thresholds := merge.AnomalyThresholds{
		ModeDegradationPctThreshold: 40.0,
		ModeDegradationWindowHours:  24 * time.Hour,
	}
	d := newDetector(t, em, thresholds, clk)

	for i := 0; i < 5; i++ {
		if err := d.OnEvent(context.Background(), makeMergeStartedEvt(t, "Normal")); err != nil {
			t.Fatalf("OnEvent Normal: %v", err)
		}
	}
	for i := 0; i < 5; i++ {
		if err := d.OnEvent(context.Background(), makeMergeStartedEvt(t, "Degraded60")); err != nil {
			t.Fatalf("OnEvent Degraded60: %v", err)
		}
	}
	saw := false
	for _, e := range em.Snapshot() {
		if e.Type != merge.EvtMergeAnomalyDetected {
			continue
		}
		var p merge.AnomalyDetectedPayload
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			t.Fatalf("unmarshal anomaly payload: %v", err)
		}
		if p.Type != merge.AnomalyModeDegradationPersistent {
			continue
		}
		saw = true
		if p.Severity != merge.SeverityWarning {
			t.Errorf("Severity = %v want SeverityWarning", p.Severity)
		}
		if p.ThresholdBreach == "" {
			t.Error("ThresholdBreach empty")
		}
		if p.Evidence == nil {
			t.Error("Evidence nil")
		}
		for _, k := range []string{"degraded_pct", "window_sessions", "degraded_count"} {
			if _, ok := p.Evidence[k]; !ok {
				t.Errorf("Evidence missing key %q", k)
			}
		}
		if p.Detail == "" {
			t.Error("Detail empty")
		}
	}
	if !saw {
		t.Error("50% degradation did not emit AnomalyModeDegradationPersistent")
	}
}

func TestEvalModeDegradationWindowSlidesPastByTime(t *testing.T) {
	em := &recordingEmitter{}
	clk := &fakeClock{now: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)}
	thresholds := merge.AnomalyThresholds{
		ModeDegradationPctThreshold: 40.0,
		ModeDegradationWindowHours:  1 * time.Hour,
	}
	d := newDetector(t, em, thresholds, clk)

	for i := 0; i < 5; i++ {
		if err := d.OnEvent(context.Background(), makeMergeStartedEvt(t, "Degraded60")); err != nil {
			t.Fatalf("OnEvent saturation: %v", err)
		}
	}

	clk.Advance(2 * time.Hour)
	// One Normal event in fresh window — buffer holds 1 entry, 0% degraded;
	// also below the saturation guard (total < 5) → MUST NOT emit again.
	if err := d.OnEvent(context.Background(), makeMergeStartedEvt(t, "Normal")); err != nil {
		t.Fatalf("OnEvent post-slide Normal: %v", err)
	}
	emCount := 0
	for _, e := range em.Snapshot() {
		if e.Type == merge.EvtMergeAnomalyDetected {
			emCount++
		}
	}
	if emCount > 1 {
		t.Errorf("post-slide emit count = %d want ≤1", emCount)
	}
}

// Defense-in-depth: malformed payloads MUST be silently ignored (return nil,
// no emission, no panic). The detector is a fire-and-forget consumer at the
// goroutine boundary; surfacing a JSON error there has no useful subscriber.
// These tests exercise the early-return branch on json.Unmarshal failure.

func TestEvalFlakeRateMalformedPayloadIgnored(t *testing.T) {
	em := &recordingEmitter{}
	clk := &fakeClock{now: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)}
	d := newDetector(t, em, merge.DefaultAnomalyThresholds(), clk)

	evt := merge.Event{
		Type:    merge.EvtCandidateComplete,
		Payload: []byte("not-json{"),
	}
	if err := d.OnEvent(context.Background(), evt); err != nil {
		t.Errorf("OnEvent malformed = %v want nil", err)
	}
	if got := len(em.Snapshot()); got != 0 {
		t.Errorf("Snapshot len = %d want 0 (malformed payload should not emit)", got)
	}
}

func TestEvalModeDegradationMalformedPayloadIgnored(t *testing.T) {
	em := &recordingEmitter{}
	clk := &fakeClock{now: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)}
	d := newDetector(t, em, merge.DefaultAnomalyThresholds(), clk)
	evt := merge.Event{
		Type:    merge.EvtMergeStartedWithMode,
		Payload: []byte("{not-json"),
	}
	if err := d.OnEvent(context.Background(), evt); err != nil {
		t.Errorf("OnEvent malformed = %v want nil", err)
	}
	if got := len(em.Snapshot()); got != 0 {
		t.Errorf("Snapshot len = %d want 0 (malformed payload should not emit)", got)
	}
}

// TestEmitWithGenCtrPropagatesGenerationID closes the C-4 follow-up coverage
// gap on the emit() helper's GenCtr-non-nil branch (anomaly.go ~lines 418-420):
// when AnomalyDeps.GenCtr is injected non-nil, both the wrapping Event AND the
// AnomalyDetectedPayload must carry the current generation ID — never zero —
// and the two MUST be equal (defense-in-depth: subscribers reconstructing
// causal chains from either source see the same value).
//
// Per project doctrine no-defer / no-tech-debt: this branch is reachable
// (AnomalyDeps.GenCtr is a public field), testable (~10 LoC inject), exists
// today (shipped at C-3), and is not forward-looking — so it gets closed
// inline rather than deferred to a Phase D engine-level test.
func TestEmitWithGenCtrPropagatesGenerationID(t *testing.T) {
	em := &recordingEmitter{}
	clk := &fakeClock{now: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)}
	gc := &merge.GenerationCounter{}

	gc.Next()
	gc.Next()

	thresholds := merge.AnomalyThresholds{
		FlakeRateThresholdPct:   5.0,
		FlakeRateWindowSessions: 10,
	}
	d, err := merge.NewAnomalyDetector(merge.AnomalyDeps{
		Emitter: em,
		Clock:   clk,
		GenCtr:  gc,
	}, thresholds)
	if err != nil {
		t.Fatalf("NewAnomalyDetector: %v", err)
	}

	for i := 0; i < 10; i++ {
		if err := d.OnEvent(context.Background(), makeCandidateCompleteEvt(t, 1)); err != nil {
			t.Fatalf("OnEvent[%d]: %v", i, err)
		}
	}
	var saw bool
	for _, e := range em.Snapshot() {
		if e.Type != merge.EvtMergeAnomalyDetected {
			continue
		}
		if e.GenerationID == 0 {
			t.Errorf("Event.GenerationID = 0 want non-zero (GenCtr was injected)")
		}
		var p merge.AnomalyDetectedPayload
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if p.GenerationID == 0 {
			t.Errorf("payload.GenerationID = 0 want non-zero (GenCtr was injected)")
		}
		if p.GenerationID != e.GenerationID {
			t.Errorf("payload.GenerationID (%d) != Event.GenerationID (%d) — divergent gen propagation", p.GenerationID, e.GenerationID)
		}

		if p.GenerationID != 2 {
			t.Errorf("payload.GenerationID = %d want 2 (counter bumped twice pre-construction)", p.GenerationID)
		}
		saw = true
	}
	if !saw {
		t.Fatal("no anomaly emitted; expected one on flake-rate saturation with 100% flake")
	}
}

func makeScoringCompleteVetoEvt(t *testing.T, vetoed bool) merge.Event {
	t.Helper()
	p, err := json.Marshal(merge.ScoringCompletePayload{
		WinnerID:        "h1",
		TiebreakApplied: false,
		OperatorVetoed:  vetoed,
	})
	if err != nil {
		t.Fatalf("marshal ScoringCompletePayload: %v", err)
	}
	return merge.Event{Type: merge.EvtScoringComplete, Payload: p}
}

func makeBaselineCompleteEvt(t *testing.T, baseSHA, passingSetHash string) merge.Event {
	t.Helper()
	p, err := json.Marshal(merge.BaselineCompletePayload{
		BaseSHA:        baseSHA,
		PassingSetHash: passingSetHash,
		TestCount:      10,
	})
	if err != nil {
		t.Fatalf("marshal BaselineCompletePayload: %v", err)
	}
	return merge.Event{Type: merge.EvtBaselineComplete, Payload: p}
}

func makePatchRejectedEvt(t *testing.T) merge.Event {
	t.Helper()
	p, err := json.Marshal(merge.CandidateFailedPayload{
		CandidateID: "h1",
		FailureType: merge.CandidateFailurePatchRejected.String(),
	})
	if err != nil {
		t.Fatalf("marshal CandidateFailedPayload: %v", err)
	}
	return merge.Event{Type: merge.EvtCandidateFailed, Payload: p}
}

func makeOtherCandidateFailedEvt(t *testing.T) merge.Event {
	t.Helper()
	p, err := json.Marshal(merge.CandidateFailedPayload{
		CandidateID: "h2",
		FailureType: merge.CandidateFailureTimeout.String(),
	})
	if err != nil {
		t.Fatalf("marshal CandidateFailedPayload: %v", err)
	}
	return merge.Event{Type: merge.EvtCandidateFailed, Payload: p}
}

func TestEvalScoringWinnerVetoedBelowCountNoEmit(t *testing.T) {
	em := &recordingEmitter{}
	clk := &fakeClock{now: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)}
	thresholds := merge.AnomalyThresholds{
		ScoringWinnerVetoedCount:       3,
		ScoringWinnerVetoedWindowHours: 24 * time.Hour,
	}
	d := newDetector(t, em, thresholds, clk)
	for i := 0; i < 2; i++ {
		if err := d.OnEvent(context.Background(), makeScoringCompleteVetoEvt(t, true)); err != nil {
			t.Fatalf("OnEvent: %v", err)
		}
	}
	for _, e := range em.Snapshot() {
		if e.Type == merge.EvtMergeAnomalyDetected {
			t.Errorf("2 vetoes emitted anomaly (threshold count=3)")
		}
	}
}

func TestEvalScoringWinnerVetoedAboveCountEmits(t *testing.T) {
	em := &recordingEmitter{}
	clk := &fakeClock{now: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)}
	thresholds := merge.AnomalyThresholds{
		ScoringWinnerVetoedCount:       3,
		ScoringWinnerVetoedWindowHours: 24 * time.Hour,
	}
	d := newDetector(t, em, thresholds, clk)

	for i := 0; i < 4; i++ {
		if err := d.OnEvent(context.Background(), makeScoringCompleteVetoEvt(t, true)); err != nil {
			t.Fatalf("OnEvent: %v", err)
		}
	}
	saw := false
	for _, e := range em.Snapshot() {
		if e.Type != merge.EvtMergeAnomalyDetected {
			continue
		}
		var p merge.AnomalyDetectedPayload
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			t.Fatalf("unmarshal anomaly payload: %v", err)
		}
		if p.Type != merge.AnomalyScoringFormulaWinnerVetoed {
			continue
		}
		saw = true
		if p.Severity != merge.SeverityHigh {
			t.Errorf("Severity = %v want SeverityHigh", p.Severity)
		}
		if p.ThresholdBreach == "" {
			t.Error("ThresholdBreach empty")
		}
		if p.Evidence == nil {
			t.Error("Evidence nil")
		}
		for _, k := range []string{"vetoes_in_window", "threshold", "window_hours"} {
			if _, ok := p.Evidence[k]; !ok {
				t.Errorf("Evidence missing key %q", k)
			}
		}
		if p.Detail == "" {
			t.Error("Detail empty")
		}
	}
	if !saw {
		t.Error("4 vetoes did not emit AnomalyScoringFormulaWinnerVetoed")
	}
}

func TestEvalScoringWinnerVetoedNonVetoIgnored(t *testing.T) {
	em := &recordingEmitter{}
	clk := &fakeClock{now: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)}
	thresholds := merge.AnomalyThresholds{
		ScoringWinnerVetoedCount:       1,
		ScoringWinnerVetoedWindowHours: 24 * time.Hour,
	}
	d := newDetector(t, em, thresholds, clk)

	for i := 0; i < 5; i++ {
		if err := d.OnEvent(context.Background(), makeScoringCompleteVetoEvt(t, false)); err != nil {
			t.Fatalf("OnEvent: %v", err)
		}
	}
	for _, e := range em.Snapshot() {
		if e.Type == merge.EvtMergeAnomalyDetected {
			t.Errorf("non-vetoed ScoringComplete events emitted anomaly")
		}
	}
}

func TestEvalScoringWinnerVetoedWindowEvictsByTime(t *testing.T) {
	em := &recordingEmitter{}
	clk := &fakeClock{now: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)}
	thresholds := merge.AnomalyThresholds{
		ScoringWinnerVetoedCount:       3,
		ScoringWinnerVetoedWindowHours: 1 * time.Hour,
	}
	d := newDetector(t, em, thresholds, clk)

	for i := 0; i < 2; i++ {
		if err := d.OnEvent(context.Background(), makeScoringCompleteVetoEvt(t, true)); err != nil {
			t.Fatalf("OnEvent t0: %v", err)
		}
	}

	clk.Advance(2 * time.Hour)

	for i := 0; i < 2; i++ {
		if err := d.OnEvent(context.Background(), makeScoringCompleteVetoEvt(t, true)); err != nil {
			t.Fatalf("OnEvent post-evict: %v", err)
		}
	}
	for _, e := range em.Snapshot() {
		if e.Type == merge.EvtMergeAnomalyDetected {
			t.Errorf("post-eviction window holds 2 ≤ threshold=3 but anomaly emitted")
		}
	}
}

func TestEvalScoringWinnerVetoedMalformedPayloadIgnored(t *testing.T) {
	em := &recordingEmitter{}
	clk := &fakeClock{now: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)}
	d := newDetector(t, em, merge.DefaultAnomalyThresholds(), clk)
	evt := merge.Event{
		Type:    merge.EvtScoringComplete,
		Payload: []byte("not-json{"),
	}
	if err := d.OnEvent(context.Background(), evt); err != nil {
		t.Errorf("OnEvent malformed = %v want nil", err)
	}
	if got := len(em.Snapshot()); got != 0 {
		t.Errorf("Snapshot len = %d want 0 (malformed payload should not emit)", got)
	}
}

func TestEvalBaselineUnstableSameHashNoEmit(t *testing.T) {
	em := &recordingEmitter{}
	clk := &fakeClock{now: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)}
	thresholds := merge.AnomalyThresholds{
		BaselineUnstableMinDivergentTests: 1,
	}
	d := newDetector(t, em, thresholds, clk)

	for i := 0; i < 2; i++ {
		if err := d.OnEvent(context.Background(), makeBaselineCompleteEvt(t, "base-1", "hash-A")); err != nil {
			t.Fatalf("OnEvent: %v", err)
		}
	}
	for _, e := range em.Snapshot() {
		if e.Type == merge.EvtMergeAnomalyDetected {
			t.Errorf("same hash twice emitted anomaly")
		}
	}
}

func TestEvalBaselineUnstableDivergentEmits(t *testing.T) {
	em := &recordingEmitter{}
	clk := &fakeClock{now: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)}
	thresholds := merge.AnomalyThresholds{
		BaselineUnstableMinDivergentTests: 1,
	}
	d := newDetector(t, em, thresholds, clk)

	if err := d.OnEvent(context.Background(), makeBaselineCompleteEvt(t, "base-1", "hash-A")); err != nil {
		t.Fatalf("OnEvent A: %v", err)
	}
	if err := d.OnEvent(context.Background(), makeBaselineCompleteEvt(t, "base-1", "hash-B")); err != nil {
		t.Fatalf("OnEvent B: %v", err)
	}
	saw := false
	for _, e := range em.Snapshot() {
		if e.Type != merge.EvtMergeAnomalyDetected {
			continue
		}
		var p merge.AnomalyDetectedPayload
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			t.Fatalf("unmarshal anomaly payload: %v", err)
		}
		if p.Type != merge.AnomalyBaselineUnstableAcrossSessions {
			continue
		}
		saw = true
		if p.Severity != merge.SeverityHigh {
			t.Errorf("Severity = %v want SeverityHigh", p.Severity)
		}
		if p.ThresholdBreach == "" {
			t.Error("ThresholdBreach empty")
		}
		if p.Evidence == nil {
			t.Error("Evidence nil")
		}
		for _, k := range []string{"base_sha", "distinct_hashes"} {
			if _, ok := p.Evidence[k]; !ok {
				t.Errorf("Evidence missing key %q", k)
			}
		}
		if p.Detail == "" {
			t.Error("Detail empty")
		}
	}
	if !saw {
		t.Error("2 distinct hashes for same base did not emit AnomalyBaselineUnstableAcrossSessions")
	}
}

func TestEvalBaselineUnstableDifferentBasesIsolated(t *testing.T) {
	em := &recordingEmitter{}
	clk := &fakeClock{now: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)}
	thresholds := merge.AnomalyThresholds{
		BaselineUnstableMinDivergentTests: 1,
	}
	d := newDetector(t, em, thresholds, clk)

	if err := d.OnEvent(context.Background(), makeBaselineCompleteEvt(t, "base-1", "hash-A")); err != nil {
		t.Fatalf("OnEvent base-1: %v", err)
	}
	if err := d.OnEvent(context.Background(), makeBaselineCompleteEvt(t, "base-2", "hash-B")); err != nil {
		t.Fatalf("OnEvent base-2: %v", err)
	}
	for _, e := range em.Snapshot() {
		if e.Type == merge.EvtMergeAnomalyDetected {
			t.Errorf("isolated bases emitted anomaly")
		}
	}
}

func TestEvalBaselineUnstableBelowMinDivergentNoEmit(t *testing.T) {
	em := &recordingEmitter{}
	clk := &fakeClock{now: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)}

	thresholds := merge.AnomalyThresholds{
		BaselineUnstableMinDivergentTests: 2,
	}
	d := newDetector(t, em, thresholds, clk)

	if err := d.OnEvent(context.Background(), makeBaselineCompleteEvt(t, "base-1", "hash-A")); err != nil {
		t.Fatalf("OnEvent A: %v", err)
	}
	if err := d.OnEvent(context.Background(), makeBaselineCompleteEvt(t, "base-1", "hash-B")); err != nil {
		t.Fatalf("OnEvent B: %v", err)
	}
	for _, e := range em.Snapshot() {
		if e.Type == merge.EvtMergeAnomalyDetected {
			t.Errorf("divergent=2 below MinDivergent+1=3 emitted anomaly")
		}
	}
}

func TestEvalBaselineUnstableMalformedPayloadIgnored(t *testing.T) {
	em := &recordingEmitter{}
	clk := &fakeClock{now: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)}
	d := newDetector(t, em, merge.DefaultAnomalyThresholds(), clk)
	evt := merge.Event{
		Type:    merge.EvtBaselineComplete,
		Payload: []byte("{not-json"),
	}
	if err := d.OnEvent(context.Background(), evt); err != nil {
		t.Errorf("OnEvent malformed = %v want nil", err)
	}
	if got := len(em.Snapshot()); got != 0 {
		t.Errorf("Snapshot len = %d want 0 (malformed payload should not emit)", got)
	}
}

func TestEvalTextualUnresolvableBelowThresholdNoEmit(t *testing.T) {
	em := &recordingEmitter{}
	clk := &fakeClock{now: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)}
	thresholds := merge.AnomalyThresholds{
		TextualUnresolvableRatePct:        10.0,
		TextualUnresolvableWindowSessions: 100,
	}
	d := newDetector(t, em, thresholds, clk)

	for i := 0; i < 5; i++ {
		if err := d.OnEvent(context.Background(), makePatchRejectedEvt(t)); err != nil {
			t.Fatalf("OnEvent rejected: %v", err)
		}
	}
	for i := 0; i < 95; i++ {
		if err := d.OnEvent(context.Background(), makeOtherCandidateFailedEvt(t)); err != nil {
			t.Fatalf("OnEvent other: %v", err)
		}
	}
	for _, e := range em.Snapshot() {
		if e.Type == merge.EvtMergeAnomalyDetected {
			t.Errorf("5%% unresolvable rate emitted anomaly (threshold=10%%)")
		}
	}
}

func TestEvalTextualUnresolvableAboveThresholdEmits(t *testing.T) {
	em := &recordingEmitter{}
	clk := &fakeClock{now: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)}
	thresholds := merge.AnomalyThresholds{
		TextualUnresolvableRatePct:        10.0,
		TextualUnresolvableWindowSessions: 100,
	}
	d := newDetector(t, em, thresholds, clk)

	for i := 0; i < 15; i++ {
		if err := d.OnEvent(context.Background(), makePatchRejectedEvt(t)); err != nil {
			t.Fatalf("OnEvent rejected: %v", err)
		}
	}
	for i := 0; i < 85; i++ {
		if err := d.OnEvent(context.Background(), makeOtherCandidateFailedEvt(t)); err != nil {
			t.Fatalf("OnEvent other: %v", err)
		}
	}
	saw := false
	for _, e := range em.Snapshot() {
		if e.Type != merge.EvtMergeAnomalyDetected {
			continue
		}
		var p merge.AnomalyDetectedPayload
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			t.Fatalf("unmarshal anomaly payload: %v", err)
		}
		if p.Type != merge.AnomalyTextualMergeUnresolvableRateHigh {
			continue
		}
		saw = true
		if p.Severity != merge.SeverityHigh {
			t.Errorf("Severity = %v want SeverityHigh", p.Severity)
		}
		if p.ThresholdBreach == "" {
			t.Error("ThresholdBreach empty")
		}
		if p.Evidence == nil {
			t.Error("Evidence nil")
		}
		for _, k := range []string{"rate", "window_sessions", "unresolvable_count"} {
			if _, ok := p.Evidence[k]; !ok {
				t.Errorf("Evidence missing key %q", k)
			}
		}
		if p.Detail == "" {
			t.Error("Detail empty")
		}
	}
	if !saw {
		t.Error("15% unresolvable rate did not emit AnomalyTextualMergeUnresolvableRateHigh")
	}
}

func TestEvalTextualUnresolvableSaturationGuard(t *testing.T) {
	em := &recordingEmitter{}
	clk := &fakeClock{now: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)}
	thresholds := merge.AnomalyThresholds{
		TextualUnresolvableRatePct:        10.0,
		TextualUnresolvableWindowSessions: 100,
	}
	d := newDetector(t, em, thresholds, clk)

	for i := 0; i < 5; i++ {
		if err := d.OnEvent(context.Background(), makePatchRejectedEvt(t)); err != nil {
			t.Fatalf("OnEvent rejected: %v", err)
		}
	}
	for _, e := range em.Snapshot() {
		if e.Type == merge.EvtMergeAnomalyDetected {
			t.Errorf("pre-saturation emitted anomaly (window=5 < required=100)")
		}
	}
}

func TestEvalTextualUnresolvableWindowSlidesPastThreshold(t *testing.T) {
	em := &recordingEmitter{}
	clk := &fakeClock{now: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)}
	thresholds := merge.AnomalyThresholds{
		TextualUnresolvableRatePct:        10.0,
		TextualUnresolvableWindowSessions: 10,
	}
	d := newDetector(t, em, thresholds, clk)

	for i := 0; i < 10; i++ {
		if err := d.OnEvent(context.Background(), makePatchRejectedEvt(t)); err != nil {
			t.Fatalf("OnEvent saturation: %v", err)
		}
	}

	for i := 0; i < 10; i++ {
		if err := d.OnEvent(context.Background(), makeOtherCandidateFailedEvt(t)); err != nil {
			t.Fatalf("OnEvent slide: %v", err)
		}
	}
	emCountBefore := 0
	for _, e := range em.Snapshot() {
		if e.Type == merge.EvtMergeAnomalyDetected {
			emCountBefore++
		}
	}

	for i := 0; i < 5; i++ {
		if err := d.OnEvent(context.Background(), makeOtherCandidateFailedEvt(t)); err != nil {
			t.Fatalf("OnEvent post-slide: %v", err)
		}
	}
	emCountAfter := 0
	for _, e := range em.Snapshot() {
		if e.Type == merge.EvtMergeAnomalyDetected {
			emCountAfter++
		}
	}
	if emCountAfter != emCountBefore {
		t.Errorf("post-slide clean events emitted new anomalies (before=%d after=%d)", emCountBefore, emCountAfter)
	}
}

func TestEvalTextualUnresolvableMalformedPayloadIgnored(t *testing.T) {
	em := &recordingEmitter{}
	clk := &fakeClock{now: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)}
	d := newDetector(t, em, merge.DefaultAnomalyThresholds(), clk)
	evt := merge.Event{
		Type:    merge.EvtCandidateFailed,
		Payload: []byte("{not-json"),
	}
	if err := d.OnEvent(context.Background(), evt); err != nil {
		t.Errorf("OnEvent malformed = %v want nil", err)
	}
	if got := len(em.Snapshot()); got != 0 {
		t.Errorf("Snapshot len = %d want 0 (malformed payload should not emit)", got)
	}
}

func TestOnEventReturnsCtxErrOnCancelledCtx(t *testing.T) {
	em := &recordingEmitter{}
	clk := &fakeClock{now: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)}
	d := newDetector(t, em, merge.DefaultAnomalyThresholds(), clk)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := d.OnEvent(ctx, makeCandidateCompleteEvt(t, 1))
	if err == nil {
		t.Fatal("OnEvent on cancelled ctx returned nil err want ctx.Err()")
	}
	if err != context.Canceled {
		t.Errorf("OnEvent err = %v want context.Canceled", err)
	}
	if got := len(em.Snapshot()); got != 0 {
		t.Errorf("Snapshot len = %d want 0 (cancelled ctx should short-circuit before any emit)", got)
	}
}

func TestPlan6ADRRangeAccessor(t *testing.T) {
	start, end := merge.Plan6ADRRange()
	if start != merge.Plan6ADRRangeStart {
		t.Errorf("Plan6ADRRange start = %d want Plan6ADRRangeStart=%d", start, merge.Plan6ADRRangeStart)
	}
	if end != merge.Plan6ADRRangeEnd {
		t.Errorf("Plan6ADRRange end = %d want Plan6ADRRangeEnd=%d", end, merge.Plan6ADRRangeEnd)
	}
	if start != 30 {
		t.Errorf("Plan6ADRRangeStart = %d want 30 (inv-zen-112)", start)
	}
	if end != 39 {
		t.Errorf("Plan6ADRRangeEnd = %d want 39 (inv-zen-112)", end)
	}
}
