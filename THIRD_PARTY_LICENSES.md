# Third-Party Licenses

HADES system is released under the MIT License. This document summarizes the
main third-party licensing posture for this repository.

## Runtime And Build Dependencies

The Go dependency graph is managed by `go.mod` and `go.sum`. Generate a precise
machine-readable dependency inventory with:

```bash
make sbom
```

Release verification uses generated SBOMs, checksums, attestations, and the CGO
supplement under `configs/`.

## Notable Components

- `hermes-agent` provides the Hermes substrate used by the HADES plugin.
- `sqlite-vec` provides SQLite vector-search support through the Go dependency
  graph and CGO release build.
- `smacker/go-tree-sitter` provides tree-sitter bindings used by Caronte's
  code-graph indexing.
- Caronte is in-tree HADES system code and is covered by this repository's MIT
  License.

## Policy

- Keep SPDX headers in source files.
- Keep `go.mod`, `go.sum`, SBOM output, and this summary in sync before
  publishing releases.
- Do not vendor or bundle dependencies with incompatible licenses.
