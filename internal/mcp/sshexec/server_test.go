package sshexec_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/mcp/sshexec"
	"github.com/cbip-solutions/hades-system/tests/testharness"
)

func TestServerToolRegistration(t *testing.T) {
	srv := sshexec.NewServer(sshexec.ServerConfig{
		Component:         "ssh-exec",
		AllowlistResolver: stubAllowlist([]string{"alembic *"}, []string{"127.0.0.1:22"}),
	})
	tools := srv.RegisteredTools()
	want := map[string]bool{"validate": true, "exec": true, "list_allowed": true}
	for _, name := range tools {
		delete(want, name)
	}
	if len(want) > 0 {
		t.Errorf("missing tools: %v (got %v)", want, tools)
	}
}

func TestServerValidateTool(t *testing.T) {
	srv := sshexec.NewServer(sshexec.ServerConfig{
		Component:         "ssh-exec",
		AllowlistResolver: stubAllowlist([]string{"alembic *"}, []string{"127.0.0.1:22"}),
	})
	out, err := srv.InvokeForTest(context.Background(), "validate", map[string]any{
		"cmd":     "alembic ; rm -rf /",
		"project": "internal-platform-x",
	})
	if err != nil {
		t.Fatalf("InvokeForTest: %v", err)
	}
	var vr sshexec.ValidationResult
	if err := json.Unmarshal(out, &vr); err != nil {
		t.Fatalf("unmarshal: %v (out=%s)", err, out)
	}
	if vr.OK {
		t.Errorf("vr.OK = true, want false")
	}
	if !strings.Contains(vr.Reason, "forbidden") {
		t.Errorf("Reason = %q", vr.Reason)
	}
}

func TestServerValidateAllowed(t *testing.T) {
	srv := sshexec.NewServer(sshexec.ServerConfig{
		Component:         "ssh-exec",
		AllowlistResolver: stubAllowlist([]string{"alembic *"}, []string{"127.0.0.1:22"}),
	})
	out, _ := srv.InvokeForTest(context.Background(), "validate", map[string]any{
		"cmd":     "alembic upgrade head",
		"project": "internal-platform-x",
	})
	var vr sshexec.ValidationResult
	json.Unmarshal(out, &vr)
	if !vr.OK {
		t.Errorf("vr.OK = false (reason=%q)", vr.Reason)
	}
}

