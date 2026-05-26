package hra

import (
	"encoding/json"
	"sort"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

func mkRec(t *testing.T, ts time.Time, et eventlog.EventType, payload map[string]any) eventlog.Record {
	t.Helper()
	var raw []byte
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("mkRec: marshal payload: %v", err)
		}
		raw = b
	}
	return eventlog.Record{
		EventType: et,
		Timestamp: ts.UnixNano(),
		Payload:   raw,
	}
}

func TestWindowOf_FiltersInclusiveSinceExclusiveUntil(t *testing.T) {
	base := time.Unix(0, 0).Add(time.Hour)
	since := base
	until := base.Add(3 * time.Second)
	recs := []eventlog.Record{
		{Timestamp: base.Add(-1 * time.Second).UnixNano()},
		{Timestamp: base.UnixNano()},
		{Timestamp: base.Add(time.Second).UnixNano()},
		{Timestamp: base.Add(3 * time.Second).UnixNano()},
		{Timestamp: base.Add(5 * time.Second).UnixNano()},
	}
	got := WindowOf(recs, since, until)
	if len(got) != 2 {
		t.Fatalf("len(WindowOf) = %d, want 2 (got %+v)", len(got), got)
	}
	if got[0].Timestamp != base.UnixNano() {
		t.Errorf("got[0].Timestamp = %d, want %d (since-inclusive)", got[0].Timestamp, base.UnixNano())
	}
	if got[1].Timestamp != base.Add(time.Second).UnixNano() {
		t.Errorf("got[1].Timestamp = %d, want %d", got[1].Timestamp, base.Add(time.Second).UnixNano())
	}
}

func TestWindowOf_EmptyInput(t *testing.T) {
	got := WindowOf(nil, time.Unix(0, 0), time.Unix(0, 0).Add(time.Hour))
	if len(got) != 0 {
		t.Errorf("len(WindowOf(nil)) = %d, want 0", len(got))
	}
	got2 := WindowOf([]eventlog.Record{}, time.Unix(0, 0), time.Unix(0, 0).Add(time.Hour))
	if len(got2) != 0 {
		t.Errorf("len(WindowOf([])) = %d, want 0", len(got2))
	}
}

func TestWindowOf_RejectsInvertedRange(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("WindowOf(since>until) did not panic")
		}
	}()
	since := time.Unix(0, 0).Add(time.Hour)
	until := time.Unix(0, 0)
	_ = WindowOf(nil, since, until)
}

func TestWindowOf_EqualSinceUntilEmptyResult(t *testing.T) {
	t0 := time.Unix(0, 0).Add(time.Hour)
	recs := []eventlog.Record{
		{Timestamp: t0.UnixNano()},
		{Timestamp: t0.Add(-1).UnixNano()},
	}
	got := WindowOf(recs, t0, t0)
	if len(got) != 0 {
		t.Errorf("WindowOf(t,t) returned %d records, want 0 (half-open semantics)", len(got))
	}
}

func TestTacticalAggregator_PluralityAck(t *testing.T) {
	t0 := time.Unix(0, 0).Add(time.Hour)
	recs := []eventlog.Record{
		mkRec(t, t0, eventlog.EvtWorkerCheckpoint, map[string]any{"verdict": "ack"}),
		mkRec(t, t0.Add(time.Second), eventlog.EvtWorkerCheckpoint, map[string]any{"verdict": "ack"}),
		mkRec(t, t0.Add(2*time.Second), eventlog.EvtWorkerCheckpoint, map[string]any{"verdict": "needs_fix"}),
	}
	f := tacticalAggregator{}.Aggregate(recs, t0, t0.Add(time.Minute))
	if f.Layer != LayerTactical {
		t.Errorf("Layer = %v, want LayerTactical", f.Layer)
	}
	if f.EventCount != 3 {
		t.Errorf("EventCount = %d, want 3", f.EventCount)
	}
	if f.Verdict != "ack" {
		t.Errorf("Verdict = %q, want ack", f.Verdict)
	}
	if f.NeedsFix {
		t.Error("NeedsFix = true, want false")
	}
	if f.Disagreement {
		t.Error("Disagreement = true, want false (plurality 2/1 above threshold)")
	}
	if f.Split != [2]int{2, 1} {
		t.Errorf("Split = %v, want [2 1]", f.Split)
	}
}

