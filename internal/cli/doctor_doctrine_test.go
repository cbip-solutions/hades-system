package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/client"
)

func newDoctrineClientForURL(t *testing.T, baseURL string) *client.Client {
	t.Helper()
	return client.NewWithBaseURL(baseURL)
}

func TestCheckDoctrineActiveResolves_HappyPath_PerProjectChain(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/doctrine/active":
			_ = json.NewEncoder(w).Encode(client.DoctrineV2ActiveResp{
				Name:            "max-scope",
				SchemaVersion:   "1.0",
				DoctrineVersion: "1.0.0",
				Source:          "embed",
			})
		case "/v1/doctrine/status":
			_ = json.NewEncoder(w).Encode(client.DoctrineV2StatusResp{
				Active: client.DoctrineV2ActiveResp{
					Name: "max-scope", SchemaVersion: "1.0", DoctrineVersion: "1.0.0", Source: "embed",
				},
				WatcherHealthy: true,
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	c := newDoctrineClientForURL(t, srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	got := checkDoctrineActiveResolves(ctx, c)
	if got.Status != "ok" {
		t.Fatalf("Status = %q; want ok; detail=%q", got.Status, got.Detail)
	}
	if !strings.Contains(got.Detail, "max-scope") {
		t.Errorf("Detail = %q; want to contain active doctrine name max-scope", got.Detail)
	}
	if !strings.Contains(got.Detail, "schema_version=1.0") {
		t.Errorf("Detail = %q; want to contain schema_version=1.0", got.Detail)
	}
}

func TestCheckDoctrineActiveResolves_DaemonUnreachable(t *testing.T) {

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, _ := w.(http.Hijacker)
		conn, _, _ := hj.Hijack()
		_ = conn.Close()
	}))
	defer srv.Close()
	c := newDoctrineClientForURL(t, srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	got := checkDoctrineActiveResolves(ctx, c)
	if got.Status != "fail" {
		t.Fatalf("Status = %q; want fail; detail=%q", got.Status, got.Detail)
	}
	if !strings.Contains(got.Hint, "zen doctor daemon") {
		t.Errorf("Hint = %q; want to mention zen doctor daemon", got.Hint)
	}
}

func TestCheckDoctrineActiveResolves_EmptyName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.DoctrineV2ActiveResp{
			Name: "", SchemaVersion: "1.0", Source: "embed",
		})
	}))
	defer srv.Close()
	c := newDoctrineClientForURL(t, srv.URL)
	got := checkDoctrineActiveResolves(context.Background(), c)
	if got.Status != "warn" {
		t.Fatalf("Status = %q; want warn; detail=%q", got.Status, got.Detail)
	}
}

func TestCheckDoctrineBuiltinsLoad_HappyPath_SchemaVersionAssertion(t *testing.T) {
	got := checkDoctrineBuiltinsLoad(context.Background(), nil)
	if got.Status != "ok" {
		t.Fatalf("Status = %q; want ok; detail=%q", got.Status, got.Detail)
	}
	for _, expected := range []string{"max-scope", "default", "capa-firewall"} {
		if !strings.Contains(got.Detail, expected) {
			t.Errorf("Detail = %q; want to contain built-in name %q", got.Detail, expected)
		}
	}
	if !strings.Contains(got.Detail, "schema_version=1.0") {
		t.Errorf("Detail = %q; want to contain schema_version=1.0 assertion", got.Detail)
	}
}

func TestCheckDoctrineOverridesPerProjectHealthy_NoOverrides(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.DoctrineV2ListResp{
			Items: []client.DoctrineV2ListItem{
				{Name: "max-scope", Source: "embed", SchemaVersion: "1.0", DoctrineVersion: "1.0.0"},
				{Name: "default", Source: "embed", SchemaVersion: "1.0", DoctrineVersion: "1.0.0"},
				{Name: "capa-firewall", Source: "embed", SchemaVersion: "1.0", DoctrineVersion: "1.0.0"},
			},
		})
	}))
	defer srv.Close()
	c := newDoctrineClientForURL(t, srv.URL)
	got := checkDoctrineOverridesPerProjectHealthy(context.Background(), c)
	if got.Status != "ok" {
		t.Fatalf("Status = %q; want ok; detail=%q", got.Status, got.Detail)
	}
	if !strings.Contains(got.Detail, "0 project overrides") {
		t.Errorf("Detail = %q; want to mention 0 project overrides", got.Detail)
	}
}

