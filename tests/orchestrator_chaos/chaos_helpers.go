// SPDX-License-Identifier: MIT
// Package orchestrator_chaos provides the Tier 9 (orchestrator-chaos)
// build-tag-gated supervisor-failure suite. The helpers below spawn a
// real bin/zen-swarm-ctld subprocess inside an isolated temp directory,
// inject SIGKILL, and restart the daemon to validate replay-recovery
// (spec §5.4 + inv-zen-095 integration-level).
//
// Helpers live without a build tag (they are reused by smoke-style
// integration tests as well); the actual chaos cases hide behind
// // +build orchestrator_chaos because they take 10-60s each (subprocess
// build + spawn + kill + restart cycles).
//
// Daemon flag contract (see cmd/zen-swarm-ctld/main.go):
//
//	--uds  <path>   Unix domain socket path
//	--http <addr>   optional TCP HTTP address
//	--db   <path>   SQLite path
//
// The helpers SET --uds and --db explicitly (no --http) so the daemon
// runs entirely over the per-test UDS without contending for TCP ports.
//
// Subprocess lifetime: t.Cleanup is registered on every successful
// spawn so abrupt test failures still tear down the daemon. SIGTERM is
// the default Stop signal; chaos cases override with SIGKILL via Kill.
package orchestrator_chaos

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/client"
)

type DaemonOpts struct {
	DataDir string

	BuildOnce string
}

type TestDaemon struct {
	dataDir string
	binary  string
	socket  string
	dbPath  string

	mu      sync.Mutex
	cmd     *exec.Cmd
	stopped bool

	cli *client.Client
}

func SpawnTestDaemon(t *testing.T, opts DaemonOpts) *TestDaemon {
	t.Helper()
	dir := opts.DataDir
	if dir == "" {
		dir = t.TempDir()
	}

	binary := opts.BuildOnce
	if binary == "" {
		binary = filepath.Join(dir, "zen-swarm-ctld")
		repoRoot := findRepoRoot(t)
		// Build tags + ldflags MUST mirror the canonical Makefile
		// invocation (lines ~206, 567-569): -tags=sqlite_fts5 enables
		// FTS5 in store init; the ldflags rename the ncruces sqlite3
		// driver to "sqlite3_ncruces" so it can coexist with mattn/go-
		// sqlite3 without "sql: Register called twice for driver
		// sqlite3" panicking the daemon at init (driver-collision fix
		// shipped in 4d8cb401).
		build := exec.Command("go", "build",
			"-tags", "sqlite_fts5",
			"-ldflags", "-X github.com/ncruces/go-sqlite3/driver.driverName=sqlite3_ncruces",
			"-o", binary, "./cmd/zen-swarm-ctld")
		build.Dir = repoRoot
		build.Stdout, build.Stderr = os.Stdout, os.Stderr
		if err := build.Run(); err != nil {
			t.Fatalf("build daemon: %v", err)
		}
	}

	socket := shortSocketPath(t, dir)
	dbPath := filepath.Join(dir, "state.db")

	cmd := exec.Command(binary, "--uds", socket, "--db", dbPath)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr

	cmd.Env = append(os.Environ(), "ZEN_BYPASS_DISABLE_KEYCHAIN=1", "ZEN_KEYCHAIN_DISABLE=1")

	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("spawn daemon: %v", err)
	}

	d := &TestDaemon{
		dataDir: dir,
		binary:  binary,
		socket:  socket,
		dbPath:  dbPath,
		cmd:     cmd,
		cli:     client.New(socket),
	}

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if exited, _ := d.processExited(); exited {
			t.Fatalf("daemon exited before socket became reachable; check stderr")
		}
		conn, err := net.DialTimeout("unix", socket, 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			t.Cleanup(func() { _ = d.Stop(t) })
			return d
		}
		time.Sleep(50 * time.Millisecond)
	}
	_ = d.Stop(t)
	t.Fatalf("daemon did not bind socket within 30s: %s", socket)
	return nil
}

func shortSocketPath(t *testing.T, fallbackDir string) string {
	t.Helper()

	tmpDir := "/tmp"
	if _, err := os.Stat(tmpDir); err != nil {
		tmpDir = fallbackDir
	}
	pid := os.Getpid()
	name := fmt.Sprintf("zen-chaos-%d-%d.sock", pid, time.Now().UnixNano()%1_000_000)
	path := filepath.Join(tmpDir, name)
	t.Cleanup(func() { _ = os.Remove(path) })
	return path
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {

			data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
			if err == nil {
				if matchesModule(data, "github.com/cbip-solutions/hades-system") {
					return dir
				}
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("repo root not found from %s", cwd)
		}
		dir = parent
	}
}

func matchesModule(modBytes []byte, name string) bool {

	pat := []byte("module " + name)
	for i := 0; i+len(pat) <= len(modBytes); i++ {
		if string(modBytes[i:i+len(pat)]) == string(pat) {
			return true
		}
	}
	return false
}

func (d *TestDaemon) Client() *client.Client { return d.cli }

func (d *TestDaemon) DataDir() string { return d.dataDir }

func (d *TestDaemon) Socket() string { return d.socket }

func (d *TestDaemon) Binary() string { return d.binary }

func (d *TestDaemon) processExited() (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.cmd == nil || d.cmd.Process == nil {
		return true, nil
	}

	if d.cmd.ProcessState != nil {
		return true, nil
	}
	if err := d.cmd.Process.Signal(syscall.Signal(0)); err != nil {
		return true, err
	}
	return false, nil
}

func (d *TestDaemon) Stop(t *testing.T) error {
	t.Helper()
	d.mu.Lock()
	if d.stopped {
		d.mu.Unlock()
		return nil
	}
	d.stopped = true
	cmd := d.cmd
	d.mu.Unlock()

	if cmd == nil || cmd.Process == nil {
		return nil
	}
	_ = cmd.Process.Signal(syscall.SIGTERM)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:

	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		<-done
	}
	return nil
}

func (d *TestDaemon) Kill() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.cmd == nil || d.cmd.Process == nil {
		return errors.New("Kill: daemon not started")
	}
	if d.stopped {
		return errors.New("Kill: daemon already stopped")
	}
	d.stopped = true
	if runtime.GOOS == "windows" {
		// SIGKILL is not portable to Windows; fall back to .Kill() which
		// uses TerminateProcess. Chaos tests do not target Windows
		// today (the daemon is unix-only) but the helper stays portable
		// at the API level.
		return d.cmd.Process.Kill()
	}
	return syscall.Kill(d.cmd.Process.Pid, syscall.SIGKILL)
}

func (d *TestDaemon) Restart(t *testing.T) *TestDaemon {
	t.Helper()
	return SpawnTestDaemon(t, DaemonOpts{
		DataDir:   d.dataDir,
		BuildOnce: d.binary,
	})
}

func (d *TestDaemon) WaitFor(t *testing.T, pred func() bool, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if pred() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("WaitFor: predicate did not become true within %v", timeout)
}

func (d *TestDaemon) Health(ctx context.Context) (bool, error) {
	resp, err := d.cli.Health(ctx)
	if err != nil {
		return false, err
	}
	return resp != nil && resp.Status != "", nil
}
