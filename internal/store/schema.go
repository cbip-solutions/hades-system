// SPDX-License-Identifier: MIT
package store

import (
	_ "embed"
	"fmt"
)

// schemaVersion is the current schema version. Increment when adding migrations.
// plans should add migrations rather than alter v1 except via additive
// columns (which always go in a new migration).
//
// bypass_anomaly_observations (per-event rolling-window numerator).
//
// - v4: conversation_id column on bypass_audit (design choice D)
//
// - v5: bypass_audit_bodies (encrypted bodies, invariant)
//
// - v6: bypass_audit_pins (retention-exempt registry)
//
// - v7: conversation_wal (per-conversation write-ahead log)
//
// - v8: idempotency_keys (TTL 24h replay cache)
//
// - v9: notifications (bypass-event ledger + 1h CRITICAL repeat)
//
// - v10: cost_ledger (one row per LLM request, USD-converted, idempotency
// UNIQUE for invariant no-double-charge guarantee)
//
// - v11: pin_overrides (design choice D pin hierarchy with optional TTL, invariant)
//
// - v12: doctrine_state singleton (last-loaded Resolved.Schema +
// Provenance JSON snapshot; daemon reads on startup so
//
// - v13: workforce_tasks (SharedTaskList), workforce_checkpoints
// (CheckpointQueue), workforce_fix_prompts (FixPromptQueue).
// WAL + busy_timeout enforced by workforceadapter (invariant).
// project_id on every row for logical isolation (spec §7.1).
//
// - v14: subprocess_sessions — persistent TeamLead + Reviewer L3/L4
// subprocess registry for crash recovery (design choice C lifecycle).
// Idempotency key (spec_id, doctrine_name); ttl_seconds drives the
// invariant TTL evictor.
//
// - v15: worker_specs / team_lead_specs / reviewer_specs — three tables
// holding the immutable WorkerSpec snapshots persisted by the daemon
// adapter. ships the
// schema so HADES design orchestrator persistence is unblocked.
//
// - v16: aggregation_windows + aggregation_events — AggregationStream
// SQLite-durability layer. Window open/close/event records survive
// daemon restart; LoadOpenWindows surfaces in-progress windows for
// recovery. Partial index on status='open' accelerates restart scan.
//
// - v17: operator_gate_state — singleton row (id=1 UPSERT) persisting
// OperatorGate pause/resume state across daemon restarts. CHECK
// constraint enforces the four-value State enum.
//
// - v18: cost_axis_tags + axis_tag_loss_events — 4-axis attribution
// store (project × doctrine × stage × task; +operation +worker_id
// optional). UNIQUE (cost_id, axis_name) + INSERT OR IGNORE for
// PostCall idempotency. cost_ledger now exists on main (HADES design
// F-1 merged), so a future migration may add the FK; current
// migration ships without FK to keep the diff minimal.
//
// - v19: budget_pauses + budget_anomalies + budget_anomaly_samples
// — 4-scope hierarchical pause state machine + z-score event log
//
// - per-scope rolling sample window. invariant: internal/budget
// never imports internal/store; bridge via dispatcheradapter.
//
// - v20: budget_anomaly_samples gains cost_id column + UNIQUE
// (scope, scope_value, cost_id). PostCallWithCost retries become
// idempotent at the SQL layer via INSERT OR IGNORE. Legacy rows
// (none on production yet) get cost_id = -id so UNIQUE is satisfied.
// New callers MUST pass a positive cost_id for the idempotency
// guarantee; cost_id=0 is the sentinel for opt-out callers
// (CLI bulk-import, retro-fill).
//
// - v21: research_cache — sha256-keyed response cache for the research MCP
// . TTL enforced by handler; background eviction goroutine 1h.
//
// - v22: audit_events_raw — daemon-owned audit event ledger. All MCPs and
// handlers emit here via POST /v1/audit/emit. HADES design wraps with hash-chain
// OTel export WITHOUT schema migration (additive ALTER TABLE only).
//
// - v23: substrate_health — per-commit test pass-rate + doctrine-lint
// outcome. The regression-by-self detector queries this table to spot
// "substrate is regressing on its own commits" (Apr 23 chicken-and-egg
// failure mode). HADES design extends additively (history queries, time-series,
// adversarial corpus). invariant boundary: writes go through the
// SubstrateHealthWriter interface declared in safetynet/regression.go;
// adapter wired in (internal/daemon/orchestratoradapter/).
//
// - v24: projects_alias + path_history — sha256 canonical id (content-
// addressable) separated from human alias (operator-facing). Path
// history table tracks every (id_sha256, path) tuple ever observed,
// enabling mv-detection in projectctx.DetectMv. ON DELETE CASCADE
// links path_history → projects_alias. invariant.
//
// - v25: priority_overrides — UNIQUE(project_alias), multiplier > 0,
// reason NOT NULL. Set/Reset emit audit events in the same SQL
// transaction as the row mutation (invariant). will add
// tmux_session_state in a subsequent migration; the reservation in
// master plan §"Migration numbering coordination" was for the joint
// pair, but ships only the priority_overrides half because
// owns its own DDL.
//
// - v26: tmux_session_state — one row per spawned hades-system tmux
// session, keyed by canonical "hades-<alias>-<sha8>" name. Status
// four-value enum (Active=0/Idle=1/Orphaned=2/Archived=3) bounded
// by SQL CHECK + Go validateTmuxStatus (defense in depth).
// expected_panes is a JSON-encoded map[WindowName][]string of
// daemon-recorded pane ids per daemon-owned window — EXCLUDES
// WindowScratch (invariant enforced by the tmuxlife encoder).
// Drift note: master plan reserved slot 060 for a joint
// priority_overrides + tmux_session_state migration;
// shipped 060_priority_overrides.sql alone, so picks
// the next free number (slot 061 reserved for knowledge-
// index on a separate DB file). invariant: internal/tmuxlife
// never imports internal/store; wires the adapter that
// bridges tmuxlife.SessionStore to *store.Store via these CRUD
// primitives.
//
// - v27: schedules + schedule_history — durable scheduler substrate
// for Routine + Task + Loop schedules (3-tier per design contract)
// plus per-fire outcome ledger driving `hades schedule history`.
// Five CHECK-constrained enums (tier, trigger_type, miss_policy,
// status, outcome) plus three indexes (idx_schedules_due partial
// for the tick scan, idx_schedules_project_alias for `hades
// schedule list --project=...`, idx_schedule_history_lookup for
// time-window queries). invariant: internal/scheduler never
// imports internal/store; the adapter at
// internal/daemon/scheduleradapter is the only legitimate bridge.
// Drift note: master plan §"Migration numbering coordination"
// reserved slot 059 for under an A→C→B→D→E execution
// sequence; reality at HEAD on 2026-05-07 has 057 / 060 / 062
// already taken (Phases A / B-6 / C-11). picks 063 —
// the next free number on the daemon.db chain. Slot 059 is
// reserved for inbox storage; slot 061 for
// knowledge-index DB (separate SQLite file, no daemon.db
// schemaVersion bump).
//
// - v28: per-project `inbox` table + daemon.db `inbox_aggregator_cache`.
// design choice C hybrid storage substrate. Per-project authoritative inbox
// carries severity 4-tier CHECK (invariant), 5min sliding-window
// dedup UNIQUE on (event_type, content_hash, created_at_bucket),
// and partial unacked index for the hot render path. Daemon-level
// aggregator cache is a denormalized read view written by the
// outbox bridge on every per-project INSERT;
// project_id + project_alias indexed for the `hades day`
// cross-project digest hot path;
// UNIQUE (project_id, notification_id) prevents duplicate fanout
// under at-least-once outbox replay. Cold rebuildable from
// per-project sources on daemon boot (Aggregator.Rebuild). Cascade
// delete on project rm via the per-project DB-file unlink (atomic).
// invariant: internal/inbox MUST NEVER import internal/store;
// internal/daemon/inboxadapter is the only legitimate
// bridge. invariant: aggregator cache rows carry source-DB
// project_id (no cross-project leak).
// Drift note: spec-frozen HADES design projected migrationV27
// under the original A→C→B→D→E execution sequence (
// would land 059 instead of 063). Reality at HEAD on 2026-05-07
// has already at v27, so picks v28 — the next
// free number. The migration FILE number stays at 058 (slot
// reserved per master plan §"Migration numbering coordination").
//
// - v29: audit_events_raw chain integration — adds four chain columns
// (prev_hash, record_hash, partition_id, tessera_leaf_id) + REFUSE
// triggers (invariant append-only enforcement) + monthly partition
// view (audit_events_partitions) + audit_partition_seals CRUD table.
// Chain hashes are computed in app-layer (auditadapter post-INSERT
// same-row UPDATE) — no SQL trigger recursion. Boundary invariant:
// internal/audit/chain MUST NEVER import internal/store; the bridge
// is internal/daemon/auditadapter. schemaVersion bump
// path: 28 → 29 (this migration).
const schemaVersion = 31

