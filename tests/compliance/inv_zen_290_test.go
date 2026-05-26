// tests/compliance/inv_zen_290_test.go
//
// Compliance gate for inv-zen-290 (v0.20.7 fix #1): the
// "plan-1-h-prime-executed" probe in the daemon coordination handler
// and the matching "plan-1-h-prime.executed" check in the CLI doctor
// coordination section are RETIRED because the underlying landing test
// (presence of plugin/zen-swarm/plugin.yaml + Hermes-format markers)
// is obsolete per Plan 18b Hermes pivot (ADR-0080).
//
// Why this gate exists: Plan 1 H' was the deferred Claude-Code-plugin
// conversion path; Plan 18b replaced that path with the Hermes plugin
// at plugin/hades/ (different canonical location + format). The
// probe-target plugin/zen-swarm/plugin.yaml never existed at HEAD and
// the probe always reported "fail" — a misleading active signal in
// `zen doctor coordination` output. The Q1 substrate decision +
// ADR-0080 supersede Plan 1 H', so the probe has no underlying
// behaviour to assert.
//
// Anchor 1 (negative): internal/daemon/handlers/coordination_probe.go
// MUST NOT contain the literal `case "plan-1-h-prime-executed":` (the
// switch-case signature). The retirement note still references the
// probe name in prose; this anchor distinguishes the active case block
// from the deprecation comment.
//
// Anchor 2 (negative): internal/cli/doctor_coordination.go MUST NOT
// contain the literal `probe: "plan-1-h-prime-executed"` (the struct
// field initialiser shape used by the `checks` slice). Same
// distinguishing strategy as Anchor 1 — the retirement note still
// references the check name in prose.
//
// Sister-test bite check: re-add the `case "plan-1-h-prime-executed":`
// block to coordination_probe.go — Anchor 1 fails. Re-add the
// `probe: "plan-1-h-prime-executed"` entry to doctor_coordination.go's
// `checks` slice — Anchor 2 fails. The behavioural sister-test
// `TestCoordinationProbe_Plan1HPrimeExecuted_Retired` in
// internal/daemon/handlers/coordination_probe_test.go independently
// asserts the runtime behaviour (the retired name falls into the
// default branch with the unknown-check hint).
//
// inv-zen-290 (v0.20.7 fix #1).
package compliance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	inv290HandlerPath = "internal/daemon/handlers/coordination_probe.go"
	inv290CLIPath     = "internal/cli/doctor_coordination.go"
)

func TestInvZen290_HandlerHasNoRetiredCase(t *testing.T) {
	abs, err := filepath.Abs(filepath.Join("..", "..", inv290HandlerPath))
	if err != nil {
		t.Fatalf("resolve %s: %v", inv290HandlerPath, err)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("read %s: %v", abs, err)
	}
	src := string(data)
	forbidden := `case "plan-1-h-prime-executed":`
	if strings.Contains(src, forbidden) {
		t.Errorf("inv-zen-290 violated: %s contains the forbidden retired case-statement signature %q. The plan-1-h-prime-executed probe was retired in v0.20.7 because the underlying landing test (plugin/zen-swarm/plugin.yaml) is obsolete per Plan 18b Hermes pivot (ADR-0080); re-adding the case block reintroduces a misleading active fail signal in `zen doctor coordination` output.", inv290HandlerPath, forbidden)
	}
}

func TestInvZen290_CLIHasNoRetiredEntry(t *testing.T) {
	abs, err := filepath.Abs(filepath.Join("..", "..", inv290CLIPath))
	if err != nil {
		t.Fatalf("resolve %s: %v", inv290CLIPath, err)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("read %s: %v", abs, err)
	}
	src := string(data)
	forbidden := `probe: "plan-1-h-prime-executed"`
	if strings.Contains(src, forbidden) {
		t.Errorf("inv-zen-290 violated: %s contains the forbidden retired probe-field initialiser %q. The plan-1-h-prime.executed check was retired in v0.20.7 because the underlying probe is obsolete per Plan 18b Hermes pivot (ADR-0080); re-adding the entry to the `checks` slice reintroduces a misleading active fail check.", inv290CLIPath, forbidden)
	}
}
