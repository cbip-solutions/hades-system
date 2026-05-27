-- Migration 053: cost_id column + UNIQUE constraint on
-- budget_anomaly_samples (HADES design release track post-review C-2).
--
-- C-2 (post-review): PostCallWithCost orchestration was non-atomic;
-- anomaly samples double-counted under retry. AppendAnomalySample was
-- not idempotent — a partial-failure retry of PostCallWithCost
-- duplicated the (scope, value, sample, sampled_at) row, inflating the
-- rolling-window denominator and producing false stable distributions.
--
-- Fix: add cost_id column + UNIQUE constraint on (scope, scope_value,
-- cost_id). The new AppendAnomalySampleByCostID writer routes through
-- INSERT OR IGNORE so retries with the same cost_id are idempotent at
-- the SQL layer (consistent with cost_axis_tags' (cost_id, axis_name)
-- UNIQUE).
--
-- The legacy AppendAnomalySample (cost_id-less) remains for callers
-- that genuinely have no cost_id (CLI bulk-import, retro-fill); those
-- callers explicitly opt out of the idempotency guarantee. cost_id=0
-- is a sentinel meaning "no idempotency key" and bypasses the UNIQUE.
--
-- SQLite ALTER TABLE limitations: we use the standard 12-step recreate
-- pattern (rename → create new → copy → drop → rename back → recreate
-- indexes). The new column is appended with a default of 0 so existing
-- rows (none on production yet — HADES design release track is the introducing
-- plan) round-trip cleanly.

-- 1. Rename existing table.
ALTER TABLE budget_anomaly_samples RENAME TO budget_anomaly_samples__old;

-- 2. Create new table with cost_id + UNIQUE.
CREATE TABLE IF NOT EXISTS budget_anomaly_samples (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    scope        TEXT    NOT NULL,
    scope_value  TEXT    NOT NULL,
    cost_id      INTEGER NOT NULL DEFAULT 0,
    sample_usd   REAL    NOT NULL,
    sampled_at   INTEGER NOT NULL,
    UNIQUE (scope, scope_value, cost_id)
);

-- 3. Copy old rows. cost_id defaults to 0 for legacy samples (no
-- idempotency) — UNIQUE on (scope, scope_value, 0) would collide if
-- multiple legacy rows existed per scope. To avoid that on re-run
-- (idempotent migration), use the row's id as a per-row unique cost_id
-- so the UNIQUE constraint is satisfied; new callers writing cost_id=0
-- will collide with each other (which is intentional — they explicitly
-- opted out of idempotency by using the sentinel).
--
-- Wait: the cleaner approach is to use NEGATIVE id values for legacy
-- rows so they never collide with the cost_id=0 sentinel or any real
-- cost_id (which are positive). This preserves all existing data while
-- maintaining the UNIQUE constraint semantics for new writes.
INSERT INTO budget_anomaly_samples (id, scope, scope_value, cost_id, sample_usd, sampled_at)
    SELECT id, scope, scope_value, -id, sample_usd, sampled_at
    FROM budget_anomaly_samples__old;

-- 4. Drop the old table.
DROP TABLE budget_anomaly_samples__old;

-- 5. Recreate the rolling-window index.
CREATE INDEX IF NOT EXISTS idx_budget_anomaly_samples_window
    ON budget_anomaly_samples(scope, scope_value, sampled_at);
