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

func TestADR0089Frontmatter(t *testing.T) {
	repoRoot := findRepoRoot(t)
	adrPath := filepath.Join(repoRoot, "docs", "decisions", "0089-jina-code-matryoshka-twostage.md")

	body, err := os.ReadFile(adrPath)
	if err != nil {
		t.Fatalf("read ADR-0089: %v", err)
	}

	v, err := adr.NewValidator(filepath.Join(repoRoot, "docs", "decisions", "_schema.json"))
	if err != nil {
		t.Fatalf("load validator: %v", err)
	}

	parsed, err := v.ParseAndValidate(adrPath, body)
	if err != nil {
		t.Fatalf("ADR-0089 schema violation: %v", err)
	}

	if parsed.Frontmatter.ID != "ADR-0089" {
		t.Errorf("Frontmatter.ID = %q, want ADR-0089", parsed.Frontmatter.ID)
	}
	if parsed.Frontmatter.Plan != "plan-14" {
		t.Errorf("Frontmatter.Plan = %q, want plan-14", parsed.Frontmatter.Plan)
	}
	if string(parsed.Frontmatter.Status) != "accepted" {
		t.Errorf("Frontmatter.Status = %q, want accepted", parsed.Frontmatter.Status)
	}

	bodyStr := string(body)

	for _, hdr := range mandatorySections {
		if !strings.Contains(bodyStr, hdr) {
			t.Errorf("ADR-0089 missing mandatory header %q", hdr)
		}
	}

	sotaSources := []string{
		"jina-code-embeddings",
		"voyage-code-3",
		"Matryoshka",
		"sqlite-vec",
		"CoIR",
		"MTEB",
		"Qwen3-Embedding",
		"SFR-Embedding-Code",
		"Qodo-Embed",
		"Anthropic",
	}
	for _, src := range sotaSources {
		if !strings.Contains(bodyStr, src) {
			t.Errorf("ADR-0089 SOTA refs missing %q (must cite spec §9.1 Topic 4 verbatim)", src)
		}
	}

	expectedRelates := []string{"ADR-0006", "ADR-0067", "ADR-0087"}
	for _, rel := range expectedRelates {
		found := false
		for _, r := range parsed.Frontmatter.RelatesTo {
			if r == rel {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("ADR-0089 relates-to missing %q (Plan 14 Q4=A cross-references)", rel)
		}
	}

	assertNoClaudeAttribution(t, "ADR-0089", bodyStr)
}
