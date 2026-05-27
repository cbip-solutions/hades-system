// SPDX-License-Identifier: MIT
// Package auth implements the zen-swarm-ctld HTTP auth boundary
// .
//
// Three authentication classes:
//
// 1. Unix-socket peer-cred (default for /v1/*; PeerCredOnly middleware).
// Caller must connect via UDS /tmp/zen-swarm.sock (mode 0700) OR via
// loopback TCP if the daemon was started with --http 127.0.0.1:<port>.
//
// 2. Daemon-bearer (POST /v1/events/handoff_posted; RequireDaemonBearer).
// Plugin writes a structured event after writing HANDOFF.md. The
// daemon-bearer token is generated at boot (rotates with daemon
// restart) and persisted to ~/.config/zen-swarm/daemon-bearer.txt
// (mode 0600) for the plugin to read.
//
// 3. Per-routine bearer (POST /v1/schedules/{id}/fire;
// RequirePerRoutineBearer). Per-routine token persists as a sha256
// hash in daemon.db schedules.bearer_token_hash; cleartext shown
// once at routine creation time and never
// re-shown. Mismatch → 401 + ScheduleHttpTriggerAuthFailed audit
// event (action-needed if 5+ in 1h per spec §4.3).
//
// All three classes use crypto/subtle.ConstantTimeCompare for token
// comparison. The package never imports internal/store
// ; the per-routine token store is reached via the
// HTTPTokenStore interface defined locally and satisfied by
// scheduler-side adapter.
//
// Build tags isolate per-OS peer-cred extraction:
// - unix_peer_darwin.go (LOCAL_PEERCRED via golang.org/x/sys/unix)
// - unix_peer_linux.go (SO_PEERCRED via golang.org/x/sys/unix)
// - unix_peer_other.go (returns ErrPeerCredUnsupported; daemon refuses Start)
//
// invariant: every /v1/* route (except the two bearer endpoints) MUST
// be wrapped by PeerCredOnly. Compliance test enforces.
//
// invariant: per-routine bearer MUST use ConstantTimeCompare AND emit
// ScheduleHttpTriggerAuthFailed on mismatch. Compliance test enforces.
package auth
