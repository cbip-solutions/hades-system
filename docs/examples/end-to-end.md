# End-To-End Workflows

These examples show how the major HADES subsystems fit together. They assume a
source checkout, built binaries, and commands run from the repository root.

## 1. First Local Bring-Up

```bash
make build
bin/hades-ctld
```

In another terminal:

```bash
bin/hades status
bin/hades doctor
bin/hades providers list
bin/hades doctor caronte
```

Expected result: the daemon responds, optional integrations are either healthy
or explicitly degraded, and missing provider credentials are reported without
stopping unrelated local checks.

## 2. Configure A Local Provider

Create `providers.toml`:

```toml
[[providers]]
name = "ollama-local"
type = "ollama"
endpoint = "http://127.0.0.1:11434"
model = "qwen2.5-coder:32b"
family = "local"
```

Create `profiles.toml`:

```toml
[profiles.worker-code]
description = "local coding worker"
cascade = ["ollama-local"]
```

Verify:

```bash
bin/hades providers list
bin/hades doctor
```

## 3. Inspect Risk Before Editing

```bash
bin/hades caronte reindex
bin/hades why internal/daemon/server.go
bin/hades risk internal/daemon/server.go
bin/hades impact internal/daemon/server.go
bin/hades cochange internal/daemon/server.go
```

Use the result to decide review depth, test scope, and whether HRA attention is
needed before dispatching work.

## 4. Validate A Cross-Repository Contract

```bash
bin/hades workspace init example-ws --owner example-api --member example-web
bin/hades workspace members example-ws --format json
bin/hades contract validate /path/to/example-web --workspace example-ws
bin/hades federation health example-ws
```

If validation fails, fix the manifest before relying on federation results.

## 5. Inspect Recorded API Impact

```bash
bin/hades contract example-service:http:GET:/users/{id} --workspace example-ws --format json
bin/hades contract why change-123 --format json
bin/hades api-impact change:change-123 --workspace example-ws --format json
```

Expected result: if the change exists in the ledger, HADES reports affected
consumers. If no record exists, the command should say so directly.

## 6. Resolve A Pause

```bash
bin/hades status
bin/hades doctor
bin/hades audit events --limit 20
bin/hades budget cap-status --axis project --value example-api --estimate-usd 0.25
```

Then inspect the HRA or confirmation surface through the CLI or TUI. Resume
only after the reason for the pause is understood.

## 7. Verify Local Source Before Trusting It

```bash
make build
make test
make lint
git diff --check
```

For release artifacts, also verify checksums, signatures, and attestations
attached to the release.
