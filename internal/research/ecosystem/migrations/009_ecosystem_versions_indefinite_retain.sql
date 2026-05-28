-- internal/research/ecosystem/migrations/009_ecosystem_versions_indefinite_retain.sql
--
-- HADES design stage task. per design contract
--
-- Operator-confirmed retention: `hades docs pin --ecosystem X --version Y`
-- sets indefinite_retain=true so the version is excluded from the
-- 2-prior-stable retention window (design choice "Pinned: ... never archived
-- nor pruned"). `hades docs prune --confirm` consults this column before
-- deleting; pinned versions are refused.
--
-- Default 0 (false): newly ingested versions follow the standard
-- retention window unless explicitly pinned by the operator.
--
-- SchemaVersion bump: 1 -> 2. Idempotent: ALTER TABLE ADD COLUMN is
-- safe to re-run because the migration runner short-circuits when the
-- meta-table version >= SchemaVersion. The CHECK constraint pins the
-- column to a strict 0|1 boolean view (SQLite stores as INTEGER).

ALTER TABLE ecosystem_versions
    ADD COLUMN indefinite_retain INTEGER NOT NULL DEFAULT 0
        CHECK (indefinite_retain IN (0, 1));
