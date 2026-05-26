package writer

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/migrate/mapping"
)

func TestWriteHook_BashShim(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "hooks", "pre_tool_call.py")
	e := mapping.PlanEntry{
		Kind:      mapping.EntryKindHook,
		HookEvent: "pre_tool_call",
		BodyBytes: []byte("echo hi"),
		Notes:     []string{"source-lang=bash"},
	}
	if err := writeHook(path, e); err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	if !strings.Contains(s, "subprocess.run(") {
		t.Errorf("missing subprocess.run shim: %s", s)
	}
	if !strings.Contains(s, "def pre_tool_call_callback") {
		t.Errorf("missing callback def: %s", s)
	}
	if !strings.Contains(s, "action") {
		t.Errorf("missing action key in stderr branch")
	}

	sidecarPath := filepath.Join(filepath.Dir(path), "pre_tool_call.sh")
	sidecar, err := os.ReadFile(sidecarPath)
	if err != nil {
		t.Fatalf("sidecar missing: %v", err)
	}
	if string(sidecar) != "echo hi" {
		t.Errorf("sidecar body: got %q, want %q (raw bash verbatim)", sidecar, "echo hi")
	}

	if strings.Contains(s, "echo hi") {
		t.Errorf("wrapper inlined the body instead of delegating to sidecar — escape-bug class re-introduced: %s", s)
	}
	if !strings.Contains(s, "pre_tool_call.sh") {
		t.Errorf("wrapper doesn't reference sidecar by name: %s", s)
	}
}

func TestWriteHook_BashBodyEndingInQuote(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "hooks", "pre_tool_call.py")
	hostileBody := `echo "Hello $USER"`
	e := mapping.PlanEntry{
		Kind:      mapping.EntryKindHook,
		HookEvent: "pre_tool_call",
		BodyBytes: []byte(hostileBody),
		Notes:     []string{"source-lang=bash"},
	}
	if err := writeHook(path, e); err != nil {
		t.Fatal(err)
	}

	sidecarPath := filepath.Join(filepath.Dir(path), "pre_tool_call.sh")
	sidecar, _ := os.ReadFile(sidecarPath)
	if string(sidecar) != hostileBody {
		t.Errorf("sidecar mutated body: got %q, want %q", sidecar, hostileBody)
	}

	wrapper, _ := os.ReadFile(path)
	if strings.Contains(string(wrapper), hostileBody) {
		t.Errorf("wrapper inlined hostile body: %s", wrapper)
	}

	assertParsablePython(t, wrapper)
}

func TestWriteHook_BashBodyContainingTripleQuote(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "hooks", "pre_tool_call.py")
	hostileBody := `echo """triple quotes inside"""`
	e := mapping.PlanEntry{
		Kind:      mapping.EntryKindHook,
		HookEvent: "pre_tool_call",
		BodyBytes: []byte(hostileBody),
		Notes:     []string{"source-lang=bash"},
	}
	if err := writeHook(path, e); err != nil {
		t.Fatal(err)
	}
	sidecarPath := filepath.Join(filepath.Dir(path), "pre_tool_call.sh")
	sidecar, _ := os.ReadFile(sidecarPath)
	if string(sidecar) != hostileBody {
		t.Errorf("sidecar mutated body: got %q, want %q", sidecar, hostileBody)
	}
	wrapper, _ := os.ReadFile(path)
	if strings.Contains(string(wrapper), `"""triple`) {
		t.Errorf("wrapper inlined hostile triple-quotes: %s", wrapper)
	}
	assertParsablePython(t, wrapper)
}

func TestWriteHook_PythonNative_Rejected(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "hooks", "on_session_start.py")
	e := mapping.PlanEntry{
		Kind:      mapping.EntryKindHook,
		HookEvent: "on_session_start",
		BodyBytes: []byte("print('hi')"),
		Notes:     []string{"source-lang=python"},
	}
	err := writeHook(path, e)
	if !errors.Is(err, ErrPythonHookManualMigration) {
		t.Errorf("expected ErrPythonHookManualMigration for native python hook, got %v", err)
	}
	if err == nil {
		t.Fatalf("expected error, got nil")
	}

	msg := err.Error()
	for _, hint := range []string{"Python", "manually", "bash"} {
		if !strings.Contains(msg, hint) {
			t.Errorf("operator-friendly hint %q missing from error: %s", hint, msg)
		}
	}
}

func TestWriteHook_EmptyBodyErrors(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "h.py")
	err := writeHook(path, mapping.PlanEntry{Kind: mapping.EntryKindHook, HookEvent: "pre_tool_call"})
	if err == nil {
		t.Errorf("expected error on empty body")
	}
}

func TestWriteHook_NoLangDefaultsToBash(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "hooks", "pre_tool_call.py")
	e := mapping.PlanEntry{
		Kind:      mapping.EntryKindHook,
		HookEvent: "pre_tool_call",
		BodyBytes: []byte("echo hi"),
	}
	if err := writeHook(path, e); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(path)
	s := string(body)
	if !strings.Contains(s, "subprocess.run(") {
		t.Errorf("missing subprocess.run default-bash branch: %s", s)
	}
}

func assertParsablePython(t *testing.T, body []byte) {
	t.Helper()
	pyParseOrSkip(t, body)
}
