// SPDX-License-Identifier: MIT
// Package nostore — see analyzer.go for the Analyzer instance and Doc field.
// Test fixtures live at../../analysistest/testdata/src/no-store-import/{good,bad}/.
//
// Diagnostic IDs (emitted via analysis.Pass.Reportf):
//
// - nostore-forbidden : import of internal/store from non-allowlisted package
//
// Adapter allowlist:
//
// - github.com/cbip-solutions/hades-system/internal/daemon/bypassadapter
// - github.com/cbip-solutions/hades-system/internal/daemon/dispatcheradapter
// - github.com/cbip-solutions/hades-system/internal/daemon/doctrineadapter
// - github.com/cbip-solutions/hades-system/internal/daemon/workforceadapter
//
// task owns the full implementation.
package nostore
