-- Bypass notifications ledger (the release design release track Task L-4, spec §8.4)
--
-- Distinct from notifications_queue (the release design). This table holds bypass-
-- module events specifically: tier switches, refresh failures, cert pin
-- failures, anomaly thresholds. CRITICAL severity repeats every 1h until
-- ack to ensure operator never misses a permanent failure.

CREATE TABLE IF NOT EXISTS notifications (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    severity      TEXT NOT NULL CHECK (severity IN ('INFO','WARN','CRITICAL')),
    title         TEXT NOT NULL,
    body          TEXT NOT NULL DEFAULT '',
    source        TEXT NOT NULL,                 -- e.g. "bypass.refresh", "bypass.cert"
    ts            INTEGER NOT NULL,              -- UTC unix seconds
    acknowledged  INTEGER NOT NULL DEFAULT 0,    -- 0/1
    ack_ts        INTEGER,                       -- when operator acked
    last_repeated INTEGER                        -- last time CRITICAL re-fired (NULL if never)
);
CREATE INDEX IF NOT EXISTS idx_notifications_unacked ON notifications(acknowledged, severity, ts);
CREATE INDEX IF NOT EXISTS idx_notifications_source ON notifications(source);
