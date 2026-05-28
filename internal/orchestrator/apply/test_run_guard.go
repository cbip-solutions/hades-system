// SPDX-License-Identifier: MIT
package apply

import "flag"

// IsTestRun reports whether the current process is running under
// `go test` (or any test-binary entry point that registers the standard
// testing flags). Production binaries do NOT register `test.v`, so the
// flag.Lookup returns nil there.
//
// This function lives in a file WITHOUT a build tag so the production
// binary still compiles it — the resolver is what protects
// invariant: MergeEngineFake's constructor calls mustBeTestRun() which
// reads IsTestRun() and panics if false.
//
// Why flag.Lookup("test.v") rather than a build-tag-flipped const?
// Two reasons. First, the standard library's testing.Init registers
// test.v lazily on first use under `go test`, so the flag is present
// in every Go test binary regardless of how the project's build tags
// are wired. Second, the operator-mandated simpler shape (single file,
// no build-tag-flipped const, no resolver indirection) is doctrine-
// consistent with the project's narrow-surface convention — fewer
// moving parts means fewer accident vectors. The compliance test
// (J-6) verifies IsTestRun() returns false in the production binary
// + true under `go test`.
//
// A future extension may strengthen this guard if a production path
// legitimately needs to distinguish "test binary" from "test run" —
// e.g. a binary that uses the testing package for benchmarks outside
// of `go test`. For now the simpler shape suffices.
func IsTestRun() bool {
	return flag.Lookup("test.v") != nil
}

func mustBeTestRun() {
	mustBeTestRunWith(IsTestRun())
}

func mustBeTestRunWith(isTestRun bool) {
	if !isTestRun {
		panic("apply: NewMergeEngineFake invoked outside `go test` (invariant)")
	}
}
