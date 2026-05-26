package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/daemon/handlers"
)

func TestAugmentWithPipeline_HappyPath(t *testing.T) {
	dr := &fakeDoctrineReader{enable: true, maxTokens: 10000, doctrine: "max-scope"}
	runner := func(_ context.Context, req handlers.PipelineRequest) (handlers.PipelineResponse, error) {
		if req.Doctrine != "max-scope" {
			t.Errorf("expected doctrine=max-scope, got %q", req.Doctrine)
		}
		return handlers.PipelineResponse{
			StaticContext:   `{"project_meta":{"project_id":"internal-platform-x"}}`,
			VolatileContext: `{"fused_results":[]}`,
			Citations:       []byte(`[{"id":"c-abc","source":"aggregator_fts","confidence":0.85,"payload":"snip","audit_event_id":"evt-1","project_id":"internal-platform-x"}]`),
			AuditEventID:    "evt-anchor",
		}, nil
	}
	h := handlers.AugmentWithPipeline(dr, runner)

	body, _ := json.Marshal(handlers.AugmentRequest{
		SessionID: "sess-1",
		Project:   "internal-platform-x",
		Prompt:    "refactor",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/augment", bytes.NewReader(body))
	w := httptest.NewRecorder()

	h(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", w.Code)
	}
	var resp handlers.AugmentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.StaticContext == "" {
		t.Error("expected populated StaticContext")
	}
	if len(resp.Citations) != 1 || resp.Citations[0].ID != "c-abc" {
		t.Errorf("citations: want [c-abc], got %v", resp.Citations)
	}
	if resp.AuditEventID != "evt-anchor" {
		t.Errorf("audit_event_id: want evt-anchor, got %q", resp.AuditEventID)
	}
}

func TestAugmentWithPipeline_DoctrineGateReturns204(t *testing.T) {
	dr := &fakeDoctrineReader{enable: false, doctrine: "capa-firewall"}
	runner := func(_ context.Context, _ handlers.PipelineRequest) (handlers.PipelineResponse, error) {
		t.Error("runner should not be called when DoctrineReader.Enable=false")
		return handlers.PipelineResponse{}, nil
	}
	h := handlers.AugmentWithPipeline(dr, runner)

	body, _ := json.Marshal(handlers.AugmentRequest{Project: "internal-platform-x", Prompt: "x"})
	req := httptest.NewRequest(http.MethodPost, "/v1/augment", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("status: want 204, got %d", w.Code)
	}
}

func TestAugmentWithPipeline_PipelineSkipReturns204(t *testing.T) {
	dr := &fakeDoctrineReader{enable: true, maxTokens: 10000, doctrine: "max-scope"}
	runner := func(_ context.Context, _ handlers.PipelineRequest) (handlers.PipelineResponse, error) {
		return handlers.PipelineResponse{SkippedReason: "budget-cap"}, nil
	}
	h := handlers.AugmentWithPipeline(dr, runner)
	body, _ := json.Marshal(handlers.AugmentRequest{Project: "internal-platform-x", Prompt: "x"})
	req := httptest.NewRequest(http.MethodPost, "/v1/augment", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h(w, req)
	if w.Code != http.StatusNoContent {
		t.Errorf("status: want 204, got %d", w.Code)
	}
}

func TestAugmentWithPipeline_PipelineErrorReturns500(t *testing.T) {
	dr := &fakeDoctrineReader{enable: true, maxTokens: 10000, doctrine: "max-scope"}
	runner := func(_ context.Context, _ handlers.PipelineRequest) (handlers.PipelineResponse, error) {
		return handlers.PipelineResponse{}, errors.New("pipeline boom")
	}
	h := handlers.AugmentWithPipeline(dr, runner)
	body, _ := json.Marshal(handlers.AugmentRequest{Project: "internal-platform-x", Prompt: "x"})
	req := httptest.NewRequest(http.MethodPost, "/v1/augment", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status: want 500, got %d", w.Code)
	}
}

func TestAugmentWithPipeline_NilDoctrinePanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on nil DoctrineReader")
		}
	}()
	_ = handlers.AugmentWithPipeline(nil, func(_ context.Context, _ handlers.PipelineRequest) (handlers.PipelineResponse, error) {
		return handlers.PipelineResponse{}, nil
	})
}

func TestAugmentWithPipeline_NilRunnerPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on nil runner")
		}
	}()
	_ = handlers.AugmentWithPipeline(&fakeDoctrineReader{}, nil)
}

func TestAugmentWithPipeline_MethodNotAllowed(t *testing.T) {
	dr := &fakeDoctrineReader{enable: true, maxTokens: 10000, doctrine: "max-scope"}
	runner := func(_ context.Context, _ handlers.PipelineRequest) (handlers.PipelineResponse, error) {
		return handlers.PipelineResponse{}, nil
	}
	h := handlers.AugmentWithPipeline(dr, runner)
	req := httptest.NewRequest(http.MethodGet, "/v1/augment", nil)
	w := httptest.NewRecorder()
	h(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status: want 405, got %d", w.Code)
	}
}

