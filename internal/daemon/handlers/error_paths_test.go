package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type errResearchCache struct{}

func (e *errResearchCache) ResearchCacheGet(hash string) (string, int64, bool, error) {
	return "", 0, false, errors.New("db error")
}
func (e *errResearchCache) ResearchCacheSet(hash, responseJSON string, ttlUnix int64) error {
	return errors.New("db write error")
}
func (e *errResearchCache) ResearchCacheTTL() time.Duration { return 7 * 24 * time.Hour }

func TestResearchCacheGet_DBError(t *testing.T) {
	h := ResearchCacheGet(&errResearchCache{})
	req := httptest.NewRequest(http.MethodGet, "/v1/research/cache/get?hash=abc", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d", w.Code)
	}
}

func TestResearchCacheSet_DBError(t *testing.T) {
	h := ResearchCacheSet(&errResearchCache{})
	body := map[string]any{
		"hash":          "k",
		"response_json": `{}`,
		"ttl_seconds":   3600,
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/research/cache/set", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d", w.Code)
	}
}

func TestResearchCacheSet_MissingHash(t *testing.T) {
	srv := &researchCacheServer{}
	h := ResearchCacheSet(srv)
	body := map[string]any{"response_json": `{}`}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/research/cache/set", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for missing hash, got %d", w.Code)
	}
}

func TestResearchCacheSet_DefaultTTL(t *testing.T) {

	srv := &researchCacheServer{}
	h := ResearchCacheSet(srv)
	body := map[string]any{
		"hash":          "ttl-default-key",
		"response_json": `{"ok":true}`,
		"ttl_seconds":   0,
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/research/cache/set", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d: %s", w.Code, w.Body.String())
	}
}

type errAuditEmit struct{}

func (e *errAuditEmit) AuditEmit(event AuditEventIn) error {
	return errors.New("queue full")
}

func TestAuditEmit_DBError(t *testing.T) {
	h := AuditEmit(&errAuditEmit{})
	body := map[string]any{"type": "test.err", "payload": map[string]string{}}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/audit/emit", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d", w.Code)
	}
}

type errBudget struct{}

func (b *errBudget) BudgetCapStatus(axis, value string) (BudgetCapStatusResult, error) {
	return BudgetCapStatusResult{}, errors.New("cap error")
}
func (b *errBudget) BudgetRecord(req BudgetRecordReq) error { return errors.New("record error") }
func (b *errBudget) BudgetAxes(costID string) ([]BudgetAxisTag, error) {
	return nil, errors.New("axes error")
}
func (b *errBudget) BudgetAnomalyCheck(scope, value string, windowSec int64) (BudgetAnomalyResult, error) {
	return BudgetAnomalyResult{}, errors.New("anomaly error")
}
func (b *errBudget) BudgetEvents(sinceUnix int64, limitN int) ([]BudgetEventRow, error) {
	return nil, errors.New("events error")
}
func (b *errBudget) BudgetPause(scope, value, reason string) (string, error) {
	return "", errors.New("pause error")
}
func (b *errBudget) BudgetResume(scope, value string) (string, error) {
	return "", errors.New("resume error")
}

func TestBudgetCapStatus_DBError(t *testing.T) {
	h := BudgetCapStatus(&errBudget{})
	req := httptest.NewRequest(http.MethodGet, "/v1/budget/cap_status?axis=a&value=v", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d", w.Code)
	}
}

func TestBudgetRecord_DBError(t *testing.T) {
	h := BudgetRecord(&errBudget{})
	body := BudgetRecordReq{CostID: "c1", AmountUSD: 1.0}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/budget/record", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d", w.Code)
	}
}

