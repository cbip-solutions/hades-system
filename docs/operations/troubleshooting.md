# Troubleshooting

Use these checks when the first run does not line up.

## Daemon Or Socket Missing

```bash
hades status
hades doctor
```

The default socket is `/tmp/hades-system.sock`. If you changed it, export
`HADES_DAEMON_SOCKET=/path/to/hades.sock` for CLI, Hermes, and MCP processes.

## Homebrew Service Not Started

```bash
brew services start cbip-solutions/tap/hades
hades-ctld --version
hades status
```

## Hermes Agent Missing

```bash
hermes --version
```

If the command is missing, install Hermes Agent before running `hades init`,
`hades new`, or `hades config init`. Homebrew installs it automatically with
`brew install hades`; source and Linux users can install Hermes Agent through
Homebrew/Linuxbrew or their platform package, then rerun `hermes --version`.

## Hermes Plugin Missing

```bash
hermes --version
mkdir -p ~/.hermes/plugins
ln -sfn "$(brew --prefix hades)/share/hades/hades" ~/.hermes/plugins/hades
hades doctor hermes
```

For source checkouts:

```bash
hermes --version
make plugin-install
bin/hades doctor hermes
```

If Hermes was already running, restart Hermes or refresh its plugin registry
after changing `~/.hermes/plugins/hades`.

## MCP Cannot Reach The Daemon

```bash
hades doctor mcps
hades-mcp-research --help
hades-mcp-budget --help
hades-mcp-audit --help
hades-mcp-sshexec --help
```

Confirm each MCP shows `/tmp/hades-system.sock` or the value of
`HADES_DAEMON_SOCKET`.

## Provider Credentials Missing

```bash
hades providers list
hades providers setup
hades doctor
```

Provider credentials are optional at first boot. On Linux/source installs, set
the env alias that matches the `api_key_keychain` reference before starting the
daemon:

```bash
export HADES_KEYCHAIN_OPENROUTER="$OPENROUTER_API_KEY"
export HADES_KEYCHAIN_GOOGLE_AI="$GEMINI_API_KEY"
brew services restart cbip-solutions/tap/hades
```

On macOS the env aliases work too; `hades providers rotate <name>` can store
the key in macOS Keychain instead.

## Caronte Index Empty

```bash
hades caronte reindex
hades doctor caronte
```

## Go Toolchain Mismatch

```bash
go version
make build
make test
```

The source tree declares Go 1.26. With `GOTOOLCHAIN=auto`, Go downloads the
matching toolchain automatically.
