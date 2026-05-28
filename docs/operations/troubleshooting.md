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

## Hermes Plugin Missing

```bash
mkdir -p ~/.hermes/plugins
ln -sfn "$(brew --prefix hades)/share/hades/hades" ~/.hermes/plugins/hades
hades doctor hermes
```

For source checkouts:

```bash
make plugin-install
hades doctor hermes
```

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
hades doctor
```

Store secrets in the OS credential store and keep only references in
`~/.config/hades-system`.

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
