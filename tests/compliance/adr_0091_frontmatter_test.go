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

func TestADR0091Frontmatter(t *testing.T) {
	repoRoot := findRepoRoot(t)
	adrPath := filepath.Join(repoRoot, "docs", "decisions", "0091-rrf-bge-local-classifier-router.md")

	body, err := os.ReadFile(adrPath)
	if err != nil {
		t.Fatalf("read ADR-0091: %v", err)
	}

	v, err := adr.NewValidator(filepath.Join(repoRoot, "docs", "decisions", "_schema.json"))
	if err != nil {
		t.Fatalf("load validator: %v", err)
	}

	parsed, err := v.ParseAndValidate(adrPath, body)
	if err != nil {
		t.Fatalf("ADR-0091 schema violation: %v", err)
	}

	if parsed.Frontmatter.ID != "ADR-0091" {
		t.Errorf("Frontmatter.ID = %q, want ADR-0091", parsed.Frontmatter.ID)
	}
	if parsed.Frontmatter.Status != "accepted" {
		t.Errorf("Frontmatter.Status = %q, want accepted (Plan 14 back-fill)", parsed.Frontmatter.Status)
	}
	if parsed.Frontmatter.Plan != "plan-14" {
		t.Errorf("Frontmatter.Plan = %q, want plan-14", parsed.Frontmatter.Plan)
	}
	if !strings.HasPrefix(parsed.Frontmatter.Date, "2026-05-") {
		t.Errorf("Frontmatter.Date = %q, want 2026-05-* (Plan 14 dates)", parsed.Frontmatter.Date)
	}

	bodyStr := string(body)

	for _, hdr := range []string{
		"## Context",
		"## Decision",
		"## Consequences",
		"## Doctrine alignment",
		"## SOTA references",
		"## Plan impact",
		"## Related ADRs",
	} {
		if !strings.Contains(bodyStr, hdr) {
			t.Errorf("ADR-0091 missing mandatory section header %q", hdr)
		}
	}

	// Topic 2 SOTA sources MUST be cited verbatim per spec §9.1.
	for _, src := range []string{
		"RAGRoute",
		"FeB4RAG",
		"Bruch",
		"Elastic",
		"Curator",
		"ZeroEntropy",
		"BGE-reranker-v2-m3",
		"Cohere",
		"Jina-ColBERT",
	} {
		if !strings.Contains(bodyStr, src) {
			t.Errorf("ADR-0091 SOTA references missing %q", src)
		}
	}

	// Q6 specifics MUST appear in body (router + reranker + fusion).
	for _, term := range []string{
		"k=60",
		"RRF",
		"weighted",
		"FuseWeighted",
		"BGE",
		"149M",
		"local classifier",
		"sub-ms",
		"softmax",
		"single-egress",
	} {
		if !strings.Contains(bodyStr, term) {
			t.Errorf("ADR-0091 Q6 specifics missing %q", term)
		}
	}

	for _, symbol := range []string{
		"internal/knowledge/aggregator/rrf.go",
		"internal/research/ecosystem/router.go",
		"BGEReRankerV2M3",
		"LogisticClassifier",
	} {
		if !strings.Contains(bodyStr, symbol) {
			t.Errorf("ADR-0091 missing production symbol reference %q", symbol)
		}
	}

	// Rejected alternatives MUST be documented (Options B/C/D rationale
	// is load-bearing for future agents asking "why not Cohere v4 primary?").
	for _, alt := range []string{"Option A", "Option B", "Option C", "Option D"} {
		if !strings.Contains(bodyStr, alt) {
			t.Errorf("ADR-0091 missing alternative %q (Q6 4-option evaluation)", alt)
		}
	}

	for _, doctrine := range []string{"max-scope", "single-egress", "privacy-by-default"} {
		if !strings.Contains(bodyStr, doctrine) {
			t.Errorf("ADR-0091 doctrine-alignment section missing %q", doctrine)
		}
	}

	for _, inv := range []string{"inv-zen-198", "inv-zen-200"} {
		if !strings.Contains(bodyStr, inv) {
			t.Errorf("ADR-0091 missing invariant %q (Phase H test gate)", inv)
		}
	}

	// Stable upstream ADRs MUST be cross-referenced (ADR-0006 = research
	// SOTA mandate, ADR-0065 = aggregator/rrf.go substrate). Per
	// precedent (ADR-0083..0086), sibling ADRs
	// (Layer 4 / jina-code / VersionRAG / hallucination stack) are cited
	// descriptively in body where slot numbers are uncertain due to the
	// parallel dispatch shift.
	wantRelatesTo := map[string]bool{"ADR-0006": false, "ADR-0065": false}
	for _, rel := range parsed.Frontmatter.RelatesTo {
		if _, ok := wantRelatesTo[rel]; ok {
			wantRelatesTo[rel] = true
		}
	}
	for adrID, found := range wantRelatesTo {
		if !found {
			t.Errorf("ADR-0091 frontmatter relates-to missing %q; got %v",
				adrID, parsed.Frontmatter.RelatesTo)
		}
	}

	assertNoClaudeAttribution(t, "ADR-0091", bodyStr)
}
