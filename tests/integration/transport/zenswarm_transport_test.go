//go:build integration
// +build integration

package transport_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/daemon/transport"
	"github.com/cbip-solutions/hades-system/internal/providers"
	"github.com/cbip-solutions/hades-system/tests/testharness"
)

func pythonBinary(t *testing.T) string {
	t.Helper()
	root := repoRoot(t)
	venvPython := filepath.Join(root, ".venv", "bin", "python3")
	if _, err := os.Stat(venvPython); err == nil {
		return venvPython
	}
	bin, err := exec.LookPath("python3")
	if err != nil {
		t.Skipf("python3 not on PATH and no .venv: %v", err)
	}
	return bin
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}

func pythonSyspathPreamble(repo string) string {
	return fmt.Sprintf(`
import sys, types, importlib.util
sys.path.insert(0, %q)
_PLUGIN_DIR = %q + "/plugin/hades"
if "hermes_plugins" not in sys.modules:
    ns = types.ModuleType("hermes_plugins")
    ns.__path__ = []
    sys.modules["hermes_plugins"] = ns
_module_name = "hermes_plugins.hades"
if _module_name not in sys.modules:
    spec = importlib.util.spec_from_file_location(
        _module_name,
        _PLUGIN_DIR + "/__init__.py",
        submodule_search_locations=[_PLUGIN_DIR],
    )
    mod = importlib.util.module_from_spec(spec)
    mod.__package__ = _module_name
    mod.__path__ = [_PLUGIN_DIR]
    sys.modules[_module_name] = mod
    # NOTE: do NOT execute the plugin's __init__.py here — it imports
    # commands/* which require dependencies we don't need for the
    # transport integration test. We only need the transports subpackage.
`, repo, repo)
}

