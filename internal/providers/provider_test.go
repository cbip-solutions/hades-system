package providers

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/redact"
)

func TestTierString(t *testing.T) {
	cases := []struct {
		t    Tier
		want string
	}{
		{TierInHouse, "in-house"},
		{TierCommunity, "community"},
		{TierPAYG, "anthropic-paygo"},
		{TierAnthropicPAYG, "anthropic-paygo"},
		{TierGemini, "gemini"},
		{TierOllama, "ollama"},
		{TierGenericOpenAICompat, "openai-compat"},
		{TierPause, "pause"},
	}
	for _, c := range cases {
		if got := c.t.String(); got != c.want {
			t.Errorf("Tier(%d).String() = %q, want %q", int(c.t), got, c.want)
		}
	}
}

func TestTierStringUnknown(t *testing.T) {
	got := Tier(999).String()
	if !strings.HasPrefix(got, "unknown(") {
		t.Errorf("Tier(999).String() = %q, want prefix unknown(", got)
	}
}

func TestTierAnthropicPAYGAlias(t *testing.T) {
	if TierPAYG != TierAnthropicPAYG {
		t.Fatalf("TierPAYG (%d) and TierAnthropicPAYG (%d) must be the same value (alias)",
			int(TierPAYG), int(TierAnthropicPAYG))
	}
}

func TestParseTier(t *testing.T) {
	cases := []struct {
		in   string
		want Tier
		ok   bool
	}{
		{"in-house", TierInHouse, true},
		{"community", TierCommunity, true},
		{"anthropic-paygo", TierAnthropicPAYG, true},
		{"gemini", TierGemini, true},
		{"ollama", TierOllama, true},
		{"openai-compat", TierGenericOpenAICompat, true},
		{"pause", TierPause, true},
		{"frobnicate", Tier(-1), false},
		{"", Tier(-1), false},
	}
	for _, c := range cases {
		got, err := ParseTier(c.in)
		if c.ok {
			if err != nil {
				t.Errorf("ParseTier(%q) err=%v, want ok", c.in, err)
				continue
			}
			if got != c.want {
				t.Errorf("ParseTier(%q) = %v, want %v", c.in, got, c.want)
			}
		} else {
			if err == nil {
				t.Errorf("ParseTier(%q) err=nil, want error", c.in)
			}

			if got != c.want {
				t.Errorf("ParseTier(%q) returned %v on error, want %v (sentinel)",
					c.in, got, c.want)
			}
		}
	}
}

func TestTierRequestZeroValueOK(t *testing.T) {

	var req TierRequest
	if req.Method != "" {
		t.Errorf("zero-value Method = %q", req.Method)
	}
	if req.Headers != nil {
		t.Errorf("zero-value Headers should be nil map")
	}
	if req.Credentials != nil {
		t.Errorf("zero-value Credentials should be nil map")
	}
}

func TestTierRequestWithCredentials(t *testing.T) {
	req := TierRequest{
		Method:  "POST",
		Path:    "/v1/messages",
		Headers: map[string]string{"Content-Type": "application/json"},
		Credentials: map[string]redact.Secret{
			"Authorization": redact.NewSecret("sk-ant-test"),
		},
		Body:           []byte(`{"model":"opus"}`),
		ConversationID: "conv-1",
		SessionID:      "sess-1",
		IdempotencyKey: "idem-1",
		Profile:        "orchestrator",
		Project:        "internal-platform-x",
		Model:          "claude-opus-4-6",
	}

	rendered := fmt.Sprintf("%+v", req)
	if strings.Contains(rendered, "sk-ant-test") {
		t.Fatalf("formatted request leaked credential: %s", rendered)
	}
	if !strings.Contains(rendered, "[REDACTED]") {
		t.Fatalf("formatted request missing redaction marker: %s", rendered)
	}
}

