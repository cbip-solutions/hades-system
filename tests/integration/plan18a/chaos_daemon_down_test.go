//go:build integration

package plan18a_integration_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestPlan18aFoundation_ChaosDaemonDownPlaceholderHint asserts D-6: when
// the daemon UDS is absent, hades surfaces a curated recovery hint pointing
// operators at the shipped `hades daemon` command family.
//
// (raw `zen-swarm-ctld -uds &`, banner "HADES: daemon unreachable") was
// replaced by the curated hint — banner "HADES: daemon not running" +
// `hades daemon install` / `hades daemon start`. The wrapper now also
// pre-flights the daemon on the bare path (kickstart-if-LaunchAgent-installed,
// hint otherwise); these sandboxes set HOME to a temp dir with no LaunchAgent
// installed, so the hint path is exercised (no real launchctl). D-6 now
// asserts the curated shape:
//   - exit code != 0 (wrapper failed gracefully; did NOT crash)
//   - stdout/stderr contains "hades daemon" (the shipped recovery command)
//   - stdout/stderr contains the substring matching the UDS path
//   - stdout/stderr contains "HADES: daemon not running"
//   - stdout/stderr does NOT contain a Go panic traceback
//     ("goroutine 1 [running]:" pattern or "panic:" prefix)
//
// inv-zen-088 (single-egress, pre-existing): the wrapper does NOT attempt
// to dial the daemon directly when the UDS is absent — it surfaces the
// hint and exits via the child's exit code (or 127 if the binary itself
// is missing from PATH).
//
// Scenario coverage:
//  1. UDS absent + stub hermes exits non-zero → hint MUST appear.
//  2. UDS absent + zen passthrough exits non-zero → hint MUST appear.
//  3. UDS absent + hermes binary not on PATH (127 path) → hint MUST appear.
//  4. UDS present + child exits non-zero → hint MUST NOT appear (false-
//     positive guard; the hint is daemon-down-specific).
func TestPlan18aFoundation_ChaosDaemonDownPlaceholderHint(t *testing.T) {
	t.Parallel()
	hadesBin := buildHadesBinary(t)

	t.Run("hermes_child_fails_uds_absent", func(t *testing.T) {
		t.Parallel()
		stubDir := t.TempDir()

		_ = buildStubBinaryAt(t, stubDir, "hermes", filepath.Join(stubDir, "hermes-record.jsonl"), 7)
		_ = buildStubBinaryAt(t, stubDir, "zen", filepath.Join(stubDir, "zen-record.jsonl"), 0)

		sandboxDir := t.TempDir()
		udsPath := filepath.Join(sandboxDir, "zen-swarm.sock")

		if _, err := os.Stat(udsPath); err == nil {
			t.Fatalf("pre-condition broken: UDS path %s already exists", udsPath)
		}

		env := newSandboxEnv(t, stubDir)
		env = append(env, "ZEN_DAEMON_UDS="+udsPath)

		cmd := exec.Command(hadesBin)
		cmd.Env = env
		out, err := cmd.CombinedOutput()

		s := string(out)
		assertChaosHintShape(t, s, udsPath)

		if err == nil {
			t.Errorf("wrapper exited 0; expected non-zero child-forwarded exit\noutput:\n%s", s)
		}
	})

	t.Run("zen_passthrough_fails_uds_absent", func(t *testing.T) {
		t.Parallel()
		stubDir := t.TempDir()

		_ = buildStubBinaryAt(t, stubDir, "zen", filepath.Join(stubDir, "zen-record.jsonl"), 1)
		_ = buildStubBinaryAt(t, stubDir, "hermes", filepath.Join(stubDir, "hermes-record.jsonl"), 0)

		sandboxDir := t.TempDir()
		udsPath := filepath.Join(sandboxDir, "zen-swarm.sock")

		env := newSandboxEnv(t, stubDir)
		env = append(env, "ZEN_DAEMON_UDS="+udsPath)

		cmd := exec.Command(hadesBin, "status")
		cmd.Env = env
		out, _ := cmd.CombinedOutput()
		s := string(out)
		assertChaosHintShape(t, s, udsPath)
	})

	t.Run("hermes_binary_missing_uds_absent", func(t *testing.T) {
		t.Parallel()

		stubDir := t.TempDir()

		_ = buildStubBinaryAt(t, stubDir, "zen", filepath.Join(stubDir, "zen-record.jsonl"), 0)

		sandboxDir := t.TempDir()
		udsPath := filepath.Join(sandboxDir, "zen-swarm.sock")

		env := newSandboxEnv(t, "")
		env = replacePATH(env, stubDir)
		env = append(env, "ZEN_DAEMON_UDS="+udsPath)

		cmd := exec.Command(hadesBin)
		cmd.Env = env
		out, err := cmd.CombinedOutput()

		s := string(out)
		assertChaosHintShape(t, s, udsPath)

		if !strings.Contains(s, "cannot launch hermes") {
			t.Errorf("expected 'cannot launch hermes' message; output:\n%s", s)
		}
		if err == nil {
			t.Errorf("wrapper exited 0 with hermes missing; expected non-zero")
		}
	})

	t.Run("uds_present_suppresses_hint", func(t *testing.T) {
		t.Parallel()
		stubDir := t.TempDir()

		_ = buildStubBinaryAt(t, stubDir, "hermes", filepath.Join(stubDir, "hermes-record.jsonl"), 5)
		_ = buildStubBinaryAt(t, stubDir, "zen", filepath.Join(stubDir, "zen-record.jsonl"), 0)

		sandboxDir := t.TempDir()
		udsPath := filepath.Join(sandboxDir, "fake.sock")

		if err := os.WriteFile(udsPath, []byte("not a socket"), 0o600); err != nil {
			t.Fatalf("write fake uds: %v", err)
		}

		env := newSandboxEnv(t, stubDir)
		env = append(env, "ZEN_DAEMON_UDS="+udsPath)

		cmd := exec.Command(hadesBin)
		cmd.Env = env
		out, _ := cmd.CombinedOutput()
		s := string(out)

		// Hint MUST NOT appear (false-positive guard).
		if strings.Contains(s, "HADES: daemon not running") {
			t.Errorf("hint emitted despite UDS path existing; output:\n%s", s)
		}
		if strings.Contains(s, "hades daemon install") {
			t.Errorf("recovery hint appeared despite UDS present; output:\n%s", s)
		}

		if strings.Contains(s, "panic:") || strings.Contains(s, "goroutine 1 [running]:") {
			t.Errorf("panic traceback in output:\n%s", s)
		}
	})
}

// assertChaosHintShape factors the substring assertions for the chaos hint
// out so each sub-test reads cleanly. Hint MUST contain (v0.17.2 / ADR-0099):
//
//	"HADES: daemon not running"  — banner-style header
//	"hades daemon"               — shipped recovery command family
//	udsPath                      — actual UDS probe path (echoed in hint)
//
// Hint MUST NOT contain:
//
//	"panic:"                     — Go runtime panic
//	"goroutine 1 [running]:"     — panic traceback signature
func assertChaosHintShape(t *testing.T, out, udsPath string) {
	t.Helper()
	wantSubs := []string{
		"HADES: daemon not running",
		"hades daemon",
		udsPath,
	}
	for _, s := range wantSubs {
		if !strings.Contains(out, s) {
			t.Errorf("chaos hint missing substring %q\nfull output:\n%s", s, out)
		}
	}
	bad := []string{"panic:", "goroutine 1 [running]:"}
	for _, b := range bad {
		if strings.Contains(out, b) {
			t.Errorf("chaos output contains panic marker %q\nfull output:\n%s", b, out)
		}
	}
}
