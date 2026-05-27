package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/cbip-solutions/hades-system/internal/client"
	"github.com/cbip-solutions/hades-system/internal/mcp/sshexec"
)

func invokeSSHExecCmd(t *testing.T, args []string, baseURL string) (string, string, error) {
	t.Helper()
	prev := TestOnlyClientFactory
	TestOnlyClientFactory = func(uds string) *client.Client {
		return client.NewWithBaseURL(baseURL)
	}
	t.Cleanup(func() { TestOnlyClientFactory = prev })

	cmd := NewSSHExecCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

func writeTOMLFile(t *testing.T, dir, project string, patterns, hosts []string) string {
	t.Helper()
	p := filepath.Join(dir, "zenswarm.toml")
	type tomlShape struct {
		Project struct {
			ID string `toml:"id"`
		} `toml:"project"`
		SSHExec struct {
			Allowlist struct {
				Patterns []string `toml:"patterns"`
				Hosts    []string `toml:"hosts"`
			} `toml:"allowlist"`
		} `toml:"ssh_exec"`
	}
	var doc tomlShape
	doc.Project.ID = project
	doc.SSHExec.Allowlist.Patterns = patterns
	doc.SSHExec.Allowlist.Hosts = hosts
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := toml.NewEncoder(f).Encode(doc); err != nil {
		t.Fatal(err)
	}
	return p
}

func mockSSHExecAuditServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/audit/events", func(w http.ResponseWriter, r *http.Request) {

		typeFilter := r.URL.Query().Get("type")
		w.Header().Set("X-Type-Filter", typeFilter)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []client.AuditEvent{
				{ID: "ev_1", ProjectID: "test-proj", Type: "ssh_exec.started",
					PayloadRaw: `{"host":"vps","cmd_preview":"git status","project":"test-proj","timeout_ms":60000}`,
					EmittedAt:  1759320000},
				{ID: "ev_2", ProjectID: "test-proj", Type: "ssh_exec.completed",
					PayloadRaw: `{"host":"vps","cmd_preview":"git status","exit_code":0,"exit_reason":"normal"}`,
					EmittedAt:  1759320001},
				{ID: "ev_3", ProjectID: "test-proj", Type: "ssh_exec.denied",
					PayloadRaw: `{"host":"vps","cmd_preview":"rm -rf /","reason":"validation rejected"}`,
					EmittedAt:  1759320002},
				{ID: "ev_4", ProjectID: "test-proj", Type: "ssh_exec.interactive_blocked",
					PayloadRaw: `{"host":"vps","cmd_preview":"sudo apt","interactive_snippet_b64":"WzM3OF0K"}`,
					EmittedAt:  1759320003},
			},
		})
	})
	return httptest.NewServer(mux)
}

func TestSSHExecValidate_RequiresFlags(t *testing.T) {
	srv := mockSSHExecAuditServer(t)
	defer srv.Close()
	_, _, err := invokeSSHExecCmd(t, []string{"validate"}, srv.URL)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestSSHExecValidate_AllowedCommand(t *testing.T) {
	dir := t.TempDir()
	tomlPath := writeTOMLFile(t, dir, "test-proj", []string{"git status", "git log"}, []string{"vps"})
	srv := mockSSHExecAuditServer(t)
	defer srv.Close()
	stdout, _, err := invokeSSHExecCmd(t, []string{
		"validate", "--cmd=git status", "--project=test-proj", "--toml=" + tomlPath,
	}, srv.URL)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !strings.Contains(stdout, "ok") || !strings.Contains(stdout, "git status") {
		t.Errorf("got %s", stdout)
	}
}

func TestSSHExecValidate_DeniedCommand(t *testing.T) {
	dir := t.TempDir()
	tomlPath := writeTOMLFile(t, dir, "test-proj", []string{"git status"}, []string{"vps"})
	srv := mockSSHExecAuditServer(t)
	defer srv.Close()
	stdout, _, err := invokeSSHExecCmd(t, []string{
		"validate", "--cmd=rm -rf /", "--project=test-proj", "--toml=" + tomlPath,
	}, srv.URL)
	if err == nil {
		t.Fatal("expected denial error")
	}
	if !strings.Contains(stdout, "deny") {
		t.Errorf("got %s", stdout)
	}
}

func TestSSHExecAllowlistShow(t *testing.T) {
	dir := t.TempDir()
	tomlPath := writeTOMLFile(t, dir, "test-proj", []string{"git status", "alembic *"}, []string{"vps"})
	srv := mockSSHExecAuditServer(t)
	defer srv.Close()
	stdout, _, err := invokeSSHExecCmd(t, []string{
		"allowlist", "show", "--project=test-proj", "--toml=" + tomlPath,
	}, srv.URL)
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	for _, want := range []string{"git status", "alembic", "vps"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in %s", want, stdout)
		}
	}
}

