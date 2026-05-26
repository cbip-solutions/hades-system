package providers_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/keychain"
	"github.com/cbip-solutions/hades-system/internal/providers"
	"github.com/cbip-solutions/hades-system/internal/redact"
)

type fakeResolver struct {
	entries map[string]string
}

func (f fakeResolver) Lookup(service, _ string) (redact.Secret, error) {
	v, ok := f.entries[service]
	if !ok {
		return nil, keychain.ErrNotFound
	}
	return redact.NewSecret(v), nil
}

func TestAnthropicPaygoForwardSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("path = %q, want /v1/messages", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if got := r.Header.Get("x-api-key"); got != "sk-ant-test" {
			t.Errorf("x-api-key = %q, want sk-ant-test", got)
		}
		if got := r.Header.Get("anthropic-version"); got == "" {
			t.Error("anthropic-version header not set")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "msg_01", "model": "claude-haiku-4-5",
			"content": []any{map[string]any{"type": "text", "text": "ok"}},
			"usage":   map[string]any{"input_tokens": 8, "output_tokens": 3},
		})
	}))
	defer srv.Close()

	cfg := providers.ProviderConfig{
		Name: "anthropic-paygo", Type: "anthropic-paygo", Endpoint: srv.URL,
		Model: "claude-haiku-4-5", Family: "anthropic", APIKeyKeychain: "zen-swarm/anthropic-paygo",
	}
	kc := fakeResolver{entries: map[string]string{"zen-swarm/anthropic-paygo": "sk-ant-test"}}
	backend, err := providers.NewAnthropicPaygoBackend(cfg, kc)
	if err != nil {
		t.Fatalf("NewAnthropicPaygoBackend: %v", err)
	}
	resp, err := backend.Forward(context.Background(), providers.TierRequest{
		Body:  []byte(`{"model":"claude-haiku-4-5","messages":[{"role":"user","content":"hi"}],"max_tokens":16}`),
		Model: "claude-haiku-4-5",
	})
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	if resp.TierUsed != providers.TierAnthropicPAYG {
		t.Errorf("TierUsed = %v, want TierAnthropicPAYG", resp.TierUsed)
	}
	if resp.InputTokens != 8 || resp.OutputTokens != 3 {
		t.Errorf("usage = (%d,%d), want (8,3)", resp.InputTokens, resp.OutputTokens)
	}
}

func TestAnthropicPaygoConstructorMissingKey(t *testing.T) {
	cfg := providers.ProviderConfig{
		Name: "anthropic-paygo", Type: "anthropic-paygo", Endpoint: "https://api.anthropic.com",
		Model: "claude-haiku-4-5", Family: "anthropic", APIKeyKeychain: "zen-swarm/anthropic-paygo",
	}
	_, err := providers.NewAnthropicPaygoBackend(cfg, fakeResolver{entries: map[string]string{}})
	if !errors.Is(err, keychain.ErrNotFound) {
		t.Errorf("err = %v, want chain to keychain.ErrNotFound", err)
	}
}

func TestAnthropicPaygoForwardNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"type":"rate_limit_error"}}`))
	}))
	defer srv.Close()
	cfg := providers.ProviderConfig{
		Name: "anthropic-paygo", Type: "anthropic-paygo", Endpoint: srv.URL,
		Model: "claude-haiku-4-5", Family: "anthropic", APIKeyKeychain: "zen-swarm/anthropic-paygo",
	}
	kc := fakeResolver{entries: map[string]string{"zen-swarm/anthropic-paygo": "sk-ant-test"}}
	backend, err := providers.NewAnthropicPaygoBackend(cfg, kc)
	if err != nil {
		t.Fatalf("NewAnthropicPaygoBackend: %v", err)
	}
	resp, err := backend.Forward(context.Background(), providers.TierRequest{
		Body: []byte(`{"model":"claude-haiku-4-5","messages":[]}`), Model: "claude-haiku-4-5",
	})
	if err == nil {
		t.Fatal("Forward returned nil error for a 429 response")
	}
	if resp != nil {
		t.Errorf("Forward returned non-nil resp on error: %+v", resp)
	}
}

func TestAnthropicPaygoNameTierCapabilities(t *testing.T) {
	cfg := providers.ProviderConfig{
		Name: "anthropic-paygo", Type: "anthropic-paygo", Endpoint: "https://api.anthropic.com",
		Model: "claude-haiku-4-5", Family: "anthropic", APIKeyKeychain: "zen-swarm/anthropic-paygo",
	}
	kc := fakeResolver{entries: map[string]string{"zen-swarm/anthropic-paygo": "sk-ant-test"}}
	backend, err := providers.NewAnthropicPaygoBackend(cfg, kc)
	if err != nil {
		t.Fatalf("NewAnthropicPaygoBackend: %v", err)
	}
	if backend.Name() != "anthropic-paygo" {
		t.Errorf("Name = %q, want anthropic-paygo", backend.Name())
	}
	if backend.Tier() != providers.TierAnthropicPAYG {
		t.Errorf("Tier = %v, want TierAnthropicPAYG", backend.Tier())
	}
	if !backend.Capabilities().SupportsPromptCaching {
		t.Error("Capabilities.SupportsPromptCaching = false, want true")
	}
}
