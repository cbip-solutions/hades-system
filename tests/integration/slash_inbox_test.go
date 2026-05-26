//   - Task H-3: Structural assertion that the /inbox slash command
//     markdown contains the YAML frontmatter, the canonical CLI invo-
//     cation (`bin/zen inbox`), every documented flag passthrough
//     (`--severity`, `--since`, `--project`), every value of the 4-tier
//     severity enum (`urgent`, `action-needed`, `info-immediate`,
//     `info-digest` — inv-zen-124 enforces enum exhaustiveness), and
//     the output-render instruction.
//   - Task H-6: End-to-end integration tests that exercise the slash
//     command bash body against a stubbed `bin/zen` binary (PATH stub
//     pattern shared with H-5 via the PluginSlashRunner testhelper).
//     Seven scenarios cover default / severity / since / project /
//     all-filters-combined / daemon-down abort / empty-exit pass-
//     through per spec §1 Q11 + §1 Q12 + §2.6 + §6.8 + §6.9 +
//     inv-zen-124.
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

func TestInboxSlashMarkdownStructure(t *testing.T) {
	const path = "../../plugin/hades/.claude/commands/inbox.md"
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %q: %v", path, err)
	}
	content := string(body)

	required := []string{
		"description:",
		"allowed-tools:",
		"bin/zen inbox",
		"--severity",
		"--since",
		"--project",
		"urgent",
		"action-needed",
		"info-digest",
		"info-immediate",
		"render",
	}
	for _, s := range required {
		if !strings.Contains(content, s) {
			t.Errorf("inbox.md missing required keyword %q", s)
		}
	}

	forbidden := []string{
		"api.anthropic.com",
		"https://api.openai.com",
		"Co-Authored-By: prohibited assistant",
	}
	for _, s := range forbidden {
		if strings.Contains(content, s) {
			t.Errorf("inbox.md MUST NOT contain forbidden phrase %q", s)
		}
	}
}

func TestInboxSlash_Default(t *testing.T) {
	r := testhelpers.NewPluginSlashRunner(t)
	r.SeedProject("zen-swarm", "tldr", nil, nil, "idle")
	r.WriteFakeZen("123 urgent zen-swarm 2026-05-01 daemon panic\n", "", 0)

	out, code := r.RunInboxSlash()
	if code != 0 {
		t.Fatalf("exit=%d, output:\n%s", code, out)
	}
	if got := r.LastZenArgv(); got != "inbox" {
		t.Errorf("zen argv = %q, want %q", got, "inbox")
	}
	if !strings.Contains(out, "daemon panic") {
		t.Errorf("expected listing in output; got:\n%s", out)
	}
}

func TestInboxSlash_SeverityFilter(t *testing.T) {
	r := testhelpers.NewPluginSlashRunner(t)
	r.SeedProject("zen-swarm", "tldr", nil, nil, "idle")
	r.WriteFakeZen("urgent items\n", "", 0)

	if _, code := r.RunInboxSlash("--severity", "urgent"); code != 0 {
		t.Fatalf("exit=%d", code)
	}
	if got := r.LastZenArgv(); got != "inbox --severity urgent" {
		t.Errorf("zen argv = %q, want preserved flag", got)
	}
}

func TestInboxSlash_SinceFilter(t *testing.T) {
	r := testhelpers.NewPluginSlashRunner(t)
	r.SeedProject("zen-swarm", "tldr", nil, nil, "idle")
	r.WriteFakeZen("recent items\n", "", 0)

	if _, code := r.RunInboxSlash("--since", "24h"); code != 0 {
		t.Fatalf("exit=%d", code)
	}
	if got := r.LastZenArgv(); got != "inbox --since 24h" {
		t.Errorf("zen argv = %q, want preserved flag", got)
	}
}

func TestInboxSlash_ProjectFilter(t *testing.T) {
	r := testhelpers.NewPluginSlashRunner(t)
	r.SeedProject("zen-swarm", "tldr", nil, nil, "idle")
	r.WriteFakeZen("project-scoped items\n", "", 0)

	if _, code := r.RunInboxSlash("--project", "internal-platform-x"); code != 0 {
		t.Fatalf("exit=%d", code)
	}
	if got := r.LastZenArgv(); got != "inbox --project internal-platform-x" {
		t.Errorf("zen argv = %q, want preserved flag", got)
	}
}

func TestInboxSlash_AllFiltersCombined(t *testing.T) {
	r := testhelpers.NewPluginSlashRunner(t)
	r.SeedProject("zen-swarm", "tldr", nil, nil, "idle")
	r.WriteFakeZen(`{"items":[]}`+"\n", "", 0)

	if _, code := r.RunInboxSlash(
		"--severity", "action-needed",
		"--since", "7d",
		"--project", "zen-swarm",
		"--limit", "20",
		"--format", "json",
	); code != 0 {
		t.Fatalf("exit=%d", code)
	}
	want := "inbox --severity action-needed --since 7d --project zen-swarm --limit 20 --format json"
	if got := r.LastZenArgv(); got != want {
		t.Errorf("zen argv = %q\nwant     %q", got, want)
	}
}

func TestInboxSlash_DaemonDownAborts(t *testing.T) {
	r := testhelpers.NewPluginSlashRunner(t)
	r.SeedProject("zen-swarm", "tldr", nil, nil, "idle")
	r.WriteFakeZen("(should not run)\n", "", 0)
	r.StopDaemon()

	out, code := r.RunInboxSlash()
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

func TestInboxSlash_EmptyExitSurfaced(t *testing.T) {
	r := testhelpers.NewPluginSlashRunner(t)
	r.SeedProject("zen-swarm", "tldr", nil, nil, "idle")
	r.WriteFakeZen("(empty)\n", "", 2)

	out, code := r.RunInboxSlash()
	if code != 2 {
		t.Errorf("exit=%d, want 2 (forwarded from bin/zen)", code)
	}
	if !strings.Contains(out, "(empty)") {
		t.Errorf("expected (empty) in output; got:\n%s", out)
	}
}
