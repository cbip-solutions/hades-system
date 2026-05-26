// SPDX-License-Identifier: MIT
// Package testhelpers — onboard test daemon harness.
//
// contract (per project memory feedback_macos_keychain_ci_blocker.md)
// so onboarding integration tests don't hang on macOS CI runners where
// the login keychain is locked.
//
// Phase A surfaces work daemon-less (preflight / prefs / plugin / mcp
// are pure-function or filesystem-only); this shim sets up the env
// scaffolding subsequent Phase B/C/D/E/F integration tests inherit.
//
// Design note: a richer daemon harness (`SpawnDaemon` /
// `SpawnDaemonWithPID`) already exists in `tests/testhelpers/daemon.go`
// for full HTTP-surface coverage; that helper requires `bin/zen-swarm-ctld`
// to exist (run `make build` first) and is invoked by e2e + chaos suites.
// The onboarding harness here is intentionally LIGHTER — it surfaces
// the env-var contract without spawning a real daemon process, because
// Phase A onboarding code never speaks to the daemon. When Phase B/C/D
// integration tests need the daemon they should compose:
//
//	td := testhelpers.NewOnboardTestDaemon(t)
//	defer td.Stop()
//	uds := testhelpers.SpawnDaemon(t)  // inherits the env var
//	// ...
//
// The Stop() method is safe to call multiple times (idempotent) and
// restores the env to its pre-test state via t.Cleanup or explicit
// invocation.
package testhelpers

import (
	"os"
	"testing"
)

const envKeyKeychainDisable = "ZEN_BYPASS_DISABLE_KEYCHAIN"

type OnboardTestDaemon struct {
	t           *testing.T
	prevValue   string
	prevPresent bool
	stopped     bool
}

func NewOnboardTestDaemon(t *testing.T) *OnboardTestDaemon {
	t.Helper()
	td := &OnboardTestDaemon{t: t}
	td.prevValue, td.prevPresent = os.LookupEnv(envKeyKeychainDisable)
	if err := os.Setenv(envKeyKeychainDisable, "1"); err != nil {
		t.Fatalf("set %s: %v", envKeyKeychainDisable, err)
	}
	t.Cleanup(td.Stop)
	return td
}

func (td *OnboardTestDaemon) Stop() {
	if td == nil || td.t == nil {
		return
	}
	if td.stopped {
		return
	}
	td.stopped = true
	td.t.Helper()
	if !td.prevPresent {

		if err := os.Unsetenv(envKeyKeychainDisable); err != nil {
			td.t.Errorf("unset %s during cleanup: %v", envKeyKeychainDisable, err)
		}
		return
	}
	if err := os.Setenv(envKeyKeychainDisable, td.prevValue); err != nil {
		td.t.Errorf("restore %s=%q during cleanup: %v", envKeyKeychainDisable, td.prevValue, err)
	}
}

func KeychainEnvKey() string { return envKeyKeychainDisable }
