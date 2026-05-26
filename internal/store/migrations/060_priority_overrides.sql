-- Migration 060: priority_overrides + tmux_session_state (Plan 7 Phase B+C joint).
--
-- Reservation: per master plan §"Migration numbering coordination", 060 is the
-- joint migration shared by Phase B (priority_overrides) and Phase C
-- (tmux_session_state). Phase B-6 ships priority_overrides because the quota
-- override storage seam needs it now. Phase C will add tmux_session_state in a
-- subsequent migration (e.g., 060a or a new number) — Phase B leaving room
-- here would invite a half-empty migration; Phase C owns its own DDL.
--
-- inv-zen-115 (audit chain integrity): every priority_overrides mutation MUST
-- emit a row in the events table inside the same transaction. The Go-layer
-- helpers (UpsertPriorityOverrideTx + InsertEventTx in
-- internal/store/priority_overrides.go) compose the multi-statement atomic
-- write; this migration only ships the table.
--
-- Constraints:
--   - project_alias TEXT NOT NULL UNIQUE: at most one override per project.
--                   UPSERT semantics replace the prior row + emit a `replaced`
--                   audit event ahead of the `set` event (quota.priority_boost.{replaced,set}).
--   - multiplier REAL NOT NULL CHECK(multiplier > 0): defence in depth against a
--                   hand-edited DB row carrying an invalid multiplier.
--                   internal/quota/override.go validates the same range
--                   (>0 + <=100) on Set; SQL CHECK is the floor.
--   - expires_at TIMESTAMP NOT NULL: the absolute UTC instant after which the
--                   override is auto-removed. The boost-expiry sweeper goroutine
--                   (Phase B-9) physically removes rows where expires_at < now.
--   - reason TEXT NOT NULL: audit trail demands operator intent (spec §1 Q10).
--                   internal/quota/override.go rejects empty/whitespace reasons.
--   - created_at TIMESTAMP NOT NULL: the wall-clock instant Set was invoked.
--                   ORDER BY created_at DESC drives `zen project priority --ls`.
--
-- Index strategy:
--   - UNIQUE(project_alias) is implicitly indexed (PRIMARY KEY-equivalent).
--   - idx_priority_overrides_expires_at accelerates the sweeper's range scan
--     for `WHERE expires_at < ?` — the sweeper runs every ~30s; a sequential
--     scan would be wasteful at scale.
--
-- No FK to projects_alias(alias):
--   The priority_overrides table tracks aliases by string; an FK would either
--   require ON DELETE CASCADE (silently lose audit-relevant overrides when a
--   project is archived) or block project deletion. Both are wrong:
--   - silent loss violates inv-zen-115 (audit chain integrity).
--   - blocking deletion couples lifecycle. Operator-driven `zen project rm`
--     should also Reset the override; if the override survives, ListPriorityOverrides
--     returns it as a dangling row that the next `zen project priority --ls`
--     surfaces (forensic visibility). The override sweeper additionally GCs
--     by expires_at so dangling rows expire on their own TTL.

CREATE TABLE IF NOT EXISTS priority_overrides (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    project_alias   TEXT      NOT NULL UNIQUE,
    multiplier      REAL      NOT NULL CHECK(multiplier > 0),
    expires_at      TIMESTAMP NOT NULL,
    reason          TEXT      NOT NULL,
    created_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_priority_overrides_expires_at
    ON priority_overrides(expires_at);
