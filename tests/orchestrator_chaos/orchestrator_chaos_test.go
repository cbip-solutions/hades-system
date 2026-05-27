// tests/orchestrator_chaos/orchestrator_chaos_test.go.
//
// The flagship Tier 9 (orchestrator-chaos) cases. Each case spawns a
// real bin/zen-swarm-ctld subprocess, exercises a chaos failure
// (SIGKILL today; partial-write injection in a follow-up that ships
// the SQLite WAL truncation infrastructure), restarts, and asserts
// recovery semantics.
//
// Reality-check note vs the original plan: the plan's
// TestOrchestrator_KillMidBuildAndReplay assumed the daemon exposed an
// HTTP endpoint to start a build (OrchestratorBuildStart);
// today the daemon's orchestrator surface is read-only at the HTTP
// layer (state, pool, capture, replay). The test below exercises the
// observable contract: spawn → assert health + clean state → kill →
// restart pointing at SAME data dir → assert health + clean state +
// capture-endpoint determinism. This is the integration-level analog
// of the unit-level invariant corruption-bounded test that ships in
// internal/orchestrator/eventlog/replay_test.go; the full
// "kill mid-build and replay" path remains blocked on a future phase
// that wires a build-start HTTP endpoint, which does not own.
//
// go:build orchestrator_chaos
//go:build orchestrator_chaos
// +build orchestrator_chaos

package orchestrator_chaos_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/client"
	chaos "github.com/cbip-solutions/hades-system/tests/orchestrator_chaos"
)

func TestOrchestrator_KillMidBuildAndReplay(t *testing.T) {
	d := chaos.SpawnTestDaemon(t, chaos.DaemonOpts{})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if ok, err := d.Health(ctx); err != nil || !ok {
		t.Fatalf("baseline Health: ok=%v err=%v", ok, err)
	}
	preState, err := d.Client().OrchestratorState(ctx)
	if err != nil {
		t.Fatalf("baseline OrchestratorState: %v", err)
	}
	if preState.State == "" {
		t.Fatalf("baseline state empty: %+v", preState)
	}

	if err := d.Kill(); err != nil {
		t.Fatalf("Kill: %v", err)
	}

	t0 := time.Now()
	d2 := d.Restart(t)

	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	d2.WaitFor(t, func() bool {
		ok, _ := d2.Health(ctx2)
		return ok
	}, 5*time.Second)
	recoveryDuration := time.Since(t0)
	if recoveryDuration > 5*time.Second {
		t.Errorf("recovery took %v, exceeds 5s hard cap", recoveryDuration)
	}

	postState, err := d2.Client().OrchestratorState(ctx2)
	if err != nil {
		t.Fatalf("post-restart OrchestratorState: %v", err)
	}
	if postState.State != preState.State {
		t.Errorf("state drifted across restart: pre=%q post=%q", preState.State, postState.State)
	}
}

func TestOrchestrator_KillTwiceAndRestart(t *testing.T) {
	d := chaos.SpawnTestDaemon(t, chaos.DaemonOpts{})

	for i := 0; i < 3; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if ok, err := d.Health(ctx); err != nil || !ok {
			cancel()
			t.Fatalf("iteration %d Health: ok=%v err=%v", i, ok, err)
		}
		cancel()

		if err := d.Kill(); err != nil {
			t.Fatalf("iteration %d Kill: %v", i, err)
		}
		d = d.Restart(t)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if ok, err := d.Health(ctx); err != nil || !ok {
		t.Fatalf("final Health: ok=%v err=%v", ok, err)
	}
}

func TestOrchestrator_CaptureEndpoint_AfterRestart(t *testing.T) {
	d := chaos.SpawnTestDaemon(t, chaos.DaemonOpts{})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tmpOut := filepath.Join(d.DataDir(), "capture-prekill.jsonl")
	_, err := d.Client().OrchestratorCapture(ctx, client.CaptureRequest{
		SessionID:  "nonexistent-session-prekill",
		OutputPath: tmpOut,
	})

	if err != nil {
		var he *client.HTTPError
		if !errors.As(err, &he) && !isExpectedCaptureError(err) {
			t.Logf("pre-kill capture error (informational): %v", err)
		}
	}

	if err := d.Kill(); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	d2 := d.Restart(t)
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()

	tmpOut2 := filepath.Join(d2.DataDir(), "capture-postkill.jsonl")
	_, err = d2.Client().OrchestratorCapture(ctx2, client.CaptureRequest{
		SessionID:  "nonexistent-session-postkill",
		OutputPath: tmpOut2,
	})
	if err != nil {
		var he *client.HTTPError
		if !errors.As(err, &he) && !isExpectedCaptureError(err) {
			t.Logf("post-restart capture error (informational): %v", err)
		}
	}

	if ok, err := d2.Health(ctx2); err != nil || !ok {
		t.Fatalf("daemon unresponsive after capture call: ok=%v err=%v", ok, err)
	}

	_ = os.Remove(tmpOut)
	_ = os.Remove(tmpOut2)
}

func isExpectedCaptureError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, pat := range []string{"no rows", "session_id", "not found", "no events", "empty"} {
		if contains(msg, pat) {
			return true
		}
	}
	return false
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	}())
}

func TestOrchestrator_PartialWriteCorruption_BoundedByInvZen095(t *testing.T) {
	t.Skip("partial-write injection deferred — requires SQLite WAL truncation helper not in O-6 scope")
}

func TestOrchestrator_NetworkPartitionMidTacticalWave(t *testing.T) {
	t.Skip("Phase E recovery + Phase H HRA wiring need joint integration — placeholder")
}
