// SPDX-License-Identifier: MIT
// Package transport implements the daemon-side counterpart of zen-swarm's
// ZenSwarmTransport — the cross-language LLM-dispatch bridge between the
// Python Hermes plugin and the Go daemon's dispatcher.
//
// # Architecture
//
// Per spec §1 Q1=B AND the post-
// implementation Hermes audit amendment (spike_01_provider_transport_abc.md
// 2026-05-15), ZenSwarmTransport is split across two layers, but Hermes
// itself drives a THIRD path — a declarative ProviderProfile, NOT subclassing
// of a Hermes ABC — which is the production routing path. Concretely:
//
// - Python side (plugin/zen-swarm/transports/zen_swarm_transport.py):
// a standalone class (no Hermes ABC subclass — none exists for HTTP
// substitution; Hermes' agent/transports.ProviderTransport is a format
// adapter only). Forwards completions via HTTP to the daemon's
// /v1/messages endpoint. Consumed by direct callers — zen CLI Python
// wrappers, MCPs, integration tests, future automation hooks — NOT by
// Hermes' main LLM loop.
//
// - Go side (this package): receives the forwarded HTTP requests at
// /v1/messages and dispatches via the existing dispatcher chain
// (BypassBackend → fail). Satisfies the providers.TierBackend interface
// so the compile-time anchor proves the daemon side honours the same
// contract.
//
// - Hermes-driven path (plugin/zen-swarm/providers/__init__.py): registers a
// ProviderProfile(name="zen-swarm", api_mode="anthropic_messages",
// base_url=daemon socket). Hermes' AIAgent loads the profile and uses
// anthropic.Anthropic(base_url=...) to POST native Anthropic Messages
// to the daemon's NewAnthropicProxy handler (already wired at
// internal/daemon/server.go:639-648). No translation layer required.
// The /zen-swarm:install-mcps slash command writes the provider entry
// to ~/.hermes/config.yaml + symlinks the plugin into the model-providers
// directory so Hermes auto-loads it.
//
// # Why three paths
//
// The three paths are NOT redundant — they enforce invariant single-egress
// from three angles:
//
// 1. Python class: covers direct callers (CLI Python wrappers, MCPs, tests)
// that bypass Hermes entirely. Any code embedding the class POSTs via
// the daemon, never directly to upstream providers.
//
// 2. Go side: the daemon's /v1/messages endpoint is the single chokepoint
// for LLM forwarding. anthropic_proxy.go already hands every
// request to dispatcher.Forward; this package wraps that with a
// transport-aware shim that emits a Tessera anchor when the call
// originates from a Hermes session (X-Zen-Transport: zenswarm header
// present).
//
// 3. Hermes ProviderProfile + NewAnthropicProxy: Hermes' main agent's LLM
// loop selects the zen-swarm provider and dispatches via the Anthropic
// SDK against the daemon socket. invariant compliance scans verify
// no second LLM path exists in plugin Python source.
//
// # Compile-time invariant
//
// The ZenSwarmTransport type satisfies providers.TierBackend at the Go
// surface. This is enforced by the compile-time guard at package scope:
//
// var _ providers.TierBackend = (*ZenSwarmTransport)(nil)
//
// Any future change to the TierBackend contract that breaks ZenSwarmTransport
// will fail at compile-time, not at first dispatch. The compliance test in
// tests/compliance/inv_zen_164_*_test.go scans for this guard line.
//
// # Boundary
//
// This package imports providers + redact
// only. It does NOT import internal/store. The audit-anchor and dispatcher
// dependencies are expressed as abstract interfaces declared in this
// package; the daemon bootstrap supplies the concrete implementations from
// internal/audit/chain and internal/daemon/dispatcher respectively.
//
// # Cross-phase type discipline
//
// Per master plan §"Cross-phase type discipline":
// - This package owns: ZenSwarmTransport, MessagesHandler, ForwardedRequest,
// ForwardedResponse, Dispatcher (interface), AuditAnchor (interface).
// - mcpgateway owns: Server, Aggregator, ToolRegistry, RBAC,
// CaronteProxy.
// - augment owns: Pipeline, DoctrineGate, BudgetGate, etc.
// - citation owns: Envelope, Renderer, MarkdownFallback.
//
// No type collision across packages.
//
// # Nil-dependency policy (reviewer M4)
//
// Constructors in this package fail-fast on missing dependencies:
// passing a nil dispatcher to NewMessagesHandler / NewZenSwarmTransport
// is a wiring bug at daemon bootstrap and panics here rather than at
// first request. The sibling internal/daemon/handlers package follows
// the same discipline (Augment panics on nil DoctrineReader); see
// internal/daemon/handlers/doc.go § "Nil-dependency policy" for the
// cross-package rationale. Optional dependencies with documented
// graceful-degradation paths (e.g. audit anchors at nil → no
// emit) MAY be nil; required engines MUST NOT.
package transport
