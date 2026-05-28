# Changelog

All notable changes to HADES system are documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
The first tagged source release is `v1.0.0`.

## [v1.0.3] - 2026-05-29

### Fixed

- First-run Linux/source onboarding now reports missing Hermes Agent as an
  onboarding preflight failure instead of misclassifying it as an MCP spawn
  failure.
- Public installation, first-run, troubleshooting, and Hermes/MCP docs now
  explicitly cover `hermes --version`, plugin linking, and `hades doctor
  hermes`.
- The global wizard prompt now describes the actual plugin-linking action after
  Hermes preflight instead of promising an unreachable Hermes Agent install
  step.

### Changed

- Daemon and wizard recovery hints now point at public HADES docs, socket names,
  and commands.
- Homebrew caveats now include a Hermes Agent verification step before plugin
  linking.

## [v1.0.2] - 2026-05-28

### Fixed

- Linux/source provider credentials can be supplied through `HADES_KEYCHAIN_*`
  environment aliases before daemon start.

## [v1.0.1] - 2026-05-27

### Fixed

- Source distribution now exposes only the documented sidecar contract and
  omits implementation material that is not part of the supported public API.

## [v1.0.0] - 2026-05-25

### Added

- HADES system v1.0.0 release under the MIT License.
- Local daemon, CLI, TUI, Hermes plugin, and four MCP servers for agentic
  development flows.
- Autonomous orchestration primitives: task queues, worktree management,
  hierarchical review, merge evaluation, confirmation controls, budget controls,
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
- DCO-based contribution guidelines and artifact-verification notes.

### Changed

- Repository identity is `github.com/cbip-solutions/hades-system`.
- Documentation is organized at the repository root for users and contributors.
- Optional advanced Anthropic integration is exposed as a sidecar contract
  rather than an in-process backend.

### Security

- Added secret scanning expectations for published material.
- Added identity and local-path checks for published material.
- Documented exported Go APIs and public security expectations.

### Notes

- The tagged source release line begins at `v1.0.0`.
- The project remains single-maintainer and best-effort for community support;
  see [CONTRIBUTING.md](CONTRIBUTING.md).
