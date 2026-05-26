package cli

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/client"
)

type doctorRoutes map[string]http.HandlerFunc

func withRoute(path string, h http.HandlerFunc) func(doctorRoutes) {
	return func(routes doctorRoutes) { routes[path] = h }
}

func newFakeDoctorDaemon(t *testing.T, sess client.SessionInfo, pool client.PoolStatus, sn client.SafetynetStatus, opts ...func(doctorRoutes)) *httptest.Server {
	t.Helper()
	routes := doctorRoutes{
		"/v1/orchestrator/state": func(w http.ResponseWriter, _ *http.Request) {
			writeJSONP5(w, sess)
		},
		"/v1/orchestrator/pool": func(w http.ResponseWriter, _ *http.Request) {
			writeJSONP5(w, pool)
		},
		"/v1/safetynet/status": func(w http.ResponseWriter, _ *http.Request) {
			writeJSONP5(w, sn)
		},
		"/v1/orchestrator/health/event_log_writable": func(w http.ResponseWriter, _ *http.Request) {
			writeJSONP5(w, map[string]any{"writable": true, "corruption_count": 0})
		},
		"/v1/orchestrator/health/research_mcp_up": func(w http.ResponseWriter, _ *http.Request) {
			writeJSONP5(w, map[string]bool{"up": true})
		},
		"/v1/orchestrator/health/caronte_up": func(w http.ResponseWriter, _ *http.Request) {
			writeJSONP5(w, map[string]any{"up": true, "index_currency_hours": 3})
		},
		"/v1/orchestrator/health/adapters_clean": func(w http.ResponseWriter, _ *http.Request) {
			writeJSONP5(w, map[string]bool{"clean": true})
		},
		"/v1/orchestrator/health/last_session_clean": func(w http.ResponseWriter, _ *http.Request) {
			writeJSONP5(w, map[string]bool{"clean": true})
		},
	}
	for _, opt := range opts {
		opt(routes)
	}
	mux := http.NewServeMux()
	for p, h := range routes {
		mux.HandleFunc(p, h)
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestDoctor_NamesAreExpected(t *testing.T) {
	srv := newFakeDoctorDaemon(t, client.SessionInfo{BackgroundGoroutines: 11}, client.PoolStatus{HealthOK: true}, client.SafetynetStatus{SubstratePassRate7d: 0.99})
	checks := newOrchestratorPlan5DoctorChecks(srv.URL)
	want := []string{
		"orchestrator.daemon_up",
		"orchestrator.event_log_writable",
		"orchestrator.worktree_pool_healthy",
		"orchestrator.research_mcp_up",
		"orchestrator.caronte_up",
		"orchestrator.adapters_clean",
		"orchestrator.background_goroutines",
		"orchestrator.last_session_clean",
		"orchestrator.substrate_health",
	}
	if len(checks) != len(want) {
		t.Fatalf("expected %d checks, got %d", len(want), len(checks))
	}
	for i, c := range checks {
		if c.Name() != want[i] {
			t.Errorf("check[%d]: got %q want %q", i, c.Name(), want[i])
		}
	}
}

func TestDoctor_AllChecksPass(t *testing.T) {
	srv := newFakeDoctorDaemon(t,
		client.SessionInfo{State: "RUNNING", BackgroundGoroutines: 11},
		client.PoolStatus{Floor: 8, CurrentLeased: 2, OrphansCleaned: 0, HealthOK: true},
		client.SafetynetStatus{SubstratePassRate7d: 0.997, DriftIncidents24h: 0})
	checks := newOrchestratorPlan5DoctorChecks(srv.URL)
	for _, c := range checks {
		res := c.Run(context.Background())
		if !res.Pass {
			t.Errorf("check %s should pass; detail=%s", c.Name(), res.Detail)
		}
	}
}

func TestDoctor_DaemonUp_OverUnixSocket(t *testing.T) {

	f, err := os.CreateTemp("/tmp", "zt-doctor-*.sock")
	if err != nil {
		t.Fatalf("temp sock: %v", err)
	}
	sock := f.Name()
	_ = f.Close()
	_ = os.Remove(sock)
	t.Cleanup(func() { _ = os.Remove(sock) })
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/orchestrator/state", func(w http.ResponseWriter, _ *http.Request) {
		writeJSONP5(w, client.SessionInfo{State: "RUNNING", BackgroundGoroutines: 11})
	})
	srv := httptest.NewUnstartedServer(mux)
	_ = srv.Listener.Close()
	srv.Listener = ln
	srv.Start()
	t.Cleanup(srv.Close)

	checks := newOrchestratorPlan5DoctorChecks("http+unix://" + sock)
	var daemonUp *orchestratorCheckP5
	for i := range checks {
		if checks[i].Name() == "orchestrator.daemon_up" {
			daemonUp = &checks[i]
			break
		}
	}
	if daemonUp == nil {
		t.Fatal("orchestrator.daemon_up check not found")
	}
	res := daemonUp.Run(context.Background())
	if !res.Pass {
		t.Fatalf("daemon_up should pass over a http+unix:// socket; got fail: %s", res.Detail)
	}
}

