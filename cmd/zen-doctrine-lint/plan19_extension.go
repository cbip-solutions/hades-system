// SPDX-License-Identifier: MIT
// plan19_extension.go — (Caronte) analyzer registration.
//
// Registers go vet analyzer surface into the zen-doctrine-lint
// plugin binary, mirroring plan9_extension.go / plan14_extension.go:
//
// 1. loretrailer — when -loretrailer.enabled=true (default false), flags
// commits touching high-risk nodes that lack a Lore-Constraint: git-
// trailer. Default OFF = no-op.
//
// # Registration mechanism
//
// Same shape as plan14_extension.go: this file exports
// Plan19RegisteredAnalyzers() so a future BuildAnalyzers() update in main.go
// can merge it into the plugin's returned set. The wiring is intentionally
// additive — RegisteredAnalyzers + Plan14RegisteredAnalyzers
// surfaces are NOT modified. Each plan owns its own extension file so the
// frozen-surface invariant of earlier plans is preserved.
//
// # Caronte cutover wiring note
//
// To activate the analyzer alongside + 9 + 14 in the golangci-
// lint plugin bundle, the Caronte cutover phase updates BuildAnalyzers() in
// main.go:
//
// func (p *doctrineLintPlugin) BuildAnalyzers() ([]*analysis.Analyzer, error) {
// bundle := append(standaloneAnalyzers(), RegisteredAnalyzers()...)
// bundle = append(bundle, Plan14RegisteredAnalyzers()...)
// bundle = append(bundle, Plan19RegisteredAnalyzers()...)
// return bundle, nil
// }
//
// Until then, loretrailer is exercised standalone (analyzer_test.go) and via
// the daemon / make-lint wrapper that passes -loretrailer.enabled +
// -loretrailer.high-risk-files when adoption is on.
//
// References
// - Spec §10 (get_why — Lore source) + §21 (adoption-gated default OFF)
// - invariant (Lore enforcement when enabled)
// - J-10 plan9_extension.go + H-1 plan14_extension.go precedent
package main

import (
	"github.com/cbip-solutions/hades-system/internal/doctrine/lint/analyzers/loretrailer"
	"golang.org/x/tools/go/analysis"
)

var plan19Analyzers = []*analysis.Analyzer{
	loretrailer.Analyzer,
}

func Plan19RegisteredAnalyzers() []*analysis.Analyzer {
	result := make([]*analysis.Analyzer, len(plan19Analyzers))
	copy(result, plan19Analyzers)
	return result
}
