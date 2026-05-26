# ADR-0033 — Plan 6 cost-gating Mode enum + doctrine mapping

**Status:** Accepted
**Date:** 2026-05-01
**Decision-maker:** the operator
**Plan:** Plan 6 (Q7 B + Q13 D)
**Related:** ADR-0030, ADR-0034, Plan 5 cost_gating

## Context

Plan 5 cost_gating tracks budget consumption per session/project. When budget reaches 60%/80%/90% thresholds, downstream merge orchestration must adapt: fewer candidates, stricter test tier, lower flake-rerun budget. Two shapes were considered:

- **Q7 A** — Plan 6 reads cost_gating numeric percentage directly; per-merge logic computes degradation.
- **Q7 B** — Plan 6 owns a closed `Mode` enum (Normal / Degraded60 / Degraded80 / EmergencyOnly); Plan 5 cost_gating maps (doctrine, level) → Mode at the boundary.

## Decision

**Q7 B**: Plan 6 declares `merge.Mode` as a closed enum. Plan 5 cost_gating performs the mapping at boundary; Plan 6 consumes only the enum value. Doctrine matrix per Q13 D ships day 1 with three columns (max-scope / default / capa-firewall) for every per-Mode parameter (candidate count, test tier, flake budget, timeout).

## Consequences

- **Closed-enum dispatch compiles** — switch-on-Mode catches missing cases at build time (vs string-percent comparison race).
- **Doctrine matrix is a single source of truth** — Phase F-5 doctrine TOML loader exposes per-Mode parameters; per-project TIGHTEN-only override (inv-zen-111).
- **Plan 5 boundary stays narrow** — cost_gating doesn't grow merge-specific knowledge; just a small mapping function.
- **Capa-firewall doctrine override** — operator can map (cost_level=80%) → ModeEmergencyOnly instead of ModeDegraded80 to escalate L4 rather than degrade silently.

## Doctrine alignment

- **Max-scope:** 4-mode taxonomy (not 2 or 3) covers full doctrine spectrum day 1.
- **Build the final product:** Plan 6 declares all 4 modes from inception; not "ship Normal+Degraded, add EmergencyOnly later".
- **No defer:** doctrine matrix shipped in Phase A `mode.go` + Phase F-5 TOML loader; runtime-tunable.

## SOTA references

- Plan 5 cost_gating boundary (Plan 5 Phase D) — single mapping point; symmetric with circuit-breaker tier mapping.
- Q13 D doctrine matrix prior art — Plan 4 doctrine TOML pattern (max-scope/default/capa-firewall columns).

## Plan impact

- Phase A: `Mode` enum + `ModeConfig` + `TestTier` enum.
- Phase D Engine: Mode propagated through `Merge → Baseline → Runner → Scorer`.
- Phase F-5: `[merge.modes]` TOML section with per-doctrine column.
- Plan 5 cost_gating amendment: maps (doctrine, level%) → merge.Mode (Phase J.5 amendment).
