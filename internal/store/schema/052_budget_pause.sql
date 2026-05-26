-- Migration 052: budget_pauses + budget_anomalies + budget_anomaly_samples
-- (Plan 4 Phase F, Q6 C, inv-zen-078 + inv-zen-079).
--
-- budget_pauses: durable 4-scope state machine. (scope, scope_value)
-- is the natural key; UPSERT replaces the row on Trigger if the same
-- pair is already paused (e.g. anomaly trigger after operator manual
-- pause — second reason replaces first to surface freshest cause).
-- auto_resume_at = 0 means "indefinite, requires explicit Resume".
--
-- The four legitimate scopes (per spec §2.2) are project / doctrine /
-- stage / worker_id. The storage layer is free-text (defensive against
-- future taxonomy additions); the engine layer (internal/budget/pause.go)
-- guards via ValidPauseScopes.
CREATE TABLE IF NOT EXISTS budget_pauses (
    scope          TEXT    NOT NULL,
    scope_value    TEXT    NOT NULL,
    reason         TEXT    NOT NULL,
    started_at     INTEGER NOT NULL,
    auto_resume_at INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (scope, scope_value)
);

CREATE INDEX IF NOT EXISTS idx_budget_pauses_started_at
    ON budget_pauses(started_at);

CREATE INDEX IF NOT EXISTS idx_budget_pauses_auto_resume
    ON budget_pauses(auto_resume_at);

-- budget_anomalies: append-only event log of every z-score trigger.
-- Used by `zen budget anomalies` and morning-brief.
CREATE TABLE IF NOT EXISTS budget_anomalies (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    scope        TEXT    NOT NULL,
    scope_value  TEXT    NOT NULL,
    z_score      REAL    NOT NULL,
    mean         REAL    NOT NULL,
    std          REAL    NOT NULL,
    window_size  INTEGER NOT NULL,
    detected_at  INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_budget_anomalies_scope
    ON budget_anomalies(scope, scope_value, detected_at);

-- budget_anomaly_samples: per-scope rolling window of cost-delta samples.
-- Engine queries this via QueryAnomalyWindow with LIMIT N to feed the
-- z-score computation. Bounded by housekeeping vacuum (Plan 4 Phase G
-- handler prunes >24h after default 1440 samples = 24h-of-minutes).
CREATE TABLE IF NOT EXISTS budget_anomaly_samples (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    scope        TEXT    NOT NULL,
    scope_value  TEXT    NOT NULL,
    sample_usd   REAL    NOT NULL,
    sampled_at   INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_budget_anomaly_samples_window
    ON budget_anomaly_samples(scope, scope_value, sampled_at);
