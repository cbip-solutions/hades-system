# Installing HADES system

This document covers installing the `hades-system` source tree published at
`github.com/cbip-solutions/hades-system`.

## Requirements

- macOS 13+ or Linux on amd64 or arm64.
- Go 1.26.0 or newer in the Go 1.26 line when building from source.
- Git 2.40+ for worktree and merge operations.
- SQLite support through the bundled Go driver.
- Hermes Agent for the plugin UX.

Optional tools:

- Docker 24+ for container image builds.
- `cosign` or GitHub's attestation tooling for published artifact verification.

The module declares `go 1.26` and `toolchain go1.26.0`. With the default
`GOTOOLCHAIN=auto`, Go will download the matching toolchain when needed. If your
environment disables automatic toolchain download, install Go 1.26.x manually
before running `make build`.

## Install Hermes Agent

HADES uses Hermes Agent as the conversational/plugin substrate. The Homebrew
formula installs Hermes Agent automatically as a required dependency; source and
Linux users should install Hermes Agent first through Homebrew/Linuxbrew or
their platform package, then verify the binary before running any HADES wizard:

```bash
hermes --version
```

If `hermes --version` fails, install Hermes Agent and rerun the check before
`hades config init`, `hades init`, or `hades new`. Those commands refuse early
when Hermes is missing so the wizard does not create a half-wired plugin setup.

## Build From Source

Homebrew is the primary install path:

```bash
brew tap cbip-solutions/tap
brew install hades
hermes --version
mkdir -p ~/.hermes/plugins
ln -sfn "$(brew --prefix hades)/share/hades/hades" ~/.hermes/plugins/hades
hades-ctld --version
hades --version
brew services start cbip-solutions/tap/hades
hades status
hades doctor
hades doctor hermes
hades doctor mcps
```

Source builds are useful for inspection, local patching, and platforms where
the tap is not available:

```bash
git clone https://github.com/cbip-solutions/hades-system.git
cd hades-system
hermes --version
make build
make test
make plugin-install
bin/hades doctor hermes
bin/hades doctor mcps
```

The primary binaries are:

- `bin/hades`
- `bin/hades-ctld`

## Run The Daemon

If you want external LLM providers active on the first daemon boot, export
their credentials before starting `hades-ctld`; see Provider Credentials below.

```bash
bin/hades-ctld
```

Then inspect local health:

```bash
bin/hades status
bin/hades doctor
bin/hades providers list
bin/hades dashboard
```

## Provider Credentials

HADES can run without pay-as-you-go provider credentials; missing providers are
reported as degraded and the daemon should still start. Configure direct
provider credentials only when you want the provider cascade to call external
LLM APIs.

Linux and source installs use environment variables before any OS credential
store lookup:

```bash
hades providers init
export HADES_KEYCHAIN_OPENROUTER="$OPENROUTER_API_KEY"
export HADES_KEYCHAIN_GOOGLE_AI="$GEMINI_API_KEY"
export HADES_KEYCHAIN_DEEPSEEK="$DEEPSEEK_API_KEY"
hades providers verify openrouter-deepseek
```

If the daemon is already running, restart it after exporting new credentials so
the provider registry observes the updated environment.

On macOS the same environment variables work. You can also store a credential
in the macOS Keychain:

```bash
hades providers rotate openrouter-deepseek
```

## SSH Exec Host Verification

The SSH MCP uses `golang.org/x/crypto/ssh` directly. Credentials come only from
`SSH_AUTH_SOCK`; HADES does not read private keys from disk. Host keys are
verified with `known_hosts`:

- set `HADES_SSH_KNOWN_HOSTS=/path/to/known_hosts` to use an explicit trust file;
- otherwise HADES reads `~/.ssh/known_hosts` and `~/.ssh/known_hosts2` when
  present;
- `HADES_SSH_INSECURE_TEST=1` is reserved for deterministic fake-SSH tests and
  should not be used for real hosts.

## Install The Hermes Plugin

The repository includes the HADES Hermes plugin under `plugin/hades`.
Homebrew installs the payload under the package share directory; source
checkouts can copy it with the repository tooling.

```bash
mkdir -p ~/.hermes/plugins
ln -sfn "$(brew --prefix hades)/share/hades/hades" ~/.hermes/plugins/hades
hermes --version
hades doctor hermes
```

For a source checkout:

```bash
hermes --version
make plugin-install
bin/hades doctor hermes
bin/hades doctor mcps
```

If Hermes keeps an already-running plugin registry, restart Hermes or run the
plugin refresh command supported by your Hermes build after changing the
`~/.hermes/plugins/hades` link.

Use the doctor output as the authority for missing local prerequisites.

## Optional Tier 1 Sidecar

HADES exposes a Tier 1 sidecar HTTP contract so advanced users can provide
their own local Anthropic integration backend.

The sidecar is intentionally optional. The sidecar contract is the daemon's
localhost HTTP integration point; deployments that need it should provide a
compatible local implementation and keep credentials outside this repository.

## Source Verification

Before publishing or trusting a release artifact, build and test from a clean
checkout:

```bash
make build
make test
git diff --check
```

Published artifacts should also be checked against the checksums, attestations,
and signatures attached to the corresponding release.
