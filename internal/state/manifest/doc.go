// SPDX-License-Identifier: MIT
// Package manifest implements the docs/system-state.toml substrate per
//
// The package owns:
//
// - Manifest, Section value types mirroring the TOML structure
// - Walker orchestrator + per-section sub-walkers in walkers/
// - Regenerator (preserve-manual + emit-toml)
// - Differ for `zen state verify`
// - ManualTracker (detect manual changes + emit events)
// - AutonomyValidator ( prereq validators consumed
// orchestrator's autonomy gate)
// - RegenerateWatcher (fsnotify-driven background goroutine per §3.7)
// - 3 typed events (state.manual_field_changed, state.regenerate_partial,
// state.regenerated)
//
// Invariants enforced by this package:
//
// - invariant: never imports internal/store; chain integration flows
// via internal/audit/chain + internal/daemon/auditadapter
// - invariant: docs/system-state.toml freshness < 7d (manifest.Differ
// reports stale; CI gate `zen state verify` fails) UNLESS recent
// state.manual_field_changed events compensate (operator-pinned
// freshness via the chain anchor)
// - invariant: regenerate-and-diff CI gate via `make verify-system-state`
// integrated into `make verify-invariants`
//
// Threat model coverage (§7.1 T10): system-state.toml manual field silent
// tampering — defense is regenerate-and-diff CI gate + manual field change
// MUST emit event con reason + freshness compliance + the chain anchor
// makes silent post-write modification detectable cryptographically.
package manifest
