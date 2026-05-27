// SPDX-License-Identifier: MIT
// Package scheduler is documented in scheduler.go (the package doc
// comment is on the `package scheduler` declaration there).
//
// File layout:
//
// scheduler.go — D-2 enums + value types + sentinel errors
// store_iface.go — D-2 Store interface (invariant bridge)
// quota_iface.go — D-2 QuotaPreFlightChecker
// dispatcher_iface.go — D-2 Dispatcher (invariant single-egress)
// eventlog_iface.go — D-2 EventEmitter
// ratelimiter_iface.go — D-2 RateLimiter (per-project fire gate)
// sentinel.go — D-2 compile-time invariant anchors
// cron.go — D-3 robfig/cron/v3 wrapper, 5-field vixie
// jitter.go — D-4 deterministic jitter (invariant)
// miss_policy.go — D-5 doctrine matrix + ComputeMissed (D-6)
// coalesce.go — D-6 BackfillWindow construction
// routine.go — D-7 durable cron-driven schedule
// task.go — D-8 ephemeral one-shot
// loop.go — D-9 session-bound polling
// httptrigger.go — D-10 per-routine bearer token verifier
// gitpoll.go — D-11 `gh` CLI poll
// fire.go — D-12 orchestration (jitter → miss → quota → dispatch)
//
// Invariant cross-references:
//
// - invariant — package never imports internal/store; bridge via
// internal/daemon/scheduleradapter satisfying Store interface.
// - invariant / invariant — package never imports
// internal/providers or private-tier1-module; LLM dispatch
// goes only through the Dispatcher interface.
// - invariant — Scheduler jitter offset deterministic
// (jitterDeterministicSentinel anchor in sentinel.go).
// - invariant — Per-doctrine miss policy correctly applied
// (missPolicyDoctrineSentinel anchor in sentinel.go).
package scheduler
