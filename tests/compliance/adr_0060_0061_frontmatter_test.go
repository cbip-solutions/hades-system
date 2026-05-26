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

// TestADR0060FrontmatterValid verifies ADR-0060 (Tessera vendor mode)
// validates against docs/decisions/_schema.json — the Plan 9 ADRs are
// the FIRST corpus for the Structured MADR validator and MUST be
// exemplar quality.
//
// Spec §9: ADR-0060 captures Q1 D (Tessera substrate cross-eje); each
// ADR includes Context (from §1 Q rationale) + Decision (from §1 Q
// decision) + Consequences (positive/negative/neutral) + Doctrine
// alignment + SOTA refs (verbatim from §8) + Plan impact + Related ADRs.
func TestADR0060FrontmatterValid(t *testing.T) {
	repoRoot := findRepoRoot(t)
	adrPath := filepath.Join(repoRoot, "docs", "decisions", "0060-tessera-vendor-mode.md")

	body, err := os.ReadFile(adrPath)
	if err != nil {
		t.Fatalf("read ADR-0060: %v", err)
	}

	v, err := adr.NewValidator(filepath.Join(repoRoot, "docs", "decisions", "_schema.json"))
	if err != nil {
		t.Fatalf("load validator: %v", err)
	}

	parsed, err := v.ParseAndValidate(adrPath, body)
	if err != nil {
		t.Fatalf("ADR-0060 schema violation: %v", err)
	}

	if parsed.Frontmatter.ID != "ADR-0060" {
		t.Errorf("ADR-0060 frontmatter id = %q; want %q", parsed.Frontmatter.ID, "ADR-0060")
	}
	if parsed.Frontmatter.Status != "accepted" {
		t.Errorf("ADR-0060 status = %q; want %q (Plan 9 ADRs back-filled with status=accepted)", parsed.Frontmatter.Status, "accepted")
	}
	if parsed.Frontmatter.Plan != "plan-9" {
		t.Errorf("ADR-0060 plan = %q; want %q", parsed.Frontmatter.Plan, "plan-9")
	}
	if !strings.HasPrefix(parsed.Frontmatter.Date, "2026-05-") {
		t.Errorf("ADR-0060 date = %q; want 2026-05-* (Plan 9 dates)", parsed.Frontmatter.Date)
	}

	mandatory := []string{
		"## Context",
		"## Decision",
		"## Consequences",
		"## Doctrine alignment",
		"## SOTA references",
		"## Plan impact",
		"## Related ADRs",
	}
	bodyStr := string(body)
	for _, hdr := range mandatory {
		if !strings.Contains(bodyStr, hdr) {
			t.Errorf("ADR-0060 missing mandatory section header %q", hdr)
		}
	}

	sotaCites := []string{
		"transparency-dev/tessera",
		"RFC 9162",
		"RFC 6962",
		"Let's Encrypt",
		"Sigstore Rekor",
	}
	for _, cite := range sotaCites {
		if !strings.Contains(bodyStr, cite) {
			t.Errorf("ADR-0060 missing SOTA citation %q (spec §8.1 Topic 1 verbatim)", cite)
		}
	}

	for _, doctrine := range []string{"max-scope", "default", "capa-firewall"} {
		if !strings.Contains(bodyStr, doctrine) {
			t.Errorf("ADR-0060 doctrine-alignment section missing %q", doctrine)
		}
	}
}

func TestADR0061FrontmatterValid(t *testing.T) {
	repoRoot := findRepoRoot(t)
	adrPath := filepath.Join(repoRoot, "docs", "decisions", "0061-per-project-tile-log-daemon-witness-federation.md")

	body, err := os.ReadFile(adrPath)
	if err != nil {
		t.Fatalf("read ADR-0061: %v", err)
	}

	v, err := adr.NewValidator(filepath.Join(repoRoot, "docs", "decisions", "_schema.json"))
	if err != nil {
		t.Fatalf("load validator: %v", err)
	}

	parsed, err := v.ParseAndValidate(adrPath, body)
	if err != nil {
		t.Fatalf("ADR-0061 schema violation: %v", err)
	}

	if parsed.Frontmatter.ID != "ADR-0061" {
		t.Errorf("ADR-0061 frontmatter id = %q; want %q", parsed.Frontmatter.ID, "ADR-0061")
	}

	bodyStr := string(body)

	for _, inv := range []string{"inv-zen-031", "inv-zen-144"} {
		if !strings.Contains(bodyStr, inv) {
			t.Errorf("ADR-0061 missing invariant reference %q", inv)
		}
	}

	// Topic 2 17% YoY cross-tenant leakage stat MUST be cited per spec §1 Q2 A rationale.
	if !strings.Contains(bodyStr, "17%") {
		t.Errorf("ADR-0061 missing Topic 2 17%% YoY cross-tenant leakage stat (spec §1 Q2 A rationale)")
	}

	if !strings.Contains(bodyStr, "ADR-0003") {
		t.Errorf("ADR-0061 missing related-ADR reference ADR-0003 (single multi-tenant daemon)")
	}
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	for dir := cwd; dir != "/"; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
	}
	t.Fatalf("go.mod not found walking up from %s", cwd)
	return ""
}
