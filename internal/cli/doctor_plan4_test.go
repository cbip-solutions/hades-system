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

func invokeDoctorCmd(t *testing.T, args []string, baseURL string) (string, string, error) {
	t.Helper()
	prev := TestOnlyClientFactory
	TestOnlyClientFactory = func(uds string) *client.Client {
		return client.NewWithBaseURL(baseURL)
	}
	t.Cleanup(func() { TestOnlyClientFactory = prev })

	root := NewRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(append([]string{"--uds", "/tmp/zen-test.sock"}, args...))
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

func fullDaemonMock(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/health", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.HealthResponse{Status: "ok", Version: "v0.4.0", UptimeSeconds: 100})
	})
	mux.HandleFunc("/v1/workforce/gate/state", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.GateStateResp{State: "running", CanPause: true})
	})
	mux.HandleFunc("/v1/workforce/specs", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []client.WorkforceSpec{{ID: "spec_a"}}, "count": 1})
	})
	mux.HandleFunc("/v1/workforce/checkpoints", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []client.WorkforceCheckpoint{}})
	})
	mux.HandleFunc("/v1/workforce/fix_prompts", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []client.WorkforceFixPrompt{}})
	})
	mux.HandleFunc("/v1/research/cache/stats", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.ResearchCacheStats{TotalEntries: 5, TotalBytes: 1000})
	})
	mux.HandleFunc("/v1/budget/events", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"events": []client.BudgetEvent{}, "count": 0})
	})
	mux.HandleFunc("/v1/budget/cap_status", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.BudgetCapStatus{RemainingUSD: 100})
	})
	mux.HandleFunc("/v1/audit/events", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []client.AuditEvent{}})
	})
	mux.HandleFunc("/v1/doctrine/state", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"name": "max-scope", "schema_version": 1})
	})

	mux.HandleFunc("/v1/bypass/doctor", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.BypassDoctorResp{Status: "ok", Detail: "test mock"})
	})
	return httptest.NewServer(mux)
}

func TestDoctor_Workforce(t *testing.T) {
	srv := fullDaemonMock(t)
	defer srv.Close()
	stdout, _, _ := invokeDoctorCmd(t, []string{"doctor", "workforce"}, srv.URL)
	for _, want := range []string{"workforce.gate.reachable", "workforce.queue.depths", "workforce.specs.loaded"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in %s", want, stdout)
		}
	}
}

func TestDoctor_Research(t *testing.T) {
	srv := fullDaemonMock(t)
	defer srv.Close()
	stdout, _, _ := invokeDoctorCmd(t, []string{"doctor", "research"}, srv.URL)
	for _, want := range []string{"research.cache.reachable", "research.cache.size"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in %s", want, stdout)
		}
	}
}

func TestDoctor_Budget(t *testing.T) {
	srv := fullDaemonMock(t)
	defer srv.Close()
	stdout, _, _ := invokeDoctorCmd(t, []string{"doctor", "budget"}, srv.URL)
	for _, want := range []string{"budget.events.reachable", "budget.cap_status.reachable"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in %s", want, stdout)
		}
	}
}

func TestDoctor_Audit(t *testing.T) {
	srv := fullDaemonMock(t)
	defer srv.Close()
	stdout, _, _ := invokeDoctorCmd(t, []string{"doctor", "audit"}, srv.URL)
	for _, want := range []string{"audit.events.reachable", "audit.family_disjoint.size", "audit.criteria.loaded"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in %s", want, stdout)
		}
	}
}

func TestDoctor_SSHExec(t *testing.T) {
	srv := fullDaemonMock(t)
	defer srv.Close()
	stdout, _, _ := invokeDoctorCmd(t, []string{"doctor", "sshexec"}, srv.URL)
	for _, want := range []string{"sshexec.allowlist.resolves", "sshexec.audit.events"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in %s", want, stdout)
		}
	}
}

func TestDoctor_Doctrine(t *testing.T) {
	srv := fullDaemonMock(t)
	defer srv.Close()
	stdout, _, _ := invokeDoctorCmd(t, []string{"doctor", "doctrine"}, srv.URL)
	for _, want := range []string{"doctrine.active.resolves", "doctrine.builtins.load"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in %s", want, stdout)
		}
	}
}

func TestDoctor_MCPs(t *testing.T) {
	srv := fullDaemonMock(t)
	defer srv.Close()
	stdout, _, _ := invokeDoctorCmd(t, []string{"doctor", "mcps"}, srv.URL)
	for _, want := range []string{"mcp.zen-mcp-research", "mcp.zen-mcp-budget", "mcp.zen-mcp-audit", "mcp.zen-mcp-sshexec"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in %s", want, stdout)
		}
	}
}

func TestDoctor_Caronte(t *testing.T) {
	srv := fullDaemonMock(t)
	defer srv.Close()
	stdout, _, _ := invokeDoctorCmd(t, []string{"doctor", "caronte"}, srv.URL)

	expectedChecks := []string{
		"caronte.engine.healthy",
		"caronte.index.freshness",
		"caronte.language.coverage",
		"caronte.project-db.status",
	}
	for _, check := range expectedChecks {
		if !strings.Contains(stdout, check) {
			t.Errorf("missing check %q in doctor caronte output:\n%s", check, stdout)
		}
	}
}

func TestDoctor_AggregateRunsAllSections(t *testing.T) {
	srv := fullDaemonMock(t)
	defer srv.Close()
	stdout, _, _ := invokeDoctorCmd(t, []string{"doctor"}, srv.URL)
	for _, sec := range []string{"Environment", "Daemon", "Workforce", "Research", "Budget", "Audit", "SSH-Exec", "Doctrine", "MCPs", "Caronte"} {
		if !strings.Contains(stdout, sec) {
			t.Errorf("missing section %q in aggregate output", sec)
		}
	}
}

