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
//   - v4: conversation_id column on bypass_audit (Q7 D)
//
//   - v5: bypass_audit_bodies (encrypted bodies, inv-zen-055)
//
//   - v6: bypass_audit_pins (retention-exempt registry)
//
//   - v7: conversation_wal (per-conversation write-ahead log)
//
//   - v8: idempotency_keys (TTL 24h replay cache)
//
//   - v9: notifications (bypass-event ledger + 1h CRITICAL repeat)
//
//   - v10: cost_ledger (one row per LLM request, USD-converted, idempotency
//     UNIQUE for inv-zen-062 no-double-charge guarantee)
//
//   - v11: pin_overrides (Q8 D pin hierarchy with optional TTL, inv-zen-063)
//
//   - v12: doctrine_state singleton (last-loaded Resolved.Schema +
//     Provenance JSON snapshot; daemon reads on startup so
//
//   - v13: workforce_tasks (SharedTaskList), workforce_checkpoints
//     (CheckpointQueue), workforce_fix_prompts (FixPromptQueue).
//     WAL + busy_timeout enforced by workforceadapter (inv-zen-073).
//     project_id on every row for logical isolation (spec §7.1).
//
//   - v14: subprocess_sessions — persistent TeamLead + Reviewer L3/L4
//     subprocess registry for crash recovery (Q3 C lifecycle).
//     Idempotency key (spec_id, doctrine_name); ttl_seconds drives the
//     inv-zen-074 TTL evictor.
//
//   - v15: worker_specs / team_lead_specs / reviewer_specs — three tables
//     holding the immutable WorkerSpec snapshots persisted by the daemon
//     adapter (Phase G wires the read/write CRUD). Phase D ships the
//     schema so Plan 5 orchestrator persistence is unblocked.
//
//   - v16: aggregation_windows + aggregation_events — AggregationStream
//     SQLite-durability layer. Window open/close/event records survive
//     daemon restart; LoadOpenWindows surfaces in-progress windows for
//     recovery. Partial index on status='open' accelerates restart scan.
//
//   - v17: operator_gate_state — singleton row (id=1 UPSERT) persisting
//     OperatorGate pause/resume state across daemon restarts. CHECK
//     constraint enforces the four-value State enum.
//
//   - v18: cost_axis_tags + axis_tag_loss_events — 4-axis attribution
//     store (project × doctrine × stage × task; +operation +worker_id
//     optional). UNIQUE (cost_id, axis_name) + INSERT OR IGNORE for
//     PostCall idempotency. cost_ledger now exists on main (Plan 3
//     F-1 merged), so a future migration may add the FK; current
//     migration ships without FK to keep the diff minimal.
//
//   - v19: budget_pauses + budget_anomalies + budget_anomaly_samples
//     — 4-scope hierarchical pause state machine + z-score event log
//
//   - per-scope rolling sample window. inv-zen-031: internal/budget
//     never imports internal/store; bridge via dispatcheradapter.
//
//   - v20: budget_anomaly_samples gains cost_id column + UNIQUE
//     (scope, scope_value, cost_id). PostCallWithCost retries become
//     idempotent at the SQL layer via INSERT OR IGNORE. Legacy rows
//     (none on production yet) get cost_id = -id so UNIQUE is satisfied.
//     New callers MUST pass a positive cost_id for the idempotency
//     guarantee; cost_id=0 is the sentinel for opt-out callers
//     (CLI bulk-import, retro-fill).
//
//   - v21: research_cache — sha256-keyed response cache for the research MCP
//     (Phase I). TTL enforced by handler; background eviction goroutine 1h.
//
//   - v22: audit_events_raw — daemon-owned audit event ledger. All MCPs and
//     handlers emit here via POST /v1/audit/emit. Plan 9 wraps with hash-chain
//     OTel export WITHOUT schema migration (additive ALTER TABLE only).
//
//   - v23: substrate_health — per-commit test pass-rate + doctrine-lint
//     outcome. The regression-by-self detector queries this table to spot
//     "substrate is regressing on its own commits" (Apr 23 chicken-and-egg
//     failure mode). Plan 9 extends additively (history queries, time-series,
//     adversarial corpus). inv-zen-031 boundary: writes go through the
//     SubstrateHealthWriter interface declared in safetynet/regression.go;
//     adapter wired in Phase N (internal/daemon/orchestratoradapter/).
//
//   - v24: projects_alias + path_history — sha256 canonical id (content-
//     addressable) separated from human alias (operator-facing). Path
//     history table tracks every (id_sha256, path) tuple ever observed,
//     enabling mv-detection in projectctx.DetectMv. ON DELETE CASCADE
//     links path_history → projects_alias. inv-zen-114.
//
//   - v25: priority_overrides — UNIQUE(project_alias), multiplier > 0,
//     reason NOT NULL. Set/Reset emit audit events in the same SQL
//     transaction as the row mutation (inv-zen-115). Phase C will add
//     tmux_session_state in a subsequent migration; the reservation in
//     master plan §"Migration numbering coordination" was for the joint
//     pair, but Phase B-6 ships only the priority_overrides half because
//     Phase C owns its own DDL.
//
//   - v26: tmux_session_state — one row per spawned zen-swarm tmux
//     session, keyed by canonical "zen-<alias>-<sha8>" name. Status
//     four-value enum (Active=0/Idle=1/Orphaned=2/Archived=3) bounded
//     by SQL CHECK + Go validateTmuxStatus (defense in depth).
//     expected_panes is a JSON-encoded map[WindowName][]string of
//     daemon-recorded pane ids per daemon-owned window — EXCLUDES
//     WindowScratch (inv-zen-118 enforced by the tmuxlife encoder).
//     Drift note: master plan reserved slot 060 for a joint
//     priority_overrides + tmux_session_state migration; Phase B-6
//     shipped 060_priority_overrides.sql alone, so Phase C-11 picks
//     the next free number (slot 061 reserved for Phase G knowledge-
//     index on a separate DB file). inv-zen-031: internal/tmuxlife
//     never imports internal/store; Phase I wires the adapter that
//     bridges tmuxlife.SessionStore to *store.Store via these CRUD
//     primitives.
//
//   - v27: schedules + schedule_history — durable scheduler substrate
//     for Routine + Task + Loop schedules (3-tier per spec §1 Q8 D)
//     plus per-fire outcome ledger driving `zen schedule history`.
//     Five CHECK-constrained enums (tier, trigger_type, miss_policy,
//     status, outcome) plus three indexes (idx_schedules_due partial
//     for the tick scan, idx_schedules_project_alias for `zen
//     schedule list --project=...`, idx_schedule_history_lookup for
//     time-window queries). inv-zen-031: internal/scheduler never
//     imports internal/store; the adapter at
//     internal/daemon/scheduleradapter is the only legitimate bridge.
//     Drift note: master plan §"Migration numbering coordination"
//     reserved slot 059 for Phase D under an A→C→B→D→E execution
//     sequence; reality at HEAD on 2026-05-07 has 057 / 060 / 062
//     already taken (Phases A / B-6 / C-11). Phase D-1 picks 063 —
//     the next free number on the daemon.db chain. Slot 059 is
//     reserved for Phase E inbox storage; slot 061 for Phase G
//     knowledge-index DB (separate SQLite file, no daemon.db
//     schemaVersion bump).
//
//   - v28: per-project `inbox` table + daemon.db `inbox_aggregator_cache`.
//     Q11 C hybrid storage substrate. Per-project authoritative inbox
//     carries severity 4-tier CHECK (inv-zen-124), 5min sliding-window
//     dedup UNIQUE on (event_type, content_hash, created_at_bucket),
//     and partial unacked index for the hot render path. Daemon-level
//     aggregator cache is a denormalized read view written by the
//     outbox bridge (Phase E-8) on every per-project INSERT;
//     project_id + project_alias indexed for the `zen day`
//     cross-project digest hot path (Phase F leverage-sort);
//     UNIQUE (project_id, notification_id) prevents duplicate fanout
//     under at-least-once outbox replay. Cold rebuildable from
//     per-project sources on daemon boot (Aggregator.Rebuild). Cascade
//     delete on project rm via the per-project DB-file unlink (atomic).
//     inv-zen-031: internal/inbox MUST NEVER import internal/store;
//     internal/daemon/inboxadapter (Phase E-10) is the only legitimate
//     bridge. inv-zen-113: aggregator cache rows carry source-DB
//     project_id (no cross-project leak).
//     Drift note: spec-frozen plan-7-phase-E projected migrationV27
//     under the original A→C→B→D→E execution sequence (Phase D
//     would land 059 instead of 063). Reality at HEAD on 2026-05-07
//     has Phase D-1 already at v27, so Phase E-1 picks v28 — the next
//     free number. The migration FILE number stays at 058 (slot
//     reserved per master plan §"Migration numbering coordination").
//
//   - v29: audit_events_raw chain integration — adds four chain columns
//     (prev_hash, record_hash, partition_id, tessera_leaf_id) + REFUSE
//     triggers (inv-zen-143 append-only enforcement) + monthly partition
//     view (audit_events_partitions) + audit_partition_seals CRUD table.
//     Chain hashes are computed in app-layer (auditadapter post-INSERT
//     same-row UPDATE) — no SQL trigger recursion. Boundary inv-zen-031:
//     internal/audit/chain MUST NEVER import internal/store; the bridge
//     is internal/daemon/auditadapter (Phase B B-9). schemaVersion bump
//     path: 28 (Plan 7 Phase E-1) → 29 (this migration).
const schemaVersion = 31

