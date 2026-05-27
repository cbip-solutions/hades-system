// SPDX-License-Identifier: MIT
// Package queue provides three SQLite-backed durable queues for the
// zen-swarm workforce layer:
//
// - SharedTaskList — Kanban board (pending/in_progress/review/done/failed);
// concurrent-safe; checkpoint-keyed-by-thread_id; LangGraph pattern.
// - CheckpointQueue — async durable channel from generation Worker to L2
// tactical reviewer; deadline-stamped per invariant.
// - FixPromptQueue — async durable channel from L2/L3/L4 reviewer to next
// worker iteration.
//
// All three queues are SQLite-backed with WAL mode + busy_timeout=5 s.
// This package declares ONLY interfaces and value types. Concrete SQL-backed
// implementations live in internal/daemon/workforceadapter.
//
// # project_id isolation contract
//
// The backing tables (workforce_tasks, workforce_checkpoints,
// workforce_fix_prompts) all carry a project_id column (spec §7.1
// "project_id on every row"). UNIQUE on workforce_tasks is on
// (project_id, task_id) — the same task_id is legal across projects.
//
// Production callers (orchestrator, SubprocessManager, reviewers in Plans 5+)
// MUST scope every queue operation to a single projectID by calling
// ScopedTo(projectID) on the *Impl. The returned wrapper injects
// project_id = ? into every WHERE clause, eliminating cross-project leaks
// when task_id collides between projects.
//
// The unscoped *Impl methods are retained for in-package and single-project
// tests; they do not filter by project_id and may return rows from any
// project when collisions exist. New code outside this package and the
// adapter SHOULD use ScopedTo.
//
// # Status transitions
//
// Advance enforces Kanban transitions via IsValidTransition; invalid
// transitions return ErrInvalidTransition. See IsValidTransition for the
// complete edge map.
//
// Invariant compile-checks:
//
// var _ = DurableQueueOpened() // invariant: WAL requirement documented at interface level
package queue

// DurableQueueOpened is a zero-cost compile-time marker for invariant.
// Concrete implementations MUST configure: PRAGMA journal_mode=WAL;
// PRAGMA synchronous=NORMAL; PRAGMA busy_timeout=5000;
// This function is called by adapters in their constructor.
func DurableQueueOpened() bool { return true }
