-- Migration 062: tmux_session_state (the release design release track, inv-hades-117 + inv-hades-118 + inv-hades-119).
--
-- One row per spawned hades-system tmux session, keyed by canonical name.
-- The internal/tmuxlife.Manager + DriftPoller + IdleReaper read/write here
-- through the SessionStore interface declared in internal/tmuxlife/session.go;
-- internal/daemon/handlers/sessions.go (release track) is the only package permitted
-- to bridge tmuxlife.SessionStore to *store.Store.
--
-- Drift note (the release design release track):
--   The master plan §"Migration numbering coordination" reserved slot 060
--   for a JOINT migration shipping priority_overrides + tmux_session_state.
--   release track (Quota Layer 3) shipped 060_priority_overrides.sql alone — the
--   joint payload was split because release track owns its own DDL and release track
--   needed unblocked storage. Slot 061 is reserved for release track knowledge
--   index (separate database file, structural-only). release track therefore
--   ships 062 as the next free number on the daemon.db schema chain.
--   schemaVersion bump path: 25 (release track) → 26 (this migration).
--
-- Constraints:
--
--   - name TEXT PRIMARY KEY: canonical "hades-<alias>-<sha8>". Spawn is
--                            idempotent at the daemon level via
--                            UpsertSession (interface declared in
--                            internal/tmuxlife/session.go); duplicate
--                            INSERT (without UPSERT) returns
--                            ErrDuplicateTmuxSessionName so the seam can
--                            distinguish "operator created twice" vs.
--                            "race window" semantics.
--   - alias TEXT NOT NULL:   release track projectctx alias. Plain text — NOT a
--                            FK to projects_alias.alias. Tmux state is
--                            forensic-relevant after archive (`hades day`
--                            digest); coupling lifecycle to projects_alias
--                            via FK CASCADE would silently lose audit-
--                            relevant rows.
--   - sha8 TEXT NOT NULL:    First 8 hex chars of project sha256.
--                            isValidSha8 (tmuxlife) validates lowercase-
--                            hex; the SQL layer is content-blind.
--   - status INTEGER NOT NULL CHECK(status >= 0 AND status <= 3):
--                            mirrors tmuxlife.SessionStatus enum
--                            (Active=0, Idle=1, Orphaned=2, Archived=3).
--                            CHECK is the floor; the Go-side
--                            UpdateTmuxSessionStatus rejects out-of-range
--                            values BEFORE the SQL CHECK fires (better
--                            error message). Defense in depth: same value
--                            range guarded twice.
--   - created_at INTEGER:    UTC unix seconds; first successful Spawn.
--                            Survives idle reaping (snapshot+restore
--                            preserves the row + this column).
--   - last_attach_at INTEGER NOT NULL DEFAULT 0:
--                            UTC unix seconds; 0 = never attached.
--                            UpdateTmuxSessionLastAttach uses 0 as the
--                            sentinel for "fresh row" detection in tests.
--   - expected_panes TEXT NOT NULL DEFAULT '{}':
--                            JSON-encoded map[WindowName][]string of
--                            daemon-recorded pane ids per daemon-owned
--                            window. EXCLUDES WindowScratch
--                            (inv-hades-118): the JSON encoder in
--                            tmuxlife.encodeExpectedPanes filters
--                            scratch out before serialisation; the
--                            store layer is content-blind. Empty
--                            map "{}" means "no panes registered yet"
--                            (pre-CreateWindows or stale row); poller
--                            treats this as "no expectation" not
--                            "drift detected".
--
-- Index strategy:
--
--   - PRIMARY KEY(name) is implicitly indexed; GetTmuxSessionState fast
--     path bypasses any other lookup.
--
--   - idx_tmux_session_state_alias accelerates release track
--     `hades sessions ls` and the IdleReaper's resolveAlias scan when
--     ListSessions filters by alias (1-2 active sessions per project on
--     the typical workstation, but the index keeps the lookup O(log n)
--     rather than O(n) when project count grows).
--
--   - idx_tmux_session_state_status_attach is the IdleReaper's primary
--     index: enumerate StatusActive (=0) sessions sorted by
--     last_attach_at ascending (oldest-first reap order). The composite
--     keeps the scan in-index without touching the full row.
--
-- No FK to projects_alias.alias:
--   Same rationale as priority_overrides (migration 060). Tmux state is
--   forensic-relevant beyond project lifecycle: `hades day` digest reads
--   archived sessions; an FK with ON DELETE CASCADE would silently lose
--   audit-relevant rows when a project is archived. An FK without CASCADE
--   would block project removal, coupling lifecycle. The IdleReaper +
--   operator-driven `hades project rm <alias>` are the legitimate row
--   removers.

CREATE TABLE IF NOT EXISTS tmux_session_state (
    name           TEXT PRIMARY KEY,                                 -- "hades-<alias>-<sha8>"
    alias          TEXT NOT NULL,                                    -- release track projectctx alias
    sha8           TEXT NOT NULL,                                    -- First 8 lowercase-hex chars of project sha256
    status         INTEGER NOT NULL DEFAULT 0
                   CHECK (status >= 0 AND status <= 3),              -- 0=Active, 1=Idle, 2=Orphaned, 3=Archived
    created_at     INTEGER NOT NULL,                                 -- UTC unix seconds
    last_attach_at INTEGER NOT NULL DEFAULT 0,                       -- UTC unix seconds; 0 = never attached
    expected_panes TEXT NOT NULL DEFAULT '{}'                        -- JSON: map[WindowName][]string; excludes scratch (inv-hades-118)
);

CREATE INDEX IF NOT EXISTS idx_tmux_session_state_alias
    ON tmux_session_state(alias);

CREATE INDEX IF NOT EXISTS idx_tmux_session_state_status_attach
    ON tmux_session_state(status, last_attach_at);
