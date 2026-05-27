# HADES Architecture

HADES is a local-first agentic development orchestrator. It is built around a
daemon that owns long-lived state, with short-lived operator surfaces layered on
top: CLI commands, the TUI, the Hermes plugin, MCP servers, and release gates.

The runtime model below is the supported architecture for HADES v1.0.

## Runtime Map

| Layer | Component | Responsibility |
| --- | --- | --- |
| Operator entry points | `hades`, `zen`, TUI, Hermes plugin | Human-facing command, status, workflow, and review surfaces. |
| Local control plane | `zen-swarm-ctld` daemon | Queue ownership, auth, subsystem lifecycle, local HTTP, audit emission, and recovery hooks. |
| Tool boundary | MCP servers | Narrow tool contracts for research, budget, audit review, and SSH execution. |
| Execution engine | Orchestrator packages | Worktree leases, task dispatch, review flow, merge decisions, scheduling, and autonomy gates. |
| Model routing | Provider registry and profiles | Provider selection, fallback cascades, rate-card metadata, cost gates, and optional sidecar registration. |
| Knowledge substrate | Caronte | In-process code graph, impact analysis, design-intent lookup, co-change, and API-contract federation. |
| Evidence plane | Audit and release gates | Hash-chained audit events, security checks, license checks, SBOM inputs, and artifact verification. |

## Control Flow

1. The operator invokes a CLI command, TUI action, Hermes command, MCP tool, or
   scheduled routine.
2. The surface routes the request to the daemon through the local transport
   selected by that surface.
3. The daemon authenticates the request, resolves project and doctrine state,
   checks confirmations, budget status, provider availability, and recovery
   posture.
4. The daemon dispatches work through isolated worktrees or narrow adapters.
5. Subsystems emit audit events as work advances, fails, pauses, recovers, or
   crosses a confirmation boundary.
6. Review, merge, release, and sync gates consume the recorded state
   instead of relying on ad hoc operator memory.

The daemon is the center of authority. Frontends render and initiate actions;
they do not own durable orchestration state.

## Daemon Subsystems

- **HTTP and auth**: Local endpoints expose status, doctor, dispatcher,
  provider, audit, code-graph, federation, and orchestration surfaces. Bearer
  checks use constant-time comparison.
- **Dispatcher**: Resolves profile cascades, registers providers, applies cost
  checks, records spend events, and degrades across configured backends.
- **Sidecar bridge**: Optionally registers a loopback-only Tier 1 sidecar after
  health and capability probes. Missing sidecar configuration is a normal
  degraded state.
- **Orchestrator**: Owns autonomous work queues, worktree leasing, review
  state, merge decisions, confirmation pauses, and recovery scheduling.
- **Caronte**: Runs in-process. It indexes code, builds symbol and call graphs,
  computes risk, answers intent queries, and supports workspace-level API
  contract federation.
- **Audit**: Captures security-relevant and operator-relevant events in an
  append-oriented evidence plane.

## Primary Interfaces

- `hades`: preferred operator-facing entry point.
- `zen`: compatibility CLI with the full command surface.
- `zen-swarm-ctld`: daemon binary.
- `plugin/hades`: Hermes plugin, slash-command handlers, hooks, and renderers.
- `mcp/*`: MCP server implementations.
- `configs/*`: release and runtime configuration examples.

## Persistence And State

HADES separates configuration from runtime state:

- Global configuration lives under the XDG config directory.
- Per-project checkout configuration lives with the project.
- Daemon and federation state live under the XDG state directory.
- Credentials are referenced by key names and are expected to live in the
  operating-system credential store, not in TOML files.
- Release artifacts are verified through checksums, SBOM material, signatures,
  and dedicated release-gate commands.

## Failure And Degraded Modes

HADES treats partial failure as a normal operating condition:

- A missing optional sidecar falls through to configured provider cascades.
- A missing provider credential prevents that backend from registering but does
  not require every unrelated provider to fail.
- A stale or missing Caronte index is surfaced through doctor and reindex
  commands.
- Budget caps can pause scopes while leaving unrelated scopes available.
- Confirmation gates can pause risky work without tearing down daemon state.
- Release gates fail closed when secret, license, signature, or artifact checks
  detect drift.

## Release Verification

Source releases include runtime code, tests, documentation, and release
configuration. Release gates validate licenses, checksums, signatures, SBOM
inputs, and secret-scanning results before publication.
