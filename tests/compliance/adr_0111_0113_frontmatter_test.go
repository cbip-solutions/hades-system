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

func TestADR0111_CaronteArchitecture(t *testing.T) {
	body, parsed := loadCaronteADR(t, "0111-caronte-architecture.md")
	if parsed.Frontmatter.ID != "ADR-0111" {
		t.Errorf("ID = %q; want ADR-0111", parsed.Frontmatter.ID)
	}
	if parsed.Frontmatter.Plan != "plan-19" {
		t.Errorf("Plan = %q; want plan-19", parsed.Frontmatter.Plan)
	}
	if parsed.Frontmatter.Status != "accepted" {
		t.Errorf("Status = %q; want accepted", parsed.Frontmatter.Status)
	}

	if !containsStr(parsed.Frontmatter.Supersedes, "ADR-0082") {
		t.Errorf("Frontmatter.Supersedes missing ADR-0082: %v", parsed.Frontmatter.Supersedes)
	}
	for _, rel := range []string{"ADR-0007", "ADR-0081", "ADR-0006"} {
		if !containsStr(parsed.Frontmatter.RelatesTo, rel) {
			t.Errorf("Frontmatter.RelatesTo missing %s", rel)
		}
	}
	bs := string(body)

	for _, frag := range []string{
		"native in-daemon", "per-project", "static-first", "confidence tier",
		"k-core", "Tarjan SCC", "package", "get_why", "Lore",
		"query-answering", "Caronte",
	} {
		if !strings.Contains(bs, frag) {
			t.Errorf("ADR-0111 body missing load-bearing fragment %q", frag)
		}
	}

	for _, frag := range []string{"k-core", "Leiden", "static", "engraph"} {
		if !strings.Contains(bs, frag) {
			t.Errorf("ADR-0111 body missing ADR-0082 correction fragment %q", frag)
		}
	}
	assertSectionsAndNoAttribution(t, "ADR-0111", bs)
}

func TestADR0112_BlastRadiusHooks(t *testing.T) {
	body, parsed := loadCaronteADR(t, "0112-blast-radius-integration-hooks.md")
	if parsed.Frontmatter.ID != "ADR-0112" {
		t.Errorf("ID = %q; want ADR-0112", parsed.Frontmatter.ID)
	}
	bs := string(body)

	for _, frag := range []string{
		"HRA", "L2", "L3", "merge", "ConfirmationPolicy", "chaos",
		"blast-radius", "risk", "inv-zen-235",
	} {
		if !strings.Contains(bs, frag) {
			t.Errorf("ADR-0112 body missing hook fragment %q", frag)
		}
	}
	assertSectionsAndNoAttribution(t, "ADR-0112", bs)
}

func TestADR0113_Plan20FederationSeam(t *testing.T) {
	body, parsed := loadCaronteADR(t, "0113-plan-20-federation-seam.md")
	if parsed.Frontmatter.ID != "ADR-0113" {
		t.Errorf("ID = %q; want ADR-0113", parsed.Frontmatter.ID)
	}
	bs := string(body)

	for _, frag := range []string{
		"Workspace", "FederatedQuery", "CrossRepoLink", "capa-firewall",
		"Plan 20", "decomposition", "D1", "inv-zen-241", "seam",
	} {
		if !strings.Contains(bs, frag) {
			t.Errorf("ADR-0113 body missing seam fragment %q", frag)
		}
	}

	if !containsStr(parsed.Frontmatter.RelatesTo, "ADR-0111") {
		t.Errorf("Frontmatter.RelatesTo missing ADR-0111: %v", parsed.Frontmatter.RelatesTo)
	}
	assertSectionsAndNoAttribution(t, "ADR-0113", bs)
}

func loadCaronteADR(t *testing.T, name string) ([]byte, *adr.ADR) {
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

func assertSectionsAndNoAttribution(t *testing.T, id, body string) {
	t.Helper()
	for _, hdr := range mandatorySections {
		if !strings.Contains(body, hdr) {
			t.Errorf("%s body missing section header %q", id, hdr)
		}
	}
	assertNoClaudeAttribution(t, id, body)
}

func containsStr(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
