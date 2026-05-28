-- aggregation_windows + aggregation_events — HADES design stage.
--
-- AggregationStream uses these tables to survive daemon restart:
--   1. On window open:  INSERT INTO aggregation_windows (status='open').
--   2. On event:        INSERT INTO aggregation_events.
--   3. On window close: UPDATE aggregation_windows SET status='closed'.
--   4. On restart:      SELECT * FROM aggregation_windows WHERE status='open'
--                       → LoadOpenWindows recovers in-progress windows.
--
-- Doctrine-tunable cadence (30s/5min max-scope; 60s/15min default)
-- lives in doctrine TOML and is passed as Config to AggregationStream.
-- The tables are cadence-agnostic; they record what happened, not how fast.

CREATE TABLE IF NOT EXISTS aggregation_windows (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    layer       INTEGER NOT NULL,   -- 2=L2, 3=L3, 4=L4 (Layer enum)
    status      TEXT    NOT NULL CHECK(status IN ('open','closed')),
    opened_at   INTEGER NOT NULL,   -- UTC unix seconds (invariant)
    closed_at   INTEGER,            -- NULL while open
    event_count INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_agg_windows_status
    ON aggregation_windows(status) WHERE status = 'open';

CREATE INDEX IF NOT EXISTS idx_agg_windows_layer_opened
    ON aggregation_windows(layer, opened_at DESC);

CREATE TABLE IF NOT EXISTS aggregation_events (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    window_id   INTEGER NOT NULL REFERENCES aggregation_windows(id)
                    ON DELETE CASCADE,
    event_type  TEXT    NOT NULL,
    -- payload: opaque JSON, validated at the storage layer via
    -- CHECK(json_valid(payload)). I-9 fix: previously TEXT NOT NULL
    -- allowed any byte sequence including malformed JSON; the doc
    -- contract was "opaque JSON" but nothing enforced it. Adding
    -- CHECK json_valid makes the contract structural — INSERT fails
    -- fast on bad payload instead of corrupting the event stream.
    --
    -- Why CHECK and not BLOB:
    --   - All current callers (Worker checkpoint, fix prompts, future
    --     L4 strategic events) emit JSON.
    --   - Subscribers + reviewers expect JSON for Pulido §3.5 traversal.
    --   - BLOB would lose the structural validation + force re-marshal
    --     on every read.
    payload     TEXT    NOT NULL CHECK(json_valid(payload)),
    published_at INTEGER NOT NULL   -- UTC unix seconds
);

CREATE INDEX IF NOT EXISTS idx_agg_events_window
    ON aggregation_events(window_id);
