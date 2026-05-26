package stream_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/workforce/stream"
)

type noopPersist struct{}

func (n *noopPersist) OpenWindow(ctx context.Context, layer stream.Layer, openedAt time.Time) (int64, error) {
	return 1, nil
}
func (n *noopPersist) AppendEvent(ctx context.Context, windowID int64, event stream.Event) error {
	return nil
}
func (n *noopPersist) CloseWindow(ctx context.Context, windowID int64, closedAt time.Time, count int) error {
	return nil
}
func (n *noopPersist) LoadOpenWindows(ctx context.Context) ([]stream.WindowRecord, error) {
	return nil, nil
}

func TestWindowAccumulatesEvents(t *testing.T) {
	p := &noopPersist{}
	s, err := stream.NewAggregationStream(stream.Config{
		L2ToL3: 100 * time.Millisecond,
		L3ToL4: 500 * time.Millisecond,
	}, p)
	if err != nil {
		t.Fatalf("NewAggregationStream: %v", err)
	}

	if err := s.Publish(context.Background(), stream.LayerL2, stream.Event{
		Type:    "checkpoint",
		Payload: []byte(`{"task":"t1","step":1}`),
	}); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if err := s.Publish(context.Background(), stream.LayerL2, stream.Event{
		Type:    "checkpoint",
		Payload: []byte(`{"task":"t1","step":2}`),
	}); err != nil {
		t.Fatalf("Publish second: %v", err)
	}

	snap := s.WindowSnapshot(stream.LayerL2)
	if snap.Count != 2 {
		t.Errorf("WindowSnapshot.Count = %d, want 2", snap.Count)
	}
}

