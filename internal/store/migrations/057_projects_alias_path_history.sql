-- Migration 057: project alias dual-ID + path_history (the release design release track, Q3, inv-hades-114).
-- Two new tables introduce AIP-2510 dual-ID separation:
--   - projects_alias: sha256(canonical-path) canonical id + human alias
--                     UX-facing operator commands resolve via alias
--   - path_history:   tracks (id_sha256, path) tuples for mv-detection.
--                     Same alias appearing at new canonical path with a
--                     different sha256 → daemon prompts `hades project doctor`
--                     rebind (see internal/projectctx/path_history.go).
--
-- NOTE: release track leaves the existing v1 `projects` table untouched. Plans
-- 1-3 remain functional.
-- the release design reads/writes via `projects_alias` + `path_history`. Future plans
-- may reconcile the legacy table at a major version boundary; until then
-- the dual-source-of-truth split is intentional + documented.
--
-- Constraints:
--   - id_sha256 PRIMARY KEY: rejects duplicate canonical id INSERTs
--                            (translated to ErrDuplicateProjectID).
--   - alias UNIQUE: rejects collision (translated to ErrDuplicateAlias).
--                   When operator wants intentional rebind (mv-detection),
--                   app layer first DeleteProjectAlias(old) then
--                   InsertProjectAlias(new) — never INSERT-OR-REPLACE.
--   - path_history.(id_sha256, path) PRIMARY KEY:
--                   one row per project × path; upsert updates last_seen_at.
--   - FOREIGN KEY (id_sha256) REFERENCES projects_alias(id_sha256)
--                   ON DELETE CASCADE: DeleteProjectAlias triggers cascade.
--                   Requires `PRAGMA foreign_keys = ON` (set in Open()).
--   - archived_at INTEGER (nullable): NULL = active, non-NULL = archived
--                   timestamp (ms). Archive ≠ delete; preserves audit trail.
--
-- ts columns: unix MILLISECONDS (UnixMilli()) — matches the release design cost_ledger
-- pattern + supports boundary-correctness tests.
CREATE TABLE IF NOT EXISTS projects_alias (
    id_sha256       TEXT PRIMARY KEY,        -- sha256 hex (64 chars), of EvalSymlinks(canonical_path)
    alias           TEXT NOT NULL UNIQUE,    -- human alias from hadessystem.toml [project] id (or fallback <dirname>-<sha8>)
    canonical_path  TEXT NOT NULL,           -- absolute filepath.EvalSymlinks(path)
    first_seen_at   INTEGER NOT NULL,        -- ms when project first activated
    last_seen_at    INTEGER NOT NULL,        -- ms last activation
    archived_at     INTEGER                  -- ms; NULL = active, non-NULL = archived
);

CREATE INDEX IF NOT EXISTS idx_projects_alias_alias        ON projects_alias(alias);
CREATE INDEX IF NOT EXISTS idx_projects_alias_archived_at  ON projects_alias(archived_at);

CREATE TABLE IF NOT EXISTS path_history (
    id_sha256       TEXT NOT NULL,           -- canonical project id
    path            TEXT NOT NULL,           -- one of the paths this project_id has been seen at
    first_seen_at   INTEGER NOT NULL,        -- ms
    last_seen_at    INTEGER NOT NULL,        -- ms
    PRIMARY KEY (id_sha256, path),
    FOREIGN KEY (id_sha256) REFERENCES projects_alias(id_sha256) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_path_history_id_sha256  ON path_history(id_sha256);
CREATE INDEX IF NOT EXISTS idx_path_history_path       ON path_history(path);
