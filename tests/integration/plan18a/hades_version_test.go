// go:build integration
package plan18a_integration_test

import (
	"os/exec"
	"strings"
	"testing"
)

func TestPlan18aFoundation_HadesVersionBranded(t *testing.T) {
	t.Parallel()
	bin := buildHadesBinary(t)

	out, err := exec.Command(bin, "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("hades --version exited non-zero: %v\noutput:\n%s", err, out)
	}
	stdout := string(out)

	// 1. HADES wordmark present (case-sensitive — spec §Q2 locks the
	// uppercase form). The wordmark MUST appear at least once.
	if !strings.Contains(stdout, "HADES") {
		t.Errorf("--version output missing wordmark 'HADES':\n%s", stdout)
	}

	if !strings.Contains(strings.ToLower(stdout), "system") {
		t.Errorf("--version output missing 'system' tagline:\n%s", stdout)
	}

	// 3. Forward-looking inv-zen-XXX-V1: no "zen-swarm" substring leaks
	// through the wrapper boundary. The wrapper is HADES-branded by
	// construction; references to the legacy zen-swarm name MUST NOT
	// appear in user-facing output.
	if strings.Contains(stdout, "zen-swarm") {
		t.Errorf("--version output contains forbidden 'zen-swarm' substring (forward-looking inv-zen-XXX-V1):\n%s", stdout)
	}

	// 4. First line MUST start with "HADES" — not "Hermes". The wrapper
	// is HADES-branded; it does NOT forward `hermes --version`. The
	// Hermes substrate reference is fine on subsequent lines (sister-
	// product per spec §Q1), but the leading wordmark is HADES.
	firstLine := strings.SplitN(stdout, "\n", 2)[0]
	if !strings.HasPrefix(strings.TrimSpace(firstLine), "HADES") {
		t.Errorf("first line of --version does NOT start with 'HADES'; got %q\nfull output:\n%s", firstLine, stdout)
	}

	if !strings.HasSuffix(stdout, "\n") {
		t.Errorf("--version output not newline-terminated:\n%s", stdout)
	}
}

func TestPlan18aFoundation_HadesVersionHermesReferencePresent(t *testing.T) {
	t.Parallel()
	bin := buildHadesBinary(t)

	out, err := exec.Command(bin, "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("hades --version: %v\n%s", err, out)
	}
	stdout := string(out)

	if !strings.Contains(stdout, "Hermes") {
		t.Errorf("--version output missing 'Hermes' substrate reference (spec §Q1 sister-product):\n%s", stdout)
	}

	if !strings.Contains(stdout, "v0.13") {
		t.Errorf("--version output missing 'v0.13' Hermes minimum-version anchor:\n%s", stdout)
	}
}
