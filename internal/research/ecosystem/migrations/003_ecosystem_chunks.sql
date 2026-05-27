-- internal/research/ecosystem/migrations/003_ecosystem_chunks.sql
--
-- HADES design release track Task A-9. Per spec §3.4.
--
-- The core chunk table. release track chunker emits these via indexer.WriteChunks;
-- release track embedder fills embedding_binary_256d in the same write transaction.
-- chunk_fingerprint = sha256(content_text); cross-version dedup hot path.
-- parent_chunk_id links leaf chunks (~512 tokens) to parent chunks (~2048
-- tokens) for the LlamaIndex HierarchicalNodeParser auto-merging pattern.
--
-- The 3 chunk-table indexes (idx_chunks_pkg_version + idx_chunks_symbol_path
-- + idx_chunks_fingerprint) are declared in this same migration so the
-- table + its indexes are atomic from the runner's perspective (one .sql
-- file → one apply).

CREATE TABLE IF NOT EXISTS ecosystem_chunks (
    id                    INTEGER PRIMARY KEY AUTOINCREMENT,
    package_id            INTEGER NOT NULL REFERENCES ecosystem_packages(id) ON DELETE CASCADE,
    version_introduced    TEXT NOT NULL,
    version_deprecated    TEXT,
    stable_in_json        TEXT NOT NULL DEFAULT '[]',
    content_text          TEXT NOT NULL,
    contextual_prefix     TEXT,
    chunk_fingerprint     TEXT NOT NULL,
    parent_chunk_id       INTEGER REFERENCES ecosystem_chunks(id) ON DELETE SET NULL,
    source_type           TEXT NOT NULL,
    symbol_path           TEXT,
    kind                  TEXT,
    source_url            TEXT NOT NULL,
    embedding_binary_256d BLOB,
    -- release stage amendment 2026-05-15: tracks cAST chunker boundary-preservation
    -- overflows. 1 = chunk exceeded maxLeafTokens to preserve a tree-sitter
    -- node boundary; weekly sweep query targets oversized=1 for re-chunking.
    oversized             INTEGER NOT NULL DEFAULT 0,
    CHECK (length(stable_in_json) >= 2),  -- minimum is "[]"
    CHECK (oversized IN (0, 1))
);

CREATE INDEX IF NOT EXISTS idx_chunks_pkg_version  ON ecosystem_chunks(package_id, version_introduced);
CREATE INDEX IF NOT EXISTS idx_chunks_symbol_path  ON ecosystem_chunks(symbol_path);
CREATE INDEX IF NOT EXISTS idx_chunks_fingerprint  ON ecosystem_chunks(chunk_fingerprint);
