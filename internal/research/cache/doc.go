// SPDX-License-Identifier: MIT
// Package cache owns research_cache.db — the SQLite database that stores
// research dispatch records, findings, validation logs, and a sqlite-vec
// virtual table for semantic query deduplication.
//
// # Architecture boundary (invariant)
//
// This package MUST NOT import internal/store. Each database owns its own
// *sql.DB handle and schema-version bookkeeping. research_cache.db is fully
// isolated from the daemon's main store.db and from aggregator.db.
// Cross-DB joins are intentionally prohibited: the research subsystem reads
// findings via its own SQL API, not via store queries.
//
// # SQLite driver choice
//
// research_cache.db uses mattn/go-sqlite3 (CGO) — the same driver as
// internal/knowledge/aggregator — to host the sqlite-vec C extension. The
// ncruces/go-sqlite3 (pure-Go WASM) driver cannot host the sqlite-vec C
// extension because sqlite3_auto_extension is unavailable in the WASM sandbox.
//
// Two drivers in one binary (mattn for cache + aggregator; ncruces for
// internal/store) are intentional and authorised (lines
// 151-160). The mattn driver registers under the "sqlite3" name; ncruces
// under a renamed driver name so they coexist without collision.
//
// # Schema versioning
//
// _cache_schema_version holds exactly one row with the current schema
// version integer. Open is idempotent: re-opening an existing DB is a
// no-op (every CREATE uses IF NOT EXISTS); the version row is inserted
// with INSERT OR IGNORE to prevent duplicate rows across re-opens.
//
// # sqlite-vec virtual table
//
// research_query_vec is a vec0(float[384]) virtual table used for KNN
// lookup of previously-dispatched query embeddings to detect semantic
// duplicates (cache-hit path). The 384-dimension matches Model2Vec
// gte-small, which is the lightweight embedding model used by the
// research-cache subsystem (distinct from the mpnet-base-v2 / gte-small
// used by knowledge_pin_vec — each cache owns an independent
// embedding domain).
//
// # CGO requirement
//
// This package requires CGO_ENABLED=1. There is no !cgo fallback stub
// because the research-cache subsystem is not on the daemon's critical path
// . A binary built
// with CGO_ENABLED=0 will fail to compile this package, which is the
// correct signal that sqlite-vec is unavailable for research caching.
package cache
