-- Migration 036: pin registry (Q7 D).
-- Pinned conversations survive the nightly retention purge regardless of age.
-- Operator pins via `zen bypass pin <id>` (the release design release track CLI).
CREATE TABLE IF NOT EXISTS bypass_audit_pins (
    conversation_id TEXT    PRIMARY KEY,
    pinned_at       INTEGER NOT NULL,
    reason          TEXT
);
