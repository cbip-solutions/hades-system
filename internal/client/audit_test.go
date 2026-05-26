package client_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/client"
	"github.com/cbip-solutions/hades-system/internal/mcp/audit"
)

func TestAuditEmit(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/audit/emit", func(w http.ResponseWriter, r *http.Request) {
		var req client.AuditEmitReq
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Type != "test.event" {
			t.Errorf("type: %q", req.Type)
		}
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(client.AuditEmitResp{ID: "uuid-1", Accepted: true, EmittedAt: 1234})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	resp, err := c.AuditEmit(context.Background(), client.AuditEmitReq{Type: "test.event", Payload: map[string]string{"k": "v"}})
	if err != nil {
		t.Fatalf("AuditEmit: %v", err)
	}
	if !resp.Accepted || resp.ID != "uuid-1" {
		t.Errorf("got %+v", resp)
	}
}

func TestAuditEvents_Filter(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/audit/events", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("type") != "audit_review" || q.Get("project") != "internal-platform-x" || q.Get("since") != "1000" {
			t.Errorf("query: %s", r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []client.AuditEvent{
				{ID: "e1", Type: "audit_review.completed", ProjectID: "internal-platform-x", PayloadRaw: `{"verdict":"accept"}`},
			},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	events, err := c.AuditEvents(context.Background(), "audit_review", "internal-platform-x", 1000, 50)
	if err != nil {
		t.Fatalf("AuditEvents: %v", err)
	}
	if len(events) != 1 || !strings.Contains(events[0].PayloadRaw, "accept") {
		t.Errorf("got %+v", events)
	}
}

func TestAuditTypes(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/audit/types", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []client.AuditType{
				{Type: "audit_review.completed", Count: 42},
				{Type: "sshexec.started", Count: 19},
			},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	items, err := c.AuditTypes(context.Background())
	if err != nil {
		t.Fatalf("AuditTypes: %v", err)
	}
	if len(items) != 2 || items[0].Count != 42 {
		t.Errorf("got %+v", items)
	}
}

func TestAuditFamilies_HasCanonicalSet(t *testing.T) {
	fams := client.AuditFamilies()
	if len(fams) < 2 {
		t.Fatalf("inv-zen-080: need ≥2 families, got %d", len(fams))
	}
	names := map[string]bool{}
	for _, f := range fams {
		names[f.Name] = true
	}
	for _, want := range []string{"anthropic", "google", "deepseek"} {
		if !names[want] {
			t.Errorf("missing family %q", want)
		}
	}
}

func TestAuditCriteria_HasDefault(t *testing.T) {
	crits := client.AuditCriteria()
	hasDefault := false
	for _, c := range crits {
		if c.Name == "default" {
			hasDefault = true
		}
	}
	if !hasDefault {
		t.Error("missing default criterion")
	}
}

// TestAuditCriteria_MatchesAuditMCPRegistry asserts that every CLI-side
// criterion name is one the audit MCP's CriteriaRegistry can dispatch.
//
// SOURCE OF TRUTH (review C-3): the canonical set is computed from
// audit.DefaultCriteriaTemplateNames() — the same function the MCP
// CriteriaRegistry constructs from. If a new template is added to
// internal/mcp/audit/criteria.go::defaultTemplates OR an existing one
// is renamed, this test fails until the CLI catalog is updated. Pre-fix
// the test compared against a hardcoded `canonicalSet` map; both the
// CLI catalog AND the test would silently continue "passing" with stale
// names because they referenced their own copy of the list.
//
// security, performance, doctrine-violation. A drift here makes
// `zen audit criteria show <name>` advertise a name the MCP rejects
// (review F-4 + C-3).
func TestAuditCriteria_MatchesAuditMCPRegistry(t *testing.T) {
	crits := client.AuditCriteria()
	canonicalNames := audit.DefaultCriteriaTemplateNames()
	canonicalSet := make(map[string]bool, len(canonicalNames))
	for _, n := range canonicalNames {
		canonicalSet[n] = true
	}
	cliNames := map[string]bool{}
	for _, c := range crits {
		cliNames[c.Name] = true
		if !canonicalSet[c.Name] {
			t.Errorf("non-canonical criterion %q (audit MCP CriteriaRegistry will reject; "+
				"valid names from audit.DefaultCriteriaTemplateNames(): %v)", c.Name, canonicalNames)
		}
	}

	for _, bad := range []string{"doctrine-max-scope", "doctrine-default", "doctrine-capa-firewall"} {
		if cliNames[bad] {
			t.Errorf("legacy criterion %q must not appear in catalog", bad)
		}
	}
	// Every canonical name MUST be present in the CLI catalog (no
	// silent omissions either — operators must see all dispatchable
	// templates).
	for _, want := range canonicalNames {
		if !cliNames[want] {
			t.Errorf("CLI catalog missing canonical criterion %q (registered in audit MCP "+
				"defaultTemplates but absent from client.AuditCriteria)", want)
		}
	}
}

