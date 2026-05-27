-- ==============================================================================
-- Migration 061: Knowledge index extension hooks (the release design release track; spec §1 Q17 D)
-- ==============================================================================
--
-- IMPORTANT: This migration is NOT applied via internal/store/schema.go's
-- migrations slice. The knowledge index lives in a SEPARATE SQLite database
-- at ~/.cache/hades-system/knowledge-index/index.db (per spec §1 Q16 + §2.1 +
-- §2.4 the release design = migrations 057-061 reservation, §2.4 final entry).
-- The actual schema is materialized at runtime by internal/knowledge/index.go
-- Init function. This .sql file serves as:
--   (a) cross-Plan documentation (the release design hash-chain + the release design RAG +
--       gitnexus-mcp grep here for the canonical extension-hook column list);
--   (b) `make verify-invariants` grep target (compliance tests inv-hades-129
--       and inv-hades-130 reference this file by path);
--   (c) operator audit point (`hades knowledge stats --schema` reads this file
--       and renders to operator).
--
-- The schema below MUST stay in lockstep with internal/knowledge/index.go's
-- Init function. The Go test TestSchemaParityWithMigrationFile in
-- internal/knowledge/index_test.go enforces structural parity in CI
-- (every column listed in the runtime schema must appear here, and the
-- FTS5 / index DDL fragments must be referenced).
--
-- Per spec §1 Q17 D + inv-hades-130: the three extension-hook columns
-- (audit_chain_anchor, ecosystem_join_keys, caronte_symbol_refs) ship NULL
-- by default in the release design the release design fills audit_chain_anchor at audit-event
-- materialization time (separate writer, separate boundary). the release design
-- fills ecosystem_join_keys at RAG-link discovery time. Caronte fills
-- caronte_symbol_refs at code-graph reverse-link discovery time (the release design).
-- the release design INSERT statements NEVER populate any of these three columns;
-- inv-hades-130 compliance test enforces.
--
-- Per spec §3.5 + §6.6: query path is structured-filter-first (uses indexes)
-- then FTS5 MATCH on the filtered subset, then rank (BM25 + recency + project
-- match). The composite index idx_knowledge_meta_project supports the
-- structured-filter prefix (project_id, file_type, last_modified DESC).
-- ==============================================================================

-- FTS5 virtual table — content-only, minimal schema. All metadata external.
-- Rationale (spec §1 Q17 D): FTS5 ALTER TABLE constraints would block the release design /
-- the release design / gitnexus column adds. Keeping FTS5 minimal + supplementary
-- metadata table separate is the correct shape.
CREATE VIRTUAL TABLE IF NOT EXISTS knowledge_fts USING fts5(content_text);

-- Supplementary metadata. Joined to knowledge_fts by rowid.
CREATE TABLE IF NOT EXISTS knowledge_meta (
    rowid                INTEGER PRIMARY KEY,            -- matches knowledge_fts.rowid
    file_path            TEXT    NOT NULL UNIQUE,
    project_id           TEXT,                           -- sha256(canonical-path); NULL for global research cache
    project_alias        TEXT,
    file_type            TEXT    NOT NULL CHECK (file_type IN ('memory','research','adr','spec','plan','handoff')),
    title                TEXT,
    frontmatter_json     TEXT,                           -- raw JSON of YAML frontmatter; NULL if absent/malformed
    last_modified        INTEGER NOT NULL,               -- Unix nanos
    last_indexed         INTEGER NOT NULL,               -- Unix nanos

    -- Extension-hook columns: NULL by default in the release design (inv-hades-130).
    audit_chain_anchor   TEXT,                           -- the release design fills (hash-chain anchor)
    ecosystem_join_keys  TEXT,                           -- the release design reads (URLs in content; JSON array)
    caronte_symbol_refs  TEXT                            -- Caronte reverse-links from markdown (JSON array; the release design)
);

-- Composite index supports the structured-filter prefix in §3.5 query flow.
CREATE INDEX IF NOT EXISTS idx_knowledge_meta_project
    ON knowledge_meta (project_id, file_type, last_modified DESC);

-- Supporting index for delete-by-path on file watcher unlink events.
CREATE INDEX IF NOT EXISTS idx_knowledge_meta_file_path
    ON knowledge_meta (file_path);