// migrationV21 adds research_cache — HADES design task, design choice B.
//
//go:embed schema/054_research_cache.sql
var migrationV21 string

// migrationV22 adds audit_events_raw — HADES design task, design choice B.
//
//go:embed schema/055_audit_events_raw.sql
var migrationV22 string

// migrationV2 is the bypass_anomalies migration. Lives in a sibling file
// rather than inline so the SQL is editable without rebuilding Go strings.
//
//go:embed schema/032_bypass_anomalies.sql
var migrationV2 string

// migrationV3 introduces bypass_anomaly_observations — a per-event
// observation log used as the rolling-window numerator. v2's aggregated
// row was structurally weak for sliding-window analytics (it saturated
// at steady state). v3 keeps the v2 lifetime aggregate AND adds the
// observations table; QueryAnomalyCount now counts from observations.
//
//go:embed schema/033_bypass_anomaly_observations.sql
var migrationV3 string

// migrationV4 adds conversation_id to bypass_audit so can group
// audit rows by upstream conversation (invariant / design choice D pin registry).
//
//go:embed schema/034_bypass_audit.sql
var migrationV4 string

// migrationV5 adds bypass_audit_bodies — the AES-256-GCM-encrypted body
// table populated only when tier=in-house (invariant, invariant).
//
//go:embed schema/035_bypass_audit_bodies.sql
var migrationV5 string

