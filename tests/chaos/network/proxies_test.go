//go:build chaos

package network

import (
	"slices"
	"testing"
)

func TestRegistryHasExactlyEightEdges(t *testing.T) {
	reg, err := LoadRegistryForTest()
	if err != nil {
		t.Skipf("Toxiproxy not available (run scripts/setup_toxiproxy_dev.sh); skipping: %v", err)
	}
	if got := len(reg.Edges); got != 8 {
		t.Fatalf("registry edges = %d, want 8", got)
	}
}

func TestRegistryEdgePortsUnique(t *testing.T) {
	reg, err := LoadRegistryForTest()
	if err != nil {
		t.Skipf("Toxiproxy config missing; skipping: %v", err)
	}
	ports := make([]string, 0, len(reg.Edges))
	for _, e := range reg.Edges {
		ports = append(ports, e.Listen)
	}
	slices.Sort(ports)
	for i := 1; i < len(ports); i++ {
		if ports[i] == ports[i-1] {
			t.Errorf("duplicate proxy listen port: %s", ports[i])
		}
	}
}

// TestRegistryEdgeNamesCanonical guards the canonical edge name set
// against drift. If a new daemon edge is added the registry MUST
// be updated AND the scenario count must be re-stated in spec §6.3.
// inv-zen-305 contract pin.
func TestRegistryEdgeNamesCanonical(t *testing.T) {
	reg, err := LoadRegistryForTest()
	if err != nil {
		t.Skipf("Toxiproxy config missing; skipping: %v", err)
	}
	want := []string{
		"hermes_plugin", "ctld", "providers_anthropic_paygo",
		"providers_gemini", "mcp_research", "mcp_budget",
		"mcp_audit", "sidecar_bypass",
	}
	got := make([]string, 0, len(reg.Edges))
	for name := range reg.Edges {
		got = append(got, name)
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("edge names drifted: got=%v want=%v", got, want)
	}
}
