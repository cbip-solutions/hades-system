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

type orchPlan5Rec struct {
	calledPrune    bool
	depth          client.DepthOverride
	capture        client.CaptureRequest
	replay         client.ReplayRequest
	captureResult  client.CaptureResult
	replayResult   client.ReplayResult
	state          client.SessionInfo
	pool           client.PoolStatus
	replayHTTPCode int
}

func newFakeOrchPlan5Daemon(t *testing.T, rec *orchPlan5Rec) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/orchestrator/state", func(w http.ResponseWriter, _ *http.Request) {
		writeJSONP5(w, rec.state)
	})
	mux.HandleFunc("/v1/orchestrator/pool", func(w http.ResponseWriter, _ *http.Request) {
		writeJSONP5(w, rec.pool)
	})
	mux.HandleFunc("/v1/orchestrator/pool/prune", func(w http.ResponseWriter, _ *http.Request) {
		rec.calledPrune = true
		writeJSONP5(w, map[string]int{"orphans_pruned": 4})
	})
	mux.HandleFunc("/v1/orchestrator/depth", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&rec.depth)
		writeJSONP5(w, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/v1/orchestrator/capture", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&rec.capture)
		writeJSONP5(w, rec.captureResult)
	})
	mux.HandleFunc("/v1/orchestrator/replay", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&rec.replay)
		if rec.replayHTTPCode != 0 {
			http.Error(w, "replay failed", rec.replayHTTPCode)
			return
		}
		writeJSONP5(w, rec.replayResult)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func writeJSONP5(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func runOrchSubcommand(t *testing.T, srvURL string, args ...string) (string, error) {
	t.Helper()
	root := NewOrchestratorCmd()
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

func TestOrchestratorState_Happy(t *testing.T) {
	rec := &orchPlan5Rec{
		state: client.SessionInfo{
			SessionID: "sess-1", State: "RUNNING", Mode: "semi",
			BackgroundGoroutines: 11,
			RecentTransitions: []client.StateTransition{
				{From: "IDLE", To: "RUNNING", Reason: "operator-start"},
			},
		},
	}
	srv := newFakeOrchPlan5Daemon(t, rec)
	out, err := runOrchSubcommand(t, srv.URL, "state")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, want := range []string{"RUNNING", "sess-1", "11", "operator-start"} {
		if !strings.Contains(out, want) {
			t.Errorf("state output missing %q\n--- got ---\n%s", want, out)
		}
	}
}

func TestOrchestratorPoolStatus(t *testing.T) {
	rec := &orchPlan5Rec{
		pool: client.PoolStatus{Floor: 8, Maximum: 16, CurrentLeased: 3, ElasticInUse: 2, OrphansCleaned: 1, HealthOK: true},
	}
	srv := newFakeOrchPlan5Daemon(t, rec)
	out, err := runOrchSubcommand(t, srv.URL, "pool", "status")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, want := range []string{"Floor: 8", "Maximum: 16", "Leased: 3", "Elastic in-use: 2", "Orphans cleaned: 1", "true"} {
		if !strings.Contains(out, want) {
			t.Errorf("pool status output missing %q\n--- got ---\n%s", want, out)
		}
	}
}

func TestOrchestratorPoolPrune(t *testing.T) {
	rec := &orchPlan5Rec{}
	srv := newFakeOrchPlan5Daemon(t, rec)
	out, err := runOrchSubcommand(t, srv.URL, "pool", "prune")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !rec.calledPrune {
		t.Fatal("daemon prune endpoint not called")
	}
	if !strings.Contains(out, "4") {
		t.Errorf("expected 4 orphans pruned, got %q", out)
	}
}

func TestOrchestratorDepthOverride(t *testing.T) {
	rec := &orchPlan5Rec{}
	srv := newFakeOrchPlan5Daemon(t, rec)
	out, err := runOrchSubcommand(t, srv.URL, "depth", "--project", "proj-1", "--spec", "spec.md", "--depth", "5")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if rec.depth.ProjectID != "proj-1" || rec.depth.Depth != 5 || rec.depth.SpecPath != "spec.md" {
		t.Errorf("depth override not propagated: %+v", rec.depth)
	}
	if !strings.Contains(out, "depth override applied") {
		t.Errorf("expected confirmation, got %q", out)
	}
}

func TestOrchestratorDepthMutuallyExclusive(t *testing.T) {
	rec := &orchPlan5Rec{}
	srv := newFakeOrchPlan5Daemon(t, rec)
	_, err := runOrchSubcommand(t, srv.URL, "depth", "--project", "p", "--spec", "s.md", "--depth", "5", "--reset")
	if err == nil {
		t.Fatal("expected error for --depth + --reset")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("error should mention mutually exclusive: %v", err)
	}
}

func TestOrchestratorDepthRequiresFlags(t *testing.T) {
	rec := &orchPlan5Rec{}
	srv := newFakeOrchPlan5Daemon(t, rec)
	if _, err := runOrchSubcommand(t, srv.URL, "depth", "--project", "p"); err == nil {
		t.Error("expected error: missing --spec")
	}
	if _, err := runOrchSubcommand(t, srv.URL, "depth", "--spec", "s"); err == nil {
		t.Error("expected error: missing --project")
	}
}

func TestOrchestratorDepthResetAlone(t *testing.T) {
	rec := &orchPlan5Rec{}
	srv := newFakeOrchPlan5Daemon(t, rec)
	_, err := runOrchSubcommand(t, srv.URL, "depth", "--project", "p", "--spec", "s.md", "--reset")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !rec.depth.Reset {
		t.Errorf("--reset not propagated: %+v", rec.depth)
	}
}

func TestOrchestratorCaptureHappy(t *testing.T) {
	rec := &orchPlan5Rec{
		captureResult: client.CaptureResult{
			OutputPath: "/tmp/capture.jsonl", EventCount: 42, BytesWritten: 1024,
		},
	}
	srv := newFakeOrchPlan5Daemon(t, rec)
	out, err := runOrchSubcommand(t, srv.URL, "capture", "--session-id", "sess-X", "--output", "/tmp/capture.jsonl")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if rec.capture.SessionID != "sess-X" || rec.capture.OutputPath != "/tmp/capture.jsonl" {
		t.Errorf("capture request not propagated: %+v", rec.capture)
	}
	for _, want := range []string{"42 events", "/tmp/capture.jsonl", "1024 bytes"} {
		if !strings.Contains(out, want) {
			t.Errorf("capture output missing %q\n--- got ---\n%s", want, out)
		}
	}
}

func TestOrchestratorCaptureRequiresFlags(t *testing.T) {
	rec := &orchPlan5Rec{}
	srv := newFakeOrchPlan5Daemon(t, rec)
	if _, err := runOrchSubcommand(t, srv.URL, "capture"); err == nil {
		t.Error("expected error for missing flags")
	}
	if _, err := runOrchSubcommand(t, srv.URL, "capture", "--session-id", "sess-X"); err == nil {
		t.Error("expected error for missing --output")
	}
}

func TestOrchestratorReplayHappy(t *testing.T) {
	rec := &orchPlan5Rec{
		replayResult: client.ReplayResult{
			EventsReplayed: 10, Deterministic: true,
		},
	}
	srv := newFakeOrchPlan5Daemon(t, rec)
	out, err := runOrchSubcommand(t, srv.URL, "replay", "/tmp/fixture.jsonl")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if rec.replay.InputPath != "/tmp/fixture.jsonl" {
		t.Errorf("replay request not propagated: %+v", rec.replay)
	}
	for _, want := range []string{"10 events", "deterministic=true"} {
		if !strings.Contains(out, want) {
			t.Errorf("replay output missing %q\n--- got ---\n%s", want, out)
		}
	}
}

func TestOrchestratorReplayWithDivergence(t *testing.T) {
	rec := &orchPlan5Rec{
		replayResult: client.ReplayResult{
			EventsReplayed: 10, Deterministic: false,
			Divergences: []string{"event 5: tier mismatch", "event 8: cost delta"},
		},
	}
	srv := newFakeOrchPlan5Daemon(t, rec)
	out, err := runOrchSubcommand(t, srv.URL, "replay", "/tmp/fixture.jsonl")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, want := range []string{"divergences=2", "event 5: tier mismatch", "event 8: cost delta"} {
		if !strings.Contains(out, want) {
			t.Errorf("replay output missing %q\n--- got ---\n%s", want, out)
		}
	}
}

func TestOrchestratorReplayRequiresFile(t *testing.T) {
	rec := &orchPlan5Rec{}
	srv := newFakeOrchPlan5Daemon(t, rec)
	_, err := runOrchSubcommand(t, srv.URL, "replay")
	if err == nil {
		t.Error("expected error: replay requires file argument")
	}
}

func TestOrchestratorReplayHTTPError(t *testing.T) {
	rec := &orchPlan5Rec{replayHTTPCode: http.StatusInternalServerError}
	srv := newFakeOrchPlan5Daemon(t, rec)
	_, err := runOrchSubcommand(t, srv.URL, "replay", "/tmp/fixture.jsonl")
	if err == nil {
		t.Error("expected error from 500 response")
	}
}
