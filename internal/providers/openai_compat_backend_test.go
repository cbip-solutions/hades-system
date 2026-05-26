package providers_test

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

	"github.com/cbip-solutions/hades-system/internal/keychain"
	"github.com/cbip-solutions/hades-system/internal/providers"
	"github.com/cbip-solutions/hades-system/internal/redact"
)

func TestOpenAICompatForwardTranslates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %q, want /v1/chat/completions", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-ds-test" {
			t.Errorf("Authorization = %q, want Bearer sk-ds-test", got)
		}

		var req struct {
			Model    string `json:"model"`
			Messages []struct {
				Role string `json:"role"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode translated request: %v", err)
		}
		if req.Model != "deepseek-chat" {
			t.Errorf("translated model = %q, want deepseek-chat", req.Model)
		}
		if len(req.Messages) == 0 || req.Messages[0].Role != "system" {
			t.Errorf("expected leading system message, got %+v", req.Messages)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl-x", "model": "deepseek-chat",
			"choices": []any{map[string]any{
				"message":       map[string]any{"role": "assistant", "content": "translated reply"},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 15, "completion_tokens": 6},
		})
	}))
	defer srv.Close()

	cfg := providers.ProviderConfig{
		Name: "deepseek-direct", Type: "openai-compat", Endpoint: srv.URL,
		Model: "deepseek-chat", Family: "deepseek", APIKeyKeychain: "zen-swarm/deepseek",
	}
	kc := fakeResolver{entries: map[string]string{"zen-swarm/deepseek": "sk-ds-test"}}
	backend, err := providers.NewOpenAICompatBackend(cfg, kc)
	if err != nil {
		t.Fatalf("NewOpenAICompatBackend: %v", err)
	}
	resp, err := backend.Forward(context.Background(), providers.TierRequest{
		Body:  []byte(`{"model":"claude-x","system":"sys","messages":[{"role":"user","content":"hi"}],"max_tokens":32}`),
		Model: "deepseek-chat",
	})
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	if resp.TierUsed != providers.TierGenericOpenAICompat {
		t.Errorf("TierUsed = %v, want TierGenericOpenAICompat", resp.TierUsed)
	}
	if resp.InputTokens != 15 || resp.OutputTokens != 6 {
		t.Errorf("usage = (%d,%d), want (15,6)", resp.InputTokens, resp.OutputTokens)
	}
	if !strings.Contains(string(resp.Body), "translated reply") {
		t.Errorf("canonical body missing text: %s", string(resp.Body))
	}

	if !strings.Contains(string(resp.Body), `"type":"text"`) {
		t.Errorf("response not translated to canonical shape: %s", string(resp.Body))
	}
}

func TestOpenAICompatName(t *testing.T) {
	cfg := providers.ProviderConfig{
		Name: "moonshot-kimi", Type: "openai-compat", Endpoint: "https://api.moonshot.cn",
		Model: "moonshot-v1-128k", Family: "moonshot", APIKeyKeychain: "zen-swarm/moonshot",
	}
	kc := fakeResolver{entries: map[string]string{"zen-swarm/moonshot": "sk-ms-test"}}
	backend, err := providers.NewOpenAICompatBackend(cfg, kc)
	if err != nil {
		t.Fatalf("NewOpenAICompatBackend: %v", err)
	}
	if backend.Name() != "moonshot-kimi" {
		t.Errorf("Name = %q, want moonshot-kimi (the config name, not the type)", backend.Name())
	}
	if backend.Tier() != providers.TierGenericOpenAICompat {
		t.Errorf("Tier = %v, want TierGenericOpenAICompat", backend.Tier())
	}
}

func TestOpenAICompatConstructorMissingKey(t *testing.T) {
	cfg := providers.ProviderConfig{
		Name: "deepseek-direct", Type: "openai-compat", Endpoint: "https://api.deepseek.com",
		Model: "deepseek-chat", Family: "deepseek", APIKeyKeychain: "zen-swarm/deepseek",
	}
	_, err := providers.NewOpenAICompatBackend(cfg, fakeResolver{entries: map[string]string{}})
	if !errors.Is(err, keychain.ErrNotFound) {
		t.Errorf("err = %v, want chain to keychain.ErrNotFound", err)
	}
}

func TestOpenAICompatForwardNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream down"))
	}))
	defer srv.Close()
	cfg := providers.ProviderConfig{
		Name: "deepseek-direct", Type: "openai-compat", Endpoint: srv.URL,
		Model: "deepseek-chat", Family: "deepseek", APIKeyKeychain: "zen-swarm/deepseek",
	}
	kc := fakeResolver{entries: map[string]string{"zen-swarm/deepseek": "sk-ds-test"}}
	backend, err := providers.NewOpenAICompatBackend(cfg, kc)
	if err != nil {
		t.Fatalf("NewOpenAICompatBackend: %v", err)
	}
	resp, err := backend.Forward(context.Background(), providers.TierRequest{
		Body: []byte(`{"messages":[{"role":"user","content":"x"}]}`), Model: "deepseek-chat",
	})
	if err == nil {
		t.Fatal("Forward returned nil error for a 502")
	}
	if resp != nil {
		t.Errorf("Forward returned non-nil resp on error: %+v", resp)
	}
}

func TestOpenAICompatCapabilities(t *testing.T) {
	cfg := providers.ProviderConfig{
		Name: "deepseek-direct", Type: "openai-compat", Endpoint: "https://api.deepseek.com",
		Model: "deepseek-chat", Family: "deepseek", APIKeyKeychain: "zen-swarm/deepseek",
	}
	kc := fakeResolver{entries: map[string]string{"zen-swarm/deepseek": "sk-ds-test"}}
	backend, err := providers.NewOpenAICompatBackend(cfg, kc)
	if err != nil {
		t.Fatalf("NewOpenAICompatBackend: %v", err)
	}
	caps := backend.Capabilities()
	if caps.SupportsStreaming || caps.SupportsToolUse || caps.SupportsVision || caps.SupportsPromptCaching {
		t.Errorf("Capabilities advertise unsupported feature: %+v", caps)
	}
	if caps.MaxContextTokens != 128_000 {
		t.Errorf("MaxContextTokens = %d, want 128_000", caps.MaxContextTokens)
	}
	if caps.MaxOutputTokens != 8_192 {
		t.Errorf("MaxOutputTokens = %d, want 8_192", caps.MaxOutputTokens)
	}
}

func TestOpenAICompatProbeSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("probe path = %q, want /v1/chat/completions", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-ds-test" {
			t.Errorf("probe auth header = %q, want Bearer sk-ds-test", got)
		}
		if got := r.Header.Get("HTTP-Referer"); got != "https://zen-swarm.local" {
			t.Errorf("probe HTTP-Referer = %q, want https://zen-swarm.local (operator-static header parity)", got)
		}

		var got struct {
			MaxTokens int `json:"max_tokens"`
			Messages  []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode probe body: %v", err)
		}
		if got.MaxTokens != 1 {
			t.Errorf("probe max_tokens = %d, want 1", got.MaxTokens)
		}
		if len(got.Messages) != 1 || got.Messages[0].Content != "hi" {
			t.Errorf("probe content not fixed 'hi' (inv-zen-071): %+v", got.Messages)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	cfg := providers.ProviderConfig{
		Name: "deepseek-direct", Type: "openai-compat", Endpoint: srv.URL,
		Model: "deepseek-chat", Family: "deepseek", APIKeyKeychain: "zen-swarm/deepseek",
		Headers: map[string]string{"HTTP-Referer": "https://zen-swarm.local"},
	}
	kc := fakeResolver{entries: map[string]string{"zen-swarm/deepseek": "sk-ds-test"}}
	backend, err := providers.NewOpenAICompatBackend(cfg, kc)
	if err != nil {
		t.Fatalf("NewOpenAICompatBackend: %v", err)
	}
	if err := backend.Probe(context.Background()); err != nil {
		t.Errorf("Probe: %v", err)
	}
}

func TestOpenAICompatProbeUnauthorized(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
			}))
			defer srv.Close()
			cfg := providers.ProviderConfig{
				Name: "deepseek-direct", Type: "openai-compat", Endpoint: srv.URL,
				Model: "deepseek-chat", Family: "deepseek", APIKeyKeychain: "zen-swarm/deepseek",
			}
			kc := fakeResolver{entries: map[string]string{"zen-swarm/deepseek": "sk-ds-test"}}
			backend, err := providers.NewOpenAICompatBackend(cfg, kc)
			if err != nil {
				t.Fatalf("NewOpenAICompatBackend: %v", err)
			}
			err = backend.Probe(context.Background())
			if !errors.Is(err, providers.ErrOpenAICompatUnavailable) {
				t.Errorf("err = %v, want chain to ErrOpenAICompatUnavailable", err)
			}
		})
	}
}

func TestOpenAICompatProbeTransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.Close()
	cfg := providers.ProviderConfig{
		Name: "deepseek-direct", Type: "openai-compat", Endpoint: srv.URL,
		Model: "deepseek-chat", Family: "deepseek", APIKeyKeychain: "zen-swarm/deepseek",
	}
	kc := fakeResolver{entries: map[string]string{"zen-swarm/deepseek": "sk-ds-test"}}
	backend, err := providers.NewOpenAICompatBackend(cfg, kc)
	if err != nil {
		t.Fatalf("NewOpenAICompatBackend: %v", err)
	}
	err = backend.Probe(context.Background())
	if !errors.Is(err, providers.ErrOpenAICompatUnavailable) {
		t.Errorf("err = %v, want chain to ErrOpenAICompatUnavailable on transport error", err)
	}
}

func TestOpenAICompatClose(t *testing.T) {
	cfg := providers.ProviderConfig{
		Name: "deepseek-direct", Type: "openai-compat", Endpoint: "https://api.deepseek.com",
		Model: "deepseek-chat", Family: "deepseek", APIKeyKeychain: "zen-swarm/deepseek",
	}
	kc := fakeResolver{entries: map[string]string{"zen-swarm/deepseek": "sk-ds-test"}}
	backend, err := providers.NewOpenAICompatBackend(cfg, kc)
	if err != nil {
		t.Fatalf("NewOpenAICompatBackend: %v", err)
	}
	if err := backend.Close(); err != nil {
		t.Errorf("first Close: %v", err)
	}
	if err := backend.Close(); err != nil {
		t.Errorf("second Close (idempotent): %v", err)
	}
}

func TestOpenAICompatForwardTransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.Close()
	cfg := providers.ProviderConfig{
		Name: "deepseek-direct", Type: "openai-compat", Endpoint: srv.URL,
		Model: "deepseek-chat", Family: "deepseek", APIKeyKeychain: "zen-swarm/deepseek",
	}
	kc := fakeResolver{entries: map[string]string{"zen-swarm/deepseek": "sk-ds-test"}}
	backend, err := providers.NewOpenAICompatBackend(cfg, kc)
	if err != nil {
		t.Fatalf("NewOpenAICompatBackend: %v", err)
	}
	resp, err := backend.Forward(context.Background(), providers.TierRequest{
		Body: []byte(`{"model":"x","messages":[{"role":"user","content":"hi"}]}`), Model: "deepseek-chat",
	})
	if err == nil {
		t.Fatal("Forward returned nil error against closed server")
	}
	if resp != nil {
		t.Errorf("Forward returned non-nil resp on error: %+v", resp)
	}
	if !strings.Contains(err.Error(), "deepseek-direct") {
		t.Errorf("error %q does not name the provider", err.Error())
	}
}

func TestOpenAICompatForwardTranslateResponseError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("not valid json"))
	}))
	defer srv.Close()
	cfg := providers.ProviderConfig{
		Name: "deepseek-direct", Type: "openai-compat", Endpoint: srv.URL,
		Model: "deepseek-chat", Family: "deepseek", APIKeyKeychain: "zen-swarm/deepseek",
	}
	kc := fakeResolver{entries: map[string]string{"zen-swarm/deepseek": "sk-ds-test"}}
	backend, err := providers.NewOpenAICompatBackend(cfg, kc)
	if err != nil {
		t.Fatalf("NewOpenAICompatBackend: %v", err)
	}
	resp, err := backend.Forward(context.Background(), providers.TierRequest{
		Body: []byte(`{"model":"x","messages":[{"role":"user","content":"hi"}]}`), Model: "deepseek-chat",
	})
	if err == nil {
		t.Fatal("Forward returned nil error on malformed upstream response")
	}
	if resp != nil {
		t.Errorf("Forward returned non-nil resp on translate error: %+v", resp)
	}
	if !strings.Contains(err.Error(), "translate response") {
		t.Errorf("error %q does not mention translate-response failure", err.Error())
	}
}

func TestOpenAICompatForwardTranslateRequestError(t *testing.T) {
	cfg := providers.ProviderConfig{
		Name: "deepseek-direct", Type: "openai-compat", Endpoint: "https://api.deepseek.com",
		Model: "deepseek-chat", Family: "deepseek", APIKeyKeychain: "zen-swarm/deepseek",
	}
	kc := fakeResolver{entries: map[string]string{"zen-swarm/deepseek": "sk-ds-test"}}
	backend, err := providers.NewOpenAICompatBackend(cfg, kc)
	if err != nil {
		t.Fatalf("NewOpenAICompatBackend: %v", err)
	}
	resp, err := backend.Forward(context.Background(), providers.TierRequest{
		Body: []byte("not json"), Model: "deepseek-chat",
	})
	if err == nil {
		t.Fatal("Forward returned nil error on malformed request body")
	}
	if resp != nil {
		t.Errorf("Forward returned non-nil resp on translate-request error: %+v", resp)
	}
	if !strings.Contains(err.Error(), "translate request") {
		t.Errorf("error %q does not mention translate-request failure", err.Error())
	}
}

func TestOpenAICompatForwardForwardsHeaders(t *testing.T) {
	var gotXZenProfile, gotXZenSession, gotContentType, gotAuthHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotXZenProfile = r.Header.Get("X-Zen-Profile")
		gotXZenSession = r.Header.Get("X-Zen-Session")
		gotContentType = r.Header.Get("Content-Type")
		gotAuthHeader = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl-h", "model": "deepseek-chat",
			"choices": []any{map[string]any{
				"message":       map[string]any{"role": "assistant", "content": "ok"},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1},
		})
	}))
	defer srv.Close()
	cfg := providers.ProviderConfig{
		Name: "deepseek-direct", Type: "openai-compat", Endpoint: srv.URL,
		Model: "deepseek-chat", Family: "deepseek", APIKeyKeychain: "zen-swarm/deepseek",
	}
	kc := fakeResolver{entries: map[string]string{"zen-swarm/deepseek": "sk-ds-test"}}
	backend, err := providers.NewOpenAICompatBackend(cfg, kc)
	if err != nil {
		t.Fatalf("NewOpenAICompatBackend: %v", err)
	}
	_, err = backend.Forward(context.Background(), providers.TierRequest{
		Body:  []byte(`{"model":"claude-x","messages":[{"role":"user","content":"hi"}],"max_tokens":1}`),
		Model: "deepseek-chat",
		Headers: map[string]string{
			"X-Zen-Profile": "test-profile",
			"X-Zen-Session": "sess-123",

			"Content-Type":  "text/plain",
			"Authorization": "Bearer hijacked",
		},
	})
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	if gotXZenProfile != "test-profile" {
		t.Errorf("X-Zen-Profile = %q, want test-profile", gotXZenProfile)
	}
	if gotXZenSession != "sess-123" {
		t.Errorf("X-Zen-Session = %q, want sess-123", gotXZenSession)
	}

	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json (backend-managed)", gotContentType)
	}
	if gotAuthHeader != "Bearer sk-ds-test" {
		t.Errorf("Authorization = %q, want Bearer sk-ds-test (backend-managed, not hijacked)", gotAuthHeader)
	}
}

func TestOpenAICompatForwardForwardsCredentials(t *testing.T) {
	var gotSecretToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSecretToken = r.Header.Get("X-Secret-Token")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl-c", "model": "deepseek-chat",
			"choices": []any{map[string]any{
				"message":       map[string]any{"role": "assistant", "content": "ok"},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1},
		})
	}))
	defer srv.Close()
	cfg := providers.ProviderConfig{
		Name: "deepseek-direct", Type: "openai-compat", Endpoint: srv.URL,
		Model: "deepseek-chat", Family: "deepseek", APIKeyKeychain: "zen-swarm/deepseek",
	}
	kc := fakeResolver{entries: map[string]string{"zen-swarm/deepseek": "sk-ds-test"}}
	backend, err := providers.NewOpenAICompatBackend(cfg, kc)
	if err != nil {
		t.Fatalf("NewOpenAICompatBackend: %v", err)
	}
	_, err = backend.Forward(context.Background(), providers.TierRequest{
		Body:  []byte(`{"model":"claude-x","messages":[{"role":"user","content":"hi"}],"max_tokens":1}`),
		Model: "deepseek-chat",
		Credentials: map[string]redact.Secret{
			"X-Secret-Token": redact.NewSecret("s3cr3t"),
		},
	})
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	if gotSecretToken != "s3cr3t" {
		t.Errorf("X-Secret-Token = %q, want s3cr3t", gotSecretToken)
	}
}

func TestOpenAICompatNewResolverNil(t *testing.T) {
	cfg := providers.ProviderConfig{
		Name: "deepseek-direct", Type: "openai-compat", Endpoint: "https://api.deepseek.com",
		Model: "deepseek-chat", Family: "deepseek", APIKeyKeychain: "zen-swarm/deepseek",
	}
	_, err := providers.NewOpenAICompatBackend(cfg, nil)
	if err == nil {
		t.Fatal("NewOpenAICompatBackend accepted nil keychain.Resolver")
	}
}

func TestOpenAICompatForwardHermesShapedBody(t *testing.T) {
	var receivedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %q, want /v1/chat/completions", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		receivedBody = body
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl-h", "model": "deepseek-chat",
			"choices": []any{map[string]any{
				"message":       map[string]any{"role": "assistant", "content": "hermes-shaped reply"},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 100, "completion_tokens": 25},
		})
	}))
	defer srv.Close()
	cfg := providers.ProviderConfig{
		Name: "deepseek-direct", Type: "openai-compat", Endpoint: srv.URL,
		Model: "deepseek-chat", Family: "deepseek", APIKeyKeychain: "zen-swarm/deepseek",
	}
	kc := fakeResolver{entries: map[string]string{"zen-swarm/deepseek": "sk-ds-test"}}
	backend, err := providers.NewOpenAICompatBackend(cfg, kc)
	if err != nil {
		t.Fatalf("NewOpenAICompatBackend: %v", err)
	}
	hermesBody := []byte(`{
		"model":"claude-opus-4-7",
		"max_tokens":4096,
		"system":[
			{"type":"text","text":"You are an autonomous coding agent."},
			{"type":"text","text":"Be terse.","cache_control":{"type":"ephemeral"}}
		],
		"messages":[
			{"role":"user","content":[
				{"type":"text","text":"# session bootstrap\n...","cache_control":{"type":"ephemeral"}},
				{"type":"text","text":"explain prometheus metrics endpoint"}
			]}
		]
	}`)
	resp, err := backend.Forward(context.Background(), providers.TierRequest{
		Body:  hermesBody,
		Model: "deepseek-chat",
	})
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}

	if bytes.Contains(receivedBody, []byte("cache_control")) {
		t.Errorf("cache_control leaked to OpenAI body: %s", string(receivedBody))
	}

	if !bytes.Contains(receivedBody, []byte(`"model":"deepseek-chat"`)) {
		t.Errorf("model override missing from body: %s", string(receivedBody))
	}
	if bytes.Contains(receivedBody, []byte("claude-opus-4-7")) {
		t.Errorf("Claude model name leaked to OpenAI provider: %s", string(receivedBody))
	}

	var sent struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(receivedBody, &sent); err != nil {
		t.Fatalf("unmarshal received body: %v", err)
	}
	if len(sent.Messages) != 2 {
		t.Fatalf("messages len = %d, want 2 (system + user)", len(sent.Messages))
	}
	if sent.Messages[0].Role != "system" {
		t.Errorf("first message role = %q, want system", sent.Messages[0].Role)
	}
	if sent.Messages[0].Content != "You are an autonomous coding agent.\nBe terse." {
		t.Errorf("system content not flattened correctly: %q", sent.Messages[0].Content)
	}
	if sent.Messages[1].Role != "user" {
		t.Errorf("second message role = %q, want user", sent.Messages[1].Role)
	}
	if !strings.Contains(sent.Messages[1].Content, "session bootstrap") ||
		!strings.Contains(sent.Messages[1].Content, "explain prometheus") {
		t.Errorf("user content blocks not flattened: %q", sent.Messages[1].Content)
	}

	if !strings.Contains(string(resp.Body), `"type":"text"`) {
		t.Errorf("response not in canonical content-block shape: %s", string(resp.Body))
	}
	if resp.InputTokens != 100 || resp.OutputTokens != 25 {
		t.Errorf("usage = (%d,%d), want (100,25)", resp.InputTokens, resp.OutputTokens)
	}
}

func TestOpenAICompatRejectsToolsField(t *testing.T) {
	httpHit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpHit = true
	}))
	defer srv.Close()
	cfg := providers.ProviderConfig{
		Name: "deepseek-direct", Type: "openai-compat", Endpoint: srv.URL,
		Model: "deepseek-chat", Family: "deepseek", APIKeyKeychain: "zen-swarm/deepseek",
	}
	kc := fakeResolver{entries: map[string]string{"zen-swarm/deepseek": "sk-ds-test"}}
	backend, err := providers.NewOpenAICompatBackend(cfg, kc)
	if err != nil {
		t.Fatalf("NewOpenAICompatBackend: %v", err)
	}
	_, err = backend.Forward(context.Background(), providers.TierRequest{
		Body: []byte(`{
			"model":"claude-x","max_tokens":1,
			"tools":[{"name":"get_weather","input_schema":{"type":"object"}}],
			"messages":[{"role":"user","content":"hi"}]
		}`),
		Model: "deepseek-chat",
	})
	if err == nil {
		t.Fatal("Forward accepted body with tools array")
	}
	if !errors.Is(err, providers.ErrToolsUnsupported) {
		t.Errorf("err = %v, want errors.Is(err, providers.ErrToolsUnsupported)", err)
	}
	if httpHit {
		t.Error("Forward MUST short-circuit without an HTTP call when tools field present")
	}
}
