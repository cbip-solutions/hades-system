// SPDX-License-Identifier: MIT
// plan14_extension.go — Plan 14 Phase H Task H-1 analyzer registration.
//
// Registers Plan 14's go vet analyzer surface into the zen-doctrine-lint
// plugin binary, mirroring the plan9_extension.go pattern:
//
//  1. noWebInEcosystem — rejects net/http + crypto/tls imports + callsites
//     in internal/research/ecosystem/* (inv-zen-191; spec §7.3; ADR-0087).
//
// # Registration mechanism
//
// Same shape as plan9_extension.go: this file exports
// Plan14RegisteredAnalyzers() so a future BuildAnalyzers() update in
// main.go can merge it into the plugin's returned set. Until that wiring
// lands, the Plan 14 analyzer is exercised:
//
//  1. Standalone via internal/lint/*_test.go (analysistest fixtures).
//
//  2. Via dogfood targets that point cmd/zen-doctrine-lint at the
//     ecosystem package surface.
//
// # Phase L wiring note
//
// To activate Plan 14 analyzers alongside Plan 8 + Plan 9 in the
// golangci-lint plugin bundle, update BuildAnalyzers() in main.go:
//
//	func (p *doctrineLintPlugin) BuildAnalyzers() ([]*analysis.Analyzer, error) {
//	    bundle := append(standaloneAnalyzers(), RegisteredAnalyzers()...)
//	    bundle = append(bundle, Plan14RegisteredAnalyzers()...)
//	    return bundle, nil
//	}
//
// Note the wiring is intentionally additive — Plan 9's RegisteredAnalyzers
// surface is NOT modified. Each plan owns its own extension file so the
// frozen-surface invariant of earlier plans is preserved.
//
// References
//   - Spec §7.3 Plan 14 ecosystem invariants
//   - ADR-0087 Revalidator.Fetch single-egress amendment
//   - Plan 14 Phase H Task H-1 (docs/superpowers/plans/2026-05-14-plan-14-phase-H-tests.md)
//   - Plan 9 J-10 plan9_extension.go precedent
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
