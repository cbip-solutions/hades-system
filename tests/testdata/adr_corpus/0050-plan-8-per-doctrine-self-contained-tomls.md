# ADR-0050 — Plan 8 doctrine bundle layout: per-doctrine self-contained TOMLs

**Status:** Accepted
**Date:** 2026-05-03
**Decision-maker:** the operator
**Plan:** Plan 8 (Q2 A)
**Related:** ADR-0007 (gitnexus vendor mode hybrid pattern reference), Q9 B (`//go:embed` built-ins), Q7 C (hybrid override layout)

## Context

Plan 8 ships three built-in doctrines (`max-scope`, `default`, `capa-firewall`), each declaring ~30 knobs (cost-gating thresholds, test-tier enable, retry-recovery cadence, voting weights, severity escalation, HRA cadence, etc.). Three architectural shapes were considered for the on-disk file layout:

- **Q2 A** — Per-doctrine self-contained TOMLs. Each TOML declares ALL ~30 knobs completely. No base+overlay merging; no cross-file inheritance.
- **Q2 B** — Base + overlay model. A `base.toml` declares default values; `<doctrine>.toml` declares only overrides; runtime merges `base + overlay`. Fewer duplicated lines on disk; introduces a merge engine.
- **Q2 C** — Hybrid: per-section ownership (e.g., `cost-gating` lives only in `cost-gating.toml`; `merge` lives only in `merge.toml`); per-doctrine TOMLs reference sections by name. Modular but introduces section-scope graph.

Q2 B and Q2 C minimize redundancy on disk but introduce a merge engine — runtime code that operators must mentally simulate to predict the effective doctrine. Q2 A accepts ~30% surface duplication across the three TOMLs in exchange for direct operator UX: a single file IS the effective doctrine; what you read is what runs.

The operator-directive `feedback_no_stubs_complete_code.md` rejects scaffolds that defer real shape; a merge engine is exactly such a scaffold (it adds infrastructure to support modular composition that, in practice, the operator rarely exercises — there are 3 doctrines, not 30).

## Decision

**Q2 A**: per-doctrine self-contained TOMLs. Each of the three built-in doctrines ships as a complete ~150-200 LOC TOML at `internal/doctrine/builtin/{max-scope,default,capa-firewall}.toml`. Operator-authored doctrines + per-project overrides follow the same shape (mirror layout per Q7 C).

### Concrete shape

```
internal/doctrine/builtin/
├── max-scope.toml         # ~180 LOC: schema_version, doctrine_version, all ~30 knobs
├── default.toml           # ~150 LOC: same knobs at default values
└── capa-firewall.toml     # ~160 LOC: same knobs at strict values

~/.config/zen-swarm/doctrines/             (operator override; mirror shape)
└── <name>.toml             # operator-authored; same ~30 knobs; complete file

<project>/.zen/doctrine-override.toml      (per-project tighten; mirror shape, optional fields)
```

Each TOML carries:

- `schema_version = "1.0"` (file shape; per ADR-0053)
- `doctrine_version = "X.Y.Z"` (rule content semver; per ADR-0053)
- `[transverse]` — 4 axioms hardcoded operator-only (Tier 1 per ADR-0051)
- `[cost_gating]`, `[merge]`, `[recovery]`, `[voting]`, `[severity]`, `[hra]`, ... — all ~30 matrix sections, fully populated

A doctrine TOML is read-evaluable: an operator can `cat ~/.config/zen-swarm/doctrines/max-scope.toml` and see EVERY value that will be applied. No runtime merge to mentally simulate.

### 30% duplication accepted as explicit by-design redundancy

