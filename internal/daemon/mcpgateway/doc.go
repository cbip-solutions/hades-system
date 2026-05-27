// SPDX-License-Identifier: MIT
// Package mcpgateway implements the hades-ctld single HTTP MCP endpoint
// (Q1=B gateway aggregator pattern, spec §1). Hermes consumes ONE MCP
// endpoint URL; this gateway multiplexes internal Go MCPs (research /
// budget / audit / sshexec / codegen / caronte in-process engine).
// Industry-converged pattern — MetaMCP, ContextForge, Stacklok ToolHive
// (Q1 SOTA Report).
//
// Boundaries (lint + compliance enforced):
//
// inv-hades-165 Gateway aggregator dedupes tool registrations. If two
// downstream MCPs register the same canonical tool name
// (e.g. both budget + research export `cap_status`), the
// ToolRegistry rejects with ErrToolNameCollision. Compile
// anchor: AssertToolRegistryDedup. Runtime test:
// TestGatewayDedupOnConflict. Compliance test:
// tests/compliance/inv_hades_165_gateway_dedup_test.go.
//
// inv-hades-168 Gitnexus subprocess restart
// rate-limited (3 in 5min). No longer applies: the gitnexus
// subprocess is replaced by the in-process caronte engine
// (no restart needed). The invariant check file is preserved
// as historical record.
//
// inv-hades-031 internal/daemon/mcpgateway/* MUST NOT import
// internal/store. State access is bridged through the
// daemon storeadapter pattern ( requires no store
// access; if future phases need it, the bridge is added
// via internal/daemon/mcpgateway/storeadapter/).
//
// Tool name canonical form: "mcp_hades-system_<subsystem>_<tool>". Subsystem
// is one of: research, budget, audit, sshexec, codegen, caronte. Tool
// is the underlying tool name as the downstream MCP exposes it. The
// gateway DOES NOT mutate tool names beyond prefixing; downstream MCPs
// remain owners of their tool surface.
//
// RBAC layers (in evaluation order, ALL must allow):
// 1. Active doctrine filter (capa-firewall denies a configurable set)
// 2. Per-tool ACL (default-deny on unknown tool names)
// 3. Concurrency gate (Q8=C doctrine-tunable; max-scope=20, default=10,
// capa-firewall=5; queue depth 50)
//
// Failure handling (Q7=B robust):
// - caronte engine error → per-mode escalation (degraded with doctor warning)
// - per-mode escalation (autonomy → WAITING_FOR_CONFIRMATION;
// interactive → degraded with doctor warning)
// - tool dispatch panic → recover + audit emit; HTTP 500 to caller
// - downstream MCP not yet wired → 503 + Retry-After header
//
// The sentinel.go file carries compile-time anchors that, if accidentally
// removed by a future contributor, cause the inv-hades-165 / inv-hades-031
// compliance tests to FAIL — making structural drift loud rather than
// silent.
package mcpgateway

// substrateSeparated is a compile-time marker that this package compiles
// without importing internal/store. Removing the line MUST NOT cause a
// missing import; if a future contributor accidentally imports
// internal/store, the inv-hades-031 compliance test fails.
var _ = substrateSeparated()

func substrateSeparated() bool { return true }
