// tests/integration/plugin_lifecycle/hermes_plugin_loads_test.go
// go:build integration
package plugin_lifecycle_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHermesBinaryAvailable(t *testing.T) {
	if _, err := exec.LookPath("hermes"); err != nil {
		t.Skipf("hermes binary not on PATH (Phase H' execution prerequisite); "+
			"install via 'brew install hermes-agent'. err: %v", err)
	}
	cmd := exec.Command("hermes", "--version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("hermes --version failed: %v\noutput: %s", err, out)
	}
	t.Logf("hermes version: %s", strings.TrimSpace(string(out)))
}

func TestHermesLoadsZenPlugin(t *testing.T) {
	if _, err := exec.LookPath("hermes"); err != nil {
		t.Skip("hermes not on PATH")
	}

	root, err := repoRoot()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	pluginSourcePath := filepath.Join(root, "plugin", "hades")

	posterBin := filepath.Join(pluginSourcePath, "bin", "zen-event-poster")
	if _, err := os.Stat(posterBin); err != nil {
		t.Fatalf("zen-event-poster not built; run 'make plugin' first: %v", err)
	}

	hermesHome := t.TempDir()
	pluginsDir := filepath.Join(hermesHome, "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		t.Fatalf("mkdir plugins: %v", err)
	}
	pluginInstallPath := filepath.Join(pluginsDir, "zen-swarm")
	if err := os.Symlink(pluginSourcePath, pluginInstallPath); err != nil {
		t.Fatalf("symlink plugin into temp home: %v", err)
	}

	configPath := filepath.Join(hermesHome, "config.yaml")
	configBody := []byte("plugins:\n  enabled:\n    - zen-swarm\n")
	if err := os.WriteFile(configPath, configBody, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	env := append(os.Environ(),
		"HERMES_HOME="+hermesHome,

		"ZEN_HOOK_DRY_RUN=1",
	)

	cmd := exec.CommandContext(ctx, "hermes", "plugins", "list", "--json")
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {

		cmdText := exec.CommandContext(ctx, "hermes", "plugins", "list")
		cmdText.Env = env
		outText, errText := cmdText.CombinedOutput()
		if errText != nil {
			t.Fatalf("hermes plugins list failed both --json and plain: %v / %v\n"+
				"json output: %s\ntext output: %s", err, errText, out, outText)
		}

		if !strings.Contains(string(outText), "zen-swarm") {
			t.Errorf("hermes plugins list (plain) did not list zen-swarm:\n%s", outText)
		}
		return
	}

	var plugins []map[string]any
	if err := json.Unmarshal(out, &plugins); err != nil {

		var wrapped map[string]any
		if err2 := json.Unmarshal(out, &wrapped); err2 == nil {
			if list, ok := wrapped["plugins"].([]any); ok {
				for _, item := range list {
					if m, ok := item.(map[string]any); ok {
						plugins = append(plugins, m)
					}
				}
			}
		}
		if len(plugins) == 0 {
			t.Skipf("could not parse `hermes plugins list --json` output "+
				"(shape may vary across versions); raw: %s", out)
		}
	}

	found := false
	for _, p := range plugins {
		name, _ := p["name"].(string)
		if name == "zen-swarm" {
			found = true
			if errStr, ok := p["error"].(string); ok && errStr != "" {
				t.Errorf("zen-swarm plugin loaded with error: %s", errStr)
			}
			break
		}
	}
	if !found {
		t.Errorf("zen-swarm plugin not found in `hermes plugins list --json`:\n%s", out)
	}
}

func TestHermesDirectImportOfRegister(t *testing.T) {

	pythonCandidates := []string{
		"/opt/homebrew/Cellar/hermes-agent/2026.5.7/libexec/bin/python3",
		"python3.14", "python3.13", "python3.12", "python3.11", "python3",
	}
	var python string
	for _, candidate := range pythonCandidates {
		if _, err := exec.LookPath(candidate); err == nil {
			python = candidate
			break
		}
		if _, err := os.Stat(candidate); err == nil {
			python = candidate
			break
		}
	}
	if python == "" {
		t.Skip("no python3 available for direct register(ctx) probe")
	}

	root, err := repoRoot()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	pluginPath := filepath.Join(root, "plugin", "hades")

	snippet := `
import importlib.util
import json
import sys
import types
from pathlib import Path

plugin_dir = Path(sys.argv[1])

# Mirror hermes_cli/plugins.py:1042-1064 loader pattern:
# 1) Create hermes_plugins namespace package
ns_parent = "hermes_plugins"
if ns_parent not in sys.modules:
    ns_pkg = types.ModuleType(ns_parent)
    ns_pkg.__path__ = []
    ns_pkg.__package__ = ns_parent
    sys.modules[ns_parent] = ns_pkg

# 2) Load the plugin as hermes_plugins.hades with full package context
module_name = f"{ns_parent}.hades"
spec = importlib.util.spec_from_file_location(
    module_name,
    plugin_dir / "__init__.py",
    submodule_search_locations=[str(plugin_dir)],
)
mod = importlib.util.module_from_spec(spec)
mod.__package__ = module_name
mod.__path__ = [str(plugin_dir)]
sys.modules[module_name] = mod
spec.loader.exec_module(mod)

# Fake ctx that records calls
class FakeCtx:
    def __init__(self):
        self.manifest = type("M", (), {"name": "zen-swarm"})()
        self.hooks = []
        self.skills = []
        self.commands = []
    def register_hook(self, name, cb): self.hooks.append(name)
    def register_skill(self, name, path, description=""): self.skills.append(name)
    def register_command(self, name, handler, description="", args_hint=""): self.commands.append(name)
    def register_cli_command(self, *a, **kw): pass
    def register_tool(self, name, **kw): pass

ctx = FakeCtx()
mod.register(ctx)
print(json.dumps({"hooks": ctx.hooks, "skills": ctx.skills, "commands": ctx.commands}))
`
	cmd := exec.Command(python, "-c", snippet, pluginPath)
	cmd.Env = append(os.Environ(), "ZEN_HOOK_DRY_RUN=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("direct register probe failed: %v\noutput: %s", err, out)
	}

	var result map[string][]string
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("parse probe output: %v\nraw: %s", err, out)
	}
	for _, expected := range []string{"on_session_start", "on_session_end",
		"pre_tool_call", "post_tool_call", "pre_llm_call"} {
		found := false
		for _, h := range result["hooks"] {
			if h == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("hook %s not registered by register(ctx); got: %v", expected, result["hooks"])
		}
	}
	for _, expected := range []string{"hades", "start", "handoff"} {
		found := false
		for _, s := range result["skills"] {
			if s == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("skill %s not registered; got: %v", expected, result["skills"])
		}
	}
}
