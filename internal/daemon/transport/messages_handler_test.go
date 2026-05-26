package transport_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/daemon/transport"
	"github.com/cbip-solutions/hades-system/internal/providers"
)

type recordingAnchor struct {
	calls []anchorCall
	err   error
	id    string
}

type anchorCall struct {
	eventType string
	payload   map[string]any
}

func (r *recordingAnchor) Emit(_ context.Context, eventType string, payload map[string]any) (string, error) {
	r.calls = append(r.calls, anchorCall{eventType: eventType, payload: payload})
	return r.id, r.err
}

func TestMessagesHandlerServeHTTPSuccess(t *testing.T) {
	disp := &fakeDispatcher{
		resp: &providers.TierResponse{
			Status:       200,
			Body:         []byte(`{"id":"msg_TEST","content":[{"type":"text","text":"hi"}]}`),
			TierUsed:     providers.TierInHouse,
			ModelUsed:    "claude-sonnet-4-6",
			InputTokens:  10,
			OutputTokens: 5,
			LatencyMs:    42,
		},
	}
	anchor := &recordingAnchor{id: "evt-001"}
	h := transport.NewMessagesHandler(disp, anchor)

	body, _ := json.Marshal(transport.ForwardedRequest{
		Body:            []byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hi"}]}`),
		SessionID:       "sess-1",
		Profile:         "orchestrator",
		Project:         "zen-swarm",
		Model:           "claude-sonnet-4-6",
		TransportSource: "zenswarm",
	})

	srv := httptest.NewServer(h)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Zen-Transport", "zenswarm")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("Status = %d, want 200", resp.StatusCode)
	}
	respBody, _ := io.ReadAll(resp.Body)
	var fr transport.ForwardedResponse
	if err := json.Unmarshal(respBody, &fr); err != nil {
		t.Fatalf("decode response: %v (raw=%q)", err, string(respBody))
	}
	if fr.Status != 200 {
		t.Errorf("ForwardedResponse.Status = %d, want 200", fr.Status)
	}
	if !strings.Contains(fr.Body, "msg_TEST") {
		t.Errorf("response body missing canned text; got %q", fr.Body)
	}
	if fr.AuditEventID != "evt-001" {
		t.Errorf("AuditEventID = %q, want evt-001", fr.AuditEventID)
	}
	if len(anchor.calls) != 1 {
		t.Fatalf("anchor.calls = %d, want 1", len(anchor.calls))
	}
	if anchor.calls[0].eventType != "MessageForwarded" {
		t.Errorf("eventType = %q, want MessageForwarded", anchor.calls[0].eventType)
	}
	if disp.calls != 1 {
		t.Errorf("dispatcher.calls = %d, want 1", disp.calls)
	}

	if disp.lastReq.SessionID != "sess-1" {
		t.Errorf("dispatcher SessionID = %q, want sess-1", disp.lastReq.SessionID)
	}
	if disp.lastReq.Profile != "orchestrator" {
		t.Errorf("dispatcher Profile = %q, want orchestrator", disp.lastReq.Profile)
	}
	if disp.lastReq.Project != "zen-swarm" {
		t.Errorf("dispatcher Project = %q, want zen-swarm", disp.lastReq.Project)
	}
	if disp.lastReq.Model != "claude-sonnet-4-6" {
		t.Errorf("dispatcher Model = %q, want claude-sonnet-4-6", disp.lastReq.Model)
	}

	pl := anchor.calls[0].payload
	if pl["session_id"] != "sess-1" {
		t.Errorf("payload session_id = %v, want sess-1", pl["session_id"])
	}
	if pl["transport_source"] != "zenswarm-transport" {
		t.Errorf("payload transport_source = %v, want zenswarm-transport", pl["transport_source"])
	}
	if pl["status"] != 200 {
		t.Errorf("payload status = %v, want 200", pl["status"])
	}
}

