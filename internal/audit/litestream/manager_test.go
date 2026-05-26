package litestream

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeStarter captures the exec.CommandContext invocations so tests can
// inspect the command+args the Manager would have run, and substitutes a
// caller-controlled subprocess behaviour. The returned *exec.Cmd MUST
// have its Path/Args populated as the real cmd would (Manager reads them
// for log prefixes); the actual subprocess that runs is whatever the
// caller-supplied scriptPath does.
//
// Synchronisation gotPath / gotArgs are guarded by mu because they are
// written from the supervisor goroutine and read from the test goroutine
// after a busy-wait on calls.Load(). The atomic counter alone is
// insufficient because the writes to gotPath / gotArgs happen alongside
// the atomic Add, not before it; the race detector correctly flags that
// without an explicit happens-before edge. Using a mutex for the
// reflective fields keeps the atomic counter free for the polling loop.
type fakeStarter struct {
	calls atomic.Int32
	mu    sync.Mutex

	gotPath string
	gotArgs []string

	scriptPath string
}

func (f *fakeStarter) start(ctx context.Context, name string, arg ...string) *exec.Cmd {
	f.mu.Lock()
	f.gotPath = name
	f.gotArgs = append([]string(nil), arg...)
	f.mu.Unlock()
	f.calls.Add(1)

	cmd := exec.CommandContext(ctx, "/bin/bash", append([]string{f.scriptPath}, arg...)...)
	return cmd
}

func (f *fakeStarter) snapshot() (path string, args []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.gotPath, append([]string(nil), f.gotArgs...)
}

