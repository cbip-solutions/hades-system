# Installing HADES system

This document covers installing `hades-system`, the public HADES system
distribution published at `github.com/cbip-solutions/hades-system`.

## Requirements

- macOS 13+ or Linux on amd64 or arm64.
- Go 1.26 when building from source.
- Git 2.40+ for worktree and merge operations.
- SQLite support through the bundled Go driver.
- Hermes Agent for the plugin UX.

Optional tools:

- Docker 24+ for container image workflows.
- `gitleaks`, `cosign`, and SBOM tooling for full release verification.

## Build From Source

```bash
git clone https://github.com/cbip-solutions/hades-system.git
cd hades-system
make build
make test
```

The primary binaries are:

- `bin/hades`
- `bin/zen`
- `bin/zen-swarm-ctld`

## Run The Daemon

```bash
bin/zen-swarm-ctld
```

Then inspect local health:

```bash
bin/hades status
bin/zen doctor
```

## Install The Hermes Plugin

The repository includes the HADES Hermes plugin under `plugin/hades`.
Install it with the repository tooling or copy it into the Hermes plugin path
used by your environment.

```bash
bin/hades doctor
```

Use the doctor output as the authority for missing local prerequisites.

## Optional Tier 1 Sidecar

The public repo documents the Tier 1 sidecar HTTP contract so advanced users can
provide their own local Anthropic integration backend. The private reference
implementation is not part of this distribution.

The sidecar is intentionally optional. The public contract is the daemon's
localhost HTTP integration point; deployments that need it should provide their
own local implementation and keep credentials outside this repository.

## Release Verification

Before publishing or trusting a release artifact, run the public gates:

```bash
make verify-license-compliance
make verify-no-personal-references
make verify-no-task-context-comments
make build
```

Supply-chain checks are rooted in the release artifacts, generated SBOMs,
checksums, and the public CGO supplement under `configs/`.
