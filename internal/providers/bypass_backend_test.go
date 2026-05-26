package providers_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/providers"
	"github.com/cbip-solutions/hades-system/internal/redact"
)

type fakeBypassClient struct {
	respBody   []byte
	respStatus int
	respErr    error

	retryAfter time.Duration

	healthFn func(ctx context.Context) error

	lastHeaders        map[string]string
	lastBody           []byte
	lastConversationID string
}

func (f *fakeBypassClient) ForwardRaw(
	ctx context.Context,
	body []byte,
	headers map[string]string,
	conversationID string,
) ([]byte, int, time.Duration, error) {
	f.lastHeaders = headers
	f.lastBody = body
	f.lastConversationID = conversationID
	return f.respBody, f.respStatus, f.retryAfter, f.respErr
}

func (f *fakeBypassClient) Health(ctx context.Context) error {
	if f.healthFn != nil {
		return f.healthFn(ctx)
	}
	return nil
}

func TestBypassBackendForwardSuccess(t *testing.T) {
	respBody, _ := json.Marshal(map[string]any{
		"id":    "msg_01BYPASS",
		"model": "claude-sonnet-4-6",
		"content": []any{
			map[string]any{"type": "text", "text": "bypass reply"},
		},
		"usage": map[string]any{
			"input_tokens":  20,
			"output_tokens": 8,
		},
	})

	fake := &fakeBypassClient{
		respBody:   respBody,
		respStatus: 200,
	}
	backend := providers.NewBypassBackend(fake)

	req := providers.TierRequest{
		Method:         "POST",
		Path:           "/v1/messages",
		Body:           []byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hi"}]}`),
		Model:          "claude-sonnet-4-6",
		ConversationID: "conv-7",
	}
	resp, err := backend.Forward(context.Background(), req)
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}

	if resp.TierUsed != providers.TierInHouse {
		t.Errorf("TierUsed = %v, want TierInHouse", resp.TierUsed)
	}
	if !strings.Contains(string(resp.Body), "bypass reply") {
		t.Errorf("Body missing canned text; got %s", string(resp.Body))
	}
	if resp.InputTokens != 20 || resp.OutputTokens != 8 {
		t.Errorf("token usage = (%d, %d), want (20, 8)", resp.InputTokens, resp.OutputTokens)
	}
	if resp.Status != 200 {
		t.Errorf("Status = %d, want 200", resp.Status)
	}
	if resp.ModelUsed != "claude-sonnet-4-6" {
		t.Errorf("ModelUsed = %q, want claude-sonnet-4-6", resp.ModelUsed)
	}
	// Forward MUST thread TierRequest.ConversationID into the bypass client
	// so the WAL BeginTurn records the turn under its conversation. The prior
	// signature dropped it → BeginTurn always failed with an empty
	// conversationID and the WAL never recorded a bypass turn (the HANDOFF
	// "prime suspect"). This is the regression guard.
	if fake.lastConversationID != "conv-7" {
		t.Errorf("ConversationID not threaded to bypass client: got %q, want conv-7", fake.lastConversationID)
	}
}

func TestBypassBackendForwardModelFallback(t *testing.T) {
	respBody := []byte(`{"usage":{"input_tokens":1,"output_tokens":1}}`)
	fake := &fakeBypassClient{respBody: respBody, respStatus: 200}
	backend := providers.NewBypassBackend(fake)

	req := providers.TierRequest{Body: []byte(`{}`), Model: "fallback-model"}
	resp, err := backend.Forward(context.Background(), req)
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	if resp.ModelUsed != "fallback-model" {
		t.Errorf("ModelUsed = %q, want fallback-model", resp.ModelUsed)
	}
}

func TestBypassBackendForwardMissingUsage(t *testing.T) {
	respBody := []byte(`{"id":"msg_nousage","model":"claude-haiku-3-5"}`)
	fake := &fakeBypassClient{respBody: respBody, respStatus: 200}
	backend := providers.NewBypassBackend(fake)

	resp, err := backend.Forward(context.Background(), providers.TierRequest{Body: []byte(`{}`)})
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	if resp.InputTokens != 0 || resp.OutputTokens != 0 {
		t.Errorf("expected zero tokens on missing usage field, got (%d, %d)",
			resp.InputTokens, resp.OutputTokens)
	}
}

func TestBypassBackendForwardError(t *testing.T) {
	upstreamErr := errors.New("dial timeout")
	fake := &fakeBypassClient{respErr: upstreamErr}
	backend := providers.NewBypassBackend(fake)

	_, err := backend.Forward(context.Background(), providers.TierRequest{Body: []byte(`{}`)})
	if err == nil {
		t.Fatal("expected error from bypass client, got nil")
	}
	if !strings.Contains(err.Error(), "bypass") {
		t.Errorf("error should mention bypass; got %v", err)
	}
	if !errors.Is(err, upstreamErr) {
		t.Errorf("errors.Is chain should reach upstreamErr; got %v", err)
	}
}