func TestServerListAllowedTool(t *testing.T) {
	srv := sshexec.NewServer(sshexec.ServerConfig{
		Component:         "ssh-exec",
		AllowlistResolver: stubAllowlist([]string{"alembic *", "pytest *"}, []string{"vps"}),
	})
	out, err := srv.InvokeForTest(context.Background(), "list_allowed", map[string]any{
		"project": "internal-platform-x",
	})
	if err != nil {
		t.Fatalf("InvokeForTest: %v", err)
	}
	var r sshexec.ListAllowedResult
	if err := json.Unmarshal(out, &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(r.Patterns) != 2 {
		t.Errorf("Patterns = %v, want 2", r.Patterns)
	}
	if r.Project != "internal-platform-x" {
		t.Errorf("Project = %q", r.Project)
	}
}

func TestServerInvokeUnknownTool(t *testing.T) {
	srv := sshexec.NewServer(sshexec.ServerConfig{
		AllowlistResolver: stubAllowlist(nil, nil),
	})
	_, err := srv.InvokeForTest(context.Background(), "unknown", map[string]any{})
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
}

func TestServerValidateMissingCmd(t *testing.T) {
	srv := sshexec.NewServer(sshexec.ServerConfig{
		AllowlistResolver: stubAllowlist([]string{"alembic *"}, []string{"vps"}),
	})
	_, err := srv.InvokeForTest(context.Background(), "validate", map[string]any{
		"project": "internal-platform-x",
	})
	if err == nil {
		t.Fatal("expected error for missing cmd")
	}
}

func TestServerListAllowedMissingProject(t *testing.T) {
	srv := sshexec.NewServer(sshexec.ServerConfig{
		AllowlistResolver: stubAllowlist([]string{"alembic *"}, []string{"vps"}),
	})
	_, err := srv.InvokeForTest(context.Background(), "list_allowed", map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing project")
	}
}

func TestServerExecToolEndToEnd(t *testing.T) {
	t.Setenv("ZEN_SSH_INSECURE_TEST", "1")
	sshd, _ := testharness.NewFakeSSHD(testharness.HandlerFunc(func(req string) testharness.HandlerScript {
		return testharness.HandlerScript{Stdout: "alembic ok\n", ExitCode: 0}
	}))
	defer sshd.Close()

	srv := sshexec.NewServer(sshexec.ServerConfig{
		Component:         "ssh-exec",
		AllowlistResolver: stubAllowlist([]string{"alembic *"}, []string{sshd.Addr()}),
		Auth:              sshexec.AgentAuthForTest(),
		Emitter:           sshexec.NopAuditEmitter{},
	})

	chunkCh := make(chan sshexec.StreamChunk, 16)
	out, err := srv.InvokeExecForTest(context.Background(), map[string]any{
		"host":    sshd.Addr(),
		"cmd":     "alembic upgrade head",
		"project": "internal-platform-x",
		"timeout": "5s",
	}, chunkCh)
	if err != nil {
		t.Fatalf("InvokeExecForTest: %v", err)
	}
	close(chunkCh)
	collected := ""
	for c := range chunkCh {
		collected += string(c.Data)
	}
	if !strings.Contains(collected, "alembic ok") {
		t.Errorf("collected chunks = %q", collected)
	}
	var res sshexec.ExecResult
	if err := json.Unmarshal(out, &res); err != nil {
		t.Fatalf("unmarshal exec result: %v (out=%s)", err, out)
	}
	if res.ExitCode != 0 {
		t.Errorf("exit_code = %d, want 0", res.ExitCode)
	}
}

func TestServerExecBadTimeout(t *testing.T) {
	srv := sshexec.NewServer(sshexec.ServerConfig{
		AllowlistResolver: stubAllowlist([]string{"alembic *"}, []string{"vps"}),
		Auth:              sshexec.AgentAuthForTest(),
		Emitter:           sshexec.NopAuditEmitter{},
	})
	chunkCh := make(chan sshexec.StreamChunk, 1)
	_, err := srv.InvokeExecForTest(context.Background(), map[string]any{
		"host":    "vps",
		"cmd":     "alembic upgrade",
		"project": "internal-platform-x",
		"timeout": "not-a-duration",
	}, chunkCh)
	if err == nil {
		t.Fatal("expected error for bad timeout")
	}
}

func TestServerExecResolverError(t *testing.T) {
	srv := sshexec.NewServer(sshexec.ServerConfig{
		AllowlistResolver: func(string) (*sshexec.Allowlist, error) {
			return nil, errFakeResolver
		},
		Auth:    sshexec.AgentAuthForTest(),
		Emitter: sshexec.NopAuditEmitter{},
	})
	chunkCh := make(chan sshexec.StreamChunk, 1)
	_, err := srv.InvokeExecForTest(context.Background(), map[string]any{
		"host":    "vps",
		"cmd":     "alembic upgrade",
		"project": "internal-platform-x",
	}, chunkCh)
	if err == nil {
		t.Fatal("expected error from resolver")
	}
}

func TestWrapperBashSyntax(t *testing.T) {
	cmd := exec.Command("bash", "-n", wrapperPath(t))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bash -n: %v\n%s", err, out)
	}
}

