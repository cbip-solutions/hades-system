package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func writeTokenFile(t *testing.T, contents string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func TestBuildServer_MissingAuthTokenPath(t *testing.T) {
	srv, cleanup, err := buildServer(buildOptions{
		Socket:        "/var/run/zen-swarm/zen-swarm.sock",
		AuthTokenPath: "",
	})
	if err == nil {
		cleanup()
		_ = srv
		t.Fatal("expected error on missing --auth-token-path; got nil")
	}
	if !strings.Contains(err.Error(), "--auth-token-path") {
		t.Errorf("error must mention '--auth-token-path' for operator UX; got: %v", err)
	}
}

func TestBuildServer_InvalidAuthTokenFile(t *testing.T) {
	srv, cleanup, err := buildServer(buildOptions{
		Socket:        "/var/run/zen-swarm/zen-swarm.sock",
		AuthTokenPath: "/no/such/token-file",
	})
	if err == nil {
		cleanup()
		_ = srv
		t.Fatal("expected error on missing token file; got nil")
	}
	if !strings.Contains(err.Error(), "build client") {
		t.Errorf("error must wrap with 'build client' prefix; got: %v", err)
	}
}

func TestBuildServer_DaemonURLOverridesSocket(t *testing.T) {
	tokenPath := writeTokenFile(t, "test-token")
	srv, cleanup, err := buildServer(buildOptions{
		Socket:        "/no/such/socket.sock",
		DaemonURL:     "http://127.0.0.1:0",
		AuthTokenPath: tokenPath,
	})
	if err != nil {
		t.Fatalf("buildServer with DaemonURL unexpectedly failed: %v", err)
	}
	defer cleanup()
	if srv == nil {
		t.Fatal("buildServer returned nil server with no error")
	}
}

func TestBuildServer_SocketPathDefaultsApplied(t *testing.T) {
	tokenPath := writeTokenFile(t, "test-token")
	srv, cleanup, err := buildServer(buildOptions{
		Socket:        "/var/run/zen-swarm/zen-swarm.sock",
		DaemonURL:     "",
		AuthTokenPath: tokenPath,
	})
	if err != nil {
		t.Fatalf("buildServer with Socket unexpectedly failed: %v", err)
	}
	defer cleanup()
	if srv == nil {
		t.Fatal("buildServer returned nil server with no error")
	}
}

func TestRun_PropagatesBuildServerError(t *testing.T) {

	origArgs := os.Args
	defer func() { os.Args = origArgs }()
	os.Args = []string{"zen-mcp-budget"}

	err := run()
	if err == nil {
		t.Fatal("expected error from run() with missing --auth-token-path; got nil")
	}
	if !strings.Contains(err.Error(), "--auth-token-path") {
		t.Errorf("run() error must surface flag-validation message; got: %v", err)
	}
}

func TestRun_UnknownFlagError(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()
	os.Args = []string{"zen-mcp-budget", "--no-such-flag-xyz"}

	err := run()
	if err == nil {
		t.Fatal("expected error from run() for unknown flag; got nil")
	}
	if !strings.Contains(err.Error(), "parse flags") {
		t.Errorf("run() error must wrap with 'parse flags'; got: %v", err)
	}
}

func TestRun_HappyPathCleanupInvoked(t *testing.T) {
	tokenPath := writeTokenFile(t, "test-token")

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
		"zen-mcp-budget",
		"--daemon-url", "http://127.0.0.1:0",
		"--auth-token-path", tokenPath,
	}

	done := make(chan error, 1)
	go func() { done <- run() }()

	select {
	case <-done:

	case <-time.After(5 * time.Second):
		t.Fatal("run() did not return after stdin EOF — possible hang")
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

func TestMainBinaryGracefulShutdown(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	binPath := tmpDir + "/zen-mcp-budget"

	build := exec.Command("go", "build", "-o", binPath, ".")
	build.Dir = "."
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	tokenPath := filepath.Join(tmpDir, "token")
	if err := os.WriteFile(tokenPath, []byte("test-token"), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}

	cmd := exec.Command(binPath,
		"--auth-token-path", tokenPath,
		"--daemon-url", "http://127.0.0.1:0",
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
