// SPDX-License-Identifier: MIT
// Package budget implements the zen-swarm budget MCP server.
//
// The server exposes seven stdio tools wrapping the daemon /v1/budget/* HTTP
// endpoints via the internal/mcp/client typed wrapper (Phase H). All budget
// logic — multi-axis accounting, z-score anomaly detection, hierarchical
// hard-pause state machine — lives in internal/budget/ (Phase F) and is
// accessed exclusively through the daemon HTTP API (Q9 B: daemon owns shared
// state).
//
// Boundary invariants:
//   - inv-zen-031: this package NEVER imports internal/store, internal/budget/,
//     or internal/daemon/. Only internal/mcp/client/ and stdlib.
//   - inv-zen-086: no HTTP server code in this package; go-sdk stdio canonical.
//   - inv-zen-085: the HTTP client (Phase H) enforces daemon-socket-only egress.
//
// (internal/store/cost_ledger.go) is the single write-path for cost rows.
// Phase J never inserts cost rows; the tag tool uses /v1/budget/record to
// post axis attribution only (AmountUSD=0).
//
// Daemon semantic of POST /v1/budget/record amount_usd=0: the daemon treats
// a record with zero amount as an axis-only attribution against an existing
// cost_ledger row identified by cost_id (no new ledger insert). Idempotency
// is enforced server-side via INSERT OR IGNORE on (cost_id, axis_name); a
// repeat tag call returns ok=true without error.
package budget
