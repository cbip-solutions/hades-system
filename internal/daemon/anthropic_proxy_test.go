// internal/daemon/anthropic_proxy_test.go
//
// Tests for NewAnthropicProxy after Plan 3 Phase B-8 wiring. The handler
// no longer calls bypass.Client.Forward directly; ALL LLM traffic flows
// through orchestrator → dispatcher → Tier 1 (bypass) / Tier 2+ (OpenClaude).
//
// Coverage targets:
//   - Idempotency-Key generation when missing + propagation when supplied
//   - X-Zen-Conversation-Id propagation to orchestrator.Call.ConversationID
//   - Caller upstream-bound headers (Anthropic-Beta multi-value, etc.) flow
//     into orchestrator.Call.Headers; control-plane headers do NOT
//   - Method/Path stamped on the Call (B-8 typed flow)
//   - Model extracted from request body (best-effort, non-fatal on parse fail)
//   - TierResponse.Status/Headers/Body mirrored to HTTP response; X-Zen-Tier-Used stamped
//   - Non-POST → 405 Method Not Allowed
//   - Orchestrator error → 502 Bad Gateway (single-tier failure shape)
//   - dispatcher.ErrAllTiersUnavailable → 503 Service Unavailable
//     (graceful-degradation contract)

package daemon

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/daemon/dispatcher"
	"github.com/cbip-solutions/hades-system/internal/daemon/orchestrator"
	"github.com/cbip-solutions/hades-system/internal/providers"
)

type fakeOrchestrator struct {
	lastCall providers.TierResponse
	calls    int
	last     orchestrator.Call

	respBody    []byte
	respStatus  int
	respHeaders map[string]string
	respTier    providers.Tier

	respErr error
}

func (f *fakeOrchestrator) Forward(ctx context.Context, call orchestrator.Call) (*providers.TierResponse, error) {
	f.calls++
	f.last = call
	if f.respErr != nil {
		return nil, f.respErr
	}
	return &providers.TierResponse{
		Status:   f.respStatus,
		Headers:  f.respHeaders,
		Body:     f.respBody,
		TierUsed: f.respTier,
	}, nil
}

