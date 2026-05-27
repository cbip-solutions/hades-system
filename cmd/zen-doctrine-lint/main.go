// SPDX-License-Identifier: MIT
// Command zen-doctrine-lint is the golangci-lint module-plugin entry point
// for zen-swarm doctrine static analysis (per spec §1 Q4 B).
//
// Three analyzers ship in this plugin:
//
// 1. nostub — detects panic("not implemented"), errors.ErrNotImplementedPlanN
// returns, // TODO implement later comments, and empty method bodies on
// concrete (non-interface) types. Enforces CLAUDE.md project doctrine
// "No stubs, código completo".
//
// 2. nostore — detects forbidden imports of internal/store from packages
// outside the bypass/dispatcher/doctrineadapter allowlist. Subsumes
// invariant per Q16 D and generalizes to invariant
// (internal/doctrine/* ⊥ internal/store).
//
// 3. conventional_commit — scans recent git log subjects via
// `git log --pretty=%s -n N` and verifies each matches the conventional-
// commit regex per CLAUDE.md hard rule 2. Configurable depth via the
// -conventional_commit.depth flag (default 50).
//
// Build as a golangci-lint custom binary:
//
// #.custom-gcl.yml at repo root:
// # version: v1.61.0
// # plugins:
// # - module: 'github.com/cbip-solutions/hades-system/cmd/zen-doctrine-lint'
// # import: 'github.com/cbip-solutions/hades-system/cmd/zen-doctrine-lint'
// # version: v0.0.0-replace
// # replace:
// # - 'github.com/cbip-solutions/hades-system => /path/to/zen-swarm'
// golangci-lint custom
//
// Or invoke standalone for ad-hoc dogfood:
//
// go install./cmd/zen-doctrine-lint
// go vet -vettool=$(go env GOPATH)/bin/zen-doctrine-lint./internal/doctrine/lint/...
//
// References
//
// - Spec §1 Q4 B: hybrid lint stack (typed Go analyzers + ast-grep YAML)
// - Spec §1 Q16 D: invariant → analysistest single source of truth
// - Spec §2.1: lint package perimeter
// - Spec §2.2 invariant: internal/doctrine/* ⊥ internal/store
// - Spec §7.4 defense-in-depth: this plugin is Layer A static enforcement
// - golangci-lint module plugin docs: https://golangci-lint.run/docs/plugins/module-plugins/
package main

import (
	"github.com/golangci/plugin-module-register/register"
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/multichecker"

	conventional_commit "github.com/cbip-solutions/hades-system/internal/doctrine/lint/analyzers/conventional_commit"
	"github.com/cbip-solutions/hades-system/internal/doctrine/lint/analyzers/nostore"
	"github.com/cbip-solutions/hades-system/internal/doctrine/lint/analyzers/nostub"
	"github.com/cbip-solutions/hades-system/internal/doctrine/lint/analyzers/tierspertool"
)

func init() {
	register.Plugin("zen-doctrine-lint", New)
}

func New(settings any) (register.LinterPlugin, error) {
	return &doctrineLintPlugin{}, nil
}

type doctrineLintPlugin struct{}

func (p *doctrineLintPlugin) BuildAnalyzers() ([]*analysis.Analyzer, error) {
	return []*analysis.Analyzer{
		conventional_commit.Analyzer,
		nostore.Analyzer,
		nostub.Analyzer,
		tierspertool.Analyzer,
	}, nil
}

func (p *doctrineLintPlugin) GetLoadMode() string {
	return register.LoadModeTypesInfo
}

func standaloneAnalyzers() []*analysis.Analyzer {
	return []*analysis.Analyzer{
		conventional_commit.Analyzer,
		nostore.Analyzer,
		nostub.Analyzer,
		tierspertool.Analyzer,
	}
}

func main() {
	multichecker.Main(standaloneAnalyzers()...)
}
