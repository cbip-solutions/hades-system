# ADR-0031 — Plan 6 MergeEngine OWNS full test orchestration

**Status:** Accepted
**Date:** 2026-05-01
**Decision-maker:** the operator
**Plan:** Plan 6 (Q2 C)
**Related:** ADR-0030 (package boundary), Plan 5 Phase D (dispatcher)

## Context

The merge pipeline must execute regression baseline + N candidate test runs + flake-rerun + scoring. Two architectural shapes were considered:

- **Q2 A** — Plan 5 dispatcher orchestrates each test command; Plan 6 only computes the score.
- **Q2 C** — Plan 6 OWNS the entire test orchestration; orchestrator dispatcher passes commands only at construction time.

Q2 A keeps Plan 5 dispatcher central but bloats it with merge-specific state (passing-set, flake budget, mode-aware tier selection). Q2 C concentrates merge-specific orchestration in `merge/` where it belongs.

## Decision

Plan 6 `MergeEngine.Merge(ctx, req)` OWNS the full test pipeline:
1. Regression baseline run via `BaselineRunner` (Phase B).
2. Per-candidate runs (parallel, Phase D fanout) via `CandidateRunner` (Phase B).
3. Flake-rerun budget enforcement (Phase B).
4. Scoring + tiebreak (Phase C).

Plan 5 dispatcher passes test commands at construction time (`merge.NewEngine(deps)` consumes pre-built `BaselineRunner`/`CandidateRunner` instances) and is otherwise uninvolved in per-merge orchestration.

## Consequences

- **Plan 5 dispatcher stays focused** on cost-gating + circuit-breaker + LLM tier dispatch. No merge-specific state.
- **Test-pipeline state lives in `merge/`** — passing-set, flake budget, mode-aware tier — close to where it's consumed.
- **Easier replay determinism** (inv-zen-105) — single owner of cache key derivation + outcome composition.

## Doctrine alignment

- **Max-scope:** Plan 6 ships the complete test orchestration day 1; no "MVP that delegates to Plan 5 then refactors later".
- **Hard parts are where value lives:** flake-rerun budget + tier-aware test selection are subtle; consolidating in `merge/` keeps them auditable.
- **No defer:** all 4 pipeline stages ship in Phase D; no "orchestrator extension for Plan 7".

## SOTA references

- Trae Agent (arXiv 2507.23370) — agentic test orchestration in a leaf component.
- Team Atlanta DARPA AIxCC 2026 — patch-test-merge pipelines with regression baseline.
- Anthropic 16-agent C compiler — per-stage test ownership.

## Plan impact

- Phase B: Cache + BaselineRunner + CandidateRunner.
- Phase C: Scorer + AnomalyDetector.
- Phase D: Runner (fanout) + Engine (8-step pipeline).
- Plan 5 dispatcher emits `EvtMergeStartedWithMode` only as observability handoff.
