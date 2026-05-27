package hra

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

var time0 = time.Unix(0, 0)

func TestNopEscalator_HandleDisagreementIsNoOp(t *testing.T) {
	var esc EscalationHandler = nopEscalator{}

	esc.HandleDisagreement(LayerTactical, Finding{
		Layer:        LayerTactical,
		EventCount:   1,
		Verdict:      "disagreement",
		Disagreement: true,
	})
	esc.HandleDisagreement(LayerStrategic, Finding{})
	esc.HandleDisagreement(LayerArchitectural, Finding{})
}

func TestAggregateTactical_DelegateContract(t *testing.T) {
	got := aggregateTactical(nil, time0, time0.Add(time.Minute))
	if got.Layer != LayerTactical {
		t.Errorf("Layer = %v, want LayerTactical", got.Layer)
	}
	if got.EventCount != 0 {
		t.Errorf("EventCount(empty) = %d, want 0", got.EventCount)
	}
	if got.Verdict != "ack" {
		t.Errorf("Verdict = %q, want ack (vacuous-ack)", got.Verdict)
	}
	if got.NeedsFix {
		t.Error("NeedsFix = true, want false")
	}
	if got.Disagreement {
		t.Error("Disagreement = true, want false")
	}
}

func TestAggregateStrategic_DelegateContract(t *testing.T) {
	got := aggregateStrategic(nil, time0, time0.Add(time.Minute))
	if got.Layer != LayerStrategic {
		t.Errorf("Layer = %v, want LayerStrategic", got.Layer)
	}
	if got.EventCount != 0 {
		t.Errorf("EventCount(empty) = %d, want 0", got.EventCount)
	}
	if got.Verdict != "ack" {
		t.Errorf("Verdict = %q, want ack", got.Verdict)
	}
	if got.NeedsFix {
		t.Error("NeedsFix = true, want false")
	}
	if got.Disagreement {
		t.Error("Disagreement = true, want false")
	}
	if got.Split != [2]int{0, 0} {
		t.Errorf("Split = %v, want [0 0]", got.Split)
	}

	recs := []eventlog.Record{
		{Timestamp: time0.Add(10 * time.Second).UnixNano()},
		{Timestamp: time0.Add(20 * time.Second).UnixNano()},
		{Timestamp: time0.Add(30 * time.Second).UnixNano()},
	}
	got2 := aggregateStrategic(recs, time0, time0.Add(time.Minute))
	if got2.EventCount != 3 {
		t.Errorf("EventCount(3 recs) = %d, want 3", got2.EventCount)
	}
}

type internalCaptureEventLog struct {
	mu      sync.Mutex
	appends []eventlog.Event
}

func (l *internalCaptureEventLog) Subscribe(_ eventlog.Filter, _ int) eventlog.Subscription {

	return nil
}

func (l *internalCaptureEventLog) Append(_ context.Context, ev eventlog.Event) (int64, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.appends = append(l.appends, ev)
	return int64(len(l.appends)), nil
}

type fakeInternalContext struct{}

func (fakeInternalContext) SessionID() string { return "sess-int" }
func (fakeInternalContext) ProjectID() string { return "proj-int" }
func (fakeInternalContext) Doctrine() string  { return "max-scope" }

// TestEmitStrategicAggregation_IncludesSplitWhenNonZero pins the FROZEN
// payload contract: the "split" key MUST be present (and equal to a
// 2-element [ack, needs_fix] JSON array) when Finding.Split has at
// least one non-zero count, and MUST be absent when both counts are
// zero. H-5's wholesale-replace changed Split from []string (cluster
// names placeholder) to [2]int — the strategic emit now serializes
// Split as a 2-element JSON array under "split".
func TestEmitStrategicAggregation_IncludesSplitWhenNonZero(t *testing.T) {
	log := &internalCaptureEventLog{}
	h := &HRACoordinator{
		cfg: Config{
			EventLog: log,
			Context:  fakeInternalContext{},
		},
	}
	fireAt := time0.Add(10 * time.Minute)
	since := time0
	finding := Finding{
		Layer:        LayerStrategic,
		EventCount:   2,
		Verdict:      "needs_fix",
		Disagreement: true,
		NeedsFix:     true,
		Split:        [2]int{1, 1},
	}
	h.emitStrategicAggregation(context.Background(), fireAt, since, finding)

	if len(log.appends) != 1 {
		t.Fatalf("appends = %d, want 1", len(log.appends))
	}
	ev := log.appends[0]
	if ev.Type != eventlog.EvtStrategicAggregation {
		t.Errorf("Type = %v, want EvtStrategicAggregation", ev.Type)
	}
	if ev.SessionID != "sess-int" {
		t.Errorf("SessionID = %q, want sess-int", ev.SessionID)
	}
	if ev.ProjectID != "proj-int" {
		t.Errorf("ProjectID = %q, want proj-int", ev.ProjectID)
	}
	got, ok := ev.Payload["split"]
	if !ok {
		t.Fatalf("payload missing split key when Finding.Split has counts: %+v", ev.Payload)
	}
	gotSplit, ok := got.([]int)
	if !ok {
		t.Fatalf("payload split type = %T, want []int", got)
	}
	if len(gotSplit) != 2 || gotSplit[0] != 1 || gotSplit[1] != 1 {
		t.Errorf("payload split = %v, want [1 1]", gotSplit)
	}
	if got := ev.Payload["disagreement"]; got != true {
		t.Errorf("payload disagreement = %v, want true", got)
	}
	if got := ev.Payload["verdict"]; got != "needs_fix" {
		t.Errorf("payload verdict = %v, want needs_fix", got)
	}
}

