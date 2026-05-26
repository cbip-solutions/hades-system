package hra_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/hra"
)

type fakeCoordinatorContext struct {
	sessionID string
	projectID string
	doctrine  string
}

func (f *fakeCoordinatorContext) SessionID() string { return f.sessionID }
func (f *fakeCoordinatorContext) ProjectID() string { return f.projectID }
func (f *fakeCoordinatorContext) Doctrine() string  { return f.doctrine }

type fakeSubscription struct {
	mu     sync.Mutex
	events chan eventlog.Record
	done   chan struct{}
	closed bool
}

func newFakeSubscription(buf int) *fakeSubscription {
	if buf < 1 {
		buf = 1
	}
	return &fakeSubscription{
		events: make(chan eventlog.Record, buf),
		done:   make(chan struct{}),
	}
}

func (s *fakeSubscription) Events() <-chan eventlog.Record { return s.events }
func (s *fakeSubscription) Done() <-chan struct{}          { return s.done }

func (s *fakeSubscription) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	close(s.done)
}

func (s *fakeSubscription) IsClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

type fakeEventLog struct {
	mu         sync.Mutex
	subscribes []eventlog.Filter
	subs       []*fakeSubscription
	appends    []eventlog.Event
}

func (f *fakeEventLog) Subscribe(filter eventlog.Filter, bufferSize int) eventlog.Subscription {
	f.mu.Lock()
	defer f.mu.Unlock()
	sub := newFakeSubscription(bufferSize)
	f.subscribes = append(f.subscribes, filter)
	f.subs = append(f.subs, sub)
	return sub
}

func (f *fakeEventLog) Append(_ context.Context, ev eventlog.Event) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.appends = append(f.appends, ev)
	return int64(len(f.appends)), nil
}

func (f *fakeEventLog) snapshot() ([]eventlog.Filter, []*fakeSubscription) {
	f.mu.Lock()
	defer f.mu.Unlock()
	filters := make([]eventlog.Filter, len(f.subscribes))
	copy(filters, f.subscribes)
	subs := make([]*fakeSubscription, len(f.subs))
	copy(subs, f.subs)
	return filters, subs
}

func (f *fakeEventLog) Appends() []eventlog.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]eventlog.Event, len(f.appends))
	copy(out, f.appends)
	return out
}

func (f *fakeEventLog) appendedOf(t eventlog.EventType) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, ev := range f.appends {
		if ev.Type == t {
			n++
		}
	}
	return n
}

func (f *fakeEventLog) subscribeForFilter(t eventlog.EventType) *fakeSubscription {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, fil := range f.subscribes {
		for _, ft := range fil.Types {
			if ft == t {
				return f.subs[i]
			}
		}
	}
	return nil
}

func newConfig(t *testing.T, doctrine string) hra.Config {
	t.Helper()
	return hra.Config{
		Clock:    clock.Real{},
		EventLog: &fakeEventLog{},
		Context: &fakeCoordinatorContext{
			sessionID: "sess-1",
			projectID: "proj-1",
			doctrine:  doctrine,
		},
	}
}

func TestCadenceFor_AllDoctrines(t *testing.T) {
	tests := []struct {
		doctrine string
		want     hra.CadenceMatrix
	}{
		{
			doctrine: "max-scope",
			want: hra.CadenceMatrix{
				Tactical:      3 * time.Minute,
				Strategic:     10 * time.Minute,
				Architectural: 30 * time.Minute,
			},
		},
		{
			doctrine: "default",
			want: hra.CadenceMatrix{
				Tactical:      5 * time.Minute,
				Strategic:     0,
				Architectural: 0,
			},
		},
		{
			doctrine: "capa-firewall",
			want:     hra.CadenceMatrix{},
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.doctrine, func(t *testing.T) {
			got, err := hra.CadenceFor(tc.doctrine)
			if err != nil {
				t.Fatalf("CadenceFor(%q): unexpected error: %v", tc.doctrine, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("CadenceFor(%q) = %+v, want %+v", tc.doctrine, got, tc.want)
			}
		})
	}
}

func TestCadenceFor_UnknownDoctrineReturnsError(t *testing.T) {
	got, err := hra.CadenceFor("fictional")
	if err == nil {
		t.Fatalf("CadenceFor(fictional): want error, got nil (matrix=%+v)", got)
	}
	if got != (hra.CadenceMatrix{}) {
		t.Errorf("CadenceFor(fictional): want zero matrix on error, got %+v", got)
	}
}

func TestLayer_String(t *testing.T) {
	cases := []struct {
		layer hra.Layer
		want  string
	}{
		{hra.LayerTactical, "tactical"},
		{hra.LayerStrategic, "strategic"},
		{hra.LayerArchitectural, "architectural"},
		{hra.Layer(0), "unknown"},
		{hra.Layer(99), "unknown"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.want, func(t *testing.T) {
			if got := tc.layer.String(); got != tc.want {
				t.Errorf("Layer(%d).String() = %q, want %q", int(tc.layer), got, tc.want)
			}
		})
	}
}

func TestNew_HappyPathWithDoctrineResolution(t *testing.T) {
	cfg := newConfig(t, "max-scope")
	c, err := hra.New(cfg)
	if err != nil {
		t.Fatalf("New: unexpected error: %v", err)
	}
	if c == nil {
		t.Fatalf("New: returned nil coordinator")
	}
	got := c.Cadence()
	want := hra.CadenceMatrix{
		Tactical:      3 * time.Minute,
		Strategic:     10 * time.Minute,
		Architectural: 30 * time.Minute,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Cadence() = %+v, want %+v", got, want)
	}
}

func TestNew_ExplicitCadenceOverrideSkipsDoctrineResolution(t *testing.T) {
	override := hra.CadenceMatrix{
		Tactical:      1 * time.Second,
		Strategic:     2 * time.Second,
		Architectural: 3 * time.Second,
	}
	cfg := newConfig(t, "fictional")
	cfg.Cadence = override
	c, err := hra.New(cfg)
	if err != nil {
		t.Fatalf("New with override: unexpected error: %v", err)
	}
	if got := c.Cadence(); !reflect.DeepEqual(got, override) {
		t.Errorf("Cadence() = %+v, want override %+v", got, override)
	}
}

func TestNew_UnknownDoctrineWithoutOverride(t *testing.T) {
	cfg := newConfig(t, "fictional")
	_, err := hra.New(cfg)
	if err == nil {
		t.Fatalf("New: want error for unknown doctrine, got nil")
	}
	if !errors.Is(err, hra.ErrInvalidConfig) {
		t.Errorf("New: error = %v, want wraps ErrInvalidConfig", err)
	}
}

func TestNew_NilDeps(t *testing.T) {
	good := newConfig(t, "max-scope")

	cases := []struct {
		name string
		mut  func(c *hra.Config)
	}{
		{"nil clock", func(c *hra.Config) { c.Clock = nil }},
		{"nil eventlog", func(c *hra.Config) { c.EventLog = nil }},
		{"nil context", func(c *hra.Config) { c.Context = nil }},
		{"empty session id", func(c *hra.Config) {
			c.Context = &fakeCoordinatorContext{sessionID: "", projectID: "p", doctrine: "max-scope"}
		}},
		{"empty project id", func(c *hra.Config) {
			c.Context = &fakeCoordinatorContext{sessionID: "s", projectID: "", doctrine: "max-scope"}
		}},
		{"empty doctrine", func(c *hra.Config) {
			c.Context = &fakeCoordinatorContext{sessionID: "s", projectID: "p", doctrine: ""}
		}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			cfg := good

			cfg.EventLog = &fakeEventLog{}
			tc.mut(&cfg)
			_, err := hra.New(cfg)
			if err == nil {
				t.Fatalf("New(%s): want error, got nil", tc.name)
			}
			if !errors.Is(err, hra.ErrInvalidConfig) {
				t.Errorf("New(%s): error = %v, want wraps ErrInvalidConfig", tc.name, err)
			}
		})
	}
}