func TestSSHExecAllowlistAdd_RequiresYes(t *testing.T) {
	dir := t.TempDir()
	tomlPath := writeTOMLFile(t, dir, "p1", []string{"git status"}, []string{"vps"})
	srv := mockSSHExecAuditServer(t)
	defer srv.Close()
	_, _, err := invokeSSHExecCmd(t, []string{
		"allowlist", "add", "--project=p1", "--pattern=git log", "--toml=" + tomlPath,
	}, srv.URL)
	if err == nil {
		t.Fatal("expected --yes error")
	}
}

func TestSSHExecAllowlistAdd_HappyPath(t *testing.T) {
	dir := t.TempDir()
	prevWD, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(prevWD) })

	tomlPath := writeTOMLFile(t, dir, "p1", []string{"git status"}, []string{"vps"})
	srv := mockSSHExecAuditServer(t)
	defer srv.Close()
	relTOML := filepath.Base(tomlPath)
	_, _, err := invokeSSHExecCmd(t, []string{
		"allowlist", "add", "--project=p1", "--pattern=git log", "--toml=" + relTOML, "--yes",
	}, srv.URL)
	if err != nil {
		t.Fatalf("add: %v", err)
	}

	data, _ := os.ReadFile(tomlPath)
	if !strings.Contains(string(data), "git log") {
		t.Errorf("toml missing 'git log': %s", string(data))
	}
}

func TestSSHExecAllowlistAdd_Idempotent(t *testing.T) {
	dir := t.TempDir()
	prevWD, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(prevWD) })

	tomlPath := writeTOMLFile(t, dir, "p1", []string{"git status"}, []string{"vps"})
	srv := mockSSHExecAuditServer(t)
	defer srv.Close()
	relTOML := filepath.Base(tomlPath)
	for i := 0; i < 3; i++ {
		_, _, err := invokeSSHExecCmd(t, []string{
			"allowlist", "add", "--project=p1", "--pattern=git log", "--toml=" + relTOML, "--yes",
		}, srv.URL)
		if err != nil {
			t.Fatalf("idempotent add iter %d: %v", i, err)
		}
	}

	data, _ := os.ReadFile(tomlPath)
	count := strings.Count(string(data), `"git log"`)
	if count != 1 {
		t.Errorf("expected 1 occurrence of 'git log', got %d in %s", count, string(data))
	}
}

func TestSSHExecAllowlistRemove(t *testing.T) {
	dir := t.TempDir()
	prevWD, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(prevWD) })

	tomlPath := writeTOMLFile(t, dir, "p1", []string{"git status", "git log"}, []string{"vps"})
	srv := mockSSHExecAuditServer(t)
	defer srv.Close()
	relTOML := filepath.Base(tomlPath)
	_, _, err := invokeSSHExecCmd(t, []string{
		"allowlist", "remove", "--project=p1", "--pattern=git log", "--toml=" + relTOML, "--yes",
	}, srv.URL)
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	data, _ := os.ReadFile(tomlPath)
	if strings.Contains(string(data), `"git log"`) {
		t.Errorf("toml still has 'git log': %s", string(data))
	}
}

func TestSSHExecAuditLog(t *testing.T) {
	srv := mockSSHExecAuditServer(t)
	defer srv.Close()
	stdout, _, err := invokeSSHExecCmd(t, []string{"audit-log", "--project=test-proj"}, srv.URL)
	if err != nil {
		t.Fatalf("audit-log: %v", err)
	}
	// Canonical event types (review F-5): MUST contain at least one
	// of the four MCP-emitted ssh_exec.* types.
	wantAny := []string{"ssh_exec.started", "ssh_exec.completed", "ssh_exec.denied", "ssh_exec.interactive_blocked"}
	hit := false
	for _, w := range wantAny {
		if strings.Contains(stdout, w) {
			hit = true
			break
		}
	}
	if !hit {
		t.Errorf("expected one of %v in stdout: %s", wantAny, stdout)
	}

	if strings.Contains(stdout, "sshexec.completed") || strings.Contains(stdout, "sshexec.started") {
		t.Errorf("legacy 'sshexec.*' (no underscore) leaked: %s", stdout)
	}
}

// TestSSHExec_DoctrineCeilingFromDaemon asserts that the CLI queries
// /v1/doctrine/state for the active doctrine ceiling instead of
// hardcoding doctrine.DefaultBuiltin (review F-6). When the daemon
// advertises capa-firewall the ssh-exec ceiling shrinks to read-only
// diagnostics (only `git status`, `git log`); a pattern only present
// in default/max-scope (e.g. `alembic *`) MUST be rejected.
func TestSSHExec_DoctrineCeilingFromDaemon(t *testing.T) {
	dir := t.TempDir()

	tomlPath := writeTOMLFile(t, dir, "p1", []string{"alembic *"}, []string{"vps"})

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/doctrine/state", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"name": "capa-firewall"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	_, _, err := invokeSSHExecCmd(t, []string{
		"validate", "--cmd=alembic upgrade head", "--project=p1", "--toml=" + tomlPath,
	}, srv.URL)
	if err == nil {
		t.Fatal("expected ceiling-violation error: capa-firewall doesn't allow alembic")
	}
	if !strings.Contains(err.Error(), "ceiling") && !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("error should mention ceiling/exceeds: %v", err)
	}
}

