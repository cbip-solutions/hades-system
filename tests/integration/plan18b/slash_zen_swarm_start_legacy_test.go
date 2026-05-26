//go:build integration

package plan18b_integration_test

import (
	"os/exec"
	"strings"
	"testing"
)

// TestPlan18bJInt3_LegacyZenSwarmStartNotRegistered asserts J-int-3: the
// legacy slash command `/zen-swarm:start` is NOT registered post-Phase-B.
//
// Per spec §Q4 hard cutover: `/zen-swarm:*` removed; legacy invocations
// MUST hit Hermes' built-in command-not-found path. Plan 18b does NOT
// install a custom recovery-hint handler — that's Plan 18c's error catalog
// per spec §Q6.
//
// Strategy: Python subprocess scans __init__.py source text via regex (same
// source-text approach as J-int-1/J-int-2) to assert:
//   - "zen-swarm:start", "zen-swarm:handoff", "zen-swarm:install-mcps" NOT present.
//   - NO ctx.register_command call uses a "zen-swarm:" prefix.
//   - All 22 register_command calls use "hades:" prefix.
//
// NOTE(plan-15): Full runtime import requires hermes_plugins which is not available in
// CI. Source-text scanning is the correct granularity per the Plan 18b
// integration test strategy.
//
// Regression guard: this test catches a future agent re-introducing a
// custom legacy-alias handler ("for backward compat") — which would
// violate spec §Q4 hard cutover doctrine. Plan 18c will ADD curated
// command-not-found hints via internal/cli/error_render.go; Plan 18b
// leaves the legacy form purely unregistered.
func TestPlan18bJInt3_LegacyZenSwarmStartNotRegistered(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skipf("python3 not on PATH: %v", err)
	}

	pluginDir := pluginHadesDir(t)

	script := `
import os
import re
plugin_dir = ` + quoteForPython(pluginDir) + `

with open(os.path.join(plugin_dir, "__init__.py")) as fh:
    init_body = fh.read()

# Required absences (the 3 originally-namespaced Plan-1-era commands):
# Multi-line form: ctx.register_command(\n        "zen-swarm:..."
has_legacy_start = bool(re.search(r'ctx\.register_command\(\s*"zen-swarm:start"', init_body))
has_legacy_handoff = bool(re.search(r'ctx\.register_command\(\s*"zen-swarm:handoff"', init_body))
has_legacy_install_mcps = bool(re.search(r'ctx\.register_command\(\s*"zen-swarm:install-mcps"', init_body))
print("HAS_LEGACY_START:%s" % has_legacy_start)
print("HAS_LEGACY_HANDOFF:%s" % has_legacy_handoff)
print("HAS_LEGACY_INSTALL_MCPS:%s" % has_legacy_install_mcps)

# Defense-in-depth: count ALL zen-swarm: registrations.
legacy_matches = re.findall(r'ctx\.register_command\(\s*"(zen-swarm:[^"]+)"', init_body)
print("LEGACY_KEY_COUNT:%d" % len(legacy_matches))
if legacy_matches:
    print("LEGACY_KEYS_FOUND:%s" % ",".join(sorted(legacy_matches)))

# All 22 commands should be under hades: namespace.
non_hades_matches = re.findall(r'ctx\.register_command\(\s*"(?!hades:)([^"]+)"', init_body)
print("NON_HADES_KEY_COUNT:%d" % len(non_hades_matches))
if non_hades_matches:
    print("NON_HADES_KEYS_FOUND:%s" % ",".join(sorted(non_hades_matches)))

total_count = len(re.findall(r'ctx\.register_command\(', init_body))
hades_count = len(re.findall(r'ctx\.register_command\(\s*"hades:', init_body))
print("TOTAL_REGISTERED:%d" % total_count)
print("HADES_REGISTERED:%d" % hades_count)
`

	cmd := exec.Command("python3", "-c", script)
	cmd.Env = newSandboxEnv(t, "")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("python3 probe failed: %v\noutput:\n%s", err, out)
	}
	s := string(out)

	wantAbsenceMarkers := []string{
		"HAS_LEGACY_START:False",
		"HAS_LEGACY_HANDOFF:False",
		"HAS_LEGACY_INSTALL_MCPS:False",
		"LEGACY_KEY_COUNT:0",
		"NON_HADES_KEY_COUNT:0",
	}
	for _, m := range wantAbsenceMarkers {
		if !strings.Contains(s, m) {
			t.Errorf("J-int-3 required absence marker %q missing or violated in probe output:\n%s", m, s)
		}
	}

	if total := markerInt(t, s, "TOTAL_REGISTERED"); total < 22 {
		t.Errorf("J-int-3: TOTAL_REGISTERED(%d) below the Plan-18b floor of 22", total)
	}

	if strings.Contains(s, "LEGACY_KEYS_FOUND:") {
		for _, line := range strings.Split(s, "\n") {
			if strings.HasPrefix(line, "LEGACY_KEYS_FOUND:") {
				t.Errorf("J-int-3 legacy zen-swarm: registration found (regression): %s", line)
			}
		}
	}
}