func runUntilReady(t *testing.T, c *hra.HRACoordinator, log *fakeEventLog) (context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- c.Run(ctx)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		filters, _ := log.snapshot()
		if len(filters) >= 3 {
			return cancel, errCh
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	t.Fatalf("Run: subscriptions not registered within 2s; got %d", len(log.subscribes))
	return cancel, errCh
}

var startCoord = runUntilReady

func injectTacticalEvent(t *testing.T, log *fakeEventLog, projectID string) {
	t.Helper()
	sub := log.subscribeForFilter(eventlog.EvtWorkerCheckpoint)
	if sub == nil {
		t.Fatalf("no fake subscription for tactical layer (EvtWorkerCheckpoint)")
	}
	rec := eventlog.Record{
		EventID:   1,
		SessionID: "sess-h2",
		ProjectID: projectID,
		EventType: eventlog.EvtWorkerCheckpoint,
		Timestamp: 1,
	}
	select {
	case sub.events <- rec:
	case <-time.After(time.Second):
		t.Fatalf("tactical sub events channel blocked on send")
	}
}

func TestRun_TacticalCadenceFiresAndEmitsAggregation(t *testing.T) {
	fake := clock.NewFake(time.Unix(0, 0))
	log := &fakeEventLog{}
	cfg := hra.Config{
		Clock:    fake,
		EventLog: log,
		Context: &fakeCoordinatorContext{
			sessionID: "sess-h2",
			projectID: "proj-h2",
			doctrine:  "max-scope",
		},
	}
	c, err := hra.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	cancel, errCh := startCoord(t, c, log)
	t.Cleanup(func() {
		cancel()
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
			t.Fatalf("Run did not exit after cancel")
		}
	})

	injectTacticalEvent(t, log, "proj-h2")
	injectTacticalEvent(t, log, "proj-h2")
	injectTacticalEvent(t, log, "proj-h2")

	sub := log.subscribeForFilter(eventlog.EvtWorkerCheckpoint)
	if !fake.BlockUntilCondition(func() bool { return len(sub.events) == 0 }, 2*time.Second) {
		t.Fatalf("cadence goroutine did not drain injected events")
	}

	fake.Advance(3*time.Minute + time.Second)

	if !fake.BlockUntilCondition(func() bool {
		return log.appendedOf(eventlog.EvtTacticalAggregation) == 1
	}, 2*time.Second) {
		t.Fatalf("EvtTacticalAggregation not emitted (appendedOf=%d, all=%+v)",
			log.appendedOf(eventlog.EvtTacticalAggregation), log.Appends())
	}

	var found eventlog.Event
	for _, ev := range log.Appends() {
		if ev.Type == eventlog.EvtTacticalAggregation {
			found = ev
			break
		}
	}
	if found.SessionID != "sess-h2" {
		t.Errorf("aggregation event SessionID = %q, want sess-h2", found.SessionID)
	}
	if found.ProjectID != "proj-h2" {
		t.Errorf("aggregation event ProjectID = %q, want proj-h2", found.ProjectID)
	}
	if found.Payload == nil {
		t.Fatalf("aggregation event Payload is nil")
	}
	if got := found.Payload["layer"]; got != "tactical" {
		t.Errorf("payload layer = %v, want tactical", got)
	}
	if got := found.Payload["events_count"]; got != 3 {
		t.Errorf("payload events_count = %v (%T), want 3", got, got)
	}
	if got := found.Payload["verdict"]; got != "ack" {
		t.Errorf("payload verdict = %v, want ack (vacuous-ack: records carry no verdict in payload)", got)
	}
	if got := found.Payload["needs_fix"]; got != false {
		t.Errorf("payload needs_fix = %v, want false", got)
	}
	if got := found.Payload["disagreement"]; got != false {
		t.Errorf("payload disagreement = %v, want false", got)
	}
	if _, ok := found.Payload["window_start"]; !ok {
		t.Errorf("payload missing window_start")
	}
	if _, ok := found.Payload["window_end"]; !ok {
		t.Errorf("payload missing window_end")
	}
}

func TestRun_DefaultDoctrineUsesFiveMinuteTacticalCadence(t *testing.T) {
	fake := clock.NewFake(time.Unix(0, 0))
	log := &fakeEventLog{}
	cfg := hra.Config{
		Clock:    fake,
		EventLog: log,
		Context: &fakeCoordinatorContext{
			sessionID: "sess-d",
			projectID: "proj-d",
			doctrine:  "default",
		},
	}
	c, err := hra.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	cancel, errCh := startCoord(t, c, log)
	t.Cleanup(func() {
		cancel()
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
			t.Fatalf("Run did not exit after cancel")
		}
	})

	injectTacticalEvent(t, log, "proj-d")
	injectTacticalEvent(t, log, "proj-d")
	injectTacticalEvent(t, log, "proj-d")

	sub := log.subscribeForFilter(eventlog.EvtWorkerCheckpoint)
	if !fake.BlockUntilCondition(func() bool { return len(sub.events) == 0 }, 2*time.Second) {
		t.Fatalf("cadence goroutine did not drain injected events")
	}

	fake.Advance(4 * time.Minute)

	time.Sleep(50 * time.Millisecond)
	if got := log.appendedOf(eventlog.EvtTacticalAggregation); got != 0 {
		t.Fatalf("premature emission at 4min under default doctrine: count=%d", got)
	}

	fake.Advance(2 * time.Minute)
	if !fake.BlockUntilCondition(func() bool {
		return log.appendedOf(eventlog.EvtTacticalAggregation) == 1
	}, 2*time.Second) {
		t.Fatalf("EvtTacticalAggregation not emitted after 6min (count=%d)",
			log.appendedOf(eventlog.EvtTacticalAggregation))
	}
}

func TestRun_CapaFirewallSkipsTacticalGoroutine(t *testing.T) {
	fake := clock.NewFake(time.Unix(0, 0))
	log := &fakeEventLog{}
	cfg := hra.Config{
		Clock:    fake,
		EventLog: log,
		Context: &fakeCoordinatorContext{
			sessionID: "sess-cf",
			projectID: "proj-cf",
			doctrine:  "capa-firewall",
		},
	}
	c, err := hra.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := c.Cadence().Tactical; got != 0 {
		t.Fatalf("capa-firewall tactical cadence = %v, want 0", got)
	}
	cancel, errCh := startCoord(t, c, log)
	t.Cleanup(func() {
		cancel()
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
			t.Fatalf("Run did not exit after cancel")
		}
	})

	injectTacticalEvent(t, log, "proj-cf")
	fake.Advance(time.Hour)
	time.Sleep(100 * time.Millisecond)

	if got := log.appendedOf(eventlog.EvtTacticalAggregation); got != 0 {
		t.Fatalf("capa-firewall: cadence emitted %d aggregations, want 0", got)
	}
}

func TestRun_EmptyWindowSkipsEmission(t *testing.T) {
	fake := clock.NewFake(time.Unix(0, 0))
	log := &fakeEventLog{}
	cfg := hra.Config{
		Clock:    fake,
		EventLog: log,
		Context: &fakeCoordinatorContext{
			sessionID: "sess-ew",
			projectID: "proj-ew",
			doctrine:  "max-scope",
		},
	}
	c, err := hra.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	cancel, errCh := startCoord(t, c, log)
	t.Cleanup(func() {
		cancel()
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
			t.Fatalf("Run did not exit after cancel")
		}
	})

	fake.Advance(3*time.Minute + time.Second)
	time.Sleep(100 * time.Millisecond)

	if got := log.appendedOf(eventlog.EvtTacticalAggregation); got != 0 {
		t.Fatalf("empty window emitted %d aggregations, want 0 (compact-log discipline)", got)
	}
}

type recordingEscalator struct {
	mu    sync.Mutex
	calls []recordedEscalation
}

type recordedEscalation struct {
	layer   hra.Layer
	finding hra.Finding
}

func (r *recordingEscalator) HandleDisagreement(layer hra.Layer, f hra.Finding) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, recordedEscalation{layer: layer, finding: f})
}

func (r *recordingEscalator) Calls() []recordedEscalation {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]recordedEscalation, len(r.calls))
	copy(out, r.calls)
	return out
}

