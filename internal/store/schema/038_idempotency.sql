-- Idempotency keys cache — TTL 24h (the release design release track).
--
-- Lifecycle:
--   MarkPending    — written BEFORE upstream call (invariant).
--   MarkCompleted  — written AFTER validator+audit; persists full
--                    response (status_code + headers + body) so a
--                    restart in the gap upstream-response → orchestrator
--                    delivery replays without a second upstream charge.
--   MarkFailed     — terminal failure; subsequent Check returns
--                    status='failed' so caller may retry with a NEW key.
--
-- Expired rows surface as "unknown" via the bypass-side wrapper so the
-- caller falls through to a fresh upstream call. PurgeExpired runs on
-- a 1h ticker (StartPurgeScheduler) launched by the daemon (release track).
--
-- error_message captures the human-readable cause when MarkFailed is
-- invoked, mirroring conversation_wal.error_message (FailTurn). Symmetry
-- between the two persistence layers keeps post-restart inspection
-- coherent: every 'failed' row carries its trigger string, regardless
-- of which layer recorded the failure first.

CREATE TABLE IF NOT EXISTS idempotency_keys (
    key                  TEXT    PRIMARY KEY,
    request_hash         BLOB    NOT NULL,
    status               TEXT    NOT NULL CHECK(status IN ('pending','completed','failed')),
    response_status_code INTEGER,
    response_headers     TEXT,
    response_body        BLOB,
    error_message        TEXT,
    ts                   INTEGER NOT NULL,
    expires_at           INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_idempotency_expires
    ON idempotency_keys(expires_at);
CREATE INDEX IF NOT EXISTS idx_idempotency_status
    ON idempotency_keys(status);