// migrationV6 adds bypass_audit_pins — the operator pin registry that
// exempts marked conversations from the nightly retention purge (design choice D).
//
//go:embed schema/036_bypass_audit_pins.sql
var migrationV6 string

// migrationV7 adds conversation_wal — Layer 1 of the bypass resilience
// model. One row per orchestrator turn; pending state
// persisted BEFORE upstream call so a restart can replay the in-flight
// turn (half of invariant).
//
//go:embed schema/037_conversation_wal.sql
var migrationV7 string

// migrationV8 adds idempotency_keys — TTL 24h replay cache (HADES design
// ). MarkPending persists BEFORE upstream call; MarkCompleted
// stores the full response so a restart in the upstream-response →
// orchestrator-delivery gap replays without a second upstream charge
// (other half of invariant).
//
//go:embed schema/038_idempotency.sql
var migrationV8 string

// migrationV9 adds notifications — bypass-event ledger (HADES design,
// task, spec §8.4). Severity ∈ {INFO,WARN,CRITICAL}; CRITICAL rows
// re-fire macOS osascript every 1h until acknowledged. Distinct from
// notifications_queue.
//
//go:embed schema/039_notifications.sql
var migrationV9 string

// migrationV10 adds cost_ledger — Layer 2 of orchestrator observability
// . One row per LLM request, with
// cost_usd already converted to USD by the provider's RateCard;
// idempotency_key UNIQUE blocks double-charge under retry / concurrent
// dispatch (the Go layer translates the SQL UNIQUE failure into
// ErrDuplicateIdempotency via cost_ledger.go).
//
//go:embed schema/040_cost_ledger.sql
var migrationV10 string

// migrationV11 adds pin_overrides — operator-set tier pins at three scope
// levels (session, project, global) with optional TTL (HADES design,
// task, invariant). UNIQUE(scope, scope_id) with scope_id=” for
// global (SQLite NULL-distinctness workaround). expires_at is INTEGER unix
// seconds; NULL means permanent. The 5-min sweep runs PurgeExpiredPins.
//
//go:embed schema/043_pin_overrides.sql
var migrationV11 string

// migrationV12 adds doctrine_state — singleton row holding the last-
// loaded Resolved.Schema + Provenance JSON snapshot (HADES design,
// task A-6). Daemon reads on startup so HADES design workers dispatched
// before a restart see consistent doctrine values across the restart
// boundary; wires the /v1/doctrine/state endpoint.
//
//go:embed schema/044_doctrine_state.sql
var migrationV12 string

// migrationV13 adds the three workforce durable queue tables (HADES design
// ): workforce_tasks (SharedTaskList), workforce_checkpoints
// (CheckpointQueue), workforce_fix_prompts (FixPromptQueue). All three
// tables carry project_id for logical isolation (spec §7.1). WAL mode
// + busy_timeout=5000 are enforced by workforceadapter constructors
// (invariant); independent failure domains, no FK between tables
// (spec §2.2).
//
//go:embed schema/045_workforce_queues.sql
var migrationV13 string

// migrationV14 adds subprocess_sessions — HADES design task,
// persistent TeamLead + Reviewer L3/L4 crash recovery (design choice C lifecycle).
// Ephemeral Worker rows never appear here; only persistent variants.
// Idempotency key (spec_id, doctrine_name). TTL semantics diverge per
// doctrine so the same SpecID under two doctrines yields two rows.
// ttl_seconds + last_use_at drive the invariant evictor. invariant
// preserved by the SessionStore interface in subprocess package (no
// internal/store import there); wires the adapter.
//
//go:embed schema/048_subprocess_lifecycle.sql
var migrationV14 string

// migrationV15 adds worker_specs / team_lead_specs / reviewer_specs —
// snapshots persisted by the daemon adapter. project_id
// on every row (spec §7.1). reviewer_specs has a composite PK
// (id, project_id, reviewer_tier) so the same spec ID can hold L2/L3/L4
// rows independently. invariant preserved: internal/workforce/worker
// MUST NOT import internal/store; the daemon workforceadapter
// owns the read/write surface.
//
//go:embed schema/049_worker_specs.sql
var migrationV15 string

// migrationV16 adds aggregation_windows + aggregation_events — HADES design
// AggregationStream SQLite-durability layer (invariant boundary:
// workforce/stream never imports internal/store; bridge via StreamAdapter
// in workforceadapter). Window open/close/event records survive daemon
// restart; LoadOpenWindows surfaces in-progress windows for recovery.
// Partial index on status='open' accelerates restart scan.
//
//go:embed schema/046_aggregation_streams.sql
var migrationV16 string

// migrationV17 adds operator_gate_state — singleton row (id=1 UPSERT)
// persisting OperatorGate pause/resume state across daemon restarts (HADES design
// ). CHECK constraint enforces the four-value State enum; LoadState
// returns StateRunning when row absent (clean boot). invariant: gate/*
// never imports internal/store; bridge via GateAdapter in workforceadapter.
//
//go:embed schema/047_operator_gate.sql
var migrationV17 string

