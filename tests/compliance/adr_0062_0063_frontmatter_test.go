//go:build !race
// +build !race

package compliance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/adr"
)

func TestADR0062_PerEventLeafPerPartitionSealHybrid(t *testing.T) {
	repoRoot := findRepoRoot(t)
	adrPath := filepath.Join(repoRoot, "docs", "decisions",
		"0062-audit-chain-per-event-leaf-per-partition-seal-hybrid.md")

	body, err := os.ReadFile(adrPath)
	if err != nil {
		t.Fatalf("read ADR-0062: %v", err)
	}

	v, err := adr.NewValidator(filepath.Join(repoRoot, "docs", "decisions", "_schema.json"))
	if err != nil {
		t.Fatalf("load validator: %v", err)
	}

	parsed, err := v.ParseAndValidate(adrPath, body)
	if err != nil {
		t.Fatalf("ADR-0062 schema violation: %v", err)
	}

	if parsed.Frontmatter.ID != "ADR-0062" {
		t.Errorf("ADR-0062 frontmatter id = %q; want %q", parsed.Frontmatter.ID, "ADR-0062")
	}
	if parsed.Frontmatter.Status != "accepted" {
		t.Errorf("ADR-0062 status = %q; want %q", parsed.Frontmatter.Status, "accepted")
	}
	if parsed.Frontmatter.Plan != "plan-9" {
		t.Errorf("ADR-0062 plan = %q; want %q", parsed.Frontmatter.Plan, "plan-9")
	}
	if !strings.HasPrefix(parsed.Frontmatter.Date, "2026-05-") {
		t.Errorf("ADR-0062 date = %q; want 2026-05-* (Plan 9 dates)", parsed.Frontmatter.Date)
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
			t.Errorf("ADR-0062 missing mandatory section header %q", hdr)
		}
	}

	for _, kw := range []string{"per-event leaf", "per-partition seal", "audit_chain_anchor", "Plan 7 spec"} {
		if !strings.Contains(bodyStr, kw) {
			t.Errorf("ADR-0062 missing keyword %q (Q3 C rationale)", kw)
		}
	}

	for _, kw := range []string{"100K", "events/", "1-10GB"} {
		if !strings.Contains(bodyStr, kw) {
			t.Errorf("ADR-0062 missing volume math citation %q (spec §1 Q3 C)", kw)
		}
	}

	for _, kw := range []string{"hot-path latency", "audit.hot_path_latency_breach"} {
		if !strings.Contains(bodyStr, kw) {
			t.Errorf("ADR-0062 missing latency budget %q (spec §3.1 + §4.2)", kw)
		}
	}

	sotaCites := []string{
		"Litestream",
		"Event Sourcing",
		"litestream.io/how-it-works",
	}
	for _, cite := range sotaCites {
		if !strings.Contains(bodyStr, cite) {
			t.Errorf("ADR-0062 missing Topic 5 SOTA citation %q (spec §8.1 Topic 5)", cite)
		}
	}

	// inv-zen-143 (REFUSE triggers: audit_events_raw append-only) MUST be cited.
	if !strings.Contains(bodyStr, "inv-zen-143") {
		t.Errorf("ADR-0062 missing invariant reference inv-zen-143 (REFUSE triggers)")
	}

	for _, doctrine := range []string{"max-scope", "default", "capa-firewall"} {
		if !strings.Contains(bodyStr, doctrine) {
			t.Errorf("ADR-0062 doctrine-alignment section missing %q", doctrine)
		}
	}

	graphPath := filepath.Join(repoRoot, "docs", "decisions", "_graph.json")
	graphBytes, err := os.ReadFile(graphPath)
	if err != nil {
		t.Fatalf("read _graph.json: %v (Phase E generated it; L-2 regenerates after ADR write)", err)
	}
	var graph struct {
		Nodes []struct {
			ID string `json:"id"`
		} `json:"nodes"`
		Edges []struct {
			From string `json:"from"`
			To   string `json:"to"`
			Kind string `json:"kind"`
		} `json:"edges"`
	}
	if err := json.Unmarshal(graphBytes, &graph); err != nil {
		t.Fatalf("parse _graph.json: %v", err)
	}

	wantNode := false
	for _, n := range graph.Nodes {
		if n.ID == "ADR-0062" {
			wantNode = true
			break
		}
	}
	if !wantNode {
		t.Errorf("_graph.json does not contain ADR-0062 node")
	}

	hasEdgeTo0062 := false
	for _, e := range graph.Edges {
		if e.Kind == "relates-to" && e.To == "ADR-0062" &&
			(e.From == "ADR-0060" || e.From == "ADR-0061") {
			hasEdgeTo0062 = true
			break
		}
	}
	if !hasEdgeTo0062 {
		t.Errorf("_graph.json missing relates-to edge ADR-0060/0061 → ADR-0062")
	}
}

