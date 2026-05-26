-- Conversation WAL — one row per orchestrator turn (Plan 2 Phase H).
--
-- Each request through the bypass path appends a row in 'pending' state
-- BEFORE the upstream HTTP call leaves the daemon. CompleteTurn (or
-- FailTurn) promotes status atomically. After a daemon restart, scanning
-- WHERE status='pending' surfaces every in-flight turn for replay.
--
-- Layer 1 of the bypass resilience model. Half of inv-zen-056 (the
-- idempotency keys table is the other half).

CREATE TABLE IF NOT EXISTS conversation_wal (
    turn_id         INTEGER PRIMARY KEY AUTOINCREMENT,
    conversation_id TEXT    NOT NULL,
    request_hash    BLOB    NOT NULL,
    request_ts      INTEGER NOT NULL,
    response_hash   BLOB,
    response_ts     INTEGER,
    status          TEXT    NOT NULL CHECK(status IN ('pending','completed','failed')),
    error_message   TEXT
);

CREATE INDEX IF NOT EXISTS idx_conversation_wal_conv
    ON conversation_wal(conversation_id, request_ts);

CREATE INDEX IF NOT EXISTS idx_conversation_wal_pending
    ON conversation_wal(status) WHERE status = 'pending';
