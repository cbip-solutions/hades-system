-- Migration 043: pin_overrides table (the release design release track Task I-1, Q8 D, inv-hades-063).
-- Tracks operator-set tier pins at three scope levels (session, project,
-- global). At most one active pin per (scope, scope_id). NULL expires_at
-- means permanent (no TTL); non-NULL is a Unix epoch second past which
-- the pin is auto-purged by the orchestrator's 5-min sweep.
--
-- inv-hades-063 anchor: this table only stores PIN intent. The cap check
-- runs LATER (in tier_resolver.Select, release track) and OVERRIDES a pin when
-- the pinned tier has reached its cap. There is no SQL constraint that
-- couples pin_overrides to cost_ledger; the invariant is enforced at the
-- code level by the capOverridesPin sentinel + integration tests.
--
-- scope_id uses empty string (not NULL) for the global scope because
-- SQLite treats NULLs as distinct in UNIQUE constraints, which would
-- allow multiple "global" rows. Using '' enforces a single global pin.
--
-- expires_at stores INTEGER unix epoch seconds (not milliseconds — pin
-- TTLs are coarse; second-precision is sufficient and avoids confusion
-- with cost_ledger.ts which uses milliseconds).
CREATE TABLE IF NOT EXISTS pin_overrides (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    scope      TEXT    NOT NULL CHECK(scope IN ('session','project','global')),
    scope_id   TEXT    NOT NULL DEFAULT '',
    tier       TEXT    NOT NULL,
    provider   TEXT    NOT NULL DEFAULT '',
    set_at     INTEGER NOT NULL,
    expires_at INTEGER,
    reason     TEXT    NOT NULL DEFAULT '',
    UNIQUE(scope, scope_id)
);

-- Partial index on expires_at for the 5-min TTL sweep — only indexes rows
-- that actually have a TTL, keeping permanent pins from bloating the index.
CREATE INDEX IF NOT EXISTS idx_pin_expires
    ON pin_overrides(expires_at) WHERE expires_at IS NOT NULL;

-- Covering index for QueryPin (scope, scope_id lookup) — avoids table scan
-- for the primary single-row lookup path.
CREATE INDEX IF NOT EXISTS idx_pin_scope
    ON pin_overrides(scope, scope_id);