func TestCheckDoctrineOverridesPerProjectHealthy_WithProjectOverrides(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.DoctrineV2ListResp{
			Items: []client.DoctrineV2ListItem{
				{Name: "max-scope", Source: "embed", SchemaVersion: "1.0", DoctrineVersion: "1.0.0"},
				{Name: "internal-platform-x", Source: "project", SchemaVersion: "1.0", DoctrineVersion: "1.0.0"},
				{Name: "zen-swarm", Source: "project", SchemaVersion: "1.0", DoctrineVersion: "1.0.0"},
			},
		})
	}))
	defer srv.Close()
	c := newDoctrineClientForURL(t, srv.URL)
	got := checkDoctrineOverridesPerProjectHealthy(context.Background(), c)
	if got.Status != "ok" {
		t.Fatalf("Status = %q; want ok; detail=%q", got.Status, got.Detail)
	}
	if !strings.Contains(got.Detail, "2 project overrides") {
		t.Errorf("Detail = %q; want to contain 2 project overrides", got.Detail)
	}
}

func TestCheckDoctrineOverridesPerProjectHealthy_DaemonUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := newDoctrineClientForURL(t, srv.URL)
	got := checkDoctrineOverridesPerProjectHealthy(context.Background(), c)
	if got.Status != "fail" {
		t.Fatalf("Status = %q; want fail; detail=%q", got.Status, got.Detail)
	}
	if !strings.Contains(got.Hint, "zen doctrine") {
		t.Errorf("Hint = %q; want to mention zen doctrine", got.Hint)
	}
}

func TestCheckDoctrineReloadWatcherHealthy_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.DoctrineV2StatusResp{
			Active:         client.DoctrineV2ActiveResp{Name: "max-scope"},
			WatcherHealthy: true,
			LastReloadAt:   "2026-05-08T10:00:00Z",
			LastReloadOk:   true,
		})
	}))
	defer srv.Close()
	c := newDoctrineClientForURL(t, srv.URL)
	got := checkDoctrineReloadWatcherHealthy(context.Background(), c)
	if got.Status != "ok" {
		t.Fatalf("Status = %q; want ok; detail=%q", got.Status, got.Detail)
	}
	if !strings.Contains(got.Detail, "healthy") {
		t.Errorf("Detail = %q; want to contain healthy", got.Detail)
	}
}

func TestCheckDoctrineReloadWatcherHealthy_StalledWatcher(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.DoctrineV2StatusResp{
			Active:         client.DoctrineV2ActiveResp{Name: "max-scope"},
			WatcherHealthy: false,
		})
	}))
	defer srv.Close()
	c := newDoctrineClientForURL(t, srv.URL)
	got := checkDoctrineReloadWatcherHealthy(context.Background(), c)
	if got.Status != "fail" {
		t.Fatalf("Status = %q; want fail; detail=%q", got.Status, got.Detail)
	}
	if !strings.Contains(got.Hint, "zen doctrine reload") {
		t.Errorf("Hint = %q; want to mention zen doctrine reload", got.Hint)
	}
}

func TestCheckDoctrineReloadWatcherHealthy_LastReloadFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.DoctrineV2StatusResp{
			Active:         client.DoctrineV2ActiveResp{Name: "max-scope"},
			WatcherHealthy: true,
			LastReloadAt:   "2026-05-08T10:00:00Z",
			LastReloadOk:   false,
		})
	}))
	defer srv.Close()
	c := newDoctrineClientForURL(t, srv.URL)
	got := checkDoctrineReloadWatcherHealthy(context.Background(), c)
	if got.Status != "warn" {
		t.Fatalf("Status = %q; want warn; detail=%q", got.Status, got.Detail)
	}
}

func TestCheckDoctrineTelemetrySubscriberHealthy_HappyPath_NoReverts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		_ = json.NewEncoder(w).Encode(client.DoctrineV2HistoryResp{
			Events: []client.DoctrineV2HistoryEvent{
				{Type: "DoctrineLoaded", AtUnix: 100},
				{Type: "DoctrineReloaded", AtUnix: 200},
			},
		})
	}))
	defer srv.Close()
	c := newDoctrineClientForURL(t, srv.URL)
	got := checkDoctrineTelemetrySubscriberHealthy(context.Background(), c)
	if got.Status != "ok" {
		t.Fatalf("Status = %q; want ok; detail=%q", got.Status, got.Detail)
	}
	if !strings.Contains(got.Detail, "0 autonomous reverts") {
		t.Errorf("Detail = %q; want to contain 0 autonomous reverts", got.Detail)
	}
}

