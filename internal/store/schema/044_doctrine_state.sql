-- the release design release track — singleton row holding the last-loaded doctrine
-- snapshot. Daemon reads on startup so the release design workers dispatched
-- before a restart see consistent doctrine values across the restart
-- boundary (no layer-flicker due to mid-flight ~/.config/ changes).
--
-- release track wires /v1/doctrine/state endpoint that reads here.
-- Singleton enforced by CHECK(id = 1).
--
-- IF NOT EXISTS (defense-in-depth, I-2 fix): aligns with migrations
-- 032-039 and lets the DDL re-execute safely against a database that
-- already has the table — guards against split-brain scenarios where
-- the table exists but no schema_version row records the migration.

CREATE TABLE IF NOT EXISTS doctrine_state (
    id              INTEGER PRIMARY KEY CHECK(id = 1),
    schema_json     TEXT NOT NULL,         -- json.Marshal(Resolved.Schema)
    provenance_json TEXT NOT NULL,         -- json.Marshal(Resolved.Provenance)
    loaded_at_unix  INTEGER NOT NULL,      -- unix seconds
    doctrine_name   TEXT NOT NULL          -- e.g. "max-scope" | "default" | "capa-firewall" | "<custom-name>"
);