func TestSSHExec_DoctrineCeilingDaemonDownFallsBack(t *testing.T) {
	dir := t.TempDir()
	tomlPath := writeTOMLFile(t, dir, "p1", []string{"alembic *"}, []string{"vps"})

	stdout, _, err := invokeSSHExecCmd(t, []string{
		"validate", "--cmd=alembic upgrade head", "--project=p1", "--toml=" + tomlPath,
	}, "http://127.0.0.1:1")
	if err != nil {
		t.Fatalf("default-builtin fallback should permit alembic: %v", err)
	}
	if !strings.Contains(stdout, "ok") {
		t.Errorf("got %s", stdout)
	}
}

func TestSSHExec_TomlAbsentFallsThroughToDoctrineOnly(t *testing.T) {
	dir := t.TempDir()
	prevWD, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(prevWD) })

	// `git status` is in DefaultBuiltin doctrine; with no zenswarm.toml
	// in cwd the CLI MUST allow it (doctrine-only mode).
	stdout, _, err := invokeSSHExecCmd(t, []string{
		"validate", "--cmd=git status", "--project=p1",
	}, "http://127.0.0.1:1")
	if err != nil {
		t.Fatalf("doctrine-only validate should succeed: %v", err)
	}
	if !strings.Contains(stdout, "ok") {
		t.Errorf("got %s", stdout)
	}
}

func TestSSHExec_ExplicitTomlMissingErrors(t *testing.T) {
	_, _, err := invokeSSHExecCmd(t, []string{
		"validate", "--cmd=git status", "--project=p1", "--toml=/tmp/does-not-exist-zenswarm.toml",
	}, "http://127.0.0.1:1")
	if err == nil {
		t.Fatal("expected error for explicitly-passed missing --toml")
	}
}

func TestSSHExecAuditLog_FiltersCanonicalPrefix(t *testing.T) {
	var captured string
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/audit/events", func(w http.ResponseWriter, r *http.Request) {
		captured = r.URL.Query().Get("type")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []client.AuditEvent{},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	_, _, err := invokeSSHExecCmd(t, []string{"audit-log", "--project=test-proj"}, srv.URL)
	if err != nil {
		t.Fatalf("audit-log: %v", err)
	}
	if captured != "ssh_exec" {
		t.Errorf("CLI filter prefix = %q, want %q (canonical wire form per "+
			"internal/mcp/sshexec/emit.go::EmitStarted etc.)", captured, "ssh_exec")
	}
}

func TestSSHExecExec_RequiresFlags(t *testing.T) {
	srv := mockSSHExecAuditServer(t)
	defer srv.Close()
	_, _, err := invokeSSHExecCmd(t, []string{"exec"}, srv.URL)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestSSHExecExec_DryRun(t *testing.T) {
	dir := t.TempDir()
	tomlPath := writeTOMLFile(t, dir, "p1", []string{"git status"}, []string{"vps"})
	srv := mockSSHExecAuditServer(t)
	defer srv.Close()
	stdout, _, err := invokeSSHExecCmd(t, []string{
		"exec", "--host=vps", "--cmd=git status", "--project=p1", "--toml=" + tomlPath, "--dry-run",
	}, srv.URL)
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if !strings.Contains(stdout, "DRY-RUN") {
		t.Errorf("got %s", stdout)
	}
}

func TestSSHExecExec_RequiresYesOrDryRun(t *testing.T) {
	dir := t.TempDir()
	tomlPath := writeTOMLFile(t, dir, "p1", []string{"git status"}, []string{"vps"})
	srv := mockSSHExecAuditServer(t)
	defer srv.Close()
	_, _, err := invokeSSHExecCmd(t, []string{
		"exec", "--host=vps", "--cmd=git status", "--project=p1", "--toml=" + tomlPath,
	}, srv.URL)
	if err == nil {
		t.Fatal("expected --yes/--dry-run error")
	}
}

func TestSSHExecExec_BadHost(t *testing.T) {
	dir := t.TempDir()
	tomlPath := writeTOMLFile(t, dir, "p1", []string{"git status"}, []string{"vps"})
	srv := mockSSHExecAuditServer(t)
	defer srv.Close()
	_, _, err := invokeSSHExecCmd(t, []string{
		"exec", "--host=evil:22", "--cmd=git status", "--project=p1", "--toml=" + tomlPath, "--dry-run",
	}, srv.URL)
	if err == nil {
		t.Fatal("expected host-not-in-allowlist error")
	}
}

func TestSSHExecSubcommandsRegistered(t *testing.T) {
	root := NewSSHExecCmd()
	want := []string{"validate", "allowlist", "audit-log", "exec"}
	have := map[string]bool{}
	for _, c := range root.Commands() {
		have[c.Name()] = true
	}
	for _, w := range want {
		if !have[w] {
			t.Errorf("missing subcommand: ssh-exec %s", w)
		}
	}
}

func TestSSHExecAllowlistSubcommands(t *testing.T) {
	root := NewSSHExecCmd()
	for _, c := range root.Commands() {
		if c.Name() == "allowlist" {
			want := []string{"show", "add", "remove"}
			have := map[string]bool{}
			for _, sub := range c.Commands() {
				have[sub.Name()] = true
			}
			for _, w := range want {
				if !have[w] {
					t.Errorf("missing subcommand: allowlist %s", w)
				}
			}
			return
		}
	}
	t.Fatal("allowlist not found")
}

type recordingEmitServer struct {
	*httptest.Server
	mu     sync.Mutex
	events []client.AuditEmitReq
}

func (r *recordingEmitServer) recorded() []client.AuditEmitReq {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]client.AuditEmitReq, len(r.events))
	copy(out, r.events)
	return out
}