func TestWrapperRejectsForbiddenChar(t *testing.T) {
	tmpAllow := filepath.Join(t.TempDir(), "allow")
	if err := os.WriteFile(tmpAllow, []byte("alembic *\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", wrapperPath(t))
	cmd.Env = append(os.Environ(),
		"ZEN_ALLOWLIST_FILE="+tmpAllow,
		"SSH_ORIGINAL_COMMAND=alembic ; rm -rf /",
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("wrapper accepted forbidden command: %s", out)
	}
	if !strings.Contains(string(out), "forbidden char") {
		t.Errorf("wrapper output = %q, want forbidden char message", out)
	}
}

func TestWrapperRejectsEmptyCommand(t *testing.T) {
	tmpAllow := filepath.Join(t.TempDir(), "allow")
	if err := os.WriteFile(tmpAllow, []byte("alembic *\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", wrapperPath(t))
	cmd.Env = append(os.Environ(),
		"ZEN_ALLOWLIST_FILE="+tmpAllow,
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("wrapper accepted empty command: %s", out)
	}
	if !strings.Contains(string(out), "empty command") {
		t.Errorf("wrapper output = %q", out)
	}
}

func TestWrapperRejectsNonAllowlistedCommand(t *testing.T) {
	tmpAllow := filepath.Join(t.TempDir(), "allow")
	if err := os.WriteFile(tmpAllow, []byte("alembic *\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", wrapperPath(t))
	cmd.Env = append(os.Environ(),
		"ZEN_ALLOWLIST_FILE="+tmpAllow,
		"SSH_ORIGINAL_COMMAND=ls -la /etc",
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("wrapper accepted non-allowlisted command: %s", out)
	}
	if !strings.Contains(string(out), "not in allowlist") {
		t.Errorf("wrapper output = %q", out)
	}
}

func TestWrapperZenCwdRequestedButMissing(t *testing.T) {
	tmpAllow := filepath.Join(t.TempDir(), "allow")
	if err := os.WriteFile(tmpAllow, []byte("alembic *\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", wrapperPath(t))
	cmd.Env = append(os.Environ(),
		"ZEN_ALLOWLIST_FILE="+tmpAllow,
		"SSH_ORIGINAL_COMMAND=alembic upgrade",
		"HOME="+t.TempDir(),
		"ZEN_CWD_REQUESTED=1",
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("wrapper accepted ZEN_CWD_REQUESTED+missing-CWD: %s", out)
	}
	if !strings.Contains(string(out), "ZEN_CWD_REQUESTED") {
		t.Errorf("wrapper output = %q, want explicit ZEN_CWD_REQUESTED diagnostic", out)
	}
}

func TestWrapperZenCwdNotRequestedSilentFallback(t *testing.T) {
	tmpAllow := filepath.Join(t.TempDir(), "allow")
	if err := os.WriteFile(tmpAllow, []byte("alembic *\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	homeDir := t.TempDir()
	cmd := exec.Command("bash", wrapperPath(t))
	cmd.Env = append(os.Environ(),
		"ZEN_ALLOWLIST_FILE="+tmpAllow,
		"SSH_ORIGINAL_COMMAND=alembic --help-or-fail",
		"HOME="+homeDir,
	)
	out, _ := cmd.CombinedOutput()

	if strings.Contains(string(out), "ZEN_CWD_REQUESTED") {
		t.Errorf("wrapper emitted ZEN_CWD_REQUESTED diagnostic on no-cwd path: %q", out)
	}
}

// TestPatternSlashStarEndToEnd is the regression for code-review
// CRITICAL C-1: a pattern of shape "pytest tests/integration/*" must be
// accepted at all three enforcement layers — validator (Go), allowlist
// loader (validatePatterns), and the wrapper script — for the same
// concrete request "pytest tests/integration/test_foo.py".
//
// Drift between layers is a security liability: if validator allows but
// wrapper rejects, the operator sees an opaque exit-126; if validator
// rejects but loader passed, the doctrine config silently breaks at
// runtime. This test enforces synchronized behaviour.
func TestPatternSlashStarEndToEnd(t *testing.T) {
	pattern := "pytest tests/integration/*"
	cmd := "pytest tests/integration/test_foo.py"

	vr := sshexec.Validate(cmd, []string{pattern})
	if !vr.OK {
		t.Errorf("validator rejected slash-star pattern: %s", vr.Reason)
	}

	tmpAllow := filepath.Join(t.TempDir(), "allow")
	if err := os.WriteFile(tmpAllow, []byte(pattern+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wrapperCmd := exec.Command("bash", wrapperPath(t))
	wrapperCmd.Env = append(os.Environ(),
		"ZEN_ALLOWLIST_FILE="+tmpAllow,
		"SSH_ORIGINAL_COMMAND="+cmd,

		"HOME="+t.TempDir(),
	)
	out, _ := wrapperCmd.CombinedOutput()
	body := string(out)
	if strings.Contains(body, "not in allowlist") {
		t.Errorf("wrapper rejected slash-star pattern in allowlist match: %q", body)
	}
	if strings.Contains(body, "forbidden char") {
		t.Errorf("wrapper rejected slash-star pattern as forbidden-char: %q", body)
	}
}

func TestWrapperRejectsPTY(t *testing.T) {
	tmpAllow := filepath.Join(t.TempDir(), "allow")
	if err := os.WriteFile(tmpAllow, []byte("alembic *\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", wrapperPath(t))
	cmd.Env = append(os.Environ(),
		"ZEN_ALLOWLIST_FILE="+tmpAllow,
		"SSH_ORIGINAL_COMMAND=alembic upgrade",
		"SSH_TTY=/dev/pts/0",
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("wrapper accepted PTY: %s", out)
	}
	if !strings.Contains(string(out), "PTY refused") {
		t.Errorf("wrapper output = %q", out)
	}
}

func TestInvokeToolUnknown(t *testing.T) {
	srv := sshexec.NewServer(sshexec.ServerConfig{
		Component:         "ssh-exec",
		AllowlistResolver: stubAllowlist(nil, nil),
		Emitter:           sshexec.NopAuditEmitter{},
	})
	_, err := srv.InvokeTool(context.Background(), "non-existent-tool-name", map[string]any{})
	if err == nil {
		t.Fatal("InvokeTool unknown tool: nil err; expected non-nil")
	}
	if !strings.Contains(err.Error(), "unknown tool") {
		t.Errorf("err = %v; expected mention of 'unknown tool'", err)
	}
}

func TestInvokeToolValidateRoutesToHandler(t *testing.T) {
	srv := sshexec.NewServer(sshexec.ServerConfig{
		Component:         "ssh-exec",
		AllowlistResolver: stubAllowlist([]string{"alembic *"}, []string{"127.0.0.1:22"}),
		Emitter:           sshexec.NopAuditEmitter{},
	})
	out, err := srv.InvokeTool(context.Background(), "validate", map[string]any{
		"cmd":     "alembic ; rm -rf /",
		"project": "internal-platform-x",
	})
	if err != nil {
		t.Fatalf("InvokeTool validate: %v", err)
	}

	raw, ok := out.(json.RawMessage)
	if !ok {
		t.Fatalf("out type = %T; expected json.RawMessage", out)
	}
	var vr sshexec.ValidationResult
	if err := json.Unmarshal(raw, &vr); err != nil {
		t.Fatalf("unmarshal: %v (raw=%s)", err, raw)
	}
	if vr.OK {
		t.Errorf("vr.OK = true on forbidden-char input; want false")
	}
}

func TestInvokeToolExecRoutesToHandler(t *testing.T) {
	srv := sshexec.NewServer(sshexec.ServerConfig{
		Component:         "ssh-exec",
		AllowlistResolver: stubAllowlist([]string{"alembic *"}, []string{"vps"}),
		Auth:              sshexec.AgentAuthForTest(),
		Emitter:           sshexec.NopAuditEmitter{},
	})
	_, err := srv.InvokeTool(context.Background(), "exec", map[string]any{
		"host":    "vps",
		"cmd":     "alembic upgrade",
		"project": "internal-platform-x",
		"timeout": "not-a-duration",
	})
	if err == nil {
		t.Fatal("InvokeTool exec with bad timeout: nil err; expected non-nil")
	}
}

func TestInvokeToolListAllowedRoutesToHandler(t *testing.T) {
	srv := sshexec.NewServer(sshexec.ServerConfig{
		Component:         "ssh-exec",
		AllowlistResolver: stubAllowlist([]string{"alembic *"}, []string{"vps"}),
		Emitter:           sshexec.NopAuditEmitter{},
	})
	out, err := srv.InvokeTool(context.Background(), "list_allowed", map[string]any{
		"project": "internal-platform-x",
	})
	if err != nil {
		t.Fatalf("InvokeTool list_allowed: %v", err)
	}
	raw, ok := out.(json.RawMessage)
	if !ok {
		t.Fatalf("out type = %T; expected json.RawMessage", out)
	}
	var r sshexec.ListAllowedResult
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if r.Project != "internal-platform-x" {
		t.Errorf("Project = %q; want internal-platform-x", r.Project)
	}
}

func stubAllowlist(patterns, hosts []string) sshexec.AllowlistResolver {
	return func(project string) (*sshexec.Allowlist, error) {
		return &sshexec.Allowlist{
			Project:  project,
			Patterns: patterns,
			Hosts:    hosts,
			Source:   "stub",
		}, nil
	}
}

var errFakeResolver = stubError("fake resolver failure")

type stubError string

func (s stubError) Error() string { return string(s) }

func wrapperPath(t *testing.T) string {
	t.Helper()
	for _, candidate := range []string{
		"../../../scripts/zen-ssh-exec-wrapper.sh",
		"../../scripts/zen-ssh-exec-wrapper.sh",
		"../scripts/zen-ssh-exec-wrapper.sh",
		"scripts/zen-ssh-exec-wrapper.sh",
	} {
		if _, err := os.Stat(candidate); err == nil {
			abs, _ := filepath.Abs(candidate)
			return abs
		}
	}
	t.Fatalf("could not find scripts/zen-ssh-exec-wrapper.sh from cwd")
	return ""
}