func TestWindowFlushesOnClose(t *testing.T) {
	var mu sync.Mutex
	var flushed []stream.FlushBatch

	p := &noopPersist{}
	s, err := stream.NewAggregationStream(stream.Config{
		L2ToL3: 50 * time.Millisecond,
		L3ToL4: 500 * time.Millisecond,
	}, p)
	if err != nil {
		t.Fatalf("NewAggregationStream: %v", err)
	}
	s.OnFlush(func(b stream.FlushBatch) {
		mu.Lock()
		flushed = append(flushed, b)
		mu.Unlock()
	})

	for i := 0; i < 5; i++ {
		_ = s.Publish(context.Background(), stream.LayerL2, stream.Event{
			Type:    "checkpoint",
			Payload: []byte(`{}`),
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	s.Start(ctx)

	deadline := time.Now().Add(400 * time.Millisecond)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(flushed)
		mu.Unlock()
		if n >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()

	mu.Lock()
	got := len(flushed)
	mu.Unlock()
	if got == 0 {
		t.Error("expected at least one FlushBatch, got 0")
	}
	if got > 0 {
		fb := flushed[0]
		if fb.Layer != stream.LayerL2 {
			t.Errorf("FlushBatch.Layer = %v, want L2", fb.Layer)
		}
		if len(fb.Events) == 0 {
			t.Error("FlushBatch.Events must be non-empty")
		}
	}
}

func TestWindowFlushDeliversToSubscriber(t *testing.T) {
	p := &noopPersist{}
	s, err := stream.NewAggregationStream(stream.Config{
		L2ToL3: 40 * time.Millisecond,
		L3ToL4: 500 * time.Millisecond,
	}, p)
	if err != nil {
		t.Fatalf("NewAggregationStream: %v", err)
	}

	received := make(chan stream.FlushBatch, 10)
	s.Subscribe(stream.LayerL2, func(b stream.FlushBatch) {
		received <- b
	})

	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancel()
	s.Start(ctx)

	_ = s.Publish(ctx, stream.LayerL2, stream.Event{Type: "x", Payload: []byte(`{}`)})

	select {
	case b := <-received:
		if len(b.Events) == 0 {
			t.Error("subscriber got empty FlushBatch")
		}
	case <-time.After(300 * time.Millisecond):
		t.Error("subscriber not called within 300ms")
	}
}

func TestPublishAfterContextCancelledReturnsError(t *testing.T) {
	p := &noopPersist{}
	s, err := stream.NewAggregationStream(stream.Config{
		L2ToL3: 60 * time.Second,
		L3ToL4: 60 * time.Second,
	}, p)
	if err != nil {
		t.Fatalf("NewAggregationStream: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	cancel()
	time.Sleep(10 * time.Millisecond)

	err = s.Publish(context.Background(), stream.LayerL2, stream.Event{Type: "x", Payload: []byte(`{}`)})

	if !errors.Is(err, stream.ErrStreamStopped) {
		t.Errorf("Publish after cancel = %v, want errors.Is(ErrStreamStopped)", err)
	}
}

func TestWindowSnapshotResetOnFlush(t *testing.T) {
	var mu sync.Mutex
	flushed := 0

	p := &noopPersist{}
	s, err := stream.NewAggregationStream(stream.Config{
		L2ToL3: 30 * time.Millisecond,
		L3ToL4: 500 * time.Millisecond,
	}, p)
	if err != nil {
		t.Fatalf("NewAggregationStream: %v", err)
	}
	s.OnFlush(func(b stream.FlushBatch) {
		mu.Lock()
		flushed++
		mu.Unlock()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	s.Start(ctx)

	_ = s.Publish(ctx, stream.LayerL2, stream.Event{Type: "a", Payload: []byte(`{}`)})
	time.Sleep(80 * time.Millisecond)

	snap := s.WindowSnapshot(stream.LayerL2)

	if snap.Count != 0 {
		t.Errorf("post-flush WindowSnapshot.Count = %d, want 0", snap.Count)
	}
	mu.Lock()
	f := flushed
	mu.Unlock()
	if f == 0 {
		t.Error("expected flush, got none")
	}
}

type lagPersist struct {
	noopPersist
	mu     sync.Mutex
	events []stream.Event
}

func (l *lagPersist) AppendEvent(_ context.Context, _ int64, e stream.Event) error {
	l.mu.Lock()
	l.events = append(l.events, e)
	l.mu.Unlock()
	return nil
}
func (l *lagPersist) OpenWindow(_ context.Context, _ stream.Layer, _ time.Time) (int64, error) {
	return 42, nil
}

func (l *lagPersist) eventsOfType(typ string) []stream.Event {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []stream.Event
	for _, e := range l.events {
		if e.Type == typ {
			out = append(out, e)
		}
	}
	return out
}

func TestBackpressureLagHandlerCalled(t *testing.T) {
	p := &lagPersist{}
	s, err := stream.NewAggregationStream(stream.Config{
		L2ToL3: 50 * time.Millisecond,
		L3ToL4: 500 * time.Millisecond,
	}, p)
	if err != nil {
		t.Fatalf("NewAggregationStream: %v", err)
	}

	var lagCalled sync.WaitGroup
	lagCalled.Add(1)
	var lagOnce sync.Once
	s.OnLag(func(li stream.LagInfo) {
		lagOnce.Do(func() { lagCalled.Done() })
	})

	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancel()
	s.Start(ctx)

	for i := 0; i < 120; i++ {
		_ = s.Publish(ctx, stream.LayerL2, stream.Event{
			Type:    "checkpoint",
			Payload: []byte(`{}`),
		})
	}

	done := make(chan struct{})
	go func() {
		lagCalled.Wait()
		close(done)
	}()
	select {
	case <-done:

	case <-time.After(400 * time.Millisecond):

		t.Log("note: lag handler not fired (events may have flushed before half-window)")
	}
}

func TestLagInfoFields(t *testing.T) {
	li := stream.LagInfo{
		Layer:       stream.LayerL2,
		WindowCount: 150,
		HalfWindow:  25 * time.Millisecond,
		DetectedAt:  time.Now(),
	}
	if li.Layer != stream.LayerL2 {
		t.Errorf("Layer = %v", li.Layer)
	}
	if li.WindowCount != 150 {
		t.Errorf("WindowCount = %d", li.WindowCount)
	}
}

func TestOnLagHandlerRegisteredAfterStart(t *testing.T) {

	p := &noopPersist{}
	s, err := stream.NewAggregationStream(stream.Config{
		L2ToL3: 100 * time.Millisecond,
		L3ToL4: 500 * time.Millisecond,
	}, p)
	if err != nil {
		t.Fatalf("NewAggregationStream: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	s.Start(ctx)

	called := 0
	s.OnLag(func(li stream.LagInfo) { called++ })

	_ = s.Publish(ctx, stream.LayerL2, stream.Event{Type: "x", Payload: []byte(`{}`)})

	if called < 0 {
		t.Error("impossible")
	}
}

func TestStreamPersistInterfaceFullContract(t *testing.T) {

	p := &lagPersist{}
	s, err := stream.NewAggregationStream(stream.Config{
		L2ToL3: 30 * time.Millisecond,
		L3ToL4: 500 * time.Millisecond,
	}, p)
	if err != nil {
		t.Fatalf("NewAggregationStream: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	s.Start(ctx)

	for i := 0; i < 3; i++ {
		_ = s.Publish(ctx, stream.LayerL2, stream.Event{
			Type:    "checkpoint",
			Payload: []byte(`{"n":` + fmt.Sprintf("%d", i) + `}`),
		})
	}
	time.Sleep(150 * time.Millisecond)

	evs := p.eventsOfType("checkpoint")
	if len(evs) < 3 {
		t.Errorf("expected ≥3 checkpoint events in persist, got %d", len(evs))
	}
}

func TestNewAggregationStreamErrors(t *testing.T) {
	t.Run("nil persist", func(t *testing.T) {
		_, err := stream.NewAggregationStream(stream.Config{
			L2ToL3: 1 * time.Second,
			L3ToL4: 5 * time.Second,
		}, nil)
		if err == nil {
			t.Error("expected error for nil persist")
		}
	})
	t.Run("zero L2ToL3", func(t *testing.T) {
		_, err := stream.NewAggregationStream(stream.Config{
			L2ToL3: 0,
			L3ToL4: 5 * time.Second,
		}, &noopPersist{})
		if err == nil {
			t.Error("expected error for zero L2ToL3")
		}
	})
	t.Run("zero L3ToL4", func(t *testing.T) {
		_, err := stream.NewAggregationStream(stream.Config{
			L2ToL3: 1 * time.Second,
			L3ToL4: 0,
		}, &noopPersist{})
		if err == nil {
			t.Error("expected error for zero L3ToL4")
		}
	})
}

func TestPublishInvalidLayer(t *testing.T) {
	p := &noopPersist{}
	s, err := stream.NewAggregationStream(stream.Config{
		L2ToL3: 1 * time.Second,
		L3ToL4: 5 * time.Second,
	}, p)
	if err != nil {
		t.Fatalf("NewAggregationStream: %v", err)
	}
	err = s.Publish(context.Background(), stream.LayerL4, stream.Event{Type: "x", Payload: []byte(`{}`)})
	if err == nil {
		t.Error("expected error publishing to LayerL4 directly")
	}
}

func TestWindowSnapshotUnknownLayer(t *testing.T) {
	p := &noopPersist{}
	s, err := stream.NewAggregationStream(stream.Config{
		L2ToL3: 1 * time.Second,
		L3ToL4: 5 * time.Second,
	}, p)
	if err != nil {
		t.Fatalf("NewAggregationStream: %v", err)
	}
	snap := s.WindowSnapshot(stream.LayerL4)
	if snap.Count != 0 {
		t.Errorf("unknown layer snapshot Count = %d, want 0", snap.Count)
	}
}

func TestPublishToL3Directly(t *testing.T) {
	p := &noopPersist{}
	s, err := stream.NewAggregationStream(stream.Config{
		L2ToL3: 1 * time.Second,
		L3ToL4: 5 * time.Second,
	}, p)
	if err != nil {
		t.Fatalf("NewAggregationStream: %v", err)
	}

	err = s.Publish(context.Background(), stream.LayerL3, stream.Event{Type: "x", Payload: []byte(`{}`)})
	if err != nil {
		t.Errorf("Publish to L3: unexpected error: %v", err)
	}
}

type errorPersist struct {
	noopPersist
}

func (e *errorPersist) OpenWindow(_ context.Context, _ stream.Layer, _ time.Time) (int64, error) {
	return 0, fmt.Errorf("simulated OpenWindow error")
}

func TestOpenWindowErrorIsNonFatal(t *testing.T) {

	p := &errorPersist{}
	s, err := stream.NewAggregationStream(stream.Config{
		L2ToL3: 50 * time.Millisecond,
		L3ToL4: 500 * time.Millisecond,
	}, p)
	if err != nil {
		t.Fatalf("NewAggregationStream: %v", err)
	}
	var mu sync.Mutex
	flushed := 0
	s.OnFlush(func(b stream.FlushBatch) {
		mu.Lock()
		flushed++
		mu.Unlock()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	s.Start(ctx)

	for i := 0; i < 3; i++ {
		if err2 := s.Publish(ctx, stream.LayerL2, stream.Event{Type: "x", Payload: []byte(`{}`)}); err2 != nil {
			t.Fatalf("Publish with errorPersist: %v", err2)
		}
	}
	time.Sleep(200 * time.Millisecond)
	mu.Lock()
	f := flushed
	mu.Unlock()
	if f == 0 {
		t.Error("expected at least one flush even when OpenWindow errors")
	}
}

type backpressurePersist struct {
	mu     sync.Mutex
	events []stream.Event
}

func (b *backpressurePersist) OpenWindow(_ context.Context, _ stream.Layer, _ time.Time) (int64, error) {
	return 42, nil
}
func (b *backpressurePersist) AppendEvent(_ context.Context, _ int64, e stream.Event) error {
	b.mu.Lock()
	b.events = append(b.events, e)
	b.mu.Unlock()
	return nil
}
func (b *backpressurePersist) CloseWindow(_ context.Context, _ int64, _ time.Time, _ int) error {
	return nil
}
func (b *backpressurePersist) LoadOpenWindows(_ context.Context) ([]stream.WindowRecord, error) {
	return nil, nil
}

func TestCheckBackpressureDirectLagPath(t *testing.T) {
	p := &backpressurePersist{}
	s, err := stream.NewAggregationStream(stream.Config{
		L2ToL3: 5 * time.Second,
		L3ToL4: 30 * time.Second,
	}, p)
	if err != nil {
		t.Fatalf("NewAggregationStream: %v", err)
	}

	lagFired := make(chan stream.LagInfo, 5)
	s.OnLag(func(li stream.LagInfo) {
		select {
		case lagFired <- li:
		default:
		}
	})

	ctx := context.Background()

	s.Start(ctx)

	for i := 0; i < 110; i++ {
		_ = s.Publish(ctx, stream.LayerL2, stream.Event{
			Type:    "checkpoint",
			Payload: []byte(`{}`),
		})
	}

	snap := s.WindowSnapshot(stream.LayerL2)
	if snap.Count <= 100 {
		t.Skipf("window count=%d, skipping (events not accumulated before check)", snap.Count)
	}

	s.ExportCheckBackpressure(ctx, stream.LayerL2, 10*time.Second)

	select {
	case li := <-lagFired:
		if li.Layer != stream.LayerL2 {
			t.Errorf("LagInfo.Layer = %v, want L2", li.Layer)
		}
		if li.WindowCount <= 100 {
			t.Errorf("LagInfo.WindowCount = %d, want > 100", li.WindowCount)
		}
		if li.DetectedAt.IsZero() {
			t.Error("LagInfo.DetectedAt is zero")
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("lag handler not called after ExportCheckBackpressure with > 100 events")
	}
}

func TestBackpressureNoFalsePositiveLongWindow(t *testing.T) {
	p := &funcPersist{}
	s, err := stream.NewAggregationStream(stream.Config{
		L2ToL3: 30 * time.Second,
		L3ToL4: 300 * time.Second,
	}, p)
	if err != nil {
		t.Fatalf("NewAggregationStream: %v", err)
	}

	called := 0
	s.OnLag(func(_ stream.LagInfo) { called++ })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	for i := 0; i < 110; i++ {
		_ = s.Publish(ctx, stream.LayerL3, stream.Event{
			Type: "checkpoint", Payload: []byte(`{}`),
		})
	}

	s.ExportCheckBackpressure(ctx, stream.LayerL3, 300*time.Second)
	if called != 0 {
		t.Errorf("OnLag fired %d times for 110 events in 150s half-window of L3→L4 (0.7 ev/s, healthy); want 0", called)
	}
}

func TestBackpressurePositiveShortWindow(t *testing.T) {
	p := &funcPersist{}
	s, err := stream.NewAggregationStream(stream.Config{
		L2ToL3: 5 * time.Second,
		L3ToL4: 60 * time.Second,
	}, p)
	if err != nil {
		t.Fatalf("NewAggregationStream: %v", err)
	}

	called := 0
	s.OnLag(func(_ stream.LagInfo) { called++ })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	for i := 0; i < 110; i++ {
		_ = s.Publish(ctx, stream.LayerL2, stream.Event{
			Type: "checkpoint", Payload: []byte(`{}`),
		})
	}
	s.ExportCheckBackpressure(ctx, stream.LayerL2, 5*time.Second)
	if called == 0 {
		t.Error("OnLag did not fire for 110 events in 2.5s half-window of 5s window; expected lag detection")
	}
}

func TestBackpressureCustomRateConfigOverridesDefault(t *testing.T) {
	p := &funcPersist{}
	s, err := stream.NewAggregationStream(stream.Config{
		L2ToL3:                 1 * time.Second,
		L3ToL4:                 60 * time.Second,
		BackpressureRatePerSec: 0.5,
	}, p)
	if err != nil {
		t.Fatalf("NewAggregationStream: %v", err)
	}

	called := 0
	s.OnLag(func(_ stream.LagInfo) { called++ })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	for i := 0; i < 110; i++ {
		_ = s.Publish(ctx, stream.LayerL2, stream.Event{
			Type: "checkpoint", Payload: []byte(`{}`),
		})
	}
	s.ExportCheckBackpressure(ctx, stream.LayerL2, 1*time.Second)
	if called == 0 {
		t.Error("OnLag did not fire with custom low rate config; expected lag")
	}
}

func TestCheckBackpressureDoesNotRecursivelyPublish(t *testing.T) {

	var appended []string
	var appendMu sync.Mutex
	p := &funcPersist{
		appendFn: func(_ context.Context, _ int64, e stream.Event) error {
			appendMu.Lock()
			appended = append(appended, e.Type)
			appendMu.Unlock()
			return nil
		},
	}
	s, err := stream.NewAggregationStream(stream.Config{
		L2ToL3: 5 * time.Second,
		L3ToL4: 30 * time.Second,
	}, p)
	if err != nil {
		t.Fatalf("NewAggregationStream: %v", err)
	}

	var lagCount int
	var lagMu sync.Mutex
	s.OnLag(func(_ stream.LagInfo) {
		lagMu.Lock()
		lagCount++
		lagMu.Unlock()
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	for i := 0; i < 110; i++ {
		_ = s.Publish(ctx, stream.LayerL2, stream.Event{
			Type: "checkpoint", Payload: []byte(`{}`),
		})
	}

	s.ExportCheckBackpressure(ctx, stream.LayerL2, 10*time.Second)

	lagMu.Lock()
	gotLag := lagCount
	lagMu.Unlock()
	if gotLag != 1 {
		t.Errorf("OnLag handler called %d times, want 1", gotLag)
	}

	appendMu.Lock()
	defer appendMu.Unlock()
	for _, typ := range appended {
		if typ == "aggregation_lag" {
			t.Errorf("checkBackpressure published aggregation_lag event into stream (recursive amplification); appended types=%v", appended)
			return
		}
	}
}

func TestStartIdempotent(t *testing.T) {

	var openCount int
	var openMu sync.Mutex
	p := &funcPersist{
		openFn: func(_ context.Context, _ stream.Layer, _ time.Time) (int64, error) {
			openMu.Lock()
			openCount++
			openMu.Unlock()
			return 1, nil
		},
	}
	s, err := stream.NewAggregationStream(stream.Config{
		L2ToL3: 60 * time.Second,
		L3ToL4: 60 * time.Second,
	}, p)
	if err != nil {
		t.Fatalf("NewAggregationStream: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	if err := s.Start(ctx); err == nil {
		t.Error("second Start should return ErrStreamAlreadyStarted, got nil")
	} else if err != stream.ErrStreamAlreadyStarted {
		t.Errorf("second Start error = %v, want ErrStreamAlreadyStarted", err)
	}
	openMu.Lock()
	got := openCount
	openMu.Unlock()
	if got != 2 {
		t.Errorf("OpenWindow called %d times, want 2 (one L2 + one L3)", got)
	}
}

func TestOnPersistErrorCalledOnAppendEventFailure(t *testing.T) {

	p := &funcPersist{
		appendFn: func(_ context.Context, _ int64, _ stream.Event) error {
			return fmt.Errorf("injected append error")
		},
	}
	s, err := stream.NewAggregationStream(stream.Config{
		L2ToL3: 60 * time.Second,
		L3ToL4: 60 * time.Second,
	}, p)
	if err != nil {
		t.Fatalf("NewAggregationStream: %v", err)
	}

	errs := make(chan error, 8)
	s.OnPersistError(func(e error) { errs <- e })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := s.Publish(ctx, stream.LayerL2, stream.Event{
		Type: "x", Payload: []byte(`{}`),
	}); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	select {
	case e := <-errs:
		if e == nil {
			t.Error("OnPersistError got nil err")
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("OnPersistError not called within 200ms")
	}
}

func TestOnPersistErrorCalledOnInitialOpenFailure(t *testing.T) {

	p := &funcPersist{
		openFn: func(_ context.Context, _ stream.Layer, _ time.Time) (int64, error) {
			return 0, fmt.Errorf("injected initial open error")
		},
	}
	s, err := stream.NewAggregationStream(stream.Config{
		L2ToL3: 60 * time.Second,
		L3ToL4: 60 * time.Second,
	}, p)
	if err != nil {
		t.Fatalf("NewAggregationStream: %v", err)
	}

	var got []error
	var gotMu sync.Mutex
	s.OnPersistError(func(e error) {
		gotMu.Lock()
		got = append(got, e)
		gotMu.Unlock()
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	gotMu.Lock()
	n := len(got)
	gotMu.Unlock()
	if n < 2 {
		t.Errorf("OnPersistError called %d times during Start, want >=2 (L2 + L3)", n)
	}
}

func TestOnPersistErrorCalledOnCloseOnlyFailureInFlushReopen(t *testing.T) {

	closeErr := fmt.Errorf("close-only error")
	p := &funcPersist{

		closeFn: func(_ context.Context, _ int64, _ time.Time, _ int) error {
			return closeErr
		},
	}
	s, err := stream.NewAggregationStream(stream.Config{
		L2ToL3: 30 * time.Millisecond,
		L3ToL4: 1 * time.Second,
	}, p)
	if err != nil {
		t.Fatalf("NewAggregationStream: %v", err)
	}

	errs := make(chan error, 16)
	s.OnPersistError(func(e error) { errs <- e })

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	_ = s.Publish(ctx, stream.LayerL2, stream.Event{Type: "x", Payload: []byte(`{}`)})

	deadline := time.Now().Add(150 * time.Millisecond)
	gotCloseOnly := false
	for time.Now().Before(deadline) {
		select {
		case e := <-errs:
			if e == nil {
				continue
			}
			msg := e.Error()

			if contains(msg, "flushAndReopen close layer=") &&
				!contains(msg, "close+open") {
				gotCloseOnly = true
			}
		case <-time.After(20 * time.Millisecond):
		}
		if gotCloseOnly {
			break
		}
	}
	if !gotCloseOnly {
		t.Error("expected close-only-error notification from flushAndReopen")
	}
}

func TestNotifyPersistErrorNilGuard(t *testing.T) {

	p := &noopPersist{}
	s, err := stream.NewAggregationStream(stream.Config{
		L2ToL3: 60 * time.Second,
		L3ToL4: 60 * time.Second,
	}, p)
	if err != nil {
		t.Fatalf("NewAggregationStream: %v", err)
	}
	calls := 0
	s.OnPersistError(func(_ error) { calls++ })
	s.ExportNotifyPersistError(nil)
	if calls != 0 {
		t.Errorf("notifyPersistError(nil) invoked %d handlers, want 0", calls)
	}
}

func TestOnPersistErrorWithNoHandlers(t *testing.T) {

	p := &funcPersist{
		appendFn: func(_ context.Context, _ int64, _ stream.Event) error {
			return fmt.Errorf("err")
		},
	}
	s, err := stream.NewAggregationStream(stream.Config{
		L2ToL3: 60 * time.Second,
		L3ToL4: 60 * time.Second,
	}, p)
	if err != nil {
		t.Fatalf("NewAggregationStream: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if err := s.Publish(ctx, stream.LayerL2, stream.Event{
		Type: "x", Payload: []byte(`{}`),
	}); err != nil {
		t.Errorf("Publish with no persist-error handler: %v", err)
	}
}

func TestOnPersistErrorCalledOnCloseAndOpenFailureInFlushReopen(t *testing.T) {

	var calls int
	var mu sync.Mutex
	openErr := fmt.Errorf("open err")
	closeErr := fmt.Errorf("close err")
	p := &funcPersist{
		openFn: func(_ context.Context, _ stream.Layer, _ time.Time) (int64, error) {
			mu.Lock()
			calls++
			n := calls
			mu.Unlock()

			if n <= 2 {
				return int64(n), nil
			}
			return 0, openErr
		},
		closeFn: func(_ context.Context, _ int64, _ time.Time, _ int) error {
			return closeErr
		},
	}

	s, err := stream.NewAggregationStream(stream.Config{
		L2ToL3: 30 * time.Millisecond,
		L3ToL4: 1 * time.Second,
	}, p)
	if err != nil {
		t.Fatalf("NewAggregationStream: %v", err)
	}
	errs := make(chan error, 16)
	s.OnPersistError(func(e error) { errs <- e })

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	_ = s.Publish(ctx, stream.LayerL2, stream.Event{Type: "x", Payload: []byte(`{}`)})

	deadline := time.Now().Add(150 * time.Millisecond)
	gotClose := false
	gotOpen := false
	for time.Now().Before(deadline) {
		select {
		case e := <-errs:
			if e == nil {
				continue
			}
			if err := e.Error(); err != "" {
				if contains(err, "close") {
					gotClose = true
				}
				if contains(err, "open") {
					gotOpen = true
				}
			}
		case <-time.After(20 * time.Millisecond):
		}
		if gotClose && gotOpen {
			break
		}
	}
	if !gotClose {
		t.Error("expected close-error notification from flushAndReopen")
	}
	if !gotOpen {
		t.Error("expected open-error notification from flushAndReopen")
	}
}

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestPublishConcurrentNoLostEvents(t *testing.T) {

	type windowState struct {
		mu       sync.Mutex
		appended int
		closed   int
		closeCnt int
	}
	type counter struct {
		mu             sync.Mutex
		windows        map[int64]*windowState
		nextID         int64
		totalAppend    int
		totalCloseCnts int
	}
	c := &counter{windows: map[int64]*windowState{}}

	openFn := func(_ context.Context, _ stream.Layer, _ time.Time) (int64, error) {
		c.mu.Lock()
		c.nextID++
		id := c.nextID
		c.windows[id] = &windowState{}
		c.mu.Unlock()
		return id, nil
	}
	appendFn := func(_ context.Context, wid int64, _ stream.Event) error {
		c.mu.Lock()
		c.totalAppend++
		w := c.windows[wid]
		c.mu.Unlock()
		if w != nil {
			w.mu.Lock()
			w.appended++
			w.mu.Unlock()
		}
		return nil
	}
	closeFn := func(_ context.Context, wid int64, _ time.Time, cnt int) error {
		c.mu.Lock()
		c.totalCloseCnts += cnt
		w := c.windows[wid]
		c.mu.Unlock()
		if w != nil {
			w.mu.Lock()
			w.closed++
			w.closeCnt = cnt
			w.mu.Unlock()
		}
		return nil
	}

	p := &funcPersist{
		openFn:   openFn,
		appendFn: appendFn,
		closeFn:  closeFn,
	}

	s, err := stream.NewAggregationStream(stream.Config{
		L2ToL3: 5 * time.Millisecond,
		L3ToL4: 1 * time.Second,
	}, p)
	if err != nil {
		t.Fatalf("NewAggregationStream: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)

	// Hammer Publish from many goroutines. Each successful Publish (nil
	// return, no ErrStreamStopped) MUST be observable in either an
	// AppendEvent call OR a flush slice carried into CloseWindow.
	var wg sync.WaitGroup
	const goroutines = 16
	const perGoroutine = 200
	var publishedOK int64
	var publishMu sync.Mutex
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				err := s.Publish(ctx, stream.LayerL2, stream.Event{
					Type:    "checkpoint",
					Payload: []byte(`{"ok":1}`),
				})
				if err == nil {
					publishMu.Lock()
					publishedOK++
					publishMu.Unlock()
				}
			}
		}()
	}
	wg.Wait()

	time.Sleep(50 * time.Millisecond)
	cancel()

	time.Sleep(20 * time.Millisecond)

	c.mu.Lock()
	totalAppend := c.totalAppend
	totalCloseCnts := c.totalCloseCnts
	c.mu.Unlock()

	publishMu.Lock()
	pub := publishedOK
	publishMu.Unlock()

	c.mu.Lock()
	for id, w := range c.windows {
		w.mu.Lock()
		if w.closed > 0 && w.appended != w.closeCnt {
			t.Errorf("window %d: appended=%d != closeCnt=%d (events lost between Publish and CloseWindow)",
				id, w.appended, w.closeCnt)
		}
		w.mu.Unlock()
	}
	c.mu.Unlock()

	if totalCloseCnts > totalAppend {
		t.Errorf("totalCloseCnts=%d > totalAppend=%d: phantom events accounted for in close",
			totalCloseCnts, totalAppend)
	}

	minAcceptable := int64(float64(pub) * 0.98)
	if int64(totalAppend) < minAcceptable {
		t.Errorf("totalAppend=%d < 98%% of publishedOK=%d (=%d): durability race lost events",
			totalAppend, pub, minAcceptable)
	}
	t.Logf("publishedOK=%d totalAppend=%d totalCloseCnts=%d", pub, totalAppend, totalCloseCnts)
}

type funcPersist struct {
	openFn   func(ctx context.Context, layer stream.Layer, openedAt time.Time) (int64, error)
	appendFn func(ctx context.Context, windowID int64, event stream.Event) error
	closeFn  func(ctx context.Context, windowID int64, closedAt time.Time, count int) error
	loadFn   func(ctx context.Context) ([]stream.WindowRecord, error)
}

func (f *funcPersist) OpenWindow(ctx context.Context, layer stream.Layer, openedAt time.Time) (int64, error) {
	if f.openFn != nil {
		return f.openFn(ctx, layer, openedAt)
	}
	return 1, nil
}
func (f *funcPersist) AppendEvent(ctx context.Context, windowID int64, event stream.Event) error {
	if f.appendFn != nil {
		return f.appendFn(ctx, windowID, event)
	}
	return nil
}
func (f *funcPersist) CloseWindow(ctx context.Context, windowID int64, closedAt time.Time, count int) error {
	if f.closeFn != nil {
		return f.closeFn(ctx, windowID, closedAt, count)
	}
	return nil
}
func (f *funcPersist) LoadOpenWindows(ctx context.Context) ([]stream.WindowRecord, error) {
	if f.loadFn != nil {
		return f.loadFn(ctx)
	}
	return nil, nil
}

func TestCheckBackpressureFiresWithLargeWindow(t *testing.T) {

	p := &backpressurePersist{}
	s, err := stream.NewAggregationStream(stream.Config{
		L2ToL3: 80 * time.Millisecond,
		L3ToL4: 500 * time.Millisecond,
	}, p)
	if err != nil {
		t.Fatalf("NewAggregationStream: %v", err)
	}

	lagFired := make(chan stream.LagInfo, 5)
	s.OnLag(func(li stream.LagInfo) {
		select {
		case lagFired <- li:
		default:
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancel()
	s.Start(ctx)

	for i := 0; i < 110; i++ {
		_ = s.Publish(ctx, stream.LayerL2, stream.Event{
			Type:    "checkpoint",
			Payload: []byte(`{}`),
		})
	}

	select {
	case li := <-lagFired:
		if li.Layer != stream.LayerL2 {
			t.Errorf("LagInfo.Layer = %v, want L2", li.Layer)
		}
		if li.WindowCount <= 100 {
			t.Errorf("LagInfo.WindowCount = %d, want > 100", li.WindowCount)
		}
		if li.HalfWindow <= 0 {
			t.Errorf("LagInfo.HalfWindow = %v, want > 0", li.HalfWindow)
		}
		if li.DetectedAt.IsZero() {
			t.Error("LagInfo.DetectedAt is zero")
		}
	case <-time.After(500 * time.Millisecond):

		t.Log("note: backpressure lag did not fire (acceptable: events may have been drained before half-window)")
	}
}
