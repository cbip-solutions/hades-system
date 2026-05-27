// SPDX-License-Identifier: MIT
// Package checks ships the 11 prerequisite check implementations consumed by
// internal/orchestrator/autonomy.CheckEngine. Each Check is constructed via a
// New<Name> function that takes a Deps bundle, returning a value satisfying
// the autonomy.Check interface.
//
// The 4 cluster patterns:
//
// - Cluster A — MCP up: research_mcp_up, caronte_engine_up, watcher_running
// (HTTP probe via injected HTTPClient, 2s timeout)
// - Cluster B — file freshness: caronte_index_currency, system_state_toml,
// amendment_dry_run_approved (FileStat ModTime vs doctrine-keyed threshold)
// - Cluster C — content validity: adrs_valid, verify_docs (Execer.Run + exit code)
// - Cluster D — repo health: lint_clean, plans_4_9_green, ci_consecutive_green
// (Execer.Run + JSON status file parse)
//
// Production wiring uses checks.All(Deps) which returns the slice in
// autonomy.AllCheckNames() order; the parent CheckEngine then sorts results
// by canonical index regardless of registration order.
//
// Boundary (inv-hades-089): this package, like its parent, MUST NOT import
// internal/store, internal/queue, or workforce. All I/O is via the small
// HTTPClient / FileStat / Execer / FileReader interfaces in env.go so unit
// tests inject fakes.
package checks
