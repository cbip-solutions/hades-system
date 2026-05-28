// SPDX-License-Identifier: MIT
// Package preflight implements HADES design preflight gates.
//
// per design contract§5.2 + §8.1: every onboarding surface (hades config
// init / hades new / hades init / hades migrate claude-code) runs preflight
// BEFORE accepting operator input. Preflight failure exits 3 (distinct
// from generic I/O 1 / user error 2 / conflict 4).
//
// ships three checks (HADES design decision L-4: gitnexus removed; caronte
// is in-process):
// - Hermes binary + version (invariant ≥0.13.0)
// - Plugin format remnants (invariant; legacy CC/OpenClaude format halts)
// - Daemon socket (optional; Status=skip if absent per design choice+)
//
// Surface forms (per master plan §"Cross-stage type sharing" + stage
// A C4 + N1 + consumer audit):
//
// - Struct-based `Check` interface — every concrete check implements
// `Name() string` + `Run(ctx) Result`. The Preflight orchestrator
// aggregates them in parallel (bounded concurrency=2). Consumed by
// doctor aggregator (mechanical adapter).
// - Package-level helpers — `CheckHermesInstalled(ctx) error` /
// `CheckPluginFormatRemnants(ctx, dirs...string) error` /
// `CCDetect() (present, configRoot, err)` /
// `HermesVersion() (*Version, error)` /
// `HermesCheck(ctx) (ok bool, version string, err error)`.
// These are the entry points and wrap the struct-based
// checks with the simpler error-or-nil + bool signatures the CLI
// callers want.
//
// per design contract(invariant): this package does
// NOT import `internal/store`. Audit emit lives in CLI
// wrappers which call `internal/audit/chain/` after preflight pass.
//
// Per "no new go.mod deps" doctrine and reality finding:
// the plan template referenced Masterminds/semver; this package
// instead defines a local Version type + comparison helpers built on
// stdlib regexp + strconv.
package preflight
