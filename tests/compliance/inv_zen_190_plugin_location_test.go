package compliance_test

import (
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/onboard/plugin"
)

func TestInvZen190PluginLocationResolvedSpikePass(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ZEN_REPO_ROOT_OVERRIDE", tmp)
	loc, err := plugin.ResolveLocation(true)
	if err != nil {
		t.Fatalf("ResolveLocation(true): %v", err)
	}
	if loc.Kind != plugin.LocationKindProjectScope {
		t.Errorf("inv-zen-190: spike PASS → Kind=%v, want LocationKindProjectScope", loc.Kind)
	}
	wantSuffix := "/.hermes/plugins/zen-swarm"
	if !strings.HasSuffix(loc.Path, wantSuffix) {
		t.Errorf("inv-zen-190: spike PASS → Path=%q, want suffix %q", loc.Path, wantSuffix)
	}
}

func TestInvZen190PluginLocationResolvedSpikeFail(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("ZEN_REPO_ROOT_OVERRIDE", "/tmp/myproj-test")
	loc, err := plugin.ResolveLocation(false)
	if err != nil {
		t.Fatalf("ResolveLocation(false): %v", err)
	}
	if loc.Kind != plugin.LocationKindUserScope {
		t.Errorf("inv-zen-190: spike FAIL → Kind=%v, want LocationKindUserScope", loc.Kind)
	}
	if !strings.Contains(loc.Path, ".hermes/plugins/zen-swarm-") {
		t.Errorf("inv-zen-190: spike FAIL → Path=%q, want pattern .hermes/plugins/zen-swarm-<slug>", loc.Path)
	}
}