func TestAnthropicProxy_GeneratesIdempotencyKeyWhenMissing(t *testing.T) {
	fake := &fakeOrchestrator{
		respBody:    []byte(`{"id":"msg_01","type":"message"}`),
		respStatus:  200,
		respHeaders: map[string]string{"Content-Type": "application/json"},
		respTier:    providers.TierInHouse,
	}
	h := NewAnthropicProxy(fake)

	req := httptest.NewRequest("POST", "/v1/messages",
		strings.NewReader(`{"model":"claude-opus-4-6","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != 200 {
		t.Fatalf("status: got %d, want 200", rr.Code)
	}
	if fake.last.IdempotencyKey == "" {
		t.Fatal("expected idempotency_key to be auto-generated")
	}
	if len(fake.last.IdempotencyKey) != 36 {
		t.Errorf("idempotency_key not UUID v4: %q (len=%d)", fake.last.IdempotencyKey, len(fake.last.IdempotencyKey))
	}
	if rr.Header().Get("Idempotency-Key") != fake.last.IdempotencyKey {
		t.Errorf("expected echoed Idempotency-Key header to match")
	}
}

// TestAnthropicProxy_PassesIdempotencyKeyAndConversationID — when the
// caller supplies Idempotency-Key + X-Zen-Conversation-Id, those values
// flow into orchestrator.Call.IdempotencyKey + ConversationID and DO NOT
// leak into Call.Headers (where they would be misinterpreted by the
// upstream tier as caller-supplied request headers).
func TestAnthropicProxy_PassesIdempotencyKeyAndConversationID(t *testing.T) {
	fake := &fakeOrchestrator{
		respBody:    []byte(`{"id":"msg_02"}`),
		respStatus:  200,
		respHeaders: map[string]string{"Content-Type": "application/json"},
		respTier:    providers.TierInHouse,
	}
	h := NewAnthropicProxy(fake)

	req := httptest.NewRequest("POST", "/v1/messages",
		strings.NewReader(`{"model":"claude-opus-4-6"}`))
	req.Header.Set("Idempotency-Key", "client-key-1234")
	req.Header.Set("X-Zen-Conversation-Id", "conv_abc")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if fake.last.IdempotencyKey != "client-key-1234" {
		t.Errorf("idempotency key: got %q, want client-key-1234", fake.last.IdempotencyKey)
	}
	if fake.last.ConversationID != "conv_abc" {
		t.Errorf("conversation id: got %q, want conv_abc", fake.last.ConversationID)
	}

	if _, ok := fake.last.Headers["Idempotency-Key"]; ok {
		t.Error("Idempotency-Key should not be in Call.Headers (it's a typed field)")
	}
	if _, ok := fake.last.Headers["X-Zen-Conversation-Id"]; ok {
		t.Error("X-Zen-Conversation-Id should not be in Call.Headers (it's a typed field)")
	}
}

func TestAnthropicProxy_ReadsXZenProfile(t *testing.T) {
	fake := &fakeOrchestrator{
		respBody:    []byte(`{"id":"msg_03"}`),
		respStatus:  200,
		respHeaders: map[string]string{"Content-Type": "application/json"},
		respTier:    providers.TierInHouse,
	}
	h := NewAnthropicProxy(fake)

	req := httptest.NewRequest("POST", "/v1/messages",
		strings.NewReader(`{"model":"claude-opus-4-6"}`))
	req.Header.Set("X-Zen-Profile", "worker-code")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if fake.last.Profile != "worker-code" {
		t.Errorf("Call.Profile: got %q, want worker-code (X-Zen-Profile must be honored)", fake.last.Profile)
	}
	if _, ok := fake.last.Headers["X-Zen-Profile"]; ok {
		t.Error("X-Zen-Profile should not be in Call.Headers (it's a typed routing field)")
	}
}

// TestAnthropicProxy_StampsMethodAndPath — orchestrator.Call.Method/Path
// are populated from the inbound request shape. Backends today hard-code
// POST /v1/messages but the typed flow MUST be intact for future backends
// + audit pipeline.
func TestAnthropicProxy_StampsMethodAndPath(t *testing.T) {
	fake := &fakeOrchestrator{respStatus: 200, respTier: providers.TierInHouse}
	h := NewAnthropicProxy(fake)

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{}`))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if fake.last.Method != "POST" {
		t.Errorf("Call.Method = %q, want %q", fake.last.Method, "POST")
	}
	if fake.last.Path != "/v1/messages" {
		t.Errorf("Call.Path = %q, want %q", fake.last.Path, "/v1/messages")
	}
}

func TestAnthropicProxy_RejectsNonPOST(t *testing.T) {
	h := NewAnthropicProxy(&fakeOrchestrator{})
	req := httptest.NewRequest("GET", "/v1/messages", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("got %d, want 405", rr.Code)
	}
	if rr.Header().Get("Allow") != "POST" {
		t.Errorf("expected Allow: POST, got %q", rr.Header().Get("Allow"))
	}
}

