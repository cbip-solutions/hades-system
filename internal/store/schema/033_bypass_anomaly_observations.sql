-- Migration v3 — bypass_anomaly_observations (the release design release track, inv-hades-060)
--
-- Per-event observation table for the rolling-window numerator. The v2
-- bypass_anomalies table aggregates by field_path and stores a lifetime
-- count plus first_seen/last_seen; that shape is structurally weak for
-- a sliding-window percentage because at steady state every row's
-- last_seen lands inside the window and the lifetime count saturates
-- the numerator (false-positive INFO notification storms).
--
-- This migration introduces one row per observation:
--   - field_path: the un-normalised JSON path observed.
--   - ts: UTC unix seconds (inv-hades-005) at which the observation was
--     recorded. Indexed (field_path, ts) for fast window queries.
--
-- Lifecycle:
--   - INSERTed on every RecordAnomaly call (alongside the upsert into
--     bypass_anomalies which keeps the lifetime-count semantics).
--   - QueryAnomalyCount counts rows in [since, until] for the window
--     numerator instead of returning the lifetime count.
--   - A nightly purge (PurgeObservationsOlderThan) removes rows with
--     ts older than the configured retention horizon (default 24h
--     window → 24h retention; operators may extend).
--
-- The FK uses ON DELETE CASCADE so acknowledging-and-deleting an
-- anomaly row also drops its observations (the release design release track wires the
-- delete path; release track only writes ack=1 without deleting).
--
-- Refs: spec the release design §2 Q6-C, §5 row "Shape unknown field", §7
-- inv-hades-060.

CREATE TABLE IF NOT EXISTS bypass_anomaly_observations (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    field_path TEXT    NOT NULL,
    ts         INTEGER NOT NULL,
    FOREIGN KEY (field_path) REFERENCES bypass_anomalies(field_path) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_bypass_anomaly_obs_field_ts
    ON bypass_anomaly_observations(field_path, ts);

CREATE INDEX IF NOT EXISTS idx_bypass_anomaly_obs_ts
    ON bypass_anomaly_observations(ts);