func TestTacticalAggregator_DisagreementOnEvenSplit(t *testing.T) {
	t0 := time.Unix(0, 0).Add(time.Hour)
	recs := []eventlog.Record{
		mkRec(t, t0, eventlog.EvtWorkerCheckpoint, map[string]any{"verdict": "ack"}),
		mkRec(t, t0.Add(time.Second), eventlog.EvtWorkerCheckpoint, map[string]any{"verdict": "needs_fix"}),
	}
	f := tacticalAggregator{}.Aggregate(recs, t0, t0.Add(time.Minute))
	if f.Verdict != "needs_fix" {
		t.Errorf("Verdict = %q, want needs_fix (pessimistic tie-break)", f.Verdict)
	}
	if !f.NeedsFix {
		t.Error("NeedsFix = false, want true")
	}
	if !f.Disagreement {
		t.Error("Disagreement = false, want true (1==1 tie)")
	}
	if f.Split != [2]int{1, 1} {
		t.Errorf("Split = %v, want [1 1]", f.Split)
	}
}

func TestTacticalAggregator_CollectsFixProposals(t *testing.T) {
	t0 := time.Unix(0, 0).Add(time.Hour)
	recs := []eventlog.Record{
		mkRec(t, t0, eventlog.EvtWorkerCheckpoint, map[string]any{"verdict": "needs_fix", "proposal": "PX"}),
		mkRec(t, t0.Add(time.Second), eventlog.EvtWorkerCheckpoint, map[string]any{"verdict": "needs_fix", "proposal": "PY"}),
		mkRec(t, t0.Add(2*time.Second), eventlog.EvtWorkerCheckpoint, map[string]any{"verdict": "ack"}),
	}
	f := tacticalAggregator{}.Aggregate(recs, t0, t0.Add(time.Minute))
	if len(f.FixProposals) != 2 {
		t.Fatalf("len(FixProposals) = %d, want 2", len(f.FixProposals))
	}
	got := append([]string(nil), f.FixProposals...)
	sort.Strings(got)
	if got[0] != "PX" || got[1] != "PY" {
		t.Errorf("FixProposals (sorted) = %v, want [PX PY]", got)
	}
}

func TestTacticalAggregator_CollectsFixProposals_DeDups(t *testing.T) {
	t0 := time.Unix(0, 0).Add(time.Hour)
	recs := []eventlog.Record{
		mkRec(t, t0, eventlog.EvtWorkerCheckpoint, map[string]any{"verdict": "needs_fix", "proposal": "PX"}),
		mkRec(t, t0.Add(time.Second), eventlog.EvtWorkerCheckpoint, map[string]any{"verdict": "needs_fix", "proposal": "PX"}),
	}
	f := tacticalAggregator{}.Aggregate(recs, t0, t0.Add(time.Minute))
	if len(f.FixProposals) != 1 || f.FixProposals[0] != "PX" {
		t.Errorf("FixProposals = %v, want [PX]", f.FixProposals)
	}
}

func TestTacticalAggregator_IgnoresUnknownVerdicts(t *testing.T) {
	t0 := time.Unix(0, 0).Add(time.Hour)
	recs := []eventlog.Record{
		mkRec(t, t0, eventlog.EvtWorkerCheckpoint, map[string]any{"verdict": "abstain"}),
		mkRec(t, t0.Add(time.Second), eventlog.EvtWorkerCheckpoint, map[string]any{"other": "data"}),
		mkRec(t, t0.Add(2*time.Second), eventlog.EvtWorkerCheckpoint, map[string]any{"verdict": "ack"}),
	}
	f := tacticalAggregator{}.Aggregate(recs, t0, t0.Add(time.Minute))
	if f.EventCount != 3 {
		t.Errorf("EventCount = %d, want 3 (all records counted toward window)", f.EventCount)
	}
	if f.Split != [2]int{1, 0} {
		t.Errorf("Split = %v, want [1 0] (only the ack counted)", f.Split)
	}
	if f.Verdict != "ack" {
		t.Errorf("Verdict = %q, want ack", f.Verdict)
	}
}

func TestTacticalAggregator_VacuousAck(t *testing.T) {
	t0 := time.Unix(0, 0).Add(time.Hour)
	f := tacticalAggregator{}.Aggregate(nil, t0, t0.Add(time.Minute))
	if f.Verdict != "ack" {
		t.Errorf("Verdict = %q, want ack (vacuous-ack)", f.Verdict)
	}
	if f.NeedsFix {
		t.Error("NeedsFix = true, want false")
	}
	if f.Disagreement {
		t.Error("Disagreement = true, want false")
	}
	if f.Split != [2]int{0, 0} {
		t.Errorf("Split = %v, want [0 0]", f.Split)
	}
	if f.EventCount != 0 {
		t.Errorf("EventCount = %d, want 0", f.EventCount)
	}
}

