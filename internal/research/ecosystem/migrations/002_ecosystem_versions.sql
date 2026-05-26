-- internal/research/ecosystem/migrations/002_ecosystem_versions.sql
--
-- Plan 14 Phase A Task A-9. Per spec §3.4.
--
-- One row per (package_id, version) tuple. source_blob_sha256 is the
-- pointer to Plan 9 F CAS (set when ingester writes the doc body).

CREATE TABLE IF NOT EXISTS ecosystem_versions (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    package_id          INTEGER NOT NULL REFERENCES ecosystem_packages(id) ON DELETE CASCADE,
    version             TEXT NOT NULL,
    released_at         DATETIME,
    deprecated_at       DATETIME,
    changelog_url       TEXT,
    source_blob_sha256  TEXT,
    UNIQUE (package_id, version)
);
