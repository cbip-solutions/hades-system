# End-To-End Examples

These examples assume a source checkout of `cbip-solutions/hades-system` and
commands run from the repository root. Replace placeholder project names,
paths, endpoints, and workspace IDs with your local values.

## 1. Build, Start, And Inspect

```bash
make build
bin/zen-swarm-ctld
```

In another terminal:

```bash
bin/hades status
bin/zen doctor
bin/zen doctor caronte
bin/zen providers list
```

Expected shape: the daemon is reachable, doctor reports local prerequisites,
and missing optional providers or sidecars are surfaced as explicit degraded
state.

## 2. Configure A Local Provider

Create a local-only provider roster:

```toml
[[providers]]
name = "ollama-local"
type = "ollama"
endpoint = "http://127.0.0.1:11434"
model = "qwen2.5-coder:32b"
family = "local"
```

Then map a role to it:

```toml
[profiles.worker-code]
description = "local coding worker"
cascade = ["ollama-local"]
```

Verify:

```bash
bin/zen providers list
bin/zen doctor
```

## 3. Run A Code-Graph Inspection

Index or refresh the project graph, then ask for impact and intent:

```bash
bin/zen caronte reindex
bin/zen impact internal/daemon/server.go
bin/zen why internal/daemon/server.go
bin/zen cochange internal/daemon/server.go
```

Use this before editing high-blast-radius code. The goal is to learn the
affected call sites, co-changing files, and design intent before dispatching
work.

## 4. Create And Validate A Federation Workspace

Create a workspace roster, inspect it, and validate one repo's manifest
against that roster:

```bash
bin/zen workspace init example-ws --owner example-api --member example-web
bin/zen workspace link example-ws example-worker
bin/zen workspace members example-ws --format json
bin/zen contract validate /path/to/example-web --workspace example-ws
```

Policy changes and destructive removal require explicit confirmation in
non-interactive shells:

```bash
bin/zen workspace policy set example-ws permissive --yes
bin/zen workspace remove example-ws --yes
```

## 5. Inspect API-Contract Federation Readiness

For a project with indexed federation data, inspect health and known contract
records through the daemon:

```bash
bin/zen federation health example-ws
bin/zen contract example-service:http:GET:/users/{id} --workspace example-ws --format json
```

If a breaking-change record exists, inspect the attribution payload:

```bash
bin/zen contract why change-123 --format json
bin/zen api-impact change:change-123 --workspace example-ws --format json
```

If no federation data is indexed yet, these commands should report an explicit
empty, degraded, or not-found state rather than silently inventing impact data.

## 6. Inspect Audit And Budget State

Audit and cost surfaces are daemon-owned. Use them to understand what happened
and why a scope is paused.

```bash
bin/zen audit events --limit 20
bin/zen audit types
bin/zen budget events --limit 20
bin/zen budget cap-status --axis project --value example-service --estimate-usd 0.25
```

If a scope is intentionally paused or resumed:

```bash
bin/zen budget pause --axis project --value example-service --reason "release freeze" --yes
bin/zen budget resume --axis project --value example-service --yes
```

## 7. Exercise A Safe SSH MCP Path

Use an explicit `known_hosts` file for remote hosts:

```bash
export ZEN_SSH_KNOWN_HOSTS="$HOME/.ssh/known_hosts"
bin/zen doctor mcps
```

The SSH MCP should reject unknown hosts and commands outside its allowlist.

## 8. Optional Sidecar Smoke

When an operator supplies a compatible local sidecar, configure it as
loopback-only:

The `[tier1.bypass]` table name is the daemon's current schema for the optional
Tier 1 local sidecar slot. It names the integration point; the sidecar process
is supplied by the operator.

```toml
[tier1.bypass]
url = "http://127.0.0.1:39823"
tier = 1
health_probe_interval_s = 30
request_timeout_s = 30
required = false
```

Then verify:

```bash
bin/zen doctor sidecars
bin/hades status
```

If the sidecar is absent and `required = false`, the daemon should continue
through the normal provider cascade.

## 8. Run Release Gates Locally

Before publishing or trusting a source release:

```bash
make verify-license-compliance
make verify-no-personal-references
make verify-no-task-context-comments
make verify-release-artifacts
git diff --check
```

For the full local release-gate set, use the Make targets documented in
[INSTALL.md](INSTALL.md) and the release workflow files.