// TestRun_DisagreementInvokesEscalator pins the SetEscalator wiring.
// In H-2 the placeholder aggregator never returns Disagreement=true, so
// the recordingEscalator is expected to receive ZERO calls — but the
// hook MUST be installed (smoke-test that SetEscalator works and that
// the cadence goroutine reads the field). H-6 will replace this with a
// real disagreement-flow test once the aggregator + escalator wiring
// are real.
func TestRun_DisagreementInvokesEscalator(t *testing.T) {
	fake := clock.NewFake(time.Unix(0, 0))
	log := &fakeEventLog{}
	cfg := hra.Config{
		Clock:    fake,
		EventLog: log,
		Context: &fakeCoordinatorContext{
			sessionID: "sess-esc",
			projectID: "proj-esc",
			doctrine:  "max-scope",
		},
	}
	c, err := hra.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	rec := &recordingEscalator{}
	c.SetEscalator(rec)

	cancel, errCh := startCoord(t, c, log)
	t.Cleanup(func() {
		cancel()
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
			t.Fatalf("Run did not exit after cancel")
		}
	})

	injectTacticalEvent(t, log, "proj-esc")
	sub := log.subscribeForFilter(eventlog.EvtWorkerCheckpoint)
	if !fake.BlockUntilCondition(func() bool { return len(sub.events) == 0 }, 2*time.Second) {
		t.Fatalf("cadence goroutine did not drain injected events")
	}
	fake.Advance(3*time.Minute + time.Second)

	if !fake.BlockUntilCondition(func() bool {
		return log.appendedOf(eventlog.EvtTacticalAggregation) == 1
	}, 2*time.Second) {
		t.Fatalf("aggregation never emitted; cadence path broken")
	}

	if got := len(rec.Calls()); got != 0 {
		t.Errorf("escalator calls = %d, want 0 (placeholder aggregator never disagrees)", got)
	}
}

func TestSetEscalator_NilUsesNopDefault(t *testing.T) {
	cfg := newConfig(t, "max-scope")
	c, err := hra.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	c.SetEscalator(&recordingEscalator{})
	c.SetEscalator(nil)

}

func TestSetEscalator_AfterStartedNoOps(t *testing.T) {
	log := &fakeEventLog{}
	cfg := hra.Config{
		Clock:    clock.Real{},
		EventLog: log,
		Context: &fakeCoordinatorContext{
			sessionID: "sess-asx",
			projectID: "proj-asx",
			doctrine:  "max-scope",
		},
	}
	c, err := hra.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	cancel, errCh := startCoord(t, c, log)
	t.Cleanup(func() {
		cancel()
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
			t.Fatalf("Run did not exit after cancel")
		}
	})

	c.SetEscalator(&recordingEscalator{})
}

func injectStrategicEvent(t *testing.T, log *fakeEventLog, projectID string) {
	t.Helper()
	sub := log.subscribeForFilter(eventlog.EvtReviewerWaveComplete)
	if sub == nil {
		t.Fatalf("no fake subscription for strategic layer (EvtReviewerWaveComplete)")
	}
	rec := eventlog.Record{
		EventID:   1,
		SessionID: "sess-h3",
		ProjectID: projectID,
		EventType: eventlog.EvtReviewerWaveComplete,
		Timestamp: 1,
	}
	select {
	case sub.events <- rec:
	case <-time.After(time.Second):
		t.Fatalf("strategic sub events channel blocked on send")
	}
}

func TestRun_StrategicCadenceFiresAt10MinMaxScope(t *testing.T) {
	fake := clock.NewFake(time.Unix(0, 0))
	log := &fakeEventLog{}
	cfg := hra.Config{
		Clock:    fake,
		EventLog: log,
		Context: &fakeCoordinatorContext{
			sessionID: "sess-h3",
			projectID: "proj-h3",
			doctrine:  "max-scope",
		},
	}
	c, err := hra.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	cancel, errCh := startCoord(t, c, log)
	t.Cleanup(func() {
		cancel()
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
			t.Fatalf("Run did not exit after cancel")
		}
	})

	injectStrategicEvent(t, log, "proj-h3")
	injectStrategicEvent(t, log, "proj-h3")

	sub := log.subscribeForFilter(eventlog.EvtReviewerWaveComplete)
	if !fake.BlockUntilCondition(func() bool { return len(sub.events) == 0 }, 2*time.Second) {
		t.Fatalf("strategic cadence goroutine did not drain injected events")
	}

	fake.Advance(10*time.Minute + time.Second)

	if !fake.BlockUntilCondition(func() bool {
		return log.appendedOf(eventlog.EvtStrategicAggregation) == 1
	}, 2*time.Second) {
		t.Fatalf("EvtStrategicAggregation not emitted (appendedOf=%d, all=%+v)",
			log.appendedOf(eventlog.EvtStrategicAggregation), log.Appends())
	}

	var found eventlog.Event
	for _, ev := range log.Appends() {
		if ev.Type == eventlog.EvtStrategicAggregation {
			found = ev
			break
		}
	}
	if found.SessionID != "sess-h3" {
		t.Errorf("aggregation event SessionID = %q, want sess-h3", found.SessionID)
	}
	if found.ProjectID != "proj-h3" {
		t.Errorf("aggregation event ProjectID = %q, want proj-h3", found.ProjectID)
	}
	if found.Payload == nil {
		t.Fatalf("aggregation event Payload is nil")
	}
	if got := found.Payload["layer"]; got != "strategic" {
		t.Errorf("payload layer = %v, want strategic", got)
	}
	if got := found.Payload["events_count"]; got != 2 {
		t.Errorf("payload events_count = %v (%T), want 2", got, got)
	}
	if got := found.Payload["verdict"]; got != "ack" {
		t.Errorf("payload verdict = %v, want ack (vacuous-ack: records carry no verdict in payload)", got)
	}
	if got := found.Payload["needs_fix"]; got != false {
		t.Errorf("payload needs_fix = %v, want false", got)
	}
	if got := found.Payload["disagreement"]; got != false {
		t.Errorf("payload disagreement = %v, want false", got)
	}
	if _, ok := found.Payload["window_start"]; !ok {
		t.Errorf("payload missing window_start")
	}
	if _, ok := found.Payload["window_end"]; !ok {
		t.Errorf("payload missing window_end")
	}

	if _, ok := found.Payload["split"]; ok {
		t.Errorf("payload contains split for placeholder aggregator (want absent)")
	}
}

func TestRun_DefaultDoctrineNeverFiresStrategic(t *testing.T) {
	fake := clock.NewFake(time.Unix(0, 0))
	log := &fakeEventLog{}
	cfg := hra.Config{
		Clock:    fake,
		EventLog: log,
		Context: &fakeCoordinatorContext{
			sessionID: "sess-d3",
			projectID: "proj-d3",
			doctrine:  "default",
		},
	}
	c, err := hra.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := c.Cadence().Strategic; got != 0 {
		t.Fatalf("default strategic cadence = %v, want 0", got)
	}
	cancel, errCh := startCoord(t, c, log)
	t.Cleanup(func() {
		cancel()
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
			t.Fatalf("Run did not exit after cancel")
		}
	})

	fake.Advance(time.Hour)
	time.Sleep(100 * time.Millisecond)

	if got := log.appendedOf(eventlog.EvtStrategicAggregation); got != 0 {
		t.Fatalf("default doctrine: strategic cadence emitted %d aggregations, want 0", got)
	}
}

func TestRun_CapaFirewallSkipsStrategicGoroutine(t *testing.T) {
	fake := clock.NewFake(time.Unix(0, 0))
	log := &fakeEventLog{}
	cfg := hra.Config{
		Clock:    fake,
		EventLog: log,
		Context: &fakeCoordinatorContext{
			sessionID: "sess-cf3",
			projectID: "proj-cf3",
			doctrine:  "capa-firewall",
		},
	}
	c, err := hra.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := c.Cadence().Strategic; got != 0 {
		t.Fatalf("capa-firewall strategic cadence = %v, want 0", got)
	}
	cancel, errCh := startCoord(t, c, log)
	t.Cleanup(func() {
		cancel()
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
			t.Fatalf("Run did not exit after cancel")
		}
	})

	injectStrategicEvent(t, log, "proj-cf3")
	fake.Advance(time.Hour)
	time.Sleep(100 * time.Millisecond)

	if got := log.appendedOf(eventlog.EvtStrategicAggregation); got != 0 {
		t.Fatalf("capa-firewall: strategic cadence emitted %d aggregations, want 0", got)
	}
}

func TestRun_StrategicEmptyWindowSkipsEmission(t *testing.T) {
	fake := clock.NewFake(time.Unix(0, 0))
	log := &fakeEventLog{}
	cfg := hra.Config{
		Clock:    fake,
		EventLog: log,
		Context: &fakeCoordinatorContext{
			sessionID: "sess-sew",
			projectID: "proj-sew",
			doctrine:  "max-scope",
		},
	}
	c, err := hra.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	cancel, errCh := startCoord(t, c, log)
	t.Cleanup(func() {
		cancel()
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
			t.Fatalf("Run did not exit after cancel")
		}
	})

	fake.Advance(10*time.Minute + time.Second)
	time.Sleep(100 * time.Millisecond)

	if got := log.appendedOf(eventlog.EvtStrategicAggregation); got != 0 {
		t.Fatalf("strategic empty window emitted %d aggregations, want 0 (compact-log discipline)", got)
	}
}