func TestCheckDoctrineTelemetrySubscriberHealthy_RecentReverts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.DoctrineV2HistoryResp{
			Events: []client.DoctrineV2HistoryEvent{
				{Type: "DoctrineLoaded", AtUnix: 100},
				{Type: "DoctrineAutonomousReverted", AtUnix: 200, Payload: map[string]any{"adr_id": "ADR-0050"}},
				{Type: "DoctrineAutonomousReverted", AtUnix: 300, Payload: map[string]any{"adr_id": "ADR-0051"}},
			},
		})
	}))
	defer srv.Close()
	c := newDoctrineClientForURL(t, srv.URL)
	got := checkDoctrineTelemetrySubscriberHealthy(context.Background(), c)
	if got.Status != "warn" {
		t.Fatalf("Status = %q; want warn; detail=%q", got.Status, got.Detail)
	}
	if !strings.Contains(got.Detail, "2 autonomous reverts") {
		t.Errorf("Detail = %q; want to contain 2 autonomous reverts", got.Detail)
	}
	if !strings.Contains(got.Hint, "zen doctrine history") {
		t.Errorf("Hint = %q; want to mention zen doctrine history", got.Hint)
	}
}

func TestCheckDoctrineTelemetrySubscriberHealthy_DaemonUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := newDoctrineClientForURL(t, srv.URL)
	got := checkDoctrineTelemetrySubscriberHealthy(context.Background(), c)
	if got.Status != "fail" {
		t.Fatalf("Status = %q; want fail; detail=%q", got.Status, got.Detail)
	}
}

func TestCheckDoctrineLintAnalyzersRegistered_HappyPath(t *testing.T) {
	got := checkDoctrineLintAnalyzersRegistered(context.Background(), nil)
	if got.Status != "ok" {
		t.Fatalf("Status = %q; want ok; detail=%q", got.Status, got.Detail)
	}
	if !strings.Contains(got.Detail, "3 analyzers") {
		t.Errorf("Detail = %q; want to contain '3 analyzers'", got.Detail)
	}
	for _, expected := range []string{"nostub", "nostore", "conventional"} {
		if !strings.Contains(strings.ToLower(got.Detail), expected) {
			t.Errorf("Detail = %q; want to contain analyzer %q", got.Detail, expected)
		}
	}
}

func TestCheckDoctrineSchemaMigrationStatus_HappyPath_AllCurrent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.DoctrineV2ListResp{
			Items: []client.DoctrineV2ListItem{
				{Name: "max-scope", Source: "embed", SchemaVersion: "1.0", DoctrineVersion: "1.0.0"},
				{Name: "default", Source: "embed", SchemaVersion: "1.0", DoctrineVersion: "1.0.0"},
				{Name: "capa-firewall", Source: "embed", SchemaVersion: "1.0", DoctrineVersion: "1.0.0"},
			},
		})
	}))
	defer srv.Close()
	c := newDoctrineClientForURL(t, srv.URL)
	got := checkDoctrineSchemaMigrationStatus(context.Background(), c)
	if got.Status != "ok" {
		t.Fatalf("Status = %q; want ok; detail=%q", got.Status, got.Detail)
	}
	if !strings.Contains(got.Detail, "no migration") {
		t.Errorf("Detail = %q; want to contain 'no migration'", got.Detail)
	}
}

func TestCheckDoctrineSchemaMigrationStatus_DeprecatedSchema(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.DoctrineV2ListResp{
			Items: []client.DoctrineV2ListItem{
				{Name: "max-scope", Source: "embed", SchemaVersion: "1.0", DoctrineVersion: "1.0.0"},
				{Name: "custom-conservative", Source: "user", SchemaVersion: "0.9", DoctrineVersion: "0.9.0"},
			},
		})
	}))
	defer srv.Close()
	c := newDoctrineClientForURL(t, srv.URL)
	got := checkDoctrineSchemaMigrationStatus(context.Background(), c)
	if got.Status != "warn" {
		t.Fatalf("Status = %q; want warn; detail=%q", got.Status, got.Detail)
	}
	if !strings.Contains(got.Hint, "zen doctrine migrate") {
		t.Errorf("Hint = %q; want to mention zen doctrine migrate", got.Hint)
	}
	if !strings.Contains(got.Detail, "custom-conservative") {
		t.Errorf("Detail = %q; want to contain custom-conservative", got.Detail)
	}
}

