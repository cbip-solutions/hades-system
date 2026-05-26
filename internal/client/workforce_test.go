package client_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/client"
)

func newWorkforceServer(t *testing.T, handler http.Handler) (*client.Client, func()) {
	t.Helper()
	srv := httptest.NewServer(handler)
	c := client.NewWithBaseURL(srv.URL)
	return c, srv.Close
}

func TestWorkforceSpecs(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/workforce/specs", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []client.WorkforceSpec{
				{ID: "spec_a", Variant: "worker", TaskTier: "medium", ModelClass: "sonnet"},
			},
			"count": 1,
		})
	})
	c, stop := newWorkforceServer(t, mux)
	defer stop()
	specs, err := c.WorkforceSpecs(context.Background(), "", 50, 0)
	if err != nil {
		t.Fatalf("WorkforceSpecs: %v", err)
	}
	if len(specs) != 1 || specs[0].ID != "spec_a" {
		t.Errorf("got %+v", specs)
	}
}

func TestWorkforceWorkers_QueryEncoding(t *testing.T) {
	mux := http.NewServeMux()
	var seenQuery string
	mux.HandleFunc("/v1/workforce/workers", func(w http.ResponseWriter, r *http.Request) {
		seenQuery = r.URL.RawQuery
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []client.WorkforceWorker{{ID: "w_1", Status: "running"}},
			"count": 1,
		})
	})
	c, stop := newWorkforceServer(t, mux)
	defer stop()
	if _, err := c.WorkforceWorkers(context.Background(), "running", 25, 5); err != nil {
		t.Fatalf("WorkforceWorkers: %v", err)
	}
	for _, want := range []string{"status=running", "limit=25", "offset=5"} {
		if !strings.Contains(seenQuery, want) {
			t.Errorf("query %q missing %q", seenQuery, want)
		}
	}
}

func TestWorkforceCheckpoints_Filter(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/workforce/checkpoints", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("task_id") != "task_42" {
			t.Errorf("missing task_id filter; got query %q", r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []client.WorkforceCheckpoint{}})
	})
	c, stop := newWorkforceServer(t, mux)
	defer stop()
	if _, err := c.WorkforceCheckpoints(context.Background(), "task_42", 0, 0); err != nil {
		t.Fatalf("Checkpoints: %v", err)
	}
}

func TestWorkforceFixPrompts(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/workforce/fix_prompts", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []client.WorkforceFixPrompt{{ID: "fp_1", FromLayer: "l3"}},
		})
	})
	c, stop := newWorkforceServer(t, mux)
	defer stop()
	items, err := c.WorkforceFixPrompts(context.Background(), "", 0, 0)
	if err != nil {
		t.Fatalf("FixPrompts: %v", err)
	}
	if len(items) != 1 || items[0].FromLayer != "l3" {
		t.Errorf("got %+v", items)
	}
}

func TestGateState(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/workforce/gate/state", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.GateStateResp{
			State: "running", CanPause: true, CanResume: false,
		})
	})
	c, stop := newWorkforceServer(t, mux)
	defer stop()
	st, err := c.GateState(context.Background())
	if err != nil {
		t.Fatalf("GateState: %v", err)
	}
	if st.State != "running" || !st.CanPause {
		t.Errorf("got %+v", st)
	}
}

func TestGatePauseRoundTrip(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/workforce/gate/pause", func(w http.ResponseWriter, r *http.Request) {
		var got client.GatePauseReq
		_ = json.NewDecoder(r.Body).Decode(&got)
		if got.Mode != "paused_quiet" || got.Reason != "operator pause" {
			t.Errorf("body: %+v", got)
		}
		_ = json.NewEncoder(w).Encode(client.GatePauseResp{State: "paused_quiet", Paused: true})
	})
	c, stop := newWorkforceServer(t, mux)
	defer stop()
	st, err := c.GatePause(context.Background(), client.GatePauseReq{Mode: "paused_quiet", Reason: "operator pause"})
	if err != nil {
		t.Fatalf("GatePause: %v", err)
	}
	if st.State != "paused_quiet" || !st.Paused {
		t.Errorf("got %+v", st)
	}
}

func TestGateResume(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/workforce/gate/resume", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.GateResumeResp{State: "running", Running: true})
	})
	c, stop := newWorkforceServer(t, mux)
	defer stop()
	st, err := c.GateResume(context.Background())
	if err != nil {
		t.Fatalf("GateResume: %v", err)
	}
	if !st.Running || st.State != "running" {
		t.Errorf("got %+v", st)
	}
}

func TestWorkforceStatus_Composes(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/workforce/gate/state", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.GateStateResp{State: "running", CanPause: true})
	})
	mux.HandleFunc("/v1/workforce/workers", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []client.WorkforceWorker{
				{ID: "w_1", Status: "in_progress"},
				{ID: "w_2", Status: "review"},
				{ID: "w_3", Status: "done"},
			},
		})
	})
	mux.HandleFunc("/v1/workforce/specs", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []client.WorkforceSpec{{ID: "spec_a"}, {ID: "spec_b"}},
		})
	})
	mux.HandleFunc("/v1/workforce/checkpoints", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []client.WorkforceCheckpoint{}})
	})
	mux.HandleFunc("/v1/workforce/fix_prompts", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []client.WorkforceFixPrompt{}})
	})
	c, stop := newWorkforceServer(t, mux)
	defer stop()
	snap, err := c.WorkforceStatus(context.Background())
	if err != nil {
		t.Fatalf("WorkforceStatus: %v", err)
	}
	if snap.WorkersTotal != 3 || snap.WorkersInProgress != 1 || snap.WorkersReview != 1 ||
		snap.WorkersDone != 1 || snap.SpecsLoaded != 2 || snap.GateState != "running" {
		t.Errorf("snap: %+v", snap)
	}
}

