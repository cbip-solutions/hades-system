// SPDX-License-Identifier: MIT
// Package safetynet implements the 4-element self-hosting safety-net
// the Anthropic Apr 23 chicken-and-egg evidence makes non-optional:
//
//   - prev:       pinned-prior-version fallback binary (bin/zen-prev).
//   - divergence: config-snapshot comparator (operator-active vs substrate-session).
//   - regression: per-commit substrate health metric (substrate_health table).
//   - drift:      doctrine-lint over substrate's commits (severity hard|soft).
//
// inv-zen-031 boundary: this package NEVER imports internal/store directly.
// Persistence flows through SubstrateHealthWriter (regression.go); the
// production adapter lives in internal/daemon/orchestratoradapter/ (Phase N).
//
// inv-zen-096: drift findings with severity=hard transition the orchestrator
// state machine to HARD_PAUSED — the load-bearing halt-the-build behaviour.
package safetynet