func TestAnthropicProxy_MultiValueHeadersAreCommaJoined(t *testing.T) {
	fake := &fakeOrchestrator{
		respBody:    []byte(`{}`),
		respStatus:  200,
		respHeaders: map[string]string{},
		respTier:    providers.TierInHouse,
	}
	h := NewAnthropicProxy(fake)

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{}`))

	req.Header.Add("Anthropic-Beta", "messages-2024-12-15")
	req.Header.Add("Anthropic-Beta", "tools-2024-04-04")

	req.Header.Add("X-Forwarded-For", "10.0.0.1")
	req.Header.Add("X-Forwarded-For", "10.0.0.2")

	req.Header.Set("X-Single", "only-one")

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != 200 {
		t.Fatalf("status: got %d, want 200", rr.Code)
	}
	if got, want := fake.last.Headers["Anthropic-Beta"], "messages-2024-12-15, tools-2024-04-04"; got != want {
		t.Errorf("Anthropic-Beta: got %q, want %q", got, want)
	}
	if got, want := fake.last.Headers["X-Forwarded-For"], "10.0.0.1, 10.0.0.2"; got != want {
		t.Errorf("X-Forwarded-For: got %q, want %q", got, want)
	}
	if got, want := fake.last.Headers["X-Single"], "only-one"; got != want {
		t.Errorf("X-Single: got %q, want %q", got, want)
	}
}

func TestAnthropicProxy_BodyAndStatusMirrored(t *testing.T) {
	fake := &fakeOrchestrator{
		respBody:    []byte(`{"error":"upstream"}`),
		respStatus:  503,
		respHeaders: map[string]string{"Content-Type": "application/json", "X-Custom": "v"},
		respTier:    providers.TierInHouse,
	}
	h := NewAnthropicProxy(fake)

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{}`))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != 503 {
		t.Errorf("status: got %d, want 503", rr.Code)
	}
	if rr.Header().Get("X-Custom") != "v" {
		t.Errorf("custom header not mirrored: %q", rr.Header().Get("X-Custom"))
	}
	if !strings.Contains(rr.Body.String(), "upstream") {
		t.Errorf("body not mirrored: %q", rr.Body.String())
	}
}

