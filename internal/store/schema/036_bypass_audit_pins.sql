-- Migration 036: pin registry (design choice D).
-- Pinned conversations survive the nightly retention purge regardless of age.
-- Operator pins via `hades bypass pin <id>` (HADES design stage CLI).
CREATE TABLE IF NOT EXISTS bypass_audit_pins (
    conversation_id TEXT    PRIMARY KEY,
    pinned_at       INTEGER NOT NULL,
    reason          TEXT
);
