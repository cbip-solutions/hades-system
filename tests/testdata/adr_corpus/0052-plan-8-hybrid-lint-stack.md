# ADR-0052 — Plan 8 hybrid lint stack: golangci-lint module plugin (Go analyzers) + ast-grep YAML rules

**Status:** Accepted
**Date:** 2026-05-03
**Decision-maker:** the operator
**Plan:** Plan 8 (Q4 B)
**Related:** ADR-0050 (per-doctrine TOMLs), ADR-0051 (hybrid enforcement model — `enforcement` metadata pattern), inv-zen-031 (subsumed by `nostore` analyzer per Q16 D), inv-zen-133 (doctrine boundary discipline; analyzer-enforced)

## Context

Plan 8 Q19 codifies the doctrine lint surface: stub detection, store-import boundary enforcement, conventional-commit shape, no-todo-implement-later, no-todo-without-adr-ref, no-fixme-without-ref, no-claude-attribution, no-panic-in-prod-paths. Three architectural shapes were considered:

- **Q4 A** — All rules as Go analyzers (uniform tool stack; one binary). Forces textual rules (e.g., `no-claude-attribution` over commit messages) into AST shape they don't fit.
- **Q4 B** — Hybrid: typed rules (require AST + import graph) → Go analyzers via golangci-lint module plugin; textual rules (grep++ with context) → ast-grep YAML rules in `lints/`.
- **Q4 C** — All rules as ast-grep YAML (textual everywhere). Forces typed rules into pattern-matching where the AST + type info is the natural fit (false positives, no import-graph reasoning).

Q4 A produces brittle Go analyzers for textual concerns (claude-attribution scanning a commit message has nothing to do with Go AST; a textual rule fits). Q4 C loses the typed rule's import-graph reasoning (`nostore` needs to follow imports transitively; ast-grep can't).

## Decision

**Q4 B**: hybrid stack with rule-type ↔ tool-type matching.

### Tier 1 — Go analyzers (typed rules, AST + import graph)

Three analyzers ship under `internal/doctrine/lint/analyzers/`:

1. **`nostub`** — detects `panic("not implemented")`, `return ErrNotImplementedPlanN`, empty method bodies on concrete types (per `feedback_no_stubs_complete_code.md` operator directive).
2. **`nostore`** — boundary enforcement: bypass / providers / dispatcher / orchestrator + doctrine packages NEVER import `internal/store` directly. Subsumes inv-zen-031 (formerly a runtime compliance test) per Q16 D. Generalized as inv-zen-133.
3. **`conventionalcommit`** — analyzes git log on PR/branch for `type(scope): subject` shape; rejects `Co-Authored-By:` and `Generated with prohibited assistant` lines (inv-zen-004). Composable with the textual `no-claude-attribution` rule below.

Distribution: `cmd/zen-doctrine-lint/main.go` registers all three via `register.Plugin()` per the golangci-lint module plugin protocol (no `.so` legacy; in-process registration). Operators run `golangci-lint run --plugins-out=./bin/zen-doctrine-lint.so` → all three analyzers fire.

### Tier 2 — ast-grep YAML rules (textual rules)

Five YAML rules ship under `lints/`:

1. **`no-todo-implement-later.yaml`** — pattern: `// TODO.*implement.*later` (matches the operator-directive footgun).
2. **`no-todo-without-adr-ref.yaml`** — pattern: `// TODO\b` without a same-comment `ADR-\d{4}` reference.
3. **`no-fixme-without-ref.yaml`** — pattern: `// FIXME\b` without a same-comment ADR or issue reference.
4. **`no-claude-attribution.yaml`** — pattern: `Co-Authored-By:.*[Cc]laude` OR `Generated with prohibited assistant` in commit messages or comments.
5. **`no-panic-in-prod-paths.yaml`** — pattern: `panic\(` in non-`_test.go` files outside `cmd/.*/main.go` initialization paths.

Each rule has a corresponding fixture under `lints/testdata/<rule>/{good,bad}/` that ast-grep CI can verify.

### Three layers (defense-in-depth per Q19 A+B+C)

The hybrid stack is invoked from three layers:

- **Layer A — `make lint`**: `scripts/lint-no-stubs.sh` + `scripts/lint-no-tech-debt.sh` invoke `ast-grep` (textual) + `bin/zen-doctrine-lint` (typed). Fails locally before commit.
- **Layer B — `.git/hooks/pre-commit`**: same scripts hooked into pre-commit (operator-installable via `make install-hooks`). Catches drift before commit lands.
- **Layer C — GitHub Actions CI**: `.github/workflows/lint.yml` runs both tools as the canonical CI gate. Final enforcement; cannot be bypassed by `--no-verify`.

### `analysistest` golden fixtures

Each Go analyzer has a corresponding `analysistest.Run` test under `internal/doctrine/lint/analysistest/<analyzer>_test.go` with `testdata/{good,bad}/` golden corpora. TDD-aligned: failing test first; analyzer added to make it pass. Per Q16 D, the analysistest pattern subsumes the prior runtime compliance test for inv-zen-031 (deleted) — the analyzer is THE enforcement mechanism; analysistest covers it; runtime test removed.

