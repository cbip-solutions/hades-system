-- internal/research/ecosystem/migrations/007_ecosystem_chunks_fts.sql
--
-- Plan 14 Phase A Task A-9. Per spec §3.4.
--
-- FTS5 virtual table over the chunks' searchable text columns. Plan 14
-- Phase D dispatcher fan-out runs an FTS5 BM25 query in parallel with
-- the binary-256d Hamming-distance vector query, then RRF-fuses.
--
-- tokenize=unicode61 matches the Plan 9 D aggregator FTS pattern.
--
-- The sqlite-vec ecosystem_chunks_vec_bin virtual table is also declared
-- here for atomicity. BIT[256] is the sqlite-vec native binary vector
-- dimension (256 bits = 32 bytes); native Hamming-distance MATCH operator.
--
-- The vec0 module is registered process-globally via sqlite_vec.Auto()
-- inside migrations.go ApplyMigrations BEFORE this CREATE VIRTUAL TABLE
-- runs; the auto-extension entry-point fires for every connection in the
-- pool (mirrors Plan 9 D aggregator/db.go + Plan 9 F cache/db.go pattern).

CREATE VIRTUAL TABLE IF NOT EXISTS ecosystem_chunks_fts USING fts5(
    chunk_id UNINDEXED,
    content_text,
    contextual_prefix,
    symbol_path,
    tokenize='unicode61'
);

CREATE VIRTUAL TABLE IF NOT EXISTS ecosystem_chunks_vec_bin USING vec0(
    chunk_id INTEGER PRIMARY KEY,
    embedding BIT[256]
);
