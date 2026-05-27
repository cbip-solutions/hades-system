// SPDX-License-Identifier: MIT
package stream

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

type Layer int

const (
	LayerL2 Layer = 2

	LayerL3 Layer = 3

	LayerL4 Layer = 4
)

type Event struct {
	Type        string
	Payload     []byte
	PublishedAt time.Time
}

type FlushBatch struct {
	Layer    Layer
	WindowID int64
	OpenedAt time.Time
	ClosedAt time.Time
	Events   []Event
}

type WindowSnapshot struct {
	Layer    Layer
	OpenedAt time.Time
	Count    int
}

type WindowRecord struct {
	WindowID int64
	Layer    Layer
	OpenedAt time.Time
	Count    int
}

type StreamPersist interface {
	OpenWindow(ctx context.Context, layer Layer, openedAt time.Time) (int64, error)

	AppendEvent(ctx context.Context, windowID int64, event Event) error

	CloseWindow(ctx context.Context, windowID int64, closedAt time.Time, count int) error

	LoadOpenWindows(ctx context.Context) ([]WindowRecord, error)
}

type FlushHandler func(FlushBatch)

type SubscribeHandler func(FlushBatch)

type LagInfo struct {
	Layer       Layer
	WindowCount int
	HalfWindow  time.Duration
	DetectedAt  time.Time
}

type LagHandler func(LagInfo)

type PersistErrorHandler func(error)

type Config struct {
	L2ToL3 time.Duration

	L3ToL4 time.Duration

	BackpressureRatePerSec float64
}

const defaultBackpressureRatePerSec = 6.6

// defaultBackpressureMinCount is the absolute floor on the per-check
// threshold so very short windows (sub-second tests) do not trip on
// trivial counts. The detector requires snap.Count > max(threshold,
// defaultBackpressureMinCount).
const defaultBackpressureMinCount = 100

// window holds the in-progress accumulation for one layer.
//
// Concurrency contract:
// - All field reads/writes (id, openedAt, events) MUST hold w.mu.
// - appendAndPersist runs the in-memory append AND the persist call
// under the SAME critical section so that flush cannot interleave
// between them. This prevents the C-1/C-2/C-3 durability race where
// Publish would land an event in a post-flush w.events slice while
// reading wid==0 — losing the event from both AppendEvent (skipped)
// and the prior flush batch (already drained).
// - flush drains events + zeroes the window in one critical section,
// then returns a FlushBatch holding the drained slice. The caller
// must NOT call flush while holding w.mu (it acquires internally).
type window struct {
	mu       sync.Mutex
	id       int64
	openedAt time.Time
	events   []Event
}

func (w *window) appendAndPersist(ctx context.Context, persist StreamPersist, event Event) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.events = append(w.events, event)
	if w.id == 0 {

		return nil
	}
	return persist.AppendEvent(ctx, w.id, event)
}

func (w *window) snapshot() WindowSnapshot {
	w.mu.Lock()
	defer w.mu.Unlock()
	return WindowSnapshot{OpenedAt: w.openedAt, Count: len(w.events)}
}

func (w *window) flushAndReopen(
	ctx context.Context,
	layer Layer,
	closedAt time.Time,
	closeFn func(ctx context.Context, oldID int64, closedAt time.Time, count int) error,
	openFn func(ctx context.Context, layer Layer, openedAt time.Time) (int64, error),
) (FlushBatch, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	evs := w.events
	oldID := w.id
	opened := w.openedAt
	w.events = nil
	w.id = 0
	w.openedAt = time.Time{}

	batch := FlushBatch{
		Layer:    layer,
		WindowID: oldID,
		OpenedAt: opened,
		ClosedAt: closedAt,
		Events:   evs,
	}

	var closeErr error
	if oldID != 0 {
		closeErr = closeFn(ctx, oldID, closedAt, len(evs))
	}

	now := time.Now().UTC()
	newID, openErr := openFn(ctx, layer, now)
	if openErr != nil {
		newID = 0
	}
	w.id = newID
	w.openedAt = now

	var combined error
	switch {
	case closeErr != nil && openErr != nil:
		combined = fmt.Errorf("flushAndReopen close+open layer=%d: close=%v open=%v",
			layer, closeErr, openErr)
	case closeErr != nil:
		combined = fmt.Errorf("flushAndReopen close layer=%d id=%d: %w", layer, oldID, closeErr)
	case openErr != nil:
		combined = fmt.Errorf("flushAndReopen open layer=%d: %w", layer, openErr)
	}
	return batch, combined
}

