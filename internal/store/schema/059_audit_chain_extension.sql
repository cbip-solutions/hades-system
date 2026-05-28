-- schemaVersion: 29
-- HADES design stage — audit_events_raw chain integration (design choice C decision).
--
-- Adds four chain columns + REFUSE triggers + monthly partition view +
-- audit_partition_seals CRUD table. Chain hashes are computed in
-- app-layer (auditadapter post-INSERT same-row UPDATE) — no SQL trigger
-- recursion. invariant enforced via REFUSE triggers.
--
-- Boundary (invariant): writes go through auditadapter (which imports
-- both store + chain). The chain layer (internal/audit/chain) NEVER
-- imports internal/store; it operates on chain.EventStore interface
-- satisfied by auditadapter.
--
-- Schema additions:
--
--   audit_events_raw (existing):
--     prev_hash       TEXT NOT NULL DEFAULT ''   — sha256 hex of previous record_hash
--     record_hash     TEXT NOT NULL DEFAULT ''   — sha256 hex(prev_hash || type || payload_json || emitted_at)
--     partition_id    TEXT NOT NULL DEFAULT ''   — strftime('%Y_%m', emitted_at, 'unixepoch')
--     tessera_leaf_id TEXT     NULL              — populated post-batch by auditadapter
--
--   REFUSE triggers (BEFORE UPDATE OF append-only columns; BEFORE DELETE):
--     Append-only columns: id, project_id, type, payload_json, emitted_at, prev_hash, record_hash
--     Mutable columns (post-insert chain compute path):
--       partition_id (set once during chain compute UPDATE)
--       tessera_leaf_id (set once when batch is sealed; NULL → non-NULL)
--
--   audit_events_partitions VIEW: aggregate per-partition stats
--     (first_id, last_id, final_record_hash, event_count)
--
--   audit_partition_seals TABLE: monthly seal records
--     PK partition_id; FK-style references audit_events_partitions logically
--
-- HADES design chain integrity invariants:
--   invariant: audit_events_raw append-only — REFUSE triggers enforce
--   (UPDATE permitted only on chain compute path which targets the
--    chain columns explicitly via UPDATE … SET col=… WHERE col=''
--    — the trigger condition checks the OLD row's chain columns).

-- ======================================================================
-- Chain integrity columns
-- ======================================================================

ALTER TABLE audit_events_raw ADD COLUMN prev_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_events_raw ADD COLUMN record_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_events_raw ADD COLUMN partition_id TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_events_raw ADD COLUMN tessera_leaf_id TEXT;

-- Index on (partition_id, id) for chain walker partition iteration
CREATE INDEX IF NOT EXISTS idx_audit_events_raw_partition ON audit_events_raw(partition_id, id);
-- Index on tessera_leaf_id IS NULL for batch worker recovery sweep
CREATE INDEX IF NOT EXISTS idx_audit_events_raw_tessera_leaf ON audit_events_raw(tessera_leaf_id) WHERE tessera_leaf_id IS NULL;
-- Index on record_hash for chain integrity verification
CREATE INDEX IF NOT EXISTS idx_audit_events_raw_record_hash ON audit_events_raw(record_hash) WHERE record_hash != '';

-- ======================================================================
-- REFUSE triggers — append-only enforcement (invariant)
-- ======================================================================
--
-- Strategy: BEFORE UPDATE of append-only columns refuses;
-- BEFORE UPDATE of chain-compute columns is permitted ONLY when the
-- pre-image had empty chain columns (one-time write).
-- BEFORE DELETE refuses unconditionally.

-- Refuse UPDATE attempts that touch the truly immutable columns
-- (id, project_id, type, payload_json, emitted_at).
-- These columns are set at INSERT and never change again.
CREATE TRIGGER IF NOT EXISTS audit_events_raw_no_update_immutable
BEFORE UPDATE OF id, project_id, type, payload_json, emitted_at ON audit_events_raw
BEGIN
    SELECT RAISE(FAIL, 'audit_events_raw is append-only (HADES design chain integrity invariant); immutable columns id/project_id/type/payload_json/emitted_at cannot be modified');
END;

-- Refuse UPDATE attempts that touch prev_hash or record_hash AFTER they
-- have been set (one-time write only — chain compute happens once per row).
-- The trigger fires when OLD.prev_hash or OLD.record_hash is non-empty
-- AND the new value differs.
CREATE TRIGGER IF NOT EXISTS audit_events_raw_no_update_chain_hashes
BEFORE UPDATE OF prev_hash, record_hash ON audit_events_raw
WHEN OLD.prev_hash != '' OR OLD.record_hash != ''
BEGIN
    SELECT RAISE(FAIL, 'audit_events_raw is append-only (HADES design chain integrity invariant); chain hashes cannot be rewritten once computed');
END;

-- Refuse UPDATE attempts that touch partition_id AFTER it has been set
-- (partition derivation is deterministic; never re-computed).
CREATE TRIGGER IF NOT EXISTS audit_events_raw_no_update_partition
BEFORE UPDATE OF partition_id ON audit_events_raw
WHEN OLD.partition_id != ''
BEGIN
    SELECT RAISE(FAIL, 'audit_events_raw is append-only (HADES design chain integrity invariant); partition_id cannot be rewritten');
END;

-- Refuse UPDATE attempts that overwrite tessera_leaf_id once set.
-- (Batch worker writes the leaf_id once; recovery sweep also sets it
--  once if NULL after restart — both are NULL → non-NULL transitions.)
CREATE TRIGGER IF NOT EXISTS audit_events_raw_no_update_tessera_leaf
BEFORE UPDATE OF tessera_leaf_id ON audit_events_raw
WHEN OLD.tessera_leaf_id IS NOT NULL
BEGIN
    SELECT RAISE(FAIL, 'audit_events_raw is append-only (HADES design chain integrity invariant); tessera_leaf_id cannot be rewritten once batch sealed');
END;

-- DELETE is unconditionally refused.
CREATE TRIGGER IF NOT EXISTS audit_events_raw_no_delete
BEFORE DELETE ON audit_events_raw
BEGIN
    SELECT RAISE(FAIL, 'audit_events_raw is append-only (HADES design chain integrity invariant); DELETE is forbidden');
END;

-- ======================================================================
-- Monthly partition view
-- ======================================================================

CREATE VIEW IF NOT EXISTS audit_events_partitions AS
SELECT
    partition_id,
    MIN(id)        AS first_id,
    MAX(id)        AS last_id,
    COUNT(*)       AS event_count,
    -- final_record_hash: the chain tip of this partition (last row by id).
    -- Computed via correlated subquery — SQLite supports this; alternative
    -- is a window function but views with window functions are SQLite 3.40+
    -- and we keep portability. The subquery cost is negligible (indexed scan).
    (SELECT record_hash FROM audit_events_raw a2
     WHERE a2.partition_id = audit_events_raw.partition_id
       AND a2.id = (SELECT MAX(id) FROM audit_events_raw a3 WHERE a3.partition_id = audit_events_raw.partition_id)
    ) AS final_record_hash
FROM audit_events_raw
WHERE partition_id != ''
GROUP BY partition_id;

-- ======================================================================
-- Partition seal table
-- ======================================================================

CREATE TABLE IF NOT EXISTS audit_partition_seals (
    partition_id              TEXT    NOT NULL PRIMARY KEY,
    sealed_at                 INTEGER NOT NULL CHECK (sealed_at > 0),
    final_record_hash         TEXT    NOT NULL CHECK (length(final_record_hash) = 64),
    tessera_seal_leaf_id      TEXT    NOT NULL,
    daemon_witness_signature  TEXT    NOT NULL CHECK (length(daemon_witness_signature) > 0),
    cold_archive_url          TEXT,
    cold_archive_content_hash TEXT
);

CREATE INDEX IF NOT EXISTS idx_partition_seals_sealed_at ON audit_partition_seals(sealed_at);
