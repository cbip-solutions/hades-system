package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/client"
)

func TestRunBypassChecks_AllOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","detail":"verified"}`))
	}))
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	results := runBypassChecks(context.Background(), c)
	if len(results) != 10 {
		t.Fatalf("want 10 checks, got %d", len(results))
	}
	for _, r := range results {
		if r.Status != "ok" {
			t.Errorf("check %s: status=%s detail=%s", r.Name, r.Status, r.Detail)
		}
	}
}

func TestRunBypassChecks_FailsCascadeHints(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		check := r.URL.Query().Get("check")
		if check == "credentials.fresh" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"warn","detail":"expires in 4min"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","detail":"verified"}`))
	}))
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	results := runBypassChecks(context.Background(), c)
	for _, r := range results {
		if r.Name == "bypass.credentials.fresh" {
			if r.Status != "warn" {
				t.Errorf("expected warn, got %s", r.Status)
			}
			if !strings.Contains(r.Hint, "refresh-now") {
				t.Errorf("hint missing refresh-now: %s", r.Hint)
			}
			return
		}
	}
	t.Fatal("credentials.fresh check not found")
}

func TestCheckNamesMatchSpec(t *testing.T) {
	want := []string{
		"bypass.credentials.readable",
		"bypass.credentials.fresh",
		"bypass.keychain.accessible",
		"bypass.config.valid",
		"bypass.config.fresh",
		"bypass.cf-range.fresh",
		"bypass.cert.valid",
		"bypass.connectivity",
		"bypass.private config repo.repo-reachable",
		"bypass.tools.mitmproxy-available",
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","detail":"x"}`))
	}))
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	results := runBypassChecks(context.Background(), c)
	if len(results) != len(want) {
		t.Fatalf("want %d, got %d", len(want), len(results))
	}
	for i, r := range results {
		if r.Name != want[i] {
			t.Errorf("check[%d] name = %q, want %q", i, r.Name, want[i])
		}
	}
}

func TestRenderCheckIncludesHintOnNonOK(t *testing.T) {
	r := CheckResult{Name: "x.y", Status: "warn", Detail: "d", Hint: "fix it"}
	out := renderCheck(r)
	if !strings.Contains(out, "fix it") {
		t.Errorf("missing hint in: %q", out)
	}
}

func TestRunOrchestratorChecks_Count(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/orchestrator/status":
			_, _ = w.Write([]byte(`{"tiers":[],"pins":[],"costs":[]}`))
		case "/v1/orchestrator/pins":
			_, _ = w.Write([]byte(`{"pins":[]}`))
		case "/v1/budget":
			_, _ = w.Write([]byte(`{"range":"24h","total_usd":0,"by_tier":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	results := runOrchestratorChecks(context.Background(), c)
	if len(results) != 4 {
		t.Fatalf("want 4 orchestrator checks, got %d", len(results))
	}
}

func TestRunOrchestratorChecks_Names(t *testing.T) {
	want := []string{
		"orchestrator.daemon-route-reachable",
		"orchestrator.tier-states-clean",
		"orchestrator.pin-overrides-reachable",
		"orchestrator.budget-reachable",
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/orchestrator/status":
			_, _ = w.Write([]byte(`{"tiers":[],"pins":[],"costs":[]}`))
		case "/v1/orchestrator/pins":
			_, _ = w.Write([]byte(`{"pins":[]}`))
		case "/v1/budget":
			_, _ = w.Write([]byte(`{"range":"24h","total_usd":0,"by_tier":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	results := runOrchestratorChecks(context.Background(), c)
	if len(results) != len(want) {
		t.Fatalf("want %d checks, got %d", len(want), len(results))
	}
	for i, r := range results {
		if r.Name != want[i] {
			t.Errorf("check[%d] name = %q, want %q", i, r.Name, want[i])
		}
	}
}

func TestRunOrchestratorChecks_AllOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/orchestrator/status":
			_, _ = w.Write([]byte(`{"tiers":[{"tier":"tier1","state":"closed"}],"pins":[],"costs":[]}`))
		case "/v1/orchestrator/pins":
			_, _ = w.Write([]byte(`{"pins":[]}`))
		case "/v1/budget":
			_, _ = w.Write([]byte(`{"range":"24h","total_usd":0,"by_tier":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	results := runOrchestratorChecks(context.Background(), c)
	for _, r := range results {
		if r.Status != "ok" {
			t.Errorf("check %s: want ok, got %s (detail: %s)", r.Name, r.Status, r.Detail)
		}
	}
}

func TestCheckOrchestratorReachable_Fail(t *testing.T) {

	c := client.NewWithBaseURL("http://127.0.0.1:1")
	r := checkOrchestratorReachable(context.Background(), c)
	if r.Name != "orchestrator.daemon-route-reachable" {
		t.Errorf("wrong name: %q", r.Name)
	}
	if r.Status != "fail" {
		t.Errorf("want fail, got %s", r.Status)
	}
	if !strings.Contains(r.Hint, "zen daemon start") {
		t.Errorf("hint missing daemon-start hint: %q", r.Hint)
	}
}

func TestCheckTierStatesClean_Fail_NoServer(t *testing.T) {
	c := client.NewWithBaseURL("http://127.0.0.1:1")
	r := checkTierStatesClean(context.Background(), c)
	if r.Name != "orchestrator.tier-states-clean" {
		t.Errorf("wrong name: %q", r.Name)
	}
	if r.Status != "fail" {
		t.Errorf("want fail when server unreachable, got %s", r.Status)
	}
	if !strings.Contains(r.Hint, "zen orchestrator status") {
		t.Errorf("hint missing zen orchestrator status: %q", r.Hint)
	}
}

func TestCheckTierStatesClean_Warn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tiers":[{"tier":"tier1","state":"open"},{"tier":"tier2","state":"closed"}],"pins":[],"costs":[]}`))
	}))
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	r := checkTierStatesClean(context.Background(), c)
	if r.Status != "warn" {
		t.Errorf("want warn for open tier, got %s", r.Status)
	}
	if !strings.Contains(r.Detail, "tier1=open") {
		t.Errorf("detail missing non-closed tier info: %q", r.Detail)
	}
}

