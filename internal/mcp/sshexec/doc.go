// SPDX-License-Identifier: MIT
// Package sshexec implements the production-grade ssh-exec MCP.
//
// Per Q8 C (spec §1, §2.2 Capa 3, §3.5 Flow 5), the package exposes 3
// MCP tools over stdio JSON-RPC:
//
// - validate(cmd, project) -> {ok, reason, pattern}
// - exec(host, cmd, cwd, timeout, project) -> streaming chunks + result
// - list_allowed(project) -> {patterns, hosts, source}
//
// # Security model (spec §7.1 row "SSH-exec")
//
// 1. Validator (validator.go) — strict-prefix-match + forbidden-chars
// scan over ;&|$`<>(){}[]"' plus glob metachars *?~ (
// hardening). invariant.
// 2. Exec (exec.go) — golang.org/x/crypto/ssh direct (no spawn ssh
// binary); ForceCommand pattern; PTY=false; SSH credentials only via
// SSH_AUTH_SOCK; host keys verified via known_hosts.
// 3. Interactive detector (interactive.go) — first-1024-bytes pattern
// detector + SIGKILL within 100ms on detect; security-grade audit
// emit always notified. invariant.
// 4. Audit emit (emit.go) — outbound HTTP via internal/mcp/client/emit.go
// to daemon /v1/audit/emit per attempt (started/completed/denied/
// interactive_blocked). invariant no-loss.
// 5. Allowlist (allowlist.go) — doctrine config + per-project
// zenswarm.toml [ssh_exec] merge; ceiling enforced.
//
// # Defense in depth
//
// ForceCommand server-side wrapper bin/zen-ssh-exec-wrapper.sh (deployed
// via `zen ssh-exec setup-host <h>`) re-validates the allowlist at the
// SSH server. Even if the client validator is bypassed, the remote
// refuses non-allowlist commands.
//
// # Boundary
//
// This package does NOT import internal/store. State persistence is
// delegated to internal/daemon/handlers/audit_emit.go via outbound
// HTTP. Compile-check anchor is the absence of any internal/store
// import in any file in this package.
//
// # Compile-check anchors for invariants
//
// - invariant: Detector type is sealed (no public constructor —
// newDetector is unexported and called only by Run). Callers
// therefore CANNOT construct a Detector that bypasses Run's
// trigger handling.
// - invariant: Run signature requires a non-zero ValidationResult
// with OK=true; a missing validate step is a compile error.
// - invariant: server.go uses mcp.StdioTransport only; absence of
// net.Listen / http.ListenAndServe in this package is the static
// guarantee.
package sshexec
