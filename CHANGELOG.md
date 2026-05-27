# Changelog

All notable changes to HADES system are documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
The first tagged source release is `v1.0.0`.

## [v1.0.0] - 2026-05-25

### Added

- HADES system v1.0.0 release under the MIT License.
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
- Root-level documentation for installation, contribution, release notes,
  license posture, and security reporting.
- Security policy using GitHub Security Advisories as the primary private
  vulnerability reporting channel.
- DCO-based contribution workflow and release rulesets.

### Changed

- Repository identity is `github.com/cbip-solutions/hades-system`.
- Documentation is organized at the repository root for users and contributors.
- Optional advanced Anthropic integration is exposed as a sidecar contract
  rather than an in-process backend.

### Security

- Added secret scanning for release material.
- Added identity and local-path checks for release material.
- Added Go documentation gates for exported public APIs.

### Notes

- The tagged source release line begins at `v1.0.0`.
- The project remains single-maintainer and best-effort for community support;
  see [CONTRIBUTING.md](CONTRIBUTING.md).
