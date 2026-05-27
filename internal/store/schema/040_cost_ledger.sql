-- Migration 040: persistent cost ledger (the release design release track, Q4 C, invariant).
-- Append-only; one row per LLM request with cost_usd already computed
-- by the provider's RateCard. idempotency_key UNIQUE → no double-charge
-- (Go layer translates the SQL constraint failure into
-- ErrDuplicateIdempotency). ts is unix milliseconds (matches
-- time.Time.UnixMilli() and supports boundary-correctness tests).
-- Indexes:
--   (project, profile, tier, ts) — rolling-window total per cap key
--   (session_id, ts)             — session-lifetime total per session
CREATE TABLE IF NOT EXISTS cost_ledger (
    id                    INTEGER PRIMARY KEY AUTOINCREMENT,
    idempotency_key       TEXT NOT NULL UNIQUE,
    ts                    INTEGER NOT NULL,
    project               TEXT NOT NULL,
    profile               TEXT NOT NULL,
    tier                  TEXT NOT NULL,
    model                 TEXT NOT NULL,
    input_tokens          INTEGER NOT NULL,
    output_tokens         INTEGER NOT NULL,
    cache_read_tokens     INTEGER NOT NULL DEFAULT 0,
    cache_creation_tokens INTEGER NOT NULL DEFAULT 0,
    cost_usd              REAL NOT NULL,
    conversation_id       TEXT,
    session_id            TEXT,
    request_hash          BLOB
);

CREATE INDEX IF NOT EXISTS idx_cost_ledger_window
    ON cost_ledger(project, profile, tier, ts);

CREATE INDEX IF NOT EXISTS idx_cost_ledger_session
    ON cost_ledger(session_id, ts);
