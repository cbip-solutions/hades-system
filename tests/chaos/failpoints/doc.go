// go:build chaos

// SPDX-License-Identifier: MIT

// Package failpoints implements gofail-driven chaos
// scenarios across the 15 cross-package hot paths injected in F-2.
//
// # Coverage matrix
//
// 15 source-level gofail comments (see Makefile GOFAIL_PKGS) × {return,
// sleep, panic} → ~45 runtime activations. Tests in this package
// activate one failpoint per test, drive the documented caller code
// path, and assert the documented robustness path engages
// (retry / circuit-trip / tier-fallback / audit emission / etc.).
//
// # Activation
//
// The canonical committed tree is in gofail-DISABLED state (comments
// only; zero runtime cost). The test runner activates failpoints by
// (a) running `make gofail-enable` to rewrite the comments, (b)
// re-running tests under the chaos tag, (c) running `make gofail-disable`
// to return to baseline.
//
// Within an enabled tree, tests pass a Term to gofail's runtime
// (via the GOFAIL_FAILPOINTS env var) to switch a named site on:
//
// GOFAIL_FAILPOINTS='auditWALFsync=return("err")' \
// go test -tags chaos./tests/chaos/failpoints/...
//
// Or via the package-local Activate helper used by the test bodies.
//
// # Skip-on-disabled-tree
//
// Every test in this package starts with requireGofailEnabled(t)
// which probes a canary source file for the disabled-state comment
// signature. If the tree is committed-state-disabled the test
// t.Skips cleanly; CI's chaos workflow runs `make gofail-enable`
// before invoking the suite so the probe passes.
//
// # Why this package (vs F-4 network)
//
// F-4 (tests/chaos/network/) covers the network-edge robustness
// paths via Toxiproxy. F-6 (this package) covers the in-process
// failure modes at the 15 gofail-injected sites: audit-WAL-fsync,
// tessera-tile-upload, dispatcher-cancel-mid-flight, etc. The two
// suites are complementary; their fault models do not overlap.
//
// # Spec
//
// gofail upstream: https://github.com/etcd-io/gofail
package failpoints
