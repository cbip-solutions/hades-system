//   - Task H-2: Structural assertion that the /zen-day slash command
//     markdown contains the YAML frontmatter, the canonical CLI invo-
//     cation (`bin/zen day`), every documented flag passthrough
//     (`--eod`, `--force`, `--check-pending`), the documented behaviour
//     marker ("morning brief"), and the output-render instruction.
//   - Task H-5: End-to-end integration tests that exercise the slash
//     command bash body against a stubbed `bin/zen` binary (PATH stub
//     at <ProjectDir>/bin/zen; argv recorded to a log file the test
//     reads). Seven scenarios cover default / --eod / --force /
//     --check-pending / multi-flag passthrough / daemon-down abort /
//     non-zero exit propagation per spec §2.6 + §6.8 + §6.9 + §3.4
//     step 6.
//
// The PATH-stub pattern lets Phase H exercise its slash command body
// without depending on Phase F (zen day implementation) shipping. When
// Phase F + Phase L land and the real bin/zen exists, these tests
// continue to pass because the fake binary's contract (record argv,
// exit with given code, print stdout / stderr) matches the real
// binary's documented behaviour.
//
// Anti-pattern guard: this file also enforces inv-zen-004 (no Claude
// attribution in production artifacts) and inv-zen-080 (no bare
// provider-API URLs in slash command bodies — slash commands MUST
// route through `bin/zen` so the daemon dispatcher is the single
// egress point for LLM traffic) on the plugin slash command markdown.
package integration_test

import (
	"os"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/tests/testhelpers"
)

func TestZenDaySlashMarkdownStructure(t *testing.T) {
	const path = "../../plugin/hades/.claude/commands/zen-day.md"
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %q: %v", path, err)
	}
	content := string(body)

	required := []string{
		"description:",
		"allowed-tools:",
		"bin/zen day",
		"--eod",
		"--force",
		"--check-pending",
		"morning brief",
		"render",
	}
	for _, s := range required {
		if !strings.Contains(content, s) {
			t.Errorf("zen-day.md missing required keyword %q", s)
		}
	}

	forbidden := []string{
		"api.anthropic.com",
		"https://api.openai.com",
		"Co-Authored-By: prohibited assistant",
	}
	for _, s := range forbidden {
		if strings.Contains(content, s) {
			t.Errorf("zen-day.md MUST NOT contain forbidden phrase %q", s)
		}
	}
}

func TestZenDaySlash_DefaultMorningBrief(t *testing.T) {
	r := testhelpers.NewPluginSlashRunner(t)
	r.SeedProject("zen-swarm", "tldr", nil, nil, "idle")
	r.WriteFakeZen("# Morning brief\n- 1. urgent: x\n- 2. action-needed: y\n", "", 0)

	out, code := r.RunZenDaySlash()
	if code != 0 {
		t.Fatalf("exit=%d, output:\n%s", code, out)
	}
	if got := r.LastZenArgv(); got != "day" {
		t.Errorf("zen argv = %q, want %q", got, "day")
	}
	if !strings.Contains(out, "Morning brief") {
		t.Errorf("expected brief markdown in output; got:\n%s", out)
	}
}

func TestZenDaySlash_EodFlag(t *testing.T) {
	r := testhelpers.NewPluginSlashRunner(t)
	r.SeedProject("zen-swarm", "tldr", nil, nil, "idle")
	r.WriteFakeZen("# EOD digest\n", "", 0)

	out, code := r.RunZenDaySlash("--eod")
	if code != 0 {
		t.Fatalf("exit=%d, output:\n%s", code, out)
	}
	if got := r.LastZenArgv(); got != "day --eod" {
		t.Errorf("zen argv = %q, want %q", got, "day --eod")
	}
}

func TestZenDaySlash_ForceFlag(t *testing.T) {
	r := testhelpers.NewPluginSlashRunner(t)
	r.SeedProject("zen-swarm", "tldr", nil, nil, "idle")
	r.WriteFakeZen("# Brief (regenerated)\n", "", 0)

	if _, code := r.RunZenDaySlash("--force"); code != 0 {
		t.Fatalf("exit=%d", code)
	}
	if got := r.LastZenArgv(); got != "day --force" {
		t.Errorf("zen argv = %q, want %q", got, "day --force")
	}
}

func TestZenDaySlash_CheckPendingFlag(t *testing.T) {
	r := testhelpers.NewPluginSlashRunner(t)
	r.SeedProject("zen-swarm", "tldr", nil, nil, "idle")
	r.WriteFakeZen("0 candidates pending.\n", "", 0)

	if _, code := r.RunZenDaySlash("--check-pending"); code != 0 {
		t.Fatalf("exit=%d", code)
	}
	if got := r.LastZenArgv(); got != "day --check-pending" {
		t.Errorf("zen argv = %q, want %q", got, "day --check-pending")
	}
}

func TestZenDaySlash_SinceAndProjectCombined(t *testing.T) {
	r := testhelpers.NewPluginSlashRunner(t)
	r.SeedProject("zen-swarm", "tldr", nil, nil, "idle")
	r.WriteFakeZen("filtered\n", "", 0)

	if _, code := r.RunZenDaySlash("--since", "24h", "--project", "internal-platform-x"); code != 0 {
		t.Fatalf("exit=%d", code)
	}
	if got := r.LastZenArgv(); got != "day --since 24h --project internal-platform-x" {
		t.Errorf("zen argv = %q, want preserved flag order", got)
	}
}

func TestZenDaySlash_DaemonDownAborts(t *testing.T) {
	r := testhelpers.NewPluginSlashRunner(t)
	r.SeedProject("zen-swarm", "tldr", nil, nil, "idle")
	r.WriteFakeZen("(should not run)\n", "", 0)
	r.StopDaemon()

	out, code := r.RunZenDaySlash()
	if code == 0 {
		t.Errorf("exit=0, want non-zero (daemon down)")
	}
	if !strings.Contains(out, "zen-swarm-ctld not running") {
		t.Errorf("expected operator-actionable error; got:\n%s", out)
	}
	if argv := r.LastZenArgv(); argv != "" {
		t.Errorf("zen should NOT have been invoked when daemon down; got argv=%q", argv)
	}
}

func TestZenDaySlash_NonZeroExitSurfaced(t *testing.T) {
	r := testhelpers.NewPluginSlashRunner(t)
	r.SeedProject("zen-swarm", "tldr", nil, nil, "idle")
	r.WriteFakeZen("", "today's brief already exists; pass --force\n", 2)

	out, code := r.RunZenDaySlash()
	if code != 2 {
		t.Errorf("exit=%d, want 2 (forwarded from bin/zen)", code)
	}
	if !strings.Contains(out, "already exists") {
		t.Errorf("expected stderr surfaced; got:\n%s", out)
	}
}
