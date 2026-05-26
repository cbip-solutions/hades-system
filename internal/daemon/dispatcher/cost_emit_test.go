// internal/daemon/dispatcher/cost_emit_test.go
//
// External-package tests for AsyncEmitter (Plan 3 Phase B-5). Verifies:
//   - Emit is non-blocking even when the sink stalls (the response path
//     must never wait on the cost ledger).
//   - Buffer-full events are dropped (counter incremented) rather than
//     blocking the caller.
//   - Default buffer size kicks in when bufferSize <= 0.
//   - Close is idempotent (safe to call twice; no panic).
//   - Emit after Close is a silent no-op (no panic on send-to-closed-chan).
//   - Sink errors do NOT terminate the worker; subsequent events still
//     reach the sink.
//   - Flush blocks until every event Emitted before the call has been
//     delivered to the sink.
//   - All of the above run clean under the race detector.

package dispatcher_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/daemon/dispatcher"
	"github.com/cbip-solutions/hades-system/internal/providers"
)

type capturingSink struct {
	mu           sync.Mutex
	events       []dispatcher.CostEvent
	delay        time.Duration
	errOn        map[int]error
	calls        atomic.Int64
	gateOnce     sync.Once
	gate         chan struct{}
	beforeInsert func(evt dispatcher.CostEvent) error
}

func newCapturingSink() *capturingSink {
	return &capturingSink{errOn: map[int]error{}}
}

func (c *capturingSink) Insert(_ context.Context, evt dispatcher.CostEvent) error {
	idx := int(c.calls.Add(1))
	if c.gate != nil {
		<-c.gate
	}
	if c.delay > 0 {
		time.Sleep(c.delay)
	}

	if c.beforeInsert != nil {
		if err := c.beforeInsert(evt); err != nil {
			return err
		}
	}
	c.mu.Lock()
	c.events = append(c.events, evt)
	c.mu.Unlock()
	if err, ok := c.errOn[idx]; ok {
		return err
	}
	return nil
}

func (c *capturingSink) snapshot() []dispatcher.CostEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]dispatcher.CostEvent, len(c.events))
	copy(out, c.events)
	return out
}

func (c *capturingSink) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.events)
}

func (c *capturingSink) installGate() (release func()) {
	c.gate = make(chan struct{})
	return func() {
		c.gateOnce.Do(func() { close(c.gate) })
	}
}

func mkEvent(project string) dispatcher.CostEvent {
	return dispatcher.CostEvent{
		Timestamp:    time.Now(),
		Project:      project,
		SessionID:    "sess-test",
		Profile:      "default",
		Tier:         providers.TierInHouse,
		Model:        "claude-sonnet-4-5",
		InputTokens:  10,
		OutputTokens: 5,
		Status:       200,
		LatencyMS:    42,
	}
}

func TestAsyncEmitterDoesNotBlock(t *testing.T) {
	t.Parallel()
	sink := newCapturingSink()
	release := sink.installGate()

	emitter := dispatcher.NewAsyncEmitter(sink, 16)

	defer emitter.Close()
	defer release()

	deadline := time.Now().Add(200 * time.Millisecond)
	for i := 0; i < 16; i++ {
		if time.Now().After(deadline) {
			t.Fatalf("Emit blocked the caller; iteration %d", i)
		}
		if err := emitter.Emit(context.Background(), mkEvent("p")); err != nil {
			t.Fatalf("Emit returned error: %v", err)
		}
	}
}

func TestAsyncEmitterDropsWhenBufferFull(t *testing.T) {
	t.Parallel()
	sink := newCapturingSink()
	release := sink.installGate()

	emitter := dispatcher.NewAsyncEmitter(sink, 1)
	defer emitter.Close()
	defer release()

	const total = 100
	for i := 0; i < total; i++ {
		_ = emitter.Emit(context.Background(), mkEvent("p"))
	}

	dropped := emitter.DroppedCount()
	if dropped < 1 {
		t.Fatalf("expected at least one drop, got %d", dropped)
	}

	if int64(total) < dropped {
		t.Fatalf("dropped (%d) cannot exceed total emits (%d)", dropped, total)
	}
}

