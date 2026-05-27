// tests/compliance/inv_zen_283_test.go
//
// Compliance gate for invariant (v0.20.1): the doctor "Implementation
// status" footer MUST NOT contain a hardcoded numeric plan-count
// percentage expression of the form `(~NN%)` or `(~NNN%)` in
// `internal/cli/doctor.go`.
//
// Why: the prior v0.20.0 form `"Implementation status: Plans 1-11 of
// 17+ (~65%)"` decayed (true at write-time, false within 4 releases:
// Plans 16/19/20/v0.20.0 had all shipped by 2026-05-25 making the
// number stale). Hardcoded percentages anchored to a plan count are
// stale-prone — any new ship invalidates the literal without a build
// failure. The fix is to remove the percentage form entirely from the
// footer; a textual descriptive state ("post v0.20.0; v1.0
// release in flight") is acceptable because it survives the natural
// release cadence + carries no decayable arithmetic.
//
// Sister-test bite check: re-introduce `(~70%)` (or any `(~N%)` /
// `(~NN%)` / `(~NNN%)`) to the doctor footer literal; this test MUST
// fail.
//
// invariant (v0.20.1 fix #5).
package compliance

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestInvZen283_DoctorFooterNoPercentageDecay(t *testing.T) {
	src := readDoctorSource(t)
	// Anchor: the docs-status footer line MUST live within the
	// non-Quiet render path (lines after `if !opts.Quiet {`); we scan
	// the whole file because string-literal placement may move.
	if matched, where := findPercentageDecay(src); matched {
		t.Fatalf("inv-zen-283: internal/cli/doctor.go contains a hardcoded plan-count percentage %q which decays with each release; replace with a percentage-free descriptive string", where)
	}
}

func TestInvZen283_DoctorFooterContainsCurrentStatus(t *testing.T) {
	src := readDoctorSource(t)
	if !strings.Contains(src, "Implementation status:") {
		t.Fatalf("inv-zen-283: doctor footer line `Implementation status:` is absent; the descriptive footer must remain (only the percentage is forbidden)")
	}
}

func readDoctorSource(t *testing.T) string {
	t.Helper()
	rel := filepath.Join("..", "..", "internal", "cli", "doctor.go")
	abs, err := filepath.Abs(rel)
	if err != nil {
		t.Fatalf("resolve doctor.go: %v", err)
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("read %s: %v", abs, err)
	}
	return string(b)
}

func findPercentageDecay(src string) (bool, string) {

	re := regexp.MustCompile(`"[^"]*\(~\d{1,3}%\)[^"]*"`)
	if m := re.FindString(src); m != "" {
		return true, m
	}
	return false, ""
}
