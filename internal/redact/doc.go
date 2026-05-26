// SPDX-License-Identifier: MIT
// Package redact centralises every primitive that prevents OAuth tokens,
// API keys, and other credentials from leaking via logs, dumps, panics,
// or audit rows.
//
// Three layers cooperate (decision Q12 D — three-layer redaction —
// recorded in docs/superpowers/plans/2026-04-29-plan-2-bypass-master.md
// row Phase B, and detailed in
// docs/superpowers/plans/2026-04-29-plan-2-phase-B-redact-package.md):
//
//  1. Secret []byte: a value type whose String, MarshalJSON, MarshalText,
//     Format and GoString implementations all emit "[REDACTED]". The
//     plaintext is only available via the explicit Reveal() method,
//     which a small audit (grep on "\.Reveal(") can enumerate.
//
//  2. Logger: a wrapper around *log.Logger whose Output passes every
//     emission through ScrubBytes before forwarding to the underlying
//     writer. Use NewLogger(w, prefix, flag) wherever the bypass module
//     would otherwise call log.New.
//
//  3. RedactingTransport: an http.RoundTripper that wraps an inner
//     transport. It (a) sets Authorization via the Secret type so any
//     accidental dump never sees the bearer token, (b) exposes
//     SafeDumpRequest / SafeDumpResponse helpers that produce
//     httputil-style output with credentials replaced by [REDACTED].
//
// A fourth layer, the compile-time check in compile_check.go, links a
// symbol whose presence is verified by `make verify-invariants` to enforce
// that no struct field whose name contains "Token" / "Credential" / "OAuth"
// uses raw string in private-tier1-module.
//
// The package itself imports nothing from private-tier1-module to
// keep the dependency graph one-directional (bypass imports redact;
// redact never imports bypass).
package redact
