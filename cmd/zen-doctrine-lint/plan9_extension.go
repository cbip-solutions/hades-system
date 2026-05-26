// SPDX-License-Identifier: MIT
// plan9_extension.go — Plan 9 Phase J Task J-10 analyzer registration.
//
// Registers Plan 9's 3 custom go vet analyzers into the zen-doctrine-lint
// plugin binary:
//
//  1. noWebInAggregator — rejects net/http imports + callsites in
//     internal/knowledge/aggregator/* (inv-zen-149/151; spec §7.3).
//
//  2. noAutoPromote — rejects Promote() callsites lacking a non-empty
//     GateID argument (inv-zen-146; spec §7.3).
//
//  3. noCrossProjectAtTessera — rejects exported *Adapter methods in
//     internal/daemon/knowledgeadapter/* that cross project boundaries
//     without explicit project-ACL check (inv-zen-152; spec §7.3).
//
// # Registration mechanism
//
// package-level pluginAnalyzers slice). Plan 9 therefore cannot use
// init() to append to a non-existent variable; instead this file exports
// RegisteredAnalyzers() for:
//
//  1. Testing (plan9_extension_test.go): verify all 3 analyzers are
//     present and canonical.
//
//  2. Future wiring: Phase L's .custom-gcl.yml + BuildAnalyzers() update
//     will merge RegisteredAnalyzers() into the plugin's returned set.
//     That wiring is the operator-driven Phase L task; J-10 ships the
//     registration surface and validates the analyzers are ready.
//
// # Phase L wiring note
//
// To activate Plan 9 analyzers in the golangci-lint plugin bundle, Phase L
// should update BuildAnalyzers() in main.go:
//
//	func (p *doctrineLintPlugin) BuildAnalyzers() ([]*analysis.Analyzer, error) {
//	    return append(standaloneAnalyzers(), RegisteredAnalyzers()...), nil
//	}
//
// Until then, the Plan 9 analyzers are exercised standalone via dogfood-
// plan-8 / lint-doctrine-go (they are shipped in internal/lint/ and tested
// by internal/lint/*_test.go with analysistest).
//
// References
//   - Spec §7.3 Plan 9 custom analyzer invariants
//   - Spec §5.6 Makefile targets for extended verification
//   - Plan 8 Q4 B: golangci-lint module-plugin contract
//   - Plan 9 Phase J Task J-10 spec
package main

import (
	"github.com/cbip-solutions/hades-system/internal/lint"
	"golang.org/x/tools/go/analysis"
)

var plan9Analyzers = []*analysis.Analyzer{
	lint.NoAutoPromoteAnalyzer,
	lint.NoWebInAggregatorAnalyzer,
	lint.NoCrossProjectAtTesseraAnalyzer,
}

func RegisteredAnalyzers() []*analysis.Analyzer {
	result := make([]*analysis.Analyzer, len(plan9Analyzers))
	copy(result, plan9Analyzers)
	return result
}