func TestAnthropicProxy_StampsXZenTierUsed(t *testing.T) {
	fake := &fakeOrchestrator{
		respBody:    []byte(`{}`),
		respStatus:  200,
		respHeaders: map[string]string{},
		respTier:    providers.TierOpenClaude,
	}
	h := NewAnthropicProxy(fake)

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{}`))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if got, want := rr.Header().Get("X-Zen-Tier-Used"), "openclaude"; got != want {
		t.Errorf("X-Zen-Tier-Used: got %q, want %q", got, want)
	}
}

func TestAnthropicProxy_ExtractsModelFromBody(t *testing.T) {
	fake := &fakeOrchestrator{respStatus: 200, respTier: providers.TierInHouse}
	h := NewAnthropicProxy(fake)

	req := httptest.NewRequest("POST", "/v1/messages",
		strings.NewReader(`{"model":"claude-sonnet-4-6","messages":[]}`))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if fake.last.Model != "claude-sonnet-4-6" {
		t.Errorf("Call.Model = %q, want %q", fake.last.Model, "claude-sonnet-4-6")
	}
}

// TestAnthropicProxy_ModelExtractionBestEffort — an unparseable body MUST
// NOT abort the request. Empty Model on the Call is acceptable; backends
// fall back gracefully and Phase F treats it as known-unknown.
func TestAnthropicProxy_ModelExtractionBestEffort(t *testing.T) {
	fake := &fakeOrchestrator{respStatus: 200, respTier: providers.TierInHouse}
	h := NewAnthropicProxy(fake)

	req := httptest.NewRequest("POST", "/v1/messages",
		strings.NewReader(`not-json{`))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != 200 {
		t.Errorf("status: got %d, want 200 (body parse must not abort)", rr.Code)
	}
	if fake.last.Model != "" {
		t.Errorf("Call.Model = %q, want empty (unparseable body)", fake.last.Model)
	}
	// Body still flows through verbatim — backends may reject, but the
	// proxy MUST forward faithfully.
	if string(fake.last.Body) != `not-json{` {
		t.Errorf("Body not preserved: got %q, want %q", fake.last.Body, `not-json{`)
	}
}

func TestAnthropicProxy_OrchestratorErrorReturns502(t *testing.T) {
	fake := &fakeOrchestrator{respErr: errString("transport failure")}
	h := NewAnthropicProxy(fake)

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{}`))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadGateway {
		t.Errorf("status: got %d, want 502", rr.Code)
	}
}

func TestAnthropicProxy_AllTiersUnavailableReturns503(t *testing.T) {
	fake := &fakeOrchestrator{respErr: dispatcher.ErrAllTiersUnavailable}
	h := NewAnthropicProxy(fake)

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{}`))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("status: got %d, want 503", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "all tiers unavailable") {
		t.Errorf("body must surface ErrAllTiersUnavailable, got %q", rr.Body.String())
	}
}

func TestAnthropicProxy_BodyForwardedVerbatim(t *testing.T) {
	fake := &fakeOrchestrator{respStatus: 200, respTier: providers.TierInHouse}
	h := NewAnthropicProxy(fake)

	body := `{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if string(fake.last.Body) != body {
		t.Errorf("Body not forwarded verbatim:\n got=%q\nwant=%q", fake.last.Body, body)
	}
}

// TestAnthropicProxy_DropsHopByHopHeadersOnRequestSide — the eight RFC 7230
// §6.1 hop-by-hop headers MUST be removed from inbound request headers
// before they are placed in orchestrator.Call.Headers. Forwarding them
// would confuse upstream tiers that parse header semantics (e.g. Upgrade
// changing protocol, Connection controlling keep-alive for a persistent
// connection that doesn't exist end-to-end).
func TestAnthropicProxy_DropsHopByHopHeadersOnRequestSide(t *testing.T) {
	fake := &fakeOrchestrator{respStatus: 200, respTier: providers.TierInHouse}
	h := NewAnthropicProxy(fake)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{}`))
	hopHeaders := []string{
		"Connection", "Transfer-Encoding", "Keep-Alive",
		"Proxy-Authenticate", "Proxy-Authorization",
		"Te", "Trailer", "Upgrade",
	}
	for _, k := range hopHeaders {
		req.Header.Set(k, "must-be-dropped")
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if fake.calls != 1 {
		t.Fatalf("orchestrator.Forward not called (calls=%d)", fake.calls)
	}
	for _, k := range hopHeaders {
		if _, ok := fake.last.Headers[k]; ok {
			t.Errorf("hop-by-hop header %q must not flow into Call.Headers", k)
		}
	}
}

// TestAnthropicProxy_DropsHopByHopHeadersOnResponseSide — hop-by-hop headers
// returned by the upstream tier response MUST be stripped before being
// mirrored to the client. Forwarding them (e.g. "Connection: close") would
// corrupt the client's persistent connection assumptions for the
// daemon-side UDS or TCP transport.
func TestAnthropicProxy_DropsHopByHopHeadersOnResponseSide(t *testing.T) {

	fake := &fakeOrchestrator{
		respBody:   []byte(`{"id":"msg_hop"}`),
		respStatus: 200,
		respHeaders: map[string]string{
			"Content-Type":      "application/json",
			"X-Custom":          "should-pass",
			"Connection":        "close",
			"Transfer-Encoding": "chunked",
		},
		respTier: providers.TierInHouse,
	}
	h := NewAnthropicProxy(fake)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{}`))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != 200 {
		t.Fatalf("status: got %d, want 200", rr.Code)
	}

	if got := rr.Header().Get("X-Custom"); got != "should-pass" {
		t.Errorf("X-Custom: got %q, want %q", got, "should-pass")
	}

	if got := rr.Header().Get("Connection"); got != "" {
		t.Errorf("hop-by-hop response header %q must not be mirrored to client, got %q", "Connection", got)
	}
	if got := rr.Header().Get("Transfer-Encoding"); got != "" {
		t.Errorf("hop-by-hop response header %q must not be mirrored to client, got %q", "Transfer-Encoding", got)
	}
}

func TestExtractModel_EmptyBody(t *testing.T) {
	if got := extractModel(nil); got != "" {
		t.Errorf("extractModel(nil) = %q, want \"\"", got)
	}
	if got := extractModel([]byte{}); got != "" {
		t.Errorf("extractModel([]byte{}) = %q, want \"\"", got)
	}
}

func TestExtractModel_MalformedJSON(t *testing.T) {
	if got := extractModel([]byte("not-json")); got != "" {
		t.Errorf("extractModel(malformed) = %q, want \"\"", got)
	}
}

type errString string

func (e errString) Error() string { return string(e) }