func TestFormatUnix(t *testing.T) {
	if got := client.FormatUnix(0); got != "" {
		t.Errorf("zero: %q", got)
	}
	got := client.FormatUnix(1759320000)
	if got == "" || !strings.HasSuffix(got, "Z") {
		t.Errorf("FormatUnix: %q", got)
	}
}

func TestWorkforceSpecs_Error(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/workforce/specs", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	c, stop := newWorkforceServer(t, mux)
	defer stop()
	if _, err := c.WorkforceSpecs(context.Background(), "", 0, 0); err == nil {
		t.Fatal("expected error")
	}
}

func TestWorkforceSpecs_VariantFilter(t *testing.T) {
	var captured string
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/workforce/specs", func(w http.ResponseWriter, r *http.Request) {
		captured = r.URL.RawQuery
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []client.WorkforceSpec{}})
	})
	c, stop := newWorkforceServer(t, mux)
	defer stop()
	if _, err := c.WorkforceSpecs(context.Background(), "reviewer", 0, 10); err != nil {
		t.Fatalf("specs: %v", err)
	}
	if !strings.Contains(captured, "variant=reviewer") || !strings.Contains(captured, "offset=10") {
		t.Errorf("query: %s", captured)
	}
}

func TestWorkforceCheckpoints_Error(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/workforce/checkpoints", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	c, stop := newWorkforceServer(t, mux)
	defer stop()
	if _, err := c.WorkforceCheckpoints(context.Background(), "", 0, 0); err == nil {
		t.Fatal("expected error")
	}
}

func TestWorkforceCheckpoints_Pagination(t *testing.T) {
	var captured string
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/workforce/checkpoints", func(w http.ResponseWriter, r *http.Request) {
		captured = r.URL.RawQuery
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []client.WorkforceCheckpoint{}})
	})
	c, stop := newWorkforceServer(t, mux)
	defer stop()
	if _, err := c.WorkforceCheckpoints(context.Background(), "", 50, 25); err != nil {
		t.Fatalf("checkpoints: %v", err)
	}
	if !strings.Contains(captured, "limit=50") || !strings.Contains(captured, "offset=25") {
		t.Errorf("query: %s", captured)
	}
}

func TestWorkforceFixPrompts_Error(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/workforce/fix_prompts", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	c, stop := newWorkforceServer(t, mux)
	defer stop()
	if _, err := c.WorkforceFixPrompts(context.Background(), "", 0, 0); err == nil {
		t.Fatal("expected error")
	}
}

func TestWorkforceFixPrompts_Filter(t *testing.T) {
	var captured string
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/workforce/fix_prompts", func(w http.ResponseWriter, r *http.Request) {
		captured = r.URL.RawQuery
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []client.WorkforceFixPrompt{}})
	})
	c, stop := newWorkforceServer(t, mux)
	defer stop()
	if _, err := c.WorkforceFixPrompts(context.Background(), "task_42", 25, 0); err != nil {
		t.Fatalf("fix_prompts: %v", err)
	}
	if !strings.Contains(captured, "task_id=task_42") || !strings.Contains(captured, "limit=25") {
		t.Errorf("query: %s", captured)
	}
}

func TestGateState_Error(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/workforce/gate/state", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	c, stop := newWorkforceServer(t, mux)
	defer stop()
	if _, err := c.GateState(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestGatePause_Error(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/workforce/gate/pause", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	c, stop := newWorkforceServer(t, mux)
	defer stop()
	if _, err := c.GatePause(context.Background(), client.GatePauseReq{Mode: "x"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestGateResume_Error(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/workforce/gate/resume", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	c, stop := newWorkforceServer(t, mux)
	defer stop()
	if _, err := c.GateResume(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestWorkforceStatus_GateError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/workforce/gate/state", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	c, stop := newWorkforceServer(t, mux)
	defer stop()
	if _, err := c.WorkforceStatus(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestWorkforceStatus_WorkersError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/workforce/gate/state", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.GateStateResp{State: "running"})
	})
	mux.HandleFunc("/v1/workforce/workers", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	c, stop := newWorkforceServer(t, mux)
	defer stop()
	if _, err := c.WorkforceStatus(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestWorkforceStatus_AllStatusBuckets(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/workforce/gate/state", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.GateStateResp{State: "running"})
	})
	mux.HandleFunc("/v1/workforce/workers", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []client.WorkforceWorker{
				{ID: "w1", Status: "pending"},
				{ID: "w2", Status: "in_progress"},
				{ID: "w3", Status: "review"},
				{ID: "w4", Status: "done"},
				{ID: "w5", Status: "failed"},
				{ID: "w6", Status: "queued"},
			},
		})
	})
	mux.HandleFunc("/v1/workforce/specs", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []client.WorkforceSpec{}})
	})
	mux.HandleFunc("/v1/workforce/checkpoints", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []client.WorkforceCheckpoint{}})
	})
	mux.HandleFunc("/v1/workforce/fix_prompts", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []client.WorkforceFixPrompt{}})
	})
	c, stop := newWorkforceServer(t, mux)
	defer stop()
	snap, err := c.WorkforceStatus(context.Background())
	if err != nil {
		t.Fatalf("WorkforceStatus: %v", err)
	}
	if snap.WorkersTotal != 6 || snap.WorkersPending != 1 || snap.WorkersInProgress != 1 ||
		snap.WorkersReview != 1 || snap.WorkersDone != 1 || snap.WorkersFailed != 1 {
		t.Errorf("snap: %+v", snap)
	}
}
