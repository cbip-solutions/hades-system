// main_test.go — regression tests for C-16 + C-17 (CodeReview Plan 4
// Phase I): the binary MUST fail fast when the daemon is unreachable
// and --allow-fallback is NOT set, AND must log the wiring posture on
// startup so operators can grep for "NoOpBudget"/"BudgetAdapter" to
// confirm production posture vs degraded.
package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
)

func TestProductionFailFast(t *testing.T) {
	var logBuf bytes.Buffer
	srv, err := buildServer(context.Background(), buildOptions{

		Socket:        "/no/such/socket/zen-swarm.sock",
		DaemonURL:     "",
		AuthTokenPath: "/no/such/token",
		AllowFallback: false,
		LogOut:        &logBuf,
	})
	if err == nil {
		_ = srv.Close()
		t.Fatal("expected fail-fast on unreachable daemon without --allow-fallback")
	}

	if !strings.Contains(err.Error(), "daemon") {
		t.Errorf("error message missing 'daemon': %v", err)
	}
	if !strings.Contains(err.Error(), "--allow-fallback") {
		t.Errorf("error message missing '--allow-fallback' guidance: %v", err)
	}
}

func TestAllowFallbackProceedsWithWarn(t *testing.T) {
	var logBuf bytes.Buffer
	srv, err := buildServer(context.Background(), buildOptions{
		Socket:        "/no/such/socket/zen-swarm.sock",
		DaemonURL:     "",
		AuthTokenPath: "/no/such/token",
		AllowFallback: true,
		LogOut:        &logBuf,
	})
	if err != nil {
		t.Fatalf("buildServer with --allow-fallback unexpectedly failed: %v", err)
	}
	defer srv.Close()

	logged := logBuf.String()
	if !strings.Contains(logged, "WARN") {
		t.Errorf("expected WARN log line on fallback start; got: %s", logged)
	}
	if !strings.Contains(logged, "NoOp") {
		t.Errorf("expected fallback log to mention NoOp adapters; got: %s", logged)
	}

	if !strings.Contains(logged, "research MCP starting") {
		t.Errorf("expected startup posture line; got: %s", logged)
	}
	if !strings.Contains(logged, "NoOpBudget") {
		t.Errorf("expected startup line to identify NoOpBudget; got: %s", logged)
	}
	if !strings.Contains(logged, "NoOpAudit") {
		t.Errorf("expected startup line to identify NoOpAudit; got: %s", logged)
	}
}

func TestStartupPostureLineProductionHappy(t *testing.T) {
	var logBuf bytes.Buffer
	srv, err := buildServer(context.Background(), buildOptions{
		Socket:        "",
		DaemonURL:     "http://127.0.0.1:1",
		AuthTokenPath: writeTempToken(t),
		AllowFallback: false,
		LogOut:        &logBuf,
	})
	if err != nil {
		t.Fatalf("buildServer with valid URL failed: %v", err)
	}
	defer srv.Close()

	logged := logBuf.String()
	if !strings.Contains(logged, "research MCP starting") {
		t.Errorf("missing startup posture line; got: %s", logged)
	}

	if strings.Contains(logged, "NoOpBudget") || strings.Contains(logged, "NoOpAudit") {
		t.Errorf("production posture should NOT log NoOp adapters; got: %s", logged)
	}
	if !strings.Contains(logged, "BudgetAdapter") {
		t.Errorf("expected real BudgetAdapter type in startup line; got: %s", logged)
	}
	if !strings.Contains(logged, "AuditAdapter") {
		t.Errorf("expected real AuditAdapter type in startup line; got: %s", logged)
	}
}

func TestRun_MissingAuthTokenPath(t *testing.T) {
	origArgs := os.Args
	os.Args = []string{"zen-mcp-research", "--daemon-url", "http://localhost:0"}
	defer func() { os.Args = origArgs }()

	err := run()
	if err == nil {
		t.Fatal("expected error from run() with missing --auth-token-path; got nil")
	}
	if !strings.Contains(err.Error(), "--auth-token-path") {
		t.Errorf("run() error must mention '--auth-token-path' for operator UX; got: %v", err)
	}
}

func TestRun_UnknownFlag(t *testing.T) {
	origArgs := os.Args
	os.Args = []string{"zen-mcp-research", "--nonexistent-flag-xyz"}
	defer func() { os.Args = origArgs }()

	err := run()
	if err == nil {
		t.Fatal("expected error from run() for unknown flag; got nil")
	}
}

func TestRun_MissingTokenFileErrors(t *testing.T) {
	origArgs := os.Args
	os.Args = []string{
		"zen-mcp-research",
		"--auth-token-path", "/no/such/path/token",
		"--daemon-url", "http://127.0.0.1:0",
	}
	defer func() { os.Args = origArgs }()

	err := run()
	if err == nil {
		t.Fatal("expected error from run() when token file missing; got nil")
	}

	if !strings.Contains(err.Error(), "build server") && !strings.Contains(err.Error(), "daemon") {
		t.Errorf("run() error missing context; got: %v", err)
	}
}

func TestRun_AllowFallbackNoopAdapters(t *testing.T) {
	var logBuf bytes.Buffer

	srv, err := buildServer(context.Background(), buildOptions{
		Socket:        "/no/such/socket/zen-swarm.sock",
		DaemonURL:     "",
		AuthTokenPath: "/no/such/token",
		AllowFallback: true,
		LogOut:        &logBuf,
	})
	if err != nil {
		t.Fatalf("buildServer with --allow-fallback=true should succeed with NoOp adapters; got: %v", err)
	}
	defer srv.Close()
	logged := logBuf.String()
	if !strings.Contains(logged, "NoOpBudget") {
		t.Errorf("expected NoOpBudget in startup log; got: %s", logged)
	}
}

func writeTempToken(t *testing.T) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "token-*")
	if err != nil {
		t.Fatalf("create temp token: %v", err)
	}
	defer f.Close()
	if _, err := f.WriteString("test-token"); err != nil {
		t.Fatalf("write token: %v", err)
	}
	return f.Name()
}
