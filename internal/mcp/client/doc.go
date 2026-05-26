// SPDX-License-Identifier: MIT
// Package client provides a shared HTTP client library for the four
// zen-swarm MCP binaries (zen-mcp-research, zen-mcp-budget, zen-mcp-audit,
// zen-mcp-sshexec) to communicate with the zen-swarm-ctld daemon over a
// Unix domain socket.
//
// Decision Q9 B: MCPs are stdio-canonical processes. They do NOT run an HTTP
// server. All shared daemon state (research cache, audit events, budget data)
// is accessed by the MCPs as outbound HTTP clients calling daemon /v1/*
// endpoints. This package is the single implementation of that outbound
// client — imported by all 4 MCPs instead of each reimplementing HTTP
// plumbing.
//
// Security invariants enforced by this package:
//   - inv-zen-085: outbound requests from MCPs are restricted to the
//     daemon Unix socket plus a sealed allowedHosts whitelist
//     (arxiv.org, api.github.com, duckduckgo.com, configurable Firecrawl
//     host). Any request targeting a non-whitelisted host returns
//     ErrHostNotAllowed before network I/O.
//   - inv-zen-031: this package NEVER imports internal/store directly.
//     It is a pure MCP-side library.
//   - inv-zen-083: audit emit events are never silently discarded.
//     emit.go writes a local buffer file when the daemon is unreachable;
//     wires full drain-on-restart recovery via the EmitClient.DrainBuffer
//     and EmitClient.DrainAllBuffers methods.
//
// Auth the daemon auth token is read once at construction time from
// ~/.config/zen-swarm/auth-token (or a custom path in Config). The file
// MUST have mode 0600 or stricter (no group/world bits); New() rejects
// looser permissions to prevent accidental credential leaks. Every
// outgoing request carries "Authorization: Bearer <token>".
//
// Retry daemon endpoints retry up to 3 times with exponential backoff
// (base 100 ms, factor 2, max 800 ms) on 5xx responses or transport
// errors. Non-daemon backend calls (arxiv, github, duckduckgo) do NOT
// retry automatically — the individual MCP tool is responsible for its
// own retry policy on those backends. Classification is by URL host:
// see Client.Do for the full rule.
//
// Concurrency contracts:
//   - Client: safe for concurrent use after construction (immutable
//     post-init).
//   - CacheClient, BudgetClient: safe for concurrent use after
//     construction (no in-process mutable state).
//   - EmitClient: safe for concurrent Emit calls. DrainBuffer is also
//     safe to call concurrently with Emit on the same instance — it
//     uses a rotation pattern (rename live buffer to .draining, then
//     process the snapshot) so ongoing Emits append to a fresh buffer
//     while Drain works. DrainAllBuffers (orphan recovery) MUST be
//     called only at process startup, when no Emit is in flight.
package client
