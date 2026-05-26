# ADR-0034 — Plan 6 amendment-flow typed-anomaly integration

**Status:** Accepted
**Date:** 2026-05-01
**Decision-maker:** the operator
**Plan:** Plan 6 (Q11 D)
**Related:** ADR-0033, Plan 5 amendment.proposer

## Context

Plan 6 detects 5 distinct anomaly subtypes (scoring-formula winner vetoed, baseline unstable across sessions, flake rate above threshold, textual-merge unresolvable rate high, mode degradation persistent). Plan 5 amendment.proposer drafts ADRs in response to anomalies under per-doctrine cooldown windows. Two shapes:

- **Q11 A** — emit one EventType per subtype (`EvtAnomalyScoringFormulaWinnerVetoed`, `EvtAnomalyFlakeRateAboveThreshold`, ...). Compile-time taxonomy; many EventType constants.
- **Q11 D** — emit a single `EvtMergeAnomalyDetected` with `AnomalyType` discriminator carried in the payload. Subscribers decode payload + switch on `payload.Type`.

## Decision

**Q11 D**: single `EvtMergeAnomalyDetected` EventType + 5-value `AnomalyType` enum carried in `AnomalyDetectedPayload`. Plan 5 amendment.proposer subscribes to the single EventType, decodes payload, switches on `payload.Type` to dispatch per-subtype ADR templates.

This is **Drift-D resolution** — the two-EventType approach (Q11 A) was caught during Plan 6 self-review wave 4 and structurally rejected; the single-EventType approach is now compile-checked via `TestNoPerAnomalyEventTypeConstants` (anomaly_test.go) which iterates `AllEventTypes()` and ensures no per-anomaly EventType slipped in.

## Consequences

- **Closed enum at runtime:** `AnomalyType` is a closed Go int enum; switch-on-payload catches missing cases at runtime (per-template dispatch raises descriptive error if AnomalyType.String() returns "Unknown").
- **EventType inventory stays small** — 16 frozen constants; no per-anomaly explosion.
- **Subscriber dispatch:** `event.Type == EvtMergeAnomalyDetected` filters at subscription; `payload.Type` discriminates inside the handler. Plan 5 amendment.proposer reuses the cooldown windows from Plan 5 §K.
- **Replay determinism preserved** (inv-zen-105) — fewer EventType constants → simpler eventlog schema → easier reconstruction.

## Doctrine alignment

- **Max-scope:** all 5 anomaly subtypes shipped day 1 with per-subtype evaluator (Phase C C-3..C-5); not "evaluate flake first, add others later".
- **Hard parts are where value lives:** rolling-window aggregation per subtype is non-trivial; consolidating in `AnomalyDetector` keeps the math auditable.
- **No defer:** 9 invariants enforced at compile + runtime + tests across Phases A-E.

## SOTA references

- Drift-D self-review (Plan 6 wave 4) — single-EventType pattern aligns with Plan 5 amendment.proposer's existing dispatch-by-payload-discriminator design.
- Phase C `feedback_anomaly_subtype_dispatch.md` (project memory) — empirical: payload-typed dispatch reduces EventType inventory churn during plan iteration.

## Plan impact

- Phase A: `AnomalyType` enum (5 values + Unknown zero) + `EvtMergeAnomalyDetected` EventType.
- Phase C: `AnomalyDetector` with 5 evaluator methods; shared `emit()` helper.
- Phase E: compliance test inv-zen-110 (anomaly typed) + inv-zen-109 (threshold-breach metadata).
- Plan 5 amendment.proposer extension (Phase F-7 amendment): switch-on-payload.Type dispatch.

## Open follow-ups (operator-facing)

### evalTextualUnresolvable denominator semantics — deferred per C-5 review

`evalTextualUnresolvable` (in `anomaly.go`) computes its rate using a sliding session-count window over `EvtCandidateFailed` events: the denominator is **per-failed-candidate** (1 entry per `CandidateFailedPayload`), and `PatchRejected` failures contribute to the numerator. Spec §12.7 prose suggests **per-total-invocation** (1 entry per `EvtCandidateComplete` + per `EvtCandidateFailed`); spec §12.7 is itself marked "open question deferred a /write-plan o execution".

**What was chosen + why**: Plan 6 Phase §C-5 wrote the per-failed-candidate semantics into the plan-template, the implementer followed plan-fidelity, and the reviewer surfaced the divergence as I-1 (Plan 6 final review). The implementation is internally consistent + well-tested + downstream-consumable. Threshold values (`textual_unresolvable_rate_pct = 10.0` per `DefaultAnomalyThresholds`) were tuned against the per-failed denominator implicitly.

**What this means for operators**:
- Current metric: "of all candidate **failures**, what % are PatchRejected" — a meaningful diagnostic of failure-mode distribution.
- Spec metric: "of all candidate **invocations**, what % failed via textual-merge-unresolvable" — the ADR-0035 trigger semantically tracks how often textual merge breaks ANY merge attempt, not how it dominates the failure mix.
- These two diverge sharply when total failures are rare. Example: 1000 invocations, 4 failures (3 PatchRejected, 1 Timeout) → current rate = 75% (alert!), spec rate = 0.3% (no alert).

**Re-tuning path**: post-merge, the operator may either (a) accept the current semantics + tune `textual_unresolvable_rate_pct` upward (e.g., to 30-50%) so the per-failed metric only fires under sustained PatchRejected dominance, or (b) re-implement to per-total semantics + restore the 10% threshold, which requires extending `OnEvent` so `EvtCandidateComplete` also feeds `d.unresolvable` (with `unresolvable=false`) — a ~10 LoC dispatch change + threshold re-tune. ADR-0035 (AST/structured merge revisit) consumes whichever semantic the operator chooses; the trigger semantics are unaffected.
