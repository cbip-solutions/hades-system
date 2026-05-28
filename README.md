# HADES system

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Release](https://img.shields.io/github/v/release/cbip-solutions/hades-system?sort=semver)](https://github.com/cbip-solutions/hades-system/releases)
[![Go version](https://img.shields.io/github/go-mod/go-version/cbip-solutions/hades-system)](go.mod)
[![Brew tap](https://img.shields.io/badge/brew-cbip--solutions%2Ftap-orange)](https://github.com/cbip-solutions/homebrew-tap)

HADES system is a local-first agentic development orchestrator for serious
software work across multiple projects. It combines a long-running daemon, a
terminal CLI, a TUI, a Hermes plugin, four MCP servers, audit
trails, and Caronte, an in-process code-graph engine for impact, intent, and
contract-federation analysis.

This repository contains the HADES v1.0 source distribution: runtime code,
build files, and user-facing documentation needed to build, run, and
evaluate the system.

## What It Does

- Coordinates autonomous coding work through daemon-owned queues, worktrees,
  review controls, and merge orchestration.
- Tracks doctrine, confirmations, budgets, audit events, notifications, and
  recovery paths as first-class runtime surfaces.
- Provides a terminal UX through `hades`, `hades-ctld`, the TUI, and
  the Hermes plugin.
- Embeds Caronte for code-graph queries, blast-radius scoring, design-intent
  lookup, co-change analysis, and API-contract federation.
- Ships a Tier 1 sidecar contract for advanced local Anthropic integrations.
- Includes source-build, container, Homebrew, and security-disclosure surfaces.

## Quick Start

Install with Homebrew:

```bash
brew tap cbip-solutions/tap
brew install hades
hermes --version
mkdir -p ~/.hermes/plugins
ln -sfn "$(brew --prefix hades)/share/hades/hades" ~/.hermes/plugins/hades
hades-ctld --version
hades --version
brew services start cbip-solutions/tap/hades
hades status
hades doctor
hades doctor hermes
hades doctor mcps
hades providers list
hades dashboard
```

Build from source:

```bash
git clone https://github.com/cbip-solutions/hades-system.git
cd hades-system
hermes --version
make build
make test
make plugin-install
```

Start the daemon:

```bash
bin/hades-ctld
```

Use the CLI:

```bash
bin/hades status
bin/hades doctor
bin/hades providers list
bin/hades dashboard
bin/hades doctor caronte
```

See [INSTALL.md](INSTALL.md) for platform prerequisites and packaging notes.
For the guided first run, see [First Five Minutes](docs/operations/first-five-minutes.md).
For Hermes plugin wiring, see [Hermes and MCP integration](docs/integrations/hermes-and-mcp.md).

## Main Surfaces

- `cmd/hades` - operator-facing command entry point.
- `cmd/hades-ctld` - local daemon.
- `internal/daemon` - HTTP API, subsystem wiring, auth, and adapters.
- `internal/orchestrator` - autonomous execution, worktree, review, and merge
  control.
- `internal/caronte` - code graph, semantic indexing, risk, intent, and
  contract federation.
- `plugin/hades` - Hermes plugin commands, hooks, renderers, and interactive
  UX.
- `configs` - runtime configuration examples.

## Architecture At A Glance

HADES runs as a local daemon with CLI and plugin frontends. The daemon owns
work queues, project state, audit emission, budget checks, and recovery hooks.
Execution surfaces connect through explicit adapters: MCP servers expose narrow
tool contracts, the Hermes plugin renders operator UX, and Caronte provides
in-process code graph and contract-federation queries without a sidecar process.

The repository keeps runtime code, documentation, and build inputs together so
users can inspect and build the source tree directly.

## Security Model

- Local-first control plane: sensitive operations go through localhost daemon
  APIs, stdio MCP transports, or explicit SSH targets.
- Bearer-protected daemon endpoints use constant-time token comparison.
- SSH execution uses the Go SSH client directly, requires agent credentials,
  verifies host keys with `known_hosts`, does not request a PTY, and revalidates
  commands against allowlists before execution.
- Published artifacts should be verified with the checksums, attestations, and
  signatures attached to the corresponding release.

## Documentation

- [Installation](INSTALL.md)
- [Architecture](ARCHITECTURE.md)
- [Threat model](THREAT_MODEL.md)
- [Configuration reference](CONFIGURATION.md)
- [End-to-end examples](EXAMPLES.md)
- [First five minutes](docs/operations/first-five-minutes.md)
- [Hermes and MCP integration](docs/integrations/hermes-and-mcp.md)
- [Troubleshooting](docs/operations/troubleshooting.md)
- [Subsystem handbook](docs/README.md)
- [Release notes](CHANGELOG.md)
- [Contributing](CONTRIBUTING.md)
- [Security policy](SECURITY.md)

Use the root guides for first contact and the handbook for deeper subsystem
operation, including Caronte, HRA, orchestration, Contract Federation, daemon
operations, Hermes, MCP, CLI, and HTTP API surfaces.

## License

HADES system is released under the MIT License. See [LICENSE](LICENSE) and
[THIRD_PARTY_LICENSES.md](THIRD_PARTY_LICENSES.md).