func TestMessagesHandlerServeHTTPDispatcherError(t *testing.T) {
	disp := &fakeDispatcher{err: errors.New("upstream-down")}
	anchor := &recordingAnchor{id: "evt-fail-001"}
	h := transport.NewMessagesHandler(disp, anchor)

	body, _ := json.Marshal(transport.ForwardedRequest{Body: []byte(`{}`)})
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	req.Header.Set("X-Zen-Transport", "zenswarm")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("Status = %d, want 502 (upstream dispatcher failure)", w.Code)
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("upstream-down")) {
		t.Errorf("body missing upstream error text; got %q", w.Body.String())
	}

	if len(anchor.calls) != 1 {
		t.Fatalf("failure path anchor.calls = %d, want 1", len(anchor.calls))
	}
	if anchor.calls[0].eventType != "MessageForwardFailed" {
		t.Errorf("failure eventType = %q, want MessageForwardFailed", anchor.calls[0].eventType)
	}
}

func TestMessagesHandlerServeHTTPDispatcherReturnsNilNilResponse(t *testing.T) {

	disp := &fakeDispatcher{resp: nil, err: nil}
	h := transport.NewMessagesHandler(disp, nil)

	body, _ := json.Marshal(transport.ForwardedRequest{Body: []byte(`{}`)})
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Status = %d, want 500 (contract violation: nil resp + nil err)", w.Code)
	}
}

func TestMessagesHandlerServeHTTPMethodNotAllowed(t *testing.T) {
	h := transport.NewMessagesHandler(&fakeDispatcher{}, nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/messages", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Status = %d, want 405", w.Code)
	}
}

func TestMessagesHandlerServeHTTPMalformedJSON(t *testing.T) {
	h := transport.NewMessagesHandler(&fakeDispatcher{}, nil)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader([]byte("not-json{{")))
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want 400", w.Code)
	}
}

func TestMessagesHandlerServeHTTPNilAnchorGracefulDegradation(t *testing.T) {
	disp := &fakeDispatcher{
		resp: &providers.TierResponse{Status: 200, Body: []byte(`{}`)},
	}
	h := transport.NewMessagesHandler(disp, nil)

	body, _ := json.Marshal(transport.ForwardedRequest{Body: []byte(`{}`)})
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	req.Header.Set("X-Zen-Transport", "zenswarm")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("Status = %d, want 200 (audit chain offline must not block forwarding)", w.Code)
	}

	var fr transport.ForwardedResponse
	if err := json.Unmarshal(w.Body.Bytes(), &fr); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if fr.AuditEventID != "" {
		t.Errorf("AuditEventID = %q, want empty (anchor offline)", fr.AuditEventID)
	}
}

func TestMessagesHandlerServeHTTPAnchorEmitErrorNonFatal(t *testing.T) {
	disp := &fakeDispatcher{
		resp: &providers.TierResponse{Status: 200, Body: []byte(`{"ok":true}`)},
	}
	anchor := &recordingAnchor{err: errors.New("anchor-down"), id: ""}
	h := transport.NewMessagesHandler(disp, anchor)

	body, _ := json.Marshal(transport.ForwardedRequest{Body: []byte(`{}`)})
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	req.Header.Set("X-Zen-Transport", "zenswarm")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("Status = %d, want 200 (anchor.Emit failure must not block forwarding)", w.Code)
	}
	if len(anchor.calls) != 1 {
		t.Errorf("anchor calls = %d, want 1 (Emit attempted)", len(anchor.calls))
	}
}

func TestMessagesHandlerServeHTTPNonZenSwarmTransportSkipsAnchor(t *testing.T) {
	// Calls without X-Zen-Transport: zenswarm header (CLI / MCP origin) MUST
	// NOT trigger a Plan 9 anchor emit (CLI has its own audit path).
	disp := &fakeDispatcher{
		resp: &providers.TierResponse{Status: 200, Body: []byte(`{}`)},
	}
	anchor := &recordingAnchor{id: "evt-cli-001"}
	h := transport.NewMessagesHandler(disp, anchor)

	body, _ := json.Marshal(transport.ForwardedRequest{Body: []byte(`{}`)})
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))

	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("Status = %d, want 200", w.Code)
	}
	if len(anchor.calls) != 0 {
		t.Errorf("non-zenswarm call must not emit anchor; got %d calls", len(anchor.calls))
	}
}

