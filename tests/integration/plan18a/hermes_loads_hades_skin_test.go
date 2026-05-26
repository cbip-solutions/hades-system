//go:build integration

package plan18a_integration_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlan18aFoundation_HadesSkinPythonImport(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("python3"); err != nil {
		t.Skipf("python3 not on PATH; cannot exercise HADES skin via Python: %v", err)
	}
	root := repoRoot(t)
	pluginDir := filepath.Join(root, "plugin", "hades")

	script := `
import sys
sys.path.insert(0, ` + quoteForPython(pluginDir) + `)
from skins import hades
assert hades._SKIN_NAME == "hades", "skin name mismatch: %r" % (hades._SKIN_NAME,)
assert callable(hades.register_hades_skin), "register_hades_skin missing"
assert callable(hades._maybe_activate_hades), "_maybe_activate_hades missing"
assert callable(hades._build_hades_yaml), "_build_hades_yaml missing"
yaml_content = hades._build_hades_yaml()
print("YAML_LEN:%d" % len(yaml_content))
print("HAS_NAME_KEY:%s" % ("name: hades" in yaml_content))
print("HAS_BANNER_LOGO:%s" % ("banner_logo: |" in yaml_content))
print("HAS_BANNER_HERO:%s" % ("banner_hero:" in yaml_content))
print("HAS_COLORS_KEY:%s" % ("colors:" in yaml_content))
print("HAS_BRANDING_KEY:%s" % ("branding:" in yaml_content))
print("HAS_WORDMARK_REGION:%s" % ("HADES" in yaml_content or any(c in yaml_content for c in "█")))
print("HAS_BIDENT_GLYPH:%s" % ("┳" in yaml_content))
`
	cmd := exec.Command("python3", "-c", script)

	cmd.Env = newSandboxEnv(t, "")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("python3 import failed: %v\noutput:\n%s", err, out)
	}
	s := string(out)

	wantTrueMarkers := []string{
		"HAS_NAME_KEY:True",
		"HAS_BANNER_LOGO:True",
		"HAS_BANNER_HERO:True",
		"HAS_COLORS_KEY:True",
		"HAS_BRANDING_KEY:True",
		"HAS_WORDMARK_REGION:True",
		"HAS_BIDENT_GLYPH:True",
	}
	for _, m := range wantTrueMarkers {
		if !strings.Contains(s, m) {
			t.Errorf("expected marker %q missing from python probe output:\n%s", m, s)
		}
	}

	if !strings.Contains(s, "YAML_LEN:") {
		t.Fatalf("python probe did not emit YAML_LEN marker:\n%s", s)
	}
}

func TestPlan18aFoundation_HadesSkinImportIsClosure(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("python3"); err != nil {
		t.Skipf("python3 not on PATH; cannot exercise inv-zen-218 dynamic closure: %v", err)
	}
	root := repoRoot(t)
	pluginDir := filepath.Join(root, "plugin", "hades")

	script := `
import sys
forbidden = {"socket", "urllib", "urllib.request", "urllib.parse", "requests", "http.client"}
before = set(sys.modules.keys())
sys.path.insert(0, ` + quoteForPython(pluginDir) + `)
from skins import hades  # noqa: F401
after = set(sys.modules.keys())
new_modules = after - before
leaked = sorted(forbidden & new_modules)
print("LEAKED:%s" % ",".join(leaked))
`
	cmd := exec.Command("python3", "-c", script)
	cmd.Env = newSandboxEnv(t, "")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("python3 closure check failed: %v\noutput:\n%s", err, out)
	}
	s := string(out)

	if !strings.Contains(s, "LEAKED:\n") && !strings.HasSuffix(strings.TrimSpace(s), "LEAKED:") {

		for _, line := range strings.Split(s, "\n") {
			if strings.HasPrefix(line, "LEAKED:") {
				if line != "LEAKED:" {
					t.Errorf("inv-zen-218 dynamic closure violated: forbidden modules imported as side-effect: %q\nfull output:\n%s", line, s)
				}
				return
			}
		}
		t.Errorf("LEAKED marker missing from probe output:\n%s", s)
	}
}

func quoteForPython(s string) string {

	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	return "'" + s + "'"
}
