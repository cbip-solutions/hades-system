// SPDX-License-Identifier: MIT
// Package mcp implements Plan 13's 4-tier curated MCP set + Q7=D
// smart-default detection.
//
// Per spec §2.7 + §3.7 + Q7=D:
//
//	Tier 1 (Mandatory)       — cannot uncheck: zen-swarm-ctld
//	Tier 2 (Universal)       — default-yes for all kinds: playwright,
//	                           filesystem, github
//	Tier 3 (Smart-default)   — default-yes IF zen recognize detects
//	                           relevant signals: prisma-postgres,
//	                           sentry, linear, memory, sequential-thinking
//	Tier 4 (Catalog opt-in)  — wizard customize or `zen mcp add`:
//	                           sqlite, graphql, mysql, openapi
//
// Each entry carries a RiskTier (low/medium/high) consumed by Phase F
// doctrine eval (Q10=D); the field is populated NOW so Phase F has no
// retrofit work to do.
//
// Per spec §8.6 inv-zen-179: smart-default Detected fns enforce a
// confidence ≥0.6 threshold; below that, even positive signals do not
// enable the MCP. The threshold is centralized in
// SmartDefaultConfidenceThreshold + confidenceGate() (defense-in-depth:
// every Detected fn calls confidenceGate before signal extraction).
//
// Per inv-zen-181 (full invariant lands Phase F; compile-check substrate
// sits here): AssertAllTiered runs at package init and panics if any
// catalog entry has Tier=0 or empty RiskTier — programmer errors caught
// at compile-test rather than at first user call.
//
// recognize.Result is Phase B's output, consumed directly by Phase A's
// smart_default.go (C7 reconciliation 2026-05-14: no shim package).
// Phase A scaffolds the Result type at internal/recognize/result.go so
// the cross-phase compile dependency closes; Phase B extends the
// recognize package with detection logic (manifest/, config/, etc.)
// without redefining Result.
//
// # Boundary discipline
//
// Per inv-zen-031: this package NEVER imports internal/store. Catalog
// entries and smart-default detection are pure value types + pure
// functions; audit emit (if any) lives in the daemon layer downstream
// via internal/audit/chain/.
package mcp
