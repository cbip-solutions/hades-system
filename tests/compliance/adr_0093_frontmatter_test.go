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

func TestADR0093_OpenClaudeTier2DeprecationRoadmap(t *testing.T) {
	repoRoot := findRepoRoot(t)
	adrPath := filepath.Join(repoRoot, "docs", "decisions", "0093-openclaude-tier2-deprecation-roadmap.md")
	body, err := os.ReadFile(adrPath)
	if err != nil {
		t.Fatalf("read ADR-0093: %v", err)
	}
	v, err := adr.NewValidator(filepath.Join(repoRoot, "docs", "decisions", "_schema.json"))
	if err != nil {
		t.Fatalf("load validator: %v", err)
	}
	parsed, err := v.ParseAndValidate(adrPath, body)
	if err != nil {
		t.Fatalf("ADR-0093 schema violation: %v", err)
	}

	if parsed.Frontmatter.ID != "ADR-0093" {
		t.Errorf("Frontmatter.ID = %q, want ADR-0093", parsed.Frontmatter.ID)
	}
	if parsed.Frontmatter.Plan != "plan-16" {
		t.Errorf("Frontmatter.Plan = %q, want plan-16", parsed.Frontmatter.Plan)
	}
	if parsed.Frontmatter.Status != "accepted" {
		t.Errorf("Frontmatter.Status = %q, want accepted", parsed.Frontmatter.Status)
	}

	wantRelates := map[string]bool{"ADR-0001": true, "ADR-0080": true, "ADR-0006": true}
	got := map[string]bool{}
	for _, r := range parsed.Frontmatter.RelatesTo {
		got[r] = true
	}
	for adr := range wantRelates {
		if !got[adr] {
			t.Errorf("Frontmatter.RelatesTo missing %s", adr)
		}
	}

	wantTags := map[string]bool{
		"provider-cascade":       true,
		"openclaude-deprecation": true,
		"llm-routing":            true,
		"plan-3-completion":      true,
		"post-adr-0080":          true,
	}
	gotTags := map[string]bool{}
	for _, tag := range parsed.Frontmatter.Tags {
		gotTags[tag] = true
	}
	for tag := range wantTags {
		if !gotTags[tag] {
			t.Errorf("Frontmatter.Tags missing %q", tag)
		}
	}

	bodyStr := string(body)

	for _, hdr := range mandatorySections {
		if !strings.Contains(bodyStr, hdr) {
			t.Errorf("ADR-0093 body missing section header %q", hdr)
		}
	}

	phasesRequired := []string{
		"Phase A (= R-1",
		"Phase B (= R-2",
		"Phase C (= R-3",
	}
	for _, phase := range phasesRequired {
		if !strings.Contains(bodyStr, phase) {
			t.Errorf("ADR-0093 body missing phase reference %q", phase)
		}
	}

	backendsRequired := []string{
		"anthropic_paygo_backend.go",
		"gemini_backend.go",
		"openai_compat_backend.go",
		"ollama_backend.go",
	}
	for _, backend := range backendsRequired {
		if !strings.Contains(bodyStr, backend) {
			t.Errorf("ADR-0093 body missing backend reference %q", backend)
		}
	}

	invsRequired := []string{
		"cascade completeness",
		"OpenClaude sunset",
		"family-pool runtime",
	}
	for _, inv := range invsRequired {
		if !strings.Contains(bodyStr, inv) {
			t.Errorf("ADR-0093 body missing seeded invariant %q", inv)
		}
	}

	assertNoClaudeAttribution(t, "ADR-0093", bodyStr)
}
