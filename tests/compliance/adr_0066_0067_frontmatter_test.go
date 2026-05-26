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

func TestADR0066_StructuredMADRADRMachineReadableIndex(t *testing.T) {
	repoRoot := findRepoRoot(t)
	adrPath := filepath.Join(repoRoot, "docs", "decisions",
		"0066-structured-madr-adr-machine-readable-index.md")

	body, err := os.ReadFile(adrPath)
	if err != nil {
		t.Fatalf("read ADR-0066: %v", err)
	}

	v, err := adr.NewValidator(filepath.Join(repoRoot, "docs", "decisions", "_schema.json"))
	if err != nil {
		t.Fatalf("load validator: %v", err)
	}

	parsed, err := v.ParseAndValidate(adrPath, body)
	if err != nil {
		t.Fatalf("ADR-0066 schema violation: %v", err)
	}

	if parsed.Frontmatter.ID != "ADR-0066" {
		t.Errorf("ADR-0066 frontmatter id = %q; want %q", parsed.Frontmatter.ID, "ADR-0066")
	}
	if parsed.Frontmatter.Status != "accepted" {
		t.Errorf("ADR-0066 status = %q; want %q (Plan 9 ADRs back-filled with status=accepted)",
			parsed.Frontmatter.Status, "accepted")
	}
	if parsed.Frontmatter.Plan != "plan-9" {
		t.Errorf("ADR-0066 plan = %q; want %q", parsed.Frontmatter.Plan, "plan-9")
	}
	if !strings.HasPrefix(parsed.Frontmatter.Date, "2026-05-") {
		t.Errorf("ADR-0066 date = %q; want 2026-05-* (Plan 9 dates)", parsed.Frontmatter.Date)
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
			t.Errorf("ADR-0066 missing mandatory section header %q", hdr)
		}
	}

	// Self-referential note MUST be present — ADR-0066 is an exemplar of the
	// format it documents (spec L-4 requirement: explicitly mention self-referential nature).
	if !strings.Contains(bodyStr, "self-referential") && !strings.Contains(bodyStr, "Self-referential") {
		t.Errorf("ADR-0066 missing self-referential note (spec L-4: must explicitly mention that this ADR uses the format it documents)")
	}

	// YAML frontmatter contract MUST be covered: _schema.json reference.
	if !strings.Contains(bodyStr, "_schema.json") {
		t.Errorf("ADR-0066 missing _schema.json reference (spec Q7 A: YAML frontmatter contract points at canonical schema)")
	}

	for _, kw := range []string{
		"santhosh-tekuri",
		"jsonschema",
		"Draft-07",
	} {
		if !strings.Contains(bodyStr, kw) {
			t.Errorf("ADR-0066 missing JSON Schema validator keyword %q (spec Q7 A: Go validator uses santhosh-tekuri/jsonschema/v5 Draft-07)", kw)
		}
	}

	// Dual manifest MUST be described: _index.json + _graph.json.
	for _, manifest := range []string{"_index.json", "_graph.json"} {
		if !strings.Contains(bodyStr, manifest) {
			t.Errorf("ADR-0066 missing dual manifest reference %q (spec Q7 A: dual manifest emitter)", manifest)
		}
	}

	// GraphEdge JSON tags MUST use from/to/kind (per types.go GraphEdge struct).
	// This verifies that the ADR correctly documents the actual Go implementation.
	for _, tag := range []string{`"from"`, `"to"`, `"kind"`} {
		if !strings.Contains(bodyStr, tag) {
			t.Errorf("ADR-0066 missing GraphEdge JSON tag %q (types.go GraphEdge uses from/to/kind tags)", tag)
		}
	}

	// Migration tool MUST be documented.
	if !strings.Contains(bodyStr, "zen adr migrate") {
		t.Errorf("ADR-0066 missing migration tool reference 'zen adr migrate' (spec Q7 A: internal/adr/migrate.go)")
	}

	// 5 ADR transition events MUST be listed verbatim per spec §1 Q7 A.
	transitions := []string{
		"adr.proposed",
		"adr.accepted",
		"adr.rejected",
		"adr.superseded",
		"adr.deprecated",
	}
	for _, ev := range transitions {
		if !strings.Contains(bodyStr, ev) {
			t.Errorf("ADR-0066 missing ADR transition event %q (spec §1 Q7 A: 5 transition events)", ev)
		}
	}

	// Valid transition state machine MUST be documented (proposed → accepted/rejected;
	// accepted → superseded/deprecated per transitions.go).
	if !strings.Contains(bodyStr, "proposed") || !strings.Contains(bodyStr, "accepted") ||
		!strings.Contains(bodyStr, "superseded") || !strings.Contains(bodyStr, "deprecated") {
		t.Errorf("ADR-0066 missing state machine documentation (transitions.go state machine)")
	}

	// ID stability MUST be covered: inv-zen-147 + filename-is-hint.
	if !strings.Contains(bodyStr, "inv-zen-147") {
		t.Errorf("ADR-0066 missing inv-zen-147 reference (ADR ID uniqueness invariant)")
	}
	if !strings.Contains(bodyStr, "filename") {
		t.Errorf("ADR-0066 missing filename-as-hint discussion (spec Q7 A: id frontmatter is primary key, filename is hint only)")
	}

	sotaCites := []string{
		"zircote/structured-madr",
		"log4brains",
		"adr.github.io/madr",
		"remark-lint-frontmatter-schema",
	}
	for _, cite := range sotaCites {
		if !strings.Contains(bodyStr, cite) {
			t.Errorf("ADR-0066 missing Topic 3 SOTA citation %q (spec §8.1 Topic 3 verbatim)", cite)
		}
	}

	// Cross-cutting theme #3 MUST be cited (markdown-as-source-of-truth + derived-index).
	if !strings.Contains(bodyStr, "markdown-as-source-of-truth") && !strings.Contains(bodyStr, "Markdown-as-source-of-truth") {
		t.Errorf("ADR-0066 missing cross-cutting theme #3 citation (markdown-as-source-of-truth + derived-index pattern)")
	}

	for _, doctrine := range []string{"max-scope", "default", "capa-firewall"} {
		if !strings.Contains(bodyStr, doctrine) {
			t.Errorf("ADR-0066 doctrine-alignment section missing %q", doctrine)
		}
	}

	for _, related := range []string{"ADR-0001", "ADR-0007"} {
		if !strings.Contains(bodyStr, related) {
			t.Errorf("ADR-0066 missing related-ADR reference %q (declared in frontmatter relates-to)", related)
		}
	}

	// CI gate MUST be documented: make verify-invariants + zen adr index --check.
	if !strings.Contains(bodyStr, "verify-invariants") {
		t.Errorf("ADR-0066 missing verify-invariants reference (CI freshness gate)")
	}
	if !strings.Contains(bodyStr, "zen adr index") {
		t.Errorf("ADR-0066 missing 'zen adr index' reference (regenerate dual manifest CI gate)")
	}

	wantRelatesTo := map[string]bool{"ADR-0001": false, "ADR-0007": false}
	for _, rel := range parsed.Frontmatter.RelatesTo {
		if _, ok := wantRelatesTo[rel]; ok {
			wantRelatesTo[rel] = true
		}
	}
	for adrID, found := range wantRelatesTo {
		if !found {
			t.Errorf("ADR-0066 frontmatter relates-to missing %q; got %v",
				adrID, parsed.Frontmatter.RelatesTo)
		}
	}
}