## Consequences

- **Rule-type ↔ tool-type fit**: typed rules use the typed tool; textual rules use the textual tool. Neither stack is forced into a shape it doesn't fit.
- **golangci-lint integration**: zen-swarm's existing `make lint` target picks up the module plugin transparently (no separate command for operators).
- **ast-grep YAML rules are operator-readable**: a non-Go-fluent operator can read `lints/no-claude-attribution.yaml` and understand the rule. The Go analyzers are the harder kernel; the YAML rules surface the easier rules.
- **3-layer defense aligned with Plan 5/6/7**: same defense-in-depth pattern as Plan 5's `cost_gating` (multi-stage budget enforcement) and Plan 7's `safetynet` (multi-stage drift detection). Plan 8 lints integrate naturally.
- **Per-rule fail-shape uniform**: both stacks emit `path:line:col: rule_id: message` so `make lint` output is uniform regardless of which tool surfaced the violation.
- **Future rule additions cheap**: adding a typed rule = one analyzer file + one analysistest fixture; adding a textual rule = one YAML file + one good/bad fixture pair. No new infrastructure per rule.
- **inv-zen-031 runtime test DELETED, not deprecated**: per Q16 D, the analyzer is THE enforcement; the previous runtime test is removed in the same Phase F that ships the analyzer (single source of truth; no "deprecated for one release" cruft).

## Doctrine alignment

- **Max-scope:** all 8 rules ship in Plan 8 day 1; not "ship 3 typed first, add textual later".
- **Build the final product:** the 3-layer defense-in-depth shape (make + pre-commit + CI) is the final shape; no scaffold to retrofit.
- **No defer:** Layer C (CI) is enabled in Phase F same week as Layer A (make lint). Operator can't ship a stub by accident.
- **No tech debt:** the runtime compliance test for inv-zen-031 is REPLACED (not deprecated for a release) per Q16 D; single source of truth.
- **Tests are the floor:** every analyzer + every YAML rule has a good/bad fixture in `testdata/` covered by analysistest or ast-grep CI.

## SOTA references

- [golangci-lint Module Plugin System](https://golangci-lint.run/docs/plugins/module-plugins/) — register.Plugin() pattern; in-process registration; no legacy `.so` fragility.
- [ast-grep tool comparison](https://ast-grep.github.io/advanced/tool-comparison.html) — textual rules tool with structural awareness (better than `grep -E`).
- [go-ruleguard pattern-based linting](https://github.com/quasilyte/go-ruleguard) — alternative considered; rejected because rule-DSL learning curve outweighs benefit for the small typed-rule set (3 analyzers).
- [Semgrep autofix AST-based approach](https://semgrep.dev/blog/2022/autofixing-code-with-semgrep/) — security-first comparison; Semgrep's autofix tier is heavier than zen-swarm needs (no autofix in Plan 8 v1).
- 2026 ecosystem norm: Kyverno + Cedar + LaunchDarkly + Statsig converge on hybrid (typed kernel + textual extensions). zen-swarm follows this convergence.

## Plan impact

- Plan 8 Phase F: `internal/doctrine/lint/analyzers/{nostub,nostore,conventionalcommit}/` Go packages.
- Plan 8 Phase F: `cmd/zen-doctrine-lint/main.go` golangci-lint module plugin entry.
- Plan 8 Phase F: `lints/{no-todo-implement-later,no-todo-without-adr-ref,no-fixme-without-ref,no-claude-attribution,no-panic-in-prod-paths}.yaml` ast-grep rules.
- Plan 8 Phase F: `internal/doctrine/lint/analysistest/` golden corpora per analyzer.
- Plan 8 Phase F: `scripts/{lint-no-stubs,lint-no-tech-debt}.sh` Layer A wrappers.
- Plan 8 Phase F: `.github/workflows/lint.yml` Layer C GitHub Actions invocation.
- Plan 8 Phase F: `Makefile` `lint` target wires both tools via the scripts.
- Plan 8 Phase F: DELETE `tests/compliance/inv_zen_031_test.go` (runtime test removed; analyzer subsumes per Q16 D).

## Compliance test references

- `internal/doctrine/lint/analysistest/nostub_test.go` — analysistest for `nostub` analyzer.
- `internal/doctrine/lint/analysistest/inv_zen_031_test.go` — analysistest for `nostore` (subsumes the runtime compliance test).
- `internal/doctrine/lint/analysistest/conventionalcommit_test.go` — analysistest for the commit-shape analyzer.
- `lints/testdata/<rule>/{good,bad}/` — ast-grep golden fixtures per textual rule.
- `tests/compliance/lint_zero_violations_test.go` — top-level gate: `make lint` exits 0 against the entire repo (no exemptions; per Plan 8 Phase M-4 dogfood gate).
