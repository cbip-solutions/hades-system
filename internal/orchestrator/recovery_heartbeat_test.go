// internal/orchestrator/recovery_heartbeat_test.go
//
//go:build timeaccel

package orchestrator

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

type fakeProbe struct {
	alive     map[string]time.Time
	errOnce   atomic.Value
	callCount atomic.Int32
}

func (f *fakeProbe) LastBeats(ctx context.Context) (map[string]time.Time, error) {
	f.callCount.Add(1)
	if v := f.errOnce.Load(); v != nil {
		if err, ok := v.(error); ok && err != nil {
			f.errOnce.Store((error)(nil))
			return nil, err
		}
	}
	out := make(map[string]time.Time, len(f.alive))
	for k, v := range f.alive {
		out[k] = v
	}
	return out, nil
}

type fakeProbeAlwaysErr struct {
	err       error
	callCount atomic.Int32
}

func (f *fakeProbeAlwaysErr) LastBeats(ctx context.Context) (map[string]time.Time, error) {
	f.callCount.Add(1)
	return nil, f.err
}

func queryEventsByType(
	t *testing.T,
	evlog *eventlog.Log,
	sessionID string,
	et eventlog.EventType,
) []eventlog.Record {
	t.Helper()
	recs, err := evlog.Query(context.Background(), sessionID, 0)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	var out []eventlog.Record
	for _, r := range recs {
		if r.EventType == et {
			out = append(out, r)
		}
	}
	return out
}

