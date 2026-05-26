package litestream

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestRsyncSchedulerRunsAtCadence(t *testing.T) {
	dir := t.TempDir()
	scriptPath := writeFakeLitestreamScript(t, dir, fakeBehavior{exitCode: 0, sleepMs: 5})

	var calls atomic.Int32
	starter := func(ctx context.Context, name string, arg ...string) *exec.Cmd {
		calls.Add(1)
		return exec.CommandContext(ctx, "/bin/bash", append([]string{scriptPath}, arg...)...)
	}

	sched := NewRsyncScheduler(starter)
	sched.cadence = 30 * time.Millisecond

	tessDir := filepath.Join(dir, "tessera")
	_ = os.MkdirAll(tessDir, 0o700)

	if err := sched.StartProject(context.Background(), "zen-swarm", tessDir, []string{}); err != nil {
		t.Fatalf("StartProject: %v", err)
	}
	defer sched.StopProject(context.Background(), "zen-swarm")

	deadline := time.Now().Add(500 * time.Millisecond)
	for calls.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if calls.Load() < 3 {
		t.Fatalf("rsync ran %d times; want >=3", calls.Load())
	}
}

func TestRsyncSchedulerArgsContainBucketAndPrefix(t *testing.T) {
	dir := t.TempDir()
	scriptPath := writeFakeLitestreamScript(t, dir, fakeBehavior{exitCode: 0, sleepMs: 5})

	var capturedArgs atomic.Pointer[[]string]
	starter := func(ctx context.Context, name string, arg ...string) *exec.Cmd {
		copyArgs := append([]string(nil), arg...)
		capturedArgs.Store(&copyArgs)
		return exec.CommandContext(ctx, "/bin/bash", append([]string{scriptPath}, arg...)...)
	}

	sched := NewRsyncScheduler(starter)
	sched.cadence = 30 * time.Millisecond

	tessDir := filepath.Join(dir, "tessera")
	_ = os.MkdirAll(tessDir, 0o700)
	if err := sched.StartProject(context.Background(), "zen-swarm", tessDir, []string{}); err != nil {
		t.Fatalf("StartProject: %v", err)
	}
	defer sched.StopProject(context.Background(), "zen-swarm")

	deadline := time.Now().Add(500 * time.Millisecond)
	for capturedArgs.Load() == nil && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	got := capturedArgs.Load()
	if got == nil {
		t.Fatal("rsync was never invoked; args not captured")
	}
	args := *got

	wantBucket := "s3://zen-swarm-audit-zen-swarm/tessera/"
	if !containsArg(args, wantBucket) {
		t.Errorf("rsync args missing bucket prefix %q; args = %v", wantBucket, args)
	}
	if !containsArg(args, "sync") {
		t.Errorf("rsync args missing 'sync' verb; args = %v", args)
	}
	if !containsArg(args, "--delete") {
		t.Errorf("rsync args missing --delete; args = %v", args)
	}
}

func TestRsyncSchedulerRecordsLastErrorOnFailure(t *testing.T) {
	dir := t.TempDir()
	scriptPath := writeFakeLitestreamScript(t, dir, fakeBehavior{exitCode: 17, sleepMs: 1})

	starter := func(ctx context.Context, name string, arg ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "/bin/bash", append([]string{scriptPath}, arg...)...)
	}

	sched := NewRsyncScheduler(starter)
	sched.cadence = 30 * time.Millisecond
	if err := sched.StartProject(context.Background(), "zen-swarm", dir, []string{}); err != nil {
		t.Fatalf("StartProject: %v", err)
	}
	defer sched.StopProject(context.Background(), "zen-swarm")

	deadline := time.Now().Add(500 * time.Millisecond)
	for sched.LastError("zen-swarm") == "" && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if sched.LastError("zen-swarm") == "" {
		t.Errorf("LastError should be set after failure")
	}
}

func TestRsyncSchedulerRecordsLastSuccessTimestamp(t *testing.T) {
	dir := t.TempDir()
	scriptPath := writeFakeLitestreamScript(t, dir, fakeBehavior{exitCode: 0, sleepMs: 5})

	starter := func(ctx context.Context, name string, arg ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "/bin/bash", append([]string{scriptPath}, arg...)...)
	}

	sched := NewRsyncScheduler(starter)
	sched.cadence = 30 * time.Millisecond
	pre := time.Now()
	if err := sched.StartProject(context.Background(), "zen-swarm", dir, []string{}); err != nil {
		t.Fatalf("StartProject: %v", err)
	}
	defer sched.StopProject(context.Background(), "zen-swarm")

	deadline := time.Now().Add(500 * time.Millisecond)
	for sched.LastSuccess("zen-swarm").IsZero() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if sched.LastSuccess("zen-swarm").Before(pre) {
		t.Errorf("LastSuccess = %v, want after %v", sched.LastSuccess("zen-swarm"), pre)
	}
}

