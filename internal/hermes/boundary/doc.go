// SPDX-License-Identifier: MIT
// Package boundary is the single Hermes-touching surface for hades-system
// (Plan 15 Phase H-12; decisión 7-b consolidation per Stage-0 reconciliation
// memo `reference_plan_15_v1_public_release_decisions.md`).
//
// # Why this package exists
//
// Pre-H-12, the Hermes-touching surface was spread across three places:
//   - `internal/daemon/transport/zenswarm_transport.go` — Go-side
//     ZenSwarmTransport (providers.TierBackend adapter; preserved in place
//     per inv-zen-164 compile-anchor grep).
//   - `plugin/hades/__init__.py` + `plugin/hades/hooks/` — Python-side
//     plugin registrations + PreCompletion hooks + skin + status helpers.
//   - `internal/daemon/mcpgateway/server.go` — ad-hoc MCP CallToolResult
//     envelope wrapping at multiple handler sites.
//
// This spread surface made upstream Hermes API churn risky — a single API
// change could break multiple disconnected sites, multiplying the blast
// radius of "fast-follow compat point-release" work (see
// `docs/operations/hermes-compat.md` 7-day commitment).
//
// Per Stage-0 reconciliation decisión 7-b, the operator chose to
// consolidate all NEW Hermes-touching Go code through this package.
//
// # What this package provides
//
//   - Surface interface declaring every Hermes-touching capability
//     (SendCompletion, RegisterStatusProvider, OnSessionStart,
//     OnPreToolCall, RenderInlinePrompt, WrapMCPEnvelope).
//   - Adapter concrete implementation delegating to a HermesCli
//     dependency-injected backend (production wires the real Hermes
//     CLI subprocess + HTTP boundary; tests wire fakes).
//   - Capability feature-detection helpers for G2/G3/G5 graceful
//     degrade (see docs/operations/hermes-compat.md per-divergence catalog).
//   - Sentinel error ErrCapabilityUnavailable returned by capability-gated
//     methods when the underlying Hermes version lacks the expected API.
//   - Boundary lint (`scripts/verify_no_direct_hermes_imports.sh`)
//     preventing future regression: no direct `hermes_cli` /
//     `hermes_agent` imports outside this package.
//
// # Why ZenSwarmTransport stays in internal/daemon/transport/
//
// The existing `internal/daemon/transport/zenswarm_transport.go` is
// preserved in place because the inv-zen-164 compile-anchor grep checks
// that literal file path (see
// tests/compliance/inv_zen_164_zenswarm_transport_single_egress_test.go:73).
// Moving it would break the compliance test grep. The boundary package
// provides a constructor (NewTransportFromZenSwarm) that wraps the
// existing transport behind the Surface interface — preserving inv-zen-164
// while still delivering decisión 7-b consolidation for NEW code.
//
// # Boundary lint scope
//
// `scripts/verify_no_direct_hermes_imports.sh` asserts no direct
// `hermes_cli` / `hermes_agent` Go imports OUTSIDE this package. The
// Python plugin (`plugin/hades/`) is out of scope (it IS the Hermes
// plugin — Python-side Hermes imports there are by design). The
// existing `internal/daemon/transport/` package does NOT import any
// `hermes_cli` Go symbol — it operates entirely at the HTTP boundary
// (POST /v1/messages from Python ZenSwarmTransport class). The boundary
// lint therefore passes cleanly today AND prevents any future Go-side
// drift.
//
// # See also
//
//   - ADR-0117 — Hermes boundary adapter consolidation rationale.
//   - ADR-0080 — substrate pivot to hermes-agent (the why behind H-12).
//   - `docs/operations/hermes-compat.md` — G2/G3/G4/G5 divergence catalog +
//     fast-follow commitment.
//   - `internal/daemon/transport/` — preserved ZenSwarmTransport
//     (inv-zen-164 compile-anchor); the boundary wraps but does not move.
package boundary