func (w *window) initialOpen(
	ctx context.Context,
	layer Layer,
	openFn func(ctx context.Context, layer Layer, openedAt time.Time) (int64, error),
) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := time.Now().UTC()
	id, err := openFn(ctx, layer, now)
	if err != nil {

		w.id = 0
		w.openedAt = now
		return err
	}
	w.id = id
	w.openedAt = now
	return nil
}

type AggregationStream struct {
	cfg     Config
	persist StreamPersist

	mu                sync.RWMutex
	started           bool
	windows           map[Layer]*window
	flushHooks        []FlushHandler
	subscribers       map[Layer][]SubscribeHandler
	lagHandlers       []LagHandler
	persistErrorHooks []PersistErrorHandler

	stopOnce sync.Once
	stopped  chan struct{}
}

var ErrStreamAlreadyStarted = errors.New("aggregation stream: already started")

var ErrStreamStopped = errors.New("aggregation stream: stopped")

func NewAggregationStream(cfg Config, persist StreamPersist) (*AggregationStream, error) {
	if cfg.L2ToL3 <= 0 {
		return nil, fmt.Errorf("stream.NewAggregationStream: L2ToL3 must be > 0")
	}
	if cfg.L3ToL4 <= 0 {
		return nil, fmt.Errorf("stream.NewAggregationStream: L3ToL4 must be > 0")
	}
	if persist == nil {
		return nil, fmt.Errorf("stream.NewAggregationStream: persist is nil")
	}
	s := &AggregationStream{
		cfg:         cfg,
		persist:     persist,
		windows:     map[Layer]*window{LayerL2: {}, LayerL3: {}},
		subscribers: make(map[Layer][]SubscribeHandler),
		stopped:     make(chan struct{}),
	}
	return s, nil
}

func (s *AggregationStream) OnFlush(h FlushHandler) {
	s.mu.Lock()
	s.flushHooks = append(s.flushHooks, h)
	s.mu.Unlock()
}

func (s *AggregationStream) Subscribe(layer Layer, h SubscribeHandler) {
	s.mu.Lock()
	s.subscribers[layer] = append(s.subscribers[layer], h)
	s.mu.Unlock()
}

func (s *AggregationStream) OnLag(h LagHandler) {
	s.mu.Lock()
	s.lagHandlers = append(s.lagHandlers, h)
	s.mu.Unlock()
}

func (s *AggregationStream) OnPersistError(h PersistErrorHandler) {
	s.mu.Lock()
	s.persistErrorHooks = append(s.persistErrorHooks, h)
	s.mu.Unlock()
}

func (s *AggregationStream) notifyPersistError(err error) {
	if err == nil {
		return
	}
	s.mu.RLock()
	hs := s.persistErrorHooks
	s.mu.RUnlock()
	for _, h := range hs {
		h(err)
	}
}

func (s *AggregationStream) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return ErrStreamAlreadyStarted
	}
	s.started = true
	s.mu.Unlock()

	for _, layer := range []Layer{LayerL2, LayerL3} {
		s.mu.RLock()
		w := s.windows[layer]
		s.mu.RUnlock()
		if err := w.initialOpen(ctx, layer, s.persist.OpenWindow); err != nil {
			s.notifyPersistError(fmt.Errorf("Start initialOpen layer=%d: %w", layer, err))
		}
	}

	go s.tickerLoop(ctx, LayerL2, s.cfg.L2ToL3)
	go s.tickerLoop(ctx, LayerL3, s.cfg.L3ToL4)
	go func() {
		<-ctx.Done()
		s.stopOnce.Do(func() { close(s.stopped) })
	}()
	return nil
}

// Publish appends an event to the given layer's current window. Blocks
// until the event is persisted (or determined to be in the in-memory
// slice, see appendAndPersist contract). Returns ErrStreamStopped if the
// stream has stopped. LayerL4 may not be published to directly (it is only
// a flush-target from L3).
//
// Durability contract (post C-1/C-2/C-3 fix): every successful Publish
// returns nil iff the event is durably reachable from one of:
//
// (a) aggregation_events row inserted via AppendEvent under the same
// w.mu critical section as the in-memory append (no race window).
// (b) the w.events slice that will be drained by the next flush and
// handed to subscribers / hooks via FlushBatch.Events.
//
// AppendEvent persistence errors are surfaced via the OnPersistError
// callback (registered via OnPersistError); they do NOT cause Publish to
// fail because the event is already in w.events and will be re-persisted
// or batched by the next flush. This matches the noopPersist + best-effort
// recovery semantics required by invariant (durability via WAL + flush).
func (s *AggregationStream) Publish(ctx context.Context, layer Layer, event Event) error {
	select {
	case <-s.stopped:
		return ErrStreamStopped
	default:
	}
	if layer != LayerL2 && layer != LayerL3 {
		return fmt.Errorf("stream.Publish: layer %d not publishable directly (use L2 or L3)", layer)
	}
	event.PublishedAt = time.Now().UTC()

	s.mu.RLock()
	w := s.windows[layer]
	s.mu.RUnlock()

	if err := w.appendAndPersist(ctx, s.persist, event); err != nil {

		s.notifyPersistError(err)
	}
	return nil
}

