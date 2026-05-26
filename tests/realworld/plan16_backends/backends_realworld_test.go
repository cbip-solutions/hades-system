//go:build realworld

package plan16_backends

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/keychain"
	"github.com/cbip-solutions/hades-system/internal/providers"
)

func canonical1Token(model string) []byte {
	return []byte(`{"model":"` + model + `","max_tokens":1,"messages":[{"role":"user","content":"hi"}]}`)
}

func skipIfKeyAbsent(t *testing.T, service string) {
	t.Helper()
	_, err := keychain.SystemResolver{}.Lookup(service, "zen-swarm")
	if err != nil {
		if errors.Is(err, keychain.ErrNotFound) || errors.Is(err, keychain.ErrUnsupported) {
			t.Skipf("Keychain key %q absent — skipping live test (provision per spec §11)", service)
		}
		t.Fatalf("Keychain lookup %q hit a hard error: %v", service, err)
	}
}

func TestAnthropicPaygoRealWorld(t *testing.T) {
	skipIfKeyAbsent(t, "zen-swarm/anthropic-paygo")
	cfg := providers.ProviderConfig{
		Name: "anthropic-paygo", Type: "anthropic-paygo",
		Endpoint: "https://api.anthropic.com", Model: "claude-haiku-4-5",
		Family: "anthropic", APIKeyKeychain: "zen-swarm/anthropic-paygo",
	}
	backend, err := providers.NewAnthropicPaygoBackend(cfg, keychain.SystemResolver{})
	if err != nil {
		t.Fatalf("NewAnthropicPaygoBackend: %v", err)
	}
	defer backend.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	resp, err := backend.Forward(ctx, providers.TierRequest{
		Body: canonical1Token("claude-haiku-4-5"), Model: "claude-haiku-4-5",
	})
	if err != nil {
		t.Fatalf("live Anthropic paygo Forward: %v", err)
	}
	if resp.Status != 200 {
		t.Errorf("live status = %d, want 200", resp.Status)
	}
	if resp.InputTokens == 0 {
		t.Error("live response reported 0 input tokens — usage parse likely broken")
	}
}

func TestGeminiRealWorld(t *testing.T) {
	skipIfKeyAbsent(t, "zen-swarm/google-ai")
	cfg := providers.ProviderConfig{
		Name: "gemini-flash", Type: "gemini",
		Endpoint: "https://generativelanguage.googleapis.com", Model: "gemini-2.0-flash",
		Family: "gemini", APIKeyKeychain: "zen-swarm/google-ai",
	}
	backend, err := providers.NewGeminiBackend(cfg, keychain.SystemResolver{})
	if err != nil {
		t.Fatalf("NewGeminiBackend: %v", err)
	}
	defer backend.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	resp, err := backend.Forward(ctx, providers.TierRequest{
		Body: canonical1Token("gemini-2.0-flash"), Model: "gemini-2.0-flash",
	})
	if err != nil {
		t.Fatalf("live Gemini Forward: %v", err)
	}
	if resp.Status != 200 {
		t.Errorf("live status = %d, want 200", resp.Status)
	}
	if resp.InputTokens == 0 {
		t.Error("live Gemini response reported 0 input tokens — usageMetadata parse likely broken")
	}
}

func TestDeepSeekDirectRealWorld(t *testing.T) {
	skipIfKeyAbsent(t, "zen-swarm/deepseek")
	cfg := providers.ProviderConfig{
		Name: "deepseek-direct", Type: "openai-compat",
		Endpoint: "https://api.deepseek.com", Model: "deepseek-chat",
		Family: "deepseek", APIKeyKeychain: "zen-swarm/deepseek",
	}
	backend, err := providers.NewOpenAICompatBackend(cfg, keychain.SystemResolver{})
	if err != nil {
		t.Fatalf("NewOpenAICompatBackend: %v", err)
	}
	defer backend.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	resp, err := backend.Forward(ctx, providers.TierRequest{
		Body: canonical1Token("deepseek-chat"), Model: "deepseek-chat",
	})
	if err != nil {
		t.Fatalf("live DeepSeek Forward: %v", err)
	}
	if resp.Status != 200 {
		t.Errorf("live status = %d, want 200", resp.Status)
	}
	if resp.InputTokens == 0 {
		t.Error("live DeepSeek response reported 0 input tokens — translation usage parse likely broken")
	}
}