func TestRun_StrategicAndTacticalInterleave(t *testing.T) {
	fake := clock.NewFake(time.Unix(0, 0))
	log := &fakeEventLog{}
	cfg := hra.Config{
		Clock:    fake,
		EventLog: log,
		Context: &fakeCoordinatorContext{
			sessionID: "sess-mix",
			projectID: "proj-mix",
			doctrine:  "max-scope",
		},
	}
	c, err := hra.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	cancel, errCh := startCoord(t, c, log)
	t.Cleanup(func() {
		cancel()
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
			t.Fatalf("Run did not exit after cancel")
		}
	})

	injectTacticalEvent(t, log, "proj-mix")
	injectStrategicEvent(t, log, "proj-mix")

	tsub := log.subscribeForFilter(eventlog.EvtWorkerCheckpoint)
	ssub := log.subscribeForFilter(eventlog.EvtReviewerWaveComplete)
	if !fake.BlockUntilCondition(func() bool {
		return len(tsub.events) == 0 && len(ssub.events) == 0
	}, 2*time.Second) {
		t.Fatalf("cadence goroutines did not drain injected events (tac=%d str=%d)",
			len(tsub.events), len(ssub.events))
	}

	fake.Advance(30 * time.Minute)

	if !fake.BlockUntilCondition(func() bool {
		return log.appendedOf(eventlog.EvtTacticalAggregation) >= 1 &&
			log.appendedOf(eventlog.EvtStrategicAggregation) >= 1
	}, 2*time.Second) {
		t.Fatalf("interleave: tac=%d, str=%d (want both ≥ 1)",
			log.appendedOf(eventlog.EvtTacticalAggregation),
			log.appendedOf(eventlog.EvtStrategicAggregation))
	}
}

// TestRun_StrategicDisagreementInvokesEscalator pins the SetEscalator
// wiring on the strategic path. Symmetric to H-2's tactical version: in
// H-3 the placeholder aggregateStrategic returns Disagreement=false so
// the recordingEscalator receives ZERO calls — but the hook MUST be
// installed and the cadence goroutine MUST read the field. H-5/H-6 will
// replace this with a real disagreement-flow test.
func TestRun_StrategicDisagreementInvokesEscalator(t *testing.T) {
	fake := clock.NewFake(time.Unix(0, 0))
	log := &fakeEventLog{}
	cfg := hra.Config{
		Clock:    fake,
		EventLog: log,
		Context: &fakeCoordinatorContext{
			sessionID: "sess-sesc",
			projectID: "proj-sesc",
			doctrine:  "max-scope",
		},
	}
	c, err := hra.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	rec := &recordingEscalator{}
	c.SetEscalator(rec)

	cancel, errCh := startCoord(t, c, log)
	t.Cleanup(func() {
		cancel()
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
			t.Fatalf("Run did not exit after cancel")
		}
	})

	injectStrategicEvent(t, log, "proj-sesc")
	sub := log.subscribeForFilter(eventlog.EvtReviewerWaveComplete)
	if !fake.BlockUntilCondition(func() bool { return len(sub.events) == 0 }, 2*time.Second) {
		t.Fatalf("strategic cadence goroutine did not drain injected events")
	}
	fake.Advance(10*time.Minute + time.Second)

	if !fake.BlockUntilCondition(func() bool {
		return log.appendedOf(eventlog.EvtStrategicAggregation) == 1
	}, 2*time.Second) {
		t.Fatalf("strategic aggregation never emitted; cadence path broken")
	}

	calls := rec.Calls()
	for _, c := range calls {
		if c.layer == hra.LayerStrategic {
			t.Errorf("strategic escalator call recorded with placeholder aggregator: %+v", c)
		}
	}
}

func injectArchitecturalEvent(t *testing.T, log *fakeEventLog, projectID string) {
	t.Helper()
	sub := log.subscribeForFilter(eventlog.EvtTacticalAggregation)
	if sub == nil {
		t.Fatalf("no fake subscription for architectural layer (EvtTacticalAggregation)")
	}
	rec := eventlog.Record{
		EventID:   1,
		SessionID: "sess-h4",
		ProjectID: projectID,
		EventType: eventlog.EvtTacticalAggregation,
		Timestamp: 1,
	}
	select {
	case sub.events <- rec:
	case <-time.After(time.Second):
		t.Fatalf("architectural sub events channel blocked on send")
	}
}

func TestRun_ArchitecturalCadenceFiresAt30MinMaxScope(t *testing.T) {
	fake := clock.NewFake(time.Unix(0, 0))
	log := &fakeEventLog{}
	cfg := hra.Config{
		Clock:    fake,
		EventLog: log,
		Context: &fakeCoordinatorContext{
			sessionID: "sess-h4",
			projectID: "proj-h4",
			doctrine:  "max-scope",
		},
	}
	c, err := hra.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	cancel, errCh := startCoord(t, c, log)
	t.Cleanup(func() {
		cancel()
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
			t.Fatalf("Run did not exit after cancel")
		}
	})

	injectArchitecturalEvent(t, log, "proj-h4")

	sub := log.subscribeForFilter(eventlog.EvtTacticalAggregation)
	if !fake.BlockUntilCondition(func() bool { return len(sub.events) == 0 }, 2*time.Second) {
		t.Fatalf("architectural cadence goroutine did not drain injected events")
	}

	fake.Advance(30*time.Minute + time.Second)

	if !fake.BlockUntilCondition(func() bool {
		return log.appendedOf(eventlog.EvtArchitecturalReview) >= 1
	}, 2*time.Second) {
		t.Fatalf("EvtArchitecturalReview not emitted (appendedOf=%d, all=%+v)",
			log.appendedOf(eventlog.EvtArchitecturalReview), log.Appends())
	}

	var found eventlog.Event
	for _, ev := range log.Appends() {
		if ev.Type == eventlog.EvtArchitecturalReview {
			found = ev
			break
		}
	}
	if found.SessionID != "sess-h4" {
		t.Errorf("review event SessionID = %q, want sess-h4", found.SessionID)
	}
	if found.ProjectID != "proj-h4" {
		t.Errorf("review event ProjectID = %q, want proj-h4", found.ProjectID)
	}
	if found.Payload == nil {
		t.Fatalf("review event Payload is nil")
	}
	if got := found.Payload["layer"]; got != "architectural" {
		t.Errorf("payload layer = %v, want architectural", got)
	}
	if got := found.Payload["events_count"]; got != 1 {
		t.Errorf("payload events_count = %v (%T), want 1", got, got)
	}
	if got := found.Payload["verdict"]; got != "ack" {
		t.Errorf("payload verdict = %v, want ack (vacuous-ack: records carry no verdict in payload)", got)
	}
	if got := found.Payload["needs_fix"]; got != false {
		t.Errorf("payload needs_fix = %v, want false", got)
	}
	if got := found.Payload["disagreement"]; got != false {
		t.Errorf("payload disagreement = %v, want false", got)
	}
	if _, ok := found.Payload["window_start"]; !ok {
		t.Errorf("payload missing window_start")
	}
	if _, ok := found.Payload["window_end"]; !ok {
		t.Errorf("payload missing window_end")
	}

	if _, ok := found.Payload["summary"]; ok {
		t.Errorf("payload contains summary for placeholder aggregator (want absent)")
	}

	if got := log.appendedOf(eventlog.EvtEscalationDecision); got != 0 {
		t.Errorf("vacuous-ack architectural verdict emitted %d escalations, want 0", got)
	}
}

func TestRun_DefaultDoctrineArchitecturalCadenceIsZero(t *testing.T) {
	fake := clock.NewFake(time.Unix(0, 0))
	log := &fakeEventLog{}
	cfg := hra.Config{
		Clock:    fake,
		EventLog: log,
		Context: &fakeCoordinatorContext{
			sessionID: "sess-d4",
			projectID: "proj-d4",
			doctrine:  "default",
		},
	}
	c, err := hra.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := c.Cadence().Architectural; got != 0 {
		t.Fatalf("default architectural cadence = %v, want 0", got)
	}
	cancel, errCh := startCoord(t, c, log)
	t.Cleanup(func() {
		cancel()
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
			t.Fatalf("Run did not exit after cancel")
		}
	})

	fake.Advance(24 * time.Hour)
	time.Sleep(100 * time.Millisecond)

	if got := log.appendedOf(eventlog.EvtArchitecturalReview); got != 0 {
		t.Fatalf("default doctrine: architectural cadence emitted %d reviews, want 0", got)
	}
}

