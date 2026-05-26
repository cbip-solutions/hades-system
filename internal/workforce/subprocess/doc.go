// SPDX-License-Identifier: MIT
// Package subprocess owns the OS-level lifecycle of OpenClaude subprocesses
// for the zen-swarm workforce (Q3 C decision, Plan 4 spec §2.2 Capa 1).
//
// Two lifecycles are supported:
//
//  1. EPHEMERAL — Worker variant: spawn-per-task; the subprocess exits when
//     Worker.Run completes. SubprocessManager.SpawnEphemeral is the entry
//     point. Ephemeral sessions never appear in the persistence layer.
//
//  2. PERSISTENT — TeamLead + Reviewer L3/L4 variants: long-lived subprocess
//     with stable thread_id (LangGraph-style checkpoint pattern). The same
//     (specID, doctrineName) tuple yields the same Session as long as the
//     subprocess is alive (idempotency); SubprocessManager.AcquirePersistent
//     is the entry point. Persistent sessions are recorded in the
//     subprocess_sessions SQLite table (migration 048) so a daemon restart
//     can re-bind to the surviving subprocess (or re-spawn from history if
//     the subprocess is gone).
//
// Single IPC primitive: stdio JSON-RPC. Every Session, ephemeral or
// persistent, exposes Send/Receive on the same Message type; the underlying
// transport is openclaude --stdio. inv-zen-086 (stdio canonical) is enforced
// by the absence of any HTTP server constructor in this package.
//
// Invariant inv-zen-074 (TTL eviction enforcement): a goroutine in
// SubprocessManager polls every 60 s and sends SIGTERM to any persistent
// session whose last-use timestamp is older than the doctrine-bounded TTL,
// then SIGKILL after a 10 s grace period.
//
// Crash detector (the "SIGCHLD detector" in spec lore): a separate
// SubprocessManager goroutine polls each persistent entry's exitCh on the
// same cadence. When a concrete subprocess has exited outside the TTL
// evictor path (i.e., crashed or self-terminated), the detector closes
// the Session, drops the registry slot, and removes the SQLite row so
// the next AcquirePersistent spawns fresh. Polling exitCh (rather than
// installing a SIGCHLD signal handler) avoids the contention between
// raw SIGCHLD and os/exec's internal cmd.Wait, which both compete for
// the wait4 syscall and would otherwise produce stuck reaping.
//
// Both background goroutines (TTL evictor + crash detector) shut down
// cleanly when SubprocessManager.Shutdown closes the shared shutdownCh.
//
// Invariant inv-zen-031 (boundary): this package never imports
// internal/store. Persistence is reached via the SessionStore interface
// (see manager.go) and the CheckpointStore interface (see recovery.go),
// satisfied by Phase G daemon adapter.
//
// Concurrency every exported type in this package is safe for
// concurrent use unless its docstring says otherwise.
//
//   - SubprocessManager: all methods (SpawnEphemeral, AcquirePersistent,
//     Release, Shutdown) are safe to call from any goroutine. The
//     manager owns its internal mutex; callers do not need to serialize.
//     Shutdown blocks until both background goroutines (TTL evictor +
//     crash detector) have exited and all live sessions have been Closed.
//
//   - Session: Send and Receive are safe to call from any goroutine,
//     including concurrently with each other. Frames flow through
//     internal channels in FIFO arrival order. Close is safe to call
//     from any goroutine and is idempotent (subsequent calls return the
//     same error captured by the first call). Send/Receive after Close
//     return ErrSessionClosed; ctx cancellation returns ctx.Err().
//
//   - WorkerSpecRef and Message: pure value types; trivially safe to
//     copy and pass between goroutines.
//
// Phase D Worker authors can rely on these guarantees without re-reading
// the implementation.
//
// Phase H' status: this package is exercised in unit + integration tests
// against tests/testharness/openclaude_fake.go. Real openclaude binary
// integration arrives in Phase D realworld tests once Phase H' Task #55
// lands.
package subprocess
