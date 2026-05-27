// go:build !race
//go:build !race
// +build !race

package compliance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/adr"
)

func TestADR0068_SystemStateTomlAutoDerivedManualFieldsChainIntegration(t *testing.T) {
	repoRoot := findRepoRoot(t)
	adrPath := filepath.Join(repoRoot, "docs", "decisions",
		"0068-system-state-toml-auto-derived-manual-fields-chain-integration.md")

	body, err := os.ReadFile(adrPath)
	if err != nil {
		t.Fatalf("read ADR-0068: %v", err)
	}

	v, err := adr.NewValidator(filepath.Join(repoRoot, "docs", "decisions", "_schema.json"))
	if err != nil {
		t.Fatalf("load validator: %v", err)
	}

	parsed, err := v.ParseAndValidate(adrPath, body)
	if err != nil {
		t.Fatalf("ADR-0068 schema violation: %v", err)
	}

	if parsed.Frontmatter.ID != "ADR-0068" {
		t.Errorf("ADR-0068 frontmatter id = %q; want %q", parsed.Frontmatter.ID, "ADR-0068")
	}
	if parsed.Frontmatter.Status != "accepted" {
		t.Errorf("ADR-0068 status = %q; want %q (Plan 9 ADRs back-filled with status=accepted)",
			parsed.Frontmatter.Status, "accepted")
	}
	if parsed.Frontmatter.Plan != "plan-9" {
		t.Errorf("ADR-0068 plan = %q; want %q", parsed.Frontmatter.Plan, "plan-9")
	}
	if !strings.HasPrefix(parsed.Frontmatter.Date, "2026-05-") {
		t.Errorf("ADR-0068 date = %q; want 2026-05-* (Plan 9 dates)", parsed.Frontmatter.Date)
	}

	bodyStr := string(body)

	mandatory := []string{
		"## Context",
		"## Decision",
		"## Consequences",
		"## Doctrine alignment",
		"## SOTA references",
		"## Plan impact",
		"## Related ADRs",
	}
	for _, hdr := range mandatory {
		if !strings.Contains(bodyStr, hdr) {
			t.Errorf("ADR-0068 missing mandatory section header %q", hdr)
		}
	}

	// system-state.toml path MUST be documented (spec Q9 E: docs/system-state.toml).
	if !strings.Contains(bodyStr, "docs/system-state.toml") {
		t.Errorf("ADR-0068 missing docs/system-state.toml reference (spec Q9 E: primary deliverable)")
	}

	// JSON Schema MUST be documented (spec Q9 E: docs/system-state.schema.json).
	if !strings.Contains(bodyStr, "system-state.schema.json") {
		t.Errorf("ADR-0068 missing system-state.schema.json reference (spec Q9 E: JSON Schema validation)")
	}

	// x-manual-field extension MUST be documented (spec Q9 E: manual fields tagged).
	if !strings.Contains(bodyStr, "x-manual-field") {
		t.Errorf("ADR-0068 missing x-manual-field annotation (spec Q9 E: manual fields tagged in JSON Schema)")
	}

	// 7 mandatory sections of system-state.toml MUST be listed (spec Q9 E).
	tomlSections := []string{
		"[zen-swarm]",
		"[plans]",
		"[invariants]",
		"[doctrines]",
		"[mcps]",
		"[adr]",
		"[autonomous-mode]",
	}
	for _, sec := range tomlSections {
		if !strings.Contains(bodyStr, sec) {
			t.Errorf("ADR-0068 missing TOML section %q (spec Q9 E: 7 mandatory sections)", sec)
		}
	}

	// Walker framework MUST be documented (spec Q9 E: internal/state/manifest/walkers/).
	if !strings.Contains(bodyStr, "walkers") || !strings.Contains(bodyStr, "internal/state/manifest") {
		t.Errorf("ADR-0068 missing walkers framework reference (spec Q9 E: internal/state/manifest/walkers/)")
	}

	// Core components MUST be documented per spec Q9 E.
	components := []string{
		"Regenerator",
		"Differ",
		"ManualTracker",
		"AutonomyValidator",
		"Schema",
	}
	for _, comp := range components {
		if !strings.Contains(bodyStr, comp) {
			t.Errorf("ADR-0068 missing component reference %q (spec Q9 E: required components)", comp)
		}
	}

	// Chain integration MUST be documented (spec Q9 E: state.manual_field_changed event).
	if !strings.Contains(bodyStr, "state.manual_field_changed") {
		t.Errorf("ADR-0068 missing state.manual_field_changed event (spec Q9 E: chain integration for manual changes)")
	}

	// verify gate MUST be documented (spec Q9 E: make verify-system-state).
	if !strings.Contains(bodyStr, "verify-system-state") {
		t.Errorf("ADR-0068 missing verify-system-state reference (spec Q9 E: CI gate)")
	}

	// autonomy --check integration MUST be documented (spec Q9 E).
	if !strings.Contains(bodyStr, "autonomy") && !strings.Contains(bodyStr, "autonomous-mode") {
		t.Errorf("ADR-0068 missing autonomy integration (spec Q9 E: zen autonomy --check integration)")
	}

	// Q9 alternatives (A/B/C/D/E) MUST be documented.
	for _, alt := range []string{"Option A", "Option B", "Option C", "Option D"} {
		if !strings.Contains(bodyStr, alt) {
			t.Errorf("ADR-0068 missing alternative %q (spec Q9 E: 5 options A-E evaluated)", alt)
		}
	}

	// state.regenerate_partial event MUST be documented (failure-mode #12 per spec §4).
	if !strings.Contains(bodyStr, "state.regenerate_partial") {
		t.Errorf("ADR-0068 missing state.regenerate_partial event (failure-mode #12: partial result handling)")
	}

	// zen state CLI commands MUST be listed (spec §6.1 UX).
	stateCLI := []string{
		"zen state show",
		"zen state regenerate",
		"zen state pin",
		"zen state verify",
	}
	for _, cmd := range stateCLI {
		if !strings.Contains(bodyStr, cmd) {
			t.Errorf("ADR-0068 missing CLI command %q (spec §6.1: zen state subcommands)", cmd)
		}
	}

	// invariant and invariant MUST be cited (spec §7.2 state manifest invariants).
	for _, inv := range []string{"inv-zen-149", "inv-zen-151"} {
		if !strings.Contains(bodyStr, inv) {
			t.Errorf("ADR-0068 missing invariant %q (spec §7.2: state manifest invariants)", inv)
		}
	}

	// Cross-cutting theme #3 MUST be cited (regenerate-and-diff as universal freshness primitive).
	if !strings.Contains(bodyStr, "cross-cutting theme #3") && !strings.Contains(bodyStr, "Cross-cutting theme #3") {
		t.Errorf("ADR-0068 missing cross-cutting theme #3 citation (regenerate-and-diff universal freshness)")
	}

	// Topic 4 SOTA (staleness < incompleteness) MUST be cited for failure-mode #12.
	if !strings.Contains(bodyStr, "staleness") || !strings.Contains(bodyStr, "incompleteness") {
		t.Errorf("ADR-0068 missing staleness < incompleteness rationale (Topic 4 SOTA: failure-mode #12)")
	}

	// Litestream dependency MUST be acknowledged (audit chain event durability).
	if !strings.Contains(bodyStr, "Litestream") {
		t.Errorf("ADR-0068 missing Litestream reference (manual field change events land in Litestream-replicated audit_events_raw)")
	}

	for _, doctrine := range []string{"max-scope", "default", "capa-firewall"} {
		if !strings.Contains(bodyStr, doctrine) {
			t.Errorf("ADR-0068 doctrine-alignment section missing %q", doctrine)
		}
	}

	// Tessera leaf anchoring cross-ref MUST be documented (state.manual_field_changed → chain).
	if !strings.Contains(bodyStr, "ADR-0062") {
		t.Errorf("ADR-0068 missing ADR-0062 reference (state.manual_field_changed anchored as per-event Tessera leaf)")
	}

	wantRelatesTo := map[string]bool{"ADR-0062": false, "ADR-0066": false}
	for _, rel := range parsed.Frontmatter.RelatesTo {
		if _, ok := wantRelatesTo[rel]; ok {
			wantRelatesTo[rel] = true
		}
	}
	for adrID, found := range wantRelatesTo {
		if !found {
			t.Errorf("ADR-0068 frontmatter relates-to missing %q; got %v",
				adrID, parsed.Frontmatter.RelatesTo)
		}
	}

	// Q17 layer B prerequisite MUST be cited (system-design §10.2.5).
	if !strings.Contains(bodyStr, "§10.2.5") && !strings.Contains(bodyStr, "Q17") {
		t.Errorf("ADR-0068 missing system-design §10.2.5 / Q17 layer B prerequisite reference")
	}

	// Walkers list MUST include the 7 canonical walkers from spec §1 Q9.
	walkers := []string{
		"walkers/git.go",
		"walkers/gomod.go",
		"walkers/adr.go",
		"walkers/doctrine.go",
		"walkers/mcp.go",
	}
	for _, w := range walkers {
		if !strings.Contains(bodyStr, w) {
			t.Errorf("ADR-0068 missing walker file reference %q (spec Q9 E: walker dispatch table)", w)
		}
	}
}

