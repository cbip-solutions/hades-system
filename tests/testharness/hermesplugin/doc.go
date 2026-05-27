// SPDX-License-Identifier: MIT
// Package hermesplugin — FakeHermes httptest harness A-3.
//
// Provides an in-process Tier 2 stub of the Hermes plugin runtime, used
// by tests/integration/hermes_plugin_smoke_test.go to verify plugin
// discovery, slash command registration, skill loading, MCP server
// registration, and citation envelope round-trip across renderers without
// requiring a real `hermes` binary.
//
// Tier 3 (`tests/realworld/hermes_plugin_real_test.go`) exercises the
// SAME contract against the real `hermes` binary; the FakeHermes surface
// mirrors the real Hermes plugin API per ADR-0080 + amendment.
//
// Per amendment §2.3 D-3: Tier 2 covers fast PR-gate path; Tier 3 covers
// pre-release confidence. No compromise between speed and confidence.
package hermesplugin
