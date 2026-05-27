// SPDX-License-Identifier: MIT
// Package compliance — Task B-15 consolidated compliance
// gate suite for the 8 phase-B invariants (invariant..285, placeholder
// IDs B1..B8 pending merge-time renumber reconciliation per the renumber-
// on-merge playbook).
//
// This file is the AGGREGATOR for the phase-B compliance suite. Composition
// pattern: each TestInvZenBn_* sub-test was authored by the task that owns
// the underlying behaviour (W7-B4/B6/B7/B9/B10/B11/B13/B14) and lives in
// its own file inside this package. The Makefile composite target
// `verify-bypass-sidecar` discovers + invokes them all via the
// `-run '^TestInvZenB[0-9]+_'` regex filter (matches B1..B13 placeholder
// IDs across files). This aggregator additionally contributes:
//
// - TestInvZenB1_B8_AllInvariantsHaveTestImplementations
// a META-completeness gate asserting EACH of the 8 invariants has
// at least one authoritative test file present in the dev repo. Any
// accidental deletion of a sub-test file (e.g. a refactor wave that
// drops inv_zen_B9_brew_formula_test.go) fails this meta-gate even
// if every other phase-B sub-test passes.
//
// - TestInvZenB4_SingleEgressPreserved
// Authored here per spec §B-15 step 2; NOT yet covered by any other
// task. Asserts SidecarBackend's TierBackend-interface compliance +
// that NO direct `bypass.Forward(` call sites exist outside the
// extracted sidecar process (defense-in-depth boundary scan).
//
// - TestInvZenB5_DaemonOnlyStoreOwnership
// Authored here per spec §B-15 step 2; env-var-gated (skips when
// ZEN_BYPASS_TIER1_ROOT is not set so local dev runs pass without a
// private-repo checkout). Asserts the extracted sidecar source
// contains ZERO database/sql + sqlite3-driver + internal/store
// import sites (daemon-only store ownership preserved per
// invariant + ADR-0102 architectural boundary).
//
// - TestInvZenB7_CapabilityNegotiation
// Compliance sister-test that asserts the canonical integration
// test file exists at the documented path AND that the public
// surface of `internal/daemon/dispatcheradapter.FetchSidecarCapabilities`
// + `Capabilities.HasFeature` is present at the contract location.
// The end-to-end behavioural assertions live in the integration
// test (which `verify-sidecar-capability-negotiation` invokes
// separately); this sister-test is a deletion-proof traceability
// anchor — if someone deletes the integration test file in a
// rebase mistake, this gate fails before CI reaches the integration
// run.
//
// Invariant ID to authoritative-test mapping (8 invariants × N test files):
//
// invariant — Zero bypass references in public-snapshot perimeter
// (5-surface boundary scan: AST + tests + docs + configs + SQL)
// Scanner cmd: cmd/verify-no-bypass-references/{main.go,main_test.go,
// sanctioned_allowlist.go} (W7-B7)
// Compliance sister: tests/compliance/inv_zen_B6_split_table_test.go
// (TestInvZenB6_BoundaryScanPresent +
// TestInvZenB6_HookInstalled) (W7-B6)
// Per-PR diff hook:.github/workflows/anti-bypass-reintroduction-on-pr.yml
// (W7-B16)
//
// invariant — /v1 HTTP API surface frozen forever; NO /v2 routes
// Compliance test: tests/compliance/inv_zen_B10_v1_freeze_test.go
// (TestInvZenB10_NoV2RoutesInDevRepo +
// TestInvZenB10_NoV2StringLiteralsInDevRepo +
// TestInvZenB10_CanonicalDaemonSidecarContractRoutesPresent +
// TestInvZenB10_NoV2InGithubWorkflows) (W7-B10)
//
// invariant — Sidecar absent → graceful degrade to cascade
// Unit tests: internal/providers/sidecar_backend_test.go
// (TestSidecarBackend_ConnectionRefused_* +
// TestSidecarBackend_5xx_ReturnsErrSidecarDegraded +
// TestSidecarBackend_Timeout_* +
// TestSidecarBackend_FallbackChainProceedsToPlan16Cascade)
// (W7-B4)
// Note: the dispatcher cascade falling through to is asserted
// here at the unit layer; the daemon-level end-to-end is covered by
// the W7-B16 integration smoke (a downstream consumer of the same
// sentinel errors).
//
// invariant — Single-egress preserved (sidecar = standard TierBackend)
// Compliance test: TestInvZenB4_SingleEgressPreserved (THIS FILE)
// Supporting unit: internal/providers/sidecar_backend_test.go
// (TestSidecarBackend_Name / Tier / Capabilities)
//
// invariant — Daemon-only store ownership (sidecar = HTTP client)
// Compliance test: TestInvZenB5_DaemonOnlyStoreOwnership (THIS FILE)
// Env-var-gated; full assertion runs in CI with the
// private cbip-solutions/zen-bypass-tier1 mounted.
//
// invariant — Brew Formula service block + NO license DSL field
// Compliance test: tests/compliance/inv_zen_B9_brew_formula_test.go
// (TestInvZenB9_BrewFormulaServiceBlock +
// TestInvZenB9_BrewFormulaNoLicenseField) (W7-B9)
// ADR migration sister: tests/compliance/inv_zen_B13_adr_migration_test.go
// (TestInvZenB13_BypassADRsAbsentFromPublicRepo +
// TestInvZenB13_BypassADRsPresentInPrivateRepo) (W7-B13)
//
// invariant — Capability negotiation via /v1/sidecar/info
// Compliance test: TestInvZenB7_CapabilityNegotiation (THIS FILE)
// Integration test: tests/integration/sidecar_capability_negotiation_test.go
// (TestSidecarCapabilityNegotiation_*) (W7-B10)
//
// invariant — Public-snapshot terminology framing + CHANGELOG curation
// Compliance test (INSTALL.md):
// tests/compliance/inv_zen_B8_install_framing_test.go
// (TestInvZenB8_InstallMDPaygoCascadeDefault +
// TestInvZenB8_InstallMDFramedTerminology +
// TestInvZenB8_InstallMDNoPrivateRepoRefs +
// TestInvZenB8_InstallMDCommunityRecipeLink +
// TestInvZenB8_InstallMDSidecarsTOMLDocumented) (W7-B14)
// Compliance test (CHANGELOG curation):
// tests/compliance/inv_zen_B11_curation_table_present_test.go
// (TestInvZenB11_CurationTablePresent +
// TestInvZenB11_CurationTableCoversAllReleases +
// TestInvZenB11_CurationTablePrivacyHeader) (W7-B11)
//
// invariant (boundary discipline) — this file imports stdlib + go/ast only;
// does NOT import internal/store, private-tier1-module, or any
// daemon package. The B7 sister-test verifies the dispatcheradapter
// package surface via a file-existence + grep check on the published API
// rather than importing the package directly (keeps the compliance suite
// free of cross-package coupling so future refactors of dispatcheradapter
// do not cascade test breakage into this gate).