func TestADR0069_PerPackageCoverageTargetCeilingsUnderFinalFieldSetConstraints(t *testing.T) {
	repoRoot := findRepoRoot(t)
	adrPath := filepath.Join(repoRoot, "docs", "decisions",
		"0069-per-package-coverage-target-ceilings-under-final-field-set-constraints.md")

	body, err := os.ReadFile(adrPath)
	if err != nil {
		t.Fatalf("read ADR-0069: %v", err)
	}

	v, err := adr.NewValidator(filepath.Join(repoRoot, "docs", "decisions", "_schema.json"))
	if err != nil {
		t.Fatalf("load validator: %v", err)
	}

	parsed, err := v.ParseAndValidate(adrPath, body)
	if err != nil {
		t.Fatalf("ADR-0069 schema violation: %v", err)
	}

	if parsed.Frontmatter.ID != "ADR-0069" {
		t.Errorf("ADR-0069 frontmatter id = %q; want %q", parsed.Frontmatter.ID, "ADR-0069")
	}
	if parsed.Frontmatter.Status != "accepted" {
		t.Errorf("ADR-0069 status = %q; want %q (Path D formalization = accepted)",
			parsed.Frontmatter.Status, "accepted")
	}
	if parsed.Frontmatter.Plan != "plan-9" {
		t.Errorf("ADR-0069 plan = %q; want %q", parsed.Frontmatter.Plan, "plan-9")
	}
	if !strings.HasPrefix(parsed.Frontmatter.Date, "2026-05-") {
		t.Errorf("ADR-0069 date = %q; want 2026-05-* (Plan 9 dates)", parsed.Frontmatter.Date)
	}

	bodyStr := string(body)

	mandatory := []string{
		"## Context",
		"## Decision",
		"## Consequences",
		"## Doctrine alignment",
		"## SOTA references",
		"## Plan impact",
		"## Related ADRs",
	}
	for _, hdr := range mandatory {
		if !strings.Contains(bodyStr, hdr) {
			t.Errorf("ADR-0069 missing mandatory section header %q", hdr)
		}
	}

	// Each amendment commit SHA MUST be present (load-bearing for audit trail).
	// These are the 7 commits that form the Path D amendment sequence.
	commitSHAs := []string{
		"88331a3",
		"e0e964f",
		"abe383b",
		"7223e46",
		"04da478",
		"e72de3b",
		"339b9e9",
	}
	for _, sha := range commitSHAs {
		if !strings.Contains(bodyStr, sha) {
			t.Errorf("ADR-0069 missing Stage 1 amendment commit SHA %q (load-bearing audit trail)",
				sha)
		}
	}

	// All 7 architectural-limit category labels MUST be present.
	archLimitCategories := []string{
		"Category 1",
		"Category 2",
		"Category 3",
		"Category 4",
		"Category 5",
		"Category 6",
		"Category 7",
	}
	for _, cat := range archLimitCategories {
		if !strings.Contains(bodyStr, cat) {
			t.Errorf("ADR-0069 missing architectural-limit category label %q (7 categories required)", cat)
		}
	}

	// scripts/coverage-validation.sh MUST be referenced as canonical target table.
	if !strings.Contains(bodyStr, "scripts/coverage-validation.sh") {
		t.Errorf("ADR-0069 missing scripts/coverage-validation.sh reference (canonical coverage target table)")
	}

	// Each affected package MUST be documented with its ceiling.
	affectedPackages := []string{
		"internal/daemon/auditadapter",
		"internal/adr",
		"internal/daemon/handlers",
		"internal/audit/tessera",
		"internal/daemon/knowledgeadapter",
		"internal/audit/litestream",
	}
	for _, pkg := range affectedPackages {
		if !strings.Contains(bodyStr, pkg) {
			t.Errorf("ADR-0069 missing affected package %q", pkg)
		}
	}

	// FINAL field set constraint (Category 1) MUST be documented.
	if !strings.Contains(bodyStr, "FINAL") || !strings.Contains(bodyStr, "CRITICAL-11") {
		t.Errorf("ADR-0069 missing FINAL field set / CRITICAL-11 reference (Category 1: auditadapter field-set constraint)")
	}

	// Infallible stdlib calls (Category 2) MUST be documented with specific examples.
	infallibleCalls := []string{
		"bytes.Buffer.Write",
		"yaml.Marshal",
		"os.Rename",
	}
	for _, call := range infallibleCalls {
		if !strings.Contains(bodyStr, call) {
			t.Errorf("ADR-0069 missing infallible stdlib call %q (Category 2: stdlib guarantees)", call)
		}
	}

	// ncruces driver behavior (Category 3) MUST be documented.
	if !strings.Contains(bodyStr, "ncruces") {
		t.Errorf("ADR-0069 missing ncruces driver reference (Category 3: ncruces driver behavior)")
	}
	if !strings.Contains(bodyStr, "rows.Err") {
		t.Errorf("ADR-0069 missing rows.Err() reference (Category 3: ncruces rows.Err() mid-iteration)")
	}

	// macOS Keychain CI gate (Category 4) MUST cite CLAUDE.md hard rule or
	// ZEN_BYPASS_DISABLE_KEYCHAIN=1 mitigation.
	if !strings.Contains(bodyStr, "ZEN_BYPASS_DISABLE_KEYCHAIN") &&
		!strings.Contains(bodyStr, "Keychain CI gate") {
		t.Errorf("ADR-0069 missing ZEN_BYPASS_DISABLE_KEYCHAIN=1 / Keychain CI gate reference (Category 4)")
	}

	// PRE-PLAN-9 SCOPE BOUNDARY (Category 5) MUST be documented.
	if !strings.Contains(bodyStr, "PRE-PLAN-9 SCOPE BOUNDARY") {
		t.Errorf("ADR-0069 missing PRE-PLAN-9 SCOPE BOUNDARY label (Category 5: handlers pre-existing code)")
	}

	// Subprocess timeout (Category 6) MUST be documented.
	if !strings.Contains(bodyStr, "subprocess") && !strings.Contains(bodyStr, "exec.CommandContext") {
		t.Errorf("ADR-0069 missing subprocess-lifetime timeout reference (Category 6: litestream timeout)")
	}

	// Concurrent race winner paths (Category 7) MUST be documented.
	if !strings.Contains(bodyStr, "race winner") && !strings.Contains(bodyStr, "concurrent race") {
		t.Errorf("ADR-0069 missing concurrent race winner path reference (Category 7: knowledgeadapter)")
	}

	// Path D operator decision MUST be explicitly named.
	if !strings.Contains(bodyStr, "Path D") {
		t.Errorf("ADR-0069 missing 'Path D' operator decision reference (ADR formalizes Stage 1.4 Path D)")
	}

	// NOTE(path-d/adr-0069) cross-reference pattern MUST be documented.
	if !strings.Contains(bodyStr, "NOTE(path-d/adr-0069)") && !strings.Contains(bodyStr, "NOTE block") {
		t.Errorf("ADR-0069 missing NOTE block cross-reference documentation (every unreachable branch NOTE cites this ADR)")
	}

	// make verify-coverage MUST be referenced (CI enforcement gate).
	if !strings.Contains(bodyStr, "verify-coverage") {
		t.Errorf("ADR-0069 missing verify-coverage reference (make verify-coverage CI enforcement)")
	}

	for _, doctrine := range []string{"max-scope", "no tech debt", "no defer"} {
		if !strings.Contains(bodyStr, doctrine) {
			t.Errorf("ADR-0069 doctrine-alignment section missing %q", doctrine)
		}
	}

	// SOTA references: Go cover tool MUST be cited.
	if !strings.Contains(bodyStr, "go.dev/blog/cover") && !strings.Contains(bodyStr, "Go cover tool") {
		t.Errorf("ADR-0069 missing Go cover tool SOTA reference (covers the infallible-path acknowledgement)")
	}

	// SOTA references: ncruces driver source MUST be cited.
	if !strings.Contains(bodyStr, "github.com/ncruces/go-sqlite3") {
		t.Errorf("ADR-0069 missing ncruces/go-sqlite3 source reference (SOTA basis for Category 3)")
	}

	// SOTA references: B-9 CRITICAL-11 plan file MUST be cited.
	if !strings.Contains(bodyStr, "plan-9-phase-B-chain-integration") &&
		!strings.Contains(bodyStr, "Phase B plan file") {
		t.Errorf("ADR-0069 missing Phase B plan file reference (B-9 CRITICAL-11 source for Category 1)")
	}

	// Ceiling values MUST appear in the ADR body.
	ceilingValues := []string{"98%", "97%", "78%", "89%", "88%"}
	for _, ceiling := range ceilingValues {
		if !strings.Contains(bodyStr, ceiling) {
			t.Errorf("ADR-0069 missing ceiling value %q (concrete ceiling must be documented)", ceiling)
		}
	}

	// Packages retaining 100% MUST be explicitly named (audit/chain, audit/recovery).
	unchangedPackages := []string{
		"internal/audit/chain",
		"internal/audit/recovery",
	}
	for _, pkg := range unchangedPackages {
		if !strings.Contains(bodyStr, pkg) {
			t.Errorf("ADR-0069 missing unchanged-100%% package %q (must document which packages kept original targets)",
				pkg)
		}
	}

	wantRelatesTo := map[string]bool{"ADR-0050": false, "ADR-0051": false}
	for _, rel := range parsed.Frontmatter.RelatesTo {
		if _, ok := wantRelatesTo[rel]; ok {
			wantRelatesTo[rel] = true
		}
	}
	for adrID, found := range wantRelatesTo {
		if !found {
			t.Errorf("ADR-0069 frontmatter relates-to missing %q; got %v",
				adrID, parsed.Frontmatter.RelatesTo)
		}
	}
}