// TestEmitStrategicAggregation_OmitsSplitWhenZero pins the
// compact-log discipline: when both ack and needs_fix counts are zero,
// the "split" key MUST be absent from the payload (vacuous-window
// signal compression).
func TestEmitStrategicAggregation_OmitsSplitWhenZero(t *testing.T) {
	log := &internalCaptureEventLog{}
	h := &HRACoordinator{
		cfg: Config{
			EventLog: log,
			Context:  fakeInternalContext{},
		},
	}
	finding := Finding{
		Layer:      LayerStrategic,
		EventCount: 1,
		Verdict:    "ack",
		Split:      [2]int{0, 0},
	}
	h.emitStrategicAggregation(context.Background(), time0, time0, finding)

	if len(log.appends) != 1 {
		t.Fatalf("appends = %d, want 1", len(log.appends))
	}
	if _, ok := log.appends[0].Payload["split"]; ok {
		t.Errorf("split key present despite Split=[0,0]: %+v", log.appends[0].Payload)
	}
}

func TestAggregateArchitectural_DelegateContract(t *testing.T) {
	got := aggregateArchitectural(nil, time0, time0.Add(30*time.Minute))
	if got.Layer != LayerArchitectural {
		t.Errorf("Layer = %v, want LayerArchitectural", got.Layer)
	}
	if got.EventCount != 0 {
		t.Errorf("EventCount(empty) = %d, want 0", got.EventCount)
	}
	if got.Verdict != "ack" {
		t.Errorf("Verdict = %q, want ack (vacuous-ack)", got.Verdict)
	}
	if got.NeedsFix {
		t.Error("NeedsFix = true, want false")
	}
	if got.Disagreement {
		t.Error("Disagreement = true, want false")
	}
	if got.Summary != "" {
		t.Errorf("Summary = %q, want \"\"", got.Summary)
	}

	recs := []eventlog.Record{
		{Timestamp: time0.Add(time.Second).UnixNano()},
		{Timestamp: time0.Add(2 * time.Second).UnixNano()},
		{Timestamp: time0.Add(3 * time.Second).UnixNano()},
		{Timestamp: time0.Add(4 * time.Second).UnixNano()},
	}
	got2 := aggregateArchitectural(recs, time0, time0.Add(30*time.Minute))
	if got2.EventCount != 4 {
		t.Errorf("EventCount(4 recs) = %d, want 4", got2.EventCount)
	}
	if got2.Verdict != "ack" {
		t.Errorf("Verdict = %q, want ack (records present but no recognized verdict)", got2.Verdict)
	}
}

