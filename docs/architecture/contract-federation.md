# Contract Federation

Contract Federation extends Caronte across a workspace of related repositories.
It answers two operational questions:

- Which clients consume this endpoint?
- Which clients are affected if this endpoint changes?

The subsystem is designed for local-first cross-repository coordination. It is
not a hosted registry. The daemon owns the federation store and exposes CLI,
HTTP, MCP, and TUI surfaces.

## Concepts

| Concept | Meaning |
| --- | --- |
| Workspace | A named group of repositories that may exchange API contracts. |
| Member | A repository registered in a workspace. |
| Owner | The primary repository for a workspace. |
| Policy | Federation privacy mode, commonly `locked` or `permissive`. |
| Manifest | A `caronte.yaml` file that declares contract extraction and client targets. |
| Endpoint | A normalized API surface, such as HTTP method and route. |
| Consumer | A client call site resolved to an endpoint. |
| Breaking change | A persisted record that classifies an API change and its consumers. |

## Workspace Lifecycle

```bash
bin/hades workspace init example-ws --owner example-api --member example-web
bin/hades workspace list
bin/hades workspace members example-ws --format json
bin/hades workspace link example-ws example-worker
bin/hades workspace policy get example-ws
bin/hades workspace policy set example-ws permissive --yes
bin/hades workspace remove example-ws --yes
```

Destructive operations and policy changes should produce audit events and, when
interactive, explicit confirmation.

## Manifest Validation

`hades contract validate <repo>` validates a repository manifest before it is
trusted by the federation store.

```bash
bin/hades contract validate /path/to/example-web --workspace example-ws
```

Validation should reject unsafe or ambiguous manifests, including:

- Missing schema version.
- Multiple base URL variants for the same target.
- Unknown target repository when a workspace roster is supplied.
- Inline secrets.
- Overlong patterns.
- Regex denial-of-service risk.
- Invalid unresolved-reference policy.

## Impact Flow

The impact flow is ledger-backed:

1. Extract or ingest known contract endpoints and client calls.
2. Link consumers to endpoints.
3. Record classified breaking changes.
4. Query affected consumers through `api-impact` or MCP tools.

```bash
bin/hades federation health example-ws
bin/hades contract example-service:http:GET:/users/{id} --workspace example-ws --format json
bin/hades contract why change-123 --format json
bin/hades api-impact change:change-123 --workspace example-ws --format json
```

`api-impact` reports recorded impact. It should not invent consumers when the
federation store has no indexed data for the requested change.

## MCP Tools

The federation tools exposed through the Caronte MCP segment are:

- `get_contract`
- `get_consumers`
- `get_breaking_changes`
- `trace_api_call`
- `get_workspace`
- `federation_health`
- `contract_diff`
- `get_why_breaking_change`

Agents should call `get_consumers` and `get_breaking_changes` before changing
public endpoints in a registered workspace.

## Health Signals

Federation health should report:

- Store reachability.
- Workspace presence and member count.
- Index freshness.
- Unresolved consumer count.
- Gate latency or validator latency when available.
- Last error and degraded reason.

An empty workspace is different from a broken workspace. Output should make
that distinction clear.
