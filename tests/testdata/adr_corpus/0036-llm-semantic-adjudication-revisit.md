# ADR-0036 — LLM semantic conflict adjudication revisit window (RESERVATION)

**Status:** Reserved (revisit triggered)
**Date:** 2026-05-01
**Decision-maker:** the operator
**Plan:** Plan 6 (Q3 reservation)
**Related:** ADR-0032 (textual-only v1), ADR-0035 (AST revisit)

## Context

Plan 6 v1 rejects LLM-based merge conflict adjudication for replay-determinism reasons (zansara.dev 2026: even temperature=0 has output drift across model versions). State-of-the-art LLM systems (Anthropic Claude, OpenAI GPT) cannot guarantee bit-identical outputs across deployment windows.

## Decision

RESERVE the slot. Re-evaluate when ANY of these triggers fire:

1. **Deterministic-mode LLM endpoints publicly available with reproducibility guarantees**. Specifically: a major provider (Anthropic, OpenAI, Google) ships a documented "deterministic mode" with bit-identical output guarantees across deployment generations.
2. **Local-inference matures** to production-grade with reproducible outputs across hardware (e.g., llama.cpp deterministic build with fixed-precision inference; vLLM deterministic mode).
3. **Plan 6's textual-only path proves insufficient** for >10% of merges over 12 months (signal: combined `AnomalyTextualMergeUnresolvableRateHigh` + `AnomalyBaselineUnstableAcrossSessions` rate, indicating structural conflict patterns that AST also can't resolve).

## Triggers (concrete)

- Trigger 1: monitor Anthropic API release notes + OpenAI API docs + Google AI release notes. Operator opens this ADR upon a "deterministic mode" announcement with reproducibility SLA.
- Trigger 2: monitor llama.cpp + vLLM release notes for "deterministic" build flag + reproducibility test suite passing across CI matrix.
- Trigger 3: dual-anomaly-rate monitoring via Plan 6 anomaly detector + Plan 5 amendment.proposer 12-month rolling window.

## On re-evaluation

Re-evaluation produces:
- Either a new ADR-00XX upgrading the merge engine to "LLM semantic + AST + textual" (3-tier with deterministic fallback).
- Or this ADR is closed as "REJECTED" if the LLM determinism story doesn't materialize.

The slot in `docs/decisions/` is reserved as 0036 to preserve numeric continuity.

## Doctrine alignment

- **No defer with explicit re-evaluation:** triggers are externally observable; re-evaluation is data-driven.
- **Replay determinism is non-negotiable:** inv-zen-105 + inv-zen-107 require deterministic outputs; LLMs without reproducibility guarantees cannot satisfy.
- **Capa-firewall posture:** operator can choose to enable LLM adjudication post-revisit even with weaker determinism guarantees if the doctrine allows it; this ADR doesn't prejudge the answer.

## SOTA references

- zansara.dev 2026 — LLM determinism analysis: even temperature=0 has output drift across deployment generations.
- Anthropic Claude API documentation 2026 — current temperature=0 NOT documented as deterministic across model versions.
- llama.cpp 2026 — deterministic build flag exists but reproducibility across hardware not guaranteed.

## Plan impact

- Plan 6 ships textual-only.
- Plan 5 amendment.proposer trigger-3 monitoring wired (Phase F-7 amendment).
- Future plan (Plan 9+) opens ADR-00XX upgrade if determinism story matures.
