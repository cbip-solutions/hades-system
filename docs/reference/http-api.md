# HTTP API Reference

The daemon exposes local HTTP routes behind the CLI, TUI, Hermes plugin, and
MCP gateway. Prefer the CLI for manual use. Use HTTP when writing an integration
that needs structured daemon responses.

## Transport

The daemon is expected to bind to local transports. The default Unix-domain
socket is:

```text
/tmp/hades-system.sock
```

Example:

```bash
curl --unix-socket /tmp/hades-system.sock http://unix/v1/health
```

Bearer-protected endpoints require the daemon token configured for the local
session.

## Common Route Families

| Family | Purpose |
| --- | --- |
| `/v1/health` | Basic daemon liveness. |
| `/v1/status` | Runtime summary consumed by CLI and TUI. |
| `hades doctor` | CLI subsystem probes and degraded-state reporting. |
| `hades providers list` | CLI provider roster and registration state. |
| `/v1/budget/*` | Spend events, cap checks, pause, and resume. |
| `/v1/audit/*` | Audit events and evidence lookup. |
| `/v1/caronte/*` | Code-graph queries, impact, intent, and reindex surfaces. |
| `/v1/mcpgateway` | Tool gateway for MCP-style calls. |
| `/v1/mcpgateway/workspace/*` | Federation workspace lifecycle. |
| `/v1/mcpgateway/contract/*` | Contract lookup, validation, and impact surfaces. |
| `/v1/orchestrator/*` | Queue, worktree, review, and orchestration state. |
| `/v1/events` | Event stream for UI and plugin consumers. |

Route availability can depend on daemon configuration. A route that requires a
store or subsystem should return a structured degraded-state error when that
subsystem is unavailable.

## Error Shape

Integrations should expect structured errors with:

- Stable code.
- Human-readable message.
- Recoverability or retry hint when available.
- Service or subsystem name when relevant.

Treat `503` as "subsystem unavailable" when the response body identifies a
missing adapter, store, provider, or daemon dependency. Treat `404` as "record
not found" only when the subsystem is otherwise healthy.

## Federation Examples

Validate a contract manifest:

```bash
curl --unix-socket /tmp/hades-system.sock \
  -X POST \
  -H "Content-Type: application/json" \
  -d '{"repo_path":"/path/to/example-web","workspace_id":"example-ws"}' \
  http://unix/v1/mcpgateway/contract/validate
```

List workspace members:

```bash
curl --unix-socket /tmp/hades-system.sock \
  http://unix/v1/mcpgateway/workspace/example-ws/members
```

Inspect recorded API impact:

```bash
curl --unix-socket /tmp/hades-system.sock \
  "http://unix/v1/mcpgateway/contract/api-impact?workspace_id=example-ws&diff_ref=change:change-123"
```

## Integration Guidance

- Keep HTTP clients local unless you intentionally wrap the daemon behind a
  trusted local transport.
- Use CLI JSON output when you do not need route-level control.
- Preserve error codes in logs; do not collapse all failures into generic text.
- Include request scope in audit events for operations that mutate state.
- Re-run doctor before retry loops that could amplify a degraded dependency.