func TestStrategicAggregator_FlagsDisagreementAcrossLayers(t *testing.T) {
	t0 := time.Unix(0, 0).Add(time.Hour)
	recs := []eventlog.Record{
		mkRec(t, t0, eventlog.EvtReviewerWaveComplete, map[string]any{"verdict": "ack"}),
		mkRec(t, t0.Add(time.Second), eventlog.EvtReviewerWaveComplete, map[string]any{"verdict": "needs_fix", "proposal": "ignored"}),
	}
	f := strategicAggregator{}.Aggregate(recs, t0, t0.Add(11*time.Minute))
	if f.Layer != LayerStrategic {
		t.Errorf("Layer = %v, want LayerStrategic", f.Layer)
	}
	if f.Verdict != "needs_fix" {
		t.Errorf("Verdict = %q, want needs_fix", f.Verdict)
	}
	if !f.Disagreement {
		t.Error("Disagreement = false, want true")
	}
	if !f.NeedsFix {
		t.Error("NeedsFix = false, want true")
	}
	if f.Split != [2]int{1, 1} {
		t.Errorf("Split = %v, want [1 1]", f.Split)
	}
	if len(f.FixProposals) != 0 {
		t.Errorf("FixProposals = %v, want empty (strategic does not collect proposals)", f.FixProposals)
	}
}

func TestStrategicAggregator_PluralityNeedsFix(t *testing.T) {
	t0 := time.Unix(0, 0).Add(time.Hour)
	recs := []eventlog.Record{
		mkRec(t, t0, eventlog.EvtReviewerWaveComplete, map[string]any{"verdict": "needs_fix"}),
		mkRec(t, t0.Add(time.Second), eventlog.EvtReviewerWaveComplete, map[string]any{"verdict": "needs_fix"}),
		mkRec(t, t0.Add(2*time.Second), eventlog.EvtReviewerWaveComplete, map[string]any{"verdict": "ack"}),
	}
	f := strategicAggregator{}.Aggregate(recs, t0, t0.Add(11*time.Minute))
	if f.Verdict != "needs_fix" {
		t.Errorf("Verdict = %q, want needs_fix", f.Verdict)
	}
	if !f.NeedsFix {
		t.Error("NeedsFix = false, want true")
	}
	if f.Disagreement {
		t.Error("Disagreement = true, want false (plurality 2/1 at threshold)")
	}
}

func TestArchitecturalAggregator_NeedsFixOnAnyFix(t *testing.T) {
	t0 := time.Unix(0, 0).Add(time.Hour)
	recs := []eventlog.Record{
		mkRec(t, t0, eventlog.EvtTacticalAggregation, map[string]any{
			"verdict": "needs_fix",
			"summary": "L3 split detected",
		}),
	}
	f := architecturalAggregator{}.Aggregate(recs, t0, t0.Add(31*time.Minute))
	if f.Layer != LayerArchitectural {
		t.Errorf("Layer = %v, want LayerArchitectural", f.Layer)
	}
	if f.Verdict != "needs_fix" {
		t.Errorf("Verdict = %q, want needs_fix", f.Verdict)
	}
	if !f.NeedsFix {
		t.Error("NeedsFix = false, want true")
	}
	if f.Disagreement {
		t.Error("Disagreement = true, want false (no ack present)")
	}
	if f.Summary != "L3 split detected" {
		t.Errorf("Summary = %q, want %q", f.Summary, "L3 split detected")
	}
	if f.Split != [2]int{0, 1} {
		t.Errorf("Split = %v, want [0 1]", f.Split)
	}
}

func TestArchitecturalAggregator_DisagreementOnAckFixMix(t *testing.T) {
	t0 := time.Unix(0, 0).Add(time.Hour)
	recs := []eventlog.Record{
		mkRec(t, t0, eventlog.EvtTacticalAggregation, map[string]any{"verdict": "ack"}),
		mkRec(t, t0.Add(time.Second), eventlog.EvtStrategicAggregation, map[string]any{"verdict": "needs_fix", "summary": "drift"}),
	}
	f := architecturalAggregator{}.Aggregate(recs, t0, t0.Add(31*time.Minute))
	if f.Verdict != "needs_fix" {
		t.Errorf("Verdict = %q, want needs_fix", f.Verdict)
	}
	if !f.NeedsFix {
		t.Error("NeedsFix = false, want true")
	}
	if !f.Disagreement {
		t.Error("Disagreement = false, want true (ack+fix mix)")
	}
	if f.Summary != "drift" {
		t.Errorf("Summary = %q, want %q", f.Summary, "drift")
	}
	if f.Split != [2]int{1, 1} {
		t.Errorf("Split = %v, want [1 1]", f.Split)
	}
}

