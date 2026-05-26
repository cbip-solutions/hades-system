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

The public repository is a curated v1.0 release snapshot. It contains the
runtime, tests, release infrastructure, and root-level public documentation
needed to build and evaluate the system. Private operator history, historical
design records, and environment-specific material are not part of this
distribution.

## What It Does

- Coordinates autonomous coding work through daemon-owned queues, worktrees,
  review gates, and merge orchestration.
- Tracks doctrine, confirmations, budgets, audit events, notifications, and
  recovery paths as first-class runtime surfaces.
- Provides a terminal UX through `hades`, `zen`, `zen-swarm-ctld`, the TUI, and
  the Hermes plugin.
- Embeds Caronte for code-graph queries, blast-radius scoring, design-intent
  lookup, co-change analysis, and API-contract federation.
- Ships a public Tier 1 sidecar contract for advanced Anthropic integrations
  without bundling a private implementation.
- Publishes reproducible release gates for licensing, SBOM, chaos, DCO,
  security disclosure, and public snapshot hygiene.

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
bin/zen codegraph health
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
- `tests` - public verification fixtures and release-gate coverage.
- `configs` - curated public runtime and release-gate configuration.

## Public Documentation

- [Installation](INSTALL.md)
- [Release notes](CHANGELOG.md)
- [Contributing](CONTRIBUTING.md)
- [Security policy](SECURITY.md)

The docs tree is intentionally not shipped in this public snapshot. The root
documents above are the supported public documentation surface for v1.0.

## License

HADES system is released under the MIT License. See [LICENSE](LICENSE) and
[THIRD_PARTY_LICENSES.md](THIRD_PARTY_LICENSES.md).