func TestAugmentWithPipeline_MalformedBodyReturns400(t *testing.T) {
	dr := &fakeDoctrineReader{enable: true, maxTokens: 10000, doctrine: "max-scope"}
	runner := func(_ context.Context, _ handlers.PipelineRequest) (handlers.PipelineResponse, error) {
		return handlers.PipelineResponse{}, nil
	}
	h := handlers.AugmentWithPipeline(dr, runner)
	req := httptest.NewRequest(http.MethodPost, "/v1/augment", bytes.NewReader([]byte("nope{{")))
	w := httptest.NewRecorder()
	h(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status: want 400, got %d", w.Code)
	}
}

func TestAugmentWithPipeline_DoctrineReadError(t *testing.T) {
	dr := &fakeDoctrineReader{readErr: errors.New("dr down")}
	runner := func(_ context.Context, _ handlers.PipelineRequest) (handlers.PipelineResponse, error) {
		return handlers.PipelineResponse{}, nil
	}
	h := handlers.AugmentWithPipeline(dr, runner)
	body, _ := json.Marshal(handlers.AugmentRequest{Project: "p", Prompt: "x"})
	req := httptest.NewRequest(http.MethodPost, "/v1/augment", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status: want 500, got %d", w.Code)
	}
}

func TestAugmentWithPipeline_DefaultProject(t *testing.T) {
	dr := &fakeDoctrineReader{enable: true, maxTokens: 10000, doctrine: "default"}
	runner := func(_ context.Context, req handlers.PipelineRequest) (handlers.PipelineResponse, error) {
		if req.ProjectID != "default" {
			t.Errorf("expected default project, got %q", req.ProjectID)
		}
		return handlers.PipelineResponse{StaticContext: "{}", VolatileContext: "{}", Citations: nil}, nil
	}
	h := handlers.AugmentWithPipeline(dr, runner)
	body, _ := json.Marshal(handlers.AugmentRequest{Project: "", Prompt: "x"})
	req := httptest.NewRequest(http.MethodPost, "/v1/augment", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("status: want 200, got %d", w.Code)
	}
}

