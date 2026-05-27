--
-- Migration 049: worker specs persistence (the release design release track Task D-8).
-- schemaVersion bumps 12 → 13.
--
-- Three tables for the three workforce variants. Separate tables (vs
-- a single worker_specs table with a variant column) keep the schema
-- explicit and let the release design + the release design add variant-specific columns
-- without conditional checks at the call site.
--
-- All three carry project_id (spec §7.1 logical isolation; release track
-- queues already enforce this on every row).
--
-- inv-hades-031 boundary preserved: internal/workforce/worker MUST NOT
-- import internal/store. The daemon adapter (release track) will wire the
-- read/write surface; release track ships only the schema so the release design
-- orchestrator persistence is unblocked.
--
-- IF NOT EXISTS aligns with prior migrations (032..048) and lets the
-- DDL re-execute safely against a database that already has the tables.

-- worker_specs: VariantWorker rows. The leaf-level executor.
CREATE TABLE IF NOT EXISTS worker_specs (
    id              TEXT NOT NULL,                 -- WorkerSpec.ID
    project_id      TEXT NOT NULL,
    task_tier       TEXT NOT NULL
                    CHECK (task_tier IN ('trivial','simple','medium','complex')),
    model_class     TEXT NOT NULL,                 -- e.g. tier-medium
    tools_json      TEXT NOT NULL DEFAULT '[]',    -- JSON array of tool names
    quota_max_tokens   INTEGER NOT NULL,
    quota_max_cost_usd REAL NOT NULL,
    quota_max_duration_ns INTEGER NOT NULL,        -- nanoseconds
    recovery_policy TEXT NOT NULL
                    CHECK (recovery_policy IN ('auto-respawn','manual','doctrine-bound')),
    doctrine_name   TEXT NOT NULL,                 -- max-scope|default|capa-firewall|<custom>
    created_at      INTEGER NOT NULL,              -- Unix seconds UTC
    updated_at      INTEGER NOT NULL,
    PRIMARY KEY (id, project_id)
);
CREATE INDEX IF NOT EXISTS idx_worker_specs_project ON worker_specs(project_id);
CREATE INDEX IF NOT EXISTS idx_worker_specs_doctrine ON worker_specs(doctrine_name);

-- team_lead_specs: VariantTeamLead rows. Persistent composition layer.
-- The TeamLead's persistent subprocess registry lives in
-- subprocess_sessions (migration 048); this table is the spec snapshot
-- (matches the worker_specs shape but kept separate for variant-clarity
-- and the release design future-extension).
CREATE TABLE IF NOT EXISTS team_lead_specs (
    id              TEXT NOT NULL,
    project_id      TEXT NOT NULL,
    task_tier       TEXT NOT NULL
                    CHECK (task_tier IN ('trivial','simple','medium','complex')),
    model_class     TEXT NOT NULL,
    tools_json      TEXT NOT NULL DEFAULT '[]',
    quota_max_tokens   INTEGER NOT NULL,
    quota_max_cost_usd REAL NOT NULL,
    quota_max_duration_ns INTEGER NOT NULL,
    recovery_policy TEXT NOT NULL
                    CHECK (recovery_policy IN ('auto-respawn','manual','doctrine-bound')),
    doctrine_name   TEXT NOT NULL,
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL,
    PRIMARY KEY (id, project_id)
);
CREATE INDEX IF NOT EXISTS idx_team_lead_specs_project ON team_lead_specs(project_id);
CREATE INDEX IF NOT EXISTS idx_team_lead_specs_doctrine ON team_lead_specs(doctrine_name);

-- reviewer_specs: VariantReviewerL2 / L3 / L4 rows. The reviewer_tier
-- column carries the variant (l2|l3|l4) so a single table holds all
-- three (matches the queue layer's reviewer_tier enum).
CREATE TABLE IF NOT EXISTS reviewer_specs (
    id              TEXT NOT NULL,
    project_id      TEXT NOT NULL,
    reviewer_tier   TEXT NOT NULL
                    CHECK (reviewer_tier IN ('l2','l3','l4')),
    task_tier       TEXT NOT NULL
                    CHECK (task_tier IN ('trivial','simple','medium','complex')),
    model_class     TEXT NOT NULL,
    tools_json      TEXT NOT NULL DEFAULT '[]',
    quota_max_tokens   INTEGER NOT NULL,
    quota_max_cost_usd REAL NOT NULL,
    quota_max_duration_ns INTEGER NOT NULL,
    recovery_policy TEXT NOT NULL
                    CHECK (recovery_policy IN ('auto-respawn','manual','doctrine-bound')),
    doctrine_name   TEXT NOT NULL,
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL,
    PRIMARY KEY (id, project_id, reviewer_tier)
);
CREATE INDEX IF NOT EXISTS idx_reviewer_specs_project ON reviewer_specs(project_id);
CREATE INDEX IF NOT EXISTS idx_reviewer_specs_tier ON reviewer_specs(reviewer_tier);
CREATE INDEX IF NOT EXISTS idx_reviewer_specs_doctrine ON reviewer_specs(doctrine_name);
