package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/daemon/handlers"
)

type fakeDoctrineReader struct {
	enable    bool
	maxTokens int
	doctrine  string
	readErr   error
	lastProj  string
}

func (f *fakeDoctrineReader) AugmentationConfig(_ context.Context, project string) (handlers.AugmentationConfig, error) {
	f.lastProj = project
	if f.readErr != nil {
		return handlers.AugmentationConfig{}, f.readErr
	}
	return handlers.AugmentationConfig{
		Enable:       f.enable,
		MaxKGTokens:  f.maxTokens,
		DoctrineName: f.doctrine,
	}, nil
}

func TestAugmentHandlerEnabledReturnsEmptyEnvelope(t *testing.T) {
	dr := &fakeDoctrineReader{enable: true, maxTokens: 10000, doctrine: "default"}
	h := handlers.Augment(dr)

	body, _ := json.Marshal(handlers.AugmentRequest{
		SessionID:  "sess-1",
		Project:    "zen-swarm",
		Prompt:     "refactor MergeEngine",
		PromptHash: "abc123",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/augment", bytes.NewReader(body))
	w := httptest.NewRecorder()

	h(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Status = %d, want 200", w.Code)
	}
	var resp handlers.AugmentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.StaticContext != "" {
		t.Errorf("StaticContext = %q, want empty (Phase C fills)", resp.StaticContext)
	}
	if resp.VolatileContext != "" {
		t.Errorf("VolatileContext = %q, want empty (Phase C fills)", resp.VolatileContext)
	}
	if len(resp.Citations) != 0 {
		t.Errorf("Citations = %v, want empty slice", resp.Citations)
	}
	if resp.Doctrine != "default" {
		t.Errorf("Doctrine echo = %q, want default", resp.Doctrine)
	}
	if resp.MaxKGTokens != 10000 {
		t.Errorf("MaxKGTokens echo = %d, want 10000", resp.MaxKGTokens)
	}
	if dr.lastProj != "zen-swarm" {
		t.Errorf("doctrine reader saw project %q, want zen-swarm", dr.lastProj)
	}
}

func TestAugmentHandlerCapaFirewallReturns204(t *testing.T) {

	dr := &fakeDoctrineReader{enable: false, doctrine: "capa-firewall"}
	h := handlers.Augment(dr)

	body, _ := json.Marshal(handlers.AugmentRequest{
		SessionID: "sess-1",
		Project:   "secret-project",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/augment", bytes.NewReader(body))
	w := httptest.NewRecorder()

	h(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("Status = %d, want 204 (capa-firewall augmentation disabled)", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Errorf("body must be empty for 204; got %q", w.Body.String())
	}
}

func TestAugmentHandlerMethodNotAllowed(t *testing.T) {
	h := handlers.Augment(&fakeDoctrineReader{enable: true})
	req := httptest.NewRequest(http.MethodGet, "/v1/augment", nil)
	w := httptest.NewRecorder()
	h(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Status = %d, want 405", w.Code)
	}
}

func TestAugmentHandlerMalformedJSON(t *testing.T) {
	h := handlers.Augment(&fakeDoctrineReader{enable: true})
	req := httptest.NewRequest(http.MethodPost, "/v1/augment", bytes.NewReader([]byte("nope{{")))
	w := httptest.NewRecorder()
	h(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want 400", w.Code)
	}
}

func TestAugmentHandlerDoctrineReadError(t *testing.T) {
	dr := &fakeDoctrineReader{readErr: errIntentional}
	h := handlers.Augment(dr)
	body, _ := json.Marshal(handlers.AugmentRequest{Project: "p"})
	req := httptest.NewRequest(http.MethodPost, "/v1/augment", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("Status = %d, want 500", w.Code)
	}
}

var errIntentional = stringErr("intentional fake doctrine read failure")

type stringErr string

func (e stringErr) Error() string { return string(e) }

func TestAugmentHandlerEmptyProjectAccepted(t *testing.T) {

	dr := &fakeDoctrineReader{enable: true, maxTokens: 10000, doctrine: "default"}
	h := handlers.Augment(dr)
	body, _ := json.Marshal(handlers.AugmentRequest{SessionID: "sess-1"})
	req := httptest.NewRequest(http.MethodPost, "/v1/augment", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want 200 (empty project allowed)", w.Code)
	}
	if dr.lastProj != "default" {
		t.Errorf("empty project must resolve to 'default'; doctrine reader saw %q", dr.lastProj)
	}
}

func TestAugmentHandlerEnvelopeJSONRoundTrip(t *testing.T) {
	envelope := handlers.AugmentResponse{
		StaticContext:   "system context",
		VolatileContext: "user context",
		Doctrine:        "default",
		Citations: []handlers.Citation{
			{ID: "c1", SourceTool: "caronte.query", Confidence: 0.94, Snippet: "// MergeEngine"},
		},
		MaxKGTokens: 10000,
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded handlers.AugmentResponse
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.StaticContext != envelope.StaticContext ||
		decoded.VolatileContext != envelope.VolatileContext ||
		decoded.Doctrine != envelope.Doctrine ||
		len(decoded.Citations) != 1 ||
		decoded.Citations[0].ID != "c1" ||
		decoded.MaxKGTokens != 10000 {
		t.Errorf("round-trip drift: encoded=%q decoded=%#v", string(encoded), decoded)
	}
}

func TestAugmentNilDoctrineReaderPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("handlers.Augment(nil) must panic at construction (fail-fast wiring bug surfacing)")
		}
	}()
	_ = handlers.Augment(nil)
}

func TestAugmentHandlerBodyTooLargeRejected(t *testing.T) {
	h := handlers.Augment(&fakeDoctrineReader{enable: true})

	huge := bytes.Repeat([]byte("x"), (4<<20)+1024)
	req := httptest.NewRequest(http.MethodPost, "/v1/augment", bytes.NewReader(huge))
	w := httptest.NewRecorder()
	h(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want 400 (body too large)", w.Code)
	}
}

func TestAugmentRequestDecodesConversationID(t *testing.T) {
	tests := []struct {
		name           string
		body           string
		wantConvID     string
		wantSessionID  string
		wantProject    string
		wantPromptHash string
		wantMode       string
	}{
		{
			name:           "full envelope with conversation_id populated",
			body:           `{"session_id":"sess-1","conversation_id":"conv-42","project":"zen-swarm","prompt":"hi","prompt_hash":"abc","mode":"interactive"}`,
			wantConvID:     "conv-42",
			wantSessionID:  "sess-1",
			wantProject:    "zen-swarm",
			wantPromptHash: "abc",
			wantMode:       "interactive",
		},
		{
			name:           "empty conversation_id present",
			body:           `{"session_id":"sess-2","conversation_id":"","project":"p","prompt":"q","prompt_hash":"d"}`,
			wantConvID:     "",
			wantSessionID:  "sess-2",
			wantProject:    "p",
			wantPromptHash: "d",
		},
		{
			name:          "conversation_id absent (omitted) defaults to zero",
			body:          `{"session_id":"sess-3","project":"p"}`,
			wantConvID:    "",
			wantSessionID: "sess-3",
			wantProject:   "p",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var req handlers.AugmentRequest
			if err := json.Unmarshal([]byte(tc.body), &req); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if req.ConversationID != tc.wantConvID {
				t.Errorf("ConversationID = %q, want %q", req.ConversationID, tc.wantConvID)
			}
			if req.SessionID != tc.wantSessionID {
				t.Errorf("SessionID = %q, want %q", req.SessionID, tc.wantSessionID)
			}
			if req.Project != tc.wantProject {
				t.Errorf("Project = %q, want %q", req.Project, tc.wantProject)
			}
			if req.PromptHash != tc.wantPromptHash {
				t.Errorf("PromptHash = %q, want %q", req.PromptHash, tc.wantPromptHash)
			}
			if req.Mode != tc.wantMode {
				t.Errorf("Mode = %q, want %q", req.Mode, tc.wantMode)
			}
		})
	}
}

type captureDoctrineReader struct {
	enable    bool
	maxTokens int
	doctrine  string
	called    bool
}

func (c *captureDoctrineReader) AugmentationConfig(_ context.Context, _ string) (handlers.AugmentationConfig, error) {
	c.called = true
	return handlers.AugmentationConfig{
		Enable:       c.enable,
		MaxKGTokens:  c.maxTokens,
		DoctrineName: c.doctrine,
	}, nil
}

func TestAugmentHandlerEndToEndConversationIDFlow(t *testing.T) {
	dr := &captureDoctrineReader{enable: true, maxTokens: 10000, doctrine: "default"}
	h := handlers.Augment(dr)

	bodyJSON := `{"session_id":"sess-X","conversation_id":"conv-Y","project":"zen-swarm","prompt":"refactor MergeEngine","prompt_hash":"d4","mode":"interactive"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/augment", bytes.NewReader([]byte(bodyJSON)))
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Status = %d, want 200", w.Code)
	}
	if !dr.called {
		t.Errorf("doctrine reader was not called")
	}

	var decoded handlers.AugmentRequest
	if err := json.Unmarshal([]byte(bodyJSON), &decoded); err != nil {
		t.Fatalf("post-hoc unmarshal: %v", err)
	}
	if decoded.ConversationID != "conv-Y" {
		t.Fatalf("ConversationID lost during round-trip: got %q", decoded.ConversationID)
	}
}
