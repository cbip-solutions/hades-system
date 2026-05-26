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

type safetynetRec struct {
	prevExecArgv []string
}

func newFakeSafetynetDaemon(t *testing.T, status client.SafetynetStatus) (*httptest.Server, *safetynetRec) {
	t.Helper()
	rec := &safetynetRec{}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/safetynet/status", func(w http.ResponseWriter, _ *http.Request) {
		writeJSONP5(w, status)
	})
	mux.HandleFunc("/v1/safetynet/prev/install", func(w http.ResponseWriter, _ *http.Request) {
		writeJSONP5(w, map[string]string{"status": "installed", "version": "v0.4.0"})
	})
	mux.HandleFunc("/v1/safetynet/prev/show", func(w http.ResponseWriter, _ *http.Request) {
		writeJSONP5(w, map[string]string{"path": "/usr/local/bin/zen-prev", "version": "v0.4.0"})
	})
	mux.HandleFunc("/v1/safetynet/prev/exec", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Argv []string `json:"argv"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		rec.prevExecArgv = body.Argv
		writeJSONP5(w, map[string]any{"stdout": "ok"})
	})
	mux.HandleFunc("/v1/safetynet/divergence/run", func(w http.ResponseWriter, _ *http.Request) {
		writeJSONP5(w, client.DivergenceReport{Clean: true, Differences: []string{}})
	})
	mux.HandleFunc("/v1/safetynet/divergence/history", func(w http.ResponseWriter, _ *http.Request) {
		writeJSONP5(w, []client.DivergenceReport{{Clean: true, RanAt: 1714531080}})
	})
	mux.HandleFunc("/v1/safetynet/regression/query", func(w http.ResponseWriter, _ *http.Request) {
		writeJSONP5(w, []client.RegressionMetric{
			{CommitSHA: "abc12345abc12345", AuthoredBy: "substrate", TestPassRate: 1.0, TestTotal: 100, TestPassed: 100},
		})
	})
	mux.HandleFunc("/v1/safetynet/drift/run", func(w http.ResponseWriter, _ *http.Request) {
		writeJSONP5(w, []client.DriftFinding{})
	})
	mux.HandleFunc("/v1/safetynet/drift/history", func(w http.ResponseWriter, _ *http.Request) {
		writeJSONP5(w, []client.DriftFinding{
			{CommitSHA: "deadbeef", Rule: "no_" + "claude_attribution", Description: "found in HEAD"},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, rec
}

func runSafetynetSubcommand(t *testing.T, srvURL string, args ...string) (string, error) {
	t.Helper()
	root := NewSafetynetCmd()
	if err := root.PersistentFlags().Set(plan5DaemonURLFlag, srvURL); err != nil {
		t.Fatalf("set %s: %v", plan5DaemonURLFlag, err)
	}
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs(args)
	err := root.ExecuteContext(context.Background())
	return buf.String(), err
}

func TestSafetynetStatus(t *testing.T) {
	srv, _ := newFakeSafetynetDaemon(t, client.SafetynetStatus{
		PrevBinaryInstalled: true, PrevBinaryVersion: "v0.4.0",
		LastDivergenceClean: true, SubstratePassRate7d: 0.997,
		DriftIncidents24h: 0,
	})
	out, err := runSafetynetSubcommand(t, srv.URL, "status")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, want := range []string{"v0.4.0", "99.7", "drift incidents"} {
		if !strings.Contains(strings.ToLower(out), strings.ToLower(want)) {
			t.Errorf("status output missing %q\n--- got ---\n%s", want, out)
		}
	}
}

func TestSafetynetDivergenceRun(t *testing.T) {
	srv, _ := newFakeSafetynetDaemon(t, client.SafetynetStatus{})
	out, err := runSafetynetSubcommand(t, srv.URL, "divergence", "run")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "clean") {
		t.Errorf("expected clean signal: %s", out)
	}
}

func TestSafetynetDivergenceHistory(t *testing.T) {
	srv, _ := newFakeSafetynetDaemon(t, client.SafetynetStatus{})
	out, err := runSafetynetSubcommand(t, srv.URL, "divergence", "history", "--since", "24h")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "1714531080") {
		t.Errorf("expected history row: %s", out)
	}
}

func TestSafetynetRegressionQuery(t *testing.T) {
	srv, _ := newFakeSafetynetDaemon(t, client.SafetynetStatus{})
	out, err := runSafetynetSubcommand(t, srv.URL, "regression", "query", "--author", "substrate", "--since", "7d")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "abc12345") || !strings.Contains(out, "substrate") {
		t.Errorf("expected regression rows in output: %s", out)
	}
}

func TestSafetynetRegressionShow(t *testing.T) {
	srv, _ := newFakeSafetynetDaemon(t, client.SafetynetStatus{})
	out, err := runSafetynetSubcommand(t, srv.URL, "regression", "show")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "100/100") {
		t.Errorf("expected regression metric: %s", out)
	}
}

func TestSafetynetDriftRunNoFindings(t *testing.T) {
	srv, _ := newFakeSafetynetDaemon(t, client.SafetynetStatus{})
	out, err := runSafetynetSubcommand(t, srv.URL, "drift", "run")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "no drift detected") {
		t.Errorf("expected clean drift run: %s", out)
	}
}

func TestSafetynetDriftHistory(t *testing.T) {
	srv, _ := newFakeSafetynetDaemon(t, client.SafetynetStatus{})
	out, err := runSafetynetSubcommand(t, srv.URL, "drift", "history", "--since", "24h")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !strings.Contains(out, "deadbeef") {
		t.Errorf("expected drift finding in output: %s", out)
	}
}

func TestSafetynetPrevInstall(t *testing.T) {
	srv, _ := newFakeSafetynetDaemon(t, client.SafetynetStatus{})
	out, err := runSafetynetSubcommand(t, srv.URL, "prev", "install")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "v0.4.0") {
		t.Errorf("expected version: %s", out)
	}
}

func TestSafetynetPrevShow(t *testing.T) {
	srv, _ := newFakeSafetynetDaemon(t, client.SafetynetStatus{})
	out, err := runSafetynetSubcommand(t, srv.URL, "prev", "show")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, want := range []string{"/usr/local/bin/zen-prev", "v0.4.0"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q: %s", want, out)
		}
	}
}

func TestSafetynetPrevExec_RequiresArgs(t *testing.T) {
	srv, _ := newFakeSafetynetDaemon(t, client.SafetynetStatus{})
	_, err := runSafetynetSubcommand(t, srv.URL, "prev", "exec")
	if err == nil {
		t.Fatal("expected error for missing exec args")
	}
}

func TestSafetynetPrevExec_Happy(t *testing.T) {
	srv, rec := newFakeSafetynetDaemon(t, client.SafetynetStatus{})
	out, err := runSafetynetSubcommand(t, srv.URL, "prev", "exec", "doctor", "--quick")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(rec.prevExecArgv) != 2 || rec.prevExecArgv[0] != "doctor" || rec.prevExecArgv[1] != "--quick" {
		t.Errorf("argv not propagated: %+v", rec.prevExecArgv)
	}
	if !strings.Contains(out, "ok") {
		t.Errorf("expected stdout passthrough: %s", out)
	}
}