func TestDoctor_HasAllSubcommands(t *testing.T) {
	root := NewDoctorCmd()
	want := []string{"workforce", "research", "budget", "audit", "sshexec", "doctrine", "mcps", "caronte"}
	have := map[string]bool{}
	for _, c := range root.Commands() {
		have[c.Name()] = true
	}
	for _, w := range want {
		if !have[w] {
			t.Errorf("missing subcommand: doctor %s", w)
		}
	}
	if len(want) != 8 {
		t.Errorf("expected 8 subcommands, got %d", len(want))
	}
}

func failingDaemonMock(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	failHandler := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"server-side fail"}`))
	}
	for _, path := range []string{
		"/v1/health",
		"/v1/workforce/gate/state",
		"/v1/workforce/specs",
		"/v1/workforce/checkpoints",
		"/v1/workforce/fix_prompts",
		"/v1/research/cache/stats",
		"/v1/budget/events",
		"/v1/budget/cap_status",
		"/v1/audit/events",
		"/v1/doctrine/state",
		"/v1/bypass/doctor",
	} {
		mux.HandleFunc(path, failHandler)
	}
	return httptest.NewServer(mux)
}

func warnDaemonMock(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/health", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.HealthResponse{Status: "ok", Version: "v0.4.0", UptimeSeconds: 100})
	})

	mux.HandleFunc("/v1/workforce/gate/state", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.GateStateResp{State: "paused_descriptive", CanPause: false})
	})

	mux.HandleFunc("/v1/workforce/specs", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []client.WorkforceSpec{}, "count": 0})
	})

	cps := make([]client.WorkforceCheckpoint, 200)
	mux.HandleFunc("/v1/workforce/checkpoints", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"items": cps})
	})

	fps := make([]client.WorkforceFixPrompt, 60)
	mux.HandleFunc("/v1/workforce/fix_prompts", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"items": fps})
	})

	mux.HandleFunc("/v1/research/cache/stats", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.ResearchCacheStats{
			TotalEntries: 100,
			TotalBytes:   600 * 1024 * 1024,
			ExpiredCount: 60,
		})
	})
	mux.HandleFunc("/v1/budget/events", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"events": []client.BudgetEvent{}})
	})
	mux.HandleFunc("/v1/budget/cap_status", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.BudgetCapStatus{RemainingUSD: 0, Blocked: true, BlockedScope: "doctrine"})
	})
	mux.HandleFunc("/v1/audit/events", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []client.AuditEvent{}})
	})
	mux.HandleFunc("/v1/doctrine/state", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"name": "default", "schema_version": 1})
	})
	mux.HandleFunc("/v1/bypass/doctor", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.BypassDoctorResp{Status: "warn", Detail: "expiring soon"})
	})
	return httptest.NewServer(mux)
}

func TestDoctor_Workforce_FailingDaemon(t *testing.T) {
	srv := failingDaemonMock(t)
	defer srv.Close()
	stdout, _, _ := invokeDoctorCmd(t, []string{"doctor", "workforce"}, srv.URL)
	if !strings.Contains(stdout, "fail") && !strings.Contains(stdout, "warn") {
		t.Errorf("expected fail/warn signals: %s", stdout)
	}
}

func TestDoctor_Research_FailingDaemon(t *testing.T) {
	srv := failingDaemonMock(t)
	defer srv.Close()
	stdout, _, _ := invokeDoctorCmd(t, []string{"doctor", "research"}, srv.URL)
	if !strings.Contains(stdout, "fail") {
		t.Errorf("expected fail signal: %s", stdout)
	}
}

func TestDoctor_Budget_FailingDaemon(t *testing.T) {
	srv := failingDaemonMock(t)
	defer srv.Close()
	stdout, _, _ := invokeDoctorCmd(t, []string{"doctor", "budget"}, srv.URL)
	if !strings.Contains(stdout, "fail") {
		t.Errorf("expected fail signal: %s", stdout)
	}
}

func TestDoctor_Audit_FailingDaemon(t *testing.T) {
	srv := failingDaemonMock(t)
	defer srv.Close()
	stdout, _, _ := invokeDoctorCmd(t, []string{"doctor", "audit"}, srv.URL)
	if !strings.Contains(stdout, "fail") {
		t.Errorf("expected fail signal: %s", stdout)
	}
}

func TestDoctor_Doctrine_FailingDaemon(t *testing.T) {
	srv := failingDaemonMock(t)
	defer srv.Close()
	stdout, _, _ := invokeDoctorCmd(t, []string{"doctor", "doctrine"}, srv.URL)
	if !strings.Contains(stdout, "doctrine") {
		t.Errorf("expected output: %s", stdout)
	}
}

func TestDoctor_SSHExec_FailingDaemon(t *testing.T) {
	srv := failingDaemonMock(t)
	defer srv.Close()
	stdout, _, _ := invokeDoctorCmd(t, []string{"doctor", "sshexec"}, srv.URL)
	if !strings.Contains(stdout, "sshexec") {
		t.Errorf("expected output: %s", stdout)
	}
}

func TestDoctor_Warns(t *testing.T) {
	srv := warnDaemonMock(t)
	defer srv.Close()
	stdout, _, _ := invokeDoctorCmd(t, []string{"doctor"}, srv.URL)
	// At least one warn signal MUST appear (any of the 4 above warn-
	// triggering checks).
	if !strings.Contains(stdout, "warn") {
		t.Errorf("expected warn signals: %s", stdout)
	}
}
