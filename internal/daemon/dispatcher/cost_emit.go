// SPDX-License-Identifier: MIT
// internal/daemon/dispatcher/cost_emit.go
//
// AsyncEmitter is the production implementation of dispatcher.CostEmitter.
// It buffers CostEvents in a bounded channel and forwards them, in order,
// to a CostSink ( dispatcheradapter writes them to
// cost_ledger). Async by design: the response path NEVER blocks on the
// ledger writer — a slow or stalled sink degrades to event drops, never
// to caller backpressure.
//
// Concurrency contract:
//
// - Emit is non-blocking. If the buffer is full, the event is dropped
// and DroppedCount is incremented; a structured log line is emitted
// so operators can spot ledger backpressure.
//
// - Emit is safe to call concurrently with Close. The naive approach
// (select on `ch <- evt` + `<-done` + default) is broken: when ch is
// closed, the send case is "ready" in the select sense (it will run
// and panic), and Go may pick it over the done case. We therefore
// guard sends with a sync.RWMutex: Emit holds RLock around its
// closed-flag check + send; Close holds the exclusive Lock around
// setting closed=true and close(ch). While Emit holds RLock, Close
// cannot acquire Lock — so a send on a closed channel is
// structurally impossible.
//
// Close is also wrapped in sync.Once to make double-close a no-op
// (idempotent), avoiding the classic close-of-closed-channel panic
// when shutdown is retried.
//
// - Flush blocks until every event Emitted prior to the Flush call has
// been delivered to the sink. Implementation: an internal flush-
// request channel; the worker sees the request, drains everything
// currently buffered in `ch`, then acks the requestor. This gives
// the caller a true happens-before barrier without forcing the
// worker to terminate (unlike the plan-reference's "Flush == Close"
// conflation, which would prevent post-flush emission).
//
// Boundary (invariant): this file imports stdlib only. The CostSink
// interface is the seam that lets dispatcheradapter bridge
// to internal/store without violating the no-direct-store rule for the
// dispatcher package.

package dispatcher

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
)

// CostSink is the persistence seam between AsyncEmitter and the cost
// ledger. dispatcheradapter implements it on top of
// internal/store; tests use an in-memory recorder. Insert MUST be safe
// for sequential calls from the worker goroutine; the AsyncEmitter
// itself never invokes it concurrently. Implementations MAY return an
// error — AsyncEmitter logs and continues; it never crashes the worker.
// AsyncEmitter invokes Insert with context.Background(); implementations
// needing a deadline should derive their own from configuration, not
// assume a caller-bounded ctx.
type CostSink interface {
	Insert(ctx context.Context, evt CostEvent) error
}

const defaultBufferSize = 64

type AsyncEmitter struct {
	sink CostSink

	ch chan CostEvent

	flushReq chan chan struct{}

	done chan struct{}

	mu     sync.RWMutex
	closed bool

	wg        sync.WaitGroup
	closeOnce sync.Once

	droppedCount atomic.Int64
}

// NewAsyncEmitter constructs an AsyncEmitter with the given sink and
// buffer size. A bufferSize <= 0 defaults to defaultBufferSize (64). The
// worker goroutine is started immediately; the caller must Close the
// emitter on shutdown to drain pending events and release the worker.
//
// Pre sink != nil
// Post returned emitter is ready to Emit / Flush / Close
//
// Passing a nil sink panics — same fail-fast posture as dispatcher.New:
// a nil sink at boot is a wiring bug that MUST be surfaced before
// serving traffic, not a runtime condition to recover from.
func NewAsyncEmitter(sink CostSink, bufferSize int) *AsyncEmitter {
	if sink == nil {
		panic("dispatcher.NewAsyncEmitter: sink is required")
	}
	if bufferSize < 1 {
		bufferSize = defaultBufferSize
	}
	e := &AsyncEmitter{
		sink:     sink,
		ch:       make(chan CostEvent, bufferSize),
		flushReq: make(chan chan struct{}),
		done:     make(chan struct{}),
	}
	e.wg.Add(1)
	go e.run()
	return e
}