func TestRun_CapaFirewallSkipsArchitecturalGoroutine(t *testing.T) {
	fake := clock.NewFake(time.Unix(0, 0))
	log := &fakeEventLog{}
	cfg := hra.Config{
		Clock:    fake,
		EventLog: log,
		Context: &fakeCoordinatorContext{
			sessionID: "sess-cf4",
			projectID: "proj-cf4",
			doctrine:  "capa-firewall",
		},
	}
	c, err := hra.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := c.Cadence().Architectural; got != 0 {
		t.Fatalf("capa-firewall architectural cadence = %v, want 0", got)
	}
	cancel, errCh := startCoord(t, c, log)
	t.Cleanup(func() {
		cancel()
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
			t.Fatalf("Run did not exit after cancel")
		}
	})

	injectArchitecturalEvent(t, log, "proj-cf4")
	fake.Advance(time.Hour)
	time.Sleep(100 * time.Millisecond)

	if got := log.appendedOf(eventlog.EvtArchitecturalReview); got != 0 {
		t.Fatalf("capa-firewall: architectural cadence emitted %d reviews, want 0", got)
	}
}

func TestRun_ArchitecturalEmptyWindowSkipsEmission(t *testing.T) {
	fake := clock.NewFake(time.Unix(0, 0))
	log := &fakeEventLog{}
	cfg := hra.Config{
		Clock:    fake,
		EventLog: log,
		Context: &fakeCoordinatorContext{
			sessionID: "sess-aew",
			projectID: "proj-aew",
			doctrine:  "max-scope",
		},
	}
	c, err := hra.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	cancel, errCh := startCoord(t, c, log)
	t.Cleanup(func() {
		cancel()
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
			t.Fatalf("Run did not exit after cancel")
		}
	})

	fake.Advance(30*time.Minute + time.Second)
	time.Sleep(100 * time.Millisecond)

	if got := log.appendedOf(eventlog.EvtArchitecturalReview); got != 0 {
		t.Fatalf("architectural empty window emitted %d reviews, want 0 (compact-log discipline)", got)
	}
	if got := log.appendedOf(eventlog.EvtEscalationDecision); got != 0 {
		t.Fatalf("architectural empty window emitted %d escalations, want 0", got)
	}
}

func disagreementAggregator(events []eventlog.Record, _, _ time.Time) hra.Finding {
	return hra.Finding{
		Layer:        hra.LayerArchitectural,
		EventCount:   len(events),
		Verdict:      "needs_fix",
		NeedsFix:     true,
		Disagreement: true,
		Summary:      "test-injected disagreement",
	}
}

func needsFixOnlyAggregator(events []eventlog.Record, _, _ time.Time) hra.Finding {
	return hra.Finding{
		Layer:        hra.LayerArchitectural,
		EventCount:   len(events),
		Verdict:      "needs_fix",
		NeedsFix:     true,
		Disagreement: false,
	}
}

func TestRun_ArchitecturalEmitsEscalationOnDisagreement(t *testing.T) {
	fake := clock.NewFake(time.Unix(0, 0))
	log := &fakeEventLog{}
	cfg := hra.Config{
		Clock:    fake,
		EventLog: log,
		Context: &fakeCoordinatorContext{
			sessionID: "sess-aesc",
			projectID: "proj-aesc",
			doctrine:  "max-scope",
		},
	}
	c, err := hra.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c.SetArchitecturalAggregatorForTest(disagreementAggregator)
	rec := &recordingEscalator{}
	c.SetEscalator(rec)

	cancel, errCh := startCoord(t, c, log)
	t.Cleanup(func() {
		cancel()
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
			t.Fatalf("Run did not exit after cancel")
		}
	})

	injectArchitecturalEvent(t, log, "proj-aesc")
	sub := log.subscribeForFilter(eventlog.EvtTacticalAggregation)
	if !fake.BlockUntilCondition(func() bool { return len(sub.events) == 0 }, 2*time.Second) {
		t.Fatalf("architectural cadence goroutine did not drain injected events")
	}
	fake.Advance(30*time.Minute + time.Second)

	if !fake.BlockUntilCondition(func() bool {
		return log.appendedOf(eventlog.EvtArchitecturalReview) >= 1 &&
			log.appendedOf(eventlog.EvtEscalationDecision) >= 1
	}, 2*time.Second) {
		t.Fatalf("architectural review + escalation not both emitted (review=%d, esc=%d)",
			log.appendedOf(eventlog.EvtArchitecturalReview),
			log.appendedOf(eventlog.EvtEscalationDecision))
	}

	var escEv eventlog.Event
	for _, ev := range log.Appends() {
		if ev.Type == eventlog.EvtEscalationDecision {
			escEv = ev
			break
		}
	}
	if escEv.SessionID != "sess-aesc" {
		t.Errorf("escalation SessionID = %q, want sess-aesc", escEv.SessionID)
	}
	if escEv.ProjectID != "proj-aesc" {
		t.Errorf("escalation ProjectID = %q, want proj-aesc", escEv.ProjectID)
	}
	if got := escEv.Payload["class"]; got != "architectural" {
		t.Errorf("escalation class = %v, want architectural", got)
	}
	if got := escEv.Payload["target"]; got != "operator" {
		t.Errorf("escalation target = %v, want operator", got)
	}
	if got := escEv.Payload["from_layer"]; got != "architectural" {
		t.Errorf("escalation from_layer = %v, want architectural", got)
	}
	if got := escEv.Payload["verdict"]; got != "needs_fix" {
		t.Errorf("escalation verdict = %v, want needs_fix", got)
	}
	if got := escEv.Payload["needs_fix"]; got != true {
		t.Errorf("escalation needs_fix = %v, want true", got)
	}
	if got := escEv.Payload["disagreement"]; got != true {
		t.Errorf("escalation disagreement = %v, want true", got)
	}

	calls := rec.Calls()
	archCount := 0
	for _, ec := range calls {
		if ec.layer == hra.LayerArchitectural {
			archCount++
			if !ec.finding.Disagreement {
				t.Errorf("architectural escalator finding Disagreement = false, want true")
			}
		}
	}
	if archCount < 1 {
		t.Errorf("escalator received %d architectural calls, want ≥ 1", archCount)
	}

	var revEv eventlog.Event
	for _, ev := range log.Appends() {
		if ev.Type == eventlog.EvtArchitecturalReview {
			revEv = ev
			break
		}
	}
	if got, ok := revEv.Payload["summary"]; !ok {
		t.Errorf("review payload missing summary key when Finding.Summary non-empty: %+v", revEv.Payload)
	} else if got != "test-injected disagreement" {
		t.Errorf("review payload summary = %v, want test-injected disagreement", got)
	}
}

func TestRun_ArchitecturalEmitsEscalationOnNeedsFix(t *testing.T) {
	fake := clock.NewFake(time.Unix(0, 0))
	log := &fakeEventLog{}
	cfg := hra.Config{
		Clock:    fake,
		EventLog: log,
		Context: &fakeCoordinatorContext{
			sessionID: "sess-anfx",
			projectID: "proj-anfx",
			doctrine:  "max-scope",
		},
	}
	c, err := hra.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c.SetArchitecturalAggregatorForTest(needsFixOnlyAggregator)

	cancel, errCh := startCoord(t, c, log)
	t.Cleanup(func() {
		cancel()
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
			t.Fatalf("Run did not exit after cancel")
		}
	})

	injectArchitecturalEvent(t, log, "proj-anfx")
	sub := log.subscribeForFilter(eventlog.EvtTacticalAggregation)
	if !fake.BlockUntilCondition(func() bool { return len(sub.events) == 0 }, 2*time.Second) {
		t.Fatalf("architectural cadence goroutine did not drain injected events")
	}
	fake.Advance(30*time.Minute + time.Second)

	if !fake.BlockUntilCondition(func() bool {
		return log.appendedOf(eventlog.EvtEscalationDecision) >= 1
	}, 2*time.Second) {
		t.Fatalf("escalation not emitted on NeedsFix-only verdict (esc=%d)",
			log.appendedOf(eventlog.EvtEscalationDecision))
	}

	var escEv eventlog.Event
	for _, ev := range log.Appends() {
		if ev.Type == eventlog.EvtEscalationDecision {
			escEv = ev
			break
		}
	}
	if got := escEv.Payload["needs_fix"]; got != true {
		t.Errorf("escalation needs_fix = %v, want true", got)
	}
	if got := escEv.Payload["disagreement"]; got != false {
		t.Errorf("escalation disagreement = %v, want false (NeedsFix-only branch)", got)
	}
}