func TestBypassBackendForwardNonOKStatus(t *testing.T) {
	errBody := []byte(`{"error":{"type":"overloaded","message":"upstream busy"}}`)
	fake := &fakeBypassClient{respBody: errBody, respStatus: 529}
	backend := providers.NewBypassBackend(fake)

	_, err := backend.Forward(context.Background(), providers.TierRequest{Body: []byte(`{}`)})
	if err == nil {
		t.Fatal("expected error on 529, got nil")
	}
	if !strings.Contains(err.Error(), "bypass") {
		t.Errorf("error should mention bypass; got %v", err)
	}
	if !strings.Contains(err.Error(), "529") {
		t.Errorf("error should contain status 529; got %v", err)
	}
}

func TestBypassBackendForwardErrorBodyTruncation(t *testing.T) {
	largeBody := []byte(strings.Repeat("x", 1000))
	fake := &fakeBypassClient{respBody: largeBody, respStatus: 400}
	backend := providers.NewBypassBackend(fake)

	_, err := backend.Forward(context.Background(), providers.TierRequest{Body: []byte(`{}`)})
	if err == nil {
		t.Fatal("expected error on 400, got nil")
	}
	if len(err.Error()) >= 1000 {
		t.Errorf("error message too long (%d bytes); body must be truncated", len(err.Error()))
	}
	if !strings.Contains(err.Error(), "bypass") {
		t.Errorf("error should mention bypass; got %v", err)
	}
}

func TestBypassBackendForwardForwardsHeaders(t *testing.T) {
	respBody := []byte(`{"usage":{"input_tokens":1,"output_tokens":1}}`)
	fake := &fakeBypassClient{respBody: respBody, respStatus: 200}
	backend := providers.NewBypassBackend(fake)

	req := providers.TierRequest{
		Body: []byte(`{}`),
		Headers: map[string]string{
			"X-Zen-Profile": "test-profile",
			"X-Zen-Session": "sess-123",

			"Content-Type":  "text/plain",
			"Authorization": "Bearer hijacked",
		},
	}
	_, err := backend.Forward(context.Background(), req)
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}

	if fake.lastHeaders["X-Zen-Profile"] != "test-profile" {
		t.Errorf("X-Zen-Profile = %q, want test-profile", fake.lastHeaders["X-Zen-Profile"])
	}
	if fake.lastHeaders["X-Zen-Session"] != "sess-123" {
		t.Errorf("X-Zen-Session = %q, want sess-123", fake.lastHeaders["X-Zen-Session"])
	}

	if _, ok := fake.lastHeaders["Content-Type"]; ok {
		t.Error("Content-Type must not be forwarded (managed by bypass client)")
	}
	if _, ok := fake.lastHeaders["Authorization"]; ok {
		t.Error("Authorization must not be forwarded (managed by bypass client)")
	}
}

func TestBypassBackendForwardForwardsCredentials(t *testing.T) {
	respBody := []byte(`{"usage":{"input_tokens":1,"output_tokens":1}}`)
	fake := &fakeBypassClient{respBody: respBody, respStatus: 200}
	backend := providers.NewBypassBackend(fake)

	req := providers.TierRequest{
		Body: []byte(`{}`),
		Credentials: map[string]redact.Secret{
			"X-Secret-Token": redact.NewSecret("s3cr3t"),
		},
	}
	_, err := backend.Forward(context.Background(), req)
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	if fake.lastHeaders["X-Secret-Token"] != "s3cr3t" {
		t.Errorf("X-Secret-Token = %q, want s3cr3t", fake.lastHeaders["X-Secret-Token"])
	}
}

func TestBypassBackendName(t *testing.T) {
	backend := providers.NewBypassBackend(&fakeBypassClient{})
	if got := backend.Name(); got != "bypass" {
		t.Errorf("Name() = %q, want bypass", got)
	}
}

func TestBypassBackendTier(t *testing.T) {
	backend := providers.NewBypassBackend(&fakeBypassClient{})
	if got := backend.Tier(); got != providers.TierInHouse {
		t.Errorf("Tier() = %v, want TierInHouse", got)
	}
}

func TestBypassBackendTierString(t *testing.T) {
	got := providers.TierInHouse.String()
	if got != "in-house" {
		t.Errorf("TierInHouse.String() = %q, want in-house", got)
	}
}

func TestBypassBackendParseTier(t *testing.T) {
	got, err := providers.ParseTier("in-house")
	if err != nil {
		t.Fatalf("ParseTier(in-house): %v", err)
	}
	if got != providers.TierInHouse {
		t.Errorf("ParseTier(in-house) = %v, want TierInHouse", got)
	}
}