func TestTierResponseZeroValueOK(t *testing.T) {
	var resp TierResponse
	if resp.Status != 0 {
		t.Errorf("zero-value Status = %d", resp.Status)
	}
	if resp.TierUsed != TierInHouse {

		t.Errorf("zero-value TierUsed = %v, want TierInHouse", resp.TierUsed)
	}
}

func TestTierCapabilitiesZeroValue(t *testing.T) {
	var c TierCapabilities
	if c.SupportsStreaming || c.SupportsToolUse || c.SupportsVision || c.SupportsPromptCaching {
		t.Errorf("zero-value capabilities should be all false")
	}
	if c.MaxContextTokens != 0 || c.MaxOutputTokens != 0 {
		t.Errorf("zero-value max tokens should be 0")
	}
}

// TierBackend interface compile guard. fakeBackend MUST satisfy
// TierBackend — if it does not, this file fails to compile and the
// invariant documentation is broken.
type fakeBackend struct{ name string }

func (f *fakeBackend) Forward(ctx context.Context, req TierRequest) (*TierResponse, error) {
	return &TierResponse{Status: 200, TierUsed: TierInHouse, ModelUsed: req.Model}, nil
}
func (f *fakeBackend) Probe(ctx context.Context) error { return nil }
func (f *fakeBackend) Close() error                    { return nil }
func (f *fakeBackend) Name() string                    { return f.name }
func (f *fakeBackend) Capabilities() TierCapabilities {
	return TierCapabilities{SupportsStreaming: true}
}
func (f *fakeBackend) Tier() Tier { return TierInHouse }

var _ TierBackend = (*fakeBackend)(nil)

func TestTierBackendFakeForward(t *testing.T) {
	var b TierBackend = &fakeBackend{name: "fake-1"}
	resp, err := b.Forward(context.Background(), TierRequest{Model: "claude-opus-4-6"})
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	if resp.Status != 200 {
		t.Errorf("Status = %d, want 200", resp.Status)
	}
	if resp.ModelUsed != "claude-opus-4-6" {
		t.Errorf("ModelUsed = %q, want claude-opus-4-6", resp.ModelUsed)
	}
}

func TestRateLimitedError_AsAndRetryAfter(t *testing.T) {
	err := fmt.Errorf("wrapped: %w", &RateLimitedError{RetryAfter: 30 * time.Second})
	var rl *RateLimitedError
	if !errors.As(err, &rl) || rl.RetryAfter != 30*time.Second {
		t.Fatalf("errors.As must unwrap RateLimitedError with RetryAfter")
	}
}

func TestRateLimitedError_ErrorMessage(t *testing.T) {
	e := &RateLimitedError{Provider: "bypass", RetryAfter: 60 * time.Second}
	msg := e.Error()
	if msg == "" {
		t.Fatal("RateLimitedError.Error() must return non-empty string")
	}
	if !strings.Contains(msg, "bypass") {
		t.Errorf("Error() should mention provider; got %q", msg)
	}
	if !strings.Contains(msg, "429") || !strings.Contains(msg, "rate") {
		t.Errorf("Error() should mention 429 and rate-limit; got %q", msg)
	}
}

func TestRateLimitedError_ZeroRetryAfter(t *testing.T) {
	e := &RateLimitedError{Provider: "ollama"}

	if e.RetryAfter != 0 {
		t.Errorf("zero RetryAfter should be 0, got %v", e.RetryAfter)
	}
	err := fmt.Errorf("upstream: %w", e)
	var rl *RateLimitedError
	if !errors.As(err, &rl) {
		t.Fatal("errors.As must unwrap even with zero RetryAfter")
	}
}

func TestSentinelErrorsDistinct(t *testing.T) {

	all := []error{
		ErrBackendNotConfigured,
		ErrTierUnavailable,
		ErrPaused,
		ErrRateMissing,
		ErrCapacityExceeded,
	}
	for i, a := range all {
		for j, b := range all {
			if i == j {
				continue
			}
			if errors.Is(a, b) {
				t.Errorf("errors.Is reports %v == %v (must be distinct)", a, b)
			}
		}
	}
}
