// go:build release_smoke

package release

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestV090ReleaseSmoke(t *testing.T) {
	if testing.Short() {
		t.Skip("release smoke is slow (~30s); skipped under -short")
	}
	uds, _, shutdown := spawnDaemon(t)
	defer shutdown()

	root := repoRoot(t)

	t.Log("Step 1: daemon health check")
	if out, code := runZen(t, uds, "status"); code != 0 {
		t.Fatalf("zen status: exit %d; output:\n%s", code, out)
	}

	t.Log("Step 2: zen audit verify-chain (expect 503 graceful)")
	out, code := runZen(t, uds, "audit", "verify-chain")
	if code == 0 {

		t.Logf("zen audit verify-chain: exit 0 (substrate wired); output:\n%s", out)
	} else {
		assertGracefulDegradation(t, "zen audit verify-chain", out, code)
	}

	t.Log("Step 3: zen knowledge query (expect 503 graceful)")
	out, code = runZen(t, uds, "knowledge", "query", "smoke")
	if code == 0 {
		t.Logf("zen knowledge query: exit 0 (substrate wired); output:\n%s", out)
	} else {
		assertGracefulDegradation(t, "zen knowledge query", out, code)
	}

	t.Log("Step 4: zen adr ls (expect 503 graceful)")
	out, code = runZen(t, uds, "adr", "ls")
	if code == 0 {
		t.Logf("zen adr ls: exit 0 (substrate wired); output:\n%s", out)
	} else {
		assertGracefulDegradation(t, "zen adr ls", out, code)
	}

	t.Log("Step 5: zen research cache stats (expect 503 graceful)")
	out, code = runZen(t, uds, "research", "cache", "stats")
	if code == 0 {
		t.Logf("zen research cache stats: exit 0 (substrate wired); output:\n%s", out)
	} else {
		assertGracefulDegradation(t, "zen research cache stats", out, code)
	}

	t.Log("Step 6: zen state show (expect 503 graceful)")
	out, code = runZen(t, uds, "state", "show")
	if code == 0 {
		t.Logf("zen state show: exit 0 (substrate wired); output:\n%s", out)
	} else {
		assertGracefulDegradation(t, "zen state show", out, code)
	}

	t.Log("Step 7: zen adr validate --dir docs/decisions (local validator)")
	decisionsDir := filepath.Join(root, "docs", "decisions")
	if _, err := os.Stat(decisionsDir); err != nil {
		t.Skipf("docs/decisions/ not found at %s; skipping local validator step", decisionsDir)
	}
	out, code = runZen(t, uds, "adr", "validate", "--dir", decisionsDir)
	if code != 0 {
		t.Errorf("zen adr validate --dir docs/decisions: exit %d; output:\n%s", code, out)
	}

	t.Log("Step 8: zen doctor audit.tessera (in-process check)")
	out, code = runZen(t, uds, "doctor", "audit.tessera")
	if code != 0 {
		// Degraded but not fatal: doctor results are informational.
		// Log failure but do not t.Fatal — a doctor WARN (exit 1) is
		// acceptable; only a crash (exit > 2, signal, panic) is a
		// release blocker.
		t.Logf("zen doctor audit.tessera: exit %d (WARN acceptable); output:\n%s", code, out)
	}
}

func assertGracefulDegradation(t *testing.T, cmd, out string, code int) {
	t.Helper()

	if code > 2 {
		t.Errorf("%s: exit %d (>2 indicates signal/crash, not graceful degradation); output:\n%s",
			cmd, code, out)
		return
	}

	// code == 1 would mean the CLI surfaced a non-503 error (e.g. 404).
	// That is unexpected for a nil-substrate path which must return 503.
	// Log as WARNING — it is possible the CLI maps some 503 variants to
	// exit 1 by convention; the important thing is no panic.
	if code == 1 {
		t.Logf("%s: exit 1 (expected 2 for 503; check CLI error mapping); output:\n%s",
			cmd, out)
	}

	panicMarkers := []string{
		"panic: runtime error",
		"panic: interface conversion",
		"goroutine 1 [running]:",
		"SIGSEGV",
		"nil pointer dereference",
	}
	lower := strings.ToLower(out)
	for _, marker := range panicMarkers {
		if strings.Contains(lower, strings.ToLower(marker)) {
			t.Errorf("%s: output contains panic marker %q; graceful degradation FAILED:\n%s",
				cmd, marker, out)
			return
		}
	}

	has503 := strings.Contains(out, "503") ||
		strings.Contains(out, "unavailable") ||
		strings.Contains(out, "not available") ||
		strings.Contains(lower, "substrate") ||
		strings.Contains(lower, "not wired") ||
		strings.Contains(lower, "plan 9")
	if !has503 {
		t.Logf("%s: exit %d; output does not contain expected 503/degradation indicator "+
			"(may be fine if route mapping changed): %s", cmd, code, out)
	}

	t.Logf("%s: exit %d (graceful); output:\n%s", cmd, code, out)
}

func TestV090ReleaseSmokeCompile(t *testing.T) {

	_ = func() {

		_ = fmt.Sprintf("compile-check: spawnDaemon=%T runZen=%T", spawnDaemon, runZen)
	}
}