func newRecordingEmitServer(t *testing.T) *recordingEmitServer {
	t.Helper()
	rec := &recordingEmitServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/audit/emit", func(w http.ResponseWriter, r *http.Request) {
		var req client.AuditEmitReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		rec.mu.Lock()
		rec.events = append(rec.events, req)
		rec.mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(client.AuditEmitResp{
			ID: "uuid-rec", Accepted: true, EmittedAt: 1234,
		})
	})
	rec.Server = httptest.NewServer(mux)
	return rec
}

// TestDispatcherAuditEmitter_EmitsAllFourTypes is the load-bearing
// regression test for review F-1: the CLI exec path MUST produce real
// audit events (ssh_exec.{started,completed,denied,interactive_blocked})
// via the daemon /v1/audit/emit route, NOT discard them through a
// NopAuditEmitter. Without this test, the regression would resurface.
func TestDispatcherAuditEmitter_EmitsAllFourTypes(t *testing.T) {
	rec := newRecordingEmitServer(t)
	defer rec.Close()
	cli := client.NewWithBaseURL(rec.URL)
	em := newDispatcherAuditEmitter(cli, "test-proj")

	req := sshexec.ExecRequest{
		Host: "vps", Command: "git status", Project: "test-proj",
	}
	if err := em.EmitStarted(req); err != nil {
		t.Fatalf("EmitStarted: %v", err)
	}
	if err := em.EmitCompleted(req, sshexec.ExecResult{ExitCode: 0, ExitReason: sshexec.ExitReasonNormal}); err != nil {
		t.Fatalf("EmitCompleted: %v", err)
	}
	if err := em.EmitDenied(req, "validation rejected: deny test"); err != nil {
		t.Fatalf("EmitDenied: %v", err)
	}
	if err := em.EmitInteractiveBlocked(req, []byte("Are you sure? [y/N]")); err != nil {
		t.Fatalf("EmitInteractiveBlocked: %v", err)
	}

	got := rec.recorded()
	if len(got) != 4 {
		t.Fatalf("want 4 events, got %d", len(got))
	}
	wantTypes := map[string]bool{
		"ssh_exec.started":             true,
		"ssh_exec.completed":           true,
		"ssh_exec.denied":              true,
		"ssh_exec.interactive_blocked": true,
	}
	seenTypes := map[string]bool{}
	for _, ev := range got {
		seenTypes[ev.Type] = true
		if ev.ProjectID != "test-proj" {
			t.Errorf("project_id=%q want test-proj for type=%s", ev.ProjectID, ev.Type)
		}
		if ev.Payload == nil {
			t.Errorf("payload nil for type=%s", ev.Type)
		}
	}
	for w := range wantTypes {
		if !seenTypes[w] {
			t.Errorf("missing event type %q (saw: %v)", w, seenTypes)
		}
	}
}

