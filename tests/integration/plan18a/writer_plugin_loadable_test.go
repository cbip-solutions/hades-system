// go:build integration
package plan18a_integration_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/migrate/mapping"
	"github.com/cbip-solutions/hades-system/internal/migrate/source"
	"github.com/cbip-solutions/hades-system/internal/migrate/writer"
)

func TestPlan18aFoundation_WriterPlusSkinComposition(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skipf("python3 unavailable; integration test skipped: %v", err)
	}

	binDir := t.TempDir()
	root := repoRoot(t)
	src := filepath.Join(root, "tests", "integration", "plan18a", "fixtures", "claude-code-fixture")
	out := t.TempDir()

	inv, err := source.ReadAll(src)
	if err != nil {
		t.Fatalf("source.ReadAll: %v", err)
	}
	plan, err := mapping.Map(inv, mapping.PresetLenient)
	if err != nil {
		t.Fatalf("mapping.Map: %v", err)
	}
	cfg := writer.WriterConfig{
		HermesPluginRoot: filepath.Join(out, "plugin", "zen-swarm"),
		HermesConfigPath: filepath.Join(out, "hermes-config.yaml"),
		ZenConfigRoot:    filepath.Join(out, "zen-config"),
		ForceOverwrite:   true,
	}
	if err := writer.New(cfg).Apply(plan); err != nil {
		t.Fatalf("writer.Apply: %v", err)
	}

	pluginRoot := cfg.HermesPluginRoot

	stage := t.TempDir()
	stagedPkg := filepath.Join(stage, "rendered_plugin")
	if err := os.Symlink(pluginRoot, stagedPkg); err != nil {
		t.Fatalf("symlink rendered plugin: %v", err)
	}

	skinSearchDir := filepath.Join(root, "plugin", "hades")

	script := `
import sys
sys.path.insert(0, ` + quoteForPython(stage) + `)        # for ` + "`rendered_plugin`" + `
sys.path.insert(0, ` + quoteForPython(skinSearchDir) + `) # for ` + "`skins.hades`" + `

# Stub-PluginContext for register() — Bug 1 fix end-to-end.
class StubCtx:
    def __init__(self):
        self.commands = []
        self.skills = []
        self.hooks = []
    def register_hook(self, *a, **kw): self.hooks.append((a, kw))
    def register_skill(self, *a, **kw): self.skills.append((a, kw))
    def register_command(self, *a, **kw): self.commands.append((a, kw))

# 1. Render-side: rendered plugin imports cleanly + register() runs.
import rendered_plugin
ctx = StubCtx()
rendered_plugin.register(ctx)
print("RENDERED_OK:cmds=%d,skills=%d,hooks=%d" % (
    len(ctx.commands), len(ctx.skills), len(ctx.hooks)))

# 2. Skin-side: Phase B's skin module imports cleanly + _build_hades_yaml
#    produces non-empty output.
from skins import hades
yaml_body = hades._build_hades_yaml()
print("SKIN_OK:yaml_len=%d" % len(yaml_body))
print("SKIN_NAME:%s" % hades._SKIN_NAME)
print("SKIN_HAS_BIDENT:%s" % ("┳" in yaml_body))

# 3. Composition guard: both imports share the same Python session WITHOUT
#    name collision. The render-side has no module named "skins"; the
#    skin-side has no module named "rendered_plugin". The two roots are
#    disjoint.
assert "skins" in sys.modules, "skins module missing from sys.modules"
assert "rendered_plugin" in sys.modules, "rendered_plugin module missing"
print("COMPOSITION_OK")
`
	cmd := exec.Command("python3", "-c", script)
	cmd.Env = newSandboxEnv(t, binDir)
	probeOut, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("composition probe failed: %v\noutput:\n%s", err, probeOut)
	}
	s := string(probeOut)

	wantMarkers := []string{
		"RENDERED_OK:",
		"SKIN_OK:yaml_len=",
		"SKIN_NAME:hades",
		"SKIN_HAS_BIDENT:True",
		"COMPOSITION_OK",
	}
	for _, m := range wantMarkers {
		if !strings.Contains(s, m) {
			t.Errorf("expected marker %q missing from probe output:\n%s", m, s)
		}
	}
}

func TestPlan18aFoundation_WriterEnforcesInvZen217(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	src := filepath.Join(root, "tests", "integration", "plan18a", "fixtures", "claude-code-fixture")
	out := t.TempDir()

	inv, err := source.ReadAll(src)
	if err != nil {
		t.Fatalf("source.ReadAll: %v", err)
	}
	plan, err := mapping.Map(inv, mapping.PresetLenient)
	if err != nil {
		t.Fatalf("mapping.Map: %v", err)
	}
	cfg := writer.WriterConfig{
		HermesPluginRoot: filepath.Join(out, "plugin", "zen-swarm"),
		HermesConfigPath: filepath.Join(out, "hermes-config.yaml"),
		ZenConfigRoot:    filepath.Join(out, "zen-config"),
		ForceOverwrite:   true,
	}
	if err := writer.New(cfg).Apply(plan); err != nil {
		t.Fatalf("writer.Apply: %v", err)
	}

	body, err := os.ReadFile(cfg.HermesConfigPath)
	if err != nil {
		t.Fatalf("read hermes-config.yaml: %v", err)
	}
	s := string(body)

	// invariant: stdio form for zen-swarm-ctld must NOT appear; HTTP
	// transport markers MUST appear.
	if strings.Contains(s, "command: zen-swarm-ctld") {
		t.Errorf("inv-zen-217 violation: stdio form leaked through pipeline:\n%s", s)
	}
	if !strings.Contains(s, "transport: http") {
		t.Errorf("inv-zen-217: missing 'transport: http' marker (writer post-C-6 fix):\n%s", s)
	}
	if !strings.Contains(s, "http://unix/v1/mcpgateway") {
		t.Errorf("inv-zen-217: missing 'http://unix/v1/mcpgateway' URL (writer post-C-6 fix):\n%s", s)
	}
}
