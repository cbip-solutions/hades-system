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

func TestADR0064_LitestreamPerProjectColdArchiveDoctrineTunable(t *testing.T) {
	repoRoot := findRepoRoot(t)
	adrPath := filepath.Join(repoRoot, "docs", "decisions",
		"0064-litestream-per-project-cold-archive-doctrine-tunable.md")

	body, err := os.ReadFile(adrPath)
	if err != nil {
		t.Fatalf("read ADR-0064: %v", err)
	}

	v, err := adr.NewValidator(filepath.Join(repoRoot, "docs", "decisions", "_schema.json"))
	if err != nil {
		t.Fatalf("load validator: %v", err)
	}

	parsed, err := v.ParseAndValidate(adrPath, body)
	if err != nil {
		t.Fatalf("ADR-0064 schema violation: %v", err)
	}

	if parsed.Frontmatter.ID != "ADR-0064" {
		t.Errorf("ADR-0064 frontmatter id = %q; want %q", parsed.Frontmatter.ID, "ADR-0064")
	}
	if parsed.Frontmatter.Status != "accepted" {
		t.Errorf("ADR-0064 status = %q; want %q (Plan 9 ADRs back-filled with status=accepted)",
			parsed.Frontmatter.Status, "accepted")
	}
	if parsed.Frontmatter.Plan != "plan-9" {
		t.Errorf("ADR-0064 plan = %q; want %q", parsed.Frontmatter.Plan, "plan-9")
	}
	if !strings.HasPrefix(parsed.Frontmatter.Date, "2026-05-") {
		t.Errorf("ADR-0064 date = %q; want 2026-05-* (Plan 9 dates)", parsed.Frontmatter.Date)
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
			t.Errorf("ADR-0064 missing mandatory section header %q", hdr)
		}
	}

	cadences := []string{
		"continuous Litestream",
		"nightly Tessera rsync",
		"month-end partition seal",
		"Litestream hourly checkpoint",
		"weekly Tessera rsync",
		"object-lock immutable",
	}
	for _, kw := range cadences {
		if !strings.Contains(bodyStr, kw) {
			t.Errorf("ADR-0064 missing Q5 A cadence verbatim %q", kw)
		}
	}

	for _, src := range []string{
		"litestream.io/how-it-works",
		"FastAPI",
		"RPO segundos",
		"WAL streaming",
	} {
		if !strings.Contains(bodyStr, src) {
			t.Errorf("ADR-0064 missing Topic 5 SOTA citation %q (spec §8.1 Topic 5 verbatim)", src)
		}
	}

	for _, kw := range []string{
		"litestream restore",
		"847,239 records",
		"12 partition seals",
	} {
		if !strings.Contains(bodyStr, kw) {
			t.Errorf("ADR-0064 missing recovery semantics example %q (spec §1 Q5 A / §6.5 verbatim)", kw)
		}
	}

	for _, doctrine := range []string{"max-scope", "default", "capa-firewall"} {
		if !strings.Contains(bodyStr, doctrine) {
			t.Errorf("ADR-0064 doctrine-alignment section missing %q", doctrine)
		}
	}

	if !strings.Contains(bodyStr, "ADR-0062") {
		t.Errorf("ADR-0064 missing related-ADR reference ADR-0062 (partition seal triggers cold archive)")
	}
}

func TestADR0065_KnowledgeAggregatorHybridFederatedAndOptInPromote(t *testing.T) {
	repoRoot := findRepoRoot(t)
	adrPath := filepath.Join(repoRoot, "docs", "decisions",
		"0065-knowledge-aggregator-hybrid-federated-and-opt-in-promote.md")

	body, err := os.ReadFile(adrPath)
	if err != nil {
		t.Fatalf("read ADR-0065: %v", err)
	}

	v, err := adr.NewValidator(filepath.Join(repoRoot, "docs", "decisions", "_schema.json"))
	if err != nil {
		t.Fatalf("load validator: %v", err)
	}

	parsed, err := v.ParseAndValidate(adrPath, body)
	if err != nil {
		t.Fatalf("ADR-0065 schema violation: %v", err)
	}

	if parsed.Frontmatter.ID != "ADR-0065" {
		t.Errorf("ADR-0065 frontmatter id = %q; want %q", parsed.Frontmatter.ID, "ADR-0065")
	}
	if parsed.Frontmatter.Status != "accepted" {
		t.Errorf("ADR-0065 status = %q; want %q", parsed.Frontmatter.Status, "accepted")
	}
	if parsed.Frontmatter.Plan != "plan-9" {
		t.Errorf("ADR-0065 plan = %q; want %q", parsed.Frontmatter.Plan, "plan-9")
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
			t.Errorf("ADR-0065 missing mandatory section header %q", hdr)
		}
	}

	for _, kw := range []string{
		"FTS5",
		"sqlite-vec",
		"wikilink graph",
		"RRF",
		"Reciprocal Rank Fusion",
		"Engraph",
		"promote",
		"unpromote",
		"audit_chain_anchor",
		"Plan 7",
	} {
		if !strings.Contains(bodyStr, kw) {
			t.Errorf("ADR-0065 missing Q6 C keyword %q (spec §1 Q6 C verbatim)", kw)
		}
	}

	// inv-zen-129 (aggregator NEVER queries web — Plan 14 territory) MUST be cited.
	if !strings.Contains(bodyStr, "inv-zen-129") {
		t.Errorf("ADR-0065 missing inv-zen-129 (Plan 14 boundary — aggregator never queries web)")
	}

	// inv-zen-146 (promote operator-gated, no auto-promote) MUST be cited.
	if !strings.Contains(bodyStr, "inv-zen-146") {
		t.Errorf("ADR-0065 missing inv-zen-146 (promote operator-gated — no auto-promote code path)")
	}

	if !strings.Contains(bodyStr, "sub-25ms") || !strings.Contains(bodyStr, "16K") {
		t.Errorf("ADR-0065 missing Topic 2 SOTA Engraph latency citation (sub-25ms p50 hasta 16K notas)")
	}

	if !strings.Contains(bodyStr, "306-325") {
		t.Errorf("ADR-0065 missing Plan 7 spec line numbers 306-325 (cross-spec commitment)")
	}

	for _, doctrine := range []string{"max-scope", "default", "capa-firewall"} {
		if !strings.Contains(bodyStr, doctrine) {
			t.Errorf("ADR-0065 doctrine-alignment section missing %q", doctrine)
		}
	}

	if !strings.Contains(bodyStr, "ADR-0007") {
		t.Errorf("ADR-0065 missing related-ADR reference ADR-0007 (gitnexus orthogonal layer)")
	}
}
