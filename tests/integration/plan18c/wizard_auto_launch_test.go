// go:build integration

package plan18c_integration_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const stubWrapperSource = `package main

import (
	"encoding/json"
	"os"
)

func main() {
	tracePath := os.Getenv("STUB_TRACE_PATH")
	rcStr := os.Getenv("STUB_RC")
	rc := 0
	if rcStr == "1" {
		rc = 1
	} else if rcStr == "130" {
		rc = 130
	}
	cwd, _ := os.Getwd()
	trace := map[string]interface{}{
		"argv":        os.Args,
		"cwd":         cwd,
		"home":        os.Getenv("HOME"),
		"hermes_skin": os.Getenv("HERMES_SKIN"),
		"no_wizard":   os.Getenv("HADES_NO_WIZARD"),
		"xdg_config":  os.Getenv("XDG_CONFIG_HOME"),
	}
	if tracePath != "" {
		f, _ := os.Create(tracePath)
		defer f.Close()
		enc := json.NewEncoder(f)
		enc.Encode(trace)
	}
	os.Exit(rc)
}
`

func buildStubWrapper(t *testing.T, dir string) string {
	t.Helper()
	srcPath := filepath.Join(dir, "stub_hades.go")
	if err := os.WriteFile(srcPath, []byte(stubWrapperSource), 0o644); err != nil {
		t.Fatalf("write stub source: %v", err)
	}
	binPath := filepath.Join(dir, "stub-hades")
	cmd := exec.Command("go", "build", "-o", binPath, srcPath)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("build stub wrapper: %v", err)
	}
	return binPath
}

const pythonHarness = `
import os
import sys
import json
from pathlib import Path

# Make plugin importable via the same loader-mirror pytest uses.
plugin_root = Path(%q)
sys.path.insert(0, str(plugin_root.parent))

# Apply conftest's preload pattern (simplified for harness use).
import importlib.util
import types

_NS_PARENT = "hermes_plugins"
if _NS_PARENT not in sys.modules:
    ns_pkg = types.ModuleType(_NS_PARENT)
    ns_pkg.__path__ = []
    ns_pkg.__package__ = _NS_PARENT
    sys.modules[_NS_PARENT] = ns_pkg

module_name = _NS_PARENT + ".hades"
spec = importlib.util.spec_from_file_location(
    module_name,
    plugin_root / "__init__.py",
    submodule_search_locations=[str(plugin_root)],
)
mod = importlib.util.module_from_spec(spec)
mod.__package__ = module_name
mod.__path__ = [str(plugin_root)]
sys.modules[module_name] = mod
spec.loader.exec_module(mod)

# Import the wizard_handler module and patch os.isatty to simulate TTY
# (integration test harness runs in a subprocess without a TTY; the
# _is_interactive_stdin() guard would suppress the wizard without this).
from hermes_plugins.hades.hooks import wizard_handler as _wh
_wh.os.isatty = lambda fd: True  # simulate TTY for integration test

from hermes_plugins.hades.hooks.wizard_handler import _maybe_launch_wizard
_maybe_launch_wizard(session_id="integration", cwd=%q, source="startup")
`

func runWizardHarness(t *testing.T, env []string, pluginRoot, cwd, tracePath string) (string, error) {
	t.Helper()
	tmpDir := t.TempDir()
	harnessSrc := fmt.Sprintf(pythonHarness, pluginRoot, cwd)
	harnessPath := filepath.Join(tmpDir, "harness.py")
	if err := os.WriteFile(harnessPath, []byte(harnessSrc), 0o644); err != nil {
		t.Fatalf("write harness: %v", err)
	}
	cmd := exec.Command("python3", harnessPath)

	cleanedEnv := make([]string, 0, len(os.Environ())+len(env))
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "HADES_NO_WIZARD=") || strings.HasPrefix(e, "XDG_CONFIG_HOME=") {
			continue
		}
		cleanedEnv = append(cleanedEnv, e)
	}
	cleanedEnv = append(cleanedEnv, env...)
	cmd.Env = cleanedEnv
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestWizardAutoLaunchSubprocessHandoff(t *testing.T) {
	tmpDir := t.TempDir()
	binPath := buildStubWrapper(t, tmpDir)
	tracePath := filepath.Join(tmpDir, "trace.json")

	homeDir := filepath.Join(tmpDir, "home")
	if err := os.MkdirAll(homeDir, 0o755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}

	pluginRoot, err := filepath.Abs("../../../plugin/hades")
	if err != nil {
		t.Fatalf("resolve plugin root: %v", err)
	}

	output, runErr := runWizardHarness(t, []string{
		"HADES_BIN=" + binPath,
		"STUB_TRACE_PATH=" + tracePath,
		"STUB_RC=0",
		"HOME=" + homeDir,
		"HERMES_SKIN=hades",
		"ZEN_HOOK_DRY_RUN=1",
		"ZEN_BYPASS_DISABLE_KEYCHAIN=1",
	}, pluginRoot, homeDir, tracePath)
	if runErr != nil {
		t.Logf("python harness output:\n%s", output)
		t.Fatalf("python harness exited non-zero (subprocess.run exit was rc=0): %v", runErr)
	}

	data, err := os.ReadFile(tracePath)
	if err != nil {
		t.Fatalf("stub wrapper did not write trace at %s: %v (harness output: %s)", tracePath, err, output)
	}
	var trace map[string]interface{}
	if err := json.Unmarshal(data, &trace); err != nil {
		t.Fatalf("trace JSON unmarshal: %v\ncontent: %s", err, string(data))
	}

	argv, ok := trace["argv"].([]interface{})
	if !ok {
		t.Fatalf("trace argv not a list: %v", trace["argv"])
	}
	if len(argv) != 3 {
		t.Errorf("argv len = %d, want 3 (argv=%v)", len(argv), argv)
	}
	if got, want := fmt.Sprintf("%v", argv[1]), "config"; got != want {
		t.Errorf("argv[1] = %q, want %q", got, want)
	}
	if got, want := fmt.Sprintf("%v", argv[2]), "init"; got != want {
		t.Errorf("argv[2] = %q, want %q", got, want)
	}

	if got, want := fmt.Sprintf("%v", trace["home"]), homeDir; got != want {
		t.Errorf("HOME env = %q, want %q", got, want)
	}

	if got, want := fmt.Sprintf("%v", trace["hermes_skin"]), "hades"; got != want {
		t.Errorf("HERMES_SKIN env = %q, want %q", got, want)
	}
}

