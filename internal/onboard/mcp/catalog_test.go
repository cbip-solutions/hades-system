package mcp

import (
	"strings"
	"testing"
)

func TestCatalogHasNoGitnexusEntry(t *testing.T) {
	for _, e := range AllEntries() {
		if strings.EqualFold(e.Name, "gitnexus") {
			t.Errorf("onboard catalog still lists gitnexus MCP; Plan 19: in-process engine, no MCP entry")
		}
	}
}

func TestAllCatalogEntriesTiered(t *testing.T) {
	for _, e := range AllEntries() {
		if e.Tier == 0 {
			t.Errorf("MCP %q has Tier=0; programmer error", e.Name)
		}
		if e.Tier < TierMandatory || e.Tier > TierCatalog {
			t.Errorf("MCP %q has invalid Tier=%d; expected 1..4", e.Name, e.Tier)
		}
		if e.RiskTier == "" {
			t.Errorf("MCP %q has empty RiskTier; required by Q10=D doctrine eval", e.Name)
		}
		if e.Name == "" {
			t.Errorf("MCP entry has empty Name; programmer error: %+v", e)
		}
	}
}

func TestAssertAllTieredDoesNotPanicOnValid(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("AssertAllTiered panicked on valid catalog: %v", r)
		}
	}()
	AssertAllTiered()
}

func TestTierMandatoryMCPsPresent(t *testing.T) {
	want := []string{"zen-swarm-ctld"}
	got := make(map[string]bool)
	for _, e := range AllEntries() {
		if e.Tier == TierMandatory {
			got[e.Name] = true
		}
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("Tier 1 mandatory MCP %q missing from catalog", w)
		}
	}
}

func TestTierUniversalMCPsPresent(t *testing.T) {
	want := []string{"playwright", "filesystem", "github"}
	got := make(map[string]bool)
	for _, e := range AllEntries() {
		if e.Tier == TierUniversal {
			got[e.Name] = true
		}
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("Tier 2 universal MCP %q missing from catalog", w)
		}
	}
}

func TestTierSmartMCPsPresent(t *testing.T) {
	want := []string{"prisma-postgres", "sentry", "linear", "memory", "sequential-thinking"}
	got := make(map[string]bool)
	for _, e := range AllEntries() {
		if e.Tier == TierSmart {
			got[e.Name] = true
		}
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("Tier 3 smart MCP %q missing from catalog", w)
		}
	}
}

func TestTierCatalogMCPsPresent(t *testing.T) {
	want := []string{"sqlite", "graphql", "mysql", "openapi"}
	got := make(map[string]bool)
	for _, e := range AllEntries() {
		if e.Tier == TierCatalog {
			got[e.Name] = true
		}
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("Tier 4 catalog MCP %q missing from catalog", w)
		}
	}
}

func TestRiskTierValuesValid(t *testing.T) {
	valid := map[string]bool{"low": true, "medium": true, "high": true}
	for _, e := range AllEntries() {
		if !valid[strings.ToLower(e.RiskTier)] {
			t.Errorf("MCP %q has invalid RiskTier=%q; expected low|medium|high", e.Name, e.RiskTier)
		}
	}
}

func TestNamesUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, e := range AllEntries() {
		if seen[e.Name] {
			t.Errorf("catalog has duplicate name %q", e.Name)
		}
		seen[e.Name] = true
	}
}

func TestByNameLookup(t *testing.T) {
	if e, ok := ByName("zen-swarm-ctld"); !ok || e.Tier != TierMandatory {
		t.Errorf("ByName(zen-swarm-ctld): got %+v ok=%v, want Tier=1 ok=true", e, ok)
	}
	if _, ok := ByName("gitnexus"); ok {
		t.Error("ByName(gitnexus): got ok=true; gitnexus removed (Caronte in-process, Plan 19 L-4)")
	}
	if _, ok := ByName("nonexistent-mcp"); ok {
		t.Error("ByName(nonexistent): got ok=true, want false")
	}
}

func TestByTierGroups(t *testing.T) {
	cases := []struct {
		tier Tier
		want int
	}{
		{TierMandatory, 1},
		{TierUniversal, 3},
		{TierSmart, 5},
		{TierCatalog, 4},
	}
	for _, c := range cases {
		got := ByTier(c.tier)
		if len(got) != c.want {
			t.Errorf("ByTier(%s): got %d entries, want %d (%v)", c.tier, len(got), c.want, got)
		}
		for _, e := range got {
			if e.Tier != c.tier {
				t.Errorf("ByTier(%s) returned entry with tier=%s: %+v", c.tier, e.Tier, e)
			}
		}
	}
}

