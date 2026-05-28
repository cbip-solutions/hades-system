--
-- Migration 045: workforce durable queues (HADES design stage).
-- Introduces three independent tables: workforce_tasks (SharedTaskList),
-- workforce_checkpoints (CheckpointQueue), workforce_fix_prompts (FixPromptQueue).
-- No foreign keys between tables (independent failure domains per design contract).
-- project_id on every row for logical isolation (spec §7.1).
-- schemaVersion bumped to 11 by internal/store/schema.go.
--
-- PRAGMA WAL + busy_timeout MUST be set by the adapter constructor (invariant).

-- SharedTaskList: Kanban board.
-- UNIQUE on (project_id, task_id) enforces idempotent Enqueue per design contract
CREATE TABLE IF NOT EXISTS workforce_tasks (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id       TEXT    NOT NULL,
    project_id    TEXT    NOT NULL,
    title         TEXT    NOT NULL DEFAULT '',
    description   TEXT    NOT NULL DEFAULT '',
    status        TEXT    NOT NULL DEFAULT 'pending'
                  CHECK (status IN ('pending','in_progress','review','done','failed')),
    thread_id     TEXT    NOT NULL DEFAULT '',
    priority      INTEGER NOT NULL DEFAULT 0,
    error_detail  TEXT    NOT NULL DEFAULT '',
    created_at    INTEGER NOT NULL,    -- Unix seconds UTC
    updated_at    INTEGER NOT NULL,    -- Unix seconds UTC
    UNIQUE (project_id, task_id)
);
CREATE INDEX IF NOT EXISTS idx_workforce_tasks_project_status
    ON workforce_tasks(project_id, status);
CREATE INDEX IF NOT EXISTS idx_workforce_tasks_thread_id
    ON workforce_tasks(thread_id)
    WHERE thread_id != '';
CREATE INDEX IF NOT EXISTS idx_workforce_tasks_priority
    ON workforce_tasks(project_id, priority, created_at);

-- CheckpointQueue: async durable channel Worker → L2 Reviewer.
-- thread_id is the LangGraph-style stable key (design choice C).
-- deadline_at is Unix seconds for invariant hook (HADES design measures).
-- consumed = 0 (unconsumed) | 1 (consumed by HADES design orchestrator).
CREATE TABLE IF NOT EXISTS workforce_checkpoints (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id       TEXT    NOT NULL,
    project_id    TEXT    NOT NULL,
    thread_id     TEXT    NOT NULL,
    state_json    TEXT    NOT NULL,    -- JSON-encoded checkpoint state blob
    sequence_num  INTEGER NOT NULL DEFAULT 0,
    deadline_at   INTEGER,             -- Unix seconds; NULL = no deadline
    consumed      INTEGER NOT NULL DEFAULT 0 CHECK (consumed IN (0, 1)),
    created_at    INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_workforce_checkpoints_task
    ON workforce_checkpoints(project_id, task_id, consumed);
-- ByThread() queries filter only by thread_id (no consumed predicate),
-- so a single-column index is the right shape. Drain/Peek queries
-- use the (project_id, task_id, consumed) covering index above.
CREATE INDEX IF NOT EXISTS idx_workforce_checkpoints_thread
    ON workforce_checkpoints(thread_id);

-- FixPromptQueue: async durable channel L2/L3/L4 → next worker iteration.
-- worker_id identifies the destination worker (consumed by that worker only).
-- consumed = 0 (pending) | 1 (consumed by worker at iteration start).
CREATE TABLE IF NOT EXISTS workforce_fix_prompts (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id       TEXT    NOT NULL,
    project_id    TEXT    NOT NULL,
    worker_id     TEXT    NOT NULL,
    reviewer_tier TEXT    NOT NULL
                  CHECK (reviewer_tier IN ('l2','l3','l4')),
    prompt_text   TEXT    NOT NULL,
    criteria_name TEXT    NOT NULL DEFAULT 'default',
    severity      TEXT    NOT NULL DEFAULT 'minor'
                  CHECK (severity IN ('minor','major','reject')),
    consumed      INTEGER NOT NULL DEFAULT 0 CHECK (consumed IN (0, 1)),
    created_at    INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_workforce_fix_prompts_worker
    ON workforce_fix_prompts(project_id, worker_id, consumed);