// migrationV21 adds research_cache — Plan 4 Phase G Task G-1, Q9 B.
//
//go:embed schema/054_research_cache.sql
var migrationV21 string

// migrationV22 adds audit_events_raw — Plan 4 Phase G Task G-2, Q9 B.
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

// migrationV4 adds conversation_id to bypass_audit so Phase G can group
// audit rows by upstream conversation (inv-zen-054 / Q7 D pin registry).
//
//go:embed schema/034_bypass_audit.sql
var migrationV4 string

// migrationV5 adds bypass_audit_bodies — the AES-256-GCM-encrypted body
// table populated only when tier=in-house (inv-zen-054, inv-zen-055).
//
//go:embed schema/035_bypass_audit_bodies.sql
var migrationV5 string

// migrationV6 adds bypass_audit_pins — the operator pin registry that
// exempts marked conversations from the nightly retention purge (Q7 D).
//
//go:embed schema/036_bypass_audit_pins.sql
var migrationV6 string

// migrationV7 adds conversation_wal — Layer 1 of the bypass resilience
// model (Plan 2 Phase H). One row per orchestrator turn; pending state
// persisted BEFORE upstream call so a restart can replay the in-flight
// turn (half of inv-zen-056).
//
//go:embed schema/037_conversation_wal.sql
var migrationV7 string