func waitForCondition(t *testing.T, deadline time.Duration, fn func() bool) bool {
	t.Helper()
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		if fn() {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return fn()
}

func driveOneTickAndWaitForDeath(
	t *testing.T,
	fc *clock.Fake,
	probe *fakeProbe,
	evlog *eventlog.Log,
	sessionID string,
	et eventlog.EventType,
	n int,
	tickStep time.Duration,
) []eventlog.Record {
	t.Helper()

	for i := 0; i < 50; i++ {

		time.Sleep(time.Millisecond)

		if i >= 4 {
			break
		}
	}

	fc.Advance(tickStep)

	if !waitForCondition(t, 2*time.Second, func() bool {
		return len(queryEventsByType(t, evlog, sessionID, et)) >= n
	}) {
		recs := queryEventsByType(t, evlog, sessionID, et)
		t.Fatalf("driveOneTickAndWaitForDeath: want >=%d %v records; got %d (probe calls=%d)",
			n, et, len(recs), probe.callCount.Load())
	}
	return queryEventsByType(t, evlog, sessionID, et)
}

func fakeFromEng(t *testing.T, fx *recoveryFixture) *clock.Fake {
	t.Helper()
	fc, ok := fx.eng.clk.(*clock.Fake)
	if !ok {
		t.Fatalf("fixture clock is not *clock.Fake (got %T)", fx.eng.clk)
	}
	return fc
}

func TestHeartbeatMonitor_DetectsTimeout_EmitsWorkerDeath(t *testing.T) {
	defer goleak.VerifyNone(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fx := newRecoveryFixture(t, "max-scope")
	fc := fakeFromEng(t, fx)
	now := fc.Now()
	probe := &fakeProbe{
		alive: map[string]time.Time{
			"w1": now,
			"w2": now.Add(-90 * time.Second),
		},
	}
	mon, err := NewHeartbeatMonitor(HeartbeatConfig{
		Engine:   fx.eng,
		Probe:    probe,
		Interval: 30 * time.Second,
		Timeout:  60 * time.Second,
		Clock:    fc,
	})
	if err != nil {
		t.Fatalf("NewHeartbeatMonitor: %v", err)
	}

	done := make(chan struct{})
	go func() {
		mon.Run(ctx)
		close(done)
	}()

	deaths := driveOneTickAndWaitForDeath(t, fc, probe, fx.evlog,
		"s-test-max-scope", eventlog.EvtWorkerDeath, 1, 31*time.Second)
	if len(deaths) != 1 {
		t.Fatalf("WorkerDeath count=%d want 1 (only w2 stale)", len(deaths))
	}
	dec, err := eventlog.Decode(deaths[0].EventType, deaths[0].Payload)
	if err != nil {
		t.Fatalf("Decode WorkerDeath: %v", err)
	}
	wd, ok := dec.(eventlog.WorkerDeath)
	if !ok {
		t.Fatalf("decoded payload type %T, want eventlog.WorkerDeath", dec)
	}
	if wd.WorkerID != "w2" {
		t.Fatalf("WorkerID=%q want w2", wd.WorkerID)
	}
	if wd.Class != FailureTransientInfra.String() {
		t.Fatalf("Class=%q want %q", wd.Class, FailureTransientInfra.String())
	}
	if wd.Reason != "heartbeat_timeout" {
		t.Fatalf("Reason=%q want heartbeat_timeout", wd.Reason)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("monitor did not exit within 1s after cancel")
	}
}

func TestHeartbeatMonitor_GracefulShutdown(t *testing.T) {
	defer goleak.VerifyNone(t)

	ctx, cancel := context.WithCancel(context.Background())
	fx := newRecoveryFixture(t, "max-scope")
	fc := fakeFromEng(t, fx)
	probe := &fakeProbe{alive: map[string]time.Time{"w1": fc.Now()}}
	mon, err := NewHeartbeatMonitor(HeartbeatConfig{
		Engine:   fx.eng,
		Probe:    probe,
		Interval: 30 * time.Second,
		Timeout:  60 * time.Second,
		Clock:    fc,
	})
	if err != nil {
		t.Fatalf("NewHeartbeatMonitor: %v", err)
	}
	done := make(chan struct{})
	go func() {
		mon.Run(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:

	case <-time.After(100 * time.Millisecond):
		t.Fatal("heartbeat monitor did not shut down within 100ms after cancel")
	}
}

func TestHeartbeatMonitor_HealthyWorkerNoDeath(t *testing.T) {
	defer goleak.VerifyNone(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fx := newRecoveryFixture(t, "max-scope")
	fc := fakeFromEng(t, fx)
	now := fc.Now()
	probe := &fakeProbe{
		alive: map[string]time.Time{
			"w1": now,
			"w2": now.Add(-10 * time.Second),
			"w3": now.Add(-30 * time.Second),
		},
	}
	mon, err := NewHeartbeatMonitor(HeartbeatConfig{
		Engine:   fx.eng,
		Probe:    probe,
		Interval: 30 * time.Second,
		Timeout:  60 * time.Second,
		Clock:    fc,
	})
	if err != nil {
		t.Fatalf("NewHeartbeatMonitor: %v", err)
	}
	done := make(chan struct{})
	go func() {
		mon.Run(ctx)
		close(done)
	}()

	probeCallCount := func() int { return int(probe.callCount.Load()) }
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && probeCallCount() == 0 {
		fc.Advance(31 * time.Second)
		fc.BlockUntilCondition(func() bool { return probeCallCount() >= 1 }, 50*time.Millisecond)
	}
	if probeCallCount() == 0 {
		t.Fatal("probe was never called — heartbeat tick never consumed")
	}

	recs, err := fx.evlog.Query(ctx, "s-test-max-scope", 0)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	for _, r := range recs {
		if r.EventType == eventlog.EvtWorkerDeath || r.EventType == eventlog.EvtWorkerRedispatched {
			t.Fatalf("unexpected event for healthy worker: %+v", r)
		}
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("monitor did not exit within 1s after cancel")
	}
}

func TestHeartbeatMonitor_DrivesHandleWorkerDeath_EmitsRedispatched(t *testing.T) {
	defer goleak.VerifyNone(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fx := newRecoveryFixture(t, "max-scope")
	fc := fakeFromEng(t, fx)

	if _, err := fx.evlog.Append(ctx, eventlog.Event{
		Type:      eventlog.EvtWorkerDispatched,
		SessionID: "s-test-max-scope",
		ProjectID: "p-test",
		Timestamp: fc.Now(),
		Payload: map[string]any{
			"worker_id": "w-stale",
			"task_id":   "task-42",
			"tier":      "t1_bypass",
		},
	}); err != nil {
		t.Fatalf("seed dispatch: %v", err)
	}

	probe := &fakeProbe{
		alive: map[string]time.Time{
			"w-stale": fc.Now().Add(-90 * time.Second),
		},
	}
	mon, err := NewHeartbeatMonitor(HeartbeatConfig{
		Engine:   fx.eng,
		Probe:    probe,
		Interval: 30 * time.Second,
		Timeout:  60 * time.Second,
		Clock:    fc,
	})
	if err != nil {
		t.Fatalf("NewHeartbeatMonitor: %v", err)
	}
	done := make(chan struct{})
	go func() {
		mon.Run(ctx)
		close(done)
	}()

	deaths := driveOneTickAndWaitForDeath(t, fc, probe, fx.evlog,
		"s-test-max-scope", eventlog.EvtWorkerDeath, 1, 31*time.Second)
	if len(deaths) != 1 {
		t.Fatalf("WorkerDeath count=%d want 1", len(deaths))
	}
	wdDec, err := eventlog.Decode(deaths[0].EventType, deaths[0].Payload)
	if err != nil {
		t.Fatalf("decode WorkerDeath: %v", err)
	}
	wd := wdDec.(eventlog.WorkerDeath)
	if wd.TaskID != "task-42" {
		t.Fatalf("WorkerDeath.TaskID=%q want task-42 (LastAssignmentFor lookup failed)", wd.TaskID)
	}

	if !waitForCondition(t, 2*time.Second, func() bool {
		return len(queryEventsByType(t, fx.evlog, "s-test-max-scope", eventlog.EvtWorkerRedispatched)) >= 1
	}) {
		t.Fatal("WorkerRedispatched never emitted")
	}
	redis := queryEventsByType(t, fx.evlog, "s-test-max-scope", eventlog.EvtWorkerRedispatched)
	if len(redis) != 1 {
		t.Fatalf("WorkerRedispatched count=%d want 1", len(redis))
	}
	wrDec, err := eventlog.Decode(redis[0].EventType, redis[0].Payload)
	if err != nil {
		t.Fatalf("decode WorkerRedispatched: %v", err)
	}
	wr := wrDec.(eventlog.WorkerRedispatched)
	if wr.WorkerID != "w-stale" {
		t.Fatalf("Redispatched.WorkerID=%q want w-stale", wr.WorkerID)
	}
	if wr.TaskID != "task-42" {
		t.Fatalf("Redispatched.TaskID=%q want task-42", wr.TaskID)
	}
	if wr.Class != FailureTransientInfra.String() {
		t.Fatalf("Redispatched.Class=%q want %q", wr.Class, FailureTransientInfra.String())
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("monitor did not exit within 1s after cancel")
	}
}

func TestHeartbeatMonitor_ProbeError_NoEmissions(t *testing.T) {
	defer goleak.VerifyNone(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fx := newRecoveryFixture(t, "max-scope")
	fc := fakeFromEng(t, fx)
	probe := &fakeProbeAlwaysErr{err: errors.New("probe down")}
	mon, err := NewHeartbeatMonitor(HeartbeatConfig{
		Engine:   fx.eng,
		Probe:    probe,
		Interval: 30 * time.Second,
		Timeout:  60 * time.Second,
		Clock:    fc,
	})
	if err != nil {
		t.Fatalf("NewHeartbeatMonitor: %v", err)
	}
	done := make(chan struct{})
	go func() {
		mon.Run(ctx)
		close(done)
	}()

	probeCalls := func() int { return int(probe.callCount.Load()) }
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && probeCalls() < 2 {
		fc.Advance(31 * time.Second)
		fc.BlockUntilCondition(func() bool { return probeCalls() >= 2 }, 50*time.Millisecond)
	}
	if probeCalls() < 2 {
		t.Fatalf("probe call count=%d want >=2 (loop should survive errors)", probeCalls())
	}

	recs, err := fx.evlog.Query(ctx, "s-test-max-scope", 0)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	for _, r := range recs {
		if r.EventType == eventlog.EvtWorkerDeath || r.EventType == eventlog.EvtWorkerRedispatched {
			t.Fatalf("unexpected event after probe error: %+v", r)
		}
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("monitor did not exit within 1s after cancel")
	}
}

func TestNewHeartbeatMonitor_NilEngine(t *testing.T) {
	probe := &fakeProbe{alive: map[string]time.Time{}}
	_, err := NewHeartbeatMonitor(HeartbeatConfig{
		Engine: nil,
		Probe:  probe,
	})
	if err == nil {
		t.Fatal("nil engine: want wrapped ErrInvalidConfig, got nil")
	}
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("err=%v want wrapped ErrInvalidConfig", err)
	}
}

func TestNewHeartbeatMonitor_NilProbe(t *testing.T) {
	fx := newRecoveryFixture(t, "max-scope")
	_, err := NewHeartbeatMonitor(HeartbeatConfig{
		Engine: fx.eng,
		Probe:  nil,
	})
	if err == nil {
		t.Fatal("nil probe: want wrapped ErrInvalidConfig, got nil")
	}
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("err=%v want wrapped ErrInvalidConfig", err)
	}
}

func TestNewHeartbeatMonitor_DefaultsApplied(t *testing.T) {
	fx := newRecoveryFixture(t, "max-scope")
	probe := &fakeProbe{alive: map[string]time.Time{}}
	mon, err := NewHeartbeatMonitor(HeartbeatConfig{
		Engine: fx.eng,
		Probe:  probe,
	})
	if err != nil {
		t.Fatalf("NewHeartbeatMonitor: %v", err)
	}
	if mon.interval != 30*time.Second {
		t.Errorf("interval=%v want 30s default", mon.interval)
	}
	if mon.timeout != 60*time.Second {
		t.Errorf("timeout=%v want 60s (2*interval) default", mon.timeout)
	}
	if _, ok := mon.clk.(clock.Real); !ok {

		if _, okp := mon.clk.(*clock.Real); !okp {
			t.Errorf("clock=%T want clock.Real (value or pointer) default", mon.clk)
		}
	}
}

func TestNewHeartbeatMonitor_PartialDefaults(t *testing.T) {
	fx := newRecoveryFixture(t, "max-scope")
	probe := &fakeProbe{alive: map[string]time.Time{}}
	mon, err := NewHeartbeatMonitor(HeartbeatConfig{
		Engine:   fx.eng,
		Probe:    probe,
		Interval: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewHeartbeatMonitor: %v", err)
	}
	if mon.interval != 10*time.Second {
		t.Errorf("interval=%v want 10s", mon.interval)
	}
	if mon.timeout != 20*time.Second {
		t.Errorf("timeout=%v want 20s (2*interval) default", mon.timeout)
	}
}
