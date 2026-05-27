// cmd/zen-mcp-sshexec/main_test.go
//
// TDD tests for the sshexec MCP binary entrypoint.
// Mirrors the cmd/zen-mcp-{budget,audit} pattern:
// extract testable buildServer() + buildOptions shape so tests cover
// the flag-validation + server-construction branches without engaging
// the blocking stdio Run loop.
//
// Task M-4: these tests MUST fail before main.go exists, then
// PASS after main.go is written.
package main

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestBuildServer_NoDoctrineFile(t *testing.T) {
	srv, err := buildServer(buildOptions{
		DoctrineName:    "default",
		DoctrineFile:    "",
		ProjectID:       "test-project",
		ProjectTOMLPath: "",
	})
	if err != nil {
		t.Fatalf("buildServer with no doctrine file: %v", err)
	}
	if srv == nil {
		t.Fatal("buildServer returned nil server")
	}
}

func TestBuildServer_MaxScopeDoctrine(t *testing.T) {
	srv, err := buildServer(buildOptions{
		DoctrineName:    "max-scope",
		DoctrineFile:    "",
		ProjectID:       "test-project",
		ProjectTOMLPath: "",
	})
	if err != nil {
		t.Fatalf("buildServer max-scope: %v", err)
	}
	if srv == nil {
		t.Fatal("buildServer returned nil server")
	}
}

func TestBuildServer_CapaFirewallDoctrine(t *testing.T) {
	srv, err := buildServer(buildOptions{
		DoctrineName:    "capa-firewall",
		DoctrineFile:    "",
		ProjectID:       "test-project",
		ProjectTOMLPath: "",
	})
	if err != nil {
		t.Fatalf("buildServer capa-firewall: %v", err)
	}
	if srv == nil {
		t.Fatal("buildServer returned nil server")
	}
}

func TestBuildServer_UnknownDoctrine(t *testing.T) {
	_, err := buildServer(buildOptions{
		DoctrineName:    "no-such-doctrine",
		DoctrineFile:    "",
		ProjectID:       "test-project",
		ProjectTOMLPath: "",
	})
	if err == nil {
		t.Fatal("expected error for unknown doctrine name; got nil")
	}
	if !strings.Contains(err.Error(), "doctrine") {
		t.Errorf("error must mention 'doctrine' for operator UX; got: %v", err)
	}
}

func TestBuildServer_DoctrineFileNotFound(t *testing.T) {
	_, err := buildServer(buildOptions{
		DoctrineName:    "",
		DoctrineFile:    "/nonexistent/path/to/doctrine.toml",
		ProjectID:       "test-project",
		ProjectTOMLPath: "",
	})
	if err == nil {
		t.Fatal("expected error for non-existent doctrine file; got nil")
	}
}