// migrationV18 adds cost_axis_tags + axis_tag_loss_events — HADES design
// task, design choice C, invariant. The 4-axis attribution store
// (project × doctrine × stage × task; +operation +worker_id optional).
// UNIQUE (cost_id, axis_name) + INSERT OR IGNORE in the Go layer keeps
// PostCall idempotent under retries. axis_tag_loss_events records every
// missing-axis incident so completeness drift surfaces immediately rather
// than silently degrading invariant. invariant: internal/budget never
// imports internal/store; bridge via dispatcheradapter/budget_hooks.go.
// The cost_ledger (v10) is already present on main; this migration
// ships without an FK back to keep the diff minimal — a future
// migration may add it once budget tag callers are validated.
//
//go:embed schema/051_budget_axes.sql
var migrationV18 string

// migrationV19 adds budget_pauses + budget_anomalies + budget_anomaly_samples
// — HADES design task, design choice C, invariant + invariant. 4-scope
// hierarchical pause state machine (project / doctrine / stage / worker_id),
// z-score event log, and per-scope rolling sample window. budget_pauses
// PRIMARY KEY (scope, scope_value) + UPSERT keeps the latest reason on
// re-trigger. budget_anomaly_samples is housekeeping-pruned
// (>24h cutoff). invariant: internal/budget never imports
// internal/store; bridge via dispatcheradapter.
//
//go:embed schema/052_budget_pause.sql
var migrationV19 string

// migrationV20 adds cost_id column + UNIQUE constraint on
// budget_anomaly_samples — HADES design post-review C-2 fix. The
// PostCallWithCost retry path was non-idempotent: a partial-failure
// retry double-counted samples in the rolling window, inflating the
// denominator. Adding cost_id + UNIQUE (scope, scope_value, cost_id)
// + INSERT OR IGNORE in the new AppendAnomalySampleByCostID writer
// makes retries SQL-layer idempotent (mirrors cost_axis_tags pattern).
// Legacy rows get cost_id = -id (preserves data, never collides with
// real positive cost_id values).
//
//go:embed schema/053_budget_anomaly_samples_cost_id.sql
var migrationV20 string

// migrationV23 adds substrate_health — HADES design task, design choice C
// regression-by-self metric. Per-commit test pass-rate + doctrine-lint
// outcome storage. HADES design extends additively. invariant: safetynet
// writes go through SubstrateHealthWriter interface; adapter in
//
//go:embed schema/056_substrate_health.sql
var migrationV23 string

// migrationV24 introduces projects_alias + path_history — HADES design
// implements AIP-2510 dual-ID per design contract
// alias separation; path_history tracks every (id_sha256, path) tuple
// ever observed, enabling mv-detection in projectctx.DetectMv. ON
// DELETE CASCADE links path_history → projects_alias. invariant.
//
//go:embed migrations/057_projects_alias_path_history.sql
var migrationV24 string

// migrationV25 introduces priority_overrides — HADES design ships
// the Layer 3 operator override seam per design contract(project_alias)
// + multiplier > 0 + reason NOT NULL constraints. internal/quota declares
// the OverrideStore interface; internal/daemon/quotaadapter is the only
// package permitted to bridge to *store.Store (invariant + invariant).
// Set / Reset emit audit events in the SAME transaction as the row
// mutation — invariant audit-chain integrity.
//
//go:embed migrations/060_priority_overrides.sql
var migrationV25 string

// migrationV26 introduces tmux_session_state — HADES design ships
// the per-session lifecycle storage row keyed by canonical
// "hades-<alias>-<sha8>" name. Status four-value enum bounded by SQL CHECK
// + Go validateTmuxStatus (defense in depth, invariant boundary
// preserved). expected_panes JSON excludes WindowScratch (invariant
// enforced by the tmuxlife encoder). The adapter
// (internal/daemon/handlers/sessions.go) is the only legitimate bridge
// to *store.Store via this package's CRUD primitives.
//
// Drift note: slot 060 was reserved for a joint priority_overrides +
// tmux_session_state migration in the master plan; shipped
// 060_priority_overrides.sql alone, so picks 062 (slot 061
// reserved for knowledge-index on a separate DB file).
//
//go:embed migrations/062_tmux_session_state.sql
var migrationV26 string

// migrationV27 introduces schedules + schedule_history — HADES design stage
// D-1 ships the durable scheduler substrate. The schedules table hosts
// Routine + Task + Loop schedules (3-tier per design contract); the
// schedule_history table is the append-only fire-attempt outcome
// ledger driving `hades schedule history`. Five CHECK-constrained enums
// (tier 0..2, trigger_type 0..2, miss_policy 0..3, status 0..2,
// outcome 0..3) lock the contract at the SQL layer; the Go-side
// validators in internal/store/schedules.go reject out-of-range values
// before the SQL CHECK fires (defense in depth).
//
// invariant: internal/scheduler/* MUST NEVER import internal/store;
// internal/daemon/scheduleradapter/ is the only package permitted to
// bridge scheduler value types to *store.Store via this package's
// CRUD primitives.
//
// Drift note: master plan §"Migration numbering coordination" reserved
// slot 059 for under an A→C→B→D→E execution sequence; reality
// at HEAD on 2026-05-07 has 057 / 060 / 062 already taken (Phases A /
// B-6 / C-11). picks 063 — the next free number on the
// daemon.db chain. Slot 059 is reserved for inbox storage;
// slot 061 for knowledge-index DB (separate SQLite file).
//
//go:embed migrations/063_schedules.sql
var migrationV27 string

