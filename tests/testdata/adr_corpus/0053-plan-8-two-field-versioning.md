# ADR-0053 — Plan 8 doctrine versioning: two-field schema_version + doctrine_version with auto-upgrade per-project policy

**Status:** Accepted
**Date:** 2026-05-03
**Decision-maker:** the operator
**Plan:** Plan 8 (Q5 B)
**Related:** ADR-0050 (per-doctrine TOMLs carry both fields), ADR-0051 (capa-firewall hard guard on auto_upgrade), Q15 A in-memory schema migration, inv-zen-100 (capa-firewall hard guard generalized), inv-zen-142 (schema_version monotonically non-decreasing on disk)

## Context

Plan 8 ships doctrine TOMLs that have two distinct evolution axes:

- **File shape**: structural schema of the TOML — sections present, fields per section, field types. Bumps when zen-swarm releases add/remove/rename a section or field.
- **Rule content**: the values within an unchanged shape — a threshold dialed from 60% to 70%, a retry count from 3 to 5. Bumps when operator-policy or empirical telemetry tunes the value.

Three architectural shapes were considered:

- **Q5 A** — Single `version` field combining both axes (e.g., SemVer). Operator gets one number to track but can't distinguish "schema upgraded" from "values tuned".
- **Q5 B** — Two-field versioning: `schema_version` (file shape SemVer) + `doctrine_version` (rule content SemVer). Plus per-project `auto_upgrade = "patch" | "minor" | "major" | "none"` policy.
- **Q5 C** — Channel-based experimental doctrine (e.g., Gateway API channel pattern: `experimental` / `standard`). Multi-tenant pattern; rejected as overengineering for solo-operator zen-swarm.

Q5 A has a documented alert-fatigue OR data-loss anti-pattern (research): operators either react to every version bump (alert fatigue) or stop reading version changes (data loss when a real schema migration is needed). Two-field versioning is the universal convention in policy/config systems (OPA bundles, K8s CRD apiVersion, OpenTelemetry file_format).

## Decision

**Q5 B**: two-field versioning with auto-upgrade per-project policy.

### Field shape

Every doctrine TOML carries:

```toml
schema_version = "1.0"        # file shape; SemVer
doctrine_version = "2.3.1"    # rule content; SemVer
```

### `schema_version` — file shape

Bumps when zen-swarm releases:
- Add a section (e.g., new `[recovery]` block) → minor bump (`1.0` → `1.1`).
- Add an optional field within an existing section → minor bump.
- Add a required field within an existing section → major bump (`1.x` → `2.0`).
- Remove or rename a field → major bump.
- Change a field's type (e.g., int → string) → major bump.

Plan 8 v0.8.0 ships at `schema_version = "1.0"` (inaugural). The migration framework (per Q15 A in-memory only schema migration) handles VN → VN+1 transformations when the schema bumps in some future plan. Plan 8 v0.8.0 chain is empty (no historical schema versions).

### `doctrine_version` — rule content

Bumps when operator-policy or empirical telemetry adjusts values within an unchanged shape:
- Adjust a single threshold or weight → patch bump (`2.3.1` → `2.3.2`).
- Reshape multiple values together (a coordinated tuning campaign) → minor bump.
- Operator-driven philosophy shift (e.g., "max-scope" doctrine relaxes its retry budget) → major bump (rare).

`doctrine_version` is independent of `schema_version`: a release that only tunes built-in default values bumps `doctrine_version` without touching `schema_version`. Conversely, a release that adds a new section bumps `schema_version` without necessarily bumping `doctrine_version` (the section ships with defaults).

### Per-project `auto_upgrade` policy

Each project pins a doctrine + version + auto-upgrade policy in `zenswarm.toml [doctrine]`:

```toml
[doctrine]
name = "max-scope"
doctrine_version = "2.3.1"
schema_version = "1.0"
auto_upgrade = "patch"            # "patch" | "minor" | "major" | "none"
```

