-- Migration v2 — bypass_anomalies (HADES design release track, invariant)
--
-- Records every unknown field path observed in Anthropic /v1/messages
-- responses (Layer 5 stratified validation per spec §22.2 + HADES design
-- spec §2 Q6-C). One row per distinct field_path; count is the rolling
-- count of observations; first_seen/last_seen bound the lifetime;
-- total_responses_in_window + percentage are denormalised cache columns
-- updated by AnomalyTracker.RecordAndCheck so `hades bypass anomalies`
-- can render the table without recomputing from raw audit data.
--
-- acknowledged=1 means the operator has reviewed this anomaly via
-- `hades bypass anomalies --acknowledge <field>`; acknowledged anomalies
-- still record observations (count keeps incrementing) but no longer
-- fire the INFO threshold notification.

CREATE TABLE IF NOT EXISTS bypass_anomalies (
    id                         INTEGER PRIMARY KEY AUTOINCREMENT,
    field_path                 TEXT    NOT NULL,
    parent_path                TEXT,
    count                      INTEGER NOT NULL DEFAULT 1,
    first_seen                 INTEGER NOT NULL,
    last_seen                  INTEGER NOT NULL,
    total_responses_in_window  INTEGER,
    percentage                 REAL,
    acknowledged               INTEGER NOT NULL DEFAULT 0,
    UNIQUE (field_path)
);

CREATE INDEX IF NOT EXISTS idx_bypass_anomalies_last_seen
    ON bypass_anomalies(last_seen);

CREATE INDEX IF NOT EXISTS idx_bypass_anomalies_acknowledged
    ON bypass_anomalies(acknowledged);
