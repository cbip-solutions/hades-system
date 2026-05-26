// tests/compliance/inv_zen_287_test.go
//
// Compliance gate for inv-zen-287 (v0.20.4 fix + v0.20.5 wider-sweep
// extension): subprocess-test deadlines in fourteen target test files
// MUST be at least 30 seconds. Pre-v0.20.4 these sites used a 2-second
// deadline that starved exec.CommandContext / waitForUDS / waitForCount
// / time.After under GOMAXPROCS-parallel `make test` load (subprocess
// spawn for `gh` / `tmux` / daemon HTTP / UDS / TCP / row-count polling
// exceeded 2s when other lanes saturated the host) and produced
// false-negative failures in:
//
//   - v0.20.4 scope (3 files): TestGhPoller_HeadSHA_* (5 tests in
//     internal/scheduler) and TestRealPaneListerListPanes* (2 tests
//     in internal/tmuxlife) plus the HealthMonitor select-case
//     timeouts in internal/tmuxlife/api_p7_test.go. v0.20.3 HANDOFF
//     documented both families as PASS-isolated/FAIL-under-load
//     baselines; v0.20.4 bumped to 30s (15x safety margin).
//
//   - v0.20.5 wider sweep (11 additional files): cmd/zen-event-poster
//     daemon-receive deadlines, cmd/hades waitForUDS socket polling,
//     cmd/zen-swarm-ctld log-drain + subsystem-snapshot + production
//     boot UDS/TCP smoke polling, tests/testharness openclaude-fake
//     subprocess crash/hang/read-error/nil-out paths, tests/integration
//     onboard_customize Wizard.Run integration deadline,
//     tests/compliance inv-zen-081 sshexec deadline +
//     inv-zen-175 Hermes-required preflight deadline,
//     tests/replay inbox_replay row-count polling, and tests/chaos
//     inbox_aggregator_divergence row-count polling at two sites.
//     across 11 files) as the "v0.20.5 timing-debt sweep" candidate;
//     introducing a successor invariant.
//
// Anchor 1 (negative): each target file MUST NOT contain the literal
// `2*time.Second` nor `2 * time.Second`. A regression that copies the
// old shape — or reverts the bump under pressure — trips the gate
// immediately.
//
// Anchor 2 (positive): each target file MUST contain at least one of
// `30*time.Second` or `30 * time.Second`. This proves the bump landed
// and is still in place (defends against accidental removal that ALSO
// drops the file from the negative-anchor coverage).
//
// Sister-test bite check: revert any one target file to `2*time.Second`;
// the negative anchor MUST fail. Revert the bump without restoring 2s
// (e.g. delete the deadline call entirely); the positive anchor MUST
// fail. Both behaviours were verified at v0.20.4 ship time + v0.20.5
// post-extension bite-check.
//
// Why a single discrete invariant for what looks like a routine bump:
// the 2s deadline shape is structurally fragile to test-host contention
// and is easy to reintroduce via copy-paste from older test files.
// Pinning the 14 known-affected files prevents the contention
// regression from coming back through the back door. The
// single-invariant approach (vs. introducing inv-zen-288 for v0.20.5)
// reflects that the v0.20.5 extension is semantically identical to
// the discipline.
//
// inv-zen-287 (v0.20.4 fix; v0.20.5 extension).
package compliance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type subprocessTimeoutBumpTargetFile struct {
	relPath       string
	affectedTests []string
}

