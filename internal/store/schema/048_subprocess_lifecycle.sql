-- Subprocess lifecycle persistence (HADES design release track Task C-5, spec §11
-- migration 048 -> schemaVersion bump).
--
-- subprocess_sessions records LIVE persistent TeamLead + Reviewer L3/L4
-- subprocesses so a daemon restart can rebind to the surviving processes
-- (or re-spawn from history if they are gone). Ephemeral Worker sessions
-- are never persisted: they exist only for the duration of Worker.Run.
--
-- Idempotency key: (spec_id, doctrine_name). The same SpecID under two
-- different doctrines yields TWO rows with TWO distinct subprocesses
-- because TTL semantics diverge per doctrine (max-scope 8h sliding,
-- default 4h, capa-firewall per-Pulido).
--
-- invariant (TTL eviction) reads ttl_seconds + last_use_at to decide
-- which rows to send SIGTERM-then-SIGKILL.
--
-- IF NOT EXISTS aligns with migrations 032-039/044/045 and lets the DDL
-- re-execute safely against a database that already has the table.

CREATE TABLE IF NOT EXISTS subprocess_sessions (
    spec_id        TEXT NOT NULL,
    doctrine_name  TEXT NOT NULL,
    thread_id      TEXT NOT NULL,
    worktree       TEXT NOT NULL,
    project_id     TEXT NOT NULL DEFAULT '',
    pid            INTEGER NOT NULL DEFAULT 0,    -- 0 if subprocess crashed; recovered on restart
    started_at     INTEGER NOT NULL,              -- UTC unix seconds
    last_use_at    INTEGER NOT NULL,              -- UTC unix seconds; advanced on every Acquire
    ttl_seconds    INTEGER NOT NULL,              -- doctrine-resolved TTL captured at acquire
    PRIMARY KEY (spec_id, doctrine_name)
);
CREATE INDEX IF NOT EXISTS idx_subprocess_sessions_thread ON subprocess_sessions(thread_id);
CREATE INDEX IF NOT EXISTS idx_subprocess_sessions_lastuse ON subprocess_sessions(last_use_at);
