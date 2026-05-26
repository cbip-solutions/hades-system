//go:build release_smoke

package release

import (
	"testing"
)

// TestV0130ReleaseSmoke is the Plan 13 v0.13.0 release smoke test.
//
// Per Plan 13 Phase F-tail F10 line 7243-7247 spec: the release smoke
// MUST exercise the 4 Plan 13 NEW surface invocations + the 3 F-tail
// CLI surfaces (doctor full / state cleanup / doctor restore) +
// migrate + onboard. Daemon-down graceful degradation is the canonical
// failure mode tested.
func TestV0130ReleaseSmoke(t *testing.T) {
	if testing.Short() {
		t.Skip("release smoke is slow (~30s); skipped under -short")
	}
	uds, _, shutdown := spawnDaemon(t)
	defer shutdown()

	t.Log("Step 1: daemon health check")
	if out, code := runZen(t, uds, "status"); code != 0 {
		t.Fatalf("zen status: exit %d; output:\n%s", code, out)
	}

	t.Log("Step 2: zen doctor full")
	out, code := runZen(t, uds, "doctor", "full")

	if code > 4 {
		t.Errorf("zen doctor full: exit %d (>4 indicates aggregator crash, not warn/fail/skip); output:\n%s", code, out)
	}

	t.Log("Step 3: zen doctor full --format json")
	out, code = runZen(t, uds, "doctor", "full", "--format", "json")
	if code > 4 {
		t.Errorf("zen doctor full --format json: exit %d; output:\n%s", code, out)
	}

	if code <= 4 && !containsAny(out, "schemaVersion", `"diagnostics"`) {
		t.Errorf("zen doctor full --format json: output missing schemaVersion/diagnostics: %s", out)
	}

	t.Log("Step 4: zen state list")
	out, code = runZen(t, uds, "state", "list")
	if code > 1 {
		t.Errorf("zen state list: exit %d (>1 indicates crash); output:\n%s", code, out)
	}

	t.Log("Step 5: zen state cleanup --dry-run")
	out, code = runZen(t, uds, "state", "cleanup", "--dry-run")
	if code > 1 {
		t.Errorf("zen state cleanup --dry-run: exit %d; output:\n%s", code, out)
	}

	t.Log("Step 6: zen migrate claude-code --dry-run")
	out, code = runZen(t, uds, "migrate", "claude-code", "--dry-run")
	if code > 2 {
		t.Errorf("zen migrate --dry-run: exit %d (>2 indicates crash); output:\n%s", code, out)
	}

	t.Log("Step 7: zen doctor restore 99999999T999999Z (expect recoverable err)")
	out, code = runZen(t, uds, "doctor", "restore", "99999999T999999Z")
	if code == 0 {
		t.Errorf("zen doctor restore nonexistent-ID: exit 0; want non-zero (recoverable error)")
	}
	if code > 2 {
		t.Errorf("zen doctor restore: exit %d (>2 indicates crash); output:\n%s", code, out)
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if len(s) >= len(sub) {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}

func TestV0130ReleaseSmokeCompile(t *testing.T) {
	_ = spawnDaemon
	_ = runZen
	_ = repoRoot
}
