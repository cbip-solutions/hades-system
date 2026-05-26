// SPDX-License-Identifier: MIT
// Package stream implements the HRA (Hierarchical Review Architecture)
// time-windowed aggregation pipeline.
//
// AggregationStream accumulates events published at layer L2 (Worker
// checkpoints) and flushes them as FlushBatch values to layer L3
// (tactical reviewer cadence).  A second ticker aggregates L3 batches
// upward to L4 (strategic reviewer cadence).
//
// Doctrine-tunable window sizes (via doctrine.Resolve in Phase A):
//   - max-scope:      L2→L3 = 30s,  L3→L4 = 5min
//   - default:        L2→L3 = 60s,  L3→L4 = 15min
//   - capa-firewall:  per-Pulido cycle (caller supplies explicit Config)
//
// Persistence is decoupled via the StreamPersist interface so unit tests
// run without SQLite. The production adapter lives in
// internal/daemon/workforceadapter/stream_adapter.go (inv-zen-031).
//
// Background goroutines (per spec §3.6):
//   - One ticker per layer pair (L2→L3, L3→L4) for window-flush.
//   - Backpressure detector: invokes registered LagHandlers when the
//     current window's count crosses a rate-scaled threshold (see
//     Config.BackpressureRatePerSec). Lag visibility flows through
//     OnLag handlers only — checkBackpressure does NOT publish back into
//     the stream itself (C-4 amplification fix).
//
// # Concurrency contract
//
// AggregationStream is safe for concurrent use across all public
// methods. The internal locking strategy is layered:
//
//  1. AggregationStream.mu (sync.RWMutex) protects:
//     - the windows map lookup (windows[layer] resolution),
//     - the started bool (Start idempotency),
//     - the four handler slices (flushHooks, subscribers, lagHandlers,
//     persistErrorHooks).
//     Reads use RLock; writes use Lock.
//
//  2. window.mu (sync.Mutex; one per layer) protects EVERY field of the
//     window struct (id, openedAt, events). All append + persist + flush
//     operations on a window hold this lock for the duration of any I/O
//     call to the StreamPersist backend, by design — see C-1/C-2/C-3
//     fix below.
//
// # C-1/C-2/C-3 durability invariant
//
// The following property is the load-bearing safety guarantee of this
// package:
//
//	Every successful Publish (returns nil) MUST result in either
//	(a) one INSERT into aggregation_events via AppendEvent inside
//	    the open window, OR
//	(b) inclusion in the FlushBatch.Events slice handed to subscribers
//	    + hooks at the next window flush.
//
// Both paths are durable: (a) writes the event row directly; (b) goes
// through CloseWindow which records event_count and the daemon Phase G
// audit emitter persists the batch via subscribers. There is NO third
// path — and in particular, there is NO scenario where an event is
// counted in event_count but absent from aggregation_events.
//
// The buggy implementation had exactly that third path: a Publish that
// landed in w.events while w.id == 0 (mid-rotation) would skip
// AppendEvent AND be carried by the next flush with the new window's
// id, producing event_count > count(aggregation_events). The fix is
// structural: window.appendAndPersist runs append + persist under the
// same w.mu critical section; window.flushAndReopen runs drain + close
// + open + new-id-install under the same w.mu critical section. No
// Publish can interleave between "drain" and "new id installed".
//
// # Idempotency contract
//
//   - NewAggregationStream is idempotent across multiple invocations
//     with the same arguments (it constructs a new value each call).
//
//   - Start is NOT idempotent. A second invocation returns
//     ErrStreamAlreadyStarted without spawning duplicate tickers or
//     opening duplicate windows. Callers wishing to restart after
//     ctx-cancel must construct a new AggregationStream.
//
//   - Publish is callable concurrently across goroutines AND idempotent
//     in the sense that duplicate events (e.g. Worker retries) result
//     in duplicate stored events; deduplication is the caller's
//     responsibility.
//
//   - OnFlush / Subscribe / OnLag / OnPersistError are safe to call
//     before OR after Start. Handler registration is appended under
//     s.mu.Lock(); the next event delivery (flush tick / lag detection
//     / persist call) sees the new handler.
//
// # Handler delivery contract
//
//   - Global flush hooks (OnFlush) fire for EVERY tick including
//     empty windows (heartbeat / watchdog).
//   - Per-layer subscribers (Subscribe) fire ONLY for non-empty
//     batches (no point waking consumers on idle ticks).
//   - LagHandlers (OnLag) fire when the current window count exceeds
//     the rate-scaled threshold (see Config.BackpressureRatePerSec)
//     within the first half of the window period.
//   - PersistErrorHandlers (OnPersistError) fire synchronously inside
//     the persist-call site (Publish for AppendEvent errors,
//     Start/flushLayer for OpenWindow + CloseWindow errors).
//
// All handlers MUST NOT block. They run synchronously inside the
// flush goroutine / Publish call site / lag detector. Long-running
// work should be queued to a channel + drained by a separate consumer.
//
// # Persistence contract (StreamPersist)
//
// The StreamPersist interface is the only persistence surface.
// Implementations are not required to be thread-safe at the call-site
// level — AggregationStream serialises all calls through w.mu — but
// the daemon's *store.Store is itself thread-safe via SQLite WAL +
// busy_timeout=5s.
//
// Persistence errors are best-effort: they do NOT cause Publish to
// fail (the event remains in w.events and is carried by the next
// flush). They DO surface via OnPersistError so operators see
// degradation. inv-zen-073 (durability via WAL + flush) is satisfied
// because the in-memory slice + flush batch path is the authoritative
// delivery channel.
package stream
