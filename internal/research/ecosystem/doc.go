// SPDX-License-Identifier: MIT
// internal/research/ecosystem/doc.go
//
// Package ecosystem implements release — ecosystem-documentation RAG
// (Layer 4 of the 4-tier knowledge-aggregator boundary; see
// docs/operations/knowledge-aggregator-boundary.md).
//
// # Scope
//
// TypeScript, Rust) — stdlib + top-5000 packages + MDN web platform +
// arXiv research papers + GitHub top-1000-starred READMEs/docs — and
// serves them via:
// - MCP capability `research.ecosystem_docs(query, ecosystem?, version?, scope?)`
// - CLI commands `zen knowledge query --remote`, `zen memory`, `zen specs`, `zen docs`
//
// # Architecture
//
// ingester → chunker → embedder
// ↓ ↓
// indexer ecosystem.db ← 4 SQLite DBs
// ↓
// dispatcher.Query query path
// router → fan-out → RRF → rerank → verify → abstain → audit → emit
//
// # Boundaries
//
// this package MUST NOT import:
// - internal/store (direct DB ops; use aggregator + indexer abstractions)
// - net/http (HTTP egress; use internal/research/cache.Revalidator.Fetch)
// - internal/daemon/budget (declare own RAGAuditChainEmitter narrow interface)
// - internal/caronte/*
//
// this package MAY import:
// - internal/knowledge/aggregator
// - internal/research/cache
// - internal/doctrine/active
// - internal/audit/chain
// - internal/orchestrator/eventlog
//
// # 15 invariants
//
// See internal design record §5.
// Compile-time enforced where applicable ( vet analyzer
// `no_web_in_ecosystem` for invariant); runtime enforced via property
// tests; CI enforced via tests/property/ecosystem/ + tests/compliance/.
//
// # Phase status (frozen at write time)
//
// (foundation): types + interface skeletons + EventType slots 92-99
// - Revalidator.Fetch primitive + schema migrations + per-source TTL config
//
// (ingestion): chunker (cAST + Contextual Retrieval) + 7 source fetchers
// (embedding): jina-code-1.5B + Matryoshka two-stage + symbol-index
// (query): Dispatcher.Query 14-step orchestration
// (version + Δ): VersionDetector cascade + ChangeExtractor
// (surface): MCP + 4 CLI namespaces + ecosystem_join_keys populator
// (ops): BudgetMonitor + zen docs commands + cron launchd
// (tests): adversarial suite + 12 property + 2 compliance tests + vet analyzer
// (release): 5 ADRs 0087-0091 + handbooks + v0.14.0 tag candidate
package ecosystem