func TestAsyncEmitterDefaultBufferSizeOnZero(t *testing.T) {
	t.Parallel()
	for _, bs := range []int{0, -1, -100} {
		bs := bs
		t.Run("", func(t *testing.T) {
			t.Parallel()
			sink := newCapturingSink()
			release := sink.installGate()

			emitter := dispatcher.NewAsyncEmitter(sink, bs)
			defer emitter.Close()
			defer release()

			for i := 0; i < 64; i++ {
				_ = emitter.Emit(context.Background(), mkEvent("p"))
			}
			if got := emitter.DroppedCount(); got != 0 {
				t.Fatalf("with default buffer (64), 64 emits should not drop; got %d", got)
			}

			for i := 0; i < 200; i++ {
				_ = emitter.Emit(context.Background(), mkEvent("p"))
			}
			if got := emitter.DroppedCount(); got < 1 {
				t.Fatalf("expected drops on overflow; got %d", got)
			}
		})
	}
}

func TestAsyncEmitterCloseIdempotent(t *testing.T) {
	t.Parallel()
	sink := newCapturingSink()
	emitter := dispatcher.NewAsyncEmitter(sink, 4)

	emitter.Close()

	done := make(chan struct{})
	go func() {
		emitter.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("second Close blocked")
	}
}

func TestAsyncEmitterEmitAfterCloseIsNoOp(t *testing.T) {
	t.Parallel()
	sink := newCapturingSink()
	emitter := dispatcher.NewAsyncEmitter(sink, 4)
	emitter.Close()

	if err := emitter.Emit(context.Background(), mkEvent("p")); err != nil {
		t.Fatalf("post-Close Emit returned error: %v", err)
	}
	if c := sink.count(); c != 0 {
		t.Fatalf("post-Close emission reached sink: %d events", c)
	}
}

func TestAsyncEmitterConcurrentCloseAndEmit(t *testing.T) {
	t.Parallel()
	sink := newCapturingSink()
	emitter := dispatcher.NewAsyncEmitter(sink, 8)

	const producers = 16
	var wg sync.WaitGroup
	wg.Add(producers)
	for i := 0; i < producers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				_ = emitter.Emit(context.Background(), mkEvent("p"))
			}
		}()
	}

	time.Sleep(2 * time.Millisecond)
	emitter.Close()
	wg.Wait()
}

func TestAsyncEmitterSinkErrorDoesNotKillWorker(t *testing.T) {
	t.Parallel()
	sink := newCapturingSink()
	sink.errOn[1] = errors.New("ledger temporary failure")

	emitter := dispatcher.NewAsyncEmitter(sink, 8)

	if err := emitter.Emit(context.Background(), mkEvent("first")); err != nil {
		t.Fatalf("emit 1 err: %v", err)
	}
	if err := emitter.Emit(context.Background(), mkEvent("second")); err != nil {
		t.Fatalf("emit 2 err: %v", err)
	}

	emitter.Flush()
	emitter.Close()

	got := sink.snapshot()
	if len(got) != 2 {
		t.Fatalf("expected 2 events delivered (sink error must not kill worker); got %d", len(got))
	}
	if got[0].Project != "first" || got[1].Project != "second" {
		t.Fatalf("ordering corrupted: %+v", got)
	}
}

func TestAsyncEmitterFlushWaitsForDelivery(t *testing.T) {
	t.Parallel()
	sink := newCapturingSink()
	sink.delay = 5 * time.Millisecond

	emitter := dispatcher.NewAsyncEmitter(sink, 32)
	defer emitter.Close()

	const n = 20
	for i := 0; i < n; i++ {
		if err := emitter.Emit(context.Background(), mkEvent("p")); err != nil {
			t.Fatalf("emit %d: %v", i, err)
		}
	}

	emitter.Flush()

	if got := sink.count(); got != n {
		t.Fatalf("after Flush, sink should have all %d events; got %d", n, got)
	}
}

func TestAsyncEmitterFlushConcurrent(t *testing.T) {
	t.Parallel()
	sink := newCapturingSink()
	sink.delay = 1 * time.Millisecond

	const emits = 50

	emitter := dispatcher.NewAsyncEmitter(sink, emits*2)
	defer emitter.Close()

	for i := 0; i < emits; i++ {
		if err := emitter.Emit(context.Background(), mkEvent("p")); err != nil {
			t.Fatalf("emit %d: %v", i, err)
		}
	}
	if d := emitter.DroppedCount(); d != 0 {
		t.Fatalf("preflight: unexpected drops=%d (buffer too small for test)", d)
	}

	const flushers = 8
	var wg sync.WaitGroup
	wg.Add(flushers)
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	for i := 0; i < flushers; i++ {
		go func() {
			defer wg.Done()
			emitter.Flush()
		}()
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent Flush deadlocked")
	}

	if got := sink.count(); got != emits {
		t.Fatalf("after concurrent Flush, expected %d events; got %d", emits, got)
	}
}