// migrationV8 adds idempotency_keys — TTL 24h replay cache (Plan 2
// Phase H). MarkPending persists BEFORE upstream call; MarkCompleted
// stores the full response so a restart in the upstream-response →
// orchestrator-delivery gap replays without a second upstream charge
// (other half of inv-zen-056).
//
//go:embed schema/038_idempotency.sql
var migrationV8 string

// migrationV9 adds notifications — bypass-event ledger (Plan 2 Phase L,
// Task L-4, spec §8.4). Severity ∈ {INFO,WARN,CRITICAL}; CRITICAL rows
// re-fire macOS osascript every 1h until acknowledged. Distinct from
// notifications_queue (Plan 11 multi-channel routing).
//
//go:embed schema/039_notifications.sql
var migrationV9 string

// migrationV10 adds cost_ledger — Layer 2 of orchestrator observability
// (Plan 3 Phase C, Task F-1, inv-zen-062). One row per LLM request, with
// cost_usd already converted to USD by the provider's RateCard;
// idempotency_key UNIQUE blocks double-charge under retry / concurrent
// dispatch (the Go layer translates the SQL UNIQUE failure into
// ErrDuplicateIdempotency via cost_ledger.go).
//
//go:embed schema/040_cost_ledger.sql
var migrationV10 string

// migrationV11 adds pin_overrides — operator-set tier pins at three scope
// levels (session, project, global) with optional TTL (Plan 3 Phase E,
// Task I-1, inv-zen-063). UNIQUE(scope, scope_id) with scope_id=” for
// global (SQLite NULL-distinctness workaround). expires_at is INTEGER unix
// seconds; NULL means permanent. The 5-min sweep runs PurgeExpiredPins.
//
//go:embed schema/043_pin_overrides.sql
var migrationV11 string

// migrationV12 adds doctrine_state — singleton row holding the last-
// loaded Resolved.Schema + Provenance JSON snapshot (Plan 4 Phase A,
// task A-6). Daemon reads on startup so Plan 5 workers dispatched
// before a restart see consistent doctrine values across the restart
// boundary; Phase G wires the /v1/doctrine/state endpoint.
//
//go:embed schema/044_doctrine_state.sql
var migrationV12 string

// migrationV13 adds the three workforce durable queue tables (Plan 4
// Phase B): workforce_tasks (SharedTaskList), workforce_checkpoints
// (CheckpointQueue), workforce_fix_prompts (FixPromptQueue). All three
// tables carry project_id for logical isolation (spec §7.1). WAL mode
// + busy_timeout=5000 are enforced by workforceadapter constructors
// (inv-zen-073); independent failure domains, no FK between tables
// (spec §2.2).
//
//go:embed schema/045_workforce_queues.sql
var migrationV13 string

// migrationV14 adds subprocess_sessions — Plan 4 Phase C Task C-5,
// persistent TeamLead + Reviewer L3/L4 crash recovery (Q3 C lifecycle).
// Ephemeral Worker rows never appear here; only persistent variants.
// Idempotency key (spec_id, doctrine_name). TTL semantics diverge per
// doctrine so the same SpecID under two doctrines yields two rows.
// ttl_seconds + last_use_at drive the inv-zen-074 evictor. inv-zen-031
// preserved by the SessionStore interface in subprocess package (no
// internal/store import there); Phase G wires the adapter.
//
//go:embed schema/048_subprocess_lifecycle.sql
var migrationV14 string

