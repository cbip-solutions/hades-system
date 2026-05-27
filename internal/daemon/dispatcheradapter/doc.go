// SPDX-License-Identifier: MIT
// Package dispatcheradapter bridges the dispatcher's CostSink seam to
// internal/store. Per invariant, the dispatcher / orchestrator /
// providers packages MUST NOT import internal/store; this package is
// the boundary that absorbs the dependency.
//
// Mirrors the release bypassadapter pattern: store-aware translation
// lives here, exposing typed methods that satisfy the dispatcher's
// CostSink interface (a single Insert(ctx, CostEvent) method today,
// extensible by composition if release+ grows the seam).
//
// Type-translation strategy. The package defines its own
// CostLedgerRow value type rather than reusing dispatcher.CostEvent
// directly when calling the store. The two are intentionally identical
// in shape; keeping them distinct buys two things:
//
// 1. The dispatcher package never gains a transitive dependency on
// internal/store — pure unit tests stay fast and tier-1
// bypass code stays minimal.
//
// 2. Future store-side schema changes (e.g., adding a column,
// splitting tokens by cache-hit class, or persisting per-attempt
// idempotency keys) ripple only through this adapter; CostEvent
// is the on-the-wire shape between dispatcher and emitter and is
// stabilised spec.
//
// Transitional shape. The Store interface
// declared in this package is the contract
// (*store.Store).InsertCostLedger MUST satisfy. Until that method
// lands,
// the daemon wires the adapter against an in-memory implementation in
// tests; once ships, the daemon entry point passes the real
// *store.Store directly — the concrete type satisfies the interface
// structurally, no glue required. This keeps B-7 fully decoupled from
// scope while still producing complete, production-shaped code
// (hades-system doctrine: build the final shape, not a scaffold).
//
// Scope discipline. The adapter implements ONLY dispatcher.CostSink.
// dispatcher.BreakerState wire-up is job (real circuit
// breaker) or B-8's daemon wiring (where any noop stand-in MUST be
// constructed and documented at the wiring site, not embedded as
// permissive methods on this Adapter). Permanent permissive methods
// here would be exactly the kind of scaffolding the project forbids.
package dispatcheradapter