func TestCheckDoctrineSchemaMigrationStatus_DaemonUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := newDoctrineClientForURL(t, srv.URL)
	got := checkDoctrineSchemaMigrationStatus(context.Background(), c)
	if got.Status != "fail" {
		t.Fatalf("Status = %q; want fail; detail=%q", got.Status, got.Detail)
	}
}

func TestCheckDoctrineReinforcementTemplatesParse_HappyPath(t *testing.T) {
	got := checkDoctrineReinforcementTemplatesParse(context.Background(), nil)
	if got.Status != "ok" {
		t.Fatalf("Status = %q; want ok; detail=%q", got.Status, got.Detail)
	}
	if !strings.Contains(got.Detail, "3 templates") {
		t.Errorf("Detail = %q; want to contain '3 templates'", got.Detail)
	}
}

func TestCheckDoctrineAmendmentsPending_NoPending(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.DoctrineProposalList{
			Proposals: []client.DoctrineProposal{
				{ID: "ADR-0050", Status: "applied"},
				{ID: "ADR-0051", Status: "denied"},
			},
		})
	}))
	defer srv.Close()
	c := newDoctrineClientForURL(t, srv.URL)
	got := checkDoctrineAmendmentsPending(context.Background(), c)
	if got.Status != "ok" {
		t.Fatalf("Status = %q; want ok; detail=%q", got.Status, got.Detail)
	}
	if !strings.Contains(got.Detail, "0 pending") {
		t.Errorf("Detail = %q; want to contain 0 pending", got.Detail)
	}
}

func TestCheckDoctrineAmendmentsPending_HasPending(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.DoctrineProposalList{
			Proposals: []client.DoctrineProposal{
				{ID: "ADR-0050", Status: "proposed"},
				{ID: "ADR-0051", Status: "proposed"},
			},
		})
	}))
	defer srv.Close()
	c := newDoctrineClientForURL(t, srv.URL)
	got := checkDoctrineAmendmentsPending(context.Background(), c)
	if got.Status != "warn" {
		t.Fatalf("Status = %q; want warn; detail=%q", got.Status, got.Detail)
	}
	if !strings.Contains(got.Detail, "ADR-0050") {
		t.Errorf("Detail = %q; want to contain ADR-0050", got.Detail)
	}
	if !strings.Contains(got.Hint, "zen doctrine ack") {
		t.Errorf("Hint = %q; want to mention zen doctrine ack", got.Hint)
	}
}

func TestCheckDoctrineAmendmentsPending_DaemonUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := newDoctrineClientForURL(t, srv.URL)
	got := checkDoctrineAmendmentsPending(context.Background(), c)
	if got.Status != "fail" {
		t.Fatalf("Status = %q; want fail; detail=%q", got.Status, got.Detail)
	}
}

func TestCheckDoctrineEventsRecent_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.DoctrineV2HistoryResp{
			Events: []client.DoctrineV2HistoryEvent{
				{Type: "DoctrineLoaded", AtUnix: 100},
				{Type: "DoctrineReloaded", AtUnix: 200},
				{Type: "DoctrineAmendmentApplied", AtUnix: 300},
				{Type: "DoctrineLoaded", AtUnix: 400},
				{Type: "DoctrineLoaded", AtUnix: 500},
			},
		})
	}))
	defer srv.Close()
	c := newDoctrineClientForURL(t, srv.URL)
	got := checkDoctrineEventsRecent(context.Background(), c)
	if got.Status != "ok" {
		t.Fatalf("Status = %q; want ok; detail=%q", got.Status, got.Detail)
	}
	if !strings.Contains(got.Detail, "5 recent") {
		t.Errorf("Detail = %q; want to contain '5 recent'", got.Detail)
	}
	if !strings.Contains(got.Detail, "DoctrineLoaded") {
		t.Errorf("Detail = %q; want to contain DoctrineLoaded", got.Detail)
	}
}

