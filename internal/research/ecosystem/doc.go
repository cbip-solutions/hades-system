// SPDX-License-Identifier: MIT
// internal/research/ecosystem/doc.go
//
// Package ecosystem implements Plan 14 — ecosystem-documentation RAG
// (Layer 4 of the 4-tier knowledge-aggregator boundary; see
// docs/operations/knowledge-aggregator-boundary.md).
//
// # Scope
//
// TypeScript, Rust) — stdlib + top-5000 packages + MDN web platform +
// arXiv research papers + GitHub top-1000-starred READMEs/docs — and
// serves them via:
//   - MCP capability `research.ecosystem_docs(query, ecosystem?, version?, scope?)`
//   - CLI commands `zen knowledge query --remote`, `zen memory`, `zen specs`, `zen docs`
//
// # Architecture
//
//	ingester (Phase B) → chunker (Phase B) → embedder (Phase C)
//	    ↓                                       ↓
//	indexer (Phase C)                       ecosystem.db ← 4 SQLite DBs
//	                                           ↓
//	dispatcher.Query (Phase D)              query path
//	  router → fan-out → RRF → rerank → verify → abstain → audit → emit
//
// # Boundaries (inv-zen-031 preserved)
//
//	this package MUST NOT import:
//	  - internal/store (direct DB ops; use aggregator + indexer abstractions)
//	  - net/http (HTTP egress; use internal/research/cache.Revalidator.Fetch)
//	  - internal/daemon/budget (declare own RAGAuditChainEmitter narrow interface)
//	  - internal/caronte/* (project code-graph is orthogonal per ADR-0007/Plan 19; inv-zen-201)
//
//	this package MAY import:
//	  - internal/knowledge/aggregator (Plan 9 D substrate)
//	  - internal/research/cache (Plan 9 F substrate; Revalidator.Fetch added Phase A A-2)
//	  - internal/doctrine/active (Plan 8 substrate via Accessor)
//	  - internal/audit/chain (Plan 9 B primitives; wrapped by RAGAuditChainEmitter)
//	  - internal/orchestrator/eventlog (EventType constants; slots 92-99 declared Phase A A-1)
//
// # 15 invariants (inv-zen-191..205)
//
// See docs/superpowers/specs/2026-05-14-zen-swarm-plan-14-ecosystem-rag-design.md §5.
// Compile-time enforced where applicable (Phase H vet analyzer
// `no_web_in_ecosystem` for inv-zen-191); runtime enforced via property
// tests; CI enforced via tests/property/ecosystem/ + tests/compliance/.
//
// # Phase status (frozen at write time)
//
// Phase A (foundation): types + interface skeletons + EventType slots 92-99
//   - Revalidator.Fetch primitive + schema migrations + per-source TTL config
//
// Phase B (ingestion): chunker (cAST + Contextual Retrieval) + 7 source fetchers
// Phase C (embedding): jina-code-1.5B + Matryoshka two-stage + symbol-index
// Phase D (query): Dispatcher.Query 14-step orchestration
// Phase E (version + Δ): VersionDetector cascade + ChangeExtractor
// Phase F (surface): MCP + 4 CLI namespaces + ecosystem_join_keys populator
// Phase G (ops): BudgetMonitor + zen docs commands + cron launchd
// Phase H (tests): adversarial suite + 12 property + 2 compliance tests + vet analyzer
// Phase I (release): 5 ADRs 0087-0091 + handbooks + v0.14.0 tag candidate
package ecosystem
