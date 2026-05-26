# ADR-0054 — Plan 8 telemetry-driven autonomous-revert: per-rule SIGNAL + 3 generic event-categories + shared aggregators

**Status:** Accepted
**Date:** 2026-05-03
**Decision-maker:** the operator
**Plan:** Plan 8 (Q3 C + Q13 C)
**Related:** ADR-0050 (per-doctrine TOMLs carry per-rule revert metadata), ADR-0051 (hybrid enforcement model — `enforcement = "enforce"` triggers revert path), ADR-0053 (two-field versioning), Plan 5 §K (amendment.proposer + Reverter), Plan 5 event taxonomy (cost / merge / recovery groupings)

## Context

Plan 5 ships the amendment lifecycle: `Proposer` drafts ADRs in response to anomalies; `Applier` writes accepted amendments; `Reverter` rolls back applied amendments. Plan 8 extends this with telemetry-driven autonomous-revert: when post-application telemetry shows the amendment did NOT improve outcomes (or actively regressed them), trigger a `Reverter.Revert(adr_id)` automatically.

Three architectural shapes were considered:

- **Q13 A** — Per-rule custom event types (e.g., `EvtRuleCostGatingThresholdRevertSignal`). Compile-time taxonomy; ~30 new EventType constants; ~30 new aggregators.
- **Q13 B** — Single generic event type (`EvtDoctrineRuleRevertSignal`) with rule-path discriminator in payload. Plan 5 amendment.proposer subscribes once; switch on payload's `rule_path` to dispatch. Single subscriber but no per-category aggregation logic.
- **Q13 C** — Per-rule SIGNAL declared in TOML metadata (`revert_category = "cost" | "merge" | "recovery"` + threshold + window + cooldown) + 3 generic event-categories with shared aggregators. Each rule declares which category it belongs to; the 3 aggregators handle category-specific signal aggregation; a single `Reverter` consumes the aggregator output.

Q13 A explodes EventType inventory (~30 new constants for ~30 rules). Q13 B loses category-specific aggregation logic (cost trends differ from merge anomaly trends differ from recovery escalation patterns). Q13 C balances: 3 category-aggregators (cost, merge, recovery) handle the structurally-distinct signal shapes; per-rule TOML metadata routes each rule to the appropriate category.

Plan 5's existing event taxonomy already groups events into cost / merge / recovery categories (per Plan 5 Q14 C event taxonomy + per Plan 5's existing `BudgetDegradationApplied` / `MergeAnomalyDetected` / worker-failure event groupings). Q13 C is a native fit: zen-swarm already has the category boundary; Plan 8 adopts it for revert.

## Decision

**Q13 C** + Q3 C: per-rule SIGNAL with 3 generic event-categories and shared aggregators.

### Per-rule SIGNAL metadata in TOML

Each Tier 2 matrix rule (per ADR-0051) carries revert metadata:

```toml
[cost_gating.degradation]
enforcement = "enforce"             # per ADR-0051 Tier 2 enum
revert_category = "cost"            # "cost" | "merge" | "recovery" | "none"
revert_threshold_pct = 30           # below-threshold sessions trigger revert
revert_window_sessions = 50         # rolling-window size in completed sessions
revert_cooldown_hours = 168         # 7d cooldown after a revert before re-eval

[merge.flake_budget]
enforcement = "enforce"
revert_category = "merge"
revert_threshold_pct = 25
revert_window_sessions = 30
revert_cooldown_hours = 72

[recovery.retry_max]
enforcement = "enforce"
revert_category = "recovery"
revert_threshold_pct = 40
revert_window_sessions = 20
revert_cooldown_hours = 96
```

Rules with `revert_category = "none"` are not eligible for autonomous revert (telemetry observes; aggregator does not aggregate). Operator-required rules (Tier 2 `operator-required`) remain operator-gated; the SIGNAL fires but the action is `notify operator`, not `auto-revert`.

### 3 aggregators

Three aggregators ship under `internal/orchestrator/amendment/aggregator/`:

#### `cost.go` — cost aggregator

Subscribes to Plan 5 events:
- `EvtBudgetDegradationApplied` (cost-gating mode transition)
- `EvtBypassedSoftCheckEvent` (cost-related soft-check skip)

Aggregation: rolling-window pct over the rule's `revert_window_sessions`. For each rule with `revert_category = "cost"`, the aggregator tracks "sessions completed without cost-category anomaly since the rule's last `DoctrineAmendmentApplied`" / "total sessions in window". When the ratio falls below `(100 - revert_threshold_pct)%`, AND the rule's `revert_cooldown_hours` has expired since its last revert/apply, fire `RevertSignal{adr_id}`.

#### `merge.go` — merge aggregator