func TestArchitecturalAggregator_AllAck(t *testing.T) {
	t0 := time.Unix(0, 0).Add(time.Hour)
	recs := []eventlog.Record{
		mkRec(t, t0, eventlog.EvtTacticalAggregation, map[string]any{"verdict": "ack"}),
		mkRec(t, t0.Add(time.Second), eventlog.EvtStrategicAggregation, map[string]any{"verdict": "ack"}),
	}
	f := architecturalAggregator{}.Aggregate(recs, t0, t0.Add(31*time.Minute))
	if f.Verdict != "ack" {
		t.Errorf("Verdict = %q, want ack", f.Verdict)
	}
	if f.NeedsFix {
		t.Error("NeedsFix = true, want false")
	}
	if f.Disagreement {
		t.Error("Disagreement = true, want false")
	}
	if f.Summary != "" {
		t.Errorf("Summary = %q, want \"\"", f.Summary)
	}
	if f.Split != [2]int{2, 0} {
		t.Errorf("Split = %v, want [2 0]", f.Split)
	}
}

func TestArchitecturalAggregator_EmptyWindow(t *testing.T) {
	t0 := time.Unix(0, 0).Add(time.Hour)
	f := architecturalAggregator{}.Aggregate(nil, t0, t0.Add(31*time.Minute))
	if f.Verdict != "ack" {
		t.Errorf("Verdict = %q, want ack (empty window)", f.Verdict)
	}
	if f.EventCount != 0 {
		t.Errorf("EventCount = %d, want 0", f.EventCount)
	}
	if f.NeedsFix {
		t.Error("NeedsFix = true, want false")
	}
	if f.Disagreement {
		t.Error("Disagreement = true, want false")
	}
}

func TestArchitecturalAggregator_JoinsMultipleSummaries(t *testing.T) {
	t0 := time.Unix(0, 0).Add(time.Hour)
	recs := []eventlog.Record{
		mkRec(t, t0, eventlog.EvtTacticalAggregation, map[string]any{"verdict": "needs_fix", "summary": "alpha"}),
		mkRec(t, t0.Add(time.Second), eventlog.EvtStrategicAggregation, map[string]any{"verdict": "needs_fix", "summary": "beta"}),
		mkRec(t, t0.Add(2*time.Second), eventlog.EvtTacticalAggregation, map[string]any{"verdict": "needs_fix"}),
	}
	f := architecturalAggregator{}.Aggregate(recs, t0, t0.Add(31*time.Minute))
	if f.Summary != "alpha; beta" {
		t.Errorf("Summary = %q, want %q", f.Summary, "alpha; beta")
	}
}

func TestPayloadString_HandlesMissingKey(t *testing.T) {
	rec := eventlog.Record{Payload: []byte(`{"foo":"bar"}`)}
	if got := payloadString(rec, "verdict"); got != "" {
		t.Errorf("payloadString(missing key) = %q, want \"\"", got)
	}
}

func TestPayloadString_HandlesInvalidJSON(t *testing.T) {
	rec := eventlog.Record{Payload: []byte("not json {{{")}
	if got := payloadString(rec, "verdict"); got != "" {
		t.Errorf("payloadString(corrupt) = %q, want \"\"", got)
	}
}

func TestPayloadString_HandlesNonStringValue(t *testing.T) {
	rec := eventlog.Record{Payload: []byte(`{"verdict":42}`)}
	if got := payloadString(rec, "verdict"); got != "" {
		t.Errorf("payloadString(non-string) = %q, want \"\"", got)
	}
}

func TestPayloadString_HandlesEmptyPayload(t *testing.T) {
	rec := eventlog.Record{Payload: nil}
	if got := payloadString(rec, "verdict"); got != "" {
		t.Errorf("payloadString(nil payload) = %q, want \"\"", got)
	}
	rec2 := eventlog.Record{Payload: []byte{}}
	if got := payloadString(rec2, "verdict"); got != "" {
		t.Errorf("payloadString(empty payload) = %q, want \"\"", got)
	}
}

