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

func TestADR0114_Plan20Architecture(t *testing.T) {
	body, parsed := loadFederationADR(t, "0114-plan-20-architecture.md")
	if parsed.Frontmatter.ID != "ADR-0114" {
		t.Errorf("ID = %q; want ADR-0114", parsed.Frontmatter.ID)
	}
	if parsed.Frontmatter.Plan != "plan-20" {
		t.Errorf("Plan = %q; want plan-20", parsed.Frontmatter.Plan)
	}
	if string(parsed.Frontmatter.Status) != "accepted" {
		t.Errorf("Status = %q; want accepted", parsed.Frontmatter.Status)
	}

	for _, rel := range []string{"ADR-0113", "ADR-0112", "ADR-0111"} {
		if !containsStr(parsed.Frontmatter.RelatesTo, rel) {
			t.Errorf("Frontmatter.RelatesTo missing %s: %v", rel, parsed.Frontmatter.RelatesTo)
		}
	}
	bs := string(body)

	for _, frag := range []string{
		"extractor registry", "caronte.yaml", "breaking-change",
		"oasdiff", "buf", "gqlparser",
		"workspace.db", "daemon-state",
		"Tessera", "Lore",
		"federation", "capa-firewall",
		"confidence", "unresolved",
	} {
		if !strings.Contains(bs, frag) {
			t.Errorf("ADR-0114 body missing load-bearing fragment %q", frag)
		}
	}

	for _, frag := range []string{"D6", "D7", "D8", "D10"} {
		if !strings.Contains(bs, frag) {
			t.Errorf("ADR-0114 body missing decision-record fragment %q", frag)
		}
	}
	assertSectionsAndNoAttribution(t, "ADR-0114", bs)
}

func TestADR0115_L10Decoupling(t *testing.T) {
	body, parsed := loadFederationADR(t, "0115-l10-decoupling-from-f7-hooks.md")
	if parsed.Frontmatter.ID != "ADR-0115" {
		t.Errorf("ID = %q; want ADR-0115", parsed.Frontmatter.ID)
	}
	if parsed.Frontmatter.Plan != "plan-20" {
		t.Errorf("Plan = %q; want plan-20", parsed.Frontmatter.Plan)
	}

	for _, rel := range []string{"ADR-0114", "ADR-0112"} {
		if !containsStr(parsed.Frontmatter.RelatesTo, rel) {
			t.Errorf("Frontmatter.RelatesTo missing %s: %v", rel, parsed.Frontmatter.RelatesTo)
		}
	}
	bs := string(body)

	for _, frag := range []string{
		"N-4", "nil engine", "main.go", "F.7 wires",
		"HRA", "NewConfirmationPolicy", "never constructed",
	} {
		if !strings.Contains(bs, frag) {
			t.Errorf("ADR-0115 body missing Plan 19 N-4 finding fragment %q", frag)
		}
	}

	for _, frag := range []string{
		"D5", "internal/caronte/coordinated", "seam_contractfix.go",
		"D9", "capability-detect", "WorktreePool",
		"ModeAutonomy", "ModeSurface",
		"one-way", "idempotent",
	} {
		if !strings.Contains(bs, frag) {
			t.Errorf("ADR-0115 body missing D5/D9 decision fragment %q", frag)
		}
	}

	if !strings.Contains(bs, "inv-zen-270") {
		t.Error("ADR-0115 body missing inv-zen-270 (HRA/ConfirmationPolicy/MergeEngine no-import boundary)")
	}
	assertSectionsAndNoAttribution(t, "ADR-0115", bs)
}

func TestADR0116_FederationObservability(t *testing.T) {
	body, parsed := loadFederationADR(t, "0116-federation-observability-via-tessera.md")
	if parsed.Frontmatter.ID != "ADR-0116" {
		t.Errorf("ID = %q; want ADR-0116", parsed.Frontmatter.ID)
	}
	if parsed.Frontmatter.Plan != "plan-20" {
		t.Errorf("Plan = %q; want plan-20", parsed.Frontmatter.Plan)
	}

	for _, rel := range []string{"ADR-0114", "ADR-0065"} {
		if !containsStr(parsed.Frontmatter.RelatesTo, rel) {
			t.Errorf("Frontmatter.RelatesTo missing %s: %v", rel, parsed.Frontmatter.RelatesTo)
		}
	}
	bs := string(body)

	for _, frag := range []string{
		"plan20.cross_repo_link",
		"plan20.breaking_change",
		"plan20.coordinated_dispatch",
		"plan20.federated_query_denied",
	} {
		if !strings.Contains(bs, frag) {
			t.Errorf("ADR-0116 body missing EventType constant %q", frag)
		}
	}

	for _, frag := range []string{
		"Tessera", "AppendLeaf", "EmitAudit",
		"hash-chained", "append-only",
		"single-egress", "single chokepoint",
		"chain-verify",
		"inv-zen-269",
	} {
		if !strings.Contains(bs, frag) {
			t.Errorf("ADR-0116 body missing fragment %q", frag)
		}
	}
	assertSectionsAndNoAttribution(t, "ADR-0116", bs)
}

func loadFederationADR(t *testing.T, name string) ([]byte, *adr.ADR) {
	t.Helper()
	root := findRepoRoot(t)
	p := filepath.Join(root, "docs", "decisions", name)
	body, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	v, err := adr.NewValidator(filepath.Join(root, "docs", "decisions", "_schema.json"))
	if err != nil {
		t.Fatalf("load validator: %v", err)
	}
	parsed, err := v.ParseAndValidate(p, body)
	if err != nil {
		t.Fatalf("%s schema violation: %v", name, err)
	}
	return body, parsed
}
