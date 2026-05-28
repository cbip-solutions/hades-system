# First Five Minutes

Install HADES, start the daemon, and verify the public surfaces before wiring
providers or Hermes.

```bash
brew tap cbip-solutions/tap
brew install hades
mkdir -p ~/.hermes/plugins
ln -sfn "$(brew --prefix hades)/share/hades/hades" ~/.hermes/plugins/hades
hades-ctld --version
hades --version
brew services start cbip-solutions/tap/hades
hades status
hades doctor
hades providers list
hades doctor hermes
hades doctor mcps
hades dashboard
hades caronte reindex --help
```

Expected shape:

- `hades-ctld --version` and `hades --version` print the same version and commit.
- `hades status` shows daemon, socket, project, provider, cascade, cost,
  profile, and next actions. Context, Caronte, federation, and session-cost
  rows appear only when those live runtime signals are present.
- `hades doctor` reports missing optional subsystems as actionable rows.
- `hades providers list` succeeds even before providers are configured.
- `hades dashboard` opens the TUI; `hades dashboard --panel=help` starts on
  the help panel.

Default local socket: `/tmp/hades-system.sock`.
Default config root: `~/.config/hades-system`.
Override the socket with `HADES_DAEMON_SOCKET` when Hermes or MCP processes
need a non-default path.