func TestADR0067_ResearchFindingsGlobalCacheContentAddressedDualLayer(t *testing.T) {
	repoRoot := findRepoRoot(t)
	adrPath := filepath.Join(repoRoot, "docs", "decisions",
		"0067-research-findings-global-cache-content-addressed-dual-layer.md")

	body, err := os.ReadFile(adrPath)
	if err != nil {
		t.Fatalf("read ADR-0067: %v", err)
	}

	v, err := adr.NewValidator(filepath.Join(repoRoot, "docs", "decisions", "_schema.json"))
	if err != nil {
		t.Fatalf("load validator: %v", err)
	}

	parsed, err := v.ParseAndValidate(adrPath, body)
	if err != nil {
		t.Fatalf("ADR-0067 schema violation: %v", err)
	}

	if parsed.Frontmatter.ID != "ADR-0067" {
		t.Errorf("ADR-0067 frontmatter id = %q; want %q", parsed.Frontmatter.ID, "ADR-0067")
	}
	if parsed.Frontmatter.Status != "accepted" {
		t.Errorf("ADR-0067 status = %q; want %q (Plan 9 ADRs back-filled with status=accepted)",
			parsed.Frontmatter.Status, "accepted")
	}
	if parsed.Frontmatter.Plan != "plan-9" {
		t.Errorf("ADR-0067 plan = %q; want %q", parsed.Frontmatter.Plan, "plan-9")
	}
	if !strings.HasPrefix(parsed.Frontmatter.Date, "2026-05-") {
		t.Errorf("ADR-0067 date = %q; want 2026-05-* (Plan 9 dates)", parsed.Frontmatter.Date)
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
			t.Errorf("ADR-0067 missing mandatory section header %q", hdr)
		}
	}

	// Content-addressed storage (CAS) MUST be documented.
	// Spec Q8 A: SHA-256 keys, filesystem CAS path, dedup semantics.
	casKeywords := []string{
		"SHA-256",
		"sha256",
		"content-addressed",
		"CAS",
		"body_path",
		"body_inline_blob",
	}
	for _, kw := range casKeywords {
		if !strings.Contains(bodyStr, kw) {
			t.Errorf("ADR-0067 missing CAS keyword %q (spec Q8 A: content-addressed storage)", kw)
		}
	}

	// Dual-layer: exact + semantic MUST be documented.
	for _, kw := range []string{
		"exact",
		"semantic",
		"0.92",
		"cosine",
		"sqlite-vec",
		"LookupOrDispatch",
	} {
		if !strings.Contains(bodyStr, kw) {
			t.Errorf("ADR-0067 missing dual-layer keyword %q (spec Q8 A: exact + semantic 0.92 cosine)", kw)
		}
	}

	// Database schema MUST include all 3 tables per spec §1 Q8 A schema sketch.
	for _, tbl := range []string{
		"research_dispatches",
		"research_findings",
		"research_validation_log",
	} {
		if !strings.Contains(bodyStr, tbl) {
			t.Errorf("ADR-0067 missing schema table %q (spec Q8 A schema sketch)", tbl)
		}
	}

	// TTL revalidation MUST cover per-source types per spec Q8 A.
	ttlTypes := []string{
		"7 day",
		"permanent",
		"1 day",
		"ETag",
	}
	for _, kw := range ttlTypes {
		if !strings.Contains(bodyStr, kw) {
			t.Errorf("ADR-0067 missing TTL revalidation keyword %q (spec Q8 A: per-source TTL table)", kw)
		}
	}

	// 6 Plan 8 typed events MUST be listed verbatim per spec §1 Q8 A.
	events := []string{
		"research.dispatch_initiated",
		"research.cache_hit_exact",
		"research.cache_hit_semantic",
		"research.cache_revalidated_fresh",
		"research.cache_revalidated_stale_refetched",
		"research.findings_returned",
	}
	for _, ev := range events {
		if !strings.Contains(bodyStr, ev) {
			t.Errorf("ADR-0067 missing research event %q (spec §1 Q8 A: 6 typed events)", ev)
		}
	}

	if !strings.Contains(bodyStr, "Plan 4") || !strings.Contains(bodyStr, "MCP") {
		t.Errorf("ADR-0067 missing Plan 4 Research MCP integration reference (spec Q8 A consumer)")
	}

	// inv-zen-088 (LLM via orchestrator — no direct provider calls from cache) MUST be cited.
	if !strings.Contains(bodyStr, "inv-zen-088") {
		t.Errorf("ADR-0067 missing inv-zen-088 reference (single-egress-LLM: research cache never calls LLM providers directly)")
	}

	// inv-zen-148 (dispatch metadata privacy — project_id filter mandatory) MUST be cited.
	if !strings.Contains(bodyStr, "inv-zen-148") {
		t.Errorf("ADR-0067 missing inv-zen-148 reference (dispatch metadata privacy: project_id filter mandatory)")
	}

	// inv-zen-152 (Plan 14 boundary — research cache stores, never dispatches ecosystem) MUST be cited.
	if !strings.Contains(bodyStr, "inv-zen-152") {
		t.Errorf("ADR-0067 missing inv-zen-152 reference (Plan 14 boundary: research cache stores, never dispatches)")
	}

	topic4Sources := []string{
		"tianpan.co",
		"stale memories",
		"2509.17360",
		"Asteria",
	}
	for _, src := range topic4Sources {
		if !strings.Contains(bodyStr, src) {
			t.Errorf("ADR-0067 missing Topic 4 SOTA citation %q (spec §8.1 Topic 4 verbatim)", src)
		}
	}

	// Cross-cutting theme #5 MUST be cited (content-addressing universal freshness primitive).
	if !strings.Contains(bodyStr, "content-addressing") && !strings.Contains(bodyStr, "Content-addressing") {
		t.Errorf("ADR-0067 missing cross-cutting theme #5 citation (content-addressing as universal freshness primitive)")
	}

	// Cross-cutting theme #1 MUST be cited (SQLite + sqlite-vec + FTS5 unified backend).
	if !strings.Contains(bodyStr, "cross-cutting theme #1") {
		t.Errorf("ADR-0067 missing cross-cutting theme #1 citation (SQLite + sqlite-vec + FTS5 unified backend)")
	}

	for _, doctrine := range []string{"max-scope", "default", "capa-firewall"} {
		if !strings.Contains(bodyStr, doctrine) {
			t.Errorf("ADR-0067 doctrine-alignment section missing %q", doctrine)
		}
	}

	for _, related := range []string{"ADR-0006", "ADR-0065"} {
		if !strings.Contains(bodyStr, related) {
			t.Errorf("ADR-0067 missing related-ADR reference %q (declared in frontmatter relates-to)", related)
		}
	}

	// Q5 A backup integration MUST be documented (Litestream + CAS lazy-refetch).
	if !strings.Contains(bodyStr, "Litestream") {
		t.Errorf("ADR-0067 missing Litestream reference (Q5 A backup integration: research_cache.db WAL replication)")
	}
	if !strings.Contains(bodyStr, "lazy-refetch") || !strings.Contains(bodyStr, "lazy refetch") {

		if !strings.Contains(bodyStr, "lazy-refetch") && !strings.Contains(bodyStr, "lazy refetch") {
			t.Errorf("ADR-0067 missing CAS lazy-refetch recovery semantics (Q5 A integration: blob missing → refetch on-demand)")
		}
	}

	// Privacy MUST state that findings are global (public-web) and dispatch is per-project.
	if !strings.Contains(bodyStr, "public-web") {
		t.Errorf("ADR-0067 missing 'public-web' citation (Topic 4 SOTA privacy: findings global because sources are public-web)")
	}

	wantRelatesTo := map[string]bool{"ADR-0006": false, "ADR-0065": false}
	for _, rel := range parsed.Frontmatter.RelatesTo {
		if _, ok := wantRelatesTo[rel]; ok {
			wantRelatesTo[rel] = true
		}
	}
	for adrID, found := range wantRelatesTo {
		if !found {
			t.Errorf("ADR-0067 frontmatter relates-to missing %q; got %v",
				adrID, parsed.Frontmatter.RelatesTo)
		}
	}
}
