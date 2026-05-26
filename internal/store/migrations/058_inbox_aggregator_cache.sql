-- Migration 058: per-project `inbox` table + daemon.db `inbox_aggregator_cache`
-- (Plan 7 Phase E Task E-1, Q11 C hybrid storage).
--
-- ============================================================================
-- Architecture (spec §3.3, Q11 C):
--
--   Per-project authoritative `inbox` table:
--     - severity 4-tier CHECK (inv-zen-124)
--     - 5min sliding-window dedup UNIQUE on
--       (event_type, content_hash, created_at_bucket)
--     - cascade-delete on project removal (handled at adapter layer; the
--       per-project DB file is unlinked, which atomically discards rows)
--
--   Daemon-level `inbox_aggregator_cache` denormalized read view:
--     - project_id + project_alias columns indexed
--     - written via outbox bridge on every per-project INSERT
--     - rebuildable cold from per-project sources (Aggregator.Rebuild)
--     - DeleteByProject(project_id) for cascade
--
-- The same DDL is applied to BOTH per-project state.db AND daemon.db; the
-- migration runner is idempotent and `CREATE TABLE IF NOT EXISTS` guards
-- both contexts. Per Q11 C, the daemon registers each per-project
-- state.db file with the same migration runner so the inbox table lives
-- on every per-project file; the inbox_aggregator_cache table is
-- ignored by per-project DBs (no writers reach it there) but present
-- (no penalty — empty table, idle).
--
-- ============================================================================
-- Bucket strategy — `created_at_bucket` stored explicitly:
--
-- The `created_at_bucket` column is **stored explicitly** rather than
-- computed via a SQLite generated stored column or a CHECK expression
-- because:
--
--   1. SQLite CHECK constraints can't be used as expressions for UNIQUE
--      indices.
--   2. A generated stored column adds version-compat complexity (SQLite
--      ≥3.31 only); we target a wider compatibility window.
--   3. Explicit bucket storage is the cleanest path: Go-side
--      `dedup.ComputeDedupKey` (Phase E-4) writes
--      `created_at_bucket = created_at / 300`; SQLite enforces UNIQUE
--      on the triple. Defense in depth: Go-side rejects malformed
--      bucket values BEFORE the SQL layer; SQLite UNIQUE is the floor.
--
-- ============================================================================
-- Inv-zen anchors (spec §7.2):
--
--   - inv-zen-113 (no cross-project leak): inbox_aggregator_cache rows
--     carry project_id matching the originating per-project source DB.
--     Compile-time anchor in inbox/sentinel.go (Phase E-2);
--     runtime via the outbox bridge writing project_id from the source
--     scope (Phase E-8); property-based fuzz test in
--     tests/compliance/inv_zen_113_no_cross_project_inbox_leak_test.go
--     (Phase E-14).
--
--   - inv-zen-124 (severity 4-tier CHECK enum): inbox.severity column +
--     inbox_aggregator_cache.severity column both enforce the 4-tier
--     enum at the SQL layer. Defense in depth: Go-side ValidSeverity
--     (Phase E-2) rejects first; SQL CHECK is the floor.
--
--   - inv-zen-031 (boundary): internal/inbox/* MUST NEVER import
--     internal/store. The internal/daemon/inboxadapter/ package
--     (Phase E-10) is the ONLY package permitted to bridge inbox
--     value types to *store.Store via this migration's tables.
--     Phase K's inv_zen_122 compliance test extends to enforce this
--     on Plan-7 packages (greps internal/inbox/*.go for forbidden
--     internal/store imports).
--
-- ============================================================================
-- Drift note (Plan 7 Phase E-1):
--
--   The original master plan §"Migration numbering coordination"
--   reserved slot 058 for Phase E inbox storage under an
--   execution-order sequence A → C → B → D → E in which Phase E ships
--   migrationV27 (HEAD baseline 23 → ... → 26 → 27).
--
--   Reality at HEAD on 2026-05-07:
--     - 057 taken by Phase A (projects_alias + path_history, V24)
--     - 060 taken by Phase B-6 (priority_overrides, V25)
--     - 062 taken by Phase C-11 (tmux_session_state, V26)
--     - 063 taken by Phase D-1 (schedules + schedule_history, V27)
--     - schemaVersion = 27 entering Phase E-1.
--
--   Phase E-1 therefore picks migrationV28 (next free) — the FILE
--   number stays at 058 (slot reserved for inbox per master plan
--   reconciliation; only the in-Go variable name shifts to V28).
--   schemaVersion bump path: 27 (Phase D-1) → 28 (this migration).
--
--   Slot 061 remains reserved for Phase G knowledge-index DB
--   (separate SQLite file, no daemon.db schemaVersion bump).
--
-- ============================================================================

-- ----------------------------------------------------------------------------
-- inbox table — per-project authoritative notification storage.
-- ----------------------------------------------------------------------------
--
-- One row per notification. id INTEGER PRIMARY KEY AUTOINCREMENT — the
-- `inbox_aggregator_cache.notification_id` column references this id at
-- the application layer (no FK; cache rows can outlive their authoritative
-- source if the per-project DB file is unlinked between cache write and
-- cascade sweep — the periodic Rebuild reconciles).
--
-- Constraints:
--
--   - id INTEGER PRIMARY KEY AUTOINCREMENT: standard append-only PK.
--                          Used by adapter Ack/Snooze (UPDATE BY id) and
--                          by the cache-fanout path (notification_id
--                          column reflects this).
--
--   - project_id TEXT NOT NULL: sha256 hex (projectctx.ProjectID).
--                          Stored on each row even on per-project DBs
--                          (denormalized intentionally — the same DDL
--                          applies to per-project + daemon.db, and on
--                          per-project DBs all rows share the same
--                          project_id; on daemon.db the column is
--                          unused for inbox but present for shape
--                          parity with the cache table).
--
--   - severity TEXT NOT NULL CHECK: 4-tier enum per inv-zen-124.
--                          Defense in depth — Go-side ValidSeverity
--                          rejects first; SQL CHECK is the floor.
--
--   - event_type TEXT NOT NULL: e.g. "hra.l4_alert", "job.failed",
--                          "doctrine.amendment.proposed". Maps to
--                          orchestrator/eventlog.EventType verbatim
--                          (string-based).
--
--   - content_hash TEXT NOT NULL: sha256 hex of canonical fields used
--                          by inbox/dedup.ComputeDedupKey (Phase E-4).
--                          The dedup contract: same content_hash within
--                          a 5min bucket → same notification, second
--                          write rejected by UNIQUE.
--
--   - payload TEXT NOT NULL DEFAULT '{}': JSON blob, schema-free at
--                          this layer. The Go-side decoder validates
--                          per event_type before surfacing to render.
--
--   - created_at INTEGER NOT NULL: UTC unix seconds (inv-zen-005).
--
--   - created_at_bucket INTEGER NOT NULL: created_at / 300 (5min). The
--                          dedup pivot. Stored explicitly (not generated)
--                          for SQLite UNIQUE compatibility.
--
--   - acked_at INTEGER: NULL if not acked. Set to UTC unix seconds on
--                          Ack. The idx_inbox_unacked partial index
--                          accelerates the hot "show me unacked"
--                          query path.
--
--   - snoozed_until INTEGER: NULL if not snoozed. UTC unix seconds.
--                          Sweep runs Phase E-12 (zen inbox snooze
--                          surfaces).
--
-- Index strategy:
--
--   - PRIMARY KEY(id) is implicitly indexed (used by Ack/Snooze).
--
--   - idx_inbox_project_severity_created (composite):
--                          accelerates `zen inbox --severity=...
--                          --since=...` queries on per-project + cache.
--                          Project_id leftmost so per-project queries
--                          stay in-index; severity filter chains down.
--
--   - idx_inbox_unacked (partial WHERE acked_at IS NULL):
--                          accelerates the unacked-only render path
--                          (most rows acked at steady state; the
--                          partial index avoids touching them).
--
-- UNIQUE strategy:
--
--   - UNIQUE (event_type, content_hash, created_at_bucket): 5min
--                          sliding-window dedup per Q11. The
--                          Go-side dedup module computes the bucket;
--                          the SQL UNIQUE is the floor. The Go layer
--                          translates a UNIQUE failure into
--                          ErrDedupViolation (Phase E-3 sentinel)
--                          plus driver-stable
--                          sqlite3.CONSTRAINT_UNIQUE (defense in
--                          depth — same predicate in two places).
--                          NOTE: dedup is intentionally per-project
--                          (cross-project hash collisions ⊨ privacy
--                          leak; per-project DBs separate the keyspace).

CREATE TABLE IF NOT EXISTS inbox (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id        TEXT NOT NULL,                  -- sha256 hex (projectctx.ProjectID)
    severity          TEXT NOT NULL CHECK (severity IN (
                          'urgent',
                          'action-needed',
                          'info-immediate',
                          'info-digest'
                      )),                              -- inv-zen-124
    event_type        TEXT NOT NULL,                  -- e.g. "hra.l4_alert"
    content_hash      TEXT NOT NULL,                  -- sha256 hex of canonical fields
    payload           TEXT NOT NULL DEFAULT '{}',     -- JSON blob
    created_at        INTEGER NOT NULL,               -- UTC unix seconds (inv-zen-005)
    created_at_bucket INTEGER NOT NULL,               -- created_at / 300 (5min dedup pivot)
    acked_at          INTEGER,                        -- NULL if not acked
    snoozed_until     INTEGER,                        -- NULL if not snoozed
    UNIQUE (event_type, content_hash, created_at_bucket)
);

CREATE INDEX IF NOT EXISTS idx_inbox_project_severity_created
    ON inbox (project_id, severity, created_at);

CREATE INDEX IF NOT EXISTS idx_inbox_unacked
    ON inbox (acked_at, severity, created_at)
    WHERE acked_at IS NULL;

-- ----------------------------------------------------------------------------
-- inbox_aggregator_cache table — daemon-level denormalized read view.
-- ----------------------------------------------------------------------------
--
-- Q11 C hybrid: the per-project `inbox` is the authoritative source;
-- this cache is rebuildable from those sources (Aggregator.Rebuild,
-- Phase E-9). Written by the outbox bridge (Phase E-8) on every
-- per-project INSERT; read-only from the query path. Cold rebuild on
-- daemon boot — ~1s for 10 projects per spec target.
--
-- Constraints:
--
--   - cache_id INTEGER PRIMARY KEY AUTOINCREMENT: append-only.
--
--   - project_id TEXT NOT NULL: sha256 hex matching authoritative
--                          source. inv-zen-113 anchor — runtime
--                          fanout MUST write the source DB's
--                          project_id; cross-project leaks fail the
--                          property-based fuzz test (Phase E-14).
--
--   - project_alias TEXT NOT NULL: human alias joined from projects
--                          table at write time. Denormalized for the
--                          `zen day` cross-project digest path
--                          (Phase F leverage-sort) — avoids a JOIN
--                          on every render.
--
--   - notification_id INTEGER NOT NULL: the per-project inbox.id this
--                          row mirrors. No FK — cache rows must
--                          survive per-project DB unlink between
--                          cache-write and cascade-sweep moments
--                          (the periodic Rebuild reconciles).
--
--   - severity TEXT NOT NULL CHECK: mirror of the per-project
--                          inv-zen-124 enum. Denormalized but the
--                          same CHECK enforces parity at the SQL
--                          layer.
--
--   - event_type TEXT NOT NULL: mirror.
--
--   - content_hash TEXT NOT NULL: mirror.
--
--   - created_at INTEGER NOT NULL: mirror, UTC unix seconds.
--
--   - acked_at INTEGER: mirror; NULL if not acked. The cache reflects
--                          ack state lazily — the outbox replays an
--                          UPDATE on ack at the per-project source.
--
-- UNIQUE strategy:
--
--   - UNIQUE (project_id, notification_id): prevents duplicate fanout
--                          under at-least-once outbox replay (Phase
--                          E-8 retries). The adapter uses INSERT OR
--                          IGNORE so retries silently no-op.
--
-- Index strategy:
--
--   - idx_aggregator_project: hot `zen day` cross-project digest
--                          (Phase F) + cascade DeleteByProject sweep.
--
--   - idx_aggregator_severity_created (composite): accelerates
--                          severity-filtered chronological reads
--                          ("show me last 24h of action-needed
--                          across all projects").
--
--   - idx_aggregator_event_type (composite): accelerates the
--                          cross-project collapse rule (Phase E-6
--                          DetectCollapse) which queries by
--                          event_type + 60s window.

CREATE TABLE IF NOT EXISTS inbox_aggregator_cache (
    cache_id          INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id        TEXT NOT NULL,                  -- sha256 hex (matches authoritative source)
    project_alias     TEXT NOT NULL,                  -- joined from projects table at write
    notification_id   INTEGER NOT NULL,               -- the per-project inbox.id this row mirrors
    severity          TEXT NOT NULL CHECK (severity IN (
                          'urgent',
                          'action-needed',
                          'info-immediate',
                          'info-digest'
                      )),                              -- inv-zen-124 (mirror)
    event_type        TEXT NOT NULL,
    content_hash      TEXT NOT NULL,
    created_at        INTEGER NOT NULL,
    acked_at          INTEGER,
    UNIQUE (project_id, notification_id)
);

CREATE INDEX IF NOT EXISTS idx_aggregator_project
    ON inbox_aggregator_cache (project_id);

CREATE INDEX IF NOT EXISTS idx_aggregator_severity_created
    ON inbox_aggregator_cache (severity, created_at);

CREATE INDEX IF NOT EXISTS idx_aggregator_event_type
    ON inbox_aggregator_cache (event_type, created_at);
