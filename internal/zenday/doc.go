// SPDX-License-Identifier: MIT
// Package zenday composes operator daily flow loop:
// morning brief at 0 8 * * 1-5, EOD digest at 0 18 * * 1-5, plus
// operator-pull `bin/zen day` invocation. Per spec §1 Q13/Q14/Q15:
//
// - Cron-default + if_within: 2h catch-up + operator-pull idempotent
// (Q13 C verbatim Clawpilot)
// - Hard cap 7 items, leverage-sorted canonical 1..7 (Q14 B), with
// truncation marker `+ N more in zen inbox`
// - EOD digest composed from per-project HandoffPosted events from
// event-log + aggregator cache (Q15 C structured-data path)
//
// Architecture zenday.Collect fans out across N parallel sources (inbox
// cache, scheduler queue, gh CLI poll, autonomous state, cost ledger,
// event-log HandoffPosted) → zenday.SortByLeverage canonical sort →
// zenday.Render markdown → file write + stdout + macOS notif top-1 →
// emit MorningBriefReadyEvent | EODDigestReadyEvent.
//
// invariant: this package never imports internal/store. CollectDeps
// holds interfaces only — inbox.AggregatorCacheStore, scheduler.Store,
// dispatcheradapter.CostStore, eventlog.Reader, AutonomyStateReader,
// GitCli. Production wire-up is done in cmd/zen-swarm-ctld via
//
// invariant: this package never directly invokes the dispatcher.
// CollectDeps exposes only read-only views (cost ledger counters, event
// log).
//
// invariant: zen day brief output enforces the 7-item hard cap.
// Render() panics if it sees len(doc.Items) > MaxBriefItems — defense
// in depth Layer 2 against callers that bypass Collect.
//
// invariant: items sorted by canonical leverage rank 1..7 via
// SortByLeverage. Render() asserts IsSorted before emitting.
package zenday