func TestRun_ArchitecturalNoEscalationOnOkVerdict(t *testing.T) {
	fake := clock.NewFake(time.Unix(0, 0))
	log := &fakeEventLog{}
	cfg := hra.Config{
		Clock:    fake,
		EventLog: log,
		Context: &fakeCoordinatorContext{
			sessionID: "sess-aok",
			projectID: "proj-aok",
			doctrine:  "max-scope",
		},
	}
	c, err := hra.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	cancel, errCh := startCoord(t, c, log)
	t.Cleanup(func() {
		cancel()
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
			t.Fatalf("Run did not exit after cancel")
		}
	})

	injectArchitecturalEvent(t, log, "proj-aok")
	sub := log.subscribeForFilter(eventlog.EvtTacticalAggregation)
	if !fake.BlockUntilCondition(func() bool { return len(sub.events) == 0 }, 2*time.Second) {
		t.Fatalf("architectural cadence goroutine did not drain injected events")
	}
	fake.Advance(30*time.Minute + time.Second)

	if !fake.BlockUntilCondition(func() bool {
		return log.appendedOf(eventlog.EvtArchitecturalReview) >= 1
	}, 2*time.Second) {
		t.Fatalf("architectural review not emitted (review=%d)",
			log.appendedOf(eventlog.EvtArchitecturalReview))
	}

	time.Sleep(50 * time.Millisecond)
	if got := log.appendedOf(eventlog.EvtEscalationDecision); got != 0 {
		t.Errorf("ok-verdict path emitted %d escalations, want 0", got)
	}
}

func TestRun_LastArchAtUpdatesAcrossFires(t *testing.T) {
	fake := clock.NewFake(time.Unix(0, 0))
	log := &fakeEventLog{}
	cfg := hra.Config{
		Clock:    fake,
		EventLog: log,
		Context: &fakeCoordinatorContext{
			sessionID: "sess-arc-cont",
			projectID: "proj-arc-cont",
			doctrine:  "max-scope",
		},
	}
	c, err := hra.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	cancel, errCh := startCoord(t, c, log)
	t.Cleanup(func() {
		cancel()
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
			t.Fatalf("Run did not exit after cancel")
		}
	})

	sub := log.subscribeForFilter(eventlog.EvtTacticalAggregation)

	injectArchitecturalEvent(t, log, "proj-arc-cont")
	if !fake.BlockUntilCondition(func() bool { return len(sub.events) == 0 }, 2*time.Second) {
		t.Fatalf("first window: cadence did not drain")
	}
	fake.Advance(30*time.Minute + time.Second)
	if !fake.BlockUntilCondition(func() bool {
		return log.appendedOf(eventlog.EvtArchitecturalReview) == 1
	}, 2*time.Second) {
		t.Fatalf("first architectural review not emitted")
	}

	// Capture first fire's window_end (= fireAt) — second fire's
	// window_start MUST equal it.
	var firstReview eventlog.Event
	for _, ev := range log.Appends() {
		if ev.Type == eventlog.EvtArchitecturalReview {
			firstReview = ev
			break
		}
	}
	firstWindowEnd, ok := firstReview.Payload["window_end"].(int64)
	if !ok {
		t.Fatalf("first review window_end is not int64: %T (%v)",
			firstReview.Payload["window_end"], firstReview.Payload["window_end"])
	}

	injectArchitecturalEvent(t, log, "proj-arc-cont")
	if !fake.BlockUntilCondition(func() bool { return len(sub.events) == 0 }, 2*time.Second) {
		t.Fatalf("second window: cadence did not drain")
	}
	fake.Advance(30*time.Minute + time.Second)
	if !fake.BlockUntilCondition(func() bool {
		return log.appendedOf(eventlog.EvtArchitecturalReview) == 2
	}, 2*time.Second) {
		t.Fatalf("second architectural review not emitted")
	}

	var secondReview eventlog.Event
	for _, ev := range log.Appends() {
		if ev.Type != eventlog.EvtArchitecturalReview {
			continue
		}
		if !ev.Timestamp.After(firstReview.Timestamp) {
			continue
		}
		secondReview = ev
		break
	}
	secondWindowStart, ok := secondReview.Payload["window_start"].(int64)
	if !ok {
		t.Fatalf("second review window_start is not int64: %T (%v)",
			secondReview.Payload["window_start"], secondReview.Payload["window_start"])
	}

	if secondWindowStart != firstWindowEnd {
		t.Errorf("continuous-window invariant violated: second.window_start=%d, first.window_end=%d",
			secondWindowStart, firstWindowEnd)
	}
}

func TestRun_RegistersThreeSubscribersWithCorrectFilters(t *testing.T) {
	log := &fakeEventLog{}
	cfg := hra.Config{
		Clock:    clock.Real{},
		EventLog: log,
		Context: &fakeCoordinatorContext{
			sessionID: "sess-x",
			projectID: "proj-x",
			doctrine:  "max-scope",
		},
	}
	c, err := hra.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	cancel, errCh := runUntilReady(t, c, log)
	t.Cleanup(func() {
		cancel()
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
			t.Fatalf("Run did not exit after cancel")
		}
	})

	filters, subs := log.snapshot()
	if len(filters) != 3 {
		t.Fatalf("subscribe call count = %d, want 3", len(filters))
	}
	if len(subs) != 3 {
		t.Fatalf("subscription count = %d, want 3", len(subs))
	}

	type expectedFilter struct {
		types []eventlog.EventType
	}
	expected := []expectedFilter{
		{types: []eventlog.EventType{eventlog.EvtWorkerCheckpoint}},
		{types: []eventlog.EventType{eventlog.EvtReviewerWaveComplete}},
		{types: []eventlog.EventType{eventlog.EvtTacticalAggregation, eventlog.EvtStrategicAggregation}},
	}

	matched := make([]bool, len(filters))
	for ei, ef := range expected {
		want := append([]eventlog.EventType(nil), ef.types...)
		sort.Slice(want, func(i, j int) bool { return want[i] < want[j] })
		found := false
		for fi, fil := range filters {
			if matched[fi] {
				continue
			}
			if fil.ProjectID != "proj-x" {
				continue
			}
			got := append([]eventlog.EventType(nil), fil.Types...)
			sort.Slice(got, func(i, j int) bool { return got[i] < got[j] })
			if reflect.DeepEqual(got, want) {
				matched[fi] = true
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected[%d] filter %v not registered (observed=%+v)", ei, ef.types, filters)
		}
	}
}

func TestRun_DoubleCallReturnsErrAlreadyStarted(t *testing.T) {
	log := &fakeEventLog{}
	cfg := hra.Config{
		Clock:    clock.Real{},
		EventLog: log,
		Context: &fakeCoordinatorContext{
			sessionID: "sess-d",
			projectID: "proj-d",
			doctrine:  "max-scope",
		},
	}
	c, err := hra.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	cancel, errCh := runUntilReady(t, c, log)
	t.Cleanup(func() {
		cancel()
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
			t.Fatalf("Run did not exit after cancel")
		}
	})

	if err := c.Run(context.Background()); !errors.Is(err, hra.ErrAlreadyStarted) {
		t.Fatalf("second Run: error = %v, want ErrAlreadyStarted", err)
	}
}