func TestDispatcherAuditEmitter_PayloadShape(t *testing.T) {
	rec := newRecordingEmitServer(t)
	defer rec.Close()
	cli := client.NewWithBaseURL(rec.URL)
	em := newDispatcherAuditEmitter(cli, "p1")

	long := strings.Repeat("a", 200)
	req := sshexec.ExecRequest{Host: "vps", Command: long, Project: "p1"}
	if err := em.EmitStarted(req); err != nil {
		t.Fatalf("EmitStarted: %v", err)
	}
	if err := em.EmitCompleted(req, sshexec.ExecResult{
		ExitCode: 7, ExitReason: sshexec.ExitReasonNormal,
	}); err != nil {
		t.Fatalf("EmitCompleted: %v", err)
	}
	if err := em.EmitInteractiveBlocked(req, []byte{0xfd, 0x18, 'y', '?'}); err != nil {
		t.Fatalf("EmitInteractiveBlocked: %v", err)
	}

	got := rec.recorded()
	if len(got) != 3 {
		t.Fatalf("want 3 events, got %d", len(got))
	}

	started := payloadOf(t, got, "ssh_exec.started")
	if started["host"] != "vps" {
		t.Errorf("started.host=%v", started["host"])
	}
	preview, ok := started["cmd_preview"].(string)
	if !ok || len(preview) > 200 || !strings.HasPrefix(preview, "aaaa") {
		t.Errorf("started.cmd_preview=%q", preview)
	}
	if !strings.HasSuffix(preview, "...") && !strings.HasSuffix(preview, "…") {
		t.Errorf("started.cmd_preview not truncated: len=%d", len(preview))
	}

	completed := payloadOf(t, got, "ssh_exec.completed")
	exit, ok := completed["exit_code"]
	if !ok {
		t.Errorf("completed.exit_code missing")
	}

	if v, _ := exit.(float64); int(v) != 7 {
		t.Errorf("completed.exit_code=%v want 7", exit)
	}
	if completed["exit_reason"] != "normal" {
		t.Errorf("completed.exit_reason=%v", completed["exit_reason"])
	}

	blocked := payloadOf(t, got, "ssh_exec.interactive_blocked")
	snippetB64, ok := blocked["interactive_snippet_b64"].(string)
	if !ok || snippetB64 == "" {
		t.Errorf("interactive_blocked.interactive_snippet_b64 missing or empty")
	}
}

func payloadOf(t *testing.T, events []client.AuditEmitReq, eventType string) map[string]any {
	t.Helper()
	for _, ev := range events {
		if ev.Type == eventType {
			m, ok := ev.Payload.(map[string]any)
			if !ok {
				t.Fatalf("event %q payload type=%T", eventType, ev.Payload)
			}
			return m
		}
	}
	t.Fatalf("event %q not found in recorded", eventType)
	return nil
}

// TestDispatcherAuditEmitter_DaemonDown ensures emit failures don't
// panic or block. Audit emission MUST NOT prevent CLI completion (the
// audit pipeline is best-effort from the operator's perspective; the
// daemon-side EmitClient wraps invariant no-loss separately).
func TestDispatcherAuditEmitter_DaemonDown(t *testing.T) {
	cli := client.NewWithBaseURL("http://127.0.0.1:1")
	em := newDispatcherAuditEmitter(cli, "p1")
	req := sshexec.ExecRequest{Host: "vps", Command: "ls", Project: "p1"}

	em.EmitStarted(req)
	em.EmitCompleted(req, sshexec.ExecResult{})
	em.EmitDenied(req, "x")
	em.EmitInteractiveBlocked(req, nil)
}

func TestDispatcherAuditEmitter_NilClient(t *testing.T) {
	em := newDispatcherAuditEmitter(nil, "p1")
	if err := em.EmitStarted(sshexec.ExecRequest{Host: "vps", Command: "ls"}); err != nil {
		t.Errorf("nil client should be a no-op: %v", err)
	}
}

func TestCmdPreview_TruncatesAndPreserves(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"", 5, ""},
		{"abc", 5, "abc"},
		{"abcde", 5, "abcde"},
		{"abcdef", 5, "abcde..."},
	}
	for _, c := range cases {
		got := cmdPreview(c.in, c.n)
		if got != c.want {
			t.Errorf("cmdPreview(%q, %d)=%q, want %q", c.in, c.n, got, c.want)
		}
	}
}

// TestValidateZenswarmTOMLPath covers the security helper's branches:
// inside-cwd accepts, outside-cwd rejects, the literal default name
// at cwd accepts, errors propagate.
func TestValidateZenswarmTOMLPath(t *testing.T) {
	dir := t.TempDir()
	prevWD, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(prevWD) })

	abs, err := validateZenswarmTOMLPath("zenswarm.toml")
	if err != nil {
		t.Fatalf("default name should accept: %v", err)
	}
	if !strings.HasSuffix(abs, "zenswarm.toml") {
		t.Errorf("got %q", abs)
	}

	if _, err := validateZenswarmTOMLPath("subdir/x.toml"); err != nil {
		t.Errorf("subdir relative path should accept: %v", err)
	}

	if _, err := validateZenswarmTOMLPath("/etc/passwd"); err == nil {
		t.Error("expected outside-cwd rejection")
	}
}

func TestCLIStreamSink_RoutesByLabel(t *testing.T) {
	var stdout, stderr bytes.Buffer
	sink := &cliStreamSink{out: &stdout, errw: &stderr}

	if err := sink.Emit(sshexec.StreamChunk{Stream: sshexec.StreamStdout, Data: []byte("out-data")}); err != nil {
		t.Fatalf("stdout emit: %v", err)
	}
	if err := sink.Emit(sshexec.StreamChunk{Stream: sshexec.StreamStderr, Data: []byte("err-data")}); err != nil {
		t.Fatalf("stderr emit: %v", err)
	}

	if err := sink.Emit(sshexec.StreamChunk{Stream: "nope", Data: []byte("unknown")}); err != nil {
		t.Fatalf("unknown emit: %v", err)
	}

	if stdout.String() != "out-data" {
		t.Errorf("stdout=%q", stdout.String())
	}
	if stderr.String() != "err-data" {
		t.Errorf("stderr=%q", stderr.String())
	}
}

