package testhelpers_test

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/tests/testhelpers"
)

func TestCrashInjector_KillSubprocess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("crash injector relies on POSIX signals")
	}
	t.Parallel()

	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	pid := cmd.Process.Pid

	ci := testhelpers.NewCrashInjector()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := ci.KillProcess(ctx, pid); err != nil {
		t.Fatalf("KillProcess: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:

	case <-time.After(3 * time.Second):
		t.Fatalf("subprocess not killed within 3s")
	}

	if err := ci.WaitProcessGone(ctx, pid); err != nil {
		t.Errorf("WaitProcessGone: %v", err)
	}
}

func TestCrashInjector_RegisterEventTrigger(t *testing.T) {
	t.Parallel()
	ci := testhelpers.NewCrashInjector()

	triggered := false
	ci.RegisterTrigger("after_event_3", func() { triggered = true })

	for i := 1; i <= 3; i++ {
		if i == 3 {
			ci.FireTrigger("after_event_3")
		}
	}

	if !triggered {
		t.Errorf("trigger not fired")
	}

	_ = os.Stderr
}

func TestCrashInjector_FireUnregistered(t *testing.T) {
	t.Parallel()
	ci := testhelpers.NewCrashInjector()

	ci.FireTrigger("never_registered")
}

func TestCrashInjector_RegisterOverwrite(t *testing.T) {
	t.Parallel()
	ci := testhelpers.NewCrashInjector()
	var first, second bool
	ci.RegisterTrigger("ev", func() { first = true })
	ci.RegisterTrigger("ev", func() { second = true })
	ci.FireTrigger("ev")
	if first {
		t.Errorf("first callback should be replaced")
	}
	if !second {
		t.Errorf("second callback should fire")
	}
}

func TestCrashInjector_SendSignal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX signals only")
	}
	t.Parallel()

	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	pid := cmd.Process.Pid
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	ci := testhelpers.NewCrashInjector()

	if err := ci.SendSignal(pid, syscall.SIGTERM); err != nil {
		t.Fatalf("SendSignal: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatalf("subprocess did not exit on SIGTERM within 3s")
	}
}

func TestCrashInjector_WaitProcessGoneCtxCancel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX signals only")
	}
	t.Parallel()

	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	pid := cmd.Process.Pid
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})

	ci := testhelpers.NewCrashInjector()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := ci.WaitProcessGone(ctx, pid)
	if err == nil {
		t.Errorf("expected ctx-deadline error for live process, got nil")
	}
	if !errIsDeadline(err) {
		t.Errorf("expected DeadlineExceeded-style error, got %v", err)
	}
}

func errIsDeadline(err error) bool {
	return err == context.DeadlineExceeded || err == context.Canceled
}

func TestCrashInjector_KillProcess_DeadPID(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX signals only")
	}
	t.Parallel()

	cmd := exec.Command("sleep", "0.01")
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	pid := cmd.Process.Pid
	if err := cmd.Wait(); err != nil {

	}

	ci := testhelpers.NewCrashInjector()
	ctx := context.Background()
	if err := ci.KillProcess(ctx, pid); err == nil {
		t.Errorf("expected error killing dead pid, got nil")
	}
}

func TestCrashInjector_SendSignal_DeadPID(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX signals only")
	}
	t.Parallel()

	cmd := exec.Command("sleep", "0.01")
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	pid := cmd.Process.Pid
	_ = cmd.Wait()

	ci := testhelpers.NewCrashInjector()
	if err := ci.SendSignal(pid, syscall.SIGTERM); err == nil {
		t.Errorf("expected error sending signal to dead pid, got nil")
	}
}