// migrationV28 introduces per-project `inbox` + daemon.db
// `inbox_aggregator_cache` — HADES design task ships the design choice C
// hybrid storage substrate.
//
// Per-project authoritative inbox: severity 4-tier CHECK (invariant),
// 5min sliding-window dedup UNIQUE on (event_type, content_hash,
// created_at_bucket), partial idx_inbox_unacked for the hot render
// path. Cascade-delete on project rm via per-project DB-file unlink
// (atomic; no SQL-layer FK).
//
// Daemon-level aggregator cache: denormalized read view written by
// the outbox bridge on every per-project INSERT;
// project_id + project_alias indexed for the `hades day` cross-project
// digest hot path; UNIQUE (project_id,
// notification_id) prevents duplicate fanout under at-least-once
// outbox replay. Cold rebuildable from per-project sources on daemon
// boot (Aggregator.Rebuild, ~1s for 10 projects per spec target).
//
// invariant: internal/inbox/* MUST NEVER import internal/store;
// internal/daemon/inboxadapter/ is the only package
// permitted to bridge inbox value types to *store.Store via this
// migration's tables.
//
// invariant: aggregator cache rows carry source-DB project_id
// (no cross-project leak). Compile-time anchor in inbox/sentinel.go;
// runtime via outbox bridge writing project_id from source scope;
// property-based fuzz test in
// tests/compliance/inv_hades_113_no_cross_project_inbox_leak_test.go.
//
// Drift note: spec-frozen HADES design projected migrationV27 under
// the original A→C→B→D→E execution sequence ( would land 059
// instead of 063). Reality at HEAD on 2026-05-07 has already
// at v27, so picks v28 — the next free number. The migration
// FILE number stays at 058 (slot reserved per master plan
// §"Migration numbering coordination"). schemaVersion bump path:
// 27 → 28 (this migration).
//
//go:embed migrations/058_inbox_aggregator_cache.sql
var migrationV28 string

// migrationV29 introduces audit_events_raw chain integration — HADES design
// ships the design choice C decision: per-event Tessera leaf + per-partition
// seal hybrid granularity. Four chain columns added via additive ALTER
// TABLE (prev_hash, record_hash, partition_id all TEXT NOT NULL DEFAULT ”;
// tessera_leaf_id TEXT NULL) plus REFUSE triggers enforcing invariant
// append-only at the SQL layer (BEFORE UPDATE on truly-immutable columns,
// BEFORE UPDATE WHEN already-set on chain hashes / partition_id /
// tessera_leaf_id one-time-write columns, BEFORE DELETE unconditional).
// audit_events_partitions VIEW aggregates first_id/last_id/event_count/
// final_record_hash per partition_id (correlated subquery for chain tip
// extraction; SQLite 3.40+-compat). audit_partition_seals TABLE holds
// monthly seal records keyed by partition_id with sealed_at +
// final_record_hash + tessera_seal_leaf_id + daemon_witness_signature
// (NOT NULL CHECK length>0, invariant) + optional cold_archive_url +
// cold_archive_content_hash cold-archive write-back.
//
// Chain compute is app-layer (auditadapter post-INSERT same-row UPDATE);
// no SQL trigger recursion. The four refusing triggers also permit the
// chain compute path because they fire WHEN OLD.<col> != ” / IS NOT NULL
// — the one-time write that flips ” to non-empty (or NULL to non-NULL
// for tessera_leaf_id) is allowed.
//
// Boundary (invariant): internal/audit/chain MUST NEVER
// import internal/store; the bridge is internal/daemon/auditadapter
// , which translates between chain.* value types and store
// row types via field-by-field copy. This package's audit_chain.go
// holds the store-side CRUD primitives (GetChainTip, GetEventByID,
// UpdateChainColumns, UpdateTesseraLeafID, InsertPartitionSeal,
// GetPartitionSeal, ListPartitions, ListEventsForPartition,
// BackfillScan).
//
//go:embed schema/059_audit_chain_extension.sql
var migrationV29 string

// migrationV30 adds cost_ledger.provider — HADES design Task 9, C9,
// invariant. Per-provider cost attribution: the dispatcher cascade
// iterates NAMED backends, so cost must be persisted at Backend.Name()
// granularity (not just providers.Tier). DEFAULT ” so pre-HADES design rows
// decode cleanly. The cost_ledger window index is rebuilt to include
// provider. schemaVersion bump path: 29 → 30.
//
//go:embed migrations/064_cost_ledger_provider.sql
var migrationV30 string

// migrationV31 adds tier_health_samples — HADES design Task 11, C9,
// invariant. Per-provider health observability: one row per backend
// outcome (dispatcher attempt + RecoveryScheduler probe). provider is
// Backend.Name() — the per-provider counterpart to the per-Name circuit
// breaker. Append-only, no UNIQUE (health samples are not idempotency-
// keyed). schemaVersion bump path: 30 → 31.
//
//go:embed migrations/065_tier_health_samples.sql
var migrationV31 string

