# ADR-0035 — AST/structured merge revisit window (RESERVATION)

**Status:** Reserved (revisit triggered)
**Date:** 2026-05-01
**Decision-maker:** the operator
**Plan:** Plan 6 (Q3 reservation)
**Related:** ADR-0032 (textual-only v1), ADR-0036 (LLM revisit)

## Context

Plan 6 v1 ships textual-only merge driver per ADR-0032. AST-based / structured merge tools (Mergiraf, LastMerge) handle structural conflicts (e.g., refactor + simultaneous edit) that textual merge rejects. Go grammar support in these tools is immature as of 2026-05-01.

## Decision

RESERVE the slot. Re-evaluate when ANY of these triggers fire:

1. **Mergiraf or LastMerge ships production-grade Go grammar** (parses 100% of the zen-swarm Go corpus without false-positive conflicts).
2. **Plan 6 emits `AnomalyTextualMergeUnresolvableRateHigh` events at >5% rate over 6 months** (per Phase C threshold). The rate signals textual driver hitting structural limits more often than acceptable.
3. **Operator request** — explicit scope expansion via doctrine override.

## Triggers (concrete)

- Trigger 1: monitor Mergiraf releases at https://github.com/szabols/mergiraf and LastMerge at https://github.com/lastpass/lastmerge for "1.0" + "Go support" announcements. Operator opens this ADR for re-evaluation upon trigger.
- Trigger 2: Plan 5 amendment.proposer subscribes to `EvtMergeAnomalyDetected` with `payload.Type=AnomalyTextualMergeUnresolvableRateHigh`. Cooldown 6 months; rate >5% sustained → drafts revisit ADR.
- Trigger 3: operator opens this ADR via `zen merge config show` doctrine inspection.

## On re-evaluation

If any trigger fires, the revisit produces:
- Either a new ADR-00XX upgrading ADR-0032 to "AST + textual fallback" (3-tier).
- Or this ADR is closed as "REJECTED" with rationale.

The slot in `docs/decisions/` is reserved as 0035 to preserve numeric continuity.

## Doctrine alignment

- **No defer with implicit re-evaluation:** triggers are observable + automated; re-evaluation is data-driven, not calendar-driven.
- **Max-scope-feasible:** textual ships day 1 (matches market maturity); AST stays reserved (doesn't pretend ecosystem is ready when it isn't).

## SOTA references

- Mergiraf (Schesch et al. ASE 2024 arXiv:2410.09934) — JavaSelf+TypeScript grammar; Go on roadmap.
- LastMerge — academic prototype 2025; production-grade Go grammar pending.

## Plan impact

- Plan 6 ships textual-only.
- Plan 5 amendment.proposer wired to draft revisit on trigger 2 (Phase F-7 amendment).
- Future plan (Plan 8+) opens ADR-00XX upgrade if AST tools mature.