Subscribes to Plan 5 events:
- `EvtMergeAnomalyDetected.*` (per ADR-0034 typed-anomaly: 5 subtypes via payload.Type)
- `EvtMergeFailed`
- `EvtMergeAllCandidatesFailed`

Aggregation: same rolling-window logic, scoped to `revert_category = "merge"` rules. Anomaly subtype is preserved in the aggregator's event detail for ADR re-proposal.

#### `recovery.go` — recovery aggregator

Subscribes to Plan 5 events:
- Worker failure events (per Plan 5 Q7 D taxonomy: 7 fault classes)
- HRA escalation events (per Plan 5 Q5 C / Q8 B)

Aggregation: same rolling-window logic, scoped to `revert_category = "recovery"` rules. Distinguishes "recovery escalation rate" from "recovery success rate"; both feed the same window.

### Shared `TelemetrySubscriber` + Reverter wiring

`internal/orchestrator/amendment/telemetry_subscriber.go` (NEW; cross-branch additive per ADR-0050) subscribes Plan 5 eventlog. On each event, dispatches to ALL aggregators (each filters by EventType; a single eventlog read serves all 3 aggregators). When an aggregator fires `RevertSignal{adr_id}`, the subscriber:

1. Acquires the rule's per-rule cooldown lock (per inv-zen-139 prevents oscillation).
2. Validates the rule is still `enforcement = "enforce"` in the active doctrine (operator may have downgraded to `warn` mid-window).
3. Calls `amendment.Reverter.Revert(adr_id)` (Plan 5 surface; extends existing `Reverter` per ADR-0050 cross-branch additive).
4. Emits `EvtDoctrineAutonomousReverted{adr_id, rule_path, signal_category, window_pct, threshold_pct}`.

The Reverter performs the reverse-amendment write to the doctrine TOML (touches `~/.config/zen-swarm/doctrines/<name>.toml`); the file-watcher (per Q10 C) fires; atomic-swap to the reverted schema; in-flight workers complete with the old schema (inv-zen-092 atomicity), new tasks pick up the reverted schema.

### Cooldown discipline (inv-zen-139)

Each rule's `revert_cooldown_hours` is enforced strictly: after a `Reverter.Revert(adr_id)` succeeds, the rule is cooldown-locked for `revert_cooldown_hours` before any further auto-revert can fire on the same `rule_path`. Prevents oscillation (apply → revert → apply → revert flapping). Operator can manually clear cooldown via `zen doctrine cooldown clear <rule_path> --reason "..."`.

If the same rule's window dips below threshold AGAIN within cooldown, the SIGNAL is still emitted as an event (observable in `zen doctrine history`) but the Reverter is NOT invoked. Operator notified via `zen doctrine status` if a rule has been signal-firing within cooldown more than once (suggests deeper problem; manual investigation needed).

### EventType inventory bounded

Plan 8 adds exactly THREE new EventTypes:
- `EvtDoctrineAutonomousReverted` (emitted by Reverter on success)
- `EvtDoctrineRevertSignalFired` (emitted by aggregator when threshold breached; observation-only)
- `EvtDoctrineRevertCooldownActive` (emitted when SIGNAL fires within cooldown; operator-visible)

NO per-rule EventTypes (Q13 A explosion avoided). Plan 5's existing event taxonomy is consumed via subscription; not extended.

## Consequences

- **EventType inventory stable**: 3 new EventTypes total (vs ~30 in Q13 A). Eventlog schema simple; replay determinism preserved (inv-zen-105).
- **Per-category aggregation logic preserved**: cost / merge / recovery have structurally-distinct signal shapes; 3 aggregators handle them appropriately. A single generic aggregator (Q13 B) would have buried the structure.
- **Plan 5 native fit**: Plan 5 already groups events into cost / merge / recovery; Plan 8 adopts the same boundary. Zero new event taxonomy; zero churn on Plan 5's frozen v0.5.0 contract.
- **Operator can audit revert decisions**: every revert emits `EvtDoctrineAutonomousReverted` with full context (signal_category, window_pct, threshold_pct); `zen doctrine history` shows the trail.
- **Cooldown prevents flapping**: per-rule cooldown locks; oscillation impossible by construction.
- **Capa-firewall safe**: capa-firewall doctrine has all rules at `enforcement = "operator-required"` (per ADR-0051 hard guard); SIGNAL fires but action is `notify`, not `auto-revert`. Pulido-strict layer keeps operator agency.
- **Cross-branch additive coordination clean**: aggregators ship as NEW files in `internal/orchestrator/amendment/aggregator/` (no Plan 5 file edits beyond the `applier.go` + `reverter.go` extensions per ADR-0050). Sync Point S3 surface narrow.

## Doctrine alignment

