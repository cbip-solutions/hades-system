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

## Build From Source

```bash
git clone https://github.com/cbip-solutions/hades-system.git
cd hades-system
make build
make test
```

The primary binaries are:

- `bin/hades`
- `bin/hades-ctld`

## Run The Daemon

```bash
bin/hades-ctld
```

Then inspect local health:

```bash
bin/hades status
bin/hades doctor
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
Install it with the repository tooling or copy it into the Hermes plugin path
used by your environment.

```bash
bin/hades doctor
```

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