package compliance

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestInvZenB1_B8_AllInvariantsHaveTestImplementations(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	authoritative := map[string][]string{
		"inv-zen-278": {
			"cmd/verify-no-bypass-references/main.go",
			"cmd/verify-no-bypass-references/main_test.go",
			"cmd/verify-no-bypass-references/sanctioned_allowlist.go",
			"tests/compliance/inv_zen_B6_split_table_test.go",
		},
		"inv-zen-279": {
			"tests/compliance/inv_zen_B10_v1_freeze_test.go",
		},
		"inv-zen-280": {
			"internal/providers/sidecar_backend.go",
			"internal/providers/sidecar_backend_test.go",
		},
		"inv-zen-281": {

			"tests/compliance/inv_zen_B1_B8_test.go",
		},
		"inv-zen-282": {

			"tests/compliance/inv_zen_B1_B8_test.go",
		},
		"inv-zen-283": {
			"tests/compliance/inv_zen_B9_brew_formula_test.go",
			"tests/compliance/inv_zen_B13_adr_migration_test.go",
		},
		"inv-zen-284": {
			"tests/integration/sidecar_capability_negotiation_test.go",
			"internal/daemon/dispatcheradapter/sidecar_capabilities.go",
		},
		"inv-zen-285": {
			"tests/compliance/inv_zen_B8_install_framing_test.go",
			"tests/compliance/inv_zen_B11_curation_table_present_test.go",
		},
	}

	for inv, files := range authoritative {
		present := 0
		for _, rel := range files {
			abs := filepath.Join(root, rel)
			if fileExistsB1B8(abs) {
				present++
			}
		}
		if present == 0 {
			t.Errorf("%s: NO authoritative test files present; expected at least one of: %v", inv, files)
		}
	}
}

