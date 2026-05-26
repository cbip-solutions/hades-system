package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/client"
)

func invokeAuditCmd(t *testing.T, args []string, baseURL string) (string, string, error) {
	t.Helper()
	prev := TestOnlyClientFactory
	TestOnlyClientFactory = func(uds string) *client.Client {
		return client.NewWithBaseURL(baseURL)
	}
	t.Cleanup(func() { TestOnlyClientFactory = prev })

	cmd := NewAuditCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

func mockAuditServer(t *testing.T) *httptest.Server {
	t.Helper()
	return mockAuditServerWithDoctrine(t, "max-scope")
}

func mockAuditServerWithDoctrine(t *testing.T, doctrineName string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/audit/emit", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(client.AuditEmitResp{ID: "uuid-1", Accepted: true, EmittedAt: 1234})
	})
	mux.HandleFunc("/v1/audit/events", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []client.AuditEvent{
				{ID: "abc12345-uuid", ProjectID: "internal-platform-x", Type: "audit_review.completed", PayloadRaw: `{"verdict":"accept"}`, EmittedAt: 1759320000},
				{ID: "def67890-uuid", ProjectID: "internal-platform-x", Type: "sshexec.started", PayloadRaw: `{"cmd":"ls"}`, EmittedAt: 1759320500},
			},
		})
	})
	mux.HandleFunc("/v1/audit/types", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []client.AuditType{
				{Type: "audit_review.completed", Count: 42},
				{Type: "sshexec.started", Count: 19},
			},
		})
	})

	mux.HandleFunc("/v1/doctrine/state", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name":    doctrineName,
			"version": "1.0.0",
			"source":  "builtin",
		})
	})
	return httptest.NewServer(mux)
}

func TestAuditEmit_RequiresType(t *testing.T) {
	srv := mockAuditServer(t)
	defer srv.Close()
	_, _, err := invokeAuditCmd(t, []string{"emit"}, srv.URL)
	if err == nil {
		t.Fatal("expected --type error")
	}
}

