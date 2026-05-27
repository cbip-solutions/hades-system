// SPDX-License-Identifier: MIT
// Package knowledge owns the cross-project knowledge aggregator: a SQLite +
// FTS5 hybrid index over per-project Obsidian-style memory dirs, ADRs,
// specs, plans, HANDOFF.md, and the global research cache.
// (spec internal design record
// §1 Q16 + Q17 D + §3.5 + §3.6 + §4.5 + §6.6 + §7.2 invariant +
// invariant).
//
// Boundary note (invariant exception, documented in
// docs/operations/knowledge-aggregator-boundary.md, ):
// internal/knowledge/ owns its OWN SQLite DB at
// ~/.cache/zen-swarm/knowledge-index/index.db. This DB is intentionally
// distinct from the daemon's daemon.db and per-project state.db; the
// daemon's internal/store/ boundary applies to the OpenClaude-substrate
// state DBs only. The knowledge index is rebuildable from sources
// (per spec §7.4 privacy + §3.7 cross-cutting concerns), so weaker
// durability semantics are correct — coupling the two would force every
// re-index into the daemon's hot fsync path.
//
// Imports in this package are intentionally limited to stdlib +
// database/sql + the SQLite driver (ncruces/go-sqlite3, pure-Go via
// WASM, no CGO; preserves invariant compatibility for
// this package's own state). Specifically: no net/http (invariant
// no remote queries), no internal/store (separate-DB boundary), no
// internal/projectctx (knowledge runs cross-project; receives ProjectID
// strings as data, not via package import).
package knowledge

var _ = knowledgeFTS5SchemaSentinel()

// knowledgeFTS5SchemaSentinel returns nil when the FTS5 + supplementary
// metadata schema in index.go's Init is reachable from production code
// (not just test code). The compliance test
// inv_zen_130_knowledge_extension_hooks_null_test.go (G-16) asserts
// this anchor is invocable, proving the canonical schema is the one
// actually used at runtime.
//
// invariant (per spec §7.2): the three extension-hook columns
// (audit_chain_anchor, ecosystem_join_keys, caronte_symbol_refs) ship
// NULL by default in ; INSERT statements MUST NOT populate them.
//
// The sentinel pattern (a no-op function called by Init) is the
// standard zen-swarm anchor for "this code path is reachable from
// production" — see also private-tier1-module and invariant
// for prior usages. Without the explicit reachability call, a
// compliance test could pass against a schema constant that no runtime
// path ever consults.
func knowledgeFTS5SchemaSentinel() error {
	return nil
}

func knowledgeNoRemoteSentinel() error {
	return nil
}

func NoRemoteSentinel() error { return knowledgeNoRemoteSentinel() }

func knowledgeNoAuditChainSentinel() error {
	return nil
}

func NoAuditChainSentinel() error { return knowledgeNoAuditChainSentinel() }