func TestSSHExecAllowlistAdd_ProjectMismatch(t *testing.T) {
	dir := t.TempDir()
	prevWD, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(prevWD) })

	tomlPath := writeTOMLFile(t, dir, "p-original", []string{"git status"}, []string{"vps"})
	srv := mockSSHExecAuditServer(t)
	defer srv.Close()
	relTOML := filepath.Base(tomlPath)
	_, _, err := invokeSSHExecCmd(t, []string{
		"allowlist", "add", "--project=p-different", "--pattern=git log", "--toml=" + relTOML, "--yes",
	}, srv.URL)
	if err == nil {
		t.Fatal("expected project-mismatch error")
	}
	if !strings.Contains(err.Error(), "project mismatch") {
		t.Errorf("error text: %v", err)
	}
}

func TestSSHExecAllowlistRemove_RequiresYes(t *testing.T) {
	dir := t.TempDir()
	tomlPath := writeTOMLFile(t, dir, "p1", []string{"git status"}, []string{"vps"})
	srv := mockSSHExecAuditServer(t)
	defer srv.Close()
	_, _, err := invokeSSHExecCmd(t, []string{
		"allowlist", "remove", "--project=p1", "--pattern=git status", "--toml=" + tomlPath,
	}, srv.URL)
	if err == nil {
		t.Fatal("expected --yes error")
	}
}

func TestSSHExecAllowlistRemove_RequiresFlags(t *testing.T) {
	dir := t.TempDir()
	tomlPath := writeTOMLFile(t, dir, "p1", []string{"git status"}, []string{"vps"})
	srv := mockSSHExecAuditServer(t)
	defer srv.Close()
	_, _, err := invokeSSHExecCmd(t, []string{
		"allowlist", "remove", "--project=p1", "--toml=" + tomlPath, "--yes",
	}, srv.URL)
	if err == nil {
		t.Fatal("expected required-flag error")
	}
}

func TestSSHExecAllowlistAdd_RequiresFlags(t *testing.T) {
	srv := mockSSHExecAuditServer(t)
	defer srv.Close()
	_, _, err := invokeSSHExecCmd(t, []string{
		"allowlist", "add", "--project=p1", "--yes",
	}, srv.URL)
	if err == nil {
		t.Fatal("expected --pattern error")
	}
}

func TestSSHExecAuditLog_SinceFlag(t *testing.T) {
	srv := mockSSHExecAuditServer(t)
	defer srv.Close()
	_, _, err := invokeSSHExecCmd(t, []string{
		"audit-log", "--project=test-proj", "--since=1h",
	}, srv.URL)
	if err != nil {
		t.Fatalf("audit-log: %v", err)
	}
}

func TestSSHExecAuditLog_BadSince(t *testing.T) {
	srv := mockSSHExecAuditServer(t)
	defer srv.Close()
	_, _, err := invokeSSHExecCmd(t, []string{
		"audit-log", "--project=test-proj", "--since=not-a-duration",
	}, srv.URL)
	if err == nil {
		t.Fatal("expected since-parse error")
	}
}

func TestSSHExecAuditLog_ExclusiveFlags(t *testing.T) {
	srv := mockSSHExecAuditServer(t)
	defer srv.Close()
	_, _, err := invokeSSHExecCmd(t, []string{
		"audit-log", "--project=test-proj", "--quiet", "--verbose",
	}, srv.URL)
	if err == nil {
		t.Fatal("expected mutually-exclusive error")
	}
}

func TestSSHExecExec_TimeoutFlag(t *testing.T) {
	dir := t.TempDir()
	tomlPath := writeTOMLFile(t, dir, "p1", []string{"git status"}, []string{"vps"})
	srv := mockSSHExecAuditServer(t)
	defer srv.Close()
	stdout, _, err := invokeSSHExecCmd(t, []string{
		"exec", "--host=vps", "--cmd=git status", "--project=p1", "--toml=" + tomlPath,
		"--dry-run", "--timeout=30s",
	}, srv.URL)
	if err != nil {
		t.Fatalf("dry-run with timeout: %v", err)
	}
	if !strings.Contains(stdout, "DRY-RUN") {
		t.Errorf("got %s", stdout)
	}
}