func TestRun_CancelClosesAllSubscriptions(t *testing.T) {
	log := &fakeEventLog{}
	cfg := hra.Config{
		Clock:    clock.Real{},
		EventLog: log,
		Context: &fakeCoordinatorContext{
			sessionID: "sess-c",
			projectID: "proj-c",
			doctrine:  "max-scope",
		},
	}
	c, err := hra.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	cancel, errCh := runUntilReady(t, c, log)

	cancel()
	select {
	case rerr := <-errCh:
		if rerr != nil {
			t.Fatalf("Run returned error on clean shutdown: %v", rerr)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("Run did not exit after cancel")
	}

	_, subs := log.snapshot()
	if len(subs) != 3 {
		t.Fatalf("subscription count = %d, want 3", len(subs))
	}
	for i, s := range subs {
		if !s.IsClosed() {
			t.Errorf("subscription[%d] not closed after Run exit", i)
		}
		select {
		case <-s.Done():
		case <-time.After(time.Second):
			t.Errorf("subscription[%d] Done() not signalled", i)
		}
	}
}

func directInjectArchitectural(t *testing.T, log *fakeEventLog, projectID string) {
	t.Helper()
	sub := log.subscribeForFilter(eventlog.EvtTacticalAggregation)
	if sub == nil {
		t.Fatalf("no fake subscription for architectural layer (EvtTacticalAggregation)")
	}
	rec := eventlog.Record{
		EventID:   1,
		ProjectID: projectID,
		EventType: eventlog.EvtTacticalAggregation,
		Timestamp: 1,
	}
	select {
	case sub.events <- rec:
	case <-time.After(time.Second):
		t.Fatalf("architectural sub events channel blocked on send")
	}
}

func TestOnPhaseBoundary_FiresArchitecturalUnderDefault(t *testing.T) {
	fake := clock.NewFake(time.Unix(1_700_000_000, 0))
	log := &fakeEventLog{}
	cfg := hra.Config{
		Clock:    fake,
		EventLog: log,
		Context: &fakeCoordinatorContext{
			sessionID: "sess-pb-default",
			projectID: "proj-pb-default",
			doctrine:  "default",
		},
	}
	c, err := hra.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := c.Cadence().Architectural; got != 0 {
		t.Fatalf("default architectural cadence = %v, want 0 (precondition)", got)
	}
	cancel, errCh := startCoord(t, c, log)
	t.Cleanup(func() {
		cancel()
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
			t.Fatalf("Run did not exit after cancel")
		}
	})

	directInjectArchitectural(t, log, "proj-pb-default")

	c.OnPhaseBoundary(context.Background(), "phase-A")

	if got := log.appendedOf(eventlog.EvtArchitecturalReview); got != 1 {
		t.Errorf("EvtArchitecturalReview count = %d, want 1; appends=%+v", got, log.Appends())
	}

	if got := log.appendedOf(eventlog.EvtPhaseBoundaryRecorded); got != 1 {
		t.Errorf("EvtPhaseBoundaryRecorded count = %d, want 1", got)
	}

	sub := log.subscribeForFilter(eventlog.EvtTacticalAggregation)
	if got := len(sub.events); got != 0 {
		t.Errorf("architecturalSub.events len = %d, want 0 (drained)", got)
	}
}

func TestOnPhaseBoundary_AppendsPhaseBoundaryRecordedEvent(t *testing.T) {
	fake := clock.NewFake(time.Unix(1_700_000_000, 0))
	log := &fakeEventLog{}
	cfg := hra.Config{
		Clock:    fake,
		EventLog: log,
		Context: &fakeCoordinatorContext{
			sessionID: "sess-pb-shape",
			projectID: "proj-pb-shape",
			doctrine:  "max-scope",
		},
	}
	c, err := hra.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	cancel, errCh := startCoord(t, c, log)
	t.Cleanup(func() {
		cancel()
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
			t.Fatalf("Run did not exit after cancel")
		}
	})

	// No records injected — empty-window. The trigger row MUST still
	// emit (compact-log discipline applies to EvtArchitecturalReview
	// but NOT to EvtPhaseBoundaryRecorded — the trigger is the
	// load-bearing replay correlation key).
	c.OnPhaseBoundary(context.Background(), "phase-Z")

	if got := log.appendedOf(eventlog.EvtPhaseBoundaryRecorded); got != 1 {
		t.Fatalf("EvtPhaseBoundaryRecorded count = %d, want 1", got)
	}

	if got := log.appendedOf(eventlog.EvtArchitecturalReview); got != 0 {
		t.Errorf("empty-window OnPhaseBoundary emitted %d reviews, want 0", got)
	}

	var ev eventlog.Event
	for _, e := range log.Appends() {
		if e.Type == eventlog.EvtPhaseBoundaryRecorded {
			ev = e
			break
		}
	}
	if ev.SessionID != "sess-pb-shape" {
		t.Errorf("trigger event SessionID = %q, want sess-pb-shape", ev.SessionID)
	}
	if ev.ProjectID != "proj-pb-shape" {
		t.Errorf("trigger event ProjectID = %q, want proj-pb-shape", ev.ProjectID)
	}
	if ev.Payload == nil {
		t.Fatalf("trigger event Payload is nil")
	}
	if got := ev.Payload["phase_id"]; got != "phase-Z" {
		t.Errorf("payload phase_id = %v, want phase-Z", got)
	}
	if got := ev.Payload["trigger"]; got != "phase_boundary" {
		t.Errorf("payload trigger = %v, want phase_boundary", got)
	}
}

func TestOnPhaseBoundary_AfterStoppedNoOps(t *testing.T) {
	log := &fakeEventLog{}
	cfg := hra.Config{
		Clock:    clock.Real{},
		EventLog: log,
		Context: &fakeCoordinatorContext{
			sessionID: "sess-pb-stopped",
			projectID: "proj-pb-stopped",
			doctrine:  "max-scope",
		},
	}
	c, err := hra.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	cancel, errCh := runUntilReady(t, c, log)
	cancel()
	select {
	case <-errCh:
	case <-time.After(2 * time.Second):
		t.Fatalf("Run did not exit after cancel")
	}

	priorBoundary := log.appendedOf(eventlog.EvtPhaseBoundaryRecorded)
	priorReview := log.appendedOf(eventlog.EvtArchitecturalReview)

	// Post-stopped call MUST NOT panic and MUST NOT emit any new event.
	c.OnPhaseBoundary(context.Background(), "phase-after-stop")

	if got := log.appendedOf(eventlog.EvtPhaseBoundaryRecorded); got != priorBoundary {
		t.Errorf("post-stopped EvtPhaseBoundaryRecorded delta = %d, want 0", got-priorBoundary)
	}
	if got := log.appendedOf(eventlog.EvtArchitecturalReview); got != priorReview {
		t.Errorf("post-stopped EvtArchitecturalReview delta = %d, want 0", got-priorReview)
	}
}

func TestTick_CapaFirewallManualTactical(t *testing.T) {
	fake := clock.NewFake(time.Unix(1_700_000_000, 0))
	log := &fakeEventLog{}
	cfg := hra.Config{
		Clock:    fake,
		EventLog: log,
		Context: &fakeCoordinatorContext{
			sessionID: "sess-tick-t",
			projectID: "proj-tick-t",
			doctrine:  "capa-firewall",
		},
	}
	c, err := hra.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := c.Cadence().Tactical; got != 0 {
		t.Fatalf("capa-firewall tactical cadence = %v, want 0 (precondition)", got)
	}
	cancel, errCh := startCoord(t, c, log)
	t.Cleanup(func() {
		cancel()
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
			t.Fatalf("Run did not exit after cancel")
		}
	})

	sub := log.subscribeForFilter(eventlog.EvtWorkerCheckpoint)
	rec := eventlog.Record{
		EventID:   1,
		ProjectID: "proj-tick-t",
		EventType: eventlog.EvtWorkerCheckpoint,
		Timestamp: 1,
	}
	select {
	case sub.events <- rec:
	case <-time.After(time.Second):
		t.Fatalf("tactical sub blocked on send")
	}

	if err := c.Tick(context.Background(), hra.LayerTactical); err != nil {
		t.Fatalf("Tick(LayerTactical) returned error: %v", err)
	}

	if got := log.appendedOf(eventlog.EvtTacticalAggregation); got != 1 {
		t.Errorf("EvtTacticalAggregation count = %d, want 1; appends=%+v", got, log.Appends())
	}
	if got := len(sub.events); got != 0 {
		t.Errorf("tacticalSub.events len = %d, want 0 (drained)", got)
	}
}

func TestTick_RejectsUnknownLayer(t *testing.T) {
	log := &fakeEventLog{}
	cfg := hra.Config{
		Clock:    clock.Real{},
		EventLog: log,
		Context: &fakeCoordinatorContext{
			sessionID: "sess-tick-bad",
			projectID: "proj-tick-bad",
			doctrine:  "max-scope",
		},
	}
	c, err := hra.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	cancel, errCh := startCoord(t, c, log)
	t.Cleanup(func() {
		cancel()
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
			t.Fatalf("Run did not exit after cancel")
		}
	})

	cases := []struct {
		name  string
		layer hra.Layer
	}{
		{"zero-value", hra.Layer(0)},
		{"out-of-range", hra.Layer(99)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := c.Tick(context.Background(), tc.layer)
			if err == nil {
				t.Fatalf("Tick(%v) returned nil error, want wrapped not-aggregable error", tc.layer)
			}
			if !strings.Contains(err.Error(), "not an aggregable layer") {
				t.Errorf("Tick(%v) error = %q, want contains \"not an aggregable layer\"", tc.layer, err.Error())
			}
		})
	}
}

