// SPDX-License-Identifier: MIT
// Package audit implements the audit_review MCP tool.
//
// # Scope
//
// This package exposes a single MCP tool, audit_review, that reviews a diff
// against a named criteria set and returns a structured verdict produced by
// a provider-family-disjoint LLM reviewer.
//
// This package does NOT write to any database. It is an outbound HTTP client
// to the release dispatcher.
//
// # Invariant inv-hades-080
//
// The reviewer provider family MUST be disjoint from the generator provider
// family. The Pool sealed constructor enforces:
// - len(pool) >= 2 after excluding the generator family
// - generator family is absent from the chosen reviewer family
//
// This is a hard rule. Any code path that calls pool.Choose() without
// first constructing Pool via NewPool() is a violation of inv-hades-080.
//
// # release boundary
//
// NO imports of internal/store. NO SQL. NO hash-chain logic. NO OTel emit.
// Those belong to release.
package audit