- **Max-scope:** all 3 categories ship day 1 (cost + merge + recovery); not "ship cost first, add merge + recovery later".
- **Build the final product:** the per-rule SIGNAL + 3-category-aggregator + Reverter wiring IS the final shape; no MVP-then-extend penalty.
- **No defer:** cooldown discipline (inv-zen-139) shipped same Phase H as the aggregators; not "we'll add anti-flapping next release".
- **No tech debt:** the Plan 5 amendment lifecycle is extended additively (per ADR-0050); no inversion of ownership; no scaffold to retrofit.
- **Tests are the floor:** per-aggregator unit tests, integration tests covering full SIGNAL → Revert lifecycle, cooldown adversarial tests, capa-firewall safety tests.

## SOTA references

- [Statsig Auto-Rollback / Predictive Pulse](https://www.flagsmith.com/blog/statsig-alternatives) — telemetry-driven autonomous revert pattern; convergent shape.
- [LaunchDarkly Feature Flag Hierarchy](https://launchdarkly.com/docs/guides/flags/flag-hierarchy) — per-flag rollback policy precedent; zen-swarm's per-rule SIGNAL is the analogous shape at the doctrine layer.
- [OPA Bundle Versioning](https://www.openpolicyagent.org/docs/management-bundles) — bundle revision tracking precedent; aggregator's "since last apply" window mirrors OPA's bundle revision boundary.
- Plan 5 Q14 C event taxonomy — Plan 5 native cost / merge / recovery groupings; Plan 8 adopts verbatim (per Q6 C cross-matrix reconciliation).
- Plan 6 Q11 D AnomalyType payload-typed dispatch — precedent for "single EventType + payload discriminator" pattern; Plan 8's `EvtDoctrineRevertSignalFired` follows the same shape (signal_category in payload, not EventType).
- [Per-rule cooldown research](https://www.flagsmith.com/blog/statsig-alternatives) — telemetry-driven revert systems require cooldown discipline; alert-fatigue research informed inv-zen-139.

### Key findings informing the decision

- **Pure operator-gate has documented approval-fatigue failure mode**: rejected option Q3 A (per ADR-0051 hybrid enforcement adopts Tier 1 + Tier 2; Plan 8 telemetry-revert is the autonomous half of Tier 2 `enforce`).
- **Single-version field has documented alert-fatigue OR data-loss anti-pattern**: rejected option Q5 A (per ADR-0053 two-field versioning; SIGNAL events distinguish observation from action).
- **Per-rule cooldown prevents oscillation**: 2026 telemetry-driven systems converge on per-rule cooldown.

## Plan impact

- Plan 8 Phase A: `internal/doctrine/schema/v1/schema.go` matrix sections declare per-rule revert metadata fields with appropriate `tighten:` struct tags.
- Plan 8 Phase B: `parser.ParseStrict` validates revert_category enum; rejects unknown categories.
- Plan 8 Phase C: `builtin/{max-scope,default,capa-firewall}.toml` ship with revert metadata populated per Phase 0 reconciliation acks (R-items).
- Plan 8 Phase H: cross-branch additive per ADR-0050:
  - `internal/orchestrator/amendment/telemetry_subscriber.go` — NEW.
  - `internal/orchestrator/amendment/aggregator/cost.go` — NEW.
  - `internal/orchestrator/amendment/aggregator/merge.go` — NEW.
  - `internal/orchestrator/amendment/aggregator/recovery.go` — NEW.
  - `internal/orchestrator/amendment/reverter.go` — EXTENDED (TelemetrySubscriber invokes; emits `EvtDoctrineAutonomousReverted`).
- Plan 8 Phase L: integration test `tests/integration/doctrine_telemetry_revert_test.go` covers full SIGNAL → Revert lifecycle.
- Plan 8 Phase L: chaos test `tests/chaos/doctrine_revert_oscillation_test.go` asserts cooldown prevents flapping under adversarial telemetry.

## Compliance test references

- `internal/orchestrator/amendment/aggregator/cost_test.go` — cost aggregator rolling-window logic.
- `internal/orchestrator/amendment/aggregator/merge_test.go` — merge aggregator + AnomalyType subtype handling.
- `internal/orchestrator/amendment/aggregator/recovery_test.go` — recovery aggregator + Plan 5 Q7 D fault-class subscription.
- `internal/orchestrator/amendment/telemetry_subscriber_test.go` — subscriber dispatch + Reverter invocation + EventType emission.
- `tests/compliance/inv_zen_139_test.go` — per-rule cooldown lock prevents oscillation.
- `tests/compliance/inv_zen_092_test.go` — atomicity (in-flight workers complete with old doctrine; new tasks pick up reverted doctrine).
- `tests/integration/doctrine_capa_firewall_revert_safety_test.go` — capa-firewall doctrine: SIGNAL fires but Revert NOT invoked (operator-required guard).
