-- internal/research/ecosystem/migrations/001_ecosystem_packages.sql
--
-- the release design release track Task A-9. Per spec §3.4.
--
-- One row per (ecosystem, name) tuple. Updated by release track ingester's
-- last_indexed_at / last_upstream_check / latest_stable_version fields
-- on every poll cycle.
--
-- DRIFT RECONCILIATION (2026-05-17, task-driver instruction):
--
--   spec §3.4 + plan-file 001 used `language` for the column name + UNIQUE.
--   types.go A-3 declared `PackageRef.Ecosystem Ecosystem` (string enum:
--   go|python|typescript|rust) with EcoGo/EcoPython/EcoTypeScript/EcoRust
--   constants. The frozen Go contract wins: column = `ecosystem` and
--   UNIQUE = (ecosystem, name). release track indexer-writer code reads from
--   PackageRef.Ecosystem and writes here directly without translation.
--
--   invariant unaffected (this package does not import internal/store);
--   spec §3.4 column-name drift is reconciled here at the SQL layer.

CREATE TABLE IF NOT EXISTS ecosystem_packages (
    id                    INTEGER PRIMARY KEY AUTOINCREMENT,
    name                  TEXT NOT NULL,
    ecosystem             TEXT NOT NULL,
    upstream_url          TEXT NOT NULL,
    canonical_namespace   TEXT NOT NULL,
    last_indexed_at       DATETIME,
    last_upstream_check   DATETIME,
    latest_stable_version TEXT,
    UNIQUE (ecosystem, name)
);
