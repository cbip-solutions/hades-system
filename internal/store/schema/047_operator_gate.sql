-- operator_gate_state — Plan 4 Phase E.
--
-- Singleton row (id=1) records the OperatorGate state across daemon
-- restarts.  On first boot the row does not exist; GateAdapter.LoadState
-- returns StateRunning in that case (caller fills the default).  On every
-- Pause/Resume the row is UPSERTED.
--
-- reason is human-readable; logged by daemon for operator inspection via
-- /v1/workforce/gate/state.  updated_at enables stale-pause detection
-- (Phase G may surface "paused > 24h without operator acknowledgement").
--
-- Only one row is ever present (id PRIMARY KEY = 1 enforced by UPSERT).

CREATE TABLE IF NOT EXISTS operator_gate_state (
    id         INTEGER PRIMARY KEY DEFAULT 1 CHECK(id = 1),
    state      TEXT    NOT NULL
                   CHECK(state IN ('running','paused_descriptive','paused_quiet','paused_after_apply')),
    reason     TEXT    NOT NULL DEFAULT '',
    updated_at INTEGER NOT NULL    -- UTC unix seconds (inv-zen-005)
);
