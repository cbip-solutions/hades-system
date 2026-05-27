// Package integration_test holds end-to-end / cross-package scenario
// tests that exercise the daemon HTTP surface and the OpenClaude plugin
// slash commands. Tests in this package use external-test packaging
// (`package integration_test`) so they cannot import internal packages
// directly — that constraint enforces invariant boundary discipline
// for the slash-command surface.
//
// - Task H-1: Structural assertion that the extended /handoff slash
// command markdown contains the required YAML frontmatter, every
// load-bearing payload field name, the canonical UDS path / env
// override, the daemon-bearer auth header, and the backward-compat
// warning marker.
// - Task H-4: End-to-end integration tests that exercise the canonical
// step-7.5 bash body against a mocked daemon UDS HTTP server. Five
// scenarios cover happy path + daemon-down + daemon-500 + daemon-401
// - alias-fallback per spec §1 Q15 + §4.4 row "HandoffPosted emit
// failure" + §7.2 invariant (canonical 8-field schema).
//
// Why a mocked daemon (not testhelpers.SpawnDaemon): must run
// without (daemon endpoint owner) shipping. tests stub
// the endpoint contract; when Phases F + I land these tests continue
// to pass — the contract is invariant.
//
// Anti-pattern guard: this file also enforces invariant (no Claude
// attribution in production artifacts) on the plugin slash command
// markdown — the test fails if any forbidden phrase leaks in.
package integration_test

import (
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/tests/testhelpers"
)

func TestHandoffSlashMarkdownStructure(t *testing.T) {
	const path = "../../plugin/hades/.claude/commands/handoff.md"
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %q: %v", path, err)
	}
	content := string(body)

	required := []string{
		"description:",
		"## 1. Verify",
		"HANDOFF.md",
		"HandoffPosted",
		"/v1/events/handoff_posted",
		"project_id",
		"project_alias",
		"summary",
		"recent_commits",
		"autonomous_state",
		"blockers",
		"next_session_action",
		"ZEN_SWARM_UDS",
		"/tmp/zen-swarm.sock",
		"daemon-bearer.txt",
		"Authorization: Bearer",
		"continue to write",
		"WARN:", // warning prefix
	}
	for _, s := range required {
		if !strings.Contains(content, s) {
			t.Errorf("handoff.md missing required section/keyword %q", s)
		}
	}

	forbidden := []string{
		"Co-Authored-By: prohibited assistant",
		"Generated with prohibited assistant",
	}
	for _, s := range forbidden {
		if strings.Contains(content, s) {
			t.Errorf("handoff.md MUST NOT contain forbidden phrase %q (inv-zen-004)", s)
		}
	}
}

func TestHandoffSlash_EmitsEventAndWritesFile(t *testing.T) {
	r := testhelpers.NewPluginSlashRunner(t)
	r.SeedProject("zen-swarm", "Phase H tests staged. Daemon emit verified.",
		[]string{"resolve daemon-bearer rotation", "review Plan 7 brainstorm"},
		[]string{"feat(x): foo", "fix(y): bar"},
		"active",
	)

	out, code := r.RunHandoffEmit()
	if code != 0 {
		t.Fatalf("RunHandoffEmit exit=%d, output:\n%s", code, out)
	}
	if !strings.Contains(out, "OK: HandoffPosted event emitted") {
		t.Errorf("expected OK log line; got:\n%s", out)
	}

	events := r.ReceivedEvents()
	if len(events) != 1 {
		t.Fatalf("expected 1 event received, got %d", len(events))
	}
	e := events[0]

	canonicalFields := []string{
		"project_id", "project_alias", "timestamp", "summary",
		"recent_commits", "autonomous_state", "blockers", "next_session_action",
	}
	for _, f := range canonicalFields {
		if _, ok := e.Body[f]; !ok {
			t.Errorf("payload missing field %q", f)
		}
	}

	if alias, ok := e.Body["project_alias"].(string); !ok || alias != "zen-swarm" {
		t.Errorf("project_alias = %v, want %q", e.Body["project_alias"], "zen-swarm")
	}
	if state, ok := e.Body["autonomous_state"].(string); !ok || state != "active" {
		t.Errorf("autonomous_state = %v, want %q", e.Body["autonomous_state"], "active")
	}
	if commits, ok := e.Body["recent_commits"].([]any); !ok || len(commits) == 0 {
		t.Errorf("recent_commits = %v, want non-empty array", e.Body["recent_commits"])
	}
	if blockers, ok := e.Body["blockers"].([]any); !ok || len(blockers) != 2 {
		t.Errorf("blockers = %v, want 2 items", e.Body["blockers"])
	}

	if auth := e.Headers.Get("Authorization"); !strings.HasPrefix(auth, "Bearer ") {
		t.Errorf("Authorization header = %q, want Bearer prefix", auth)
	}

	if ct := e.Headers.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json prefix", ct)
	}
}

