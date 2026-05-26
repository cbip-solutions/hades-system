# Changelog

All notable public changes to HADES system are documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Public releases start at `v1.0.0`; earlier private development history is
preserved outside this curated public distribution.

## [v1.0.0] - 2026-05-25

### Added

- HADES system public release under the MIT License.
- Local daemon, CLI, TUI, Hermes plugin, and four MCP servers for agentic
  development workflows.
- Autonomous orchestration primitives: task queues, worktree management,
  hierarchical review, merge evaluation, confirmation gates, budget controls,
  notification routing, and recovery paths.
- Caronte, the in-daemon code-graph engine for symbol search, impact analysis,
  blast-radius scoring, implementation lookup, co-change analysis, and
  design-intent queries.
- Contract Federation on top of Caronte for endpoint extraction, consumer
  discovery, breaking-change classification, workspace policy, and coordinated
  cross-repository fix recommendations.
- Root-level public documentation for installation, contribution, release
  notes, license posture, and security reporting.
- Security policy using GitHub Security Advisories as the primary private
  vulnerability reporting channel.
- DCO-based contribution workflow and public release rulesets.

### Changed

- Public identity is `github.com/cbip-solutions/hades-system`.
- Public distribution is produced from an allowlisted snapshot, with private
  operator history and environment-specific material excluded.
- Public documentation is curated at the repository root for users and
  contributors rather than shipped as a verbatim development log.
- Optional advanced Anthropic integration is exposed as a sidecar contract
  rather than an in-tree private implementation.

### Security

- Added gitleaks scanning for the public snapshot.
- Added privacy and identity hygiene checks for public release material.
- Added comment-hygiene and Go documentation gates to reduce task-context rot in
  public code.

### Notes

- The authoritative public source begins at `v1.0.0`.
- The project remains single-maintainer and best-effort for community support;
  see [CONTRIBUTING.md](CONTRIBUTING.md).
