// go:build integration
package plan18b_integration_test

import (
	"os/exec"
	"strings"
	"testing"
)

func TestPlan18bJInt1_PluginTreeLoadsFromPluginHades(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skipf("python3 not on PATH; cannot exercise plugin load via Python: %v", err)
	}

	pluginDir := pluginHadesDir(t)

	legacyPath := strings.Replace(pluginDir, "/plugin/hades", "/plugin/zen-swarm", 1)
	if _, err := exec.Command("test", "-d", legacyPath).CombinedOutput(); err == nil {
		t.Fatalf("legacy plugin path still exists: %s (Phase A git mv incomplete)", legacyPath)
	}

	script := `
import os
import sys
import re
plugin_dir = ` + quoteForPython(pluginDir) + `

# (1) plugin.yaml manifest declares name: hades.
with open(os.path.join(plugin_dir, "plugin.yaml")) as fh:
    manifest = fh.read()
print("HAS_NAME_HADES:%s" % bool(re.search(r"^name:\s*hades\b", manifest, re.MULTILINE)))

# (2) __init__.py contains 22 register_command calls all under hades: namespace.
# Calls may be multi-line: ctx.register_command(\n        "hades:..." so we
# scan the collapsed text (strip all whitespace between call and first string).
with open(os.path.join(plugin_dir, "__init__.py")) as fh:
    init_body = fh.read()
register_count = len(re.findall(r'ctx\.register_command\(', init_body))
# Collapse multi-line calls: match register_command( [whitespace] "hades:
hades_register_count = len(re.findall(r'ctx\.register_command\(\s*"hades:', init_body))
legacy_register_count = len(re.findall(r'ctx\.register_command\(\s*"zen-swarm:', init_body))
print("REGISTER_TOTAL:%d" % register_count)
print("REGISTER_HADES:%d" % hades_register_count)
print("REGISTER_LEGACY:%d" % legacy_register_count)

# (3) 13 SKILL.md files at skills/*/SKILL.md.
skills_root = os.path.join(plugin_dir, "skills")
skill_files = []
for entry in os.listdir(skills_root):
    full = os.path.join(skills_root, entry)
    if os.path.isdir(full) and os.path.isfile(os.path.join(full, "SKILL.md")):
        skill_files.append(entry)
print("SKILL_COUNT:%d" % len(skill_files))

# (4) 6 platform renderer files at renderers/*_citation.py.
renderers_root = os.path.join(plugin_dir, "renderers")
renderer_files = sorted(
    f for f in os.listdir(renderers_root)
    if f.endswith("_citation.py")
)
print("RENDERER_COUNT:%d" % len(renderer_files))
print("RENDERER_FILES:%s" % ",".join(renderer_files))

# (5) Attempt full module import if Hermes runtime is available.
# This is opportunistic — fails gracefully if hermes_plugins is not installed.
sys.path.insert(0, os.path.dirname(plugin_dir))
try:
    import hades as _hades_mod
    print("MODULE_IMPORTED:%s" % _hades_mod.__name__)
except ImportError as e:
    print("MODULE_IMPORT_SKIPPED:%s" % str(e))
`

	cmd := exec.Command("python3", "-c", script)
	cmd.Env = newSandboxEnv(t, "")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("python3 plugin-load probe failed: %v\noutput:\n%s", err, out)
	}
	s := string(out)

	wantMarkers := []string{
		"HAS_NAME_HADES:True",
		"REGISTER_LEGACY:0",
		"SKILL_COUNT:13",
		"RENDERER_COUNT:6",
		"_citation.py",
	}
	for _, m := range wantMarkers {
		if !strings.Contains(s, m) {
			t.Errorf("J-int-1 expected marker %q missing from python probe output:\n%s", m, s)
		}
	}

	total := markerInt(t, s, "REGISTER_TOTAL")
	hades := markerInt(t, s, "REGISTER_HADES")
	if hades != total {
		t.Errorf("J-int-1: REGISTER_HADES(%d) != REGISTER_TOTAL(%d) — every command must be under the hades: namespace", hades, total)
	}
	if total < 22 {
		t.Errorf("J-int-1: REGISTER_TOTAL(%d) below the Plan-18b floor of 22 — command surface shrank unexpectedly", total)
	}
	// Full import is opportunistic: log result for visibility but do not fail.
	if strings.Contains(s, "MODULE_IMPORTED:hades") {
		t.Logf("J-int-1 full module import succeeded (hermes_plugins available)")
	} else if strings.Contains(s, "MODULE_IMPORT_SKIPPED:") {
		t.Logf("J-int-1 full module import skipped (hermes_plugins not installed; filesystem probes passed)")
	}
}
