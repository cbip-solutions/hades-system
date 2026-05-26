// SPDX-License-Identifier: MIT
// Package good simulates an allowlisted adapter package that legitimately
// imports internal/store. The package's import path (set via the testdata
// directory name) MUST be on the analyzer's allowlist for the test to
// succeed without diagnostics.
//
// NOTE(plan-15): analysistest runs each testdata package as if its import path were
// the directory name (e.g., "no-store-import/good"). The analyzer's
// allowlist check compares against pass.Pkg.Path(), so for the test to
// succeed the analyzer must allow "no-store-import/good" — which we achieve
// by passing -nostore.allowlist=no-store-import/good in the test invocation
// (see analyzer_test.go).
package good

import (
	_ "github.com/cbip-solutions/hades-system/internal/store"
)

func AdapterDoIt() string {
	return "allowlisted via test config"
}
