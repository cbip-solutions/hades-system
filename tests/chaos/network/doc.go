//go:build chaos

// SPDX-License-Identifier: MIT

// Package network implements Toxiproxy-driven chaos scenarios
// across the 8 zen-swarm daemon HTTP edges (inv-zen-305).
//
// # Scenario matrix
//
// 80 scenarios = 10 toxic types × 8 daemon edges. Toxic types per
// Toxiproxy v2 canonical: latency, bandwidth, slow_close, reset_peer,
// limit_data, timeout, slicer, down, modify_buffer, modify_rate.
// Daemon edges: hermes_plugin, ctld, providers_anthropic_paygo,
// providers_gemini, mcp_research, mcp_budget, mcp_audit, sidecar_bypass
// (the bypass tier; the canonical sidecar backend post-Plan-16 cascade).
//
// # Why Toxiproxy
//
// Per arXiv 2505.13654 (May 2025) industry survey, network faults are
// 40% of all real production faults — the largest single category.
// Toxiproxy (Shopify, MIT, 2013-present) is the canonical SOTA TCP
// proxy daemon for this class of testing; it's battle-tested in
// Shopify production. The 80-scenario matrix exercises the high-
// leverage interactions: e.g., latency on the sidecar edge tests
// circuit-breaker trip; reset_peer on the dispatcher edge tests retry
// + fallback; slow_close on hermes_plugin tests plugin-RPC timeout.
//
// # Asserted invariants
//
// Each scenario asserts inv-zen-305: the daemon survives the toxicity
// by exercising the documented robustness path (retry / circuit-trip
// / tier-fallback / audit emission / idempotent rerun). A scenario
// failure means the documented robustness path did NOT engage.
//
// # Skip-on-no-Toxiproxy
//
// Tests skip cleanly (t.Skip) when the Toxiproxy daemon is not
// reachable at the canonical control URL (loaded from
// ~/.config/zen-swarm/toxiproxy-dev.json per F-1). This lets
// developers without the dev daemon get a clean green local build
// while CI (which always has the sidecar service) runs the full
// matrix.
//
// The unconditional table-only tests
// (TestAllToxicTypesHasExactlyTen, TestAllToxicTypesAreDistinct,
// TestGenerateScenariosCartesianProduct) do not require Toxiproxy
// and exercise pure scenario-expansion semantics so contract
// regressions surface even when the daemon is unavailable.
//
// # Build tag
//
// All files in this package live under `//go:build chaos`. Default
// `go build` / `go test ./...` skips them; CI invokes via
// `make test-chaos-network`, which sets the tag.
//
// # Spec
//
// inv-zen-305 contract spec; see CHANGELOG for narrative.
package network
