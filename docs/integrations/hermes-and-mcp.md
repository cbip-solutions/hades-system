# Hermes And MCP Integration

HADES exposes two integration families for agent workflows:

- Hermes plugin surfaces for conversation, skills, hooks, and operator UX.
- MCP servers for narrow, tool-shaped access to daemon capabilities.

Both are clients of the daemon. They should not own durable orchestration
state.

## Hermes Plugin

The Hermes plugin lives under `plugin/hades`. It provides:

- Slash commands.
- Project skills.
- Hook callbacks.
- Interactive prompts.
- Status and event helpers.
- Renderers for HADES-specific UX.

Install and verify:

```bash
bin/hades doctor hermes
bin/hades doctor
```

The plugin should use the daemon socket through `HADES_DAEMON_SOCKET` when
possible. If Hermes is installed but the daemon is down, plugin commands should
surface a degraded state rather than inventing daemon output.

## MCP Servers

The source distribution includes four MCP server binaries:

- `hades-mcp-research`
- `hades-mcp-budget`
- `hades-mcp-audit`
- `hades-mcp-sshexec`

Build them with:

```bash
make build
```

Verify wiring:

```bash
bin/hades doctor mcps
```

## Tool Boundaries

MCP tools should be narrow:

- Research tools perform bounded lookup and summarization.
- Budget tools inspect and enforce spend posture.
- Audit tools read or verify evidence.
- SSH tools execute only allowed remote commands with host-key verification.
- Caronte tools answer code-graph and federation questions through the daemon.

This boundary keeps powerful actions behind daemon-side authorization and audit.

## SSH MCP Safety

The SSH MCP uses agent credentials and known-host verification. Configure:

```bash
export HADES_SSH_KNOWN_HOSTS="$HOME/.ssh/known_hosts"
bin/hades doctor mcps
```

The SSH path should reject unknown hosts, missing agent credentials, and
commands outside its allowlist.

## Skill Documents

HADES skill documents are runtime instructions for the Hermes plugin. They are
not architectural records. Keep them focused on:

- Trigger condition.
- Inputs.
- Daemon or CLI calls.
- Expected output.
- Safety notes.

Long design rationale belongs in the handbook or source comments that explain
load-bearing behavior.
