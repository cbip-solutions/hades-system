package builtin_test

import (
	"reflect"
	"sort"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/doctrine/builtin"
)

func TestGatewayMaxScopeNoToolsDisabled(t *testing.T) {
	s := builtin.MaxScope()
	if s == nil {
		t.Fatal("MaxScope() returned nil")
	}
	if got := len(s.Gateway.DisabledTools); got != 0 {
		t.Errorf("max-scope Gateway.DisabledTools len = %d; want 0; got %v",
			got, s.Gateway.DisabledTools)
	}
}

func TestGatewayDefaultNoToolsDisabled(t *testing.T) {
	s := builtin.Default()
	if s == nil {
		t.Fatal("Default() returned nil")
	}
	if got := len(s.Gateway.DisabledTools); got != 0 {
		t.Errorf("default Gateway.DisabledTools len = %d; want 0; got %v",
			got, s.Gateway.DisabledTools)
	}
}

func TestGatewayCapaFirewallDeniesAgentic(t *testing.T) {
	s := builtin.CapaFirewall()
	if s == nil {
		t.Fatal("CapaFirewall() returned nil")
	}
	want := []string{
		"mcp_zen-swarm_caronte_context",
		"mcp_zen-swarm_caronte_impact",
		"mcp_zen-swarm_caronte_query",
		"mcp_zen-swarm_research_agentic",
	}
	got := append([]string(nil), s.Gateway.DisabledTools...)

	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("capa-firewall Gateway.DisabledTools = %v; want %v", got, want)
	}
}

func TestGatewayDisabledToolsCanonicalForm(t *testing.T) {
	for _, name := range builtin.Names() {
		var entries []string
		switch name {
		case "max-scope":
			entries = builtin.MaxScope().Gateway.DisabledTools
		case "default":
			entries = builtin.Default().Gateway.DisabledTools
		case "capa-firewall":
			entries = builtin.CapaFirewall().Gateway.DisabledTools
		}
		const prefix = "mcp_zen-swarm_"
		for _, e := range entries {
			if len(e) <= len(prefix) || e[:len(prefix)] != prefix {
				t.Errorf("%s: Gateway.DisabledTools entry %q missing %q prefix",
					name, e, prefix)
			}
		}
	}
}