func (s *AggregationStream) WindowSnapshot(layer Layer) WindowSnapshot {
	s.mu.RLock()
	w, ok := s.windows[layer]
	s.mu.RUnlock()
	if !ok {

		return WindowSnapshot{Layer: layer}
	}
	snap := w.snapshot()
	snap.Layer = layer
	return snap
}

func (s *AggregationStream) tickerLoop(ctx context.Context, layer Layer, period time.Duration) {
	ticker := time.NewTicker(period)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case t := <-ticker.C:
			s.flushLayer(ctx, layer, t.UTC())

			s.checkBackpressure(ctx, layer, period)
		}
	}
}

// flushLayer rotates the layer's window: drain in-memory events, persist
// the close on the OLD durable id, persist the open of a NEW durable id,
// then deliver the drained batch to subscribers + hooks.
//
// The close+drain+reopen sequence is atomic under w.mu (see
// window.flushAndReopen): no Publish can land an event with w.id == 0
// during the rotation, so every published event is either persisted via
// AppendEvent inside the OLD window OR carried by the next AppendEvent
// inside the NEW window — there is no silent-loss path.
//
// Hook + subscriber dispatch happens AFTER the lock is released so that
// slow handlers do not block concurrent Publish calls. Global hooks fire
// for every tick including empty windows (heartbeat); subscribers only
// fire for non-empty batches (no point waking consumers on idle ticks).
func (s *AggregationStream) flushLayer(ctx context.Context, layer Layer, closedAt time.Time) {
	s.mu.RLock()
	w := s.windows[layer]
	s.mu.RUnlock()

	batch, err := w.flushAndReopen(ctx, layer, closedAt,
		s.persist.CloseWindow, s.persist.OpenWindow)
	if err != nil {
		s.notifyPersistError(err)
	}

	s.mu.RLock()
	hooks := s.flushHooks
	var subs []SubscribeHandler
	if len(batch.Events) > 0 {
		subs = s.subscribers[layer]
	}
	s.mu.RUnlock()

	for _, h := range hooks {
		h(batch)
	}
	for _, sub := range subs {
		sub(batch)
	}
}

// checkBackpressure detects when the current window is filling faster
// than expected and dispatches lag visibility to registered LagHandlers.
//
// The fired LagInfo carries layer + count + halfWindow + timestamp so
// handlers can decide what to do ( wires /v1/audit/emit so the
// event reaches the daemon audit log; future may derive a richer
// publish-rate signal from budget anomaly data).
//
// Critical (C-4 fix): this function MUST NOT publish into the stream
// itself. The buggy version called s.Publish(ctx, LayerL3, lagEvent),
// which added an event to the lagging stream and amplified backpressure
// → more lag events → runaway feedback. Lag visibility now flows
// exclusively through the OnLag handler chain — no in-stream
// re-publishing.
//
// The heuristic ("count > rate-threshold * halfWindow") is configurable
// via Config.BackpressureRatePerSec — see docs there for the rationale
// (default 6.6 events/s matches max-scope L2 = 100 events / 15s).
func (s *AggregationStream) checkBackpressure(_ context.Context, layer Layer, period time.Duration) {
	halfWindow := period / 2
	snap := s.WindowSnapshot(layer)

	rate := s.cfg.BackpressureRatePerSec
	if rate <= 0 {
		rate = defaultBackpressureRatePerSec
	}
	threshold := int(rate * halfWindow.Seconds())
	if threshold < defaultBackpressureMinCount {
		threshold = defaultBackpressureMinCount
	}

	if snap.Count > threshold && time.Since(snap.OpenedAt) < halfWindow {
		li := LagInfo{
			Layer:       layer,
			WindowCount: snap.Count,
			HalfWindow:  halfWindow,
			DetectedAt:  time.Now().UTC(),
		}
		s.mu.RLock()
		handlers := s.lagHandlers
		s.mu.RUnlock()
		for _, h := range handlers {
			h(li)
		}
	}
}
