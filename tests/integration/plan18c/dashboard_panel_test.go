// go:build integration

package plan18c_integration_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func buildHadesBinary(t *testing.T, root string) string {
	t.Helper()
	binPath := filepath.Join(root, "bin", "hades")
	if _, err := os.Stat(binPath); err != nil {

		cmd := exec.Command("make", "build")
		cmd.Dir = root
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("make build failed: %v\n%s", err, stderr.String())
		}
		if _, err := os.Stat(binPath); err != nil {
			t.Fatalf("bin/hades still missing after make build: %v", err)
		}
	}
	return binPath
}

func TestPluginRegistersDashboardAndPanelCommands(t *testing.T) {
	root := repoRoot(t)
	initPyPath := filepath.Join(root, "plugin", "hades", "__init__.py")
	if _, err := os.Stat(initPyPath); err != nil {
		t.Fatalf("plugin/hades/__init__.py not found: %v", err)
	}

	script := `
import ast
import sys
import pathlib

source = pathlib.Path(sys.argv[1]).read_text()
tree = ast.parse(source)
names = []
for node in ast.walk(tree):
    if isinstance(node, ast.Call) and isinstance(node.func, ast.Attribute):
        if node.func.attr == "register_command" and node.args:
            arg = node.args[0]
            if isinstance(arg, ast.Constant) and isinstance(arg.value, str):
                names.append(arg.value)
import json
print(json.dumps(names))
`
	cmd := exec.Command("python3", "-c", script, initPyPath)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("python3 ast walk failed: %v\nstderr:\n%s", err, stderr.String())
	}

	var names []string
	if err := json.Unmarshal(stdout.Bytes(), &names); err != nil {
		t.Fatalf("decode names JSON: %v\nstdout:\n%s", err, stdout.String())
	}

	if len(names) < 24 {
		t.Errorf(
			"expected at least 24 register_command calls (Plan-18b floor + dashboard/panel); got %d\nnames: %v",
			len(names), names,
		)
	}

	// /hades:dashboard MUST be present.
	hasDashboard := false
	hasPanel := false
	for _, n := range names {
		switch n {
		case "hades:dashboard":
			hasDashboard = true
		case "hades:panel":
			hasPanel = true
		}
	}
	if !hasDashboard {
		t.Errorf("expected 'hades:dashboard' command registered; got names: %v", names)
	}
	if !hasPanel {
		t.Errorf("expected 'hades:panel' command registered; got names: %v", names)
	}

	for _, n := range names {
		if !strings.HasPrefix(n, "hades:") {
			t.Errorf(
				"command %q does not start with 'hades:' prefix; "+
					"Plan 18b B contract violated",
				n,
			)
		}
	}
}

func hermesPreamble(pluginHadesDir string) string {
	return `
import sys, importlib, importlib.util, types, pathlib
_PLUGIN_DIR = pathlib.Path(sys.argv[1])
_NS_PARENT = "hermes_plugins"
if _NS_PARENT not in sys.modules:
    ns_pkg = types.ModuleType(_NS_PARENT)
    ns_pkg.__path__ = []
    ns_pkg.__package__ = _NS_PARENT
    sys.modules[_NS_PARENT] = ns_pkg
_module_name = f"{_NS_PARENT}.hades"
if _module_name not in sys.modules:
    spec = importlib.util.spec_from_file_location(
        _module_name,
        _PLUGIN_DIR / "__init__.py",
        submodule_search_locations=[str(_PLUGIN_DIR)],
    )
    mod = importlib.util.module_from_spec(spec)
    mod.__package__ = _module_name
    mod.__path__ = [str(_PLUGIN_DIR)]
    sys.modules[_module_name] = mod
    spec.loader.exec_module(mod)
`
}

func TestDashboardHandlerSmokeWithMockSubprocess(t *testing.T) {
	root := repoRoot(t)
	pluginHadesDir := filepath.Join(root, "plugin", "hades")

	preamble := hermesPreamble(pluginHadesDir)
	script := preamble + `
import subprocess
class StubCompleted:
    def __init__(self, rc):
        self.args = []
        self.returncode = rc

def fake_run(*args, **kwargs):
    return StubCompleted(0)
subprocess.run = fake_run

import shutil
shutil.which = lambda name: "/fake/path/to/hades" if name == "hades" else None

import termios
termios.tcgetattr = lambda fd: object()
termios.tcsetattr = lambda fd, when, attrs: None

sys.stdin.fileno = lambda: 0

from hermes_plugins.hades.commands.dashboard import dashboard_handler
result = dashboard_handler("")
print(repr(result))
`
	cmd := exec.Command("python3", "-c", script, pluginHadesDir)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("dashboard handler smoke failed: %v\nstderr:\n%s", err, stderr.String())
	}

	got := strings.TrimSpace(stdout.String())
	if got != "None" {
		t.Errorf(
			"expected dashboard_handler('') == None on clean exit; got %q\nstderr:\n%s",
			got, stderr.String(),
		)
	}
}

