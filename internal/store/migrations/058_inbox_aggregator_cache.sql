-- Migration 058: per-project `inbox` table + daemon.db `inbox_aggregator_cache`
-- (HADES design release track Task E-1, Q11 C hybrid storage).
--
-- ============================================================================
-- Architecture (spec §3.3, Q11 C):
--
--   Per-project authoritative `inbox` table:
--     - severity 4-tier CHECK (invariant)
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
--      `dedup.ComputeDedupKey` (release track) writes
--      `created_at_bucket = created_at / 300`; SQLite enforces UNIQUE
--      on the triple. Defense in depth: Go-side rejects malformed
--      bucket values BEFORE the SQL layer; SQLite UNIQUE is the floor.
--
-- ============================================================================
-- invariant anchors (spec §7.2):
--
--   - invariant (no cross-project leak): inbox_aggregator_cache rows
--     carry project_id matching the originating per-project source DB.
--     Compile-time anchor in inbox/sentinel.go (release track);
--     runtime via the outbox bridge writing project_id from the source
--     scope (release track); property-based fuzz test in
--     tests/compliance/inv_hades_113_no_cross_project_inbox_leak_test.go
--     (release track).
--
--   - invariant (severity 4-tier CHECK enum): inbox.severity column +
--     inbox_aggregator_cache.severity column both enforce the 4-tier
--     enum at the SQL layer. Defense in depth: Go-side ValidSeverity
--     (release track) rejects first; SQL CHECK is the floor.
--
--   - invariant (boundary): internal/inbox/* MUST NEVER import
--     internal/store. The internal/daemon/inboxadapter/ package
--     (release track) is the ONLY package permitted to bridge inbox
--     value types to *store.Store via this migration's tables.
--     release track inv_hades_122 compliance test extends to enforce this
--     on HADES design packages (greps internal/inbox/*.go for forbidden
--     internal/store imports).
--
-- ============================================================================
-- Drift note (HADES design release track):
--
--   The original master plan §"Migration numbering coordination"
--   reserved slot 058 for release track inbox storage under an
--   execution-order sequence A → C → B → D → E in which release track ships
--   migrationV27 (HEAD baseline 23 → ... → 26 → 27).
--
--   Reality at HEAD on 2026-05-07:
--     - 057 taken by release track (projects_alias + path_history, V24)
--     - 060 taken by release track (priority_overrides, V25)
--     - 062 taken by release track (tmux_session_state, V26)
--     - 063 taken by release track (schedules + schedule_history, V27)
--     - schemaVersion = 27 entering release track.
--
--   release track therefore picks migrationV28 (next free) — the FILE
--   number stays at 058 (slot reserved for inbox per master plan
--   reconciliation; only the in-Go variable name shifts to V28).
--   schemaVersion bump path: 27 (release track) → 28 (this migration).
--
--   Slot 061 remains reserved for release track knowledge-index DB
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
--   - severity TEXT NOT NULL CHECK: 4-tier enum per invariant.
--                          Defense in depth — Go-side ValidSeverity
--                          rejects first; SQL CHECK is the floor.
--
--   - event_type TEXT NOT NULL: e.g. "hra.l4_alert", "job.failed",
--                          "doctrine.amendment.proposed". Maps to
--                          orchestrator/eventlog.EventType verbatim
--                          (string-based).
--
--   - content_hash TEXT NOT NULL: sha256 hex of canonical fields used
--                          by inbox/dedup.ComputeDedupKey (release track).
--                          The dedup contract: same content_hash within
--                          a 5min bucket → same notification, second
--                          write rejected by UNIQUE.
--
--   - payload TEXT NOT NULL DEFAULT '{}': JSON blob, schema-free at
--                          this layer. The Go-side decoder validates
--                          per event_type before surfacing to render.
--
--   - created_at INTEGER NOT NULL: UTC unix seconds (invariant).
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
--                          Sweep runs release track (hades inbox snooze
--                          surfaces).
--
-- Index strategy:
--
--   - PRIMARY KEY(id) is implicitly indexed (used by Ack/Snooze).
--
--   - idx_inbox_project_severity_created (composite):
--                          accelerates `hades inbox --severity=...
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
--                          ErrDedupViolation (release track sentinel)
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
                      )),                              -- invariant
    event_type        TEXT NOT NULL,                  -- e.g. "hra.l4_alert"
    content_hash      TEXT NOT NULL,                  -- sha256 hex of canonical fields
    payload           TEXT NOT NULL DEFAULT '{}',     -- JSON blob
    created_at        INTEGER NOT NULL,               -- UTC unix seconds (invariant)
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
-- release track). Written by the outbox bridge (release track) on every
-- per-project INSERT; read-only from the query path. Cold rebuild on
-- daemon boot — ~1s for 10 projects per spec target.
--
-- Constraints:
--
--   - cache_id INTEGER PRIMARY KEY AUTOINCREMENT: append-only.
--
--   - project_id TEXT NOT NULL: sha256 hex matching authoritative
--                          source. invariant anchor — runtime
--                          fanout MUST write the source DB's
--                          project_id; cross-project leaks fail the
--                          property-based fuzz test (release track).
--
--   - project_alias TEXT NOT NULL: human alias joined from projects
--                          table at write time. Denormalized for the
--                          `hades day` cross-project digest path
--                          (release track leverage-sort) — avoids a JOIN
--                          on every render.
--
--   - notification_id INTEGER NOT NULL: the per-project inbox.id this
--                          row mirrors. No FK — cache rows must
--                          survive per-project DB unlink between
--                          cache-write and cascade-sweep moments
--                          (the periodic Rebuild reconciles).
--
--   - severity TEXT NOT NULL CHECK: mirror of the per-project
--                          invariant enum. Denormalized but the
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
--   - idx_aggregator_project: hot `hades day` cross-project digest
--                          (release track) + cascade DeleteByProject sweep.
--
--   - idx_aggregator_severity_created (composite): accelerates
--                          severity-filtered chronological reads
--                          ("show me last 24h of action-needed
--                          across all projects").
--
--   - idx_aggregator_event_type (composite): accelerates the
--                          cross-project collapse rule (release track
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
                      )),                              -- invariant (mirror)
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