// migrationV15 adds worker_specs / team_lead_specs / reviewer_specs —
// snapshots persisted by the daemon adapter (Phase G CRUD). project_id
// on every row (spec §7.1). reviewer_specs has a composite PK
// (id, project_id, reviewer_tier) so the same spec ID can hold L2/L3/L4
// rows independently. inv-zen-031 preserved: internal/workforce/worker
// MUST NOT import internal/store; the daemon workforceadapter (Phase G)
// owns the read/write surface.
//
//go:embed schema/049_worker_specs.sql
var migrationV15 string

// migrationV16 adds aggregation_windows + aggregation_events — Plan 4
// Phase E AggregationStream SQLite-durability layer (inv-zen-031 boundary:
// workforce/stream never imports internal/store; bridge via StreamAdapter
// in workforceadapter). Window open/close/event records survive daemon
// restart; LoadOpenWindows surfaces in-progress windows for recovery.
// Partial index on status='open' accelerates restart scan.
//
//go:embed schema/046_aggregation_streams.sql
var migrationV16 string

// migrationV17 adds operator_gate_state — singleton row (id=1 UPSERT)
// persisting OperatorGate pause/resume state across daemon restarts (Plan 4
// Phase E). CHECK constraint enforces the four-value State enum; LoadState
// returns StateRunning when row absent (clean boot). inv-zen-031: gate/*
// never imports internal/store; bridge via GateAdapter in workforceadapter.
//
//go:embed schema/047_operator_gate.sql
var migrationV17 string

// migrationV18 adds cost_axis_tags + axis_tag_loss_events — Plan 4 Phase F
// Task F-1, Q6 C, inv-zen-077. The 4-axis attribution store
// (project × doctrine × stage × task; +operation +worker_id optional).
// UNIQUE (cost_id, axis_name) + INSERT OR IGNORE in the Go layer keeps
// PostCall idempotent under retries. axis_tag_loss_events records every
// missing-axis incident so completeness drift surfaces immediately rather
// than silently degrading inv-zen-077. inv-zen-031: internal/budget never
// imports internal/store; bridge via dispatcheradapter/budget_hooks.go.
// The cost_ledger (v10) is already present on main; this migration
// ships without an FK back to keep the diff minimal — a future
// migration may add it once budget tag callers are validated.
//
//go:embed schema/051_budget_axes.sql
var migrationV18 string

// migrationV19 adds budget_pauses + budget_anomalies + budget_anomaly_samples
// — Plan 4 Phase F Task F-5, Q6 C, inv-zen-078 + inv-zen-079. 4-scope
// hierarchical pause state machine (project / doctrine / stage / worker_id),
// z-score event log, and per-scope rolling sample window. budget_pauses
// PRIMARY KEY (scope, scope_value) + UPSERT keeps the latest reason on
// re-trigger. budget_anomaly_samples is housekeeping-pruned by Plan 4
// Phase G (>24h cutoff). inv-zen-031: internal/budget never imports
// internal/store; bridge via dispatcheradapter.
//
//go:embed schema/052_budget_pause.sql
var migrationV19 string

// migrationV20 adds cost_id column + UNIQUE constraint on
// budget_anomaly_samples — Plan 4 Phase F post-review C-2 fix. The
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

// migrationV23 adds substrate_health — Plan 5 Phase M Task M-1, Q2 C
// regression-by-self metric. Per-commit test pass-rate + doctrine-lint
// outcome storage. Plan 9 extends additively. inv-zen-031: safetynet
// writes go through SubstrateHealthWriter interface; adapter in Phase N.
//
//go:embed schema/056_substrate_health.sql
var migrationV23 string

// migrationV24 introduces projects_alias + path_history — Plan 7 Phase A
// implements AIP-2510 dual-ID per spec §1 Q3. sha256 canonical + human
// alias separation; path_history tracks every (id_sha256, path) tuple
// ever observed, enabling mv-detection in projectctx.DetectMv. ON
// DELETE CASCADE links path_history → projects_alias. inv-zen-114.
//
//go:embed migrations/057_projects_alias_path_history.sql
var migrationV24 string

// migrationV25 introduces priority_overrides — Plan 7 Phase B-6 ships
// the Layer 3 operator override seam per spec §1 Q10. UNIQUE(project_alias)
// + multiplier > 0 + reason NOT NULL constraints. internal/quota declares
// the OverrideStore interface; internal/daemon/quotaadapter is the only
// package permitted to bridge to *store.Store (inv-zen-031 + inv-zen-122).
// Set / Reset emit audit events in the SAME transaction as the row
// mutation — inv-zen-115 audit-chain integrity.
//
//go:embed migrations/060_priority_overrides.sql
var migrationV25 string