func TestSSHExecExec_BadTimeout(t *testing.T) {
	dir := t.TempDir()
	tomlPath := writeTOMLFile(t, dir, "p1", []string{"git status"}, []string{"vps"})
	srv := mockSSHExecAuditServer(t)
	defer srv.Close()
	_, _, err := invokeSSHExecCmd(t, []string{
		"exec", "--host=vps", "--cmd=git status", "--project=p1", "--toml=" + tomlPath,
		"--dry-run", "--timeout=not-a-duration",
	}, srv.URL)
	if err == nil {
		t.Fatal("expected timeout-parse error")
	}
}

func TestSSHExecExec_DeniedByValidator(t *testing.T) {
	dir := t.TempDir()
	tomlPath := writeTOMLFile(t, dir, "p1", []string{"git status"}, []string{"vps"})
	srv := mockSSHExecAuditServer(t)
	defer srv.Close()
	_, _, err := invokeSSHExecCmd(t, []string{
		"exec", "--host=vps", "--cmd=rm -rf /", "--project=p1", "--toml=" + tomlPath,
		"--dry-run",
	}, srv.URL)
	if err == nil {
		t.Fatal("expected validator rejection")
	}
}

func TestLoadAllowlistForCmd_RequiresProject(t *testing.T) {
	srv := mockSSHExecAuditServer(t)
	defer srv.Close()
	_, _, err := invokeSSHExecCmd(t, []string{
		"validate", "--cmd=git status",
	}, srv.URL)
	if err == nil {
		t.Fatal("expected --project required")
	}
}

func TestSSHExecAllowlistShow_ExclusiveFlags(t *testing.T) {
	dir := t.TempDir()
	tomlPath := writeTOMLFile(t, dir, "p1", []string{"git status"}, []string{"vps"})
	srv := mockSSHExecAuditServer(t)
	defer srv.Close()
	_, _, err := invokeSSHExecCmd(t, []string{
		"allowlist", "show", "--project=p1", "--toml=" + tomlPath, "--quiet", "--verbose",
	}, srv.URL)
	if err == nil {
		t.Fatal("expected mutually-exclusive error")
	}
}

func TestLookupDoctrineName_GoFieldFallback(t *testing.T) {
	if got := lookupDoctrineName(client.DoctrineState{"name": "max-scope"}); got != "max-scope" {
		t.Errorf("lowercase name: got %q", got)
	}
	if got := lookupDoctrineName(client.DoctrineState{"Name": "default"}); got != "default" {
		t.Errorf("Go-field Name fallback: got %q", got)
	}
	if got := lookupDoctrineName(client.DoctrineState{}); got != "" {
		t.Errorf("empty: got %q", got)
	}
	if got := lookupDoctrineName(client.DoctrineState{"name": 123}); got != "" {
		t.Errorf("non-string lowercase: got %q", got)
	}
	if got := lookupDoctrineName(client.DoctrineState{"Name": []string{"x"}}); got != "" {
		t.Errorf("non-string Go-field: got %q", got)
	}
}

func TestSSHExecAllowlist_OutsideCwdRejected(t *testing.T) {
	dir := t.TempDir()
	prevWD, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(prevWD) })

	srv := mockSSHExecAuditServer(t)
	defer srv.Close()

	// Outside-cwd target — pre-fix would be silently written. Post-fix MUST
	// be rejected before any file I/O.
	outside := filepath.Join(t.TempDir(), "proof-add.toml")
	if _, err := os.Stat(outside); err == nil {
		_ = os.Remove(outside)
	}

	for _, sub := range []string{"add", "remove"} {
		t.Run(sub, func(t *testing.T) {
			outsideSub := filepath.Join(t.TempDir(), "proof-"+sub+".toml")
			_, _, err := invokeSSHExecCmd(t, []string{
				"allowlist", sub,
				"--project=p1", "--pattern=ls *",
				"--toml=" + outsideSub, "--yes",
			}, srv.URL)
			if err == nil {
				t.Fatalf("%s: expected outside-cwd rejection, got success (file should NOT exist)", sub)
			}
			if !strings.Contains(err.Error(), "under cwd") &&
				!strings.Contains(err.Error(), "outside") &&
				!strings.Contains(err.Error(), "must be") {
				t.Errorf("%s: error should mention cwd boundary: %v", sub, err)
			}
			if _, statErr := os.Stat(outsideSub); statErr == nil {
				t.Errorf("%s: SECURITY REGRESSION: file written outside cwd at %s", sub, outsideSub)
				_ = os.Remove(outsideSub)
			}
		})
	}
}

// TestMutateAllowlistTOML_OutsideCwdRejected covers defense-in-depth:
// even if the cobra layer is bypassed (direct call to mutateAllowlistTOML),
// the mutator itself MUST reject outside-cwd paths.
func TestMutateAllowlistTOML_OutsideCwdRejected(t *testing.T) {
	dir := t.TempDir()
	prevWD, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(prevWD) })

	outside := filepath.Join(t.TempDir(), "outside.toml")
	_, err := mutateAllowlistTOML(outside, "p1", "ls *", true)
	if err == nil {
		t.Fatalf("DEFENSE-IN-DEPTH: mutator must reject outside-cwd path %q", outside)
	}
	if !strings.Contains(err.Error(), "under cwd") &&
		!strings.Contains(err.Error(), "outside") &&
		!strings.Contains(err.Error(), "must be") {
		t.Errorf("error should mention cwd boundary: %v", err)
	}
	if _, statErr := os.Stat(outside); statErr == nil {
		t.Errorf("SECURITY REGRESSION: mutator wrote outside cwd at %s", outside)
		_ = os.Remove(outside)
	}
}

