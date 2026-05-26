package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/client"
)

type briefRoutes map[string]http.HandlerFunc

func newFakeBriefDaemon(t *testing.T, opts ...func(briefRoutes)) *httptest.Server {
	t.Helper()
	routes := briefRoutes{
		"/v1/autonomy/show": func(w http.ResponseWriter, _ *http.Request) {
			writeJSONP5(w, client.AutonomyShow{
				EffectiveMode: "semi", ResolvedFrom: "doctrine", DoctrineMode: "max-scope",
			})
		},
		"/v1/orchestrator/state": func(w http.ResponseWriter, _ *http.Request) {
			writeJSONP5(w, client.SessionInfo{
				SessionID:        "stage-build-42",
				State:            "WAITING_FOR_CONFIRMATION",
				LastTransitionAt: 1714531080,
				RecentTransitions: []client.StateTransition{
					{From: "RUNNING", To: "WAITING_FOR_CONFIRMATION", Reason: "evt-1234567", Timestamp: 1714531080},
				},
			})
		},
		"/v1/doctrine/propose-list": func(w http.ResponseWriter, _ *http.Request) {
			writeJSONP5(w, client.DoctrineProposalList{Proposals: []client.DoctrineProposal{
				{ID: "ADR-0009", Status: "proposed", Title: "Adjust HRA cadence"},
			}})
		},
		"/v1/safetynet/status": func(w http.ResponseWriter, _ *http.Request) {
			writeJSONP5(w, client.SafetynetStatus{
				SubstratePassRate7d: 0.997, DriftIncidents24h: 0,
				LastDivergenceClean: true, LastDivergenceAt: 1714530000,
			})
		},
		"/v1/orchestrator/pool": func(w http.ResponseWriter, _ *http.Request) {
			writeJSONP5(w, client.PoolStatus{Floor: 8, ElasticInUse: 14, OrphansCleaned: 3})
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

func TestMorningBriefAutonomy_AllSubsectionsPresent(t *testing.T) {
	srv := newFakeBriefDaemon(t)
	render := newMorningBriefAutonomyRenderer(srv.URL)
	out, err := render(context.Background())
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, want := range []string{
		"[plan-5 autonomy]",
		"Mode: semi",
		"max-scope",
		"WAITING_FOR_CONFIRMATION",
		"evt-1234567",
		"Pending amendments: 1",
		"ADR-0009",
		"Drift incidents: 0",
		"floor=8",
		"elastic-current=14",
		"orphans cleaned=3",
		"clean",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("brief missing %q\n--- output ---\n%s", want, out)
		}
	}
}

func TestMorningBriefAutonomy_NoPendingAmendments(t *testing.T) {
	srv := newFakeBriefDaemon(t,
		func(routes briefRoutes) {
			routes["/v1/autonomy/show"] = func(w http.ResponseWriter, _ *http.Request) {
				writeJSONP5(w, client.AutonomyShow{EffectiveMode: "manual", ResolvedFrom: "default"})
			}
			routes["/v1/orchestrator/state"] = func(w http.ResponseWriter, _ *http.Request) {
				writeJSONP5(w, client.SessionInfo{SessionID: "idle", State: "IDLE"})
			}
			routes["/v1/doctrine/propose-list"] = func(w http.ResponseWriter, _ *http.Request) {
				writeJSONP5(w, client.DoctrineProposalList{})
			}
			routes["/v1/safetynet/status"] = func(w http.ResponseWriter, _ *http.Request) {
				writeJSONP5(w, client.SafetynetStatus{})
			}
			routes["/v1/orchestrator/pool"] = func(w http.ResponseWriter, _ *http.Request) {
				writeJSONP5(w, client.PoolStatus{Floor: 3})
			}
		},
	)
	render := newMorningBriefAutonomyRenderer(srv.URL)
	out, err := render(context.Background())
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(out, "Pending amendments: 0") {
		t.Errorf("expected zero amendments line: %s", out)
	}

	if strings.Contains(out, "since") {
		t.Errorf("IDLE session should not emit State line: %s", out)
	}
}

func TestMorningBriefAutonomy_DaemonDownGracefulMessage(t *testing.T) {
	render := newMorningBriefAutonomyRenderer("http://127.0.0.1:1")
	out, err := render(context.Background())
	if err != nil {
		t.Fatalf("render should not error on daemon-down (graceful): %v", err)
	}
	if !strings.Contains(out, "daemon unreachable") {
		t.Errorf("expected graceful 'daemon unreachable' message: %s", out)
	}

	if !strings.Contains(out, "[plan-5 autonomy]") {
		t.Errorf("section header must always render: %s", out)
	}
}

func TestMorningBriefAutonomy_DivergenceDirty(t *testing.T) {
	srv := newFakeBriefDaemon(t,
		func(routes briefRoutes) {
			routes["/v1/safetynet/status"] = func(w http.ResponseWriter, _ *http.Request) {
				writeJSONP5(w, client.SafetynetStatus{
					LastDivergenceAt:    1714530000,
					LastDivergenceClean: false,
					DriftIncidents24h:   2,
				})
			}
		},
	)
	render := newMorningBriefAutonomyRenderer(srv.URL)
	out, err := render(context.Background())
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(out, "DIRTY") {
		t.Errorf("dirty divergence should surface as DIRTY: %s", out)
	}
	if !strings.Contains(out, "Drift incidents: 2") {
		t.Errorf("drift count should render: %s", out)
	}
}

func TestMorningBriefAutonomy_RunningStateNoConfirmation(t *testing.T) {
	srv := newFakeBriefDaemon(t,
		func(routes briefRoutes) {
			routes["/v1/orchestrator/state"] = func(w http.ResponseWriter, _ *http.Request) {
				writeJSONP5(w, client.SessionInfo{
					SessionID:        "active",
					State:            "RUNNING",
					LastTransitionAt: 1714531080,
				})
			}
		},
	)
	render := newMorningBriefAutonomyRenderer(srv.URL)
	out, err := render(context.Background())
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(out, "RUNNING") {
		t.Errorf("running state should render: %s", out)
	}
	if strings.Contains(out, "Action needed") {
		t.Errorf("RUNNING state should not emit Action-needed line: %s", out)
	}
}
