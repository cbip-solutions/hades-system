// SPDX-License-Identifier: MIT
// Package handlers provides the HTTP handler factories for the
// hades-system daemon (hades-ctld) /v1/* surface.
//
// Each route is registered in internal/daemon/server.go via
// http.ServeMux.Handle and wrapped with RateLimitMiddleware drawing
// per-endpoint thresholds from RateLimitCtx.RateLimitThreshold (canonical
// defaults in DefaultRateLimits below).
//
// # Route table
//
// GET /v1/research/cache/get ResearchCacheGet
// POST /v1/research/cache/set ResearchCacheSet
// POST /v1/audit/emit AuditEmit
// GET /v1/budget/cap_status BudgetCapStatus
// POST /v1/budget/record BudgetRecord
// GET /v1/budget/axes BudgetAxes
// GET /v1/budget/anomaly BudgetAnomaly
// GET /v1/budget/events BudgetEvents
// POST /v1/budget/pause BudgetPause
// POST /v1/budget/resume BudgetResume
// GET /v1/workforce/specs WorkforceSpecs
// GET /v1/workforce/workers WorkforceWorkers
// GET /v1/workforce/checkpoints WorkforceCheckpoints
// GET /v1/workforce/fix_prompts WorkforceFixPrompts
// GET /v1/workforce/aggregations WorkforceAggregations
// GET /v1/workforce/gate/state OperatorGateState
// POST /v1/workforce/gate/pause OperatorGatePause
// POST /v1/workforce/gate/resume OperatorGateResume
// GET /v1/doctrine/state DoctrineState
// POST /v1/doctrine/validate DoctrineValidate
// POST /v1/doctrine/reload DoctrineReload
//
// All routes share the writeJSON helper (events.go) and follow the
// shape: writeJSON(w, code, body any).
//
// # Default rate limits (req/s, doctrine-overridable)
//
// research_cache_get: 200 research_cache_set: 100
// audit_emit: 500 budget_cap_status: 50
// budget_record: 50 budget_axes: 50
// budget_anomaly: 20 budget_events: 20
// budget_pause: 10 budget_resume: 10
// workforce_specs: 20 workforce_workers: 20
// workforce_checkpoints: 20 workforce_fix_prompts: 20
// workforce_aggregations: 10 gate_state: 50
// gate_pause: 10 gate_resume: 10
// doctrine_state: 20 doctrine_validate: 10
// doctrine_reload: 2
//
// Single source of truth: DefaultRateLimits in ratelimit.go (post-review
// I-6 fix). Server.RateLimitThreshold delegates to handlers.Defaults().
//
// # Concurrency model
//
// - Every handler is reentrant: state lives entirely behind the
// supplied Ctx interface.
// - RateLimitMiddleware uses a per-Server *BucketRegistry (post-review
// C-1 fix). Each *daemon.Server allocates one in New(); tests
// allocate per-case for isolation. Doctrine reload calls
// registry.InvalidateAll() so threshold changes are observable on
// the very next request.
// - The threshold callback runs OUTSIDE the registry mutex so a slow
// ctx.RateLimitThreshold cannot stall concurrent dispatch (post-
// review I-5 fix).
// - Handlers do NOT spawn goroutines. Long-lived background workers
// (audit retention, research_cache eviction) live on *daemon.Server
// and shut down via Server.Stop's context cancellation.
//
// # Nil-dependency policy
//
// Handler factory functions in this package fail-fast on missing
// dependencies — passing a nil interface that the factory's body would
// dereference is a wiring bug at daemon bootstrap, surfaced as a panic
// at construction so the operator's doctor detects the bug immediately
// rather than after the daemon reports healthy on its readiness probe
// and silently 500s every request.
//
// This aligns with the sibling internal/daemon/transport package whose
// NewMessagesHandler + NewHadesSystemTransport constructors panic on
// nil dispatcher (see internal/daemon/transport/doc.go and reviewer M4
// post- audit). Optional dependencies (audit anchors, with
// documented graceful-degradation paths) MAY be nil; required engines
// (DoctrineReader, Dispatcher) MUST NOT.
//
// # Invariants
//
// - invariant: All routes ride the Unix domain socket bound by
// server.go. The TCP listener (when configured) is for the local
// web UI only; production daemons under launchd leave HTTPAddr
// empty.
// - invariant: This package never imports internal/store. The
// Ctx interfaces (ResearchCacheCtx, AuditEmitCtx, BudgetCtx,
// WorkforceCtx, OperatorGateCtx, DoctrineCtx, RateLimitCtx) are the
// bridge: *daemon.Server satisfies them via methods that delegate
// through internal/daemon/{bypass,workforce}adapter to *store.Store.
// handlers.doctrine.go imports the doctrine.ErrDoctrineValidation
// sentinel — pure value, no transitive store dependency.
// - invariant: API surface is /v1/-versioned and stays stable across
// plans; handlers for routes whose backing engine isn't wired yet
// return shape-correct defaults (PHASE_G_DEFAULT) so contract
// tests pass from day 1.
package handlers