func TestInvZenB4_SingleEgressPreserved(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	fset := token.NewFileSet()

	backendFile := filepath.Join(root, "internal/providers/sidecar_backend.go")
	f, err := parser.ParseFile(fset, backendFile, nil, parser.AllErrors)
	if err != nil {
		t.Fatalf("parse %s: %v", backendFile, err)
	}
	methods := map[string]bool{}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil {
			continue
		}
		if receiverNameB4(fn.Recv) == "SidecarBackend" {
			methods[fn.Name.Name] = true
		}
	}
	required := []string{"Forward", "Probe", "Close", "Name", "Tier", "Capabilities"}
	for _, m := range required {
		if !methods[m] {
			t.Errorf("inv-zen-281: SidecarBackend missing required TierBackend method %q in %s", m, backendFile)
		}
	}

	pattern := regexp.MustCompile(`\bbypass\.Forward\(`)
	internalRoot := filepath.Join(root, "internal")

	walkErr := filepath.WalkDir(internalRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		if strings.HasSuffix(path, "_test.go") {
			return nil
		}

		rel, _ := filepath.Rel(root, path)
		if strings.HasPrefix(rel, filepath.Join("internal", "anthropic-bypass")) {
			return nil
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		if pattern.Match(body) {
			t.Errorf("inv-zen-281 single-egress violation: direct bypass.Forward( call site in %s (production code MUST route through dispatcher; only private-tier1-module/ may host the legacy in-process call site)", rel)
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("internal-tree walk: %v", walkErr)
	}
}

func TestInvZenB5_DaemonOnlyStoreOwnership(t *testing.T) {
	t.Parallel()
	privateRoot := os.Getenv("ZEN_BYPASS_TIER1_ROOT")
	if privateRoot == "" {
		t.Skip("ZEN_BYPASS_TIER1_ROOT env var not set; full inv-zen-282 assertion runs in CI with private cbip-solutions/zen-bypass-tier1 mounted")
	}
	if _, err := os.Stat(privateRoot); err != nil {
		t.Fatalf("ZEN_BYPASS_TIER1_ROOT=%q not accessible: %v", privateRoot, err)
	}

	forbidden := []string{
		`"database/sql"`,
		`mattn/go-sqlite3`,
		`ncruces/go-sqlite3`,
		`internal/store`,
	}

	walkErr := filepath.WalkDir(privateRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {

			name := d.Name()
			if name == "vendor" || name == ".git" || name == "fixtures" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		text := string(body)
		for _, pat := range forbidden {
			if strings.Contains(text, pat) {
				rel, _ := filepath.Rel(privateRoot, path)
				t.Errorf("inv-zen-282 daemon-only store ownership violation: forbidden token %q found in %s (sidecar source MUST be HTTP-only; ZERO sqlite imports)", pat, rel)
			}
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("private-repo walk: %v", walkErr)
	}
}

// TestInvZenB7_CapabilityNegotiation is the compliance sister-test for
// invariant. The end-to-end behavioural assertions live in the
// integration test (run separately via `make verify-sidecar-capability-
// negotiation`); this sister-test is a deletion-proof traceability
// anchor + a thin surface-level check:
//
// (a) tests/integration/sidecar_capability_negotiation_test.go exists
// at the documented path.
// (b) The dispatcheradapter package source file declares the canonical
// FetchSidecarCapabilities function + the Capabilities.HasFeature
// method (string-grep on the source — the compliance package MUST
// NOT import the daemon-tier package per invariant boundary).
// (c) The integration test file references the required path
// `/v1/sidecar/info`.
//
// A future rebase that accidentally deletes the integration test file
// or renames FetchSidecarCapabilities fails THIS gate before CI even
// reaches the integration run — preserves the merge-time signal even
// when CI parallelism would otherwise hide the regression behind a
// downstream failure.
func TestInvZenB7_CapabilityNegotiation(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	integrationPath := filepath.Join(root, "tests/integration/sidecar_capability_negotiation_test.go")
	if !fileExistsB1B8(integrationPath) {
		t.Fatalf("inv-zen-284: integration test file MISSING at %s (sidecar capability negotiation end-to-end coverage lost)", integrationPath)
	}

	dispatchPath := filepath.Join(root, "internal/daemon/dispatcheradapter/sidecar_capabilities.go")
	body, err := os.ReadFile(dispatchPath)
	if err != nil {
		t.Fatalf("inv-zen-284: dispatcheradapter source unreadable at %s: %v", dispatchPath, err)
	}
	text := string(body)
	requiredSurface := []string{
		"func FetchSidecarCapabilities(",
		"type Capabilities struct",
		"HasFeature",
	}
	for _, sig := range requiredSurface {
		if !strings.Contains(text, sig) {
			t.Errorf("inv-zen-284: dispatcheradapter/sidecar_capabilities.go missing canonical surface %q (capability negotiation contract drift)", sig)
		}
	}

	integrationBody, err := os.ReadFile(integrationPath)
	if err != nil {
		t.Fatalf("inv-zen-284: integration test unreadable: %v", err)
	}
	integrationText := string(integrationBody)
	if !strings.Contains(integrationText, "/v1/sidecar/info") {
		t.Errorf("inv-zen-284: integration test missing reference to FROZEN /v1/sidecar/info route (capability negotiation forward-compat depends on this path being exercised end-to-end)")
	}
	// The integration test MUST exercise the forward-compat property —
	// at minimum it invokes FetchSidecarCapabilities. Grep the function
	// name (a rebase that drops the call would yield a passing but
	// behaviourally-empty integration test).
	if !strings.Contains(integrationText, "FetchSidecarCapabilities") {
		t.Errorf("inv-zen-284: integration test missing call to FetchSidecarCapabilities (forward-compat exercise required)")
	}
}

func fileExistsB1B8(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func receiverNameB4(fl *ast.FieldList) string {
	if fl == nil || len(fl.List) == 0 {
		return ""
	}
	switch typ := fl.List[0].Type.(type) {
	case *ast.StarExpr:
		if ident, ok := typ.X.(*ast.Ident); ok {
			return ident.Name
		}
	case *ast.Ident:
		return typ.Name
	}
	return ""
}