func TestManagerStartsSubprocessPerProject(t *testing.T) {
	dir := t.TempDir()
	scriptPath := writeFakeLitestreamScript(t, dir, fakeBehavior{exitCode: 0, sleepMs: 50})

	fs := &fakeStarter{scriptPath: scriptPath}
	mgr := NewManagerForTest(fs.start)

	cfgPath := filepath.Join(dir, "litestream-zen.yml")
	if err := writeStubConfig(cfgPath); err != nil {
		t.Fatalf("writeStubConfig: %v", err)
	}

	if err := mgr.StartProject(context.Background(), "project-zen-swarm", cfgPath); err != nil {
		t.Fatalf("StartProject: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for fs.calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if fs.calls.Load() == 0 {
		t.Fatal("starter never invoked")
	}
	gotPath, gotArgs := fs.snapshot()
	if gotPath != "litestream" {
		t.Errorf("Path = %q, want litestream", gotPath)
	}
	wantArgs := []string{"replicate", "-config", cfgPath}
	if len(gotArgs) != len(wantArgs) {
		t.Fatalf("Args = %v, want %v", gotArgs, wantArgs)
	}
	for i := range wantArgs {
		if gotArgs[i] != wantArgs[i] {
			t.Errorf("Args[%d] = %q, want %q", i, gotArgs[i], wantArgs[i])
		}
	}

	if err := mgr.StopProject(context.Background(), "project-zen-swarm"); err != nil {
		t.Errorf("StopProject: %v", err)
	}
}

func TestManagerSupervisorRestartsOnCrash(t *testing.T) {
	dir := t.TempDir()

	scriptPath := writeFakeLitestreamScript(t, dir, fakeBehavior{exitCode: 1, sleepMs: 1})

	fs := &fakeStarter{scriptPath: scriptPath}
	mgr := NewManagerForTest(fs.start)
	mgr.backoffInitial = 10 * time.Millisecond
	mgr.backoffCap = 50 * time.Millisecond

	cfgPath := filepath.Join(dir, "litestream-zen.yml")
	_ = writeStubConfig(cfgPath)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := mgr.StartProject(ctx, "project-zen-swarm", cfgPath); err != nil {
		t.Fatalf("StartProject: %v", err)
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for fs.calls.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if fs.calls.Load() < 3 {
		t.Fatalf("supervisor did not restart, calls = %d", fs.calls.Load())
	}

	cancel()

	if err := mgr.StopProject(context.Background(), "project-zen-swarm"); err != nil {
		t.Errorf("StopProject after cancel: %v", err)
	}
}

func TestManagerStopProjectStopsSupervisor(t *testing.T) {
	dir := t.TempDir()
	scriptPath := writeFakeLitestreamScript(t, dir, fakeBehavior{exitCode: 1, sleepMs: 1})

	fs := &fakeStarter{scriptPath: scriptPath}
	mgr := NewManagerForTest(fs.start)
	mgr.backoffInitial = 10 * time.Millisecond
	mgr.backoffCap = 50 * time.Millisecond

	cfgPath := filepath.Join(dir, "litestream-zen.yml")
	_ = writeStubConfig(cfgPath)

	if err := mgr.StartProject(context.Background(), "project-zen-swarm", cfgPath); err != nil {
		t.Fatalf("StartProject: %v", err)
	}

	time.Sleep(80 * time.Millisecond)
	pre := fs.calls.Load()

	if err := mgr.StopProject(context.Background(), "project-zen-swarm"); err != nil {
		t.Errorf("StopProject: %v", err)
	}

	time.Sleep(80 * time.Millisecond)
	post := fs.calls.Load()
	if post-pre > 1 {
		t.Errorf("supervisor still restarting after Stop: pre=%d post=%d", pre, post)
	}
}

func TestManagerStartProjectRejectsDuplicate(t *testing.T) {
	dir := t.TempDir()
	scriptPath := writeFakeLitestreamScript(t, dir, fakeBehavior{exitCode: 0, sleepMs: 50})

	fs := &fakeStarter{scriptPath: scriptPath}
	mgr := NewManagerForTest(fs.start)

	cfgPath := filepath.Join(dir, "litestream-zen.yml")
	_ = writeStubConfig(cfgPath)

	if err := mgr.StartProject(context.Background(), "project-zen-swarm", cfgPath); err != nil {
		t.Fatalf("StartProject 1: %v", err)
	}
	err := mgr.StartProject(context.Background(), "project-zen-swarm", cfgPath)
	if !errors.Is(err, ErrProjectAlreadyManaged) {
		t.Errorf("err = %v, want ErrProjectAlreadyManaged", err)
	}

	if err := mgr.StopProject(context.Background(), "project-zen-swarm"); err != nil {
		t.Errorf("StopProject: %v", err)
	}
}

func TestManagerStopProjectUnknownReturnsNil(t *testing.T) {
	mgr := NewManagerForTest(nil)

	if err := mgr.StopProject(context.Background(), "no-such-project"); err != nil {
		t.Errorf("StopProject unknown: %v, want nil", err)
	}
}

func TestManagerStartProjectValidation(t *testing.T) {
	mgr := NewManagerForTest(nil)
	if err := mgr.StartProject(context.Background(), "", "/tmp/cfg.yml"); err == nil {
		t.Error("StartProject empty projectID = nil, want non-nil error")
	}
	if err := mgr.StartProject(context.Background(), "p1", ""); err == nil {
		t.Error("StartProject empty cfgPath = nil, want non-nil error")
	}
}

func TestManagerStopAll(t *testing.T) {
	dir := t.TempDir()
	scriptPath := writeFakeLitestreamScript(t, dir, fakeBehavior{exitCode: 0, sleepMs: 50})

	fs := &fakeStarter{scriptPath: scriptPath}
	mgr := NewManagerForTest(fs.start)

	cfgPath := filepath.Join(dir, "litestream-zen.yml")
	if err := writeStubConfig(cfgPath); err != nil {
		t.Fatalf("writeStubConfig: %v", err)
	}

	for _, pid := range []string{"project-a", "project-b", "project-c"} {
		if err := mgr.StartProject(context.Background(), pid, cfgPath); err != nil {
			t.Fatalf("StartProject %s: %v", pid, err)
		}
	}

	if err := mgr.StopAll(context.Background()); err != nil {
		t.Errorf("StopAll: %v", err)
	}

	if err := mgr.StopAll(context.Background()); err != nil {
		t.Errorf("StopAll empty: %v", err)
	}
}

func TestManagerStartProjectInjectsEnvVars(t *testing.T) {
	dir := t.TempDir()

	envOut := filepath.Join(dir, "env.txt")
	scriptBody := `#!/bin/bash
env > ` + envOut + `
sleep 0.5
`
	scriptPath := filepath.Join(dir, "fake-litestream-env.sh")
	if err := os.WriteFile(scriptPath, []byte(scriptBody), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	fs := &fakeStarter{scriptPath: scriptPath}
	mgr := NewManagerForTest(fs.start)
	mgr.envForProject = func(projectID string) []string {
		return []string{
			"LITESTREAM_ACCESS_KEY_ID=AKIATESTKEY",
			"LITESTREAM_SECRET_ACCESS_KEY=secrettestvalue",
		}
	}

	cfgPath := filepath.Join(dir, "litestream-zen.yml")
	_ = writeStubConfig(cfgPath)
	if err := mgr.StartProject(context.Background(), "zen-swarm", cfgPath); err != nil {
		t.Fatalf("StartProject: %v", err)
	}
	defer mgr.StopProject(context.Background(), "zen-swarm")

	deadline := time.Now().Add(3 * time.Second)
	var lastSeen string
	for time.Now().Before(deadline) {
		body, err := os.ReadFile(envOut)
		if err == nil {
			s := string(body)
			lastSeen = s
			if strings.Contains(s, "LITESTREAM_ACCESS_KEY_ID=AKIATESTKEY") &&
				strings.Contains(s, "LITESTREAM_SECRET_ACCESS_KEY=secrettestvalue") {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("env vars never observed in env.txt within deadline; last seen:\n%s", lastSeen)
}

func TestManagerStopProjectCtxCanceled(t *testing.T) {
	dir := t.TempDir()

	scriptPath := writeFakeLitestreamScript(t, dir, fakeBehavior{exitCode: 0, sleepMs: 5000})

	fs := &fakeStarter{scriptPath: scriptPath}
	mgr := NewManagerForTest(fs.start)

	cfgPath := filepath.Join(dir, "litestream-zen.yml")
	_ = writeStubConfig(cfgPath)

	if err := mgr.StartProject(context.Background(), "project-zen-swarm", cfgPath); err != nil {
		t.Fatalf("StartProject: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for fs.calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}

	stopCtx, stopCancel := context.WithCancel(context.Background())
	stopCancel()

	err := mgr.StopProject(stopCtx, "project-zen-swarm")
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Errorf("StopProject(canceled-ctx): err = %v, want nil or context.Canceled", err)
	}

	_ = mgr.StopProject(context.Background(), "project-zen-swarm")
}
