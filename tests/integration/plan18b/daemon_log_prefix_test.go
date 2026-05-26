//go:build integration

package plan18b_integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPlan18bJInt5_DaemonStderrShowsHADESPrefix asserts J-int-5: the
// zen-swarm-ctld daemon source contains HADES-branded startup + shutdown
// log strings (per master §Phase F daemon log rebrand) + preserves the
// binary name in parens for journalctl/grep-ability (per spec §Q3 BORDERLINE
// daemon-binary-name carve-out).
//
// Strategy: source-text scanning over cmd/zen-swarm-ctld/main.go and
// cmd/zen-swarm-ctld/log_brand_test.go (the Phase F unit test) to assert:
//
//	(a) main.go contains the HADES-prefixed startup banner string.
//	(b) main.go contains the HADES-prefixed shutdown string.
//	(c) main.go retains "(zen-swarm-ctld)" in the banner (grep-ability
//	    carve-out per spec §Q3 BORDERLINE — binary name STAYS).
//	(d) log_brand_test.go exists (Phase F unit test gate — confirms the
//	    subprocess version of this test is in-place at the unit tier).
//	(e) Defense-in-depth: main.go's logger.Info calls outside the binary-name
//	    carve-out do NOT contain bare "zen-swarm" brand strings (Phase F miss).
//
// NOTE(plan-15): Full subprocess daemon launch (go run + UDS bind + SIGTERM flow) is
// handled by the existing Phase F unit test cmd/zen-swarm-ctld/log_brand_test.go
// which runs as part of `make test`. This integration test asserts the
// COMPOSITION property — that the Phase F source changes are present and the
// unit test gate exists — without duplicating the expensive go-run-based
// subprocess integration that the unit test already provides.
//
// Per spec §Q3 BORDERLINE: binary name "zen-swarm-ctld" STAYS in parens for
// process supervision configs (launchd, systemd). Plan 18b does NOT rename
// the binary. The HADES prefix is in the log MESSAGE body only.
func TestPlan18bJInt5_DaemonStderrShowsHADESPrefix(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	ctldDir := filepath.Join(root, "cmd", "zen-swarm-ctld")

	mainBody, err := os.ReadFile(filepath.Join(ctldDir, "main.go"))
	if err != nil {
		t.Fatalf("read cmd/zen-swarm-ctld/main.go: %v", err)
	}
	mainStr := string(mainBody)

	if !strings.Contains(mainStr, `"HADES system daemon (zen-swarm-ctld) starting"`) {
		t.Errorf("J-int-5 cmd/zen-swarm-ctld/main.go missing HADES startup banner string " +
			`(expected literal: "HADES system daemon (zen-swarm-ctld) starting")`)
	}

	if !strings.Contains(mainStr, `"HADES system daemon (zen-swarm-ctld) stopped"`) {
		t.Errorf("J-int-5 cmd/zen-swarm-ctld/main.go missing HADES shutdown banner string " +
			`(expected literal: "HADES system daemon (zen-swarm-ctld) stopped")`)
	}

	if !strings.Contains(mainStr, "(zen-swarm-ctld)") {
		t.Errorf("J-int-5 cmd/zen-swarm-ctld/main.go missing '(zen-swarm-ctld)' binary-name " +
			"grep-ability carve-out per spec §Q3 BORDERLINE")
	}

	if _, statErr := os.Stat(filepath.Join(ctldDir, "log_brand_test.go")); statErr != nil {
		t.Errorf("J-int-5 cmd/zen-swarm-ctld/log_brand_test.go missing — Phase F unit " +
			"test gate not in place (subprocess HADES banner assertion)")
	}

	violations := []string{}
	for lineno, line := range strings.Split(mainStr, "\n") {

		if !strings.Contains(line, "logger.Info") &&
			!strings.Contains(line, "logger.Warn") &&
			!strings.Contains(line, "logger.Error") {
			continue
		}

		if strings.HasPrefix(strings.TrimSpace(line), "//") {
			continue
		}

		stripped := strings.ReplaceAll(line, "zen-swarm-ctld", "<BINARY>")
		stripped = strings.ReplaceAll(stripped, "zen-swarm.sock", "<SOCKET>")
		stripped = strings.ReplaceAll(stripped, ".config/zen-swarm", "<CONFIG>")
		stripped = strings.ReplaceAll(stripped, ".local/share/zen-swarm", "<DATA>")
		stripped = strings.ReplaceAll(stripped, "zen-swarm/zen-swarm", "<MODULE>")
		if strings.Contains(stripped, "zen-swarm") {
			violations = append(violations, "  - line "+itoa_legacy(lineno+1)+": "+strings.TrimSpace(line))
		}
	}
	if len(violations) > 0 {
		t.Errorf("J-int-5 cmd/zen-swarm-ctld/main.go has %d logger call(s) with bare 'zen-swarm' brand "+
			"outside allowlisted carve-outs (Phase F miss):\n%s",
			len(violations), strings.Join(violations, "\n"))
	}
}
