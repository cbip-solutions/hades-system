// tests/timeaccel/timeaccel_test.go (Plan 5 Phase O Task O-5).
//
// HRA cadence timeaccel test — asserts the Phase H tactical cadence
// fires at the doctrine-declared interval (T=3min for max-scope) when
// driven by the harness's *clock.Fake.
//
// Pattern: spin up an hra.HRACoordinator under a Fake clock; inject a
// WorkerCheckpoint record into its tactical subscription; advance the
// clock past the cadence boundary; assert exactly one
// EvtTacticalAggregation event was emitted via the eventlog Append
// surface.
//
// This test is the template other timeaccel cases will follow (Phase
// K cooldown windows, Phase E recovery heartbeat, watcher cadence).
//
//go:build timeaccel
// +build timeaccel

package timeaccel_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/hra"
	"github.com/cbip-solutions/hades-system/tests/timeaccel"
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

func (f *fakeEventLog) eventsChannelLen(t eventlog.EventType) int {
	sub := f.subscribeForFilter(t)
	if sub == nil {
		return -1
	}
	return len(sub.events)
}

func TestHRA_TacticalCadenceFiresAtT_MaxScope(t *testing.T) {
	const T = 3 * time.Minute

	h := timeaccel.NewHarness(timeaccel.HarnessOpts{Anchor: time.Unix(0, 0).UTC()})
	log := &fakeEventLog{}
	ctx := &fakeCoordinatorContext{
		sessionID: "sess-timeaccel-T",
		projectID: "proj-timeaccel-T",
		doctrine:  "max-scope",
	}

	coord, err := hra.New(hra.Config{
		Clock:    h.Clock(),
		EventLog: log,
		Context:  ctx,
	})
	if err != nil {
		t.Fatalf("hra.New: %v", err)
	}

	runCtx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- coord.Run(runCtx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
			t.Fatalf("Run did not exit after cancel")
		}
	})

	if !h.Fake().BlockUntilCondition(func() bool {
		return log.subscribeForFilter(eventlog.EvtWorkerCheckpoint) != nil
	}, 2*time.Second) {
		t.Fatalf("HRA coordinator never subscribed to EvtWorkerCheckpoint")
	}

	sub := log.subscribeForFilter(eventlog.EvtWorkerCheckpoint)
	rec := eventlog.Record{
		EventID:   1,
		SessionID: "sess-timeaccel-T",
		ProjectID: "proj-timeaccel-T",
		EventType: eventlog.EvtWorkerCheckpoint,
		Timestamp: 1,
	}
	select {
	case sub.events <- rec:
	case <-time.After(time.Second):
		t.Fatalf("subscription buffer full; injection deadlocked")
	}

	if !h.Fake().BlockUntilCondition(func() bool {
		return log.eventsChannelLen(eventlog.EvtWorkerCheckpoint) == 0
	}, 2*time.Second) {
		t.Fatalf("cadence goroutine did not drain injected event")
	}

	h.Fake().Advance(T + time.Second)

	if !h.Fake().BlockUntilCondition(func() bool {
		return log.appendedOf(eventlog.EvtTacticalAggregation) == 1
	}, 2*time.Second) {
		t.Fatalf("EvtTacticalAggregation not emitted at T=%v (appendedOf=%d)",
			T, log.appendedOf(eventlog.EvtTacticalAggregation))
	}
}

func TestHRA_TacticalCadenceFires10TimesIn30Min_MaxScope(t *testing.T) {

	const T = 3 * time.Minute

	h := timeaccel.NewHarness(timeaccel.HarnessOpts{Anchor: time.Unix(0, 0).UTC()})
	log := &fakeEventLog{}
	ctx := &fakeCoordinatorContext{
		sessionID: "sess-T-10",
		projectID: "proj-T-10",
		doctrine:  "max-scope",
	}

	coord, err := hra.New(hra.Config{
		Clock:    h.Clock(),
		EventLog: log,
		Context:  ctx,
	})
	if err != nil {
		t.Fatalf("hra.New: %v", err)
	}

	runCtx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- coord.Run(runCtx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
			t.Fatalf("Run did not exit after cancel")
		}
	})

	if !h.Fake().BlockUntilCondition(func() bool {
		return log.subscribeForFilter(eventlog.EvtWorkerCheckpoint) != nil
	}, 2*time.Second) {
		t.Fatalf("HRA coordinator never subscribed to EvtWorkerCheckpoint")
	}

	sub := log.subscribeForFilter(eventlog.EvtWorkerCheckpoint)

	for i := 0; i < 10; i++ {
		rec := eventlog.Record{
			EventID:   int64(i + 1),
			SessionID: "sess-T-10",
			ProjectID: "proj-T-10",
			EventType: eventlog.EvtWorkerCheckpoint,
			Timestamp: int64(i + 1),
		}
		select {
		case sub.events <- rec:
		case <-time.After(time.Second):
			t.Fatalf("subscription buffer full at iteration %d", i)
		}

		if !h.Fake().BlockUntilCondition(func() bool {
			return log.eventsChannelLen(eventlog.EvtWorkerCheckpoint) == 0
		}, 2*time.Second) {
			t.Fatalf("cadence goroutine did not drain at iteration %d", i)
		}
		h.Fake().Advance(T + time.Second)

		want := i + 1
		if !h.Fake().BlockUntilCondition(func() bool {
			return log.appendedOf(eventlog.EvtTacticalAggregation) == want
		}, 2*time.Second) {
			t.Fatalf("after step %d: EvtTacticalAggregation count=%d, want %d",
				i+1, log.appendedOf(eventlog.EvtTacticalAggregation), want)
		}
	}

	if got := log.appendedOf(eventlog.EvtTacticalAggregation); got != 10 {
		t.Errorf("after 30min, EvtTacticalAggregation = %d, want 10", got)
	}
}