func TestZenSwarmTransportEndToEnd(t *testing.T) {
	cannedBody := []byte(`{"id":"msg_E2E","content":[{"type":"text","text":"hello from real flow"}],"usage":{"input_tokens":12,"output_tokens":3}}`)
	disp := &testharness.ZenSwarmRecordingDispatcher{
		Resp: &providers.TierResponse{
			Status:       200,
			Body:         cannedBody,
			TierUsed:     providers.TierInHouse,
			ModelUsed:    "claude-sonnet-4-6",
			InputTokens:  12,
			OutputTokens: 3,
		},
	}
	anchor := &testharness.ZenSwarmRecordingAnchor{ID: "evt-e2e-001"}
	h := transport.NewMessagesHandler(disp, anchor)

	srv := httptest.NewServer(h)
	defer srv.Close()

	root := repoRoot(t)
	script := pythonSyspathPreamble(root) + fmt.Sprintf(`
import json
import os
import sys

import httpx

from hermes_plugins.hades.transports.zen_swarm_transport import ZenSwarmTransport

transport = ZenSwarmTransport()
transport._set_client_for_test(httpx.Client(base_url=%q))

result = transport.complete(
    messages=[{"role": "user", "content": "hello e2e"}],
    model="claude-sonnet-4-6",
    session_id="sess-e2e",
    profile="orchestrator",
    project="zen-swarm",
    max_tokens=100,
)
# Phase B reviewer I2: complete() now returns a CompletedResponse dataclass
# exposing status/body/headers/audit_event_id. Serialize the metadata-rich
# view so the Go-side assertions can verify the audit_event_id and status
# are surfaced verbatim through the cross-language boundary.
sys.stdout.write(json.dumps({
    "id": result.body.get("id"),
    "status": result.status,
    "audit_event_id": result.audit_event_id,
    "header_count": len(result.headers),
}))
`, srv.URL)

	cmd := exec.CommandContext(
		context.Background(),
		pythonBinary(t),
		"-c", script,
	)
	cmd.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("python subprocess failed: %v\noutput: %s", err, string(out))
	}

	idx := strings.Index(string(out), "{")
	if idx < 0 {
		t.Fatalf("python output had no JSON object: %s", string(out))
	}
	jsonOut := string(out)[idx:]
	var result map[string]any
	if err := json.Unmarshal([]byte(jsonOut), &result); err != nil {
		t.Fatalf("decode python result: %v\nraw: %s", err, jsonOut)
	}
	if result["id"] != "msg_E2E" {
		t.Errorf("python received id = %v, want msg_E2E", result["id"])
	}
	// Reviewer I2: the audit_event_id MUST cross the cross-language
	// boundary so Plan 12 citation renderers can deep-link via
	// zen://audit/<id>. Before the I2 fix, complete() returned just the
	// inner body dict — the field was silently discarded.
	if got := result["audit_event_id"]; got != "evt-e2e-001" {
		t.Errorf("python received audit_event_id = %v, want evt-e2e-001", got)
	}

	if got, ok := result["status"].(float64); !ok || int(got) != 200 {
		t.Errorf("python received status = %v (type %T), want 200", result["status"], result["status"])
	}

	if disp.CallCount() != 1 {
		t.Fatalf("dispatcher.CallCount = %d, want 1", disp.CallCount())
	}
	last := disp.LastCall()
	if last.SessionID != "sess-e2e" {
		t.Errorf("dispatcher.SessionID = %q, want sess-e2e", last.SessionID)
	}
	if last.Profile != "orchestrator" {
		t.Errorf("dispatcher.Profile = %q, want orchestrator", last.Profile)
	}
	if last.Project != "zen-swarm" {
		t.Errorf("dispatcher.Project = %q, want zen-swarm", last.Project)
	}
	if last.Model != "claude-sonnet-4-6" {
		t.Errorf("dispatcher.Model = %q, want claude-sonnet-4-6", last.Model)
	}
	if !strings.Contains(string(last.Body), "hello e2e") {
		t.Errorf("dispatcher saw body = %q, want to contain 'hello e2e'", string(last.Body))
	}
	if last.Method != http.MethodPost {
		t.Errorf("dispatcher.Method = %q, want POST", last.Method)
	}
	if last.Path != "/v1/messages" {
		t.Errorf("dispatcher.Path = %q, want /v1/messages", last.Path)
	}

	if anchor.EventCount() != 1 {
		t.Fatalf("anchor.EventCount = %d, want 1", anchor.EventCount())
	}
	evt := anchor.Events[0]
	if evt.Type != "MessageForwarded" {
		t.Errorf("anchor event type = %q, want MessageForwarded", evt.Type)
	}
	if evt.Payload["session_id"] != "sess-e2e" {
		t.Errorf("anchor session_id = %v, want sess-e2e", evt.Payload["session_id"])
	}
	if evt.Payload["transport_source"] != "zenswarm-transport" {
		t.Errorf("anchor transport_source = %v, want zenswarm-transport", evt.Payload["transport_source"])
	}
}

func TestZenSwarmTransportEndToEndDispatcherFailureSurfaced(t *testing.T) {
	disp := &testharness.ZenSwarmRecordingDispatcher{Err: fmt.Errorf("upstream-down")}
	h := transport.NewMessagesHandler(disp, nil)
	srv := httptest.NewServer(h)
	defer srv.Close()

	root := repoRoot(t)
	script := pythonSyspathPreamble(root) + fmt.Sprintf(`
import json
import os
import sys

import httpx

from hermes_plugins.hades.transports.zen_swarm_transport import ZenSwarmTransport

transport = ZenSwarmTransport()
transport._set_client_for_test(httpx.Client(base_url=%q))

try:
    transport.complete(messages=[{"role": "user", "content": "x"}], model="m")
    sys.stdout.write(json.dumps({"raised": False}))
except RuntimeError as exc:
    sys.stdout.write(json.dumps({"raised": True, "msg": str(exc)}))
`, srv.URL)

	cmd := exec.CommandContext(context.Background(), pythonBinary(t), "-c", script)
	cmd.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("python subprocess failed unexpectedly: %v\n%s", err, string(out))
	}
	idx := strings.Index(string(out), "{")
	if idx < 0 {
		t.Fatalf("no JSON in stdout: %s", string(out))
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(string(out)[idx:]), &result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result["raised"] != true {
		t.Errorf("python expected to raise RuntimeError on 502; got %v", result)
	}
}