func TestADR0063_DoctrineTunableFederationCadenceAndTamperResponse(t *testing.T) {
	repoRoot := findRepoRoot(t)
	adrPath := filepath.Join(repoRoot, "docs", "decisions",
		"0063-doctrine-tunable-federation-cadence-and-tamper-response.md")

	body, err := os.ReadFile(adrPath)
	if err != nil {
		t.Fatalf("read ADR-0063: %v", err)
	}

	v, err := adr.NewValidator(filepath.Join(repoRoot, "docs", "decisions", "_schema.json"))
	if err != nil {
		t.Fatalf("load validator: %v", err)
	}

	parsed, err := v.ParseAndValidate(adrPath, body)
	if err != nil {
		t.Fatalf("ADR-0063 schema violation: %v", err)
	}

	if parsed.Frontmatter.ID != "ADR-0063" {
		t.Errorf("ADR-0063 frontmatter id = %q; want %q", parsed.Frontmatter.ID, "ADR-0063")
	}
	if parsed.Frontmatter.Status != "accepted" {
		t.Errorf("ADR-0063 status = %q; want %q", parsed.Frontmatter.Status, "accepted")
	}
	if parsed.Frontmatter.Plan != "plan-9" {
		t.Errorf("ADR-0063 plan = %q; want %q", parsed.Frontmatter.Plan, "plan-9")
	}
	if !strings.HasPrefix(parsed.Frontmatter.Date, "2026-05-") {
		t.Errorf("ADR-0063 date = %q; want 2026-05-* (Plan 9 dates)", parsed.Frontmatter.Date)
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
			t.Errorf("ADR-0063 missing mandatory section header %q", hdr)
		}
	}

	q4Cadences := []string{
		"BatchMaxAge=1s",
		"BatchMaxSize=100",
		"BatchMaxAge=30s",
		"BatchMaxSize=1000",
	}
	for _, kw := range q4Cadences {
		if !strings.Contains(bodyStr, kw) {
			t.Errorf("ADR-0063 missing Q4 B cadence verbatim %q", kw)
		}
	}

	q10Modes := []string{
		"halt-per-project",
		"log-continue",
		"cascade-halt-all",
		"audit.tamper_detected",
	}
	for _, kw := range q10Modes {
		if !strings.Contains(bodyStr, kw) {
			t.Errorf("ADR-0063 missing Q10 D mode verbatim %q", kw)
		}
	}

	// inv-zen-150 (per-project blast radius) MUST be cited per spec §7.2.
	if !strings.Contains(bodyStr, "inv-zen-150") {
		t.Errorf("ADR-0063 missing inv-zen-150 (blast radius) reference")
	}

	// inv-zen-136 (Plan 8 validateTighten) MUST be cited per Q4 B per-project
	// override rule.
	if !strings.Contains(bodyStr, "inv-zen-136") {
		t.Errorf("ADR-0063 missing inv-zen-136 (Plan 8 validateTighten) reference")
	}

	sotaCites := []string{
		"transparency-dev/tessera",
		"Sigstore Rekor",
	}
	for _, cite := range sotaCites {
		if !strings.Contains(bodyStr, cite) {
			t.Errorf("ADR-0063 missing SOTA citation %q (spec §8.1 Topic 1+5)", cite)
		}
	}

	for _, doctrine := range []string{"max-scope", "default", "capa-firewall"} {
		if !strings.Contains(bodyStr, doctrine) {
			t.Errorf("ADR-0063 doctrine-alignment section missing %q", doctrine)
		}
	}

	if !strings.Contains(bodyStr, "ADR-0062") {
		t.Errorf("ADR-0063 missing related-ADR reference ADR-0062")
	}
}
