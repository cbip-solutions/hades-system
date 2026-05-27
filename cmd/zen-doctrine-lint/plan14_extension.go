// SPDX-License-Identifier: MIT
// plan14_extension.go — Task H-1 analyzer registration.
//
// Registers go vet analyzer surface into the zen-doctrine-lint
// plugin binary, mirroring the plan9_extension.go pattern:
//
// 1. noWebInEcosystem — rejects net/http + crypto/tls imports + callsites
// in internal/research/ecosystem/*.
//
// # Registration mechanism
//
// Same shape as plan9_extension.go: this file exports
// Plan14RegisteredAnalyzers() so a future BuildAnalyzers() update in
// main.go can merge it into the plugin's returned set. Until that wiring
// lands, the analyzer is exercised:
//
// 1. Standalone via internal/lint/*_test.go (analysistest fixtures).
//
// 2. Via dogfood targets that point cmd/zen-doctrine-lint at the
// ecosystem package surface.
//
// # wiring note
//
// To activate analyzers alongside + in the
// golangci-lint plugin bundle, update BuildAnalyzers() in main.go:
//
// func (p *doctrineLintPlugin) BuildAnalyzers() ([]*analysis.Analyzer, error) {
// bundle := append(standaloneAnalyzers(), RegisteredAnalyzers()...)
// bundle = append(bundle, Plan14RegisteredAnalyzers()...)
// return bundle, nil
// }
//
// Note the wiring is intentionally additive — RegisteredAnalyzers
// surface is NOT modified. Each plan owns its own extension file so the
// frozen-surface invariant of earlier plans is preserved.
//
// References
// - Spec §7.3 ecosystem invariants
// - ADR-0087 Revalidator.Fetch single-egress amendment
// - Task H-1 (internal design record)
// - J-10 plan9_extension.go precedent
package main

import (
	"github.com/cbip-solutions/hades-system/internal/lint"
	"golang.org/x/tools/go/analysis"
)

var plan14Analyzers = []*analysis.Analyzer{
	lint.NoWebInEcosystemAnalyzer,
}

func Plan14RegisteredAnalyzers() []*analysis.Analyzer {
	result := make([]*analysis.Analyzer, len(plan14Analyzers))
	copy(result, plan14Analyzers)
	return result
}