func TestBypassBackendCapabilities(t *testing.T) {
	backend := providers.NewBypassBackend(&fakeBypassClient{})
	caps := backend.Capabilities()

	if !caps.SupportsToolUse {
		t.Error("Capabilities.SupportsToolUse = false, want true")
	}
	if !caps.SupportsVision {
		t.Error("Capabilities.SupportsVision = false, want true")
	}
	if !caps.SupportsPromptCaching {
		t.Error("Capabilities.SupportsPromptCaching = false, want true")
	}

	if caps.SupportsStreaming {
		t.Error("Capabilities.SupportsStreaming = true, want false (not implemented in Phase B-2)")
	}
	if caps.MaxContextTokens <= 0 {
		t.Errorf("Capabilities.MaxContextTokens = %d, want > 0", caps.MaxContextTokens)
	}
	if caps.MaxOutputTokens <= 0 {
		t.Errorf("Capabilities.MaxOutputTokens = %d, want > 0", caps.MaxOutputTokens)
	}
}

func TestBypassBackendClose(t *testing.T) {
	backend := providers.NewBypassBackend(&fakeBypassClient{})
	if err := backend.Close(); err != nil {
		t.Errorf("first Close() = %v, want nil", err)
	}
	if err := backend.Close(); err != nil {
		t.Errorf("second Close() (idempotent) = %v, want nil", err)
	}
}

func TestBypassBackendProbeViaHealthSuccess(t *testing.T) {
	healthCalled := false
	fake := &fakeBypassClient{
		healthFn: func(ctx context.Context) error {
			healthCalled = true
			return nil
		},
	}
	backend := providers.NewBypassBackend(fake)

	if err := backend.Probe(context.Background()); err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if !healthCalled {
		t.Error("Probe must delegate to Health; Health was not called")
	}

	if fake.lastBody != nil || fake.lastHeaders != nil {
		t.Error("Probe must not call ForwardRaw; ForwardRaw was invoked")
	}
}

func TestBypassBackendProbeViaHealthError(t *testing.T) {
	healthErr := errors.New("session expired")
	fake := &fakeBypassClient{
		healthFn: func(ctx context.Context) error {
			return healthErr
		},
	}
	backend := providers.NewBypassBackend(fake)

	err := backend.Probe(context.Background())
	if err == nil {
		t.Fatal("expected error from Probe when Health fails, got nil")
	}
	if !errors.Is(err, providers.ErrBypassUnavailable) {
		t.Errorf("Probe error should wrap ErrBypassUnavailable; got %v", err)
	}
	if !errors.Is(err, healthErr) {
		t.Errorf("Probe error should chain to original Health error; got %v", err)
	}

	if fake.lastBody != nil || fake.lastHeaders != nil {
		t.Error("Probe must not call ForwardRaw; ForwardRaw was invoked")
	}
}

func TestBypassBackendForwardMalformedJSON(t *testing.T) {
	fake := &fakeBypassClient{
		respBody:   []byte(`{not valid json`),
		respStatus: 200,
	}
	backend := providers.NewBypassBackend(fake)

	resp, err := backend.Forward(context.Background(), providers.TierRequest{Body: []byte(`{}`)})
	if err != nil {
		t.Fatalf("Forward should not error on malformed body: %v", err)
	}

	if resp.InputTokens != 0 || resp.OutputTokens != 0 {
		t.Errorf("malformed body should yield zero tokens; got (%d, %d)", resp.InputTokens, resp.OutputTokens)
	}
}

func TestBypassBackend_Forward429ReturnsRateLimitedError(t *testing.T) {
	b := providers.NewBypassBackend(&fakeBypassClient{respStatus: 429, retryAfter: 30 * time.Second})
	_, err := b.Forward(context.Background(), providers.TierRequest{Model: "claude-opus-4-7"})
	var rl *providers.RateLimitedError
	if !errors.As(err, &rl) || rl.RetryAfter != 30*time.Second || rl.Provider != "bypass" {
		t.Fatalf("429 must yield *RateLimitedError{bypass,30s}; got %v", err)
	}
}

func TestBypassBackend_ForwardNon429Status(t *testing.T) {
	b := providers.NewBypassBackend(&fakeBypassClient{respStatus: 529})
	_, err := b.Forward(context.Background(), providers.TierRequest{})
	var rl *providers.RateLimitedError
	if errors.As(err, &rl) {
		t.Fatalf("non-429 error must not yield RateLimitedError; got %v", err)
	}
	if err == nil {
		t.Fatal("expected error on 529, got nil")
	}
}

var _ providers.TierBackend = (*providers.BypassBackend)(nil)
