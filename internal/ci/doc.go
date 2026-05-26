// SPDX-License-Identifier: MIT
// Package ci provides the GitHub Actions CI status lookup + classification
// + rolling-window evaluation for Plan 15 Phase A A-5 (30-CI-green gate).
//
// Per spec §1.4 C6 fix: this library is delivered COMPLETE in Phase A
// (NOT deferred to Phase G). Phase G refines only ancillary surfaces
// (Rulesets + flake quarantine governance + cross-workflow freshness).
//
// Rolling window semantics (amendment §2.5 D-5; spec §7.3):
//
//   - Lookback: 50 commits on main.
//   - Classification per commit:
//   - success                  → counts as green.
//   - failure + infra_pattern  → bucket "infra"; excluded from denominator.
//   - failure + flake quarantine match → bucket "flake"; excluded.
//   - otherwise failure        → bucket "real"; counted.
//   - Gate passes if: success_count / (success_count + real_fail) ≥ 0.90
//     AND real_fail ≤ 2.
//   - Minimum sample: 30 classified commits (so rolling window meaningful
//     pre-1.0).
//
// Why this avoids "permanent-red trap" (per HANDOFF.md context):
//
//   - GHA billing-block commits bucket "infra"; gate stays gateable.
//   - Atlassian Flakinator + Trunk.io quarantine pattern empirically
//     validated.
//   - Matches DORA 2025 elite threshold (CFR ≤ 5%; here ≤ 10% real fail
//     rate).
//
// Surface (per master plan §2.6 + Phase A plan-file A-5):
//
//   - ClassifierVersion (string const) — bumped on any classification-rule
//     change; cache entries embed this version; classifier rejects
//     stale-version entries.
//   - CommitStatus (struct) — per-commit GH Actions CI status.
//   - RollingWindow (struct) — 50/45/2 thresholds.
//   - DefaultRollingWindow() RollingWindow — canonical constructor.
//   - (w RollingWindow) Evaluate(commits) (bool, string) — gate evaluator.
//   - Classify(commit, flakeQuarantine) CommitStatus — bucket assigner.
//   - FetchLastN(ctx, owner, repo, branch, n) ([]CommitStatus, error) —
//     GH API wrapper with per-SHA cache.
//   - LoadFlakeQuarantine(path) (*FlakeQuarantine, error) — quarantine
//     loader with 14d expiry enforcement (consumed by Phase G G-6).
//
// Cache path: ~/.cache/hades/ci/{sha}.json (per master §2.6 + decisión 4
// identity rename; NOT ~/.cache/zen-swarm/ which was the pre-rename
// location).
package ci
