package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/client"
)

type recordedAuditEvent struct {
	eventType string
	payload   map[string]any
}

type auditRecorder struct {
	mu     sync.Mutex
	events []recordedAuditEvent
}

func (r *auditRecorder) snapshot() []recordedAuditEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]recordedAuditEvent, len(r.events))
	copy(out, r.events)
	return out
}

func newAuditDaemonStub(t *testing.T) (*httptest.Server, *auditRecorder) {
	t.Helper()
	rec := &auditRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/audit/emit" {
			var req struct {
				Type    string         `json:"type"`
				Payload map[string]any `json:"payload"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
				rec.mu.Lock()
				rec.events = append(rec.events, recordedAuditEvent{eventType: req.Type, payload: req.Payload})
				rec.mu.Unlock()
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "evt-1", "accepted": true})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	return srv, rec
}

func withStubClientFactory(t *testing.T, srv *httptest.Server) {
	t.Helper()
	prev := TestOnlyClientFactory
	TestOnlyClientFactory = func(_ string) *client.Client {
		return client.NewWithBaseURL(srv.URL)
	}
	t.Cleanup(func() { TestOnlyClientFactory = prev })
}

func makeCCSource(t *testing.T, root string, withUnmappedPerm bool) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "skills", "alpha"), 0o755); err != nil {
		t.Fatalf("mkdir skills: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "skills", "alpha", "SKILL.md"), []byte("# alpha"), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	settings := map[string]any{
		"permissions": map[string]any{
			"allow": []string{"Read(*)", "Bash(go test)"},
			"deny":  []string{"Write(.env)"},
		},
		"model": "opus[1m]",
	}
	if withUnmappedPerm {

		settings["permissions"].(map[string]any)["allow"] = []string{
			"Read(*)",
			"Bash(go test)",
			"alien.opcode",
			"another.unknown.permission",
		}
	}
	body, _ := json.Marshal(settings)
	if err := os.WriteFile(filepath.Join(root, "settings.json"), body, 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}
}

func TestMigrateClaudeCode_AuditEmitOnApplySuccess(t *testing.T) {
	srv, rec := newAuditDaemonStub(t)
	defer srv.Close()
	withStubClientFactory(t, srv)

	src := t.TempDir()
	makeCCSource(t, src, false)

	target := t.TempDir()
	pluginRoot := filepath.Join(target, "plugin", "zen-swarm")
	hermesCfg := filepath.Join(target, "hermes", "config.yaml")
	zenCfg := filepath.Join(target, "zen-config")

	cmd := NewMigrateCmd()
	cmd.SetContext(context.Background())
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"claude-code",
		"--source", src,
		"--target-hermes", pluginRoot,
		"--target-config", hermesCfg,
		"--target-zen-config", zenCfg,
		"--force",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v\n%s", err, out.String())
	}

	events := rec.snapshot()
	var runEvents []recordedAuditEvent
	for _, e := range events {
		if e.eventType == "evt.migrate.claude_code.run" {
			runEvents = append(runEvents, e)
		}
	}
	if len(runEvents) != 1 {
		t.Fatalf("evt.migrate.claude_code.run count = %d; want 1\nall events: %+v", len(runEvents), events)
	}
	p := runEvents[0].payload
	if p["mode"] != "apply" {
		t.Errorf("payload.mode = %v; want apply", p["mode"])
	}
	if p["preset"] != "lenient" {
		t.Errorf("payload.preset = %v; want lenient", p["preset"])
	}
	if p["source"] != src {
		t.Errorf("payload.source = %v; want %s", p["source"], src)
	}
	if p["entry_count"] == nil {
		t.Errorf("payload.entry_count missing: %+v", p)
	}

	if mr, ok := p["merkle_root"].(string); !ok || mr == "" {
		t.Errorf("payload.merkle_root missing or empty: %+v", p)
	}
}

// TestMigrateClaudeCode_AuditEmitOnDryRunSkipped — dry-run / --plan-output
// paths do NOT emit the run event (per spec §3.7 + Phase E §5979 "emitted on
// Apply completion"). Plan.Warnings still surfaces in CLI output for operator
// inspection.
func TestMigrateClaudeCode_AuditEmitOnDryRunSkipped(t *testing.T) {
	srv, rec := newAuditDaemonStub(t)
	defer srv.Close()
	withStubClientFactory(t, srv)

	src := t.TempDir()
	makeCCSource(t, src, false)

	cmd := NewMigrateCmd()
	cmd.SetContext(context.Background())
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"claude-code", "--source", src, "--dry-run"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v\n%s", err, out.String())
	}

	for _, e := range rec.snapshot() {
		if e.eventType == "evt.migrate.claude_code.run" {
			t.Errorf("evt.migrate.claude_code.run fired on dry-run (spec §3.7: Apply only)")
		}
	}
}

// TestMigrateClaudeCode_PermissionUnmappedEmitsPerEntry asserts the
// canonical contract: under lenient preset (default), every CC permission
// that fails classifyTierStrict (no matching prefix) emits exactly one
// evt.migrate.claude_code.permission.unmapped event AND surfaces in
// Plan.Warnings — implementing spec §3.7 line 629 + Phase E plan §25:
// "Lenient-mode: missing source permission warns but does NOT silently drop
// (warning surface in `Plan.Warnings` + audit emit
// evt.migrate.claude_code.permission.unmapped)".
//
// Strict preset is NOT exercised here — strict halts at Apply-time via
// ImportDoctrineStrict before the run event would fire (tested via the
// existing inv-zen-183 compliance suite).
func TestMigrateClaudeCode_PermissionUnmappedEmitsPerEntry(t *testing.T) {
	srv, rec := newAuditDaemonStub(t)
	defer srv.Close()
	withStubClientFactory(t, srv)

	src := t.TempDir()
	makeCCSource(t, src, true)

	target := t.TempDir()
	cmd := NewMigrateCmd()
	cmd.SetContext(context.Background())
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"claude-code",
		"--source", src,
		"--target-hermes", filepath.Join(target, "plugin", "zen-swarm"),
		"--target-config", filepath.Join(target, "hermes", "config.yaml"),
		"--target-zen-config", filepath.Join(target, "zen-config"),
		"--force",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v\n%s", err, out.String())
	}

	events := rec.snapshot()
	var unmappedEvents []recordedAuditEvent
	for _, e := range events {
		if e.eventType == "evt.migrate.claude_code.permission.unmapped" {
			unmappedEvents = append(unmappedEvents, e)
		}
	}
	if len(unmappedEvents) != 2 {
		t.Fatalf("evt.migrate.claude_code.permission.unmapped count = %d; want 2 (alien.opcode + another.unknown.permission)\nall events: %+v",
			len(unmappedEvents), events)
	}
	gotPerms := map[string]bool{}
	for _, e := range unmappedEvents {
		perm, _ := e.payload["permission"].(string)
		gotPerms[perm] = true
		if e.payload["preset"] != "lenient" {
			t.Errorf("event for %s: preset = %v; want lenient", perm, e.payload["preset"])
		}
		if e.payload["reason"] != "no_tier_match" {
			t.Errorf("event for %s: reason = %v; want no_tier_match", perm, e.payload["reason"])
		}
	}
	for _, want := range []string{"alien.opcode", "another.unknown.permission"} {
		if !gotPerms[want] {
			t.Errorf("missing unmapped event for %q; got perms: %v", want, gotPerms)
		}
	}

	if !bytes.Contains(out.Bytes(), []byte("warning")) && !bytes.Contains(out.Bytes(), []byte("warnings")) {
		t.Errorf("stdout missing warning surface: %s", out.String())
	}
}

// TestMigrateClaudeCode_AuditEmitBestEffortOnDaemonDown — when daemon is
// unreachable the migrate Apply still succeeds; audit emit failure surfaces
// as a stderr warning but does NOT block. Matches doctor_restore graceful
// degradation pattern (doctor_restore_audit_test.go:104+).
func TestMigrateClaudeCode_AuditEmitBestEffortOnDaemonDown(t *testing.T) {

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("daemon down"))
	}))
	defer srv.Close()
	withStubClientFactory(t, srv)

	src := t.TempDir()
	makeCCSource(t, src, false)

	target := t.TempDir()
	cmd := NewMigrateCmd()
	cmd.SetContext(context.Background())
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"claude-code",
		"--source", src,
		"--target-hermes", filepath.Join(target, "plugin", "zen-swarm"),
		"--target-config", filepath.Join(target, "hermes", "config.yaml"),
		"--target-zen-config", filepath.Join(target, "zen-config"),
		"--force",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute should succeed despite daemon down: %v\n%s", err, out.String())
	}

	if !bytes.Contains(out.Bytes(), []byte("Migration applied")) {
		t.Errorf("stdout missing 'Migration applied' (migrate itself should have succeeded): %s", out.String())
	}

	if !bytes.Contains(out.Bytes(), []byte("audit emit")) {
		t.Errorf("stderr missing 'audit emit' warning: %s", out.String())
	}
}