`auto_upgrade` semantics:
- **`patch`** (default for non-strict projects): autonomous bumps OK for `doctrine_version` patch increments (e.g., `2.3.1` → `2.3.2`). Plan 5 amendment.proposer auto-applies; no operator intervention.
- **`minor`**: `doctrine_version` minor or patch bumps require Plan 5 Qx-4 amendment proposal (operator sees ADR draft + ack flow).
- **`major`**: any `doctrine_version` change requires operator approval; major schema-version bumps always require operator-required.
- **`none`**: no autonomous changes; operator runs `zen doctrine ack` for every bump explicitly.

### Capa-firewall hard guard (inv-zen-100 generalized; per ADR-0051)

The capa-firewall doctrine REJECTS any `auto_upgrade` value other than `"none"`. Schema validator enforces this at parse time: a TOML naming `capa-firewall` doctrine with `auto_upgrade = "patch"` produces `ErrCapaFirewallAutoUpgradeForbidden`.

Justification: capa-firewall is the strict-mode doctrine for Pulido-strict projects (per the Research-AI/Pulido-Tesis-1 capa-firewall reflex pattern); any autonomous schema upgrade undermines the operator-deterministic guarantee. The hard guard makes capa-firewall structurally incompatible with autonomous upgrades.

This is `inv-zen-100` generalized: the original Plan 4 invariant ("capa-firewall mode = full operator gate") is extended to: capa-firewall = `auto_upgrade != "none"` is REJECTED. Operator forced to opt out of autonomous upgrades for the Pulido-strict layer.

### N + N-1 deprecation policy

Daemon supports the current `schema_version` + previous `schema_version` via converters in `internal/doctrine/migrate/`. Per Q15 A in-memory only schema migration:
- `schema_version == current` → load directly.
- `schema_version == current - 1` → in-memory migration via chain; persistent file untouched.
- Older → refuse load + suggest `zen doctrine migrate <path>`.

