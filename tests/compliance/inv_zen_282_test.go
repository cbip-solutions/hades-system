// tests/compliance/inv_zen_282_test.go
//
// Compliance gate for inv-zen-282 (v0.20.1 fix #2): the `zen doctor`
// aggregator distinguishes "daemon is reachable but probes failed"
// from "daemon transport failure" when wrapping the final non-zero
// exit. Before v0.20.1, BOTH paths returned the `daemon.unreachable`
// catalog code, whose recovery hint says "rm /tmp/zen-swarm.sock" —
// a destructive action that actively misleads operators when the
// daemon is up and only probe-level checks have failed.
//
// Why: the regression surfaced empirically against v0.20.0 — operators
// running `zen doctor caronte` against a healthy daemon (with per-probe
// failures from `project_id required` responses, see inv-zen-281) saw
// "HADES daemon unreachable. The socket may be stale: rm
// /tmp/zen-swarm.sock" as the suggested fix. Following the hint would
// crash the daemon. The error catalog already exposes the right code
// (`daemon.responded-with-error`, added in commit f09a3aa8) — the
// aggregator just needed to choose between them based on the boot-
// time `daemonReachable` signal.
//
// Three source-regex anchors:
//
//  1. Aggregator imports/references the `daemon.responded-with-error`
//     catalog code in `internal/cli/doctor.go`. Without this reference,
//     the conditional dispatch is impossible.
//  2. Conditional dispatch based on `daemonReachable`: when true, the
//     responded-with-error code wraps the error; when false, the
//     unreachable code is used (existing behavior preserved).
//  3. Anchor lives on BOTH return paths (json/yaml render + table
//     render); a fix that only touches one path silently regresses the
//     other format.
//
// Sister-test bite check: revert the conditional and force
// `daemon.unreachable` on both branches; this test MUST fail because
// the responded-with-error reference disappears.
//
// inv-zen-282 (v0.20.1 fix #2).
package compliance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInvZen282SourceRegex_RespondedWithErrorReference(t *testing.T) {
	src := readDoctorAggregatorSource(t)
	const needle = `daemon.responded-with-error`
	if !strings.Contains(src, needle) {
		t.Errorf("inv-zen-282 violated: %q catalog code missing from doctor.go aggregator; misleading 'daemon.unreachable' will still wrap probe-level failures", needle)
	}
}

func TestInvZen282SourceRegex_ConditionalDispatch(t *testing.T) {
	src := readDoctorAggregatorSource(t)

	if !strings.Contains(src, "if daemonReachable {") &&
		!strings.Contains(src, "daemonReachable {") {
		t.Errorf("inv-zen-282 violated: doctor.go aggregator lacks a daemonReachable-conditional dispatch; the responded-with-error code is not actually selected by transport state")
	}
}

func TestInvZen282SourceRegex_BothFormatsCovered(t *testing.T) {
	src := readDoctorAggregatorSource(t)
	const trigger = `if anyFail {`
	count := strings.Count(src, trigger)
	if count < 2 {
		t.Errorf("inv-zen-282 violated: expected `if anyFail {` to appear at least twice (json/yaml path + table path), found %d — one of the render paths no longer wraps on failure", count)
	}

	if !strings.Contains(src, "daemon.responded-with-error") {
		t.Errorf("inv-zen-282 violated: doctor.go aggregator does not reference daemon.responded-with-error — at least one render path still wraps as daemon.unreachable unconditionally")
	}
}

func readDoctorAggregatorSource(t *testing.T) string {
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
