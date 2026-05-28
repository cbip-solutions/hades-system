-- internal/research/ecosystem/migrations/002_ecosystem_versions.sql
--
-- HADES design stage task. per design contract
--
-- One row per (package_id, version) tuple. source_blob_sha256 is the
-- pointer to HADES design F CAS (set when ingester writes the doc body).

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
