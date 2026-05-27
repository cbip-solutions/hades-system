# Threat Model

This threat model covers the HADES source distribution: the local daemon, CLI,
TUI, Hermes plugin, MCP servers, provider routing, sidecar integration point,
audit surfaces, Caronte, and release gates.

HADES is designed as a local-first control plane. It does not assume that every
local process, provider endpoint, remote SSH target, or generated worktree is
trusted.

## Assets

- Source repositories and generated worktrees.
- Daemon bearer tokens, local sockets, and operator session state.
- Provider credentials referenced by Keychain or compatible credential stores.
- SSH agent access and trusted host-key material.
- Audit events, recovery state, release evidence, checksums, and SBOM inputs.
- Caronte indexes, workspace federation state, and API-contract metadata.
- Release source, tags, and distribution artifacts.

## Trust Boundaries

| Boundary | Main Risk | Primary Controls |
| --- | --- | --- |
| Operator shell to daemon | Unauthorized local control | Local transport, bearer checks, explicit confirmation gates. |
| Daemon to provider | Credential exposure, cost runaway | Credential-store indirection, provider validation, budget caps, anomaly handling. |
| Daemon to sidecar | Credential bleed, remote endpoint abuse | Loopback-only URL validation, health probes, timeouts, capability negotiation. |
| MCP to daemon | Over-broad tool power | Narrow tool contracts, single local egress path, daemon-side authorization. |
| SSH execution | Remote command abuse | Agent-only credentials, `known_hosts`, no PTY, allowlist validation, audit events. |
| Worktree execution | Unwanted source mutation | Isolated worktrees, review and merge gates, branch-scoped operations. |
| Release artifacts | Credential or local-path leakage | Secret scanning, DCO, license, SBOM, checksum, and signature gates. |

## Threats And Controls

### Unauthorized Daemon Use

The daemon is expected to bind only to local transports. Sensitive endpoints
require bearer authentication and compare presented tokens in constant time.
Operator-facing commands should use the daemon transport instead of opening
parallel control channels.

### Credential Exposure

Provider TOML files reference credential-store keys rather than embedding API
secrets. SSH execution reads credentials through `SSH_AUTH_SOCK`; HADES does not
load private SSH keys from disk. Release gates treat local paths, unrelated
repository references, and credential-like strings as release blockers.

### Unsafe Remote Execution

The SSH MCP verifies host keys through `known_hosts`, does not request a PTY,
and validates commands against the configured allowlist before execution.
Remote execution failures are surfaced as explicit errors, not silent
fallbacks.

### Cost Runaway

Budget checks run before provider calls. Spend events are recorded by axis, caps
can pause scopes, and anomaly logic can surface unusual cost patterns. A missing
cap is visible configuration state, not an implicit guarantee of bounded spend.

### Autonomy Drift

Autonomous work is bounded by doctrine, confirmation policy, worktree isolation,
review gates, and merge policy. High-risk operations can pause for operator
confirmation. The system prefers explicit degraded state over hidden autonomy.

### Audit Tampering

Audit surfaces are append-oriented and designed for chain verification. Backup,
recovery, witness, and cold-archive commands provide additional evidence paths
when configured.

### Sidecar Misconfiguration

The optional sidecar contract is loopback-only. HADES refuses non-local sidecar
URLs, probes health before registration, applies request timeouts, and treats a
missing sidecar as normal degraded state.

### Supply-Chain Drift

Release verification includes license review, DCO, checksums, signatures, SBOM
inputs, Docker-image checks, and secret scanning.

## Operator Responsibilities

- Keep provider secrets in the operating-system credential store.
- Keep `known_hosts` current for SSH targets.
- Run release gates before publishing or trusting artifacts.
- Treat optional sidecars as privileged local components.
- Review confirmation prompts and high-risk merge decisions carefully.
- Keep docs and examples free of local paths, credentials, and unrelated
  project references.

## Out Of Scope

- Protecting a machine where the operator account is fully compromised.
- Guaranteeing third-party provider behavior after a request leaves the local
  daemon.
- Auditing external sidecar implementations beyond the documented HTTP contract.
- Preventing a trusted operator from intentionally disabling local controls.
