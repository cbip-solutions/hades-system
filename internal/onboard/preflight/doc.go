// SPDX-License-Identifier: MIT
// Package preflight implements Plan 13 preflight gates.
//
// Per spec §3.10 + §5.2 + §8.1: every onboarding surface (zen config
// init / zen new / zen init / zen migrate claude-code) runs preflight
// BEFORE accepting operator input. Preflight failure exits 3 (distinct
// from generic I/O 1 / user error 2 / conflict 4).
//
// Phase A ships three checks (Plan 19 DECISION L-4: gitnexus removed; caronte
// is in-process):
//   - Hermes binary + version (inv-zen-175 ≥0.13.0)
//   - Plugin format remnants (inv-zen-176; legacy CC/OpenClaude format halts)
//   - Daemon socket (optional; Status=skip if absent per Q5=C+)
//
// Surface forms (per master plan §"Cross-phase type sharing" + Phase
// A C4 + N1 + Phase D consumer audit):
//
//   - Struct-based `Check` interface — every concrete check implements
//     `Name() string` + `Run(ctx) Result`. The Preflight orchestrator
//     aggregates them in parallel (bounded concurrency=2). Consumed by
//     Phase F doctor aggregator (mechanical adapter).
//   - Package-level helpers — `CheckHermesInstalled(ctx) error` /
//     `CheckPluginFormatRemnants(ctx, dirs ...string) error` /
//     `CCDetect() (present, configRoot, err)` /
//     `HermesVersion() (*Version, error)` /
//     `HermesCheck(ctx) (ok bool, version string, err error)`.
//     These are the Phase C/D/E entry points and wrap the struct-based
//     checks with the simpler error-or-nil + bool signatures the CLI
//     callers want.
//
// Per spec §3.4 boundary discipline (inv-zen-031): this package does
// NOT import `internal/store`. Audit emit lives in Phase C/D CLI
// wrappers which call `internal/audit/chain/` after preflight pass.
//
// Per "no new go.mod deps" doctrine and Stage-0 reality finding:
// the plan template referenced Masterminds/semver; this package
// instead defines a local Version type + comparison helpers built on
// stdlib regexp + strconv.
package preflight
