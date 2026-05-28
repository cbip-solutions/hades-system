// SPDX-License-Identifier: MIT
// Package worktreepool — HADES design git-worktree subprocess pool.
//
// Supplies isolated working directories to every HADES design subsystem needing
// off-trunk filesystem isolation: HRA voting (Functional Majority Voting
// per design choice B), ApplyEngine live-correction (design choice D), and future HADES design cross-
// worker integration via test-driven merge.
//
// Backend is `git worktree` invoked via os/exec (design choice A: every public agent
// runner uses subprocess; libgit2/cgo eliminated by GOOS=linux cross-
// compile gate; go-git eliminated by open perf gaps in #1956 and unmerged
// PR #1749).
//
// The pool is doctrine-tuned (design choice C):
// - max-scope: Floor=8, ElasticMax=32
// - default: Floor=3, ElasticMax=12
// - capa-firewall: Floor=5, ElasticMax=15
//
// GC defense-in-depth reclaims leaks via `git worktree prune` (Layer A) +
// filesystem rm -rf of dangling dirs (Layer B) on a doctrine-tunable
// cadence (default 5 min).
//
// Boundaries (lint-enforced):
// - invariant: NEVER imports internal/store directly
// - invariant: NEVER imports internal/workforce/queue directly
// - eventlog dependency is read-write via injected EventEmitter
// (an alias for eventlog.Appender's contract)
//
// Public API:
// - NewPool(cfg PoolConfig, emitter EventEmitter, exec Executor) (Pool, error)
// - Pool.Lease(ctx) (*Worktree, error)
// - Pool.Release(ctx, *Worktree) error
// - Pool.PruneOrphans(ctx) (PruneReport, error)
// - Pool.Close(ctx) error
//
// Background goroutines (each leak-tested via goleak.VerifyTestMain):
// - prewarm: maintains warm slice at floor F; spawns elastic up to M
// - orphanGC: every cfg.GCCadence runs git worktree prune + filesystem rm
//
// dispatcher.go is the canonical consumer; HRA voting
// and ApplyEngine call Lease/Release directly.
//
// Privacy contract: error messages MUST NOT
// leak subprocess stderr verbatim. The B-2 error taxonomy classifies
// subprocess failures into sanitized error classes.
package worktreepool
