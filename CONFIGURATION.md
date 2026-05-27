# Configuration Reference

HADES uses small, explicit configuration files. The daemon should be able to
start with no optional provider or sidecar config, then degrade with actionable
doctor output until the operator wires the desired backends.

## Path Reference

| Surface | Default Path | Purpose |
| --- | --- | --- |
| Global config | `$XDG_CONFIG_HOME/hades-system/config.toml` or `~/.config/hades-system/config.toml` | Setup choices: selected provider, doctrine, Hermes scope. |
| Provider configs | `$XDG_CONFIG_HOME/hades-system/providers/<name>.toml` | Provider-specific setup written by onboarding flows. |
| Provider roster | `$XDG_CONFIG_HOME/hades-system/providers.toml` | `[[providers]]` entries consumed by the daemon registry. |
| Profile roster | `$XDG_CONFIG_HOME/hades-system/profiles.toml` | Role-to-provider cascade mapping. |
| Project roster | `$XDG_CONFIG_HOME/hades-system/projects.toml` | Known projects and per-project orchestrator overrides. |
| Checkout override | `<repo>/.hades-system.toml` | Highest-priority per-checkout provider profile/cascade override. |
| Sidecars | `$XDG_CONFIG_HOME/hades/sidecars.toml` or `~/.config/hades/sidecars.toml` | Optional loopback-only Tier 1 sidecar registration. |
| Daemon state | `$XDG_STATE_HOME/hades-system/` or `~/.local/state/hades-system/` | Runtime state, backups, federation DB, and related daemon files. |
| Daemon socket | `/tmp/hades-system.sock` | Default local socket used by CLI and plugin surfaces. |
| Hermes config | `~/.hermes/config.yaml` | Hermes plugin and MCP process registration. |

## Environment Variables

| Variable | Purpose |
| --- | --- |
| `XDG_CONFIG_HOME` | Overrides the config root used by HADES and Hermes helpers. |
| `XDG_STATE_HOME` | Overrides the daemon state root. |
| `HADES_STATE_DIR` | Overrides the daemon state root for HADES-managed state, including `hades-system/workspace.db`. |
| `HADES_DAEMON_UDS` | Overrides the socket path used by local liveness probes. |
| `HADES_SYSTEM_UDS` | Overrides the socket path used by plugin status and event surfaces. |
| `HADES_DAEMON_SOCKET` | Fallback socket variable used by Hermes MCP and hook transports. |
| `HADES_SSH_KNOWN_HOSTS` | Explicit `known_hosts` file for SSH MCP host-key verification. |
| `HADES_SSH_INSECURE_TEST` | Test-only fake-SSH escape hatch; do not use for real hosts. |
| `HADES_NO_WIZARD` | Suppresses first-run Hermes wizard launch when set by the wrapper or operator. |
| `HADES_HOOK_DRY_RUN` | Test-only hook dry-run path for plugin hook tests. |
| `HADES_MCP_DRAIN_TIMEOUT` | MCP shutdown drain timeout override for controlled process tests. |
| `HADES_LOG_DIR` | Log directory override for MCP processes that need file logging. |

## Global Config

`hades config init` writes the global config atomically. A minimal file looks
like:

```toml
schema_version = "1.0"
llm_provider = "ollama-local"
doctrine = "default"
hermes_scope = "user"
```

Secrets do not belong in this file.

## Provider Roster

The daemon consumes `[[providers]]` entries from `providers.toml`. Each entry
must include `name`, `type`, `endpoint`, `model`, and `family`. Provider types
are `anthropic-paygo`, `gemini`, `ollama`, and `openai-compat`.

Local example:

```toml
[[providers]]
name = "ollama-local"
type = "ollama"
endpoint = "http://127.0.0.1:11434"
model = "qwen2.5-coder:32b"
family = "local"
```

API-key example:

```toml
[[providers]]
name = "example-openai-compatible"
type = "openai-compat"
endpoint = "https://api.example.invalid/v1"
model = "example-model"
family = "example"
api_key_keychain = "hades/provider/example-openai-compatible"
```

`api_key_keychain` is a credential-store key name. The secret itself should live
in the operating-system credential store.

## Profile Roster

Profiles map roles to ordered provider cascades. The dispatcher tries providers
in order, subject to health, cost, and policy controls.

```toml
[profiles.worker-code]
description = "local coding worker"
cascade = ["ollama-local"]

[profiles.tactical]
description = "fast low-latency worker"
cascade = ["example-openai-compatible", "ollama-local"]
```

## Project Roster

`projects.toml` can override orchestrator routing per project.

```toml
[projects.example]
path = "/path/to/example"
doctrine = "default"

[projects.example.orchestrator]
default = "worker-code"
fallback_chain = ["ollama-local"]
allow_providers = ["ollama-local"]

[projects.example.orchestrator.payg_safety]
api_key_source = "keychain"
per_session_cap_usd = 5
per_day_cap_usd = 20
per_month_cap_usd = 100
notify_at_percent = [50, 80, 100]
auto_pause_at_cap = true
```

Zero cost caps are interpreted as uncapped. Configure explicit caps for any
pay-as-you-go provider.

## Checkout Override

The checkout-level file is the highest-priority provider routing layer. Use it
for repository-specific routing that should travel with the checkout.

```toml
profile = "worker-code"
cascade = ["ollama-local"]
```

## Sidecar Config

The sidecar file is optional. A missing file means "no sidecar registered".
When present, the daemon validates loopback-only URLs, tier, health-probe
interval, and request timeout before registering the sidecar.

The `[tier1.bypass]` table name is the current daemon schema for the optional
Tier 1 local sidecar slot. It documents the loopback integration point; use it
only with a compatible local sidecar under your control.

```toml
[tier1.bypass]
url = "http://127.0.0.1:39823"
tier = 1
health_probe_interval_s = 30
request_timeout_s = 30
required = false
```

Set `required = true` only when the daemon should treat sidecar unavailability
as a hard startup or routing problem.

## Hermes And MCP Wiring

Hermes uses `~/.hermes/config.yaml`. The HADES plugin and MCP entries should
point at the local daemon socket through `HADES_DAEMON_SOCKET` when possible.
Run:

```bash
bin/hades doctor
bin/hades doctor mcps
bin/hades doctor hermes
```

Use doctor output as the source of truth for missing binaries, socket mismatch,
plugin installation drift, and MCP reachability.

## Validation Commands

```bash
bin/hades status
bin/hades doctor
bin/hades providers list
bin/hades doctor sidecars
bin/hades doctor caronte
```

For release or distribution work, also run the clean-build checks documented in
[INSTALL.md](INSTALL.md).