func TestMutateAllowlistTOML_OutcomeReporting(t *testing.T) {
	dir := t.TempDir()
	prevWD, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(prevWD) })

	tomlPath := writeTOMLFile(t, dir, "p1", []string{"git status"}, []string{"vps"})

	got, err := mutateAllowlistTOML(tomlPath, "p1", "git log", true)
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if got != OutcomeAdded {
		t.Errorf("first add: got %v, want OutcomeAdded", got)
	}

	got, err = mutateAllowlistTOML(tomlPath, "p1", "git log", true)
	if err != nil {
		t.Fatalf("idempotent add: %v", err)
	}
	if got != OutcomeAlreadyPresent {
		t.Errorf("idempotent add: got %v, want OutcomeAlreadyPresent", got)
	}

	got, err = mutateAllowlistTOML(tomlPath, "p1", "git log", false)
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if got != OutcomeRemoved {
		t.Errorf("remove: got %v, want OutcomeRemoved", got)
	}

	got, err = mutateAllowlistTOML(tomlPath, "p1", "git log", false)
	if err != nil {
		t.Fatalf("idempotent remove: %v", err)
	}
	if got != OutcomeNotPresent {
		t.Errorf("idempotent remove: got %v, want OutcomeNotPresent", got)
	}
}

func TestSSHExecAllowlistAdd_PrintsAlreadyPresent(t *testing.T) {
	dir := t.TempDir()
	prevWD, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(prevWD) })

	tomlPath := writeTOMLFile(t, dir, "p1", []string{"git status"}, []string{"vps"})
	srv := mockSSHExecAuditServer(t)
	defer srv.Close()

	relTOML := filepath.Base(tomlPath)
	stdout, _, err := invokeSSHExecCmd(t, []string{
		"allowlist", "add", "--project=p1", "--pattern=git log",
		"--toml=" + relTOML, "--yes",
	}, srv.URL)
	if err != nil {
		t.Fatalf("first add: %v", err)
	}
	if !strings.Contains(stdout, "added") {
		t.Errorf("first add output should mention 'added': %s", stdout)
	}

	stdout, _, err = invokeSSHExecCmd(t, []string{
		"allowlist", "add", "--project=p1", "--pattern=git log",
		"--toml=" + relTOML, "--yes",
	}, srv.URL)
	if err != nil {
		t.Fatalf("re-add: %v", err)
	}
	if !strings.Contains(stdout, "already") {
		t.Errorf("re-add should report 'already' present, got: %s", stdout)
	}
}

func TestMutateAllowlistTOML_ParseError(t *testing.T) {
	dir := t.TempDir()
	prevWD, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(prevWD) })

	bad := filepath.Join(dir, "zenswarm.toml")
	if err := os.WriteFile(bad, []byte("not = valid = toml"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := mutateAllowlistTOML(bad, "p1", "ls *", true)
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !strings.Contains(err.Error(), "parse") {
		t.Errorf("error should mention parse: %v", err)
	}
}

func TestMutateAllowlistTOML_NewFileCreates(t *testing.T) {
	dir := t.TempDir()
	prevWD, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(prevWD) })

	target := filepath.Join(dir, "zenswarm.toml")
	got, err := mutateAllowlistTOML(target, "p1", "git status", true)
	if err != nil {
		t.Fatalf("create-on-add: %v", err)
	}
	if got != OutcomeAdded {
		t.Errorf("got %v, want OutcomeAdded", got)
	}
	if _, statErr := os.Stat(target); statErr != nil {
		t.Errorf("file should now exist: %v", statErr)
	}
}

func TestSSHExecAllowlistRemove_PrintsNotPresent(t *testing.T) {
	dir := t.TempDir()
	prevWD, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(prevWD) })

	tomlPath := writeTOMLFile(t, dir, "p1", []string{"git status"}, []string{"vps"})
	srv := mockSSHExecAuditServer(t)
	defer srv.Close()

	relTOML := filepath.Base(tomlPath)
	stdout, _, err := invokeSSHExecCmd(t, []string{
		"allowlist", "remove", "--project=p1", "--pattern=never-present",
		"--toml=" + relTOML, "--yes",
	}, srv.URL)
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if !strings.Contains(stdout, "not present") && !strings.Contains(stdout, "not-present") {
		t.Errorf("remove of non-existent should mention 'not present': %s", stdout)
	}
}