// migrationV26 introduces tmux_session_state — Plan 7 Phase C-11 ships
// the per-session lifecycle storage row keyed by canonical
// "zen-<alias>-<sha8>" name. Status four-value enum bounded by SQL CHECK
// + Go validateTmuxStatus (defense in depth, inv-zen-031 boundary
// preserved). expected_panes JSON excludes WindowScratch (inv-zen-118
// enforced by the tmuxlife encoder). The Phase I adapter
// (internal/daemon/handlers/sessions.go) is the only legitimate bridge
// to *store.Store via this package's CRUD primitives.
//
// Drift note: slot 060 was reserved for a joint priority_overrides +
// tmux_session_state migration in the master plan; Phase B-6 shipped
// 060_priority_overrides.sql alone, so Phase C-11 picks 062 (slot 061
// reserved for Phase G knowledge-index on a separate DB file).
//
//go:embed migrations/062_tmux_session_state.sql
var migrationV26 string

// migrationV27 introduces schedules + schedule_history — Plan 7 Phase
// D-1 ships the durable scheduler substrate. The schedules table hosts
// Routine + Task + Loop schedules (3-tier per spec §1 Q8 D); the
// schedule_history table is the append-only fire-attempt outcome
// ledger driving `zen schedule history`. Five CHECK-constrained enums
// (tier 0..2, trigger_type 0..2, miss_policy 0..3, status 0..2,
// outcome 0..3) lock the contract at the SQL layer; the Go-side
// validators in internal/store/schedules.go reject out-of-range values
// before the SQL CHECK fires (defense in depth).
//
// inv-zen-031: internal/scheduler/* MUST NEVER import internal/store;
// internal/daemon/scheduleradapter/ is the only package permitted to
// bridge scheduler value types to *store.Store via this package's
// CRUD primitives.
//
// Drift note: master plan §"Migration numbering coordination" reserved
// slot 059 for Phase D under an A→C→B→D→E execution sequence; reality
// at HEAD on 2026-05-07 has 057 / 060 / 062 already taken (Phases A /
// B-6 / C-11). Phase D-1 picks 063 — the next free number on the
// daemon.db chain. Slot 059 is reserved for Phase E inbox storage;
// slot 061 for Phase G knowledge-index DB (separate SQLite file).
//
//go:embed migrations/063_schedules.sql
var migrationV27 string

// migrationV28 introduces per-project `inbox` + daemon.db
// `inbox_aggregator_cache` — Plan 7 Phase E Task E-1 ships the Q11 C
// hybrid storage substrate.
//
// Per-project authoritative inbox: severity 4-tier CHECK (inv-zen-124),
// 5min sliding-window dedup UNIQUE on (event_type, content_hash,
// created_at_bucket), partial idx_inbox_unacked for the hot render
// path. Cascade-delete on project rm via per-project DB-file unlink
// (atomic; no SQL-layer FK).
//
// Daemon-level aggregator cache: denormalized read view written by
// the outbox bridge (Phase E-8) on every per-project INSERT;
// project_id + project_alias indexed for the `zen day` cross-project
// digest hot path (Phase F leverage-sort); UNIQUE (project_id,
// notification_id) prevents duplicate fanout under at-least-once
// outbox replay. Cold rebuildable from per-project sources on daemon
// boot (Aggregator.Rebuild, ~1s for 10 projects per spec target).
//
// inv-zen-031: internal/inbox/* MUST NEVER import internal/store;
// internal/daemon/inboxadapter/ (Phase E-10) is the only package
// permitted to bridge inbox value types to *store.Store via this
// migration's tables.
//
// inv-zen-113: aggregator cache rows carry source-DB project_id
// (no cross-project leak). Compile-time anchor in inbox/sentinel.go;
// runtime via outbox bridge writing project_id from source scope;
// property-based fuzz test in
// tests/compliance/inv_zen_113_no_cross_project_inbox_leak_test.go.
//
// Drift note: spec-frozen plan-7-phase-E projected migrationV27 under
// the original A→C→B→D→E execution sequence (Phase D would land 059
// instead of 063). Reality at HEAD on 2026-05-07 has Phase D-1 already
// at v27, so Phase E-1 picks v28 — the next free number. The migration
// FILE number stays at 058 (slot reserved per master plan
// §"Migration numbering coordination"). schemaVersion bump path:
// 27 (Phase D-1) → 28 (this migration).
//
//go:embed migrations/058_inbox_aggregator_cache.sql
var migrationV28 string

