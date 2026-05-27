# Daemon Operations

`hades-ctld` is the local daemon that owns durable HADES state. It should be
the authority for queues, worktrees, provider routing, budget posture, audit
events, Caronte state, federation state, and recovery loops.

## Start And Stop

Foreground start:

```bash
bin/hades-ctld
```

CLI-managed start and status:

```bash
bin/hades daemon start
bin/hades daemon status
bin/hades status
```

On macOS, the daemon can be installed as a per-user launch agent when the
packaging surface supports it:

```bash
bin/hades daemon install
bin/hades daemon stop
bin/hades daemon uninstall
```

## Local Transport

The default socket is:

```text
/tmp/hades-system.sock
```

Relevant environment variables:

- `HADES_DAEMON_UDS`
- `HADES_SYSTEM_UDS`
- `HADES_DAEMON_SOCKET`
- `HADES_STATE_DIR`

Prefer the CLI unless you are debugging a transport issue. The CLI handles
socket selection, request formatting, and error classification.

## Doctor Workflow

Run doctor after installation, after configuration changes, and when a surface
returns degraded state.

```bash
bin/hades doctor
bin/hades doctor caronte
bin/hades doctor hermes
bin/hades doctor mcps
bin/hades doctor sidecars
```

Doctor output should identify missing binaries, unreachable sockets, stale
indexes, missing provider credentials, sidecar failures, and integration drift.

## State Directories

HADES separates configuration from state:

- Config: `$XDG_CONFIG_HOME/hades-system` or `~/.config/hades-system`.
- State: `$XDG_STATE_HOME/hades-system` or `~/.local/state/hades-system`.
- Project overrides: `<repo>/.hades-system.toml`.
- Credentials: operating-system credential store, referenced by key name.

Do not place API keys in TOML files.

## Logs And Events

Daemon surfaces should emit events for meaningful transitions:

- Provider registration and failure.
- Budget pause and resume.
- Worktree lease and release.
- HRA attention changes.
- Confirmation request and decision.
- Recovery attempt and result.
- Federation policy change.
- Artifact verification result.

Use audit commands when the question is "what happened and why?"

```bash
bin/hades audit events --limit 20
bin/hades audit types
```

## Troubleshooting

| Symptom | First Check |
| --- | --- |
| CLI cannot connect | `bin/hades daemon status`, socket path environment. |
| Caronte returns empty output | `bin/hades doctor caronte`, then reindex. |
| Provider unavailable | `bin/hades providers list`, credential-store key, endpoint health. |
| Federation query returns not found | Workspace registration and indexing freshness. |
| Sidecar unavailable | `bin/hades doctor sidecars`, loopback URL, `required` flag. |
| Workflow paused | HRA queue, budget cap status, confirmation state, audit events. |

When a check is degraded but unrelated surfaces still work, treat that as a
normal partial-failure state rather than a daemon-wide failure.
