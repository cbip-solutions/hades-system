package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type integrationServer struct {
	cache map[string]string

	auditLog []AuditEventIn

	budgetPauses map[string]string

	workerSpecs []WorkerSpecRow

	gateState string

	doctrineName string
}

func newIntegrationServer() *integrationServer {
	return &integrationServer{
		cache:        make(map[string]string),
		budgetPauses: make(map[string]string),
		gateState:    "running",
		doctrineName: "max-scope",
		workerSpecs: []WorkerSpecRow{
			{ID: "spec-int-1", Variant: "worker", TaskTier: "medium", DoctrineName: "max-scope"},
		},
	}
}

func (s *integrationServer) ResearchCacheGet(hash string) (string, int64, bool, error) {
	v, ok := s.cache[hash]
	if !ok {
		return "", 0, false, nil
	}

	return v, time.Now().Add(time.Hour).Unix(), true, nil
}
func (s *integrationServer) ResearchCacheSet(hash, responseJSON string, ttlUnix int64) error {
	s.cache[hash] = responseJSON
	return nil
}
func (s *integrationServer) ResearchCacheTTL() time.Duration { return 7 * 24 * time.Hour }

func (s *integrationServer) AuditEmit(event AuditEventIn) error {
	s.auditLog = append(s.auditLog, event)
	return nil
}

func (s *integrationServer) BudgetCapStatus(axis, value string) (BudgetCapStatusResult, error) {
	return BudgetCapStatusResult{RemainingUSD: 99.99}, nil
}
func (s *integrationServer) BudgetRecord(req BudgetRecordReq) error { return nil }
func (s *integrationServer) BudgetAxes(costID string) ([]BudgetAxisTag, error) {
	return []BudgetAxisTag{{AxisName: "project", AxisValue: "test"}}, nil
}
func (s *integrationServer) BudgetAnomalyCheck(scope, value string, windowSec int64) (BudgetAnomalyResult, error) {
	return BudgetAnomalyResult{ZScore: 1.2, Mean: 0.5, StdDev: 0.1, Samples: 100}, nil
}
func (s *integrationServer) BudgetEvents(sinceUnix int64, limitN int) ([]BudgetEventRow, error) {
	return []BudgetEventRow{}, nil
}
func (s *integrationServer) BudgetPause(scope, value, reason string) (string, error) {
	s.budgetPauses[scope+":"+value] = "paused"
	return "paused", nil
}
func (s *integrationServer) BudgetResume(scope, value string) (string, error) {
	s.budgetPauses[scope+":"+value] = "running"
	return "running", nil
}

func (s *integrationServer) WorkforceSpecs(limit, offset int, filter string) ([]WorkerSpecRow, error) {
	return s.workerSpecs, nil
}
func (s *integrationServer) WorkforceWorkers(limit, offset int, status string) ([]WorkerRow, error) {
	return []WorkerRow{}, nil
}
func (s *integrationServer) WorkforceCheckpoints(taskID string, limit, offset int) ([]CheckpointRow, error) {
	return []CheckpointRow{}, nil
}
func (s *integrationServer) WorkforceFixPrompts(taskID string, limit, offset int) ([]FixPromptRow, error) {
	return []FixPromptRow{}, nil
}
func (s *integrationServer) WorkforceAggregations(layer string, windowSec int64, limit int) ([]AggregationRow, error) {
	return []AggregationRow{}, nil
}

func (s *integrationServer) OperatorGateState() (string, error) { return s.gateState, nil }
func (s *integrationServer) OperatorGatePause(mode, reason string) (string, error) {
	if s.gateState == "running" {
		s.gateState = mode
	}
	return s.gateState, nil
}
func (s *integrationServer) OperatorGateResume() (string, error) {
	s.gateState = "running"
	return "running", nil
}

func (s *integrationServer) RateLimitThreshold(endpoint string) int { return 1000 }

func buildIntegrationMux(srv *integrationServer) *http.ServeMux {
	mux := http.NewServeMux()
	registry := NewBucketRegistry()
	rl := func(ep string, h http.Handler) http.Handler { return RateLimitMiddleware(srv, registry, ep, h) }

	mux.Handle("GET /v1/research/cache/get", rl("research_cache_get", ResearchCacheGet(srv)))
	mux.Handle("POST /v1/research/cache/set", rl("research_cache_set", ResearchCacheSet(srv)))
	mux.Handle("POST /v1/audit/emit", rl("audit_emit", AuditEmit(srv)))
	mux.Handle("GET /v1/budget/cap_status", rl("budget_cap_status", BudgetCapStatus(srv)))
	mux.Handle("POST /v1/budget/record", rl("budget_record", BudgetRecord(srv)))
	mux.Handle("GET /v1/budget/axes", rl("budget_axes", BudgetAxes(srv)))
	mux.Handle("GET /v1/budget/anomaly", rl("budget_anomaly", BudgetAnomaly(srv)))
	mux.Handle("GET /v1/budget/events", rl("budget_events", BudgetEvents(srv)))
	mux.Handle("POST /v1/budget/pause", rl("budget_pause", BudgetPause(srv)))
	mux.Handle("POST /v1/budget/resume", rl("budget_resume", BudgetResume(srv)))
	mux.Handle("GET /v1/workforce/specs", rl("workforce_specs", WorkforceSpecs(srv)))
	mux.Handle("GET /v1/workforce/workers", rl("workforce_workers", WorkforceWorkers(srv)))
	mux.Handle("GET /v1/workforce/checkpoints", rl("workforce_checkpoints", WorkforceCheckpoints(srv)))
	mux.Handle("GET /v1/workforce/fix_prompts", rl("workforce_fix_prompts", WorkforceFixPrompts(srv)))
	mux.Handle("GET /v1/workforce/aggregations", rl("workforce_aggregations", WorkforceAggregations(srv)))
	mux.Handle("GET /v1/workforce/gate/state", rl("gate_state", OperatorGateState(srv)))
	mux.Handle("POST /v1/workforce/gate/pause", rl("gate_pause", OperatorGatePause(srv)))
	mux.Handle("POST /v1/workforce/gate/resume", rl("gate_resume", OperatorGateResume(srv)))

	return mux
}