func TestTick_AfterStoppedReturnsErr(t *testing.T) {
	log := &fakeEventLog{}
	cfg := hra.Config{
		Clock:    clock.Real{},
		EventLog: log,
		Context: &fakeCoordinatorContext{
			sessionID: "sess-tick-stopped",
			projectID: "proj-tick-stopped",
			doctrine:  "max-scope",
		},
	}
	c, err := hra.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	cancel, errCh := runUntilReady(t, c, log)
	cancel()
	select {
	case <-errCh:
	case <-time.After(2 * time.Second):
		t.Fatalf("Run did not exit after cancel")
	}

	terr := c.Tick(context.Background(), hra.LayerTactical)
	if terr == nil {
		t.Fatalf("Tick after Run exited returned nil error, want wrapped \"Tick after Run exited\"")
	}
	if !strings.Contains(terr.Error(), "Tick after Run exited") {
		t.Errorf("Tick post-stopped error = %q, want contains \"Tick after Run exited\"", terr.Error())
	}
}

func TestTick_StrategicAndArchitectural(t *testing.T) {
	fake := clock.NewFake(time.Unix(1_700_000_000, 0))
	log := &fakeEventLog{}
	cfg := hra.Config{
		Clock:    fake,
		EventLog: log,
		Context: &fakeCoordinatorContext{
			sessionID: "sess-tick-sa",
			projectID: "proj-tick-sa",
			doctrine:  "capa-firewall",
		},
	}
	c, err := hra.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	cancel, errCh := startCoord(t, c, log)
	t.Cleanup(func() {
		cancel()
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
			t.Fatalf("Run did not exit after cancel")
		}
	})

	stratSub := log.subscribeForFilter(eventlog.EvtReviewerWaveComplete)
	stratSub.events <- eventlog.Record{
		EventID:   1,
		ProjectID: "proj-tick-sa",
		EventType: eventlog.EvtReviewerWaveComplete,
		Timestamp: 1,
	}
	archSub := log.subscribeForFilter(eventlog.EvtTacticalAggregation)
	archSub.events <- eventlog.Record{
		EventID:   2,
		ProjectID: "proj-tick-sa",
		EventType: eventlog.EvtTacticalAggregation,
		Timestamp: 2,
	}

	if err := c.Tick(context.Background(), hra.LayerStrategic); err != nil {
		t.Fatalf("Tick(LayerStrategic) returned error: %v", err)
	}
	if err := c.Tick(context.Background(), hra.LayerArchitectural); err != nil {
		t.Fatalf("Tick(LayerArchitectural) returned error: %v", err)
	}

	if got := log.appendedOf(eventlog.EvtStrategicAggregation); got != 1 {
		t.Errorf("EvtStrategicAggregation count = %d, want 1", got)
	}
	if got := log.appendedOf(eventlog.EvtArchitecturalReview); got != 1 {
		t.Errorf("EvtArchitecturalReview count = %d, want 1", got)
	}
	if got := len(stratSub.events); got != 0 {
		t.Errorf("strategicSub drained: events len = %d, want 0", got)
	}
	if got := len(archSub.events); got != 0 {
		t.Errorf("architecturalSub drained: events len = %d, want 0", got)
	}
}

func mkVerdictRecord(t *testing.T, projectID string, et eventlog.EventType, ts time.Time, verdict string) eventlog.Record {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"verdict": verdict})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return eventlog.Record{
		ProjectID: projectID,
		EventType: et,
		Timestamp: ts.UnixNano(),
		Payload:   raw,
	}
}

func TestTick_TacticalDisagreementInvokesEscalator(t *testing.T) {

	now := time.Unix(1_700_000_000, 0)
	fake := clock.NewFake(now)
	log := &fakeEventLog{}
	cfg := hra.Config{
		Clock:    fake,
		EventLog: log,
		Context: &fakeCoordinatorContext{
			sessionID: "sess-tick-tdis",
			projectID: "proj-tick-tdis",
			doctrine:  "capa-firewall",
		},
	}
	c, err := hra.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	rec := &recordingEscalator{}
	c.SetEscalator(rec)

	cancel, errCh := startCoord(t, c, log)
	t.Cleanup(func() {
		cancel()
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
			t.Fatalf("Run did not exit after cancel")
		}
	})

	sub := log.subscribeForFilter(eventlog.EvtWorkerCheckpoint)

	ts := now.Add(-time.Second)
	sub.events <- mkVerdictRecord(t, "proj-tick-tdis", eventlog.EvtWorkerCheckpoint, ts, "ack")
	sub.events <- mkVerdictRecord(t, "proj-tick-tdis", eventlog.EvtWorkerCheckpoint, ts, "needs_fix")

	if err := c.Tick(context.Background(), hra.LayerTactical); err != nil {
		t.Fatalf("Tick(LayerTactical) returned error: %v", err)
	}
	if got := log.appendedOf(eventlog.EvtTacticalAggregation); got != 1 {
		t.Errorf("EvtTacticalAggregation count = %d, want 1; appends=%+v", got, log.Appends())
	}
	calls := rec.Calls()
	if len(calls) != 1 {
		t.Fatalf("escalator calls = %d, want 1 (tie-break flags Disagreement)", len(calls))
	}
	if calls[0].layer != hra.LayerTactical {
		t.Errorf("escalator call layer = %v, want LayerTactical", calls[0].layer)
	}
	if !calls[0].finding.Disagreement {
		t.Errorf("escalator finding Disagreement = false, want true")
	}
}

func TestTick_StrategicDisagreementInvokesEscalator(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	fake := clock.NewFake(now)
	log := &fakeEventLog{}
	cfg := hra.Config{
		Clock:    fake,
		EventLog: log,
		Context: &fakeCoordinatorContext{
			sessionID: "sess-tick-sdis",
			projectID: "proj-tick-sdis",
			doctrine:  "capa-firewall",
		},
	}
	c, err := hra.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	rec := &recordingEscalator{}
	c.SetEscalator(rec)

	cancel, errCh := startCoord(t, c, log)
	t.Cleanup(func() {
		cancel()
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
			t.Fatalf("Run did not exit after cancel")
		}
	})

	sub := log.subscribeForFilter(eventlog.EvtReviewerWaveComplete)
	ts := now.Add(-time.Second)
	sub.events <- mkVerdictRecord(t, "proj-tick-sdis", eventlog.EvtReviewerWaveComplete, ts, "ack")
	sub.events <- mkVerdictRecord(t, "proj-tick-sdis", eventlog.EvtReviewerWaveComplete, ts, "needs_fix")

	if err := c.Tick(context.Background(), hra.LayerStrategic); err != nil {
		t.Fatalf("Tick(LayerStrategic) returned error: %v", err)
	}
	if got := log.appendedOf(eventlog.EvtStrategicAggregation); got != 1 {
		t.Errorf("EvtStrategicAggregation count = %d, want 1; appends=%+v", got, log.Appends())
	}
	calls := rec.Calls()
	if len(calls) != 1 {
		t.Fatalf("escalator calls = %d, want 1 (tie-break flags Disagreement)", len(calls))
	}
	if calls[0].layer != hra.LayerStrategic {
		t.Errorf("escalator call layer = %v, want LayerStrategic", calls[0].layer)
	}
	if !calls[0].finding.Disagreement {
		t.Errorf("escalator finding Disagreement = false, want true")
	}
}

func TestTick_EmptyBufferSkipsEmission(t *testing.T) {
	log := &fakeEventLog{}
	cfg := hra.Config{
		Clock:    clock.Real{},
		EventLog: log,
		Context: &fakeCoordinatorContext{
			sessionID: "sess-tick-empty",
			projectID: "proj-tick-empty",
			doctrine:  "capa-firewall",
		},
	}
	c, err := hra.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	cancel, errCh := startCoord(t, c, log)
	t.Cleanup(func() {
		cancel()
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
			t.Fatalf("Run did not exit after cancel")
		}
	})

	if err := c.Tick(context.Background(), hra.LayerTactical); err != nil {
		t.Fatalf("Tick(LayerTactical) on empty buffer returned error: %v", err)
	}
	if got := log.appendedOf(eventlog.EvtTacticalAggregation); got != 0 {
		t.Errorf("empty-buffer Tick emitted %d aggregations, want 0", got)
	}
}
