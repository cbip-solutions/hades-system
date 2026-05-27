-- schemaVersion: 19
-- HADES design release track — research cache table.
-- Research MCP (release track) reads/writes via daemon /v1/research/cache/*.
-- TTL enforced by handler (ttl_unix comparison) + background eviction goroutine in
-- daemon (every 1h, DELETE WHERE ttl_unix < unixepoch()).
-- Eviction goroutine wired in internal/daemon/research_cache_evictor.go;
-- spawned from Server.Start() and torn down by Server.Stop() context cancel
-- (post-review C-3 fix).
-- HADES design does NOT modify this table.
--
-- CHECK constraints (post-review N-7):
--   ttl_unix > 0 — defends against accidental zero/negative TTLs that would
--   either evict instantly (negative) or be ambiguous (the handler treats
--   zero as a sentinel for "use doctrine default" upstream, but the SQL
--   row stores the absolute timestamp so zero is never legitimate here).
--   created_at > 0 — same defence; unixepoch() default keeps real inserts
--   compliant.

CREATE TABLE IF NOT EXISTS research_cache (
    hash          TEXT    NOT NULL PRIMARY KEY,  -- sha256(query+sources_used+iteration_index)
    response_json TEXT    NOT NULL,               -- full JSON response from research dispatch
    created_at    INTEGER NOT NULL DEFAULT (unixepoch()) CHECK (created_at > 0),
    ttl_unix      INTEGER NOT NULL CHECK (ttl_unix > 0) -- absolute unix timestamp after which entry is stale
);

CREATE INDEX IF NOT EXISTS idx_research_cache_ttl ON research_cache(ttl_unix);
