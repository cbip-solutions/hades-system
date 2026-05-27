// SPDX-License-Identifier: MIT
// Package scheduleradapter bridges internal/scheduler types to
// daemon.db. Satisfies inv-hades-031: internal/scheduler/* never imports
// internal/store; scheduleradapter is the single bridge.
//
// wraps *store.Store CRUD primitives (internal/store/schedules.go) for
// the schedules + schedule_history tables (migration 063). The skeleton
// is fully working code — no stubs — operating on store.ScheduleRow /
// store.ScheduleHistoryRow value types. release Task D-2 lands
// the internal/scheduler package with its own Schedule + HistoryEntry
// value types and adds a thin translation layer (scheduler.Schedule ↔
// store.ScheduleRow) on top of this adapter without touching the store
// boundary.
//
// Style mirrors internal/daemon/quotaadapter/, bypassadapter/, and
// projectctxadapter/ patterns from earlier plans:
// - Constructor `New(*store.Store) *Adapter` panics on nil store
// (defensive contract; daemon wiring guarantees a real store).
// - Methods accept context.Context first; cancellation is detected
// before SQL exec/query so a cancelled ctx fails fast with a
// wrapped error that satisfies errors.Is(err, context.Canceled).
// - Get returns (nil, nil) on absent row; Update / Delete return
// store.ErrScheduleNotFound on absent row (operator-driven
// mutations have no target — the absent case is an error).
//
// inv-hades-122 boundary: the import list of this package is the single
// legitimate co-location of internal/scheduler (when D-2 lands it) and
// internal/store anywhere in the codebase. The compliance test
// inv_hades_122_inv_hades_031_plan7_packages_test.go extends to enforce
// this on release packages.
package scheduleradapter