// Emit forwards evt to the worker without blocking. Three outcomes:
//
// - emitter has been Closed -> silently drops, returns nil
// - buffer has slack -> enqueues, returns nil
// - buffer is full -> increments droppedCount, logs a
// Warn, returns nil (still nil:
// the response path MUST NOT see
// ledger errors)
//
// The error return is reserved for future use (e.g., a strict-mode
// emitter that surfaces ctx cancellation); today it is always nil so
// callers can ignore it without losing information.
//
// Concurrent-safe with Close: producers take RLock around the closed-
// flag check + send. Close takes the exclusive Lock before closing
// channels, so a send-on-closed-channel panic is structurally
// impossible: while Emit holds RLock, Close cannot acquire Lock and
// therefore cannot have closed ch yet.
func (e *AsyncEmitter) Emit(_ context.Context, evt CostEvent) error {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.closed {

		return nil
	}

	// Non-blocking send: if the buffer is full, drop with a warning.
	select {
	case e.ch <- evt:
		return nil
	default:
		e.droppedCount.Add(1)
		slog.Warn("cost_emit: buffer full; event dropped",
			"buffer_cap", cap(e.ch),
			"project", evt.Project,
			"tier", evt.Tier,
		)
		return nil
	}
}

func (e *AsyncEmitter) Flush() {
	ack := make(chan struct{})
	select {
	case e.flushReq <- ack:

		select {
		case <-ack:
		case <-e.done:
		}
	case <-e.done:

		return
	}
}

func (e *AsyncEmitter) Close() {
	e.closeOnce.Do(func() {
		e.mu.Lock()
		e.closed = true
		close(e.ch)
		e.mu.Unlock()

		e.wg.Wait()
		close(e.done)
	})
}

func (e *AsyncEmitter) DroppedCount() int64 {
	return e.droppedCount.Load()
}

// run is the worker loop. Single goroutine, owns sequential access to
// the sink, exits when ch is closed (via Close).
//
// Loop body:
//
// - Receive an event from ch -> Insert into sink, log on error,
// continue (sink errors MUST NOT kill the worker; the cost ledger
// can have transient failures and we owe it best-effort delivery
// of subsequent events).
//
// - Receive a flush request -> drains everything currently buffered
// (and any events that arrive during the drain); events Emitted
// strictly after the drain returns are not awaited. The drain uses
// a default branch in the inner select to detect "no more events
// buffered right now" and closes the requester's ack chan.
// Flush's contract is a happens-before for events submitted prior
// to the Flush call; anything arriving after is best-effort.
func (e *AsyncEmitter) run() {
	defer e.wg.Done()
	for {
		select {
		case evt, ok := <-e.ch:
			if !ok {
				return
			}
			e.deliver(evt)
		case ack := <-e.flushReq:
			e.drainTo(ack)
		}
	}
}

// deliver wraps sink.Insert with structured-log error reporting and a
// panic recovery guard. ctx is context.Background() because the emitter
// outlives any single request's ctx — a request that triggered an event
// may have been cancelled long before the worker gets to it, and that
// cancellation should not abort the ledger write.
//
// The recover guard is defense-in-depth: production sinks (e.g. SQLite
// via dispatcheradapter) can panic on rare driver edge cases (GC
// pressure on cgo, corrupted WAL, etc.). A panic MUST NOT kill the
// worker goroutine — the worker must self-heal so subsequent Emits
// continue to drain.
func (e *AsyncEmitter) deliver(evt CostEvent) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("cost_emit: sink panic; worker recovered",
				"panic", r,
				"project", evt.Project,
				"tier", evt.Tier,
				"session", evt.SessionID,
			)
		}
	}()
	if err := e.sink.Insert(context.Background(), evt); err != nil {
		slog.Error("cost_emit: sink insert failed",
			"err", err,
			"project", evt.Project,
			"tier", evt.Tier,
			"session", evt.SessionID,
		)
	}
}

func (e *AsyncEmitter) drainTo(ack chan struct{}) {
	for {
		select {
		case evt, ok := <-e.ch:
			if !ok {
				close(ack)
				return
			}
			e.deliver(evt)
		default:
			close(ack)
			return
		}
	}
}
