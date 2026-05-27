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

func TestADR0092Frontmatter(t *testing.T) {
	repoRoot := findRepoRoot(t)
	adrPath := filepath.Join(repoRoot, "docs", "decisions", "0092-hallucination-mitigation-stack.md")

	body, err := os.ReadFile(adrPath)
	if err != nil {
		t.Fatalf("read ADR-0092: %v", err)
	}

	v, err := adr.NewValidator(filepath.Join(repoRoot, "docs", "decisions", "_schema.json"))
	if err != nil {
		t.Fatalf("load validator: %v", err)
	}

	parsed, err := v.ParseAndValidate(adrPath, body)
	if err != nil {
		t.Fatalf("ADR-0092 schema violation: %v", err)
	}

	if parsed.Frontmatter.ID != "ADR-0092" {
		t.Errorf("Frontmatter.ID = %q, want ADR-0092", parsed.Frontmatter.ID)
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
	if parsed.Frontmatter.RiskLevel != "high" {
		t.Errorf("Frontmatter.RiskLevel = %q, want high (hallucination is load-bearing for Plan 14 trust)", parsed.Frontmatter.RiskLevel)
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
			t.Errorf("ADR-0092 missing mandatory section header %q", hdr)
		}
	}

	// Topic 5 SOTA sources MUST be cited verbatim per spec §9.1.
	for _, src := range []string{
		"USENIX Sec 2025",
		"205k",
		"5.2%",
		"21.7%",
		"FACTUM",
		"57%",
		"RAGTruth",
		"Self-RAG",
		"Bayesian",
		"Spracklen",
	} {
		if !strings.Contains(bodyStr, src) {
			t.Errorf("ADR-0092 SOTA references missing %q", src)
		}
	}

	for _, layer := range []string{
		"Citation grammar",
		"verify-at-answer-time",
		"Bayesian abstention",
		"adversarial test suite",
		"audit chain emission",
	} {
		if !strings.Contains(bodyStr, layer) {
			t.Errorf("ADR-0092 5-layer stack missing %q", layer)
		}
	}

	for _, evt := range []string{
		"EvtRAGQuery",
		"EvtRAGRetrieval",
		"EvtRAGCitation",
		"EvtRAGVerify",
		"EvtRAGAbstain",
		"EvtRAGAnswer",
		"EvtRAGIngestPackage",
		"EvtRAGIngestJoinKey",
	} {
		if !strings.Contains(bodyStr, evt) {
			t.Errorf("ADR-0092 audit event %q not referenced (8 EventType slots 92-99 load-bearing)", evt)
		}
	}

	for _, doctrine := range []string{"max-scope", "default", "capa-firewall"} {
		if !strings.Contains(bodyStr, doctrine) {
			t.Errorf("ADR-0092 doctrine %q not referenced", doctrine)
		}
	}

	for _, term := range []string{
		"3-retry",
		"24h LRU",
		"go doc",
		"pip show",
		"npm view",
		"cargo doc",
		"symbol_index",
		"<2%",
	} {
		if !strings.Contains(bodyStr, term) {
			t.Errorf("ADR-0092 Q7 specifics missing %q", term)
		}
	}

	for _, symbol := range []string{
		"internal/research/ecosystem/citation.go",
		"internal/research/ecosystem/verifier.go",
		"internal/research/ecosystem/abstention.go",
		"internal/research/ecosystem/audit_emitter.go",
		"AbstentionPolicy",
		"RAGAuditEmitter",
	} {
		if !strings.Contains(bodyStr, symbol) {
			t.Errorf("ADR-0092 missing production symbol reference %q", symbol)
		}
	}

	// Rejected alternatives MUST be documented (Options B/C/D rationale
	// is load-bearing for future agents asking "why not minimal citation only?").
	for _, alt := range []string{"Option A", "Option B", "Option C", "Option D"} {
		if !strings.Contains(bodyStr, alt) {
			t.Errorf("ADR-0092 missing alternative %q (Q7 4-option evaluation)", alt)
		}
	}

	for _, inv := range []string{"inv-zen-194", "inv-zen-195", "inv-zen-196", "inv-zen-197", "inv-zen-205"} {
		if !strings.Contains(bodyStr, inv) {
			t.Errorf("ADR-0092 missing invariant %q (Phase H test gate)", inv)
		}
	}

	// Stable upstream ADRs MUST be cross-referenced (ADR-0006 = research
	// SOTA mandate, ADR-0062 = audit chain substrate, ADR-0087 = parent
	// (ADR-0083..0086), sibling ADRs are cited descriptively
	// in body where slot numbers are uncertain due to the parallel
	// dispatch shift.
	wantRelatesTo := map[string]bool{"ADR-0006": false, "ADR-0062": false, "ADR-0087": false}
	for _, rel := range parsed.Frontmatter.RelatesTo {
		if _, ok := wantRelatesTo[rel]; ok {
			wantRelatesTo[rel] = true
		}
	}
	for adrID, found := range wantRelatesTo {
		if !found {
			t.Errorf("ADR-0092 frontmatter relates-to missing %q; got %v",
				adrID, parsed.Frontmatter.RelatesTo)
		}
	}

	assertNoClaudeAttribution(t, "ADR-0092", bodyStr)
}