Across the three TOMLs, ~30% of values are identical (e.g., the `[reinforcement]` section's variable allowlist; certain `enforcement = "operator-required"` constants in `[capa_firewall]`). This redundancy is INTENTIONAL: each TOML is the authoritative source for that doctrine; operators reading one TOML never have to chase a base file to know what's in effect.

The redundancy is bounded: TOMLs are 150-200 LOC each, ~510 LOC total across three files; the duplicated portion is ~150 LOC. This is well within review-able size. A merge-engine equivalent would save ~150 LOC of TOML at the cost of ~300+ LOC of merge engine + cache invalidation + diagnostic output. Net: the redundancy is cheaper.

### Per-project override mirrors the shape

`<project>/.zen/doctrine-override.toml` follows the SAME TOML schema as the built-in / user-authored TOMLs (per Q7 C). The override file's optional fields are tighten-only per inv-zen-136 (validator rejects loosened values). Same parser + same validator; single code path; no special "override-only schema".

### Custom doctrines are first-class

An operator can author `~/.config/zen-swarm/doctrines/my-custom-doctrine.toml` (any name not in the embed.FS reserved set), and zen-swarm treats it as a peer of the three built-ins. The same self-contained shape applies. Custom-doctrine + per-project override stack is `<my-custom-doctrine.toml> + <project>/.zen/doctrine-override.toml` (per-project tightens custom; same Q7 C path).

### What is NOT done

- No base+overlay merging (Q2 B rejected).
- No per-section file ownership (Q2 C rejected).
- No template inheritance (`extends = "max-scope"`) — that is reserved as ADR-0056 (deferred per spec §0.2).

## Consequences

- **Operator UX direct**: single file = effective doctrine; no mental simulation of merge engine. `zen doctrine show max-scope` displays exactly what's in the TOML.
- **Diff-friendly**: `zen doctrine diff max-scope default` operates on two whole files; output is operator-readable. No "this came from base, that from overlay" annotation.
- **Validator simple**: validator runs on a single Schema struct deserialized from one TOML. No multi-stage merge resolution.
- **Reload simple**: file-watcher fires on a single TOML; atomic-swap at struct granularity. No "base changed → re-merge all overlays" cascade.
- **Migration simple**: `migrate.MigrateVNToVN+1` runs on a single Schema; no cross-file dependency to track.
- **Duplication cost contained**: ~150 LOC of duplicated TOML across three files. Trivial to maintain; trivially auditable for drift.
- **Future doctrine-inheritance is a compatible addition**: ADR-0056 reservation can introduce `extends = "max-scope"` later without breaking the self-contained shape — `extends` is purely additive (built-ins remain self-contained; operator can opt into inheritance).

## Doctrine alignment

- **Max-scope:** complete TOMLs day 1; not "ship base + 1 overlay first, add others later".
- **Build the final product:** the self-contained shape IS the final shape; no MVP-then-extend penalty.
- **No tech debt:** no merge engine to retrofit when operators ask for "show me the resolved values" (the TOML already IS the resolved values).
- **No defer:** all three built-in TOMLs ship in Phase 0 reconciliation acks + Phase A-C parser/validator/embed; no "custom doctrines come in Plan 9" deferral.
- **Tests are the floor:** every built-in TOML is parsed + validated in init-time tests (Phase C built-in package); the validator runs on each at startup.

## SOTA references

- [BurntSushi/toml strict-mode validation](https://github.com/BurntSushi/toml) — chosen TOML parser; `MetaData.Undecoded()` rejects unknown keys; strict shape validation.
- [LiteLLM Multi-Tenant Architecture](https://docs.litellm.ai/docs/proxy/multi_tenant_architecture) — per-tenant policy precedent; LiteLLM treats each tenant as a self-contained policy file.
- [LiteLLM Virtual Keys](https://docs.litellm.ai/docs/proxy/virtual_keys) — `locked: true` overrides; precedent for mirror-shape per-tenant override file.
- [Pkl: Apple's configuration language](https://pkl-lang.org/blog/introducing-pkl.html) — considered for inheritance (`extends`) primitive; rejected for Plan 8 v1 (operator must learn second config language; deferred to ADR-0056 reservation).
- [HCL / Terraform module composition](https://developer.hashicorp.com/terraform/language/modules) — alternative composition model; rejected because zen-swarm's small doctrine set (3-5) does not justify the indirection.

### Deliberate deviations from SOTA

- **Pkl/CUE rejected for Plan 8 v1** despite Apple/Google adoption: adoption cost (operator must learn second config language) outweighs benefit for solo-operator project; revisit if multi-tenant scope arrives (ADR-0057 reservation).

## Plan impact

- Plan 8 Phase 0: reconciliation acks for the ~90 default values (3 built-in TOMLs × ~30 knobs) — captured into golden corpus `tests/doctrine/reconciliation_test.go`.
- Plan 8 Phase A: `internal/doctrine/schema/v1/schema.go` Go struct mirrors the TOML shape with `tighten:"<dir>"` struct tags (per Q8 A).
- Plan 8 Phase B: `internal/doctrine/parser/parser.go` BurntSushi/toml + strict-mode validation.
- Plan 8 Phase C: `internal/doctrine/builtin/builtin.go` `//go:embed *.toml` + parse-and-validate at init; `max-scope.toml`, `default.toml`, `capa-firewall.toml` ship as physical files in same package.
- Plan 8 Phase E: `internal/doctrine/active/active.go` resolves user-authored / project-override against the built-in baseline using the same self-contained shape.
- Plan 8 Phase I: `zen doctrine init <name> --output <path>` CLI subcommand copies a built-in TOML to a target path as a starting template.

## Compliance test references

- `internal/doctrine/builtin/builtin_test.go` — asserts each of the 3 embedded TOMLs parses + validates at init (zero diagnostics).
- `tests/doctrine/reconciliation_test.go` — Phase 0 reconciliation acks for the 5 R-items + ~90 default values.
- `tests/doctrine/tighten_e2e_test.go` — E2E: built-in load + operator override + tighten validation (rejection of loosened values per inv-zen-136).
- `internal/doctrine/parser/parser_test.go` — round-trip + strict-mode rejection of unknown keys; covers the self-contained-TOML invariant (no base+overlay assumptions).
