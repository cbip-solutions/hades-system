//go:build adversarial
// +build adversarial

package adversarial_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdversarial_PythonImportCorrectness(t *testing.T) {
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 unavailable on this runner; adversarial python-import check skipped")
	}

	src := t.TempDir()

	skillDir := filepath.Join(src, "skills", "hostile-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"),
		[]byte("# Plan 9: Audit infrastructure\n\nBody with colon in heading.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	hookDir := filepath.Join(src, "hooks")
	if err := os.MkdirAll(hookDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hookDir, "tool.execute.before.sh"),
		[]byte(`echo "Hello $USER"`), 0o755); err != nil {
		t.Fatal(err)
	}

	cmdDir := filepath.Join(src, "commands")
	if err := os.MkdirAll(cmdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	canary := filepath.Join(t.TempDir(), "rce-canary.txt")
	hostileCommand := fmt.Sprintf(`# /evil command

`+"```python\n"+`"""
import os
os.system("touch %s")
"""
`+"```"+`
`, canary)
	if err := os.WriteFile(filepath.Join(cmdDir, "evil.md"),
		[]byte(hostileCommand), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(src, "settings.json"),
		[]byte(`{"permissions":{"allow":["Read(*)"]},"model":"opus[1m]"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	target := t.TempDir()
	bin := buildZen(t)
	cmd := exec.Command(bin, "migrate", "claude-code",
		"--source", src,
		"--target-hermes", filepath.Join(target, "plugin", "zen-swarm"),
		"--target-config", filepath.Join(target, "hermes", "config.yaml"),
		"--target-zen-config", filepath.Join(target, "zen-config"),
		"--preset", "lenient",
		"--force")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("migrate failed: %v\n%s", err, out)
	}

	pluginRoot := filepath.Join(target, "plugin", "zen-swarm")

	compileCmd := exec.Command(py, "-c",
		"import sys, compileall; sys.exit(0 if compileall.compile_dir(sys.argv[1], quiet=1) else 1)",
		pluginRoot)
	if out, err := compileCmd.CombinedOutput(); err != nil {
		t.Errorf("compileall failed (generated .py not syntactically valid):\n%s\nplugin root: %s", out, pluginRoot)
	}

	_ = os.Remove(canary)

	// (3) Import plugin module — hostile payload MUST NOT execute.
	// We import the plugin's `__init__.py` (the entry point); also iterate
	// over commands/* and hooks/* modules.
	importScript := `
import sys, importlib, importlib.util, os, glob
plugin_root = sys.argv[1]
sys.path.insert(0, os.path.dirname(plugin_root))
pkg = os.path.basename(plugin_root)
try:
    importlib.import_module(pkg)
except Exception as e:
    print(f"WARN: package import failed: {e}", file=sys.stderr)
for f in glob.glob(os.path.join(plugin_root, "commands", "*.py")):
    spec = importlib.util.spec_from_file_location("evil_under_test", f)
    mod = importlib.util.module_from_spec(spec)
    try:
        spec.loader.exec_module(mod)
    except Exception as e:
        print(f"WARN: {f} exec failed: {e}", file=sys.stderr)
for f in glob.glob(os.path.join(plugin_root, "hooks", "*.py")):
    spec = importlib.util.spec_from_file_location("hook_under_test", f)
    mod = importlib.util.module_from_spec(spec)
    try:
        spec.loader.exec_module(mod)
    except Exception as e:
        print(f"WARN: {f} exec failed: {e}", file=sys.stderr)
`
	importCmd := exec.Command(py, "-c", importScript, pluginRoot)
	importOut, _ := importCmd.CombinedOutput()
	t.Logf("python import output:\n%s", importOut)

	if _, statErr := os.Stat(canary); statErr == nil {
		t.Errorf("RCE detected: hostile body executed at import time; canary file appeared at %s", canary)
		_ = os.Remove(canary)
	}

	hermesYAML := filepath.Join(target, "hermes", "config.yaml")
	yamlCmd := exec.Command(py, "-c",
		"import sys; yml = open(sys.argv[1]).read(); import yaml; yaml.safe_load(yml)",
		hermesYAML)
	if out, err := yamlCmd.CombinedOutput(); err != nil {

		if strings.Contains(string(out), "No module named 'yaml'") {
			t.Logf("PyYAML unavailable; skipping config.yaml round-trip check")
		} else {
			t.Errorf("hermes config.yaml failed Python YAML parse:\n%s", out)
		}
	}

	skillPath := filepath.Join(pluginRoot, "skills", "hostile-skill", "SKILL.md")
	if _, statErr := os.Stat(skillPath); statErr == nil {

		extractScript := `
import sys, re
body = open(sys.argv[1]).read()
m = re.match(r'^---\n(.*?)\n---\n', body, re.DOTALL)
if not m:
    print("no-frontmatter", file=sys.stderr); sys.exit(1)
try:
    import yaml
    parsed = yaml.safe_load(m.group(1))
    if 'description' not in parsed:
        print("missing-description", file=sys.stderr); sys.exit(2)
    print(parsed['description'])
except ImportError:
    print("pyyaml-unavailable")
    sys.exit(0)
`
		fmCmd := exec.Command(py, "-c", extractScript, skillPath)
		if out, err := fmCmd.CombinedOutput(); err != nil {
			t.Errorf("skill SKILL.md frontmatter failed to parse:\n%s", out)
		} else {
			s := strings.TrimSpace(string(out))
			if s == "pyyaml-unavailable" {
				t.Logf("PyYAML unavailable; skipping skill frontmatter round-trip check")
			} else if !strings.Contains(s, "Plan 9: Audit") {
				t.Errorf("skill description did NOT preserve colon-bearing literal heading: got %q", s)
			}
		}
	}
}

func buildZen(t *testing.T) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "zen")
	cwd, _ := os.Getwd()
	root, _ := filepath.Abs(filepath.Join(cwd, "..", ".."))
	cmd := exec.Command("go", "build",
		"-tags=sqlite_fts5",
		"-ldflags=-X github.com/ncruces/go-sqlite3/driver.driverName=sqlite3_ncruces",
		"-o", out, "./cmd/zen")
	cmd.Dir = root
	if buildOut, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build zen: %v\n%s", err, buildOut)
	}
	return out
}