func TestMessagesHandlerServeHTTPDropsSecretHeaders(t *testing.T) {
	disp := &fakeDispatcher{
		resp: &providers.TierResponse{
			Status: 200,
			Body:   []byte(`{}`),
			Headers: map[string]string{
				"X-RateLimit-Remaining": "100",
				"Authorization":         "Bearer SHOULD-NEVER-SURFACE",
				"X-Api-Key":             "leak",
			},
		},
	}
	h := transport.NewMessagesHandler(disp, nil)

	fwd := transport.ForwardedRequest{
		Body: []byte(`{}`),
		Headers: map[string]string{
			"Content-Type":     "application/json",
			"Authorization":    "Bearer DROP-ME",
			"x-api-key":        "drop-me-too",
			"X-Anthropic-Beta": "tools-2025-05",
		},
	}
	body, _ := json.Marshal(fwd)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("Status = %d, want 200", w.Code)
	}

	for _, secret := range []string{"Authorization", "X-Api-Key"} {
		if _, ok := disp.lastReq.Headers[secret]; ok {
			t.Errorf("forbidden header %q reached dispatcher (defence-in-depth failure)", secret)
		}

		if _, ok := disp.lastReq.Headers["authorization"]; ok && secret == "Authorization" {
			t.Errorf("forbidden header authorization reached dispatcher")
		}
	}

	if got := disp.lastReq.Headers["X-Anthropic-Beta"]; got != "tools-2025-05" {
		t.Errorf("X-Anthropic-Beta = %q, want tools-2025-05", got)
	}

	var fr transport.ForwardedResponse
	if err := json.Unmarshal(w.Body.Bytes(), &fr); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if _, ok := fr.Headers["Authorization"]; ok {
		t.Error("response Authorization header leaked to Python side")
	}
	if _, ok := fr.Headers["X-Api-Key"]; ok {
		t.Error("response X-Api-Key header leaked to Python side")
	}
	if got := fr.Headers["X-RateLimit-Remaining"]; got != "100" {
		t.Errorf("X-RateLimit-Remaining = %q, want 100", got)
	}
}

func TestMessagesHandlerServeHTTPLiftsIdempotencyHeader(t *testing.T) {

	disp := &fakeDispatcher{
		resp: &providers.TierResponse{Status: 200, Body: []byte(`{}`)},
	}
	h := transport.NewMessagesHandler(disp, nil)

	body, _ := json.Marshal(transport.ForwardedRequest{Body: []byte(`{}`)})
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	req.Header.Set("X-Zen-Idempotency-Key", "uuid-header-key")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("Status = %d", w.Code)
	}
	if disp.lastReq.IdempotencyKey != "uuid-header-key" {
		t.Errorf("dispatcher IdempotencyKey = %q, want uuid-header-key (lifted from HTTP header)", disp.lastReq.IdempotencyKey)
	}
}

func TestMessagesHandlerServeHTTPBodyEnvelopeIdempotencyWins(t *testing.T) {

	disp := &fakeDispatcher{
		resp: &providers.TierResponse{Status: 200, Body: []byte(`{}`)},
	}
	h := transport.NewMessagesHandler(disp, nil)

	body, _ := json.Marshal(transport.ForwardedRequest{
		Body:           []byte(`{}`),
		IdempotencyKey: "uuid-from-body",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	req.Header.Set("X-Zen-Idempotency-Key", "uuid-from-header")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("Status = %d", w.Code)
	}
	if disp.lastReq.IdempotencyKey != "uuid-from-body" {
		t.Errorf("dispatcher IdempotencyKey = %q, want uuid-from-body (envelope authoritative)", disp.lastReq.IdempotencyKey)
	}
}

func TestMessagesHandlerNilDispatcherPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("NewMessagesHandler(nil, _) must panic")
		}
	}()
	_ = transport.NewMessagesHandler(nil, nil)
}

