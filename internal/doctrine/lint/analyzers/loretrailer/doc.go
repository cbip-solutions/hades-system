// SPDX-License-Identifier: MIT
// Package loretrailer — see analyzer.go for the Analyzer instance + Doc field.
//
// Diagnostic IDs (emitted via analysis.Pass.Reportf):
//
// - lore-missing-constraint : commit touches a high-risk node (per the
// -loretrailer.high-risk-files glob set) but carries no Lore-Constraint:
// git-trailer (invariant; spec §10). Only emitted when
// -loretrailer.enabled=true (default false — adoption-gated, spec §21).
//
// (design records design)
// owns the full implementation.
package loretrailer
