// go:build integration
//go:build integration
// +build integration

package plan18a_integration_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/cbip-solutions/hades-system/internal/migrate/mapping"
	"github.com/cbip-solutions/hades-system/internal/migrate/source"
	"github.com/cbip-solutions/hades-system/internal/migrate/writer"
)

func fixtureRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	return filepath.Join(wd, "fixtures", "claude-code-fixture")
}

func runMigratePipeline(t *testing.T, src, out string) (writer.WriterConfig, *mapping.Plan) {
	t.Helper()
	inv, err := source.ReadAll(src)
	if err != nil {
		t.Fatalf("source.ReadAll: %v", err)
	}
	plan, err := mapping.Map(inv, mapping.PresetLenient)
	if err != nil {
		t.Fatalf("mapping.Map: %v", err)
	}
	if len(plan.Entries) == 0 {
		t.Fatalf("plan empty — fixture is malformed")
	}
	cfg := writer.WriterConfig{
		HermesPluginRoot: filepath.Join(out, "plugin", "zen-swarm"),
		HermesConfigPath: filepath.Join(out, "hermes-config.yaml"),
		ZenConfigRoot:    filepath.Join(out, "zen-config"),
		ForceOverwrite:   true,
	}
	w := writer.New(cfg)
	if err := w.Apply(plan); err != nil {
		t.Fatalf("writer.Apply: %v", err)
	}
	return cfg, plan
}

func TestMigrateWriter_PluginTreeLoadable(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 unavailable; integration test skipped")
	}
	src := fixtureRoot(t)
	out := t.TempDir()
	cfg, _ := runMigratePipeline(t, src, out)
	pluginRoot := cfg.HermesPluginRoot

	initPy := filepath.Join(pluginRoot, "__init__.py")
	body, err := os.ReadFile(initPy)
	if err != nil {
		t.Fatalf("read __init__.py: %v", err)
	}

	if !strings.Contains(string(body), "from .commands.execute_plan import execute_plan_handler") {
		t.Errorf("__init__.py missing underscored execute_plan handler import:\n%s", body)
	}

	mustExist := []string{
		filepath.Join(pluginRoot, "commands", "execute_plan.py"),
		filepath.Join(pluginRoot, "commands", "execute_plan.md"),
		filepath.Join(pluginRoot, "commands", "write_plan.py"),
		filepath.Join(pluginRoot, "commands", "doctrine.py"),
	}
	for _, p := range mustExist {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected file missing: %s — %v", p, err)
		}
	}
	mustNotExist := []string{
		filepath.Join(pluginRoot, "commands", "execute-plan.py"),
		filepath.Join(pluginRoot, "commands", "write-plan.py"),
	}
	for _, p := range mustNotExist {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("hyphenated filename leaked through writer: %s", p)
		}
	}

	script := `
import sys
sys.path.insert(0, sys.argv[1])
import package_under_test
class StubCtx:
    def __init__(self):
        self.commands = []
        self.skills = []
        self.hooks = []
    def register_hook(self, *a, **kw): self.hooks.append((a, kw))
    def register_skill(self, *a, **kw): self.skills.append((a, kw))
    def register_command(self, *a, **kw): self.commands.append((a, kw))

ctx = StubCtx()
package_under_test.register(ctx)
print("CMDS=%d SKILLS=%d HOOKS=%d" % (len(ctx.commands), len(ctx.skills), len(ctx.hooks)))
`

	stage := t.TempDir()
	stagedPkg := filepath.Join(stage, "package_under_test")
	if err := os.Symlink(pluginRoot, stagedPkg); err != nil {
		t.Fatalf("symlink pluginRoot to package_under_test: %v", err)
	}
	cmd := exec.Command("python3", "-c", script, stage)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("python register() failed:\nstdout/stderr: %q\nerr: %v\n(rendered __init__.py:\n%s)", output, err, body)
	}
	outStr := string(output)
	if !strings.Contains(outStr, "CMDS=") || !strings.Contains(outStr, "SKILLS=") {
		t.Errorf("register() did not invoke ctx methods:\n%s", outStr)
	}

	if !strings.Contains(outStr, "SKILLS=2") && !strings.Contains(outStr, "SKILLS=3") {
		t.Logf("warning: skill count not 2-3 in output: %s", outStr)
	}
}

func TestMigrateWriter_HermesConfigHTTPTransport(t *testing.T) {
	src := fixtureRoot(t)
	out := t.TempDir()
	cfg, _ := runMigratePipeline(t, src, out)

	body, err := os.ReadFile(cfg.HermesConfigPath)
	if err != nil {
		t.Fatalf("read hermes-config.yaml: %v", err)
	}

	if strings.Contains(string(body), "command: zen-swarm-ctld") {
		t.Errorf("Bug 3 end-to-end: stdio form leaked through full pipeline:\n%s", body)
	}

	var parsed struct {
		MCPServers map[string]map[string]any `yaml:"mcp_servers"`
	}
	if err := yaml.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("YAML parse: %v\nbody:\n%s", err, body)
	}
	zs := parsed.MCPServers["zen-swarm"]
	if zs["transport"] != "http" {
		t.Errorf("zen-swarm transport = %v, want http", zs["transport"])
	}

	if _, present := parsed.MCPServers["gitnexus"]; present {
		t.Errorf("gitnexus entry must not be emitted post-caronte-cutover")
	}

	pw, hasPlaywright := parsed.MCPServers["playwright"]
	if !hasPlaywright {
		t.Errorf("playwright operator MCP dropped:\n%s", body)
	}
	if pw["command"] != "npx" {
		t.Errorf("playwright command corrupted: %v", pw)
	}

	var dp struct {
		DefaultProvider struct {
			Model string `yaml:"model"`
		} `yaml:"default_provider"`
	}
	if err := yaml.Unmarshal(body, &dp); err != nil {
		t.Fatalf("re-parse for default_provider: %v", err)
	}
	if dp.DefaultProvider.Model != "opus[1m]" {

		rawSettings, _ := os.ReadFile(filepath.Join(src, "settings.json"))
		var sj struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(rawSettings, &sj)
		t.Errorf("default_provider.model: got %q, want %q (fixture said %q)",
			dp.DefaultProvider.Model, "opus[1m]", sj.Model)
	}
}
