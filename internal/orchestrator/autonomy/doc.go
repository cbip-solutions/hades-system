// SPDX-License-Identifier: MIT
// Package autonomy implements the 3-layer autonomy mode resolver and the
// per-doctrine prerequisite check engine for Plan 5 Stage 4 Build.
//
// Mode resolution (Q11 C, top wins):
//
//  1. Per-build flag --autonomy=manual|semi|full
//  2. Per-project zenswarm.toml [autonomy] default = "..."
//  3. Doctrine default (max-scope=semi, default=manual, capa-firewall=manual)
//
// inv-zen-100 (capa-firewall hard guard): when the doctrine is
// "capa-firewall", Resolve always returns ModeManual irrespective of any
// override, and records the attempted override in Resolution.RejectedOverride
// so the caller can emit AutonomyOverrideRejected to the event log.
//
// Check engine (Q13 D, hard/soft/informational tiers per doctrine) is in
// check.go; the per-doctrine tier matrix is in tiers.go.
//
// Boundary (inv-zen-089): this package MUST NOT import internal/store,
// internal/queue, or workforce. It is a pure-Go decision/policy package.
package autonomy