func TestDoctor_BackgroundGoroutines_FailsOnDeviation(t *testing.T) {
	srv := newFakeDoctorDaemon(t,
		client.SessionInfo{State: "RUNNING", BackgroundGoroutines: 7},
		client.PoolStatus{Floor: 8, HealthOK: true},
		client.SafetynetStatus{SubstratePassRate7d: 0.99})
	checks := newOrchestratorPlan5DoctorChecks(srv.URL)
	for _, c := range checks {
		if c.Name() == "orchestrator.background_goroutines" {
			res := c.Run(context.Background())
			if res.Pass {
				t.Fatal("background_goroutines should fail when goroutines != 11")
			}
			if !strings.Contains(res.Detail, "11") || !strings.Contains(res.Detail, "7") {
				t.Errorf("detail should mention expected/got: %s", res.Detail)
			}
			return
		}
	}
	t.Fatal("orchestrator.background_goroutines check not found")
}

func TestDoctor_PoolHealth_FailsOnUnhealthy(t *testing.T) {
	srv := newFakeDoctorDaemon(t,
		client.SessionInfo{BackgroundGoroutines: 11},
		client.PoolStatus{Floor: 8, HealthOK: false, OrphansCleaned: 0},
		client.SafetynetStatus{})
	checks := newOrchestratorPlan5DoctorChecks(srv.URL)
	for _, c := range checks {
		if c.Name() == "orchestrator.worktree_pool_healthy" {
			res := c.Run(context.Background())
			if res.Pass {
				t.Fatal("pool health should fail when HealthOK=false")
			}
			return
		}
	}
}

func TestDoctor_PoolHealth_FailsOnOverLease(t *testing.T) {
	srv := newFakeDoctorDaemon(t,
		client.SessionInfo{BackgroundGoroutines: 11},
		client.PoolStatus{Floor: 8, Maximum: 10, CurrentLeased: 8, ElasticInUse: 5, HealthOK: true},
		client.SafetynetStatus{})
	checks := newOrchestratorPlan5DoctorChecks(srv.URL)
	for _, c := range checks {
		if c.Name() == "orchestrator.worktree_pool_healthy" {
			res := c.Run(context.Background())
			if res.Pass {
				t.Fatal("pool health should fail on over-lease")
			}
			if !strings.Contains(res.Detail, "over-leased") {
				t.Errorf("detail should mention over-leased: %s", res.Detail)
			}
			return
		}
	}
}

func TestDoctor_SubstrateHealth_FailsBelowThreshold(t *testing.T) {
	srv := newFakeDoctorDaemon(t,
		client.SessionInfo{BackgroundGoroutines: 11},
		client.PoolStatus{HealthOK: true},
		client.SafetynetStatus{SubstratePassRate7d: 0.85, DriftIncidents24h: 0})
	checks := newOrchestratorPlan5DoctorChecks(srv.URL)
	for _, c := range checks {
		if c.Name() == "orchestrator.substrate_health" {
			res := c.Run(context.Background())
			if res.Pass {
				t.Fatal("substrate_health should fail below 95% threshold")
			}
			return
		}
	}
}

func TestDoctor_SubstrateHealth_WarnsOnDrift(t *testing.T) {
	srv := newFakeDoctorDaemon(t,
		client.SessionInfo{BackgroundGoroutines: 11},
		client.PoolStatus{HealthOK: true},
		client.SafetynetStatus{SubstratePassRate7d: 0.99, DriftIncidents24h: 3})
	checks := newOrchestratorPlan5DoctorChecks(srv.URL)
	for _, c := range checks {
		if c.Name() == "orchestrator.substrate_health" {
			res := c.Run(context.Background())
			if res.Pass {
				t.Fatal("substrate_health should not pass when DriftIncidents24h>0")
			}
			if !res.Warning {
				t.Error("substrate_health should set Warning=true when only drift>0 (not below threshold)")
			}
			return
		}
	}
}

func TestDoctor_EventLogWritable_HighCorruptionFails(t *testing.T) {
	srv := newFakeDoctorDaemon(t,
		client.SessionInfo{BackgroundGoroutines: 11},
		client.PoolStatus{HealthOK: true},
		client.SafetynetStatus{SubstratePassRate7d: 0.99},
		withRoute("/v1/orchestrator/health/event_log_writable", func(w http.ResponseWriter, _ *http.Request) {
			writeJSONP5(w, map[string]any{"writable": true, "corruption_count": 7})
		}),
	)
	checks := newOrchestratorPlan5DoctorChecks(srv.URL)
	for _, c := range checks {
		if c.Name() == "orchestrator.event_log_writable" {
			res := c.Run(context.Background())
			if res.Pass {
				t.Fatal("event_log_writable should fail when corruption_count>=5")
			}
			if !strings.Contains(res.Detail, "HARD_PAUSE") {
				t.Errorf("detail should mention HARD_PAUSE: %s", res.Detail)
			}
			return
		}
	}
}