func TestPanelHandlerSmokeWithMockSubprocess(t *testing.T) {
	root := repoRoot(t)
	pluginHadesDir := filepath.Join(root, "plugin", "hades")

	validPanels := []string{
		"workforce", "cost", "audit", "hra", "confirmations", "memory",
		"skills", "doctrine", "codegraph", "inbox", "crossproject", "help",
	}

	preamble := hermesPreamble(pluginHadesDir)

	for _, panel := range validPanels {
		panel := panel
		t.Run(panel, func(t *testing.T) {
			script := preamble + `
import subprocess
class StubCompleted:
    def __init__(self, rc):
        self.returncode = rc

captured_argv = []
def fake_run(*args, **kwargs):
    captured_argv.append(args[0])
    return StubCompleted(0)
subprocess.run = fake_run

import shutil
shutil.which = lambda name: "/fake/path/to/hades" if name == "hades" else None

import termios
termios.tcgetattr = lambda fd: object()
termios.tcsetattr = lambda fd, when, attrs: None
sys.stdin.fileno = lambda: 0

from hermes_plugins.hades.commands.panel import panel_handler
result = panel_handler(sys.argv[2])
import json
print(json.dumps({"argv": captured_argv, "result_is_none": result is None}))
`
			cmd := exec.Command("python3", "-c", script, pluginHadesDir, panel)
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			if err := cmd.Run(); err != nil {
				t.Fatalf("panel handler smoke for %q failed: %v\nstderr:\n%s",
					panel, err, stderr.String())
			}

			var payload struct {
				Argv         [][]string `json:"argv"`
				ResultIsNone bool       `json:"result_is_none"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
				t.Fatalf("decode result: %v\nstdout:\n%s", err, stdout.String())
			}
			if !payload.ResultIsNone {
				t.Errorf("expected None result for valid panel %q; was non-None", panel)
			}
			if len(payload.Argv) != 1 {
				t.Fatalf("expected one subprocess.run invocation; got %d", len(payload.Argv))
			}
			argv := payload.Argv[0]
			if len(argv) != 3 || argv[1] != "dashboard" || argv[2] != "--panel="+panel {
				t.Errorf(
					"expected argv=[..hades.., dashboard, --panel=%s]; got %v",
					panel, argv,
				)
			}
		})
	}
}

func TestPanelHandlerInvalidNameReturnsCatalogBlock(t *testing.T) {
	root := repoRoot(t)
	pluginHadesDir := filepath.Join(root, "plugin", "hades")

	preamble := hermesPreamble(pluginHadesDir)
	script := preamble + `
import subprocess
captured_argv = []
class StubCompleted:
    def __init__(self, rc): self.returncode = rc
def fake_run(*args, **kwargs):
    captured_argv.append(args[0])
    return StubCompleted(0)
subprocess.run = fake_run

import shutil
shutil.which = lambda name: "/fake/path/to/hades" if name == "hades" else None

import termios
termios.tcgetattr = lambda fd: object()
termios.tcsetattr = lambda fd, when, attrs: None
sys.stdin.fileno = lambda: 0

from hermes_plugins.hades.commands.panel import panel_handler
result = panel_handler("badname-not-a-panel")
import json
print(json.dumps({"argv_count": len(captured_argv), "result": result}))
`
	cmd := exec.Command("python3", "-c", script, pluginHadesDir)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("invalid-panel smoke failed: %v\nstderr:\n%s", err, stderr.String())
	}

	var payload struct {
		ArgvCount int    `json:"argv_count"`
		Result    string `json:"result"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode result: %v\nstdout:\n%s", err, stdout.String())
	}

	// Subprocess MUST NOT have been invoked.
	if payload.ArgvCount != 0 {
		t.Errorf("expected NO subprocess.run for invalid panel; got %d invocations", payload.ArgvCount)
	}

	// Result MUST be a HADES block with all 12 panels enumerated.
	if !strings.Contains(payload.Result, "HADES") {
		t.Errorf("expected HADES branding; got: %q", payload.Result)
	}
	expectedPanels := []string{
		"workforce", "cost", "audit", "hra", "confirmations", "memory",
		"skills", "doctrine", "codegraph", "inbox", "crossproject", "help",
	}
	for _, panel := range expectedPanels {
		if !strings.Contains(payload.Result, panel) {
			t.Errorf("expected panel %q in catalog block; got: %q", panel, payload.Result)
		}
	}
}

func TestHadesDashboardSmokeBinaryExists(t *testing.T) {
	root := repoRoot(t)
	binPath := buildHadesBinary(t, root)

	cmd := exec.Command(binPath, "--help")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	_ = cmd.Run()

	output := stdout.String() + stderr.String()
	if !strings.Contains(output, "dashboard") {
		t.Errorf("expected 'dashboard' in hades --help output; got:\n%s", output)
	}
}