func TestCheckDoctrineEventsRecent_NoEvents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.DoctrineV2HistoryResp{
			Events: []client.DoctrineV2HistoryEvent{},
		})
	}))
	defer srv.Close()
	c := newDoctrineClientForURL(t, srv.URL)
	got := checkDoctrineEventsRecent(context.Background(), c)
	if got.Status != "warn" {
		t.Fatalf("Status = %q; want warn; detail=%q", got.Status, got.Detail)
	}
}

func TestCheckDoctrineEventsRecent_DaemonUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := newDoctrineClientForURL(t, srv.URL)
	got := checkDoctrineEventsRecent(context.Background(), c)
	if got.Status != "fail" {
		t.Fatalf("Status = %q; want fail; detail=%q", got.Status, got.Detail)
	}
}

func TestCheckDoctrineTransverseAxiomsHardcoded_HappyPath(t *testing.T) {
	got := checkDoctrineTransverseAxiomsHardcoded(context.Background(), nil)
	if got.Status != "ok" {
		t.Fatalf("Status = %q; want ok; detail=%q", got.Status, got.Detail)
	}
	for _, expected := range []string{
		"no_tech_debt", "no_stubs", "build_final_product", "no_defer",
	} {
		if !strings.Contains(got.Detail, expected) {
			t.Errorf("Detail = %q; want to contain axiom %q", got.Detail, expected)
		}
	}
}

func TestRunDoctrineChecks_AggregatesAll11Checks(t *testing.T) {

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/doctrine/active":
			_ = json.NewEncoder(w).Encode(client.DoctrineV2ActiveResp{
				Name: "max-scope", SchemaVersion: "1.0", DoctrineVersion: "1.0.0", Source: "embed",
			})
		case "/v1/doctrine/status":
			_ = json.NewEncoder(w).Encode(client.DoctrineV2StatusResp{
				Active:         client.DoctrineV2ActiveResp{Name: "max-scope"},
				WatcherHealthy: true,
				LastReloadOk:   true,
			})
		case "/v1/doctrine/list":
			_ = json.NewEncoder(w).Encode(client.DoctrineV2ListResp{
				Items: []client.DoctrineV2ListItem{
					{Name: "max-scope", Source: "embed", SchemaVersion: "1.0", DoctrineVersion: "1.0.0"},
				},
			})
		case "/v1/doctrine/history":
			_ = json.NewEncoder(w).Encode(client.DoctrineV2HistoryResp{
				Events: []client.DoctrineV2HistoryEvent{
					{Type: "DoctrineLoaded", AtUnix: 100},
				},
			})
		case "/v1/doctrine/propose-list":
			_ = json.NewEncoder(w).Encode(client.DoctrineProposalList{
				Proposals: []client.DoctrineProposal{},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	c := newDoctrineClientForURL(t, srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	got := runDoctrineChecks(ctx, c)
	if len(got) != 11 {
		t.Fatalf("len(runDoctrineChecks()) = %d; want 11 (Plan 8 doctor template)", len(got))
	}
	expectedNames := []string{
		"doctrine.active.resolves",
		"doctrine.builtins.load",
		"doctrine.overrides.per-project.healthy",
		"doctrine.reload.watcher.healthy",
		"doctrine.telemetry.subscriber.healthy",
		"doctrine.lint.analyzers.registered",
		"doctrine.schema.migration.status",
		"doctrine.reinforcement.templates.parse",
		"doctrine.amendments.pending",
		"doctrine.events.recent",
		"doctrine.transverse.axioms.hardcoded",
	}
	for i, want := range expectedNames {
		if got[i].Name != want {
			t.Errorf("got[%d].Name = %q; want %q (canonical order)", i, got[i].Name, want)
		}
	}
}

func TestDoctorDoctrineCmd_RunsOneSection(t *testing.T) {

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	prev := TestOnlyClientFactory
	TestOnlyClientFactory = func(uds string) *client.Client {
		return client.NewWithBaseURL(srv.URL)
	}
	t.Cleanup(func() { TestOnlyClientFactory = prev })

	cmd := doctorDoctrineCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{})

	root := NewRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"--uds", "/tmp/zen-test.sock", "doctor", "doctrine"})
	_ = root.Execute()
	out := stdout.String()
	if !strings.Contains(out, "Doctrine") {
		t.Errorf("expected output to contain 'Doctrine' section header; got %q", out)
	}
}
