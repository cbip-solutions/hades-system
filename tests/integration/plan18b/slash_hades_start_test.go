//go:build integration

package plan18b_integration_test

import (
	"os/exec"
	"strings"
	"testing"
)

func TestPlan18bJInt2_SlashHadesStartResolves(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skipf("python3 not on PATH; cannot exercise /hades:start handler: %v", err)
	}

	pluginDir := pluginHadesDir(t)

	script := `
import os
import re
plugin_dir = ` + quoteForPython(pluginDir) + `

# (1) __init__.py registers hades:start (multi-line form).
with open(os.path.join(plugin_dir, "__init__.py")) as fh:
    init_body = fh.read()
hades_start_registered = bool(re.search(r'ctx\.register_command\(\s*"hades:start"', init_body))
registered_count = len(re.findall(r'ctx\.register_command\(', init_body))
print("HAS_HADES_START:%s" % hades_start_registered)
print("REGISTERED_COUNT:%d" % registered_count)

# (2) start.py handler source contains HADES brand.
start_path = os.path.join(plugin_dir, "commands", "start.py")
with open(start_path) as fh:
    start_body = fh.read()
has_hades_brand = bool(re.search(r'\bHADES\b', start_body))
# Legacy brand check: bare "zen-swarm" not in carve-outs.
stripped = start_body.replace("zen-swarm-ctld", "").replace("(formerly zen-swarm)", "")
has_legacy_bare = "zen-swarm" in stripped
print("HAS_HADES_BRAND_IN_HANDLER:%s" % has_hades_brand)
print("HAS_LEGACY_BRAND_IN_HANDLER:%s" % has_legacy_bare)
`

	cmd := exec.Command("python3", "-c", script)
	cmd.Env = newSandboxEnv(t, "")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("python3 probe failed: %v\noutput:\n%s", err, out)
	}
	s := string(out)

	wantMarkers := []string{
		"HAS_HADES_START:True",
	}
	for _, m := range wantMarkers {
		if !strings.Contains(s, m) {
			t.Fatalf("J-int-2 required marker %q missing from probe output:\n%s", m, s)
		}
	}

	if !strings.Contains(s, "HAS_HADES_BRAND_IN_HANDLER:True") {
		t.Errorf("J-int-2 expected HAS_HADES_BRAND_IN_HANDLER:True; handler source missing HADES wordmark:\n%s", s)
	}
	if strings.Contains(s, "HAS_LEGACY_BRAND_IN_HANDLER:True") {
		t.Errorf("J-int-2 unexpected HAS_LEGACY_BRAND_IN_HANDLER:True (legacy zen-swarm string in handler source):\n%s", s)
	}
}
