// SPDX-License-Identifier: MIT
package testhelpers

import (
	"context"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func RepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)

	return filepath.Dir(filepath.Dir(filepath.Dir(file)))
}

func SpawnDaemon(t *testing.T) string {
	t.Helper()
	uds, _, _ := SpawnDaemonWithPID(t)
	return uds
}

func SpawnDaemonWithPID(t *testing.T) (uds string, pid int, dbPath string) {
	t.Helper()
	root := RepoRoot(t)
	ctld := filepath.Join(root, "bin", "zen-swarm-ctld")
	dir := t.TempDir()
	uds = filepath.Join(dir, "test.sock")
	dbPath = filepath.Join(dir, "state.db")

	cmd := exec.Command(ctld, "-uds", uds, "-db", dbPath)
	// Inherit the parent env, then force-disable Keychain bootstrap.
	//
	// Why: bootstrapBypassClient → LoadCredentials → LoadCredentialsFromKeychain
	// hits the macOS Security framework synchronously. On a GitHub
	// Actions macos-14 runner the login keychain is locked (no GUI
	// session attached), so SecItemCopyMatching either blocks until
	// the test's 2-second health probe times out OR surfaces an ACL
	// prompt nobody can answer. The same v0.2.1 audit-crypto patch
	// that fixed bootstrap_test.go applies here: e2e tests are not
	// validating bypass.Client functionality, just the daemon's HTTP
	// surface, so we isolate them from the operator's real Keychain
	// (and from CI's locked one) via the documented env-var escape
	// hatch.
	//
	// Production daemon callers MUST NOT set this var — see
	// private-tier1-module/credentials_keychain_darwin.go and
	// audit_crypto_darwin.go for the dual checks.
	cmd.Env = append(os.Environ(),
		"ZEN_BYPASS_DISABLE_KEYCHAIN=1",
		"ZEN_KEYCHAIN_DISABLE=1",
	)
	if err := cmd.Start(); err != nil {
		t.Fatalf("spawn ctld: %v", err)
	}
	pid = cmd.Process.Pid
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := net.Dial("unix", uds); err == nil {
			return uds, pid, dbPath
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("daemon socket %q never appeared", uds)
	return "", 0, ""
}

func HTTPClientForUDS(uds string) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", uds)
			},
		},
		Timeout: 2 * time.Second,
	}
}