func TestBuildServer_RegisteredTools(t *testing.T) {
	srv, err := buildServer(buildOptions{
		DoctrineName:    "default",
		DoctrineFile:    "",
		ProjectID:       "test-project",
		ProjectTOMLPath: "",
	})
	if err != nil {
		t.Fatalf("buildServer: %v", err)
	}
	tools := srv.RegisteredTools()
	want := map[string]bool{"validate": false, "exec": false, "list_allowed": false}
	for _, name := range tools {
		if _, ok := want[name]; ok {
			want[name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("expected tool %q to be registered; registered tools: %v", name, tools)
		}
	}
}

func TestBuildServer_ResolverClosureInvoked(t *testing.T) {
	srv, err := buildServer(buildOptions{
		DoctrineName:    "default",
		DoctrineFile:    "",
		ProjectID:       "test-project",
		ProjectTOMLPath: "",
	})
	if err != nil {
		t.Fatalf("buildServer: %v", err)
	}

	_, _ = srv.InvokeForTest(context.Background(), "list_allowed", map[string]any{
		"project": "test-project",
	})
}

func TestRun_UnknownFlag(t *testing.T) {
	origArgs := os.Args
	os.Args = []string{"zen-mcp-sshexec", "--nonexistent-flag-xyz"}
	defer func() { os.Args = origArgs }()

	err := run()
	if err == nil {
		t.Fatal("expected error from run() for unknown flag; got nil")
	}
}

func TestRun_FlagDefaults_NoError(t *testing.T) {
	origArgs := os.Args

	os.Args = []string{"zen-mcp-sshexec"}
	defer func() { os.Args = origArgs }()

	srv, err := buildServer(buildOptions{
		DoctrineName:    "default",
		DoctrineFile:    "",
		ProjectID:       "zen-swarm",
		ProjectTOMLPath: "",
	})
	if err != nil {
		t.Fatalf("buildServer with flag defaults: %v", err)
	}
	if srv == nil {
		t.Fatal("expected non-nil server from buildServer with defaults")
	}
}

func TestRun_UnknownDoctrineError(t *testing.T) {
	origArgs := os.Args
	os.Args = []string{"zen-mcp-sshexec", "--doctrine", "no-such-doctrine-xyz"}
	defer func() { os.Args = origArgs }()

	err := run()
	if err == nil {
		t.Fatal("expected error from run() with unknown doctrine; got nil")
	}
	if !strings.Contains(err.Error(), "doctrine") {
		t.Errorf("error must mention 'doctrine'; got: %v", err)
	}
}

func TestBuildServer_DoctrineFileValid(t *testing.T) {

	const minimalTOML = `
schema_version = 1
name = "default"

[research]
depth = "shallow"

[budget.caps]
project = "10.00 USD"
doctrine = "100.00 USD"
`
	dir := t.TempDir()
	path := dir + "/doctrine.toml"
	if err := os.WriteFile(path, []byte(minimalTOML), 0o600); err != nil {
		t.Fatalf("write doctrine TOML: %v", err)
	}

	srv, err := buildServer(buildOptions{
		DoctrineName:    "",
		DoctrineFile:    path,
		ProjectID:       "test-project",
		ProjectTOMLPath: "",
	})
	if err != nil {
		t.Fatalf("buildServer with doctrine file: %v", err)
	}
	if srv == nil {
		t.Fatal("buildServer returned nil server")
	}
}

func TestBuildServer_EmptyDoctrineNameFallsToDefault(t *testing.T) {
	srv, err := buildServer(buildOptions{
		DoctrineName:    "",
		DoctrineFile:    "",
		ProjectID:       "test-project",
		ProjectTOMLPath: "",
	})
	if err != nil {
		t.Fatalf("buildServer with empty doctrine name: %v", err)
	}
	if srv == nil {
		t.Fatal("buildServer returned nil server")
	}
}

func TestRun_HappyPathServesUntilStdinEOF(t *testing.T) {

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	origStdin := os.Stdin
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = origStdin
		_ = r.Close()
	})
	if err := w.Close(); err != nil {
		t.Fatalf("close write: %v", err)
	}

	origArgs := os.Args
	defer func() { os.Args = origArgs }()
	os.Args = []string{
		"zen-mcp-sshexec",
		"--doctrine", "default",
		"--project-id", "test-project",
	}

	done := make(chan error, 1)
	go func() { done <- run() }()

	select {
	case <-done:

	case <-time.After(5 * time.Second):
		t.Fatal("run() did not return after stdin EOF — possible hang")
	}
}

func TestMainBinaryGracefulShutdown(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	binPath := tmpDir + "/zen-mcp-sshexec"

	build := exec.Command("go", "build", "-o", binPath, ".")
	build.Dir = "."
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binPath)
	cmd.Env = append(os.Environ(),
		"ZEN_MCP_DRAIN_TIMEOUT=200ms",
	)

	cmd.Stdin = nil
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}

	time.Sleep(150 * time.Millisecond)
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("SIGTERM: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:

	case <-time.After(5 * time.Second):
		t.Fatal("process did not exit within 5s after SIGTERM")
	}
}

func TestMainBinaryBuilds(t *testing.T) {
	t.Parallel()
	cmd := exec.Command("go", "build", "-o", os.DevNull, ".")
	cmd.Dir = "."
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %v\n%s", err, out)
	}
}

func TestMainNoHTTPListen(t *testing.T) {
	t.Parallel()
	forbidden := []string{
		"http.ListenAndServe",
		`net.Listen("tcp"`,
		`net.Listen("unix"`,
		"ListenAndServeTLS",
	}
	data, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	src := string(data)
	for _, f := range forbidden {
		if strings.Contains(src, f) {
			t.Errorf("inv-zen-086 violated: main.go contains forbidden call %q", f)
		}
	}
}
