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

func TestADR0083_PlanThirteenMigrateClaudeCodeAndCuratedMCPs(t *testing.T) {
	repoRoot := findRepoRoot(t)
	adrPath := filepath.Join(repoRoot, "docs", "decisions", "0083-plan-13-migrate-claude-code-and-curated-mcps.md")
	body, err := os.ReadFile(adrPath)
	if err != nil {
		t.Fatalf("read ADR-0083: %v", err)
	}
	v, err := adr.NewValidator(filepath.Join(repoRoot, "docs", "decisions", "_schema.json"))
	if err != nil {
		t.Fatalf("load validator: %v", err)
	}
	parsed, err := v.ParseAndValidate(adrPath, body)
	if err != nil {
		t.Fatalf("ADR-0083 schema violation: %v", err)
	}
	if parsed.Frontmatter.ID != "ADR-0083" {
		t.Errorf("Frontmatter.ID = %q, want ADR-0083", parsed.Frontmatter.ID)
	}
	if parsed.Frontmatter.Plan != "plan-13" {
		t.Errorf("Frontmatter.Plan = %q, want plan-13", parsed.Frontmatter.Plan)
	}
	bodyStr := string(body)
	for _, hdr := range mandatorySections {
		if !strings.Contains(bodyStr, hdr) {
			t.Errorf("ADR-0083 missing mandatory header %q", hdr)
		}
	}
	for _, cite := range []string{"zen migrate claude-code", "curated MCP", "SOTA-5", "Phase A spike"} {
		if !strings.Contains(bodyStr, cite) {
			t.Errorf("ADR-0083 missing citation %q", cite)
		}
	}
	assertNoClaudeAttribution(t, "ADR-0083", bodyStr)
}

func TestADR0084_ZenDoctorFixSemanticsAndBackup(t *testing.T) {
	repoRoot := findRepoRoot(t)
	adrPath := filepath.Join(repoRoot, "docs", "decisions", "0084-zen-doctor-fix-semantics-and-backup.md")
	body, err := os.ReadFile(adrPath)
	if err != nil {
		t.Fatalf("read ADR-0084: %v", err)
	}
	v, err := adr.NewValidator(filepath.Join(repoRoot, "docs", "decisions", "_schema.json"))
	if err != nil {
		t.Fatalf("load validator: %v", err)
	}
	if _, err := v.ParseAndValidate(adrPath, body); err != nil {
		t.Fatalf("ADR-0084 schema violation: %v", err)
	}
	bodyStr := string(body)
	for _, hdr := range mandatorySections {
		if !strings.Contains(bodyStr, hdr) {
			t.Errorf("ADR-0084 missing mandatory header %q", hdr)
		}
	}
	for _, cite := range []string{
		"--fix",
		"FixMode",
		"interactive",
		"--auto-safe",
		"--yes",
		"inv-zen-178",
		"backup-before-modify",
		"inv-zen-177",
		"bitmask",
	} {
		if !strings.Contains(bodyStr, cite) {
			t.Errorf("ADR-0084 missing citation %q (Q5=C+ rationale)", cite)
		}
	}
	assertNoClaudeAttribution(t, "ADR-0084", bodyStr)
}

func TestADR0085_DoctrineIntegrationOnCuratedMCPs(t *testing.T) {
	repoRoot := findRepoRoot(t)
	adrPath := filepath.Join(repoRoot, "docs", "decisions", "0085-doctrine-integration-on-curated-mcps.md")
	body, err := os.ReadFile(adrPath)
	if err != nil {
		t.Fatalf("read ADR-0085: %v", err)
	}
	v, err := adr.NewValidator(filepath.Join(repoRoot, "docs", "decisions", "_schema.json"))
	if err != nil {
		t.Fatalf("load validator: %v", err)
	}
	if _, err := v.ParseAndValidate(adrPath, body); err != nil {
		t.Fatalf("ADR-0085 schema violation: %v", err)
	}
	bodyStr := string(body)
	for _, hdr := range mandatorySections {
		if !strings.Contains(bodyStr, hdr) {
			t.Errorf("ADR-0085 missing mandatory header %q", hdr)
		}
	}
	for _, cite := range []string{
		"capa-firewall",
		"risk tier",
		"CallDecision",
		"dynamic",
		"inv-zen-184",
		"inv-zen-182",
		"imported-from-claude-code",
		"single-egress",
		"inv-zen-088",
	} {
		if !strings.Contains(bodyStr, cite) {
			t.Errorf("ADR-0085 missing citation %q (Q10=D rationale)", cite)
		}
	}
	assertNoClaudeAttribution(t, "ADR-0085", bodyStr)
}

func TestADR0086_PersistenceStateModelAndPluginFallback(t *testing.T) {
	repoRoot := findRepoRoot(t)
	adrPath := filepath.Join(repoRoot, "docs", "decisions", "0086-persistence-state-model-and-plugin-fallback.md")
	body, err := os.ReadFile(adrPath)
	if err != nil {
		t.Fatalf("read ADR-0086: %v", err)
	}
	v, err := adr.NewValidator(filepath.Join(repoRoot, "docs", "decisions", "_schema.json"))
	if err != nil {
		t.Fatalf("load validator: %v", err)
	}
	if _, err := v.ParseAndValidate(adrPath, body); err != nil {
		t.Fatalf("ADR-0086 schema violation: %v", err)
	}
	bodyStr := string(body)
	for _, hdr := range mandatorySections {
		if !strings.Contains(bodyStr, hdr) {
			t.Errorf("ADR-0086 missing mandatory header %q", hdr)
		}
	}
	for _, cite := range []string{
		"XDG",
		"$XDG_STATE_HOME",
		"$XDG_CACHE_HOME",
		"$XDG_CONFIG_HOME",
		"retention",
		"inv-zen-187",
		"inv-zen-186",
		"project-scope",
		"user-scope",
		"Plan 17b",
		"fallback",
	} {
		if !strings.Contains(bodyStr, cite) {
			t.Errorf("ADR-0086 missing citation %q (Q12=D + Q13=D rationale)", cite)
		}
	}
	assertNoClaudeAttribution(t, "ADR-0086", bodyStr)
}

// mandatorySections lists the canonical Structured MADR section headers
// that every ADR MUST carry.
var mandatorySections = []string{
	"## Context",
	"## Decision",
	"## Consequences",
	"## Doctrine alignment",
	"## SOTA references",
	"## Plan impact",
	"## Related ADRs",
}

// assertNoClaudeAttribution enforces invariant: ADR content MUST NOT
// include Claude attribution markers anywhere in the body.
func assertNoClaudeAttribution(t *testing.T, adrID, body string) {
	t.Helper()
	for _, banned := range []string{
		"Co-Authored-By: prohibited assistant",
		"Generated with prohibited assistant",
		"Generated by prohibited assistant",
	} {
		if strings.Contains(body, banned) {
			t.Errorf("%s includes BANNED attribution %q (inv-zen-004)", adrID, banned)
		}
	}
}
