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

func TestADR0087FrontmatterValid(t *testing.T) {
	repoRoot := findRepoRoot(t)
	adrPath := filepath.Join(repoRoot, "docs", "decisions", "0087-plan-14-ecosystem-rag-architecture-layer-4.md")

	body, err := os.ReadFile(adrPath)
	if err != nil {
		t.Fatalf("read ADR-0087: %v", err)
	}

	v, err := adr.NewValidator(filepath.Join(repoRoot, "docs", "decisions", "_schema.json"))
	if err != nil {
		t.Fatalf("load validator: %v", err)
	}

	parsed, err := v.ParseAndValidate(adrPath, body)
	if err != nil {
		t.Fatalf("ADR-0087 schema violation: %v", err)
	}

	if parsed.Frontmatter.ID != "ADR-0087" {
		t.Errorf("ADR-0087 frontmatter id = %q; want %q", parsed.Frontmatter.ID, "ADR-0087")
	}
	if parsed.Frontmatter.Status != "accepted" {
		t.Errorf("ADR-0087 status = %q; want %q (back-filled with status=accepted)",
			parsed.Frontmatter.Status, "accepted")
	}
	if parsed.Frontmatter.Plan != "plan-14" {
		t.Errorf("ADR-0087 plan = %q; want %q", parsed.Frontmatter.Plan, "plan-14")
	}
	if !strings.HasPrefix(parsed.Frontmatter.Date, "2026-") {
		t.Errorf("ADR-0087 date = %q; want 2026-* (Plan 14 dates)", parsed.Frontmatter.Date)
	}

	mandatory := []string{
		"## Context",
		"## Decision",
		"## Consequences",
		"## Doctrine alignment",
		"## SOTA references",
		"## Plan impact",
		"## Related ADRs",
		"## ADR-0067 amendment inline",
	}
	bodyStr := string(body)
	for _, hdr := range mandatory {
		if !strings.Contains(bodyStr, hdr) {
			t.Errorf("ADR-0087 missing mandatory section header %q", hdr)
		}
	}

	sotaSources := []string{

		"Anthropic Contextual Retrieval",
		"LlamaIndex",
		"RAGFlow",
		"Sourcegraph Cody",

		"Curator",

		"VersionRAG",
		"Particula",
		"Context7",
	}
	for _, src := range sotaSources {
		if !strings.Contains(bodyStr, src) {
			t.Errorf("ADR-0087 SOTA references missing source %q (must cite spec §9 verbatim)", src)
		}
	}

	expectedRelates := []string{
		"ADR-0006",
		"ADR-0007",
		"ADR-0062",
		"ADR-0064",
		"ADR-0065",
		"ADR-0067",
		"ADR-0082",
	}
	for _, rel := range expectedRelates {
		found := false
		for _, r := range parsed.Frontmatter.RelatesTo {
			if r == rel {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("ADR-0087 relates-to missing %q", rel)
		}
	}

	if !strings.Contains(bodyStr, "Revalidator.Fetch") {
		t.Error("ADR-0087 amendment-inline section missing reference to Revalidator.Fetch primitive (Q8=A; Phase A Task A-2)")
	}

	for _, inv := range []string{"inv-zen-191", "inv-zen-205"} {
		if !strings.Contains(bodyStr, inv) {
			t.Errorf("ADR-0087 missing invariant citation %q (Plan 14 range 191-205)", inv)
		}
	}

	if !strings.Contains(bodyStr, "inv-zen-152") {
		t.Error("ADR-0087 missing inv-zen-152 cross-reference (Plan 9 F sole-HTTP-callsite invariant preserved)")
	}

	if !strings.Contains(bodyStr, "92-99") {
		t.Error("ADR-0087 missing EventType 92-99 range citation (8 new RAG audit events)")
	}

	assertNoClaudeAttribution(t, "ADR-0087", bodyStr)
}