var subprocessTimeoutBumpFiles = []subprocessTimeoutBumpTargetFile{

	{
		relPath:       filepath.Join("internal", "scheduler", "gitpoll_test.go"),
		affectedTests: []string{"TestGhPoller_HeadSHA_*"},
	},
	{
		relPath:       filepath.Join("internal", "tmuxlife", "drift_test.go"),
		affectedTests: []string{"TestRealPaneListerListPanes*"},
	},
	{
		relPath:       filepath.Join("internal", "tmuxlife", "api_p7_test.go"),
		affectedTests: []string{"TestHealthMonitor_Run_*", "TestHealthMonitor_OnOrphan*"},
	},

	{
		relPath:       filepath.Join("cmd", "zen-event-poster", "main_test.go"),
		affectedTests: []string{"TestEventPoster_*"},
	},
	{
		relPath:       filepath.Join("cmd", "hades", "main_test.go"),
		affectedTests: []string{"TestWaitForUDS_*"},
	},
	{
		relPath:       filepath.Join("cmd", "zen-swarm-ctld", "log_brand_test.go"),
		affectedTests: []string{"TestLogBrand_*"},
	},
	{
		relPath:       filepath.Join("cmd", "zen-swarm-ctld", "subsystem_snapshot_test.go"),
		affectedTests: []string{"TestRunSubsystemSnapshotLogger_*"},
	},
	{
		relPath:       filepath.Join("cmd", "zen-swarm-ctld", "production_boot_smoke_test.go"),
		affectedTests: []string{"TestProductionBootSmoke_*"},
	},
	{
		relPath:       filepath.Join("tests", "testharness", "openclaude_fake_test.go"),
		affectedTests: []string{"TestOpenClaudeFake*"},
	},
	{
		relPath:       filepath.Join("tests", "integration", "onboard_customize_test.go"),
		affectedTests: []string{"TestOnboardCustomize_*"},
	},
	{
		relPath:       filepath.Join("tests", "compliance", "inv_zen_081_test.go"),
		affectedTests: []string{"TestInvZen081_*"},
	},
	{
		relPath:       filepath.Join("tests", "compliance", "inv_zen_175_hermes_required_test.go"),
		affectedTests: []string{"TestInvZen175HermesRequired"},
	},
	{
		relPath:       filepath.Join("tests", "replay", "inbox_replay_test.go"),
		affectedTests: []string{"TestInboxReplay_*"},
	},
	{
		relPath:       filepath.Join("tests", "chaos", "inbox_aggregator_divergence_test.go"),
		affectedTests: []string{"TestInboxAggregatorDivergence_*"},
	},
}

var forbiddenTwoSecondPatterns = []string{
	"2*time.Second",
	"2 * time.Second",
}

var requiredThirtySecondPatterns = []string{
	"30*time.Second",
	"30 * time.Second",
}

func TestInvZen287SourceRegex_NoTwoSecondDeadlines(t *testing.T) {
	for _, tf := range subprocessTimeoutBumpFiles {
		tf := tf
		t.Run(tf.relPath, func(t *testing.T) {
			abs, err := filepath.Abs(filepath.Join("..", "..", tf.relPath))
			if err != nil {
				t.Fatalf("resolve %s: %v", tf.relPath, err)
			}
			data, err := os.ReadFile(abs)
			if err != nil {
				t.Fatalf("read %s: %v", abs, err)
			}
			src := string(data)
			for _, pat := range forbiddenTwoSecondPatterns {
				if strings.Contains(src, pat) {
					t.Errorf("inv-zen-287 violated: %s contains forbidden literal %q; under full-suite GOMAXPROCS contention the affected tests (%v) starve exec.CommandContext and produce false-negative failures. Bump to 30*time.Second or higher.", tf.relPath, pat, tf.affectedTests)
				}
			}
		})
	}
}

func TestInvZen287SourceRegex_HasThirtySecondDeadlines(t *testing.T) {
	for _, tf := range subprocessTimeoutBumpFiles {
		tf := tf
		t.Run(tf.relPath, func(t *testing.T) {
			abs, err := filepath.Abs(filepath.Join("..", "..", tf.relPath))
			if err != nil {
				t.Fatalf("resolve %s: %v", tf.relPath, err)
			}
			data, err := os.ReadFile(abs)
			if err != nil {
				t.Fatalf("read %s: %v", abs, err)
			}
			src := string(data)
			found := false
			for _, pat := range requiredThirtySecondPatterns {
				if strings.Contains(src, pat) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("inv-zen-287 violated: %s does not contain any of %v; the v0.20.4 30s timeout bump was not applied or has been reverted. Affected tests: %v", tf.relPath, requiredThirtySecondPatterns, tf.affectedTests)
			}
		})
	}
}