func TestMessagesHandlerBodyTooLargeRejected(t *testing.T) {
	h := transport.NewMessagesHandler(&fakeDispatcher{}, nil)

	huge := bytes.Repeat([]byte("x"), (32<<20)+1024)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(huge))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want 400 (body too large)", w.Code)
	}
}

func TestMessagesHandlerServeHTTPBodyAsNestedJSONObject(t *testing.T) {
	// The Python side may send the inner body either as a JSON string (the
	// default — json.dumps(inner_body)) or as a nested JSON object. The
	// handler MUST accept both forms transparently.
	disp := &fakeDispatcher{
		resp: &providers.TierResponse{Status: 200, Body: []byte(`{"ok":1}`)},
	}
	h := transport.NewMessagesHandler(disp, nil)

	rawEnvelope := []byte(`{
		"body": {"model": "claude-sonnet-4-6", "messages": [{"role":"user","content":"hi"}]},
		"session_id": "sess-nested"
	}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(rawEnvelope))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("Status = %d, want 200 (nested-JSON body form accepted)", w.Code)
	}
	if !bytes.Contains(disp.lastReq.Body, []byte("claude-sonnet-4-6")) {
		t.Errorf("dispatcher body missing inner content; got %q", string(disp.lastReq.Body))
	}
	if disp.lastReq.SessionID != "sess-nested" {
		t.Errorf("SessionID = %q, want sess-nested", disp.lastReq.SessionID)
	}
}

func TestMessagesHandlerServeHTTPBodyAsArrayPassthrough(t *testing.T) {

	disp := &fakeDispatcher{
		resp: &providers.TierResponse{Status: 200, Body: []byte(`{}`)},
	}
	h := transport.NewMessagesHandler(disp, nil)
	rawEnvelope := []byte(`{"body": [1, 2, 3]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(rawEnvelope))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("Status = %d, want 200 (array body form passed through)", w.Code)
	}
	if string(disp.lastReq.Body) != `[1, 2, 3]` {
		t.Errorf("dispatcher body = %q, want [1, 2, 3]", string(disp.lastReq.Body))
	}
}

func TestMessagesHandlerServeHTTPBodyEmptyPassedAsNil(t *testing.T) {

	disp := &fakeDispatcher{
		resp: &providers.TierResponse{Status: 200, Body: []byte(`{}`)},
	}
	h := transport.NewMessagesHandler(disp, nil)
	rawEnvelope := []byte(`{"session_id": "sess-empty-body"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(rawEnvelope))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("Status = %d, want 200", w.Code)
	}
	if disp.lastReq.Body != nil {
		t.Errorf("zero-body forwarded as %q, want nil", string(disp.lastReq.Body))
	}
}

func TestMessagesHandlerServeHTTPBodyMalformedQuotedString(t *testing.T) {

	disp := &fakeDispatcher{
		resp: &providers.TierResponse{Status: 200, Body: []byte(`{}`)},
	}
	h := transport.NewMessagesHandler(disp, nil)

	rawEnvelope := []byte(`{"body": "valid string"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(rawEnvelope))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("Status = %d, want 200", w.Code)
	}

	if string(disp.lastReq.Body) != "valid string" {
		t.Errorf("dispatcher body = %q, want 'valid string'", string(disp.lastReq.Body))
	}
}

func TestMessagesHandlerServeHTTPNonZenswarmTransportSourceStringFallback(t *testing.T) {

	disp := &fakeDispatcher{
		resp: &providers.TierResponse{Status: 200, Body: []byte(`{}`)},
	}
	anchor := &recordingAnchor{id: "evt-fallback"}
	h := transport.NewMessagesHandler(disp, anchor)

	body, _ := json.Marshal(transport.ForwardedRequest{Body: []byte(`{}`)})
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	req.Header.Set("X-Zen-Transport", "voice")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if len(anchor.calls) != 0 {
		t.Errorf("non-zenswarm value (voice) must not trigger anchor; got %d calls", len(anchor.calls))
	}
}