func TestWizardAutoLaunchSkippedWhenConfigPresent(t *testing.T) {
	tmpDir := t.TempDir()
	binPath := buildStubWrapper(t, tmpDir)
	tracePath := filepath.Join(tmpDir, "trace.json")

	homeDir := filepath.Join(tmpDir, "home")
	configDir := filepath.Join(homeDir, ".config", "zen-swarm")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}

	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(`name = "t"`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	pluginRoot, err := filepath.Abs("../../../plugin/hades")
	if err != nil {
		t.Fatalf("resolve plugin root: %v", err)
	}

	output, runErr := runWizardHarness(t, []string{
		"HADES_BIN=" + binPath,
		"STUB_TRACE_PATH=" + tracePath,
		"STUB_RC=0",
		"HOME=" + homeDir,
		"HERMES_SKIN=hades",
		"ZEN_HOOK_DRY_RUN=1",
		"ZEN_BYPASS_DISABLE_KEYCHAIN=1",
	}, pluginRoot, homeDir, tracePath)
	if runErr != nil {
		t.Logf("python harness output:\n%s", output)
		t.Fatalf("python harness exited non-zero: %v", runErr)
	}

	if _, err := os.Stat(tracePath); !os.IsNotExist(err) {
		t.Errorf("trace file exists at %s; subprocess was spawned despite config present", tracePath)
	}
}

func TestWizardAutoLaunchSkippedWhenNoWizardEnv(t *testing.T) {
	tmpDir := t.TempDir()
	binPath := buildStubWrapper(t, tmpDir)
	tracePath := filepath.Join(tmpDir, "trace.json")

	homeDir := filepath.Join(tmpDir, "home")
	if err := os.MkdirAll(homeDir, 0o755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}

	pluginRoot, err := filepath.Abs("../../../plugin/hades")
	if err != nil {
		t.Fatalf("resolve plugin root: %v", err)
	}

	harnessDir := t.TempDir()
	harnessSrc := fmt.Sprintf(pythonHarness, pluginRoot, homeDir)
	harnessPath := filepath.Join(harnessDir, "harness.py")
	if err := os.WriteFile(harnessPath, []byte(harnessSrc), 0o644); err != nil {
		t.Fatalf("write harness: %v", err)
	}
	cmd := exec.Command("python3", harnessPath)
	cleanedEnv := make([]string, 0, len(os.Environ())+8)
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "XDG_CONFIG_HOME=") {
			continue
		}
		cleanedEnv = append(cleanedEnv, e)
	}
	cleanedEnv = append(cleanedEnv,
		"HADES_BIN="+binPath,
		"STUB_TRACE_PATH="+tracePath,
		"STUB_RC=0",
		"HOME="+homeDir,
		"HERMES_SKIN=hades",
		"HADES_NO_WIZARD=1",
		"ZEN_HOOK_DRY_RUN=1",
		"ZEN_BYPASS_DISABLE_KEYCHAIN=1",
	)
	cmd.Env = cleanedEnv
	output, runErr := cmd.CombinedOutput()
	if runErr != nil {
		t.Logf("python harness output:\n%s", string(output))
		t.Fatalf("python harness exited non-zero: %v", runErr)
	}

	if _, err := os.Stat(tracePath); !os.IsNotExist(err) {
		t.Errorf("trace file exists; subprocess spawned despite HADES_NO_WIZARD=1")
	}
}
