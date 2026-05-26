-- internal/research/ecosystem/migrations/005_ecosystem_symbols.sql
--
-- Plan 14 Phase A Task A-9. Per spec §3.4.
--
-- Per-package symbol registry. Phase C symbol_index.Register populates
-- this on every package ingest. Phase D verifier.go queries by symbol_path
-- as the O(1) primary lookup. introduced_in is part of UNIQUE so the same
-- symbol can appear across multiple versions (e.g., crypto/sha256.Sum256
-- in 1.22.0 + 1.23.0; same path, different introduced_in).

CREATE TABLE IF NOT EXISTS ecosystem_symbols (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    package_id      INTEGER NOT NULL REFERENCES ecosystem_packages(id) ON DELETE CASCADE,
    symbol_path     TEXT NOT NULL,
    kind            TEXT,
    signature       TEXT,
    introduced_in   TEXT,
    deprecated_in   TEXT,
    source_url      TEXT,
    UNIQUE (package_id, symbol_path, introduced_in)
);

CREATE INDEX IF NOT EXISTS idx_symbols_path ON ecosystem_symbols(symbol_path);
