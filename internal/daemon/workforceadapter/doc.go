// SPDX-License-Identifier: MIT
// Package workforceadapter bridges internal/workforce/queue interfaces
// (SharedTaskList, CheckpointQueue, FixPromptQueue) to internal/store and
// the underlying SQLite tables introduced in migration 045 (workforce
// durable queues, release ).
//
// # Boundary (inv-hades-031)
//
// internal/workforce/queue MUST NOT import internal/store. This package
// is the single bridge: workforceadapter imports both. The boundary is
// enforced at compile-time by the noStoreImportAnalyzer in
// internal/doctrine/lint/analyzers/nostore (release Q16 D
// migration); analysistest fixtures in
// internal/doctrine/lint/analysistest/inv_hades_031_test.go pin the
// enforcement mechanism. The lint wrappers run the analyzer in
// CI; see project instructions "Boundary" rule.
//
// # Durability (inv-hades-073)
//
// Every constructor calls configureDurableConn on the *sql.DB to ensure
// PRAGMA journal_mode=WAL, synchronous=NORMAL, busy_timeout=5000. The
// compliance test in tests/compliance/inv_hades_073_test.go verifies that
// committed rows survive a hard-kill (os.Exit(2) without Close).
//
// # project_id isolation
//
// workforce_tasks UNIQUE is on (project_id, task_id); the same task_id is
// legal across projects. Production callers MUST use ScopedTo(projectID)
// on each *Impl to obtain a queue.{SharedTaskList,CheckpointQueue,
// FixPromptQueue} view that injects WHERE project_id = ? into every
// query. The unscoped *Impl methods are retained for in-package and
// single-project tests; they do not filter by project_id and may match
// rows in any project when task_id collides. See
// internal/workforce/queue/doc.go for the full contract.
//
// # Status transitions
//
// Advance enforces Kanban transitions inside the same tx as the SELECT/
// UPDATE read current → IsValidTransition(current, next) → write.
// Returns queue.ErrInvalidTransition on invalid edges. Both unscoped
// SharedTaskListImpl.Advance and ScopedSharedTaskList.Advance enforce
// the same edge map.
//
// # Test seams (the *Fn fields on each *Impl)
//
// Each *Impl type holds several function-typed fields named
// {operation}{Step}Fn (e.g., claimQueryFn, advanceExecFn, drainCommitFn,
// scopedAdvanceQueryFn, scannerFn). These exist solely so that
// export_test.go can inject failures at specific points inside otherwise
// uninterruptible methods (mid-tx errors, commit failures, scan
// errors). Production callers never assign these fields; the constructors
// install the canonical default* implementations and tests override only
// what each test needs to fail.
//
// Why this is intentional production "pollution":
//
// - The no-defer / no-tech-debt doctrine demands 100% coverage on
// every error branch including tx-error and commit-error paths.
// - SQLite + Go's database/sql do not provide a generic way to make a
// specific call site fail without affecting others (closing the DB
// fails everything; ignoring CHECK lets bad data through but does
// not cover the "exec returns error" branch).
// - A consolidated queueDriver interface (BeginTx/Query/Exec/Commit)
// was evaluated and rejected: per-call-site failure injection would
// then require fragile SQL-pattern matching or call-counting in the
// mock, which trade one form of pollution for another.
// - Documenting the seams (here) and isolating their use to
// export_test.go gives the cleanest balance of correctness, coverage,
// and reviewability.
//
// # Invariants
//
// var _ queue.SharedTaskList = (*SharedTaskListImpl)(nil)
// var _ queue.SharedTaskList = (*ScopedSharedTaskList)(nil)
// var _ queue.CheckpointQueue = (*CheckpointQueueImpl)(nil)
// var _ queue.CheckpointQueue = (*ScopedCheckpointQueue)(nil)
// var _ queue.FixPromptQueue = (*FixPromptQueueImpl)(nil)
// var _ queue.FixPromptQueue = (*ScopedFixPromptQueue)(nil)
package workforceadapter