func TestBudgetRecord_InvalidJSON(t *testing.T) {
	h := BudgetRecord(&errBudget{})
	req := httptest.NewRequest(http.MethodPost, "/v1/budget/record", bytes.NewReader([]byte("bad")))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

func TestBudgetAxes_MissingCostID(t *testing.T) {
	h := BudgetAxes(&errBudget{})
	req := httptest.NewRequest(http.MethodGet, "/v1/budget/axes", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

func TestBudgetAxes_DBError(t *testing.T) {
	h := BudgetAxes(&errBudget{})
	req := httptest.NewRequest(http.MethodGet, "/v1/budget/axes?cost_id=c1", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d", w.Code)
	}
}

func TestBudgetAnomaly_MissingParams(t *testing.T) {
	h := BudgetAnomaly(&errBudget{})
	req := httptest.NewRequest(http.MethodGet, "/v1/budget/anomaly", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

func TestBudgetAnomaly_DBError(t *testing.T) {
	h := BudgetAnomaly(&errBudget{})
	req := httptest.NewRequest(http.MethodGet, "/v1/budget/anomaly?scope=s&value=v", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d", w.Code)
	}
}

func TestBudgetEvents_DBError(t *testing.T) {
	h := BudgetEvents(&errBudget{})
	req := httptest.NewRequest(http.MethodGet, "/v1/budget/events", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d", w.Code)
	}
}

func TestBudgetPause_InvalidJSON(t *testing.T) {
	h := BudgetPause(&errBudget{})
	req := httptest.NewRequest(http.MethodPost, "/v1/budget/pause", bytes.NewReader([]byte("bad")))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

func TestBudgetPause_MissingParams(t *testing.T) {
	h := BudgetPause(&errBudget{})
	body := map[string]string{"scope": "project"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/budget/pause", bytes.NewReader(b))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

func TestBudgetPause_DBError(t *testing.T) {
	h := BudgetPause(&errBudget{})
	body := map[string]string{"scope": "project", "value": "p"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/budget/pause", bytes.NewReader(b))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d", w.Code)
	}
}

func TestBudgetResume_InvalidJSON(t *testing.T) {
	h := BudgetResume(&errBudget{})
	req := httptest.NewRequest(http.MethodPost, "/v1/budget/resume", bytes.NewReader([]byte("bad")))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

func TestBudgetResume_MissingParams(t *testing.T) {
	h := BudgetResume(&errBudget{})
	body := map[string]string{"scope": "project"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/budget/resume", bytes.NewReader(b))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

func TestBudgetResume_DBError(t *testing.T) {
	h := BudgetResume(&errBudget{})
	body := map[string]string{"scope": "project", "value": "p"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/budget/resume", bytes.NewReader(b))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d", w.Code)
	}
}

type errWorkforce struct{}

func (e *errWorkforce) WorkforceSpecs(limit, offset int, filter string) ([]WorkerSpecRow, error) {
	return nil, errors.New("specs error")
}
func (e *errWorkforce) WorkforceWorkers(limit, offset int, status string) ([]WorkerRow, error) {
	return nil, errors.New("workers error")
}
func (e *errWorkforce) WorkforceCheckpoints(taskID string, limit, offset int) ([]CheckpointRow, error) {
	return nil, errors.New("checkpoints error")
}
func (e *errWorkforce) WorkforceFixPrompts(taskID string, limit, offset int) ([]FixPromptRow, error) {
	return nil, errors.New("fix_prompts error")
}
func (e *errWorkforce) WorkforceAggregations(layer string, windowSec int64, limit int) ([]AggregationRow, error) {
	return nil, errors.New("aggregations error")
}

func TestWorkforceSpecs_DBError(t *testing.T) {
	h := WorkforceSpecs(&errWorkforce{})
	req := httptest.NewRequest(http.MethodGet, "/v1/workforce/specs", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d", w.Code)
	}
}

func TestWorkforceWorkers_DBError(t *testing.T) {
	h := WorkforceWorkers(&errWorkforce{})
	req := httptest.NewRequest(http.MethodGet, "/v1/workforce/workers", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d", w.Code)
	}
}

func TestWorkforceCheckpoints_DBError(t *testing.T) {
	h := WorkforceCheckpoints(&errWorkforce{})
	req := httptest.NewRequest(http.MethodGet, "/v1/workforce/checkpoints", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d", w.Code)
	}
}

func TestWorkforceFixPrompts_DBError(t *testing.T) {
	h := WorkforceFixPrompts(&errWorkforce{})
	req := httptest.NewRequest(http.MethodGet, "/v1/workforce/fix_prompts", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d", w.Code)
	}
}

func TestWorkforceAggregations_DBError(t *testing.T) {
	h := WorkforceAggregations(&errWorkforce{})
	req := httptest.NewRequest(http.MethodGet, "/v1/workforce/aggregations", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d", w.Code)
	}
}

type errGate struct{}

func (g *errGate) OperatorGateState() (string, error) { return "", errors.New("state error") }
func (g *errGate) OperatorGatePause(mode, reason string) (string, error) {
	return "", errors.New("pause error")
}
func (g *errGate) OperatorGateResume() (string, error) { return "", errors.New("resume error") }

func TestOperatorGateState_DBError(t *testing.T) {
	h := OperatorGateState(&errGate{})
	req := httptest.NewRequest(http.MethodGet, "/v1/workforce/gate/state", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d", w.Code)
	}
}

func TestOperatorGatePause_DBError(t *testing.T) {
	h := OperatorGatePause(&errGate{})
	body := map[string]string{"mode": "paused_quiet"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/workforce/gate/pause", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d", w.Code)
	}
}

func TestOperatorGatePause_NoBody(t *testing.T) {

	srv := &operatorGateServer{state: "running"}
	h := OperatorGatePause(srv)
	req := httptest.NewRequest(http.MethodPost, "/v1/workforce/gate/pause", nil)
	req.ContentLength = 0
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestOperatorGateResume_DBError(t *testing.T) {
	h := OperatorGateResume(&errGate{})
	req := httptest.NewRequest(http.MethodPost, "/v1/workforce/gate/resume", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d", w.Code)
	}
}

func TestRateLimitMiddleware_AllowsAndBlocks(t *testing.T) {

	bucket := newTokenBucket(1)
	limited := wrapBucket(bucket, okHandler)

	req1 := httptest.NewRequest(http.MethodGet, "/", nil)
	w1 := httptest.NewRecorder()
	limited.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("first: want 200, got %d", w1.Code)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	w2 := httptest.NewRecorder()
	limited.ServeHTTP(w2, req2)
	if w2.Code != http.StatusTooManyRequests {
		t.Fatalf("second: want 429, got %d", w2.Code)
	}
	if w2.Header().Get("Retry-After") == "" {
		t.Error("Retry-After header missing on 429")
	}
}

func TestRateLimitMiddleware_ReusesRegistryBucket(t *testing.T) {

	registry := NewBucketRegistry()
	const ep = "test-rl-registry-reuse-key"
	srv := &rateLimitServer{thresholds: map[string]int{ep: 1000}}
	limited := RateLimitMiddleware(srv, registry, ep, okHandler)
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()
		limited.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("request %d: want 200, got %d", i, w.Code)
		}
	}
}
