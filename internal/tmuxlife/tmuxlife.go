// SPDX-License-Identifier: MIT
// Package tmuxlife orchestrates per-project tmux sessions for hades-system.
//
// Each registered project gets a dedicated tmux session named
// "hades-<alias>-<sha8>" on a separate socket /tmp/hades-system.sock (NEVER the
// default /tmp/tmux-<uid> socket). Sessions contain 6 windows: 5
// daemon-owned (orch, leads, workers, hra, logs) and 1 operator-owned
// (scratch). Lifecycle is hybrid lazy: spawned on cwd-activation, explicit
// `hades attach`, scheduled-job firing, or autonomous-mode resume. Idle
// sessions are reaped per doctrine TTL (max-scope=∞, default=24h,
// capa-firewall=4h) after a tmux-resurrect snapshot (excluding scratch).
//
// Drift detection is forensic, NOT enforcing: a 5-second poller compares
// daemon-owned panes against the expected set in daemon.db and emits
// TmuxLayoutDriftDetected events but never auto-reverts. Recovery is
// operator-invoked via `hades layout repaint <alias>`.
//
// Boundary (invariant): tmuxlife does NOT import internal/store. Storage
// access flows through the SessionStore interface (declared in session.go,
// added in C-2..C-4) implemented by internal/daemon/handlers/sessions.go
// . The package depends only on the Go standard library.
//
// Invariants enforced:
// - invariant: every tmux invocation includes -S /tmp/hades-system.sock.
// ExecTmux panics on -S absence; SocketPath const is the single source.
// - invariant: scratch window contents NEVER serialized to snapshot.
// Save() writes tmux-resurrect config excluding :scratch and validates
// post-tar that no scratch sentinel surfaced (added in C-9).
// - invariant: idle TTL applied per doctrine. DoctrineIdleTTL maps the
// three standard doctrines; per-project override via hadessystem.toml is
// read at activation time and threaded through
// the IdleReaper.doctrineFor callback (added in C-10).
package tmuxlife

import "errors"

// SocketPath is the canonical hades-system tmux socket path.
//
// invariant: every tmux invocation MUST include -S SocketPath; the default
// tmux socket /tmp/tmux-<uid> is forbidden because it would contaminate the
// operator's regular tmux namespace with hades-spawned sessions.
//
// File permissions are 0600 owner-only after first creation (set by tmux
// itself; daemon does NOT chmod). HADES design spec §7.3 "Tmux contamination
// prevention Layer 3" documents the permission model.
const SocketPath = "/tmp/hades-system.sock"

const SnapshotDirSubpath = ".config/hades-system/tmux-snapshots"

const IdleTTLInfinity = -1

var ErrSessionNotFound = errors.New("tmuxlife: session not found")

var ErrSessionExists = errors.New("tmuxlife: session already exists")

var ErrTmuxNotInstalled = errors.New("tmuxlife: tmux binary not found in PATH; install tmux (brew install tmux on macOS)")

// ErrTmuxVersionTooOld tmux version < 3.4 (required for tmux-resurrect's
// new-style format-strings used in our save_pane_contents script). Daemon
// proceeds in degraded mode (no snapshot) and emits a warning; spec §4.1.
var ErrTmuxVersionTooOld = errors.New("tmuxlife: tmux version below 3.4; snapshot disabled (upgrade tmux for full feature set)")

var ErrSnapshotCorrupt = errors.New("tmuxlife: snapshot archive corrupt; preserved at *.corrupted-<ts>; spawn fresh session")

var ErrScratchExclusionViolated = errors.New("tmuxlife: scratch window content detected in snapshot; invariant violated; aborted")
