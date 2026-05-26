// SPDX-License-Identifier: MIT
// Package nostub — see analyzer.go for the Analyzer instance and the
// Doc field for the diagnostic catalog. Test fixtures live at
// ../../analysistest/testdata/src/no-stub/{good,bad}/.
//
// Diagnostic IDs (emitted via analysis.Pass.Reportf):
//
//   - nostub-panic   : panic("not implemented") or similar
//   - nostub-errnotimpl : return errors.ErrNotImplementedPlanN
//   - nostub-todo    : // TODO implement later comment
//   - nostub-empty-method : empty method body on concrete type
//
// Phase L Task L-2 owns the full implementation.
package nostub
