// tests/compliance/inv_zen_291_test.go
//
// Compliance gate for invariant (v0.20.7 fix #2):
// internal/doctrine/reload/health.go::handleOverflow MUST signal the
// restartNeeded channel after running force-reload-all so the Start
// loop swaps the fsnotify watcher for a fresh one (closing the
// recurrence vector where the saturated kernel queue would immediately
// re-overflow on the next event burst).
//
// Why this gate exists: §4.1 F14 specified force-reload-all on
// fsnotify queue overflow but did NOT mandate a watcher restart.
// Without restart, the parent fsnotify watcher stays bound to the
// saturated kernel queue (Linux inotify) / kqueue (macOS), so a
// follow-on event burst immediately re-overflows in a tight loop. The
// stall-detector at health.go:62-65 already uses the canonical
// signal-restart pattern (non-blocking send to restartNeeded with a
// `default:` arm to coalesce concurrent stall + overflow signals);
// handleOverflow now mirrors that pattern. The Start loop's
// `case <-w.restartNeeded:` branch (reload.go:317-318) drains the
// signal + calls performRestart, which closes the existing watcher
// (releasing the saturated queue) and constructs a fresh one.
//
// Anchor 1 (positive): handleOverflow MUST contain the canonical
// non-blocking restartNeeded signal expression `case w.restartNeeded
// <- struct{}{}:`. The same pattern as the stall-detector's signal
// at health.go:62; verifying the literal pins the canonical shape.
//
// Anchor 2 (positive): the RestartNeededSignalForTest accessor MUST
// exist in health.go. This accessor is what enables the behavioural
// sister-test TestHealth_OverflowSignalsRestartNeeded to verify the
// runtime contract — its presence is structurally load-bearing for
// the regression safety net.
//
// Sister-test bite check: remove the restartNeeded signal block from
// handleOverflow — Anchor 1 trips AND the behavioural sister-test
// `TestHealth_OverflowSignalsRestartNeeded` fails. Remove the
// RestartNeededSignalForTest accessor — Anchor 2 trips AND the
// behavioural sister-test fails to compile.
//
// invariant (v0.20.7 fix #2).
package compliance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const inv291HealthPath = "internal/doctrine/reload/health.go"

func TestInvZen291_HandleOverflowSignalsRestart(t *testing.T) {
	abs, err := filepath.Abs(filepath.Join("..", "..", inv291HealthPath))
	if err != nil {
		t.Fatalf("resolve %s: %v", inv291HealthPath, err)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("read %s: %v", abs, err)
	}
	src := string(data)
	required := "case w.restartNeeded <- struct{}{}:"

	count := strings.Count(src, required)
	if count < 2 {
		t.Errorf("inv-zen-291 violated: %s contains %d occurrence(s) of %q; want >=2 (one in stall-detector, one in handleOverflow added in v0.20.7). If handleOverflow no longer signals restartNeeded, the fsnotify watcher stays bound to the saturated kernel queue and a follow-on event burst will immediately re-overflow in a tight loop.", inv291HealthPath, count, required)
	}
}

func TestInvZen291_RestartNeededSignalForTestExists(t *testing.T) {
	abs, err := filepath.Abs(filepath.Join("..", "..", inv291HealthPath))
	if err != nil {
		t.Fatalf("resolve %s: %v", inv291HealthPath, err)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("read %s: %v", abs, err)
	}
	src := string(data)
	required := "func (w *Watcher) RestartNeededSignalForTest()"
	if !strings.Contains(src, required) {
		t.Errorf("inv-zen-291 violated: %s does not contain the test accessor %q. The behavioural sister-test TestHealth_OverflowSignalsRestartNeeded depends on this accessor to assert handleOverflow's restart-signal contract; without it the regression safety net is gone.", inv291HealthPath, required)
	}
}
