# HADES system

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Release](https://img.shields.io/github/v/release/cbip-solutions/hades-system?sort=semver)](https://github.com/cbip-solutions/hades-system/releases)
[![Go version](https://img.shields.io/github/go-mod/go-version/cbip-solutions/hades-system)](go.mod)
[![Tests](https://github.com/cbip-solutions/hades-system/actions/workflows/release-gates.yml/badge.svg)](https://github.com/cbip-solutions/hades-system/actions/workflows/release-gates.yml)
[![Brew tap](https://img.shields.io/badge/brew-cbip--solutions%2Ftap-orange)](https://github.com/cbip-solutions/homebrew-tap)

HADES system is a local-first agentic development orchestrator for serious
software work across multiple projects. It combines a long-running daemon, a
terminal CLI, a TUI, a Hermes plugin, four MCP servers, release gates, audit
trails, and Caronte, an in-process code-graph engine for impact, intent, and
contract-federation analysis.

This repository contains the HADES v1.0 source distribution: runtime code,
tests, release tooling, and user-facing documentation needed to build, run, and
evaluate the system.

## What It Does

- Coordinates autonomous coding work through daemon-owned queues, worktrees,
  review gates, and merge orchestration.
- Tracks doctrine, confirmations, budgets, audit events, notifications, and
  recovery paths as first-class runtime surfaces.
- Provides a terminal UX through `hades`, `zen`, `zen-swarm-ctld`, the TUI, and
  the Hermes plugin.
- Embeds Caronte for code-graph queries, blast-radius scoring, design-intent
  lookup, co-change analysis, and API-contract federation.
- Ships a Tier 1 sidecar contract for advanced local Anthropic integrations.
- Publishes reproducible release gates for licensing, SBOM, chaos, DCO,
  security disclosure, and artifact verification.

## Quick Start

Install from source:

```bash
git clone https://github.com/cbip-solutions/hades-system.git
cd hades-system
make build
make verify-license-compliance
```

Start the daemon:

```bash
bin/zen-swarm-ctld
```

Use the CLI:

```bash
bin/hades status
bin/zen doctor
bin/zen doctor caronte
```

See [INSTALL.md](INSTALL.md) for platform prerequisites and packaging notes.

## Main Surfaces

- `cmd/hades` - operator-facing command entry point.
- `cmd/zen` - compatibility CLI with the full command surface.
- `cmd/zen-swarm-ctld` - local daemon.
- `internal/daemon` - HTTP API, subsystem wiring, auth, and adapters.
- `internal/orchestrator` - autonomous execution, worktree, review, and merge
  control.
- `internal/caronte` - code graph, semantic indexing, risk, intent, and
  contract federation.
- `plugin/hades` - Hermes plugin commands, hooks, renderers, and interactive
  UX.
- `tests` - verification fixtures and release-gate coverage.
- `configs` - runtime and release-gate configuration examples.

## Architecture At A Glance

HADES runs as a local daemon with CLI and plugin frontends. The daemon owns
work queues, project state, audit emission, budget checks, and recovery hooks.
Execution surfaces connect through explicit adapters: MCP servers expose narrow
tool contracts, the Hermes plugin renders operator UX, and Caronte provides
in-process code graph and contract-federation queries without a sidecar process.

The repository keeps runtime code, tests, documentation, and release tooling
together so users can inspect build inputs and gate logic from one tree.

## Security Model

- Local-first control plane: sensitive operations go through localhost daemon
  APIs, stdio MCP transports, or explicit SSH targets.
- Bearer-protected daemon endpoints use constant-time token comparison.
- SSH execution uses the Go SSH client directly, requires agent credentials,
  verifies host keys with `known_hosts`, does not request a PTY, and revalidates
  commands against allowlists before execution.
- Release verification includes license checks, DCO, secret scanning, SBOM/CGO
  material, checksums, signatures, and security-advisory templates.

## Documentation

- [Installation](INSTALL.md)
- [Architecture](ARCHITECTURE.md)
- [Threat model](THREAT_MODEL.md)
- [Configuration reference](CONFIGURATION.md)
- [End-to-end examples](EXAMPLES.md)
- [Release notes](CHANGELOG.md)
- [Contributing](CONTRIBUTING.md)
- [Security policy](SECURITY.md)

Use these guides as the v1.0 handbook for installing, configuring, operating,
and contributing to HADES.

## License

HADES system is released under the MIT License. See [LICENSE](LICENSE) and
[THIRD_PARTY_LICENSES.md](THIRD_PARTY_LICENSES.md).
