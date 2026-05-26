# ADR-0032 — Plan 6 textual-only v1 via `git merge-tree --write-tree`

**Status:** Accepted
**Date:** 2026-05-01
**Decision-maker:** the operator
**Plan:** Plan 6 (Q3 A)
**Related:** ADR-0035 (AST revisit), ADR-0036 (LLM revisit)

## Context

A cross-worker merge engine has three architectural choices for the merge driver:

- **Q3 A** — textual via `git merge-tree --write-tree` (git ≥2.40 default `ort` strategy). Mature, deterministic, replay-friendly.
- **Q3 B** — AST/structured (Mergiraf, LastMerge). Better at structural conflicts; immature Go grammar support.
- **Q3 C** — LLM semantic adjudication. State-of-the-art on hard conflicts; non-deterministic without temperature=0 + reproducibility guarantees.

## Decision

Plan 6 v1 ships TEXTUAL-only via `git merge-tree --write-tree`. Conflict markers in the resulting tree → candidate is `HardRejected` with `CandidateFailurePatchRejected`. Test-failures on the merged tree → `BaselineBreaker` rejection. Scorer drops both.

## Consequences

- **Determinism preserved:** `git merge-tree` is content-addressable; same inputs → same tree → replay produces identical outcomes (inv-zen-105).
- **Predictable failure modes:** unresolvable conflicts surface as `EvtCandidateFailed{type=PatchRejected}`; rolling-window observability via `AnomalyTextualMergeUnresolvableRateHigh`.
- **Operator-facing:** Plan 5 amendment.proposer drafts ADR-0035 revisit if textual unresolvable rate >5% over 6 months (per ADR-0035 trigger).
- **Git ≥2.40 prerequisite:** `merge-tree --write-tree` API requires the version. Phase A `VersionCheck` enforces at engine init.

## Doctrine alignment

- **Max-scope as feasible:** ADR-0035 + ADR-0036 are RESERVED for revisit when ecosystem matures; not deferred-and-forgotten.
- **No defer with explicit re-evaluation:** triggers documented; observability metrics drive re-evaluation, not calendar dates.
- **Build the final shape:** `merge.Engine` is feature-complete day 1 within textual-driver scope.

## SOTA references

- Schesch ASE 2024 (arXiv 2410.09934) — survey of merge drivers; textual remains the determinism floor for replay-critical systems.
- zansara.dev 2026 — LLM determinism limitations; even temperature=0 has output drift across model versions.
- git 2.40 release notes — `merge-tree --write-tree` API stabilization.

## Plan impact

- Phase A: `VersionCheck` + `MergeBase` + `RevParse` git wrappers.
- Phase B: `ApplyPatch` rejects malformed/conflicting patches via merge-tree exit code.
- Phase E: adversarial tier exercises `git apply` rejection paths.
- ADR-0035 revisit: trigger fires on `AnomalyTextualMergeUnresolvableRateHigh` rate >5%/6mo.
