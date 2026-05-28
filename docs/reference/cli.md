# CLI Reference

The `hades` CLI is the preferred human-facing entry point. It wraps daemon
requests, formats output, and provides stable command names for scripts.

Use `--format json` on commands that support it when another tool will consume
the result.

## Health And Doctor

```bash
bin/hades status
bin/hades doctor
bin/hades doctor caronte
bin/hades doctor hermes
bin/hades doctor mcps
bin/hades doctor sidecars
```

## Daemon

```bash
bin/hades daemon start
bin/hades daemon stop
bin/hades daemon status
bin/hades daemon install
bin/hades daemon uninstall
```

## Providers

```bash
bin/hades providers init
bin/hades providers list
bin/hades providers add <name>
bin/hades providers verify <name>
bin/hades providers rotate <name>
bin/hades providers setup
```

Provider configuration lives in `providers.toml` and `profiles.toml`; secrets
belong outside those files. Use `HADES_KEYCHAIN_*` env aliases on Linux/source
installs, or `hades providers rotate <name>` on macOS.

## Caronte

```bash
bin/hades caronte reindex
bin/hades doctor caronte
bin/hades impact <path-or-symbol>
bin/hades why <path-or-symbol>
bin/hades risk <path-or-symbol>...
bin/hades cochange <path>
bin/hades impl <interface>
bin/hades context <path-or-symbol> --format json
bin/hades codegraph query "<query>"
```

## Contract Federation

```bash
bin/hades workspace init <workspace_id> --owner <project_id> [--member <project_id>]
bin/hades workspace list
bin/hades workspace members <workspace_id> --format json
bin/hades workspace link <workspace_id> <project_id>
bin/hades workspace remove <workspace_id> --yes
bin/hades workspace policy get <workspace_id>
bin/hades workspace policy set <workspace_id> locked|permissive --yes

bin/hades contract validate <repo> --workspace <workspace_id>
bin/hades contract <endpoint_id> --workspace <workspace_id> --format json
bin/hades contract why <change_id> --format json
bin/hades federation health [workspace_id]
bin/hades api-impact <diff-ref> --workspace <workspace_id> --format json
```

## Audit And Budget

```bash
bin/hades audit events --limit 20
bin/hades audit types
bin/hades budget events --limit 20
bin/hades budget cap-status --axis project --value <project> --estimate-usd 0.25
bin/hades budget pause --axis project --value <project> --reason "<reason>" --yes
bin/hades budget resume --axis project --value <project> --yes
```

## Orchestration

```bash
bin/hades orchestrator status
bin/hades orchestrator state
bin/hades orchestrator pool
bin/hades sessions ls
bin/hades schedule queue
```

Exact subcommands can evolve. Use `bin/hades <command> --help` as the
runtime authority for local builds.