func TestAuditEmit_HappyPath(t *testing.T) {
	srv := mockAuditServer(t)
	defer srv.Close()
	stdout, _, err := invokeAuditCmd(t, []string{"emit", "--type=test.event", "--payload={\"k\":\"v\"}"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	if !strings.Contains(stdout, "uuid-1") || !strings.Contains(stdout, "accepted=true") {
		t.Errorf("got %s", stdout)
	}
}

func TestAuditEmit_BadPayload(t *testing.T) {
	srv := mockAuditServer(t)
	defer srv.Close()
	_, _, err := invokeAuditCmd(t, []string{"emit", "--type=test.event", "--payload=not-json"}, srv.URL)
	if err == nil {
		t.Fatal("expected JSON error")
	}
}

func TestAuditEvents(t *testing.T) {
	srv := mockAuditServer(t)
	defer srv.Close()
	stdout, _, err := invokeAuditCmd(t, []string{"events"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	if !strings.Contains(stdout, "audit_review.completed") || !strings.Contains(stdout, "sshexec.started") {
		t.Errorf("got %s", stdout)
	}
}

func TestAuditEvents_FilterType(t *testing.T) {
	srv := mockAuditServer(t)
	defer srv.Close()
	stdout, _, err := invokeAuditCmd(t, []string{"events", "--type=audit_review", "--project=internal-platform-x"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	if !strings.Contains(stdout, "audit_review") {
		t.Errorf("got %s", stdout)
	}
}

func TestAuditVerdicts(t *testing.T) {
	srv := mockAuditServer(t)
	defer srv.Close()
	stdout, _, err := invokeAuditCmd(t, []string{"verdicts"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	if !strings.Contains(stdout, "audit_review") {
		t.Errorf("got %s", stdout)
	}
}

func TestAuditTypes(t *testing.T) {
	srv := mockAuditServer(t)
	defer srv.Close()
	stdout, _, err := invokeAuditCmd(t, []string{"types"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	if !strings.Contains(stdout, "42") || !strings.Contains(stdout, "audit_review.completed") {
		t.Errorf("got %s", stdout)
	}
}

func TestAuditFamiliesShow(t *testing.T) {
	srv := mockAuditServer(t)
	defer srv.Close()
	stdout, _, err := invokeAuditCmd(t, []string{"families", "show"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	for _, want := range []string{"anthropic", "google", "deepseek", "inv-zen-080"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in %s", want, stdout)
		}
	}
}

func TestAuditFamiliesShow_JSON(t *testing.T) {
	srv := mockAuditServer(t)
	defer srv.Close()
	stdout, _, err := invokeAuditCmd(t, []string{"families", "show", "--json"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	var arr []map[string]any
	if err := json.Unmarshal([]byte(stdout), &arr); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, stdout)
	}
	if len(arr) < 3 {
		t.Errorf("got %d", len(arr))
	}
}

func TestAuditCriteriaList(t *testing.T) {
	srv := mockAuditServer(t)
	defer srv.Close()
	stdout, _, err := invokeAuditCmd(t, []string{"criteria", "list"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	if !strings.Contains(stdout, "default") || !strings.Contains(stdout, "security") {
		t.Errorf("got %s", stdout)
	}
}

func TestAuditCriteriaShow_Found(t *testing.T) {
	srv := mockAuditServer(t)
	defer srv.Close()
	stdout, _, err := invokeAuditCmd(t, []string{"criteria", "show", "default"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	if !strings.Contains(stdout, "default") {
		t.Errorf("got %s", stdout)
	}
}

func TestAuditCriteriaShow_NotFound(t *testing.T) {
	srv := mockAuditServer(t)
	defer srv.Close()
	_, _, err := invokeAuditCmd(t, []string{"criteria", "show", "nope"}, srv.URL)
	if err == nil {
		t.Fatal("expected not-found error")
	}
}

func TestAuditEvents_BadSince(t *testing.T) {
	srv := mockAuditServer(t)
	defer srv.Close()
	_, _, err := invokeAuditCmd(t, []string{"events", "--since=not-a-duration"}, srv.URL)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAuditEvents_ExclusiveFlags(t *testing.T) {
	srv := mockAuditServer(t)
	defer srv.Close()
	_, _, err := invokeAuditCmd(t, []string{"events", "--quiet", "--verbose"}, srv.URL)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAuditEvents_WithSince(t *testing.T) {
	srv := mockAuditServer(t)
	defer srv.Close()
	_, _, err := invokeAuditCmd(t, []string{"events", "--since=24h"}, srv.URL)
	if err != nil {
		t.Fatalf("events: %v", err)
	}
}

func TestAuditVerdicts_ExclusiveFlags(t *testing.T) {
	srv := mockAuditServer(t)
	defer srv.Close()
	_, _, err := invokeAuditCmd(t, []string{"verdicts", "--quiet", "--verbose"}, srv.URL)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAuditTypes_ExclusiveFlags(t *testing.T) {
	srv := mockAuditServer(t)
	defer srv.Close()
	_, _, err := invokeAuditCmd(t, []string{"types", "--quiet", "--verbose"}, srv.URL)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAuditCriteriaList_ExclusiveFlags(t *testing.T) {
	srv := mockAuditServer(t)
	defer srv.Close()
	_, _, err := invokeAuditCmd(t, []string{"criteria", "list", "--quiet", "--verbose"}, srv.URL)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAuditFamiliesShow_ExclusiveFlags(t *testing.T) {
	srv := mockAuditServer(t)
	defer srv.Close()
	_, _, err := invokeAuditCmd(t, []string{"families", "show", "--quiet", "--verbose"}, srv.URL)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAuditFamiliesShow_DaemonDown(t *testing.T) {
	stdout, _, err := invokeAuditCmd(t, []string{"families", "show"}, "http://127.0.0.1:1")
	if err != nil {
		t.Fatalf("families show: %v", err)
	}
	// MUST render something (default-builtin pool fallback).
	if !strings.Contains(stdout, "anthropic") {
		t.Errorf("expected default fallback families: %s", stdout)
	}
}

func TestAuditFamiliesShow_PerDoctrine_CapaFirewall(t *testing.T) {
	srv := mockAuditServerWithDoctrine(t, "capa-firewall")
	defer srv.Close()
	stdout, _, err := invokeAuditCmd(t, []string{"families", "show"}, srv.URL)
	if err != nil {
		t.Fatalf("families show: %v", err)
	}
	if !strings.Contains(stdout, "local-qwen") || !strings.Contains(stdout, "deepseek") {
		t.Errorf("capa-firewall pool: got %s", stdout)
	}

	if strings.Contains(stdout, "anthropic") {
		t.Errorf("capa-firewall pool should not include anthropic: %s", stdout)
	}
}

func TestAuditFamiliesShow_PerDoctrine_Default(t *testing.T) {
	srv := mockAuditServerWithDoctrine(t, "default")
	defer srv.Close()
	stdout, _, err := invokeAuditCmd(t, []string{"families", "show"}, srv.URL)
	if err != nil {
		t.Fatalf("families show: %v", err)
	}
	if !strings.Contains(stdout, "anthropic") || !strings.Contains(stdout, "google") {
		t.Errorf("default pool missing canonical entries: %s", stdout)
	}

	if strings.Contains(stdout, "deepseek") {
		t.Errorf("default pool should not include deepseek: %s", stdout)
	}
}

func TestLookupString_AllBranches(t *testing.T) {
	if got := lookupString(map[string]any{"k": "v"}, "k"); got != "v" {
		t.Errorf("got %q", got)
	}
	if got := lookupString(map[string]any{"k": 123}, "k"); got != "" {
		t.Errorf("non-string: got %q", got)
	}
	if got := lookupString(map[string]any{}, "missing"); got != "" {
		t.Errorf("missing: got %q", got)
	}
}

func TestAuditSubcommandsRegistered(t *testing.T) {
	root := NewAuditCmd()
	want := []string{"emit", "events", "verdicts", "types", "families", "criteria"}
	have := map[string]bool{}
	for _, c := range root.Commands() {
		have[c.Name()] = true
	}
	for _, w := range want {
		if !have[w] {
			t.Errorf("missing subcommand: audit %s", w)
		}
	}
}
