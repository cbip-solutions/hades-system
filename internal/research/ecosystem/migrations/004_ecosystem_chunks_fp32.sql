-- internal/research/ecosystem/migrations/004_ecosystem_chunks_fp32.sql
--
-- Plan 14 Phase A Task A-9. Per spec §3.4.
--
-- Sidecar table for FP32 1536-d embeddings (rerank stage). 1 row per
-- chunks row; PK = chunk_id (FK to ecosystem_chunks). 6144 bytes per
-- embedding (1536 float32). Kept separate from main chunks table to
-- avoid bloating row scans during binary-256d Hamming top-200 stage.

CREATE TABLE IF NOT EXISTS ecosystem_chunks_fp32 (
    chunk_id        INTEGER PRIMARY KEY REFERENCES ecosystem_chunks(id) ON DELETE CASCADE,
    embedding_blob  BLOB NOT NULL
);