func TestCheckTierStatesClean_AllClosed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tiers":[{"tier":"tier1","state":"closed"},{"tier":"tier2","state":"closed"}],"pins":[],"costs":[]}`))
	}))
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	r := checkTierStatesClean(context.Background(), c)
	if r.Status != "ok" {
		t.Errorf("want ok when all closed, got %s (detail: %s)", r.Status, r.Detail)
	}
}

func TestCheckPinOverridesReachable_Fail(t *testing.T) {
	c := client.NewWithBaseURL("http://127.0.0.1:1")
	r := checkPinOverridesReachable(context.Background(), c)
	if r.Name != "orchestrator.pin-overrides-reachable" {
		t.Errorf("wrong name: %q", r.Name)
	}
	if r.Status != "fail" {
		t.Errorf("want fail, got %s", r.Status)
	}
}

func TestCheckBudgetReachable_Fail(t *testing.T) {
	c := client.NewWithBaseURL("http://127.0.0.1:1")
	r := checkBudgetReachable(context.Background(), c)
	if r.Name != "orchestrator.budget-reachable" {
		t.Errorf("wrong name: %q", r.Name)
	}
	if r.Status != "fail" {
		t.Errorf("want fail, got %s", r.Status)
	}
}

func TestDoctorCmd_OrchestratorSectionRendered(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/health":
			_, _ = w.Write([]byte(`{"status":"ok","version":"v0.3.0","uptime_seconds":42}`))
		case "/v1/bypass/doctor":
			_, _ = w.Write([]byte(`{"status":"ok","detail":"ok"}`))
		case "/v1/orchestrator/status":
			_, _ = w.Write([]byte(`{"tiers":[],"pins":[],"costs":[]}`))
		case "/v1/orchestrator/pins":
			_, _ = w.Write([]byte(`{"pins":[]}`))
		case "/v1/budget":
			_, _ = w.Write([]byte(`{"range":"24h","total_usd":0,"by_tier":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cmd := NewDoctorCmd()
	cmd.Flags().String("uds", "", "")
	buf := &strings.Builder{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	c := client.NewWithBaseURL(srv.URL)
	results := runOrchestratorChecks(context.Background(), c)
	out := strings.Builder{}
	for _, r := range results {
		out.WriteString(renderCheck(r))
		out.WriteString("\n")
	}
	rendered := out.String()
	if !strings.Contains(rendered, "orchestrator.daemon-route-reachable") {
		t.Errorf("missing orchestrator.daemon-route-reachable in rendered output: %q", rendered)
	}
	if !strings.Contains(rendered, "orchestrator.budget-reachable") {
		t.Errorf("missing orchestrator.budget-reachable in rendered output: %q", rendered)
	}
	_ = cmd
}

func TestDoctor_JSONOutput(t *testing.T) {

	srv := newDoctorTestServer(t)
	defer srv.Close()

	prev := TestOnlyClientFactory
	TestOnlyClientFactory = func(uds string) *client.Client {
		return client.NewWithBaseURL(srv.URL)
	}
	t.Cleanup(func() { TestOnlyClientFactory = prev })

	cmd := NewDoctorCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--json"})
	err := cmd.Execute()

	_ = err

	out := stdout.String()
	var arr []map[string]any
	if jerr := json.Unmarshal([]byte(out), &arr); jerr != nil {
		t.Fatalf("--json output not valid JSON: %v\n%s", jerr, out)
	}
	if len(arr) == 0 {
		t.Fatal("expected non-empty results slice")
	}

	for i, m := range arr {
		for _, want := range []string{"name", "status"} {
			if _, ok := m[want]; !ok {
				t.Errorf("[%d] missing %q in %+v", i, want, m)
			}
		}
	}

	hasSection := false
	for _, m := range arr {
		if s, ok := m["section"].(string); ok && s != "" {
			hasSection = true
			break
		}
	}
	if !hasSection {
		t.Error("expected at least one entry with non-empty 'section'")
	}
}

func TestDoctor_FilterStatus(t *testing.T) {
	srv := newDoctorTestServer(t)
	defer srv.Close()
	prev := TestOnlyClientFactory
	TestOnlyClientFactory = func(uds string) *client.Client {
		return client.NewWithBaseURL(srv.URL)
	}
	t.Cleanup(func() { TestOnlyClientFactory = prev })

	cmd := NewDoctorCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)
	cmd.SetArgs([]string{"--json", "--filter", "status=ok"})
	_ = cmd.Execute()

	out := stdout.String()
	var arr []map[string]any
	if jerr := json.Unmarshal([]byte(out), &arr); jerr != nil {
		t.Fatalf("--json output not valid JSON: %v\n%s", jerr, out)
	}
	for i, m := range arr {
		if m["status"] != "ok" {
			t.Errorf("[%d] filter status=ok leaked %+v", i, m)
		}
	}
}

func TestDoctor_FilterAllColumns(t *testing.T) {
	srv := newDoctorTestServer(t)
	defer srv.Close()
	prev := TestOnlyClientFactory
	TestOnlyClientFactory = func(uds string) *client.Client {
		return client.NewWithBaseURL(srv.URL)
	}
	t.Cleanup(func() { TestOnlyClientFactory = prev })

	for _, col := range []string{"section", "name", "status", "detail", "hint"} {
		col := col
		t.Run(col, func(t *testing.T) {
			cmd := NewDoctorCmd()
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
			var stdout bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stdout)

			cmd.SetArgs([]string{"--json", "--filter", col + "~.*"})
			_ = cmd.Execute()

			out := stdout.String()
			var arr []map[string]any
			if err := json.Unmarshal([]byte(out), &arr); err != nil {
				t.Fatalf("--filter %s~.* not valid JSON: %v\n%s", col, err, out)
			}
			if len(arr) == 0 {
				t.Errorf("--filter %s~.* should not exclude all rows", col)
			}
		})
	}
}

func TestDoctor_QuietSuppressesPrelude(t *testing.T) {
	srv := newDoctorTestServer(t)
	defer srv.Close()
	prev := TestOnlyClientFactory
	TestOnlyClientFactory = func(uds string) *client.Client {
		return client.NewWithBaseURL(srv.URL)
	}
	t.Cleanup(func() { TestOnlyClientFactory = prev })

	cmd := NewDoctorCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)
	cmd.SetArgs([]string{"--quiet"})
	_ = cmd.Execute()
	out := stdout.String()
	if strings.Contains(out, "zen-swarm doctor") {
		t.Errorf("--quiet should suppress prelude: %s", out)
	}
	if strings.Contains(out, "Implementation status") {
		t.Errorf("--quiet should suppress footer: %s", out)
	}
}

func newDoctorTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","version":"test","uptime_seconds":42}`))
	})
	mux.HandleFunc("/v1/bypass/doctor", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","detail":"verified"}`))
	})
	mux.HandleFunc("/v1/workforce/gate/state", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"state":"running","can_pause":true,"can_resume":false}`))
	})
	mux.HandleFunc("/v1/workforce/checkpoints", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"items":[],"count":0}`))
	})
	mux.HandleFunc("/v1/workforce/specs", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"items":[],"count":0}`))
	})
	mux.HandleFunc("/v1/workforce/fix_prompts", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"items":[],"count":0}`))
	})
	mux.HandleFunc("/v1/research/cache/stats", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"total_entries":0,"total_bytes":0}`))
	})
	mux.HandleFunc("/v1/research/cache/list", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"items":[],"count":0}`))
	})
	mux.HandleFunc("/v1/budget/events", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"items":[],"count":0}`))
	})
	mux.HandleFunc("/v1/budget/cap_status", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"remaining_usd":42,"blocked":false}`))
	})
	mux.HandleFunc("/v1/audit/events", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"items":[],"count":0}`))
	})
	mux.HandleFunc("/v1/audit/types", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"items":[],"count":0}`))
	})
	mux.HandleFunc("/v1/doctrine/state", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"name":"max-scope"}`))
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	})
	return httptest.NewServer(mux)
}

func TestDoctorAggregateContainsAllPlan11Sections(t *testing.T) {
	t.Parallel()
	root := NewDoctorCmd()
	wantSubcommands := []string{
		"hermes", "augment", "citation", "coordination",
	}
	for _, sub := range wantSubcommands {
		got, _, err := root.Find([]string{sub})
		if err != nil {
			t.Errorf("subcommand %q not registered under zen doctor: %v", sub, err)
			continue
		}
		if got == nil {
			t.Errorf("zen doctor %s: command nil", sub)
		}
	}
}