func TestHandoffSlash_DaemonDown_StillCompletes(t *testing.T) {
	r := testhelpers.NewPluginSlashRunner(t)
	r.SeedProject("zen-swarm", "tldr", nil, nil, "idle")
	r.StopDaemon()

	out, code := r.RunHandoffEmit()
	if code != 0 {
		t.Fatalf("RunHandoffEmit exit=%d (expected 0; emit failure is non-fatal); output:\n%s", code, out)
	}
	if !strings.Contains(out, "WARN:") {
		t.Errorf("expected WARN log line; got:\n%s", out)
	}
	if !strings.Contains(out, "EOD digest will skip") {
		t.Errorf("expected operator-actionable hint about EOD digest skip; got:\n%s", out)
	}
}

// TestHandoffSlash_Daemon500_StillCompletes asserts: daemon returns 500
// → slash command exits 0, warning logged.
func TestHandoffSlash_Daemon500_StillCompletes(t *testing.T) {
	r := testhelpers.NewPluginSlashRunner(t)
	r.SeedProject("zen-swarm", "tldr", nil, nil, "idle")
	r.SetHandoffHandler(func(w http.ResponseWriter, req *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	})

	out, code := r.RunHandoffEmit()
	if code != 0 {
		t.Fatalf("RunHandoffEmit exit=%d (expected 0); output:\n%s", code, out)
	}
	if !strings.Contains(out, "WARN:") {
		t.Errorf("expected WARN log line on 500; got:\n%s", out)
	}
}

// TestHandoffSlash_Daemon401_StillCompletes asserts: bearer mismatch
// (401) → slash command exits 0, warning logged.
func TestHandoffSlash_Daemon401_StillCompletes(t *testing.T) {
	r := testhelpers.NewPluginSlashRunner(t)
	r.SeedProject("zen-swarm", "tldr", nil, nil, "idle")
	r.SetHandoffHandler(func(w http.ResponseWriter, req *http.Request) {
		http.Error(w, "bearer mismatch", http.StatusUnauthorized)
	})

	out, code := r.RunHandoffEmit()
	if code != 0 {
		t.Fatalf("RunHandoffEmit exit=%d (expected 0); output:\n%s", code, out)
	}
	if !strings.Contains(out, "WARN:") {
		t.Errorf("expected WARN log line on 401; got:\n%s", out)
	}
}

func TestHandoffSlash_PayloadConformsSchema(t *testing.T) {
	r := testhelpers.NewPluginSlashRunner(t)
	r.SeedProject("internal-platform-x", "Multi-line summary about Plan 7 brainstorm Q15 C completion.",
		[]string{"keychain rotation", "Plan 7 phase F dispatch"},
		[]string{"docs: master plan"},
		"paused",
	)

	if out, code := r.RunHandoffEmit(); code != 0 {
		t.Fatalf("emit failed: %s", out)
	}

	events := r.ReceivedEvents()
	if len(events) != 1 {
		t.Fatalf("want 1 event, got %d", len(events))
	}
	body := events[0].Body

	if len(body) != 8 {
		t.Errorf("payload has %d fields, want 8 (canonical schema); body=%v", len(body), body)
	}

	if body["project_alias"] != "internal-platform-x" {
		t.Errorf("project_alias = %v, want internal-platform-x", body["project_alias"])
	}
	if body["autonomous_state"] != "paused" {
		t.Errorf("autonomous_state = %v, want paused", body["autonomous_state"])
	}

	if pid, ok := body["project_id"].(string); !ok || len(pid) != 64 {
		t.Errorf("project_id = %v, want 64-char sha256 hex", body["project_id"])
	}
}

func TestHandoffSlash_ProjectAliasFallback(t *testing.T) {
	r := testhelpers.NewPluginSlashRunner(t)
	r.SeedProject("zen-swarm", "tldr", nil, nil, "idle")

	if err := os.Remove(r.ProjectDir + "/zenswarm.toml"); err != nil {
		t.Fatalf("remove toml: %v", err)
	}

	if out, code := r.RunHandoffEmit(); code != 0 {
		t.Fatalf("emit failed: %s", out)
	}

	events := r.ReceivedEvents()
	if len(events) != 1 {
		t.Fatalf("want 1 event, got %d", len(events))
	}

	if alias := events[0].Body["project_alias"]; alias != "project" {
		t.Errorf("project_alias fallback = %v, want %q (basename of project root)", alias, "project")
	}
}