// migrationV29 introduces audit_events_raw chain integration — Plan 9
// Phase B-1 ships the Q3 C decision: per-event Tessera leaf + per-partition
// seal hybrid granularity. Four chain columns added via additive ALTER
// TABLE (prev_hash, record_hash, partition_id all TEXT NOT NULL DEFAULT ”;
// tessera_leaf_id TEXT NULL) plus REFUSE triggers enforcing inv-zen-143
// append-only at the SQL layer (BEFORE UPDATE on truly-immutable columns,
// BEFORE UPDATE WHEN already-set on chain hashes / partition_id /
// tessera_leaf_id one-time-write columns, BEFORE DELETE unconditional).
// audit_events_partitions VIEW aggregates first_id/last_id/event_count/
// final_record_hash per partition_id (correlated subquery for chain tip
// extraction; SQLite 3.40+-compat). audit_partition_seals TABLE holds
// monthly seal records keyed by partition_id with sealed_at +
// final_record_hash + tessera_seal_leaf_id + daemon_witness_signature
// (NOT NULL CHECK length>0, inv-zen-145) + optional cold_archive_url +
// cold_archive_content_hash for Plan 9 Phase C cold-archive write-back.
//
// Chain compute is app-layer (auditadapter post-INSERT same-row UPDATE);
// no SQL trigger recursion. The four refusing triggers also permit the
// chain compute path because they fire WHEN OLD.<col> != ” / IS NOT NULL
// — the one-time write that flips ” to non-empty (or NULL to non-NULL
// for tessera_leaf_id) is allowed.
//
// Boundary (inv-zen-031): internal/audit/chain (Phase B B-3) MUST NEVER
// import internal/store; the bridge is internal/daemon/auditadapter
// (Phase B B-9), which translates between chain.* value types and store
// row types via field-by-field copy. This package's audit_chain.go
// holds the store-side CRUD primitives (GetChainTip, GetEventByID,
// UpdateChainColumns, UpdateTesseraLeafID, InsertPartitionSeal,
// GetPartitionSeal, ListPartitions, ListEventsForPartition,
// BackfillScan).
//
//go:embed schema/059_audit_chain_extension.sql
var migrationV29 string

// migrationV30 adds cost_ledger.provider — Plan 16 Phase B Task 9, C9,
// inv-zen-214. Per-provider cost attribution: the dispatcher cascade
// iterates NAMED backends, so cost must be persisted at Backend.Name()
// granularity (not just providers.Tier). DEFAULT ” so pre-Plan-16 rows
// decode cleanly. The cost_ledger window index is rebuilt to include
// provider. schemaVersion bump path: 29 → 30.
//
//go:embed migrations/064_cost_ledger_provider.sql
var migrationV30 string

// migrationV31 adds tier_health_samples — Plan 16 Phase B Task 11, C9,
// inv-zen-214. Per-provider health observability: one row per backend
// outcome (dispatcher attempt + RecoveryScheduler probe). provider is
// Backend.Name() — the per-provider counterpart to the per-Name circuit
// breaker. Append-only, no UNIQUE (health samples are not idempotency-
// keyed). schemaVersion bump path: 30 → 31.
//
//go:embed migrations/065_tier_health_samples.sql
var migrationV31 string