var migrations = []string{

	"\n\t-- Schema version tracking ------------------------------------------------\n\n\tCREATE TABLE IF NOT EXISTS schema_version (\n\t\tversion    INTEGER PRIMARY KEY,\n\t\tapplied_at INTEGER NOT NULL\n\t);\n\n\t-- Core daemon event log (spec §5.3) ---------------------------------------\n\n\tCREATE TABLE IF NOT EXISTS events (\n\t\tid           INTEGER PRIMARY KEY AUTOINCREMENT,\n\t\tts           INTEGER NOT NULL,           -- UTC unix seconds (invariant)\n\t\tproject      TEXT,                       -- empty for daemon-level events\n\t\tsession_id   TEXT,\n\t\tswarm_id     TEXT,\n\t\ttask_id      TEXT,\n\t\ttype         TEXT NOT NULL,              -- e.g. session.created, task.codegen.completed\n\t\tpayload_json TEXT\n\t);\n\tCREATE INDEX IF NOT EXISTS idx_events_ts          ON events(ts);\n\tCREATE INDEX IF NOT EXISTS idx_events_project     ON events(project);\n\tCREATE INDEX IF NOT EXISTS idx_events_swarm       ON events(swarm_id);\n\tCREATE INDEX IF NOT EXISTS idx_events_session     ON events(session_id);\n\tCREATE INDEX IF NOT EXISTS idx_events_type        ON events(type);\n\n\t-- LLM call accounting (HADES design fills) --------------------------------------\n\n\tCREATE TABLE IF NOT EXISTS llm_calls (\n\t\tid          INTEGER PRIMARY KEY AUTOINCREMENT,\n\t\tts          INTEGER NOT NULL,\n\t\tproject     TEXT,\n\t\tswarm_id    TEXT,\n\t\ttask_id     TEXT,\n\t\tprovider    TEXT NOT NULL,\n\t\tmodel       TEXT NOT NULL,\n\t\ttokens_in   INTEGER NOT NULL,\n\t\ttokens_out  INTEGER NOT NULL,\n\t\tlatency_ms  INTEGER,\n\t\tcost_usd    REAL                        -- nullable: $0 marginal under bypass\n\t);\n\tCREATE INDEX IF NOT EXISTS idx_llm_calls_ts       ON llm_calls(ts);\n\tCREATE INDEX IF NOT EXISTS idx_llm_calls_project  ON llm_calls(project);\n\tCREATE INDEX IF NOT EXISTS idx_llm_calls_provider ON llm_calls(provider);\n\n\t-- Operator + orchestrator decisions (HADES design) ------------------------------\n\n\tCREATE TABLE IF NOT EXISTS decisions (\n\t\tid            INTEGER PRIMARY KEY AUTOINCREMENT,\n\t\tts            INTEGER NOT NULL,\n\t\tproject       TEXT,\n\t\tscope         TEXT NOT NULL,            -- e.g. archive, conflict, override\n\t\tdecision      TEXT NOT NULL,\n\t\tjustification TEXT,\n\t\tactor         TEXT NOT NULL             -- \"operator\" | \"orchestrator\" | \"auto\"\n\t);\n\tCREATE INDEX IF NOT EXISTS idx_decisions_ts       ON decisions(ts);\n\tCREATE INDEX IF NOT EXISTS idx_decisions_project  ON decisions(project);\n\n\t-- Auto-memory writes audit (HADES design) ---------------------------------------\n\n\tCREATE TABLE IF NOT EXISTS memory_writes (\n\t\tid            INTEGER PRIMARY KEY AUTOINCREMENT,\n\t\tts            INTEGER NOT NULL,\n\t\tproject       TEXT NOT NULL,\n\t\tfile_path     TEXT NOT NULL,\n\t\taction        TEXT NOT NULL,            -- \"create\" | \"update\" | \"delete\"\n\t\tcontent_hash  TEXT,\n\t\truntime       TEXT NOT NULL             -- \"hades-system\" | \"claude-code-vps\"\n\t);\n\tCREATE INDEX IF NOT EXISTS idx_memory_writes_project ON memory_writes(project);\n\tCREATE INDEX IF NOT EXISTS idx_memory_writes_ts      ON memory_writes(ts);\n\n\t-- Doc versions during wizard (HADES design) -------------------------------------\n\n\tCREATE TABLE IF NOT EXISTS doc_versions (\n\t\tid        INTEGER PRIMARY KEY AUTOINCREMENT,\n\t\tts        INTEGER NOT NULL,\n\t\tproject   TEXT NOT NULL,\n\t\tfeature   TEXT NOT NULL,\n\t\tdoc_path  TEXT NOT NULL,\n\t\tcontent   TEXT NOT NULL,\n\t\tauthor    TEXT NOT NULL                 -- \"operator\" | \"orchestrator\"\n\t);\n\tCREATE INDEX IF NOT EXISTS idx_doc_versions_feature ON doc_versions(project, feature, doc_path);\n\n\t-- Postmortems (HADES design) ---------------------------------------------------\n\n\tCREATE TABLE IF NOT EXISTS postmortems (\n\t\tid              INTEGER PRIMARY KEY AUTOINCREMENT,\n\t\tts              INTEGER NOT NULL,\n\t\tproject         TEXT NOT NULL,\n\t\tswarm_id        TEXT NOT NULL,\n\t\troot_cause      TEXT,\n\t\tsuggestions_json TEXT,\n\t\toutcome         TEXT NOT NULL            -- \"completed-with-intervention\" | \"aborted\"\n\t);\n\tCREATE INDEX IF NOT EXISTS idx_postmortems_swarm ON postmortems(swarm_id);\n\n\t-- Task execution state (HADES design: provider rotation continuity) -------------\n\n\tCREATE TABLE IF NOT EXISTS task_state (\n\t\tid              INTEGER PRIMARY KEY AUTOINCREMENT,\n\t\tts              INTEGER NOT NULL,\n\t\ttask_id         TEXT NOT NULL,\n\t\tswarm_id        TEXT NOT NULL,\n\t\tattempt_n       INTEGER NOT NULL,\n\t\tprior_errors    TEXT,                   -- JSON array of error summaries\n\t\tfiles_edited    TEXT,                   -- JSON array of file paths\n\t\tcurrent_phase   TEXT NOT NULL,          -- \"codegen\"|\"tests\"|\"fix-loop\"|\"commit\"\n\t\tapproach_avoid  TEXT                    -- JSON array of approaches that failed\n\t);\n\tCREATE INDEX IF NOT EXISTS idx_task_state_task ON task_state(task_id, attempt_n DESC);\n\n\t-- Worktree registry (HADES design) ----------------------------------------------\n\n\tCREATE TABLE IF NOT EXISTS worktrees (\n\t\tid          INTEGER PRIMARY KEY AUTOINCREMENT,\n\t\tproject     TEXT NOT NULL,\n\t\tfeature     TEXT NOT NULL,\n\t\ttask_id     TEXT NOT NULL,\n\t\tpath        TEXT NOT NULL UNIQUE,\n\t\tbranch      TEXT NOT NULL,\n\t\tstatus      TEXT NOT NULL,              -- \"active\"|\"completed\"|\"removed\"\n\t\tcreated_at  INTEGER NOT NULL,\n\t\tremoved_at  INTEGER\n\t);\n\tCREATE INDEX IF NOT EXISTS idx_worktrees_status ON worktrees(status);\n\n\t-- Bypass module audit (HADES design; spec §22 invariant) ----------------------\n\n\tCREATE TABLE IF NOT EXISTS bypass_audit (\n\t\tid             INTEGER PRIMARY KEY AUTOINCREMENT,\n\t\tts             INTEGER NOT NULL,\n\t\trequest_hash   TEXT NOT NULL,           -- SHA-256 of request body\n\t\tresponse_hash  TEXT NOT NULL,           -- SHA-256 of response body\n\t\tsuccess        INTEGER NOT NULL,        -- 1=ok, 0=fail\n\t\tlatency_ms     INTEGER,\n\t\terror_code     TEXT,\n\t\terror_pattern  TEXT,                    -- detected Anthropic patch pattern\n\t\ttier_used      TEXT NOT NULL            -- \"in-house\"|\"community\"|\"payg\"\n\t);\n\tCREATE INDEX IF NOT EXISTS idx_bypass_audit_ts      ON bypass_audit(ts);\n\tCREATE INDEX IF NOT EXISTS idx_bypass_audit_success ON bypass_audit(success);\n\n\t-- Bypass config version history (HADES design) ----------------------------------\n\n\tCREATE TABLE IF NOT EXISTS bypass_config_versions (\n\t\tversion       TEXT PRIMARY KEY,         -- e.g. \"2026.04.29.1\"\n\t\tapplied_at    INTEGER NOT NULL,\n\t\tdiff_summary  TEXT,\n\t\tapplied_by    TEXT NOT NULL             -- \"operator\" or \"auto\"\n\t);\n\n\t-- PAYG spend tracking (HADES design: cost caps) ---------------------------------\n\n\tCREATE TABLE IF NOT EXISTS payg_spend (\n\t\tid          INTEGER PRIMARY KEY AUTOINCREMENT,\n\t\tts          INTEGER NOT NULL,\n\t\tsession_id  TEXT,\n\t\tproject     TEXT NOT NULL,\n\t\ttokens_in   INTEGER NOT NULL,\n\t\ttokens_out  INTEGER NOT NULL,\n\t\tcost_usd    REAL NOT NULL,\n\t\tcapped      INTEGER NOT NULL DEFAULT 0  -- 1 if hit a cap and was rejected\n\t);\n\tCREATE INDEX IF NOT EXISTS idx_payg_spend_project_ts ON payg_spend(project, ts);\n\n\t-- Notifications queue (HADES design) -------------------------------------------\n\n\tCREATE TABLE IF NOT EXISTS notifications_queue (\n\t\tid            INTEGER PRIMARY KEY AUTOINCREMENT,\n\t\tts            INTEGER NOT NULL,\n\t\tproject       TEXT,\n\t\tseverity      TEXT NOT NULL,            -- \"info\"|\"warning\"|\"actionable\"|\"critical\"\n\t\ttitle         TEXT NOT NULL,\n\t\tbody          TEXT,\n\t\tchannels      TEXT NOT NULL,            -- JSON array: [\"dashboard\",\"bell\",...]\n\t\tdedupe_hash   TEXT NOT NULL,\n\t\tdispatched_at INTEGER,                  -- NULL if queued\n\t\tdismissed_at  INTEGER\n\t);\n\tCREATE INDEX IF NOT EXISTS idx_notifications_dispatched ON notifications_queue(dispatched_at);\n\tCREATE INDEX IF NOT EXISTS idx_notifications_dedupe     ON notifications_queue(dedupe_hash);\n\n\t-- Projects registered with the daemon (HADES design) ----------------------------\n\n\tCREATE TABLE IF NOT EXISTS projects (\n\t\tid                 TEXT PRIMARY KEY,    -- canonical id e.g. \"internal-platform-x\"\n\t\tpath               TEXT NOT NULL,       -- absolute filesystem path\n\t\texecution          TEXT NOT NULL,       -- \"mac\" (Architecture II)\n\t\tauthoritative_git  TEXT,\n\t\tvps_endpoint       TEXT,                -- ssh host alias if applicable\n\t\tdoctrine           TEXT NOT NULL,       -- \"max-scope\"|\"default\"|\"capa-firewall\"\n\t\tbudget_monthly_usd REAL,\n\t\tpriority_weight    INTEGER NOT NULL DEFAULT 50,\n\t\tregistered_at      INTEGER NOT NULL,\n\t\tconfig_json        TEXT                 -- serialized full config for fast reads\n\t);\n\n\t-- OpenCode sessions registered by plugin (HADES design) -------------------------\n\n\tCREATE TABLE IF NOT EXISTS sessions (\n\t\tid            TEXT PRIMARY KEY,\n\t\tproject       TEXT NOT NULL,\n\t\truntime       TEXT NOT NULL,            -- \"opencode\"|\"claude-code-vps\"\n\t\tstarted_at    INTEGER NOT NULL,\n\t\tended_at      INTEGER,\n\t\tFOREIGN KEY (project) REFERENCES projects(id)\n\t);\n\tCREATE INDEX IF NOT EXISTS idx_sessions_project ON sessions(project);\n\n\t-- Swarm runs (HADES design) -----------------------------------------------------\n\n\tCREATE TABLE IF NOT EXISTS swarms (\n\t\tid            TEXT PRIMARY KEY,\n\t\tproject       TEXT NOT NULL,\n\t\tfeature       TEXT NOT NULL,\n\t\tstage         TEXT NOT NULL,            -- \"proposing\"|\"applying\"|\"archiving\"|\"completed\"|\"aborted\"\n\t\tstarted_at    INTEGER NOT NULL,\n\t\tended_at      INTEGER,\n\t\tparallelism   INTEGER NOT NULL,\n\t\tFOREIGN KEY (project) REFERENCES projects(id)\n\t);\n\tCREATE INDEX IF NOT EXISTS idx_swarms_project_phase ON swarms(project, stage);\n\n\t-- Tasks within swarms (HADES design) --------------------------------------------\n\n\tCREATE TABLE IF NOT EXISTS tasks (\n\t\tid            TEXT PRIMARY KEY,\n\t\tswarm_id      TEXT NOT NULL,\n\t\tspec_json     TEXT NOT NULL,            -- serialized task spec\n\t\tstage         TEXT NOT NULL,            -- per task_state.current_phase\n\t\tprovider      TEXT,                     -- assigned agent profile model\n\t\tstarted_at    INTEGER NOT NULL,\n\t\tended_at      INTEGER,\n\t\toutcome       TEXT,                     -- \"green\"|\"failed\"|\"killed\"|\"accepted-as-is\"\n\t\tFOREIGN KEY (swarm_id) REFERENCES swarms(id)\n\t);\n\tCREATE INDEX IF NOT EXISTS idx_tasks_swarm ON tasks(swarm_id);\n\t",

	migrationV2,

	migrationV3,

	migrationV4,

	migrationV5,

	migrationV6,

	migrationV7,

	migrationV8,

	migrationV9,

	migrationV10,

	migrationV11,

	migrationV12,

	migrationV13,

	migrationV14,

	migrationV15,

	migrationV16,

	migrationV17,

	migrationV18,

	migrationV19,

	migrationV20,

	migrationV21,

	migrationV22,

	migrationV23,

	migrationV24,

	migrationV25,

	migrationV26,

	migrationV27,

	migrationV28,

	migrationV29,

	migrationV30,

	migrationV31,
}

func (s *Store) Migrate() error {
	if _, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_version (
			version    INTEGER PRIMARY KEY,
			applied_at INTEGER NOT NULL
		)
	`); err != nil {
		return fmt.Errorf("create schema_version: %w", err)
	}

	current, err := s.currentVersion()
	if err != nil {
		return fmt.Errorf("get current version: %w", err)
	}

	for i, ddl := range migrations {
		v := i + 1
		if v <= current {
			continue
		}
		if err := s.applyMigration(v, ddl); err != nil {
			return fmt.Errorf("migration v%d: %w", v, err)
		}
	}
	return nil
}

func (s *Store) currentVersion() (int, error) {
	var v int
	err := s.db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&v)
	return v, err
}

func (s *Store) applyMigration(version int, ddl string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(ddl); err != nil {
		return fmt.Errorf("ddl: %w", err)
	}
	if _, err := tx.Exec(
		`INSERT INTO schema_version (version, applied_at) VALUES (?, strftime('%s', 'now'))`,
		version,
	); err != nil {
		return fmt.Errorf("record version: %w", err)
	}
	return tx.Commit()
}