func TestIntegration_ResearchCache_SetThenGet(t *testing.T) {
	srv := newIntegrationServer()
	ts := httptest.NewServer(buildIntegrationMux(srv))
	defer ts.Close()

	setBody := map[string]any{
		"hash":          "integ-hash-001",
		"response_json": `{"result":"integration-test"}`,
		"ttl_seconds":   604800,
	}
	b, _ := json.Marshal(setBody)
	resp, err := ts.Client().Post(ts.URL+"/v1/research/cache/set",
		"application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("set: want 201, got %d: %s", resp.StatusCode, body)
	}

	resp2, err := ts.Client().Get(ts.URL + "/v1/research/cache/get?hash=integ-hash-001")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp2.Body)
		t.Fatalf("get: want 200, got %d: %s", resp2.StatusCode, body)
	}
	var getResp map[string]any
	json.NewDecoder(resp2.Body).Decode(&getResp)
	if getResp["hit"] != true {
		t.Error("expected hit=true after set")
	}
}

func TestIntegration_AuditEmit_Accepted(t *testing.T) {
	srv := newIntegrationServer()
	ts := httptest.NewServer(buildIntegrationMux(srv))
	defer ts.Close()

	body := map[string]any{
		"project_id": "zen-swarm",
		"type":       "test.integration",
		"payload":    map[string]string{"key": "val"},
	}
	b, _ := json.Marshal(body)
	resp, err := ts.Client().Post(ts.URL+"/v1/audit/emit", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		body2, _ := io.ReadAll(resp.Body)
		t.Fatalf("want 202, got %d: %s", resp.StatusCode, body2)
	}
	if len(srv.auditLog) != 1 {
		t.Errorf("want 1 event in log, got %d", len(srv.auditLog))
	}
}

func TestIntegration_BudgetCapStatus(t *testing.T) {
	srv := newIntegrationServer()
	ts := httptest.NewServer(buildIntegrationMux(srv))
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/v1/budget/cap_status?axis=project&value=zen-swarm")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("want 200, got %d: %s", resp.StatusCode, body)
	}
}

func TestIntegration_WorkforceSpecs(t *testing.T) {
	srv := newIntegrationServer()
	ts := httptest.NewServer(buildIntegrationMux(srv))
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/v1/workforce/specs")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("want 200, got %d: %s", resp.StatusCode, body)
	}
	var r map[string]any
	json.NewDecoder(resp.Body).Decode(&r)
	if r["count"].(float64) < 1 {
		t.Errorf("want ≥1 spec, got count=%v", r["count"])
	}
}

func TestIntegration_GatePauseResumeCycle(t *testing.T) {
	srv := newIntegrationServer()
	ts := httptest.NewServer(buildIntegrationMux(srv))
	defer ts.Close()

	pauseBody := map[string]string{"mode": "paused_quiet", "reason": "integration test"}
	b, _ := json.Marshal(pauseBody)
	resp, err := ts.Client().Post(ts.URL+"/v1/workforce/gate/pause",
		"application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("pause: want 200, got %d", resp.StatusCode)
	}
	if srv.gateState != "paused_quiet" {
		t.Errorf("state after pause: %q", srv.gateState)
	}

	resp2, err := ts.Client().Post(ts.URL+"/v1/workforce/gate/resume",
		"application/json", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("resume: want 200, got %d", resp2.StatusCode)
	}
	if srv.gateState != "running" {
		t.Errorf("state after resume: %q", srv.gateState)
	}
}

func TestIntegration_RateLimiter_429OnFlood(t *testing.T) {

	bucket := newTokenBucket(1)
	handler := wrapBucket(bucket, okHandler)
	mux := http.NewServeMux()
	mux.Handle("GET /v1/test-rl-integ", handler)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	r1, _ := ts.Client().Get(ts.URL + "/v1/test-rl-integ")
	r1.Body.Close()
	if r1.StatusCode != http.StatusOK {
		t.Fatalf("first: want 200, got %d", r1.StatusCode)
	}
	r2, _ := ts.Client().Get(ts.URL + "/v1/test-rl-integ")
	r2.Body.Close()
	if r2.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("second: want 429, got %d", r2.StatusCode)
	}
	if r2.Header.Get("Retry-After") == "" {
		t.Error("Retry-After missing on 429")
	}
}