var migrations = []string{

	`
	-- Schema version tracking ------------------------------------------------

	CREATE TABLE IF NOT EXISTS schema_version (
		version    INTEGER PRIMARY KEY,
		applied_at INTEGER NOT NULL
	);

	-- Core daemon event log (spec §5.3) ---------------------------------------

	CREATE TABLE IF NOT EXISTS events (
		id           INTEGER PRIMARY KEY AUTOINCREMENT,
		ts           INTEGER NOT NULL,           -- UTC unix seconds (inv-zen-005)
		project      TEXT,                       -- empty for daemon-level events
		session_id   TEXT,
		swarm_id     TEXT,
		task_id      TEXT,
		type         TEXT NOT NULL,              -- e.g. session.created, task.codegen.completed
		payload_json TEXT
	);
	CREATE INDEX IF NOT EXISTS idx_events_ts          ON events(ts);
	CREATE INDEX IF NOT EXISTS idx_events_project     ON events(project);
	CREATE INDEX IF NOT EXISTS idx_events_swarm       ON events(swarm_id);
	CREATE INDEX IF NOT EXISTS idx_events_session     ON events(session_id);
	CREATE INDEX IF NOT EXISTS idx_events_type        ON events(type);

	-- LLM call accounting (Plan 4 fills) --------------------------------------

	CREATE TABLE IF NOT EXISTS llm_calls (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		ts          INTEGER NOT NULL,
		project     TEXT,
		swarm_id    TEXT,
		task_id     TEXT,
		provider    TEXT NOT NULL,
		model       TEXT NOT NULL,
		tokens_in   INTEGER NOT NULL,
		tokens_out  INTEGER NOT NULL,
		latency_ms  INTEGER,
		cost_usd    REAL                        -- nullable: $0 marginal under bypass
	);
	CREATE INDEX IF NOT EXISTS idx_llm_calls_ts       ON llm_calls(ts);
	CREATE INDEX IF NOT EXISTS idx_llm_calls_project  ON llm_calls(project);
	CREATE INDEX IF NOT EXISTS idx_llm_calls_provider ON llm_calls(provider);

	-- Operator + orchestrator decisions (Plan 9) ------------------------------

	CREATE TABLE IF NOT EXISTS decisions (
		id            INTEGER PRIMARY KEY AUTOINCREMENT,
		ts            INTEGER NOT NULL,
		project       TEXT,
		scope         TEXT NOT NULL,            -- e.g. archive, conflict, override
		decision      TEXT NOT NULL,
		justification TEXT,
		actor         TEXT NOT NULL             -- "operator" | "orchestrator" | "auto"
	);
	CREATE INDEX IF NOT EXISTS idx_decisions_ts       ON decisions(ts);
	CREATE INDEX IF NOT EXISTS idx_decisions_project  ON decisions(project);

	-- Auto-memory writes audit (Plan 9) ---------------------------------------

	CREATE TABLE IF NOT EXISTS memory_writes (
		id            INTEGER PRIMARY KEY AUTOINCREMENT,
		ts            INTEGER NOT NULL,
		project       TEXT NOT NULL,
		file_path     TEXT NOT NULL,
		action        TEXT NOT NULL,            -- "create" | "update" | "delete"
		content_hash  TEXT,
		runtime       TEXT NOT NULL             -- "zen-swarm" | "claude-code-vps"
	);
	CREATE INDEX IF NOT EXISTS idx_memory_writes_project ON memory_writes(project);
	CREATE INDEX IF NOT EXISTS idx_memory_writes_ts      ON memory_writes(ts);

	-- Doc versions during wizard (Plan 9) -------------------------------------

	CREATE TABLE IF NOT EXISTS doc_versions (
		id        INTEGER PRIMARY KEY AUTOINCREMENT,
		ts        INTEGER NOT NULL,
		project   TEXT NOT NULL,
		feature   TEXT NOT NULL,
		doc_path  TEXT NOT NULL,
		content   TEXT NOT NULL,
		author    TEXT NOT NULL                 -- "operator" | "orchestrator"
	);
	CREATE INDEX IF NOT EXISTS idx_doc_versions_feature ON doc_versions(project, feature, doc_path);

	-- Postmortems (Plan 11) ---------------------------------------------------

	CREATE TABLE IF NOT EXISTS postmortems (
		id              INTEGER PRIMARY KEY AUTOINCREMENT,
		ts              INTEGER NOT NULL,
		project         TEXT NOT NULL,
		swarm_id        TEXT NOT NULL,
		root_cause      TEXT,
		suggestions_json TEXT,
		outcome         TEXT NOT NULL            -- "completed-with-intervention" | "aborted"
	);
	CREATE INDEX IF NOT EXISTS idx_postmortems_swarm ON postmortems(swarm_id);

	-- Task execution state (Plan 5: provider rotation continuity) -------------

	CREATE TABLE IF NOT EXISTS task_state (
		id              INTEGER PRIMARY KEY AUTOINCREMENT,
		ts              INTEGER NOT NULL,
		task_id         TEXT NOT NULL,
		swarm_id        TEXT NOT NULL,
		attempt_n       INTEGER NOT NULL,
		prior_errors    TEXT,                   -- JSON array of error summaries
		files_edited    TEXT,                   -- JSON array of file paths
		current_phase   TEXT NOT NULL,          -- "codegen"|"tests"|"fix-loop"|"commit"
		approach_avoid  TEXT                    -- JSON array of approaches that failed
	);
	CREATE INDEX IF NOT EXISTS idx_task_state_task ON task_state(task_id, attempt_n DESC);

	-- Worktree registry (Plan 5) ----------------------------------------------

	CREATE TABLE IF NOT EXISTS worktrees (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		project     TEXT NOT NULL,
		feature     TEXT NOT NULL,
		task_id     TEXT NOT NULL,
		path        TEXT NOT NULL UNIQUE,
		branch      TEXT NOT NULL,
		status      TEXT NOT NULL,              -- "active"|"completed"|"removed"
		created_at  INTEGER NOT NULL,
		removed_at  INTEGER
	);
	CREATE INDEX IF NOT EXISTS idx_worktrees_status ON worktrees(status);

	-- Bypass module audit (Plan 2; spec §22 inv-zen-034) ----------------------

	CREATE TABLE IF NOT EXISTS bypass_audit (
		id             INTEGER PRIMARY KEY AUTOINCREMENT,
		ts             INTEGER NOT NULL,
		request_hash   TEXT NOT NULL,           -- SHA-256 of request body
		response_hash  TEXT NOT NULL,           -- SHA-256 of response body
		success        INTEGER NOT NULL,        -- 1=ok, 0=fail
		latency_ms     INTEGER,
		error_code     TEXT,
		error_pattern  TEXT,                    -- detected Anthropic patch pattern
		tier_used      TEXT NOT NULL            -- "in-house"|"community"|"payg"
	);
	CREATE INDEX IF NOT EXISTS idx_bypass_audit_ts      ON bypass_audit(ts);
	CREATE INDEX IF NOT EXISTS idx_bypass_audit_success ON bypass_audit(success);

	-- Bypass config version history (Plan 2) ----------------------------------

	CREATE TABLE IF NOT EXISTS bypass_config_versions (
		version       TEXT PRIMARY KEY,         -- e.g. "2026.04.29.1"
		applied_at    INTEGER NOT NULL,
		diff_summary  TEXT,
		applied_by    TEXT NOT NULL             -- "operator" or "auto"
	);

	-- PAYG spend tracking (Plan 5: cost caps) ---------------------------------

	CREATE TABLE IF NOT EXISTS payg_spend (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		ts          INTEGER NOT NULL,
		session_id  TEXT,
		project     TEXT NOT NULL,
		tokens_in   INTEGER NOT NULL,
		tokens_out  INTEGER NOT NULL,
		cost_usd    REAL NOT NULL,
		capped      INTEGER NOT NULL DEFAULT 0  -- 1 if hit a cap and was rejected
	);
	CREATE INDEX IF NOT EXISTS idx_payg_spend_project_ts ON payg_spend(project, ts);

	-- Notifications queue (Plan 11) -------------------------------------------

	CREATE TABLE IF NOT EXISTS notifications_queue (
		id            INTEGER PRIMARY KEY AUTOINCREMENT,
		ts            INTEGER NOT NULL,
		project       TEXT,
		severity      TEXT NOT NULL,            -- "info"|"warning"|"actionable"|"critical"
		title         TEXT NOT NULL,
		body          TEXT,
		channels      TEXT NOT NULL,            -- JSON array: ["dashboard","bell",...]
		dedupe_hash   TEXT NOT NULL,
		dispatched_at INTEGER,                  -- NULL if queued
		dismissed_at  INTEGER
	);
	CREATE INDEX IF NOT EXISTS idx_notifications_dispatched ON notifications_queue(dispatched_at);
	CREATE INDEX IF NOT EXISTS idx_notifications_dedupe     ON notifications_queue(dedupe_hash);

	-- Projects registered with the daemon (Plan 7) ----------------------------

	CREATE TABLE IF NOT EXISTS projects (
		id                 TEXT PRIMARY KEY,    -- canonical id e.g. "internal-platform-x"
		path               TEXT NOT NULL,       -- absolute filesystem path
		execution          TEXT NOT NULL,       -- "mac" (Architecture II)
		authoritative_git  TEXT,
		vps_endpoint       TEXT,                -- ssh host alias if applicable
		doctrine           TEXT NOT NULL,       -- "max-scope"|"default"|"capa-firewall"
		budget_monthly_usd REAL,
		priority_weight    INTEGER NOT NULL DEFAULT 50,
		registered_at      INTEGER NOT NULL,
		config_json        TEXT                 -- serialized full config for fast reads
	);

	-- OpenCode sessions registered by plugin (Plan 7) -------------------------

	CREATE TABLE IF NOT EXISTS sessions (
		id            TEXT PRIMARY KEY,
		project       TEXT NOT NULL,
		runtime       TEXT NOT NULL,            -- "opencode"|"claude-code-vps"
		started_at    INTEGER NOT NULL,
		ended_at      INTEGER,
		FOREIGN KEY (project) REFERENCES projects(id)
	);
	CREATE INDEX IF NOT EXISTS idx_sessions_project ON sessions(project);

	-- Swarm runs (Plan 5) -----------------------------------------------------

	CREATE TABLE IF NOT EXISTS swarms (
		id            TEXT PRIMARY KEY,
		project       TEXT NOT NULL,
		feature       TEXT NOT NULL,
		phase         TEXT NOT NULL,            -- "proposing"|"applying"|"archiving"|"completed"|"aborted"
		started_at    INTEGER NOT NULL,
		ended_at      INTEGER,
		parallelism   INTEGER NOT NULL,
		FOREIGN KEY (project) REFERENCES projects(id)
	);
	CREATE INDEX IF NOT EXISTS idx_swarms_project_phase ON swarms(project, phase);

	-- Tasks within swarms (Plan 5) --------------------------------------------

	CREATE TABLE IF NOT EXISTS tasks (
		id            TEXT PRIMARY KEY,
		swarm_id      TEXT NOT NULL,
		spec_json     TEXT NOT NULL,            -- serialized task spec
		phase         TEXT NOT NULL,            -- per task_state.current_phase
		provider      TEXT,                     -- assigned agent profile model
		started_at    INTEGER NOT NULL,
		ended_at      INTEGER,
		outcome       TEXT,                     -- "green"|"failed"|"killed"|"accepted-as-is"
		FOREIGN KEY (swarm_id) REFERENCES swarms(id)
	);
	CREATE INDEX IF NOT EXISTS idx_tasks_swarm ON tasks(swarm_id);
	`,

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