func TestRsyncCadenceForDoctrine(t *testing.T) {
	tests := []struct {
		doctrine string
		want     time.Duration
	}{
		{"max-scope", 24 * time.Hour},
		{"default", 7 * 24 * time.Hour},
		{"capa-firewall", 24 * time.Hour},
		{"", 24 * time.Hour},
		{"unknown", 24 * time.Hour},
	}
	for _, tc := range tests {
		t.Run(tc.doctrine, func(t *testing.T) {
			got := RsyncCadenceForDoctrine(tc.doctrine)
			if got != tc.want {
				t.Errorf("RsyncCadenceForDoctrine(%q) = %v, want %v", tc.doctrine, got, tc.want)
			}
		})
	}
}

func TestRsyncSchedulerStopProjectIdempotent(t *testing.T) {
	sched := NewRsyncScheduler(nil)
	if err := sched.StopProject(context.Background(), "no-such"); err != nil {
		t.Errorf("StopProject unknown: %v", err)
	}
}

func TestRsyncSchedulerStartProjectRejectsDuplicate(t *testing.T) {
	dir := t.TempDir()
	scriptPath := writeFakeLitestreamScript(t, dir, fakeBehavior{exitCode: 0, sleepMs: 5})
	starter := func(ctx context.Context, name string, arg ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "/bin/bash", append([]string{scriptPath}, arg...)...)
	}
	sched := NewRsyncScheduler(starter)
	sched.cadence = 100 * time.Millisecond
	if err := sched.StartProject(context.Background(), "zen-swarm", dir, []string{}); err != nil {
		t.Fatalf("StartProject 1: %v", err)
	}
	err := sched.StartProject(context.Background(), "zen-swarm", dir, []string{})
	if !errors.Is(err, ErrProjectAlreadyManaged) {
		t.Errorf("err = %v, want ErrProjectAlreadyManaged", err)
	}
	defer sched.StopProject(context.Background(), "zen-swarm")
}

func TestRsyncSchedulerStartProjectRejectsEmptyArgs(t *testing.T) {
	sched := NewRsyncScheduler(nil)
	if err := sched.StartProject(context.Background(), "", "/tmp", []string{}); err == nil {
		t.Error("StartProject with empty project_id should error")
	}
	if err := sched.StartProject(context.Background(), "alpha", "", []string{}); err == nil {
		t.Error("StartProject with empty tesseraDir should error")
	}
}

func TestRsyncSchedulerStatusForUnknownProject(t *testing.T) {
	sched := NewRsyncScheduler(nil)
	if got := sched.LastSuccess("no-such"); !got.IsZero() {
		t.Errorf("LastSuccess for unknown = %v, want zero", got)
	}
	if got := sched.LastError("no-such"); got != "" {
		t.Errorf("LastError for unknown = %q, want empty", got)
	}
}

func TestRsyncSchedulerRunOnceNilStarterIsNoop(t *testing.T) {
	sched := NewRsyncScheduler(nil)
	sched.cadence = 30 * time.Millisecond
	dir := t.TempDir()
	if err := sched.StartProject(context.Background(), "alpha", dir, nil); err != nil {
		t.Fatalf("StartProject: %v", err)
	}
	defer sched.StopProject(context.Background(), "alpha")

	time.Sleep(80 * time.Millisecond)
	if !sched.LastSuccess("alpha").IsZero() {
		t.Errorf("LastSuccess should remain zero with nil starter")
	}
}

func TestRsyncSchedulerStopProjectHonoursContext(t *testing.T) {
	dir := t.TempDir()

	scriptPath := writeFakeLitestreamScript(t, dir, fakeBehavior{exitCode: 0, sleepMs: 5000})
	starter := func(ctx context.Context, name string, arg ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "/bin/bash", append([]string{scriptPath}, arg...)...)
	}
	sched := NewRsyncScheduler(starter)
	sched.cadence = 30 * time.Millisecond
	if err := sched.StartProject(context.Background(), "alpha", dir, nil); err != nil {
		t.Fatalf("StartProject: %v", err)
	}

	time.Sleep(20 * time.Millisecond)
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	err := sched.StopProject(cancelledCtx, "alpha")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("StopProject with cancelled ctx = %v, want context.Canceled", err)
	}

	if err := sched.StopProject(context.Background(), "alpha"); err != nil {
		t.Errorf("idempotent StopProject after cancelled-ctx call: %v", err)
	}
}

func containsArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}
