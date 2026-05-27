// SPDX-License-Identifier: MIT
// Package analysistest hosts the release analyzer test infrastructure
// per spec §1 Q16 D + §5.1 tier 10.
//
// # Layout
//
// testdata/src/<rule>/{good,bad}/ # analysistest convention: per-rule fixture packages
// no-stub/ # nostub analyzer fixtures (Task L-2)
// github.com/cbip-solutions/hades-system/ # nostore fixtures (real-module path so the
// no-store-import-bad/ # Go internal-visibility rule lets fixtures
// no-store-import-good/ # resolve the stubbed internal/store package)
// internal/store/ # stub: import-target only, NOT the real store
// conventional-commit/ # conventional_commit fixtures (Task L-4)
// inv_hades_031_test.go # Q16 D: replaces tests/compliance/inv_hades_031_workforce_test.go
// shared_test.go # cross-analyzer integration test (Task L-5)
//
// Q16 D contract: the invariant invariant is enforced by nostore.Analyzer
// at compile time; this package's inv_hades_031_test.go uses analysistest.Run
// over fixture packages to verify the analyzer's enforcement mechanism.
// Two-step verification model:
//
// invariant ← enforced-by analyzer ← tested-by analysistest
//
// The previous runtime test at tests/compliance/inv_hades_031_workforce_test.go
// is DELETED in Task L-8 — analyzer is THE enforcement.
//
// Adding a new fixture:
//
// 1. Create testdata/src/<rule>/<good|bad>/<descriptive>.go
//
// 2. For bad fixtures, add `// want "regex"` annotation on each line that
// should report a diagnostic (analysistest convention)
//
// 3. The analyzer's _test.go automatically picks up new files via
// analysistest.Run — no test code change needed for adding fixture files
// within an existing rule directory
//
// Adding a new rule:
//
// 1. Create testdata/src/<new-rule>/{good,bad}/ subdirectories
//
// 2. Add fixture files per convention
//
// 3. Implement the analyzer at internal/doctrine/lint/analyzers/<new-rule>/
//
// 4. Register in cmd/hades-doctrine-lint/main.go BuildAnalyzers
//
// 5. Add an analyzer_test.go that calls analysistest.Run over the new
// fixture directory
package analysistest
