# ADR-0037 — Adaptive parallelism revisit window (RESERVATION)

**Status:** Reserved (revisit triggered)
**Date:** 2026-05-01
**Decision-maker:** the operator
**Plan:** Plan 6 (Q6 reservation)
**Related:** ADR-0030 (package boundary), Plan 5 worktreepool

## Context

Plan 6 v1 ships fixed-parallelism candidate fanout: N candidates → N concurrent goroutines, each leasing one worktree from Plan 5 `WorktreePool`. Pool capacity is operator-configured (default 12). Q6 C accepted the fixed-N approach for v1; adaptive parallelism (auto-tune N based on pool saturation, host load, or candidate complexity) is reserved.

## Decision

RESERVE the slot. Re-evaluate when ANY of these triggers fire:

1. **Plan 6 emits N>pool capacity events at >5% rate** — signal that operators are submitting more candidates than pool capacity, and adaptive scaling could amortize.
2. **Pool saturation telemetry shows wait-queue depth >10s p95 over 30 days** — signal of contention; adaptive parallelism could rebalance against host load.
3. **Operator request** for explicit scope expansion (e.g., zen-swarm fleet workloads where one merge engine serves multiple operators concurrently).

## Triggers (concrete)

- Trigger 1: Plan 5 amendment.proposer subscribes to N>capacity events (custom anomaly type added in revisit ADR; not in Plan 6 v1).
- Trigger 2: Plan 5 worktreepool emits saturation telemetry; Plan 6 anomaly detector adds `AnomalyPoolSaturationPersistent` evaluator on revisit.
- Trigger 3: operator opens this ADR via doctrine inspection.

## On re-evaluation

Re-evaluation produces:
- Either a new ADR-00XX implementing adaptive parallelism (e.g., AIMD-style scale-up/scale-down based on pool saturation + host CPU).
- Or this ADR is closed as "REJECTED" if fixed-N proves sufficient over the observation window.

The slot in `docs/decisions/` is reserved as 0037 to preserve numeric continuity.

## Doctrine alignment

- **No defer with explicit re-evaluation:** triggers are observable via Plan 5 telemetry + Plan 6 anomaly detector.
- **Max-scope-feasible:** fixed-N is sufficient for single-operator workloads (zen-swarm v1 audience); adaptive parallelism is fleet-scale concern reserved for that scope expansion.

## SOTA references

- Plan 5 worktreepool design — fixed-capacity pool with elastic spawn.
- AIMD (Additive Increase Multiplicative Decrease) — TCP congestion control prior art for adaptive parallelism.
- Modern test runners (Buck2, Bazel) — adaptive parallelism via sandbox-aware schedulers.

## Plan impact

- Plan 6 ships fixed-N.
- Plan 5 amendment.proposer monitors trigger 1 (Phase F-7 amendment).
- Future plan opens ADR-00XX adaptive impl if triggers fire.
