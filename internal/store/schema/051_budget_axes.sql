-- Migration 051: cost_axis_tags + axis_tag_loss_events (Plan 4 Phase F, Q6 C, inv-zen-077).
--
-- The 4-axis attribution table holds one row per (cost_id, axis_name) pair.
-- Idempotency is achieved via UNIQUE (cost_id, axis_name) + INSERT OR IGNORE in
-- the Go layer (PostCall is potentially retried).
--
-- DESIGN NOTE (Option A coordination per METHODOLOGY.md §4.7.5):
-- The natural FK target is cost_ledger.id, owned by Plan 3 Phase F migration 040.
-- Plan 3 F-1..F-6 are committed on `plan-3-rescope-execute` branch but NOT yet
-- merged to origin/main; cost_ledger does not exist on this branch (Plan 4
-- worktree). To allow Phase F to ship standalone with full unit + adversarial
-- coverage, cost_axis_tags is declared WITHOUT a FOREIGN KEY clause. After
-- Plan 3 F-1 merges to main, a coordination migration (planned 053+) will
-- ADD the FK constraint via CREATE TABLE … SELECT … swap. Engine-layer
-- behaviour remains identical: tests inject explicit cost_id values and the
-- INSERT OR IGNORE on UNIQUE (cost_id, axis_name) is the load-bearing
-- idempotency guarantee, not the FK.
--
-- The axis_tag_loss_events table records every missing-axis incident so the
-- daemon morning brief and `zen budget axes --losses` surface drift the
-- moment it appears (rather than silently allowing axis_tag_completeness to
-- drop below 100%).

CREATE TABLE IF NOT EXISTS cost_axis_tags (
    cost_id    INTEGER NOT NULL,
    axis_name  TEXT    NOT NULL,
    axis_value TEXT    NOT NULL,
    written_at INTEGER NOT NULL,
    UNIQUE (cost_id, axis_name)
    -- FK to cost_ledger(id) deferred until Plan 3 F-1 merges (see DESIGN NOTE).
);

CREATE INDEX IF NOT EXISTS idx_cost_axis_tags_axis_value
    ON cost_axis_tags(axis_name, axis_value);

CREATE INDEX IF NOT EXISTS idx_cost_axis_tags_cost_id
    ON cost_axis_tags(cost_id);

CREATE TABLE IF NOT EXISTS axis_tag_loss_events (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    cost_id       INTEGER NOT NULL,
    missing_axis  TEXT    NOT NULL,
    detected_at   INTEGER NOT NULL
    -- FK deferred per cost_axis_tags rationale.
);

CREATE INDEX IF NOT EXISTS idx_axis_tag_loss_cost_id
    ON axis_tag_loss_events(cost_id);