func TestDoctor_ResearchMCP_DownFails(t *testing.T) {
	srv := newFakeDoctorDaemon(t,
		client.SessionInfo{BackgroundGoroutines: 11},
		client.PoolStatus{HealthOK: true},
		client.SafetynetStatus{SubstratePassRate7d: 0.99},
		withRoute("/v1/orchestrator/health/research_mcp_up", func(w http.ResponseWriter, _ *http.Request) {
			writeJSONP5(w, map[string]bool{"up": false})
		}),
	)

	checks := newOrchestratorPlan5DoctorChecks(srv.URL)
	for _, c := range checks {
		if c.Name() == "orchestrator.research_mcp_up" {
			res := c.Run(context.Background())
			if res.Pass {
				t.Fatal("research_mcp_up should fail when MCP reports down")
			}
			return
		}
	}
}

func TestDoctor_DaemonUp_FailsOnUnreachable(t *testing.T) {
	checks := newOrchestratorPlan5DoctorChecks("http://127.0.0.1:1")
	for _, c := range checks {
		if c.Name() == "orchestrator.daemon_up" {
			res := c.Run(context.Background())
			if res.Pass {
				t.Fatal("daemon_up should fail when unreachable")
			}
			return
		}
	}
}

func TestDoctor_CaronteIndexCurrencyExceeded(t *testing.T) {
	srv := newFakeDoctorDaemon(t,
		client.SessionInfo{BackgroundGoroutines: 11},
		client.PoolStatus{HealthOK: true},
		client.SafetynetStatus{SubstratePassRate7d: 0.99},
		withRoute("/v1/orchestrator/health/caronte_up", func(w http.ResponseWriter, _ *http.Request) {
			writeJSONP5(w, map[string]any{"up": true, "index_currency_hours": 30})
		}),
	)
	checks := newOrchestratorPlan5DoctorChecks(srv.URL)
	for _, c := range checks {
		if c.Name() == "orchestrator.caronte_up" {
			res := c.Run(context.Background())
			if res.Pass {
				t.Fatal("caronte_up should fail when index currency > 24h")
			}
			return
		}
	}
}

func TestDoctor_AdaptersClean_FailsOnDirty(t *testing.T) {
	srv := newFakeDoctorDaemon(t,
		client.SessionInfo{BackgroundGoroutines: 11},
		client.PoolStatus{HealthOK: true},
		client.SafetynetStatus{SubstratePassRate7d: 0.99},
		withRoute("/v1/orchestrator/health/adapters_clean", func(w http.ResponseWriter, _ *http.Request) {
			writeJSONP5(w, map[string]bool{"clean": false})
		}),
	)
	checks := newOrchestratorPlan5DoctorChecks(srv.URL)
	for _, c := range checks {
		if c.Name() == "orchestrator.adapters_clean" {
			res := c.Run(context.Background())
			if res.Pass {
				t.Fatal("adapters_clean should fail when dirty")
			}
			return
		}
	}
}

func TestDoctor_LastSessionClean_FailsOnDirty(t *testing.T) {
	srv := newFakeDoctorDaemon(t,
		client.SessionInfo{BackgroundGoroutines: 11},
		client.PoolStatus{HealthOK: true},
		client.SafetynetStatus{SubstratePassRate7d: 0.99},
		withRoute("/v1/orchestrator/health/last_session_clean", func(w http.ResponseWriter, _ *http.Request) {
			writeJSONP5(w, map[string]bool{"clean": false})
		}),
	)
	checks := newOrchestratorPlan5DoctorChecks(srv.URL)
	for _, c := range checks {
		if c.Name() == "orchestrator.last_session_clean" {
			res := c.Run(context.Background())
			if res.Pass {
				t.Fatal("last_session_clean should fail when last session crashed")
			}
			return
		}
	}
}

func TestDoctor_RunOrchestratorPlan5ChecksAt_TranslatesToCheckResult(t *testing.T) {
	srv := newFakeDoctorDaemon(t,
		client.SessionInfo{BackgroundGoroutines: 11},
		client.PoolStatus{HealthOK: true},
		client.SafetynetStatus{SubstratePassRate7d: 0.99})
	results := runOrchestratorPlan5ChecksAt(context.Background(), srv.URL)
	if len(results) != 9 {
		t.Fatalf("expected 9 results, got %d", len(results))
	}
	for _, r := range results {
		if r.Status != "ok" {
			t.Errorf("expected all ok in healthy state; got %s = %s (%s)", r.Name, r.Status, r.Detail)
		}
		if r.Name == "" {
			t.Error("CheckResult.Name should be populated")
		}
	}
}