func TestPayloadString_HandlesValidStringValue(t *testing.T) {
	rec := eventlog.Record{Payload: []byte(`{"verdict":"ack","other":"x"}`)}
	if got := payloadString(rec, "verdict"); got != "ack" {
		t.Errorf("payloadString = %q, want %q", got, "ack")
	}
	if got := payloadString(rec, "other"); got != "x" {
		t.Errorf("payloadString = %q, want %q", got, "x")
	}
}

func TestClassify_PluralityCases(t *testing.T) {
	cases := []struct {
		name             string
		ack, fix         int
		wantVerdict      string
		wantNeedsFix     bool
		wantDisagreement bool
	}{
		{"vacuous", 0, 0, "ack", false, false},
		{"ack-dominant-3-1", 3, 1, "ack", false, false},
		{"fix-dominant-1-3", 1, 3, "needs_fix", true, false},
		{"tie-1-1", 1, 1, "needs_fix", true, true},
		{"tie-2-2", 2, 2, "needs_fix", true, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := Finding{}
			classify(&f, c.ack, c.fix)
			if f.Verdict != c.wantVerdict {
				t.Errorf("Verdict = %q, want %q", f.Verdict, c.wantVerdict)
			}
			if f.NeedsFix != c.wantNeedsFix {
				t.Errorf("NeedsFix = %v, want %v", f.NeedsFix, c.wantNeedsFix)
			}
			if f.Disagreement != c.wantDisagreement {
				t.Errorf("Disagreement = %v, want %v", f.Disagreement, c.wantDisagreement)
			}
		})
	}
}

// TestAggregateTacticalDelegate_MatchesAggregator pins the delegate
// invariant: the package-private aggregateTactical function (called
// by tacticalLoop) MUST produce the same Finding as
// tacticalAggregator{}.Aggregate for any input. Locks the
// wholesale-replace contract H-5 introduces.
func TestAggregateTacticalDelegate_MatchesAggregator(t *testing.T) {
	t0 := time.Unix(0, 0).Add(time.Hour)
	recs := []eventlog.Record{
		mkRec(t, t0, eventlog.EvtWorkerCheckpoint, map[string]any{"verdict": "ack"}),
		mkRec(t, t0.Add(time.Second), eventlog.EvtWorkerCheckpoint, map[string]any{"verdict": "needs_fix", "proposal": "P1"}),
	}
	a := aggregateTactical(recs, t0, t0.Add(time.Minute))
	b := tacticalAggregator{}.Aggregate(recs, t0, t0.Add(time.Minute))
	if a.Verdict != b.Verdict || a.NeedsFix != b.NeedsFix || a.Disagreement != b.Disagreement || a.EventCount != b.EventCount || a.Split != b.Split {
		t.Errorf("delegate mismatch:\n  a=%+v\n  b=%+v", a, b)
	}
}

func TestAggregateStrategicDelegate_MatchesAggregator(t *testing.T) {
	t0 := time.Unix(0, 0).Add(time.Hour)
	recs := []eventlog.Record{
		mkRec(t, t0, eventlog.EvtReviewerWaveComplete, map[string]any{"verdict": "ack"}),
		mkRec(t, t0.Add(time.Second), eventlog.EvtReviewerWaveComplete, map[string]any{"verdict": "needs_fix"}),
	}
	a := aggregateStrategic(recs, t0, t0.Add(11*time.Minute))
	b := strategicAggregator{}.Aggregate(recs, t0, t0.Add(11*time.Minute))
	if a.Verdict != b.Verdict || a.Disagreement != b.Disagreement || a.EventCount != b.EventCount || a.Split != b.Split {
		t.Errorf("delegate mismatch:\n  a=%+v\n  b=%+v", a, b)
	}
}

func TestAggregateArchitecturalDelegate_MatchesAggregator(t *testing.T) {
	t0 := time.Unix(0, 0).Add(time.Hour)
	recs := []eventlog.Record{
		mkRec(t, t0, eventlog.EvtTacticalAggregation, map[string]any{"verdict": "needs_fix", "summary": "x"}),
	}
	a := aggregateArchitectural(recs, t0, t0.Add(31*time.Minute))
	b := architecturalAggregator{}.Aggregate(recs, t0, t0.Add(31*time.Minute))
	if a.Verdict != b.Verdict || a.NeedsFix != b.NeedsFix || a.EventCount != b.EventCount || a.Summary != b.Summary {
		t.Errorf("delegate mismatch:\n  a=%+v\n  b=%+v", a, b)
	}
}