func TestAllEntriesIsDeepCopy(t *testing.T) {
	first := AllEntries()
	if len(first) == 0 {
		t.Fatal("AllEntries returned empty slice; catalog misconfigured")
	}
	originalName := first[0].Name
	first[0].Name = "mutated-by-caller"
	second := AllEntries()
	if second[0].Name != originalName {
		t.Errorf("AllEntries returned shared backing array; first[0] mutation leaked to second[0]: %q", second[0].Name)
	}
}

func TestTierString(t *testing.T) {
	cases := []struct {
		tier Tier
		want string
	}{
		{TierUnknown, "unknown"},
		{TierMandatory, "mandatory"},
		{TierUniversal, "universal"},
		{TierSmart, "smart-default"},
		{TierCatalog, "catalog"},
		{Tier(99), "unknown"},
	}
	for _, c := range cases {
		if got := c.tier.String(); got != c.want {
			t.Errorf("Tier(%d).String() = %q, want %q", c.tier, got, c.want)
		}
	}
}

func TestEveryEntryHasDescription(t *testing.T) {
	for _, e := range AllEntries() {
		if e.Description == "" {
			t.Errorf("MCP %q has empty Description; consumed by wizard customize step", e.Name)
		}
	}
}

func TestEveryEntryHasPackage(t *testing.T) {
	for _, e := range AllEntries() {
		if e.Package == "" {
			t.Errorf("MCP %q has empty Package; consumed by Phase D install logic", e.Name)
		}
	}
}

func TestAssertAllTieredPanicCases(t *testing.T) {
	cases := []struct {
		name    string
		entries []Entry
		wantSub string
	}{
		{
			name:    "empty_name",
			entries: []Entry{{Name: "", Tier: TierMandatory, RiskTier: "low"}},
			wantSub: "empty Name",
		},
		{
			name:    "tier_unknown",
			entries: []Entry{{Name: "x", Tier: TierUnknown, RiskTier: "low"}},
			wantSub: "TierUnknown",
		},
		{
			name:    "tier_out_of_range_high",
			entries: []Entry{{Name: "x", Tier: Tier(99), RiskTier: "low"}},
			wantSub: "out-of-range Tier",
		},
		{
			name:    "tier_out_of_range_negative",
			entries: []Entry{{Name: "x", Tier: Tier(-1), RiskTier: "low"}},
			wantSub: "out-of-range Tier",
		},
		{
			name:    "empty_risk_tier",
			entries: []Entry{{Name: "x", Tier: TierMandatory, RiskTier: ""}},
			wantSub: "empty RiskTier",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatalf("assertAllTiered did not panic on invalid entries: %+v", c.entries)
				}
				msg, ok := r.(string)
				if !ok {
					t.Fatalf("panic value not a string: %T(%v)", r, r)
				}
				if !strings.Contains(msg, c.wantSub) {
					t.Errorf("panic message %q does not contain %q", msg, c.wantSub)
				}
			}()
			assertAllTiered(c.entries)
		})
	}
}

func TestAssertAllTieredEmptySlice(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("assertAllTiered([]) panicked: %v", r)
		}
	}()
	assertAllTiered(nil)
	assertAllTiered([]Entry{})
}

func TestPhase18bH_CatalogDescriptionsHADESBranding(t *testing.T) {
	cat := AllEntries()
	if len(cat) == 0 {
		t.Fatal("AllEntries() returned empty; want non-empty catalog entries")
	}
	for i, entry := range cat {
		i, entry := i, entry
		t.Run(entry.Name, func(t *testing.T) {

			if entry.Name == "zen-swarm-ctld" {
				if !strings.HasPrefix(entry.Description, "HADES daemon gateway") {
					t.Errorf("entry[%d=%q].Description = %q; want prefix \"HADES daemon gateway\" per Plan 18b Phase H H-4 (spec §Q3 IN)",
						i, entry.Name, entry.Description)
				}
			}

			if strings.HasPrefix(entry.Description, "zen-swarm ") {
				t.Errorf("entry[%d=%q].Description = %q starts with legacy brand subject; rebrand per spec §Q3 IN",
					i, entry.Name, entry.Description)
			}
		})
	}
}