Matches Kubernetes deprecation policy (see ADR-0052's SOTA references).

### inv-zen-142 — schema_version monotonically non-decreasing on disk

The CLI's `zen doctrine migrate <path> --confirm` REFUSES to write a TOML with `schema_version` lower than the file's current value. Defense-in-depth against operator typo. Returns `ErrSchemaVersionDowngradeRejected`. Implemented in `internal/doctrine/cli/write.go`.

This is orthogonal to the auto-upgrade per-project policy: `auto_upgrade` controls whether autonomous changes happen; inv-zen-142 prevents a manual rewrite from accidentally regressing the schema.

## Consequences

- **Operator distinguishes "schema changed" from "values changed"**: `zen doctrine status` shows both fields; the operator-visible diff after a version bump tells them whether the file shape moved (re-read the structure) or just values shifted (re-read the threshold).
- **Per-project autonomy policy**: each project pins its own auto-upgrade tier; high-stakes Pulido-strict project sets `auto_upgrade = "none"`; low-stakes experiment sets `auto_upgrade = "patch"`. No global daemon flag to coordinate.
- **Capa-firewall is structurally autonomous-free**: hard guard at parse time; the strict-mode doctrine cannot be silently autonomously upgraded.
- **Migration chain shape stable**: schema bumps map onto migration chain entries (per Q15 A); the chain's signature (`func(*VN) (*VN+1, error)`) does not change as new schema versions land.
- **Doctrine evolution observable**: every bump emits a `DoctrineVersionBumped` or `DoctrineSchemaUpgraded` event; queryable via `zen doctrine history`.
- **Convergent with ecosystem**: matches OPA bundles + K8s CRD apiVersion + OpenTelemetry file_format pattern. Operators familiar with Kubernetes + OPA carry mental models forward.
- **Deprecation predictable**: N + N-1 means an operator's TOML works for at least one zen-swarm release after the schema bump; no surprise breakage on upgrade.

## Doctrine alignment

- **Max-scope:** both fields ship day 1 (not "ship doctrine_version first, add schema_version later"); 4 auto-upgrade tiers ship complete.
- **Build the final product:** the two-field shape + auto-upgrade policy IS the final shape; no MVP-then-extend.
- **No tech debt:** capa-firewall hard guard at parse time (not "we'll add it Plan 9").
- **No defer:** N + N-1 deprecation policy enforced day 1; not "let's start with N-only and add N-1 when needed".
- **Tests are the floor:** parse-time tests cover field-shape rejection; integration tests cover migration chain; capa-firewall hard-guard test covers inv-zen-100.

## SOTA references

- [Kubernetes CRD Versioning](https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definition-versioning/) — apiVersion + custom-resource-version separation; same shape as zen-swarm's two-field versioning.
- [OPA Policy Bundles](https://www.openpolicyagent.org/docs/management-bundles) — bundle revision (rule content) + manifest schema (file shape) two-axis precedent.
- [OpenTelemetry Versioning and Stability](https://opentelemetry.io/docs/specs/otel/versioning-and-stability/) — `file_format` vs schema separation; convergent shape.
- [OpenTelemetry Schema File Format 1.1.0](https://opentelemetry.io/docs/specs/otel/schemas/file_format_v1.1.0/) — explicit schema-transformation chain (precedent for Q15 A migration shape).
- [Semantic Versioning 2.0.0](https://semver.org/) — version field semantics for `doctrine_version` (operator-tuned) and `schema_version` (release-tuned).
- [Kubernetes Deprecation Policy](https://kubernetes.io/docs/reference/using-api/deprecation-policy/) — N + N-1 support pattern; zen-swarm matches.
- [Vertex AI Model Registry Governance](https://oneuptime.com/blog/post/2026-02-17-how-to-set-up-model-governance-and-approval-workflows-in-vertex-ai-model-registry/view) — ML governance precedent for per-stage approval workflows.
- [SageMaker Model Governance](https://docs.aws.amazon.com/sagemaker/latest/dg/governance.html) — model approval workflows; same shape as auto_upgrade per-project policy.

### Deliberate deviations from SOTA

- **Channel-based experimental doctrine rejected** (Gateway API pattern): zen-swarm is solo-operator; experimental channel adds multi-tenant complexity without value. Deferred to ADR-0058 reservation.
- **No mid-version compatibility shim** (vs some K8s controllers' apiVersion negotiation): zen-swarm is daemon-internal; the migration chain IS the compatibility layer. Simpler.

## Plan impact

- Plan 8 Phase A: `internal/doctrine/schema/v1/schema.go` declares both fields with `tighten:"truth"` (immutable through tighten) tags; parse-time required.
- Plan 8 Phase B: `parser.ParseStrict` validates SemVer shape on both fields; rejects malformed.
- Plan 8 Phase C: `internal/doctrine/builtin/{max-scope,default,capa-firewall}.toml` ship with `schema_version = "1.0"` and `doctrine_version = "X.Y.Z"`; capa-firewall ships with `auto_upgrade = "none"` hardcoded.
- Plan 8 Phase E: `internal/doctrine/active/active.go` exposes both fields in the resolved Active() result; `zen doctrine status` reads them.
- Plan 8 Phase H: cross-branch additive on `internal/orchestrator/amendment/applier.go` consults `auto_upgrade` policy before applying autonomous changes.
- Plan 8 Phase I: `zen doctrine migrate <path> --confirm` enforces inv-zen-142 (no schema_version downgrade) in `internal/doctrine/cli/write.go`.
- Future Plan: when `schema_version` bumps to "2.0", a `migrate.MigrateV1ToV2` function lands in `internal/doctrine/migrate/v1_to_v2.go` (placeholder ships in Plan 8 v0.8.0).

## Compliance test references

- `internal/doctrine/parser/parser_test.go::TestSchemaVersionRequired` — TOML missing `schema_version` rejected at parse.
- `internal/doctrine/parser/parser_test.go::TestDoctrineVersionRequired` — TOML missing `doctrine_version` rejected at parse.
- `internal/doctrine/schema/v1/validate_test.go::TestSemVerShape` — both fields must be valid SemVer strings.
- `tests/compliance/inv_zen_100_test.go` — capa-firewall hard guard: TOML naming `capa-firewall` with `auto_upgrade != "none"` rejected.
- `tests/compliance/inv_zen_142_test.go` — `zen doctrine migrate` rejects a TOML with `schema_version` below the file's current value.
- `tests/integration/doctrine_auto_upgrade_test.go` — Plan 5 Applier honors per-project `auto_upgrade` tier (patch / minor / major / none).
