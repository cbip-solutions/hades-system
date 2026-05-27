// SPDX-License-Identifier: MIT
// Package research implements the zen-mcp-research MCP server.
//
// (research-SOTA-always-integrated). It exposes 7 deterministic tools
// (web_search, arxiv, github_search, code_graph, ecosystem_docs,
// synthesize, cite) plus an opt-in agentic refinement wrapper
// (agentic_deep) per decision Q4 C in
// internal design record
// §1.
//
// Architecture
// - server.go — modelcontextprotocol/go-sdk stdio server +
// registration of the 8 tools.
// - dispatch.go — fan-out parallel; aggregator URL-keyed dedup;
// min-source threshold; citation-verification
// gate; pre-check budget.
// - agentic.go — Q4 C agentic_deep wrapper; gap-detection +
// saturation + budget terminate.
// - web_search.go — DDG via daemon-routed search + Firecrawl
// full-page extraction.
// - arxiv.go — REST API export.arxiv.org + XML parse.
// - github_search.go — go-github + auth via macOS Keychain.
// - gitnexus_client.go— long-lived gitnexus mcp child via Go SDK MCP
// client; health probe; restart policy
// (max 3 restarts in 5 min before hard-fail emit). Includes the
// compile-time GitnexusClient assertion (formerly in
// code_graph.go which was folded post-review I-2).
// - ecosystem_docs.go — SHIPPED; backed by
// internal/research/ecosystem/Dispatcher (full corpus RAG with
// embeddings, FTS5, RRF fusion, BGE reranker, Bayesian abstention,
// citation grammar, symbol verification cascade, LLM-judge re-pass).
// - synthesize.go — calls dispatcher via daemon HTTP
// /v1/messages with X-Zen-Profile=
// research-synthesize.
// - cite.go — RawCitation + VerifiedCitation + HEAD-probe
// verifier + markdown formatter +
// OTel GenAI structured JSON.
// - cache.go — wraps internal/mcp/client/cache.go;
// cache hash sha256(query+sources+iter).
//
// Boundary
// - invariant — does NOT import internal/store; persistence reaches
// the daemon via internal/mcp/client/.
// - invariant — outbound HTTP via internal/mcp/client/http.go's
// whitelist (arxiv.org, export.arxiv.org,
// api.github.com, duckduckgo.com, daemon socket;
// firecrawl.dev added via Config.AllowedHosts).
// - invariant — stdio canonical: server.go uses only
// mcp.NewStdioServer; no net.Listen anywhere.
// - invariant — every backend dispatch is wrapped in budget.PreCall;
// CI grep rule + tests/compliance/inv_zen_076_test.go
// enforce.
// - invariant — VerifiedCitation type-distinct from RawCitation;
// only VerifiedCitation flows to cite.Format and
// downstream synthesizer.
package research
