// tests/orchestrator_chaos/chaos_helpers_test.go (Plan 5 Phase O Task O-6).
//
// These helper tests spawn a real bin/zen-swarm-ctld subprocess and
// exercise spawn/health/kill/restart. They do not require the
// orchestrator_chaos build tag (the helpers themselves are reusable),
// but they are skipped under -short because the daemon spawn cycle is
// slow (build + bind socket).
package orchestrator_chaos_test

import (
	"context"
	"testing"
	"time"

	chaos "github.com/cbip-solutions/hades-system/tests/orchestrator_chaos"
)

func TestSpawnTestDaemon_Smoke(t *testing.T) {
	if testing.Short() {
		t.Skip("daemon spawn is slow under -short")
	}
	d := chaos.SpawnTestDaemon(t, chaos.DaemonOpts{})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ok, err := d.Health(ctx)
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if !ok {
		t.Fatalf("Health returned false")
	}
}

func TestSpawnTestDaemon_BinaryReusable(t *testing.T) {
	if testing.Short() {
		t.Skip("daemon spawn is slow under -short")
	}
	d1 := chaos.SpawnTestDaemon(t, chaos.DaemonOpts{})
	if d1.Binary() == "" {
		t.Fatalf("Binary path is empty after build")
	}

	d2 := chaos.SpawnTestDaemon(t, chaos.DaemonOpts{BuildOnce: d1.Binary()})
	if d2.Binary() != d1.Binary() {
		t.Errorf("BuildOnce did not propagate: d1=%q d2=%q", d1.Binary(), d2.Binary())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := d2.Health(ctx); err != nil {
		t.Fatalf("d2.Health: %v", err)
	}
}

func TestKillAndRestart_RecoversCleanly(t *testing.T) {
	if testing.Short() {
		t.Skip("daemon spawn is slow under -short")
	}
	d := chaos.SpawnTestDaemon(t, chaos.DaemonOpts{})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := d.Health(ctx); err != nil {
		t.Fatalf("baseline Health: %v", err)
	}

	if err := d.Kill(); err != nil {
		t.Fatalf("Kill: %v", err)
	}

	d2 := d.Restart(t)

	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	d2.WaitFor(t, func() bool {
		ok, _ := d2.Health(ctx2)
		return ok
	}, 5*time.Second)
}

func TestKillTwice_ReportsError(t *testing.T) {
	if testing.Short() {
		t.Skip("daemon spawn is slow under -short")
	}
	d := chaos.SpawnTestDaemon(t, chaos.DaemonOpts{})

	if err := d.Kill(); err != nil {
		t.Fatalf("first Kill: %v", err)
	}
	if err := d.Kill(); err == nil {
		t.Errorf("second Kill should error (daemon already stopped)")
	}
}

func TestAccessors_ReturnNonEmpty(t *testing.T) {
	if testing.Short() {
		t.Skip("daemon spawn is slow under -short")
	}
	d := chaos.SpawnTestDaemon(t, chaos.DaemonOpts{})

	if d.Client() == nil {
		t.Errorf("Client() returned nil")
	}
	if d.DataDir() == "" {
		t.Errorf("DataDir() empty")
	}
	if d.Socket() == "" {
		t.Errorf("Socket() empty")
	}
	if d.Binary() == "" {
		t.Errorf("Binary() empty")
	}
}

func TestStop_IsIdempotent(t *testing.T) {
	if testing.Short() {
		t.Skip("daemon spawn is slow under -short")
	}
	d := chaos.SpawnTestDaemon(t, chaos.DaemonOpts{})
	if err := d.Stop(t); err != nil {
		t.Errorf("first Stop: %v", err)
	}

	if err := d.Stop(t); err != nil {
		t.Errorf("second Stop: %v", err)
	}
}