// TestEmitArchitecturalReview_IncludesSummaryWhenNonEmpty pins the FROZEN
// payload contract for emitArchitecturalReview: the "summary" key MUST
// be present (and equal to Finding.Summary) when Summary is non-empty,
// and MUST be absent when Summary is empty. The placeholder architectural
// aggregator never returns a non-empty Summary, so the cadence-loop path
// through this branch is exercised via SetArchitecturalAggregatorForTest
// in coordinator_test.go; this internal test locks the payload contract
// directly so H-5's swap is mechanical.
func TestEmitArchitecturalReview_IncludesSummaryWhenNonEmpty(t *testing.T) {
	log := &internalCaptureEventLog{}
	h := &HRACoordinator{
		cfg: Config{
			EventLog: log,
			Context:  fakeInternalContext{},
		},
	}
	fireAt := time0.Add(30 * time.Minute)
	since := time0
	finding := Finding{
		Layer:        LayerArchitectural,
		EventCount:   3,
		Verdict:      "needs_fix",
		NeedsFix:     true,
		Disagreement: true,
		Summary:      "L3 split detected on cache-invalidation strategy",
	}
	h.emitArchitecturalReview(context.Background(), fireAt, since, finding)

	if len(log.appends) != 1 {
		t.Fatalf("appends = %d, want 1", len(log.appends))
	}
	ev := log.appends[0]
	if ev.Type != eventlog.EvtArchitecturalReview {
		t.Errorf("Type = %v, want EvtArchitecturalReview", ev.Type)
	}
	if ev.SessionID != "sess-int" {
		t.Errorf("SessionID = %q, want sess-int", ev.SessionID)
	}
	if ev.ProjectID != "proj-int" {
		t.Errorf("ProjectID = %q, want proj-int", ev.ProjectID)
	}
	got, ok := ev.Payload["summary"]
	if !ok {
		t.Fatalf("payload missing summary key when Finding.Summary non-empty: %+v", ev.Payload)
	}
	gotSummary, ok := got.(string)
	if !ok {
		t.Fatalf("payload summary type = %T, want string", got)
	}
	if gotSummary != "L3 split detected on cache-invalidation strategy" {
		t.Errorf("payload summary = %q, want %q", gotSummary,
			"L3 split detected on cache-invalidation strategy")
	}
	if got := ev.Payload["disagreement"]; got != true {
		t.Errorf("payload disagreement = %v, want true", got)
	}
	if got := ev.Payload["needs_fix"]; got != true {
		t.Errorf("payload needs_fix = %v, want true", got)
	}
	if got := ev.Payload["verdict"]; got != "needs_fix" {
		t.Errorf("payload verdict = %v, want needs_fix", got)
	}
}

// TestEmitArchitecturalReview_OmitsSummaryWhenEmpty is the negative
// counterpart: when Finding.Summary == "", the "summary" key MUST be
// absent from the payload (compact-log discipline; gate
// parser interprets summary's presence as signal).
func TestEmitArchitecturalReview_OmitsSummaryWhenEmpty(t *testing.T) {
	log := &internalCaptureEventLog{}
	h := &HRACoordinator{
		cfg: Config{
			EventLog: log,
			Context:  fakeInternalContext{},
		},
	}
	finding := Finding{
		Layer:      LayerArchitectural,
		EventCount: 1,
		Verdict:    "ack",
	}
	h.emitArchitecturalReview(context.Background(), time0, time0, finding)

	if len(log.appends) != 1 {
		t.Fatalf("appends = %d, want 1", len(log.appends))
	}
	if _, ok := log.appends[0].Payload["summary"]; ok {
		t.Errorf("summary key present despite Finding.Summary empty: %+v",
			log.appends[0].Payload)
	}
}

func TestEmitEscalation_PayloadContract(t *testing.T) {
	log := &internalCaptureEventLog{}
	h := &HRACoordinator{
		cfg: Config{
			EventLog: log,
			Context:  fakeInternalContext{},
		},
	}
	fireAt := time0.Add(30 * time.Minute)
	finding := Finding{
		Layer:        LayerArchitectural,
		EventCount:   2,
		Verdict:      "needs_fix",
		NeedsFix:     true,
		Disagreement: true,
	}
	h.emitEscalation(context.Background(), fireAt, LayerArchitectural, finding)

	if len(log.appends) != 1 {
		t.Fatalf("appends = %d, want 1", len(log.appends))
	}
	ev := log.appends[0]
	if ev.Type != eventlog.EvtEscalationDecision {
		t.Errorf("Type = %v, want EvtEscalationDecision", ev.Type)
	}
	if ev.SessionID != "sess-int" {
		t.Errorf("SessionID = %q, want sess-int", ev.SessionID)
	}
	if ev.ProjectID != "proj-int" {
		t.Errorf("ProjectID = %q, want proj-int", ev.ProjectID)
	}
	if !ev.Timestamp.Equal(fireAt) {
		t.Errorf("Timestamp = %v, want %v", ev.Timestamp, fireAt)
	}
	if got := ev.Payload["class"]; got != "architectural" {
		t.Errorf("payload class = %v, want architectural", got)
	}
	if got := ev.Payload["target"]; got != "operator" {
		t.Errorf("payload target = %v, want operator", got)
	}
	if got := ev.Payload["from_layer"]; got != "architectural" {
		t.Errorf("payload from_layer = %v, want architectural", got)
	}
	if got := ev.Payload["verdict"]; got != "needs_fix" {
		t.Errorf("payload verdict = %v, want needs_fix", got)
	}
	if got := ev.Payload["needs_fix"]; got != true {
		t.Errorf("payload needs_fix = %v, want true", got)
	}
	if got := ev.Payload["disagreement"]; got != true {
		t.Errorf("payload disagreement = %v, want true", got)
	}
}