func TestAsyncEmitterFlushAfterClose(t *testing.T) {
	t.Parallel()
	sink := newCapturingSink()
	emitter := dispatcher.NewAsyncEmitter(sink, 4)
	emitter.Close()

	done := make(chan struct{})
	go func() {
		emitter.Flush()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Flush after Close blocked")
	}
}

func TestAsyncEmitterImplementsCostEmitter(t *testing.T) {
	t.Parallel()
	var _ dispatcher.CostEmitter = (*dispatcher.AsyncEmitter)(nil)
}

func TestNewAsyncEmitterNilSinkPanics(t *testing.T) {
	t.Parallel()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on nil sink")
		}
		msg, ok := r.(string)
		if !ok || !strings.Contains(msg, "sink is required") {
			t.Fatalf("expected fail-fast panic message; got %v", r)
		}
	}()
	_ = dispatcher.NewAsyncEmitter(nil, 8)
}

// TestAsyncEmitterSinkPanicDoesNotKillWorker pins the contract that a
// panic in sink.Insert is recovered by the worker, allowing subsequent
// events to land in the sink. Defense-in-depth: production sinks (e.g.
// SQLite via dispatcheradapter) CAN panic on rare driver edge cases;
// the worker MUST self-heal.
func TestAsyncEmitterSinkPanicDoesNotKillWorker(t *testing.T) {
	t.Parallel()
	var calls atomic.Int64
	sink := &capturingSink{
		errOn: map[int]error{},
		beforeInsert: func(evt dispatcher.CostEvent) error {
			n := calls.Add(1)
			if n == 1 {
				panic("simulated sink driver panic")
			}
			return nil
		},
	}
	emitter := dispatcher.NewAsyncEmitter(sink, 16)
	defer emitter.Close()

	if err := emitter.Emit(context.Background(), dispatcher.CostEvent{Project: "p", Tier: providers.TierInHouse}); err != nil {
		t.Fatalf("Emit 1: %v", err)
	}
	if err := emitter.Emit(context.Background(), dispatcher.CostEvent{Project: "p", Tier: providers.TierOpenClaude}); err != nil {
		t.Fatalf("Emit 2: %v", err)
	}
	emitter.Flush()

	if got := calls.Load(); got != 2 {
		t.Errorf("sink.Insert called %d times, want 2", got)
	}
	delivered := sink.snapshot()
	if len(delivered) != 1 {
		t.Errorf("delivered events = %d, want 1 (first call panicked)", len(delivered))
	} else if delivered[0].Tier != providers.TierOpenClaude {
		t.Errorf("delivered[0].Tier = %v, want TierOpenClaude (the non-panicking event)", delivered[0].Tier)
	}
}

// TestAsyncEmitter_PreservesProvider pins the Plan 16 Phase B C8 contract:
// dispatcher.CostEvent.Provider (Backend.Name() set by dispatcher.attempt())
// MUST survive the asynchronous AsyncEmitter → CostSink hop. Without this
// pin a future "field-aware" optimisation in AsyncEmitter (e.g. partial
// struct copy) could silently zero the Provider and break per-provider
// attribution end-to-end (cost_ledger.provider would land empty).
func TestAsyncEmitter_PreservesProvider(t *testing.T) {
	t.Parallel()
	sink := newCapturingSink()
	e := dispatcher.NewAsyncEmitter(sink, 16)
	defer e.Close()

	const want = "deepseek-direct"
	if err := e.Emit(context.Background(), dispatcher.CostEvent{
		Project:  "internal-platform-x",
		Profile:  "worker-code",
		Provider: want,
		Tier:     providers.TierGenericOpenAICompat,
		Model:    "deepseek-chat",
		Status:   200,
	}); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	e.Flush()

	got := sink.snapshot()
	if len(got) != 1 {
		t.Fatalf("got %d events, want 1", len(got))
	}
	if got[0].Provider != want {
		t.Errorf("CostEvent.Provider = %q, want %q", got[0].Provider, want)
	}
}

func TestAsyncEmitterFlushRacingClose(t *testing.T) {
	t.Parallel()

	sink := newCapturingSink()
	sink.delay = 5 * time.Millisecond

	emitter := dispatcher.NewAsyncEmitter(sink, 4)
	for i := 0; i < 4; i++ {
		_ = emitter.Emit(context.Background(), mkEvent("p"))
	}

	flushDone := make(chan struct{})
	go func() {
		emitter.Flush()
		close(flushDone)
	}()

	time.Sleep(2 * time.Millisecond)
	emitter.Close()

	select {
	case <-flushDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Flush deadlocked when racing Close")
	}
}