func TestAugmentWithPipeline_RequestIDFromPromptHash(t *testing.T) {
	dr := &fakeDoctrineReader{enable: true, maxTokens: 10000, doctrine: "max-scope"}
	runner := func(_ context.Context, req handlers.PipelineRequest) (handlers.PipelineResponse, error) {
		if req.RequestID != "abc123" {
			t.Errorf("expected RequestID=abc123, got %q", req.RequestID)
		}
		return handlers.PipelineResponse{StaticContext: "{}", VolatileContext: "{}"}, nil
	}
	h := handlers.AugmentWithPipeline(dr, runner)
	body, _ := json.Marshal(handlers.AugmentRequest{
		Project:    "p",
		Prompt:     "x",
		PromptHash: "abc123",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/augment", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h(w, req)
}

func TestAugmentWithPipeline_RequestIDFromSession(t *testing.T) {
	dr := &fakeDoctrineReader{enable: true, maxTokens: 10000, doctrine: "max-scope"}
	runner := func(_ context.Context, req handlers.PipelineRequest) (handlers.PipelineResponse, error) {
		if req.RequestID == "" {
			t.Error("expected non-empty RequestID")
		}
		return handlers.PipelineResponse{StaticContext: "{}", VolatileContext: "{}"}, nil
	}
	h := handlers.AugmentWithPipeline(dr, runner)
	body, _ := json.Marshal(handlers.AugmentRequest{
		Project:        "p",
		Prompt:         "x",
		SessionID:      "sess-x",
		ConversationID: "conv-y",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/augment", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h(w, req)
}

func TestAugmentWithPipeline_MalformedCitationsJSON(t *testing.T) {
	dr := &fakeDoctrineReader{enable: true, maxTokens: 10000, doctrine: "max-scope"}
	runner := func(_ context.Context, _ handlers.PipelineRequest) (handlers.PipelineResponse, error) {
		return handlers.PipelineResponse{
			StaticContext:   "{}",
			VolatileContext: "{}",
			Citations:       []byte("not-json"),
		}, nil
	}
	h := handlers.AugmentWithPipeline(dr, runner)
	body, _ := json.Marshal(handlers.AugmentRequest{Project: "p", Prompt: "x"})
	req := httptest.NewRequest(http.MethodPost, "/v1/augment", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h(w, req)

	var resp handlers.AugmentResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Citations) != 0 {
		t.Errorf("expected empty citations for malformed JSON, got %d", len(resp.Citations))
	}
}

func TestAugmentWithPipeline_BodyReadError(t *testing.T) {
	dr := &fakeDoctrineReader{enable: true, maxTokens: 10000, doctrine: "max-scope"}
	runner := func(_ context.Context, _ handlers.PipelineRequest) (handlers.PipelineResponse, error) {
		return handlers.PipelineResponse{}, nil
	}
	h := handlers.AugmentWithPipeline(dr, runner)

	req := httptest.NewRequest(http.MethodPost, "/v1/augment", &erroringReader{err: errors.New("read fail")})
	w := httptest.NewRecorder()
	h(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d; want 400 on body-read error", w.Code)
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("read body")) {
		t.Errorf("body=%q; want it to mention read-body failure", w.Body.String())
	}
}

type erroringReader struct{ err error }

func (e *erroringReader) Read(_ []byte) (int, error) { return 0, e.err }

func TestAugmentWithPipeline_RequestIDFallbackNeverCollides(t *testing.T) {
	dr := &threadSafeDoctrineReader{enable: true, maxTokens: 10000, doctrine: "max-scope"}
	var mu sync.Mutex
	seenIDs := map[string]int{}
	runner := func(_ context.Context, req handlers.PipelineRequest) (handlers.PipelineResponse, error) {
		mu.Lock()
		seenIDs[req.RequestID]++
		mu.Unlock()
		return handlers.PipelineResponse{StaticContext: "{}", VolatileContext: "{}"}, nil
	}
	h := handlers.AugmentWithPipeline(dr, runner)

	const n = 20
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			body, _ := json.Marshal(handlers.AugmentRequest{
				Project: "p",
				Prompt:  "x",
			})
			req := httptest.NewRequest(http.MethodPost, "/v1/augment", bytes.NewReader(body))
			w := httptest.NewRecorder()
			h(w, req)
		}()
	}
	wg.Wait()

	if len(seenIDs) < n {
		t.Errorf("only %d distinct RequestIDs across %d concurrent calls; want %d (UUID fallback must not collide)",
			len(seenIDs), n, n)

		for id, count := range seenIDs {
			if count > 1 {
				t.Logf("duplicate RequestID %q observed %d times", id, count)
			}
		}
	}
}

type threadSafeDoctrineReader struct {
	enable    bool
	maxTokens int
	doctrine  string
}

func (r *threadSafeDoctrineReader) AugmentationConfig(_ context.Context, _ string) (handlers.AugmentationConfig, error) {
	return handlers.AugmentationConfig{
		Enable:       r.enable,
		MaxKGTokens:  r.maxTokens,
		DoctrineName: r.doctrine,
	}, nil
}

func TestAugmentWithPipeline_RequestIDFallbackShape(t *testing.T) {
	dr := &fakeDoctrineReader{enable: true, maxTokens: 10000, doctrine: "max-scope"}
	var captured string
	runner := func(_ context.Context, req handlers.PipelineRequest) (handlers.PipelineResponse, error) {
		captured = req.RequestID
		return handlers.PipelineResponse{StaticContext: "{}", VolatileContext: "{}"}, nil
	}
	h := handlers.AugmentWithPipeline(dr, runner)
	body, _ := json.Marshal(handlers.AugmentRequest{Project: "p", Prompt: "x"})
	req := httptest.NewRequest(http.MethodPost, "/v1/augment", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h(w, req)

	if captured == "augment-no-id" {
		t.Error("RequestID=augment-no-id; want UUID-shaped fallback")
	}
	if len(captured) < len("augment-") {
		t.Errorf("RequestID=%q too short; want \"augment-<nano>-<hex(16)>\"", captured)
	}
	if captured[:8] != "augment-" {
		t.Errorf("RequestID=%q missing \"augment-\" prefix", captured)
	}
}

func TestAugmentWithPipeline_CitationNonStringFields(t *testing.T) {
	dr := &fakeDoctrineReader{enable: true, maxTokens: 10000, doctrine: "max-scope"}
	runner := func(_ context.Context, _ handlers.PipelineRequest) (handlers.PipelineResponse, error) {

		return handlers.PipelineResponse{
			StaticContext:   "{}",
			VolatileContext: "{}",
			Citations:       []byte(`[{"id":123,"source":true,"payload":456,"confidence":0.7,"project_id":[],"audit_event_id":null}]`),
		}, nil
	}
	h := handlers.AugmentWithPipeline(dr, runner)
	body, _ := json.Marshal(handlers.AugmentRequest{Project: "p", Prompt: "x"})
	req := httptest.NewRequest(http.MethodPost, "/v1/augment", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d; want 200", w.Code)
	}
	var resp handlers.AugmentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Citations) != 1 {
		t.Fatalf("citations len=%d; want 1", len(resp.Citations))
	}

	c := resp.Citations[0]
	if c.ID != "" || c.SourceTool != "" || c.Snippet != "" || c.AuditEventID != "" || c.Project != "" {
		t.Errorf("non-string fields should fall back to \"\"; got %+v", c)
	}

	if c.Confidence != 0.7 {
		t.Errorf("confidence=%v; want 0.7 (float was correctly typed)", c.Confidence)
	}
}
