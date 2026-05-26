// SPDX-License-Identifier: MIT
// Package conventional_commit — see analyzer.go for the Analyzer instance and
// Doc field. Test fixtures (commit-subject samples) live at
// ../../analysistest/testdata/src/conventional-commit/{good,bad}/subjects.txt.
//
// Diagnostic IDs (emitted via analysis.Pass.Reportf):
//
//   - cc-bad-type      : subject does not start with allowed type
//   - cc-missing-scope : subject missing (scope) parens
//   - cc-bad-scope     : scope contains forbidden chars or starts with non-letter
//   - cc-bad-subject   : subject text after colon does not start with lowercase
//   - cc-trailing-dot  : subject ends with period
//   - cc-claude-attribution : subject contains "Co-Authored-By: prohibited assistant" or "Generated with prohibited assistant"
//
// Phase L Task L-4 owns the full implementation.
package conventional_commit
