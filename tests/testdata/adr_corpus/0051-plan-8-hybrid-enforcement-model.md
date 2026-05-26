# ADR-0051 — Plan 8 doctrine enforcement model: hybrid two-tier (transverse axioms hardcoded operator-only + matrix tunables per-rule metadata)

**Status:** Accepted
**Date:** 2026-05-03
**Decision-maker:** the operator
**Plan:** Plan 8 (Q3 C)
**Related:** ADR-0050 (per-doctrine TOMLs), Plan 5 Q11 C (precedent for per-rule enforcement metadata), inv-zen-100 (capa-firewall hard guard)

## Context

Plan 8 codifies ~30 doctrine-tunable knobs spanning cost-gating, merge, recovery, voting, severity, HRA cadence, etc. Plus 4 transverse axioms (`no_tech_debt`, `no_stubs`, `build_final_product`, `no_defer`) that are operator-directive load-bearing. Three architectural shapes were considered for how the doctrine system gates rule changes:

- **Q3 A** — Pure operator-gate: every doctrine value change requires an explicit operator approval (via Plan 5's amendment lifecycle). No autonomous changes anywhere.
- **Q3 B** — Pure autonomous-with-revert: every doctrine value change is autonomous (telemetry-driven via Plan 5 Reverter); operator only intervenes on revert decisions.
- **Q3 C** — Hybrid two-tier: transverse axioms HARDCODED operator-only (no enforcement field; mutation requires source-PR); matrix tunables carry per-rule `enforcement` metadata (`warn` / `enforce` / `operator-required`); telemetry-driven autonomous-revert available per-rule.

Q3 A has documented approval-fatigue failure mode (Statsig research, LaunchDarkly research): operators stop reading approvals carefully when 30+ values fire approval requests. Q3 B exposes the transverse axioms (operator-directive load-bearing) to autonomous mutation — a buggy aggregator could silently flip `no_tech_debt = true` to `false`, undermining the entire operator-directive system. Q3 C matches the asymmetry of the underlying values: 4 transverse axioms are doctrine-defining and never autonomously tunable; ~30 matrix tunables are quantitative + observable + telemetry-tunable.

## Decision

**Q3 C**: hybrid two-tier enforcement with the schema asymmetry mirrored on disk.

### Tier 1 — Transverse axioms (HARDCODED operator-only)

Four axioms ship as `[transverse]` section fields:

```toml
[transverse]
no_tech_debt = true
no_stubs = true
build_final_product = true
no_defer = true
```

NO `enforcement` field on transverse axioms. The schema validator (per Q8 A `tighten:` struct tags) explicitly REJECTS any TOML that:
- Sets a transverse axiom value not in `{true, false}` (no per-rule metadata allowed).
- Adds an `enforcement` field within the `[transverse]` section.
- Adds any field to `[transverse]` outside the 4 hardcoded axioms.

Transverse-axiom mutation requires:
1. PR to zen-swarm source (modifies the embedded `internal/doctrine/builtin/<doctrine>.toml`).
2. Operator review + merge.
3. Release cycle.

Plan 5's amendment lifecycle (Proposer / Applier / Reverter) is structurally INCAPABLE of mutating transverse-axiom values: the `Applier.ValidateTighten()` hook (per ADR-0050 Plan 5 amendment extension) calls `schema.ValidateTransverseAxiomNotMutated()` before write; any proposed change to a `[transverse]` field returns `ErrTransverseAxiomImmutable`.

This makes the operator-directive system tamper-resistant: the doctrine values that codify "max scope, no defer, no tech debt, no stubs" cannot be silently autonomously eroded.

### Tier 2 — Matrix tunables (per-rule `enforcement` metadata)

The ~30 matrix tunables (cost thresholds, retry budgets, voting weights, etc.) ship with per-rule metadata:

```toml
[cost_gating.degradation]
enforcement = "enforce"            # "warn" | "enforce" | "operator-required"
revert_category = "cost"           # per ADR-0054
revert_threshold_pct = 30
revert_window_sessions = 50
revert_cooldown_hours = 168
threshold_60 = "Mode = Degraded60"
threshold_80 = "Mode = Degraded80"
threshold_90 = "Mode = EmergencyOnly"
```

Per-rule `enforcement` semantics:
- **`warn`**: violation observed, event emitted, no action taken. Useful for telemetry collection during rollout.
- **`enforce`**: violation triggers Plan 5 amendment.proposer (cooldown-respecting) + autonomous revert if telemetry signals threshold breach. Default for the matrix.
- **`operator-required`**: violation requires operator ack before any mutation; Plan 5 Proposer drafts ADR; Applier waits on `zen doctrine ack`.

Default is `enforce`; operators can downgrade to `warn` (e.g., during rollout of a new threshold) or upgrade to `operator-required` (e.g., for high-blast-radius rules) via per-project tighten override.

### Capa-firewall hard guard (inv-zen-100 generalized)

The `capa-firewall` doctrine ships with `auto_upgrade = "none"` (per ADR-0053 two-field versioning); the schema validator rejects ANY value other than `"none"` for `auto_upgrade` when the doctrine name is `capa-firewall`. This is enforced at parse time in `parser.ParseStrict`: capa-firewall is the strict-mode doctrine; autonomous schema upgrades are forbidden by hard guard.

Generalization: inv-zen-100 (originally Plan 4: capa-firewall mode = full operator gate) is extended to: any rule under capa-firewall doctrine with telemetry-driven autonomous revert is REJECTED at validate time. capa-firewall = always operator-required at Tier 2 within its scope.

### Recursion meta-policy resolved

Concern raised during brainstorm: "if the matrix carries `enforcement` metadata, can a doctrine TOML mutate the metadata that decides whether it can be mutated?" — i.e., TOML-in-TOML loop.

Resolution: Tier 1 = code-level (Go struct + lint enforcement); Tier 2 = TOML-level. The decision of whether a rule is operator-required is per-rule-in-TOML; the decision of whether the operator-required-flag itself can be autonomously mutated is at code level (the `enforcement` field is itself a Tier 2 field; mutating it follows Tier 2 rules: revert category `recovery`, threshold + cooldown apply). No infinite recursion: the meta-policy is one level up and is itself in the TOML, not above it.

Plan 5 Q11 C precedent generalized: Plan 5's per-rule cooldown metadata pattern is the same shape; Plan 8 extends it from cooldown-only to a richer `enforcement` enum.

## Consequences

- **Operator-directive system tamper-resistant**: the 4 transverse axioms codify the load-bearing operator directive; structurally impossible to autonomously mutate. Plan 5 Applier rejects at validate time; Plan 8 schema rejects at parse time.
- **Approval fatigue avoided**: ~30 matrix tunables default to `enforce` (autonomous-with-revert), not `operator-required`. Operator only intervenes on the rules they explicitly mark `operator-required` (typically: `capa-firewall` + a small subset for high-stakes projects).
- **Per-rule observability uniform**: every Tier 2 rule emits the same shape of `revert_category` + `revert_threshold_pct` + `revert_window_sessions` + `revert_cooldown_hours`; the 3 aggregators (per ADR-0054) consume the metadata uniformly.
- **Tier promotion cheap**: an operator who decides "this rule needs gate me from autonomous changes" runs `zen doctrine override edit` on the per-project TOML and changes `enforcement = "enforce"` to `enforcement = "operator-required"`. No code change.
- **Capa-firewall is THE strict mode**: by construction, capa-firewall projects can never have a rule autonomously mutated; the Pulido-strict doctrine remains operator-deterministic.
- **Lint enforcement**: the parser rejects malformed TOMLs (e.g., transverse-axiom with `enforcement` field) at startup; operators can never accidentally craft a self-bypassing TOML.

## Doctrine alignment

- **Max-scope:** all 3 enforcement levels ship day 1 (`warn` / `enforce` / `operator-required`); not "ship enforce-only first, add operator-required later".
- **Build the final product:** the schema asymmetry (transverse hardcoded, matrix per-rule) IS the final shape; no scaffold to retrofit.
- **No tech debt:** transverse-axiom immutability enforced day 1 (parse-time + Applier-time); no "autonomous mutation guard added next release".
- **No defer:** capa-firewall hard guard ships in Plan 8 same phase as the schema (Phase A); not "Plan 9 ships the strict-mode validator".
- **Tests are the floor:** transverse-immutability test (parse a TOML with `enforcement = "warn"` on a transverse axiom → reject); capa-firewall hard-guard test (parse a TOML with `auto_upgrade = "patch"` under capa-firewall → reject); per-rule enforcement coverage in compliance tests.

## SOTA references

- [OPA Gatekeeper for Kubernetes Admission Control](https://www.openpolicyagent.org/docs/kubernetes) — per-rule enforcement metadata precedent; OPA's `enforcementAction` per constraint = `dryrun | warn | deny`. zen-swarm's `warn | enforce | operator-required` is convergent shape.
- [Admission Controllers in Kubernetes: OPA, Kyverno, Azure Policy](https://dev.to/hkhelil/admission-controllers-in-kubernetes-opa-gatekeeper-kyverno-and-azure-policy-add-on-for-aks-which-one-wins-237d) — hybrid enforcement convergence; 2026 ecosystem norm is hybrid (each rule declares its own gate vs uniform per-bundle).
- [LaunchDarkly Feature Flag Hierarchy](https://launchdarkly.com/docs/guides/flags/flag-hierarchy) — operator-approval-required vs autonomous-with-revert precedent; same asymmetric pattern between high-stakes flags + low-stakes flags.
- [Statsig Auto-Rollback / Predictive Pulse](https://www.flagsmith.com/blog/statsig-alternatives) — telemetry-driven autonomous revert on threshold breach; Plan 8's Tier 2 `enforce` mode adopts the pattern (per ADR-0054).
- [Cedar Policy Language](https://docs.cedarpolicy.com/) — policy declares its own gate pattern; convergent with per-rule `enforcement` metadata.
- 2026 ecosystem convergence: Kyverno + Cedar + LaunchDarkly + Statsig all converge on per-rule enforcement-mode metadata. zen-swarm follows the convergence.

### Deliberate deviations from SOTA

- **Tier 1 = HARDCODED (not just metadata-marked)**: OPA + Kyverno permit metadata that effectively makes a rule "operator-only", but the metadata itself is mutable. zen-swarm hardcodes the 4 transverse axioms at code level; no metadata can flip them. Justification: the transverse axioms codify operator-directive load-bearing rules; metadata-as-Tier-1 leaves a tamper surface that defeats the purpose.
- **No "dryrun" mode**: zen-swarm's `warn` mode is observation-only (no mutation, event-only); OPA's `dryrun` is a separate state. Simplification — `warn` is sufficient for Plan 8's rollout pattern.

## Plan impact

- Plan 8 Phase A: `internal/doctrine/schema/v1/transverse.go` declares the 4 axioms as Go struct fields with `tighten:"truth"` tags + parse-time enforcement (parser rejects extra fields).
- Plan 8 Phase A: `internal/doctrine/schema/v1/schema.go` matrix sections declare per-rule `enforcement` field with `tighten:"rank:warn,enforce,operator-required"` tag.
- Plan 8 Phase B: `parser.ParseStrict` rejects transverse-section TOMLs that include `enforcement` or unknown fields.
- Plan 8 Phase C: `builtin/capa-firewall.toml` ships with all matrix sections at `enforcement = "operator-required"`; hard guard at parse time.
- Plan 8 Phase H: cross-branch additive on `internal/orchestrator/amendment/applier.go` calls `schema.ValidateTransverseAxiomNotMutated()` before any write.
- Plan 8 Phase L: integration tests `tests/integration/doctrine_transverse_immutable_test.go` + `tests/integration/doctrine_capa_firewall_hard_guard_test.go`.

## Compliance test references

- `internal/doctrine/schema/v1/transverse_test.go` — parse-time rejection of malformed transverse sections.
- `internal/doctrine/schema/v1/validate_test.go` — `ValidateTransverseAxiomNotMutated()` rejects any pre→post diff on `[transverse]`.
- `tests/compliance/inv_zen_100_test.go` — capa-firewall hard guard (auto_upgrade != "none" rejected at parse).
- `tests/compliance/inv_zen_100_extended_test.go` — capa-firewall extended hard guard (rule with `enforcement != "operator-required"` rejected for capa-firewall doctrine).
- `tests/integration/doctrine_amendment_transverse_immutable_test.go` — Plan 5 Applier path rejects a proposed amendment to a `[transverse]` field with `ErrTransverseAxiomImmutable`.
