# HADES Handbook

This handbook explains the runtime surfaces that sit behind the root quick-start
documents. Use it when you want to understand how HADES is assembled, how to
operate it, and which command surface to use for a given workflow.

## Reading Paths

For a first installation, read:

- [Installation](../INSTALL.md)
- [Configuration reference](../CONFIGURATION.md)
- [End-to-end examples](../EXAMPLES.md)

For architecture and extension work, read:

- [Caronte code graph](architecture/caronte.md)
- [Hierarchical Review Architecture](architecture/hra.md)
- [Autonomous orchestration](architecture/orchestration.md)
- [Contract Federation](architecture/contract-federation.md)

For day-to-day operation, read:

- [Daemon operations](operations/daemon.md)
- [Audit, budget, and recovery](operations/audit-budget-recovery.md)
- [Hermes and MCP integration](integrations/hermes-and-mcp.md)

For exact command and transport shapes, read:

- [CLI reference](reference/cli.md)
- [HTTP API reference](reference/http-api.md)
- [End-to-end workflows](examples/end-to-end.md)

## System Shape

HADES is a local-first control plane for agentic development. A long-running
daemon owns durable state, routing, audit events, queue ownership, worktree
leases, review gates, budget posture, recovery decisions, and code-graph
queries. Short-lived clients then render or request work through explicit
interfaces:

- `hades` for terminal workflows.
- `hades-ctld` for the daemon.
- TUI views for live status and review queues.
- Hermes plugin skills and commands for conversation-driven workflows.
- MCP servers for narrow tool contracts.
- Caronte for code understanding, intent lookup, impact analysis, and contract
  federation.

The design goal is explicit authority. Frontends request work; the daemon owns
state transitions. Background systems may degrade, but the degradation should
be visible through doctor, health, and audit surfaces.

## Operating Principles

- Prefer daemon-owned state over ad hoc shell memory.
- Run Caronte impact and intent checks before high-blast-radius changes.
- Treat HRA attention as a control surface, not a notification stream.
- Keep provider credentials outside config files. Use `HADES_KEYCHAIN_*` env
  aliases on Linux/source installs, or macOS Keychain via `hades providers
  rotate <name>`.
- Use workspace federation before changing API contracts across repositories.
- Check audit and budget state when a workflow pauses.
- Verify published artifacts with checksums, signatures, and attestations when
  consuming release builds.