func TestAuditFamiliesFromPool_DescriptionEnrichment(t *testing.T) {
	got := client.AuditFamiliesFromPool([]string{"anthropic", "google", "deepseek", "local-qwen"})
	if len(got) != 4 {
		t.Fatalf("got %d entries", len(got))
	}
	for _, f := range got {
		if !f.Default {
			t.Errorf("AuditFamiliesFromPool result must be all Default=true (active pool): %+v", f)
		}
		if f.Description == "" {
			t.Errorf("missing description for %q", f.Name)
		}
	}

	got = client.AuditFamiliesFromPool([]string{"future-provider"})
	if len(got) != 1 {
		t.Fatalf("got %d entries", len(got))
	}
	if got[0].Description == "" {
		t.Error("unknown family should still get a fallback description")
	}
}

func TestAuditFamiliesResolve_DaemonReturnsPool(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/doctrine/state", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name": "max-scope",
			"reviewer": map[string]any{
				"family_disjoint_pool": []string{"anthropic", "google", "deepseek", "local-qwen"},
				"criteria_default":     "default",
			},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := client.NewWithBaseURL(srv.URL)
	got, err := c.AuditFamiliesResolve(context.Background())
	if err != nil {
		t.Fatalf("AuditFamiliesResolve: %v", err)
	}
	if len(got) != 4 {
		t.Errorf("expected 4 families, got %d", len(got))
	}
	names := map[string]bool{}
	for _, f := range got {
		names[f.Name] = true
	}
	for _, want := range []string{"anthropic", "google", "deepseek", "local-qwen"} {
		if !names[want] {
			t.Errorf("missing %q in resolved pool", want)
		}
	}
}

func TestAuditFamiliesResolve_DaemonStubReturnsFallback(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/doctrine/state", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"name": "max-scope"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := client.NewWithBaseURL(srv.URL)
	got, err := c.AuditFamiliesResolve(context.Background())
	if err != nil {
		t.Fatalf("AuditFamiliesResolve: %v", err)
	}
	if len(got) == 0 {
		t.Error("stub state should fall back to default catalog, got empty")
	}
}

func TestAuditFamiliesResolve_DaemonDown(t *testing.T) {
	c := client.NewWithBaseURL("http://127.0.0.1:1")
	got, err := c.AuditFamiliesResolve(context.Background())
	if err != nil {
		t.Fatalf("daemon-down should fall back without error: %v", err)
	}
	if len(got) == 0 {
		t.Error("expected fallback families")
	}
}

func TestAuditFamiliesResolveFromState_GoFieldShape(t *testing.T) {
	c := client.NewWithBaseURL("http://x")
	state := client.DoctrineState{
		"Reviewer": map[string]any{
			"FamilyDisjointPool": []any{"anthropic", "google"},
		},
	}
	got, ok := c.AuditFamiliesResolveFromState(state)
	if !ok {
		t.Fatal("Go-field-shape state should resolve")
	}
	if len(got) != 2 {
		t.Errorf("got %d families", len(got))
	}
}

func TestAuditFamiliesResolveFromState_StubReturnsEmpty(t *testing.T) {
	c := client.NewWithBaseURL("http://x")
	state := client.DoctrineState{"name": "max-scope"}
	got, ok := c.AuditFamiliesResolveFromState(state)
	if ok {
		t.Error("stub state should return ok=false")
	}
	if len(got) != 0 {
		t.Errorf("stub state should return empty, got %v", got)
	}
}

func TestAuditFamiliesResolveFromState_NonStringElement(t *testing.T) {
	c := client.NewWithBaseURL("http://x")
	state := client.DoctrineState{
		"reviewer": map[string]any{
			"family_disjoint_pool": []any{"anthropic", 42},
		},
	}
	_, ok := c.AuditFamiliesResolveFromState(state)
	if ok {
		t.Error("non-string element should make resolution fail (return false)")
	}
}

func TestAuditFamiliesResolveFromState_NonArrayValue(t *testing.T) {
	c := client.NewWithBaseURL("http://x")
	state := client.DoctrineState{
		"reviewer": map[string]any{
			"family_disjoint_pool": "not-an-array",
		},
	}
	_, ok := c.AuditFamiliesResolveFromState(state)
	if ok {
		t.Error("non-array pool value should fail resolution")
	}
}

func TestAuditFamiliesResolveFromState_OuterNotMap(t *testing.T) {
	c := client.NewWithBaseURL("http://x")
	state := client.DoctrineState{"reviewer": "not-a-map"}
	_, ok := c.AuditFamiliesResolveFromState(state)
	if ok {
		t.Error("non-map reviewer should fail resolution")
	}
}

func TestAuditEmit_Error(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/audit/emit", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"x"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	if _, err := c.AuditEmit(context.Background(), client.AuditEmitReq{Type: "x"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestAuditTypes_Error(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/audit/types", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	if _, err := c.AuditTypes(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestAuditEvents_NoFilter(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/audit/events", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []client.AuditEvent{{ID: "x"}},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	events, err := c.AuditEvents(context.Background(), "", "", 0, 0)
	if err != nil {
		t.Fatalf("AuditEvents: %v", err)
	}
	if len(events) != 1 {
		t.Errorf("got %d", len(events))
	}
}
