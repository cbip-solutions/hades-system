# ADR-0030 — Plan 6 package boundary: `internal/orchestrator/merge/`

**Status:** Accepted
**Date:** 2026-05-01
**Decision-maker:** the operator
**Plan:** Plan 6 (Q1 B)
**Related:** ADR-0001 (substrate boundary), Plan 5 Phase J (interface relocation source)

## Context

Plan 5 Phase J shipped the `MergeEngine` interface in `internal/orchestrator/apply/`. As Plan 6 layers on the cross-worker merge engine (5 subcomponents: textual driver + candidate runner + regression baseline + scoring engine + cache), keeping all the new code in `apply/` would cause that package to balloon and conflate "single-branch live correction" (apply's domain) with "cross-worker integration" (Plan 6's domain).

## Decision

Plan 6 ships a NEW package `internal/orchestrator/merge/` that hosts the `MergeEngine` real implementation + the 5 subcomponents (Phase A substrate, Phase B test pipeline, Phase C decision logic, Phase D orchestration core, Phase E test infra). The `MergeEngine` INTERFACE is RELOCATED from `apply/` to `merge/` via an additive amendment commit on the `plan-5-brainstorm` branch (Phase F.7 of Plan 6). Plan 5 dispatcher + HRA imports update accordingly post-amendment.

## Consequences

- **Boundary discipline:** `merge/` is a leaf package; imports stdlib + `internal/orchestrator/{eventlog, worktreepool, clock}`; never `internal/store` (inv-zen-104 enforced via compliance test `tests/compliance/inv_zen_104_*`).
- **Forward-compat:** ADR-0035 (AST), ADR-0036 (LLM), ADR-0037 (adaptive parallelism) revisits land in `merge/` cleanly without retrofitting other packages.
- **Plan 5 amendment:** the J.5 commit on `plan-5-brainstorm` documents the relocation; existing Plan 5 code uses `merge.MergeEngine` post-amendment.
- **Reduced cognitive load:** package boundaries align with operator mental model — `apply/` = single-branch live correction; `merge/` = cross-worker integration.

## Doctrine alignment

- **Max-scope:** dedicated package for cross-worker concerns; not retrofit into `apply/`.
- **Build the final product:** package is born at full shape (10+ files at end of Phase D); no incremental MVP scaffold.
- **No tech debt:** boundary documented + enforced via inv-zen-104 import-graph compliance test.

## SOTA references

- Plan 5 §1 Q1 D — apply ↔ merge boundary at struct level.
- This ADR extends to package level per Q1 B.

## Plan impact

- Plan 6 Phases A-D ship the package contents.
- Plan 5 Phase J interface relocation in Phase F.7 (cross-branch additive commit on `plan-5-brainstorm`).
- Future Plan 7+ extensions add to `merge/` without crossing the apply boundary.