func TestSetArchitecturalAggregatorForTest_NilRestoresPlaceholder(t *testing.T) {
	h := &HRACoordinator{
		architecturalAggregatorFn: aggregateArchitectural,
	}

	custom := func(events []eventlog.Record, _, _ time.Time) Finding {
		return Finding{Layer: LayerArchitectural, EventCount: 999}
	}
	h.SetArchitecturalAggregatorForTest(custom)
	if got := h.architecturalAggregatorFn(nil, time0, time0); got.EventCount != 999 {
		t.Fatalf("custom aggregator not installed; EventCount=%d", got.EventCount)
	}

	h.SetArchitecturalAggregatorForTest(nil)
	if got := h.architecturalAggregatorFn(nil, time0, time0.Add(time.Hour)); got.EventCount != 0 || got.Verdict != "ack" {
		t.Fatalf("nil did not restore default delegate; got=%+v", got)
	}
}

func TestRunArchitecturalReview_PhaseBoundaryFirstFireUsesOneHourFallback(t *testing.T) {
	log := &internalCaptureEventLog{}
	h := &HRACoordinator{
		cfg: Config{
			EventLog: log,
			Context:  fakeInternalContext{},
		},
		cadence:                   CadenceMatrix{},
		escalator:                 nopEscalator{},
		architecturalAggregatorFn: aggregateArchitectural,
	}
	fireAt := time0.Add(2 * time.Hour)
	events := []eventlog.Record{{}}
	h.runArchitecturalReview(context.Background(), fireAt, events)

	if len(log.appends) != 1 {
		t.Fatalf("appends = %d, want 1 (review)", len(log.appends))
	}
	got := log.appends[0]
	wantStart := fireAt.Add(-1 * time.Hour).Unix()
	if gotStart := got.Payload["window_start"]; gotStart != wantStart {
		t.Errorf("window_start = %v, want %d (1h fallback)", gotStart, wantStart)
	}
	if gotEnd := got.Payload["window_end"]; gotEnd != fireAt.Unix() {
		t.Errorf("window_end = %v, want %d", gotEnd, fireAt.Unix())
	}

	// lastArchAt MUST update so the next fire's window starts here.
	if !h.lastArchAt.Equal(fireAt) {
		t.Errorf("lastArchAt = %v, want %v", h.lastArchAt, fireAt)
	}
}

// TestRunArchitecturalReview_EmptyEventsSkipsLastArchAtUpdate locks the
// continuous-window invariant: an empty-window call MUST NOT advance
// lastArchAt — otherwise the next non-empty fire would lose the prior
// interval, breaking the L4 review's wider-context promise.
func TestRunArchitecturalReview_EmptyEventsSkipsLastArchAtUpdate(t *testing.T) {
	log := &internalCaptureEventLog{}
	h := &HRACoordinator{
		cfg: Config{
			EventLog: log,
			Context:  fakeInternalContext{},
		},
		cadence:                   CadenceMatrix{Architectural: 30 * time.Minute},
		escalator:                 nopEscalator{},
		architecturalAggregatorFn: aggregateArchitectural,
	}
	priorFire := time0.Add(15 * time.Minute)
	h.lastArchAt = priorFire

	h.runArchitecturalReview(context.Background(), time0.Add(45*time.Minute), nil)

	if len(log.appends) != 0 {
		t.Errorf("empty-window emitted %d appends, want 0", len(log.appends))
	}
	if !h.lastArchAt.Equal(priorFire) {
		t.Errorf("lastArchAt mutated on empty fire: got %v, want %v", h.lastArchAt, priorFire)
	}
}

func TestSetArchitecturalAggregatorForTest_AfterStartedNoOps(t *testing.T) {
	h := &HRACoordinator{
		architecturalAggregatorFn: aggregateArchitectural,
		started:                   true,
	}
	custom := func(events []eventlog.Record, _, _ time.Time) Finding {
		return Finding{Layer: LayerArchitectural, EventCount: 42}
	}

	h.SetArchitecturalAggregatorForTest(custom)
	if got := h.architecturalAggregatorFn(nil, time0, time0); got.EventCount == 42 {
		t.Fatalf("aggregator was swapped after started=true (race-prone path)")
	}
}
