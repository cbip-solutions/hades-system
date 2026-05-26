# ADR-0038 — Git version matrix CI revisit window (RESERVATION)

**Status:** Reserved (revisit triggered)
**Date:** 2026-05-01
**Decision-maker:** the operator
**Plan:** Plan 6 (Q3 reservation)
**Related:** ADR-0032 (textual-only via git merge-tree)

## Context

Plan 6 requires git ≥2.40 for `merge-tree --write-tree` API (Phase A `VersionCheck` enforces). The CI matrix tests against the system's installed git version (whatever is on the GHA runner image). Cross-version drift is theoretically possible — git 2.40 vs 2.50 may exhibit different conflict-resolution behavior that breaks replay determinism (inv-zen-105) across operator environments.

## Decision

RESERVE the slot. Re-evaluate when ANY of these triggers fire:

1. **git 2.40-2.50 cross-version drift incidents documented** — operator or community report of `merge-tree` producing different results between minor versions on the same input.
2. **Plan 6 replay-determinism compliance test (inv-zen-105) fails** in production CI on a runner image upgrade (signaling that git version matters at the bit-replay level).
3. **Operator deploys to a fleet with heterogeneous git versions** — explicit scope where matrix testing becomes load-bearing.

## Triggers (concrete)

- Trigger 1: monitor https://lore.kernel.org/git/ for `merge-tree` regression reports + zen-swarm community issues tagged `git-version-drift`.
- Trigger 2: Plan 6 inv-zen-105 compliance test failure on CI runner image upgrade. Plan 5 amendment.proposer drafts revisit ADR.
- Trigger 3: operator request via doctrine inspection.

## On re-evaluation

Re-evaluation produces:
- Either a new ADR-00XX adding a git version matrix to CI (test against git 2.40 / 2.45 / 2.50 / latest).
- Or this ADR is closed as "REJECTED" if cross-version drift proves negligible.

The slot in `docs/decisions/` is reserved as 0038 to preserve numeric continuity.

## Doctrine alignment

- **No defer with explicit re-evaluation:** triggers are observable via CI + community.
- **Hard parts are where value lives:** replay determinism across operator environments is non-trivial; reservation acknowledges the risk without prematurely committing to matrix CI.
- **Max-scope-feasible:** single-version CI is sufficient for v1 operator audience; matrix is fleet-scale concern.

## SOTA references

- git release notes 2.40-2.50 — `merge-tree` API stabilization claims.
- zansara.dev 2026 — replay determinism across tool versions.

## Plan impact

- Plan 6 ships single-version CI (whatever GHA runner provides).
- Plan 5 amendment.proposer monitors trigger 2 (Phase F-7 amendment).
- Future plan opens ADR-00XX matrix CI if triggers fire.
