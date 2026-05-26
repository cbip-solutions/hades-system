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

func TestGeminiForwardTranslates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		if !strings.Contains(r.URL.Path, "gemini-2.0-flash:generateContent") {
			t.Errorf("path = %q, want .../models/gemini-2.0-flash:generateContent", r.URL.Path)
		}

		if got := r.URL.Query().Get("key"); got != "ai-studio-test" {
			t.Errorf("?key= = %q, want ai-studio-test", got)
		}

		var req struct {
			Contents          []json.RawMessage `json:"contents"`
			SystemInstruction json.RawMessage   `json:"systemInstruction"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode translated gemini request: %v", err)
		}
		if len(req.Contents) != 1 {
			t.Errorf("contents len = %d, want 1", len(req.Contents))
		}
		if len(req.SystemInstruction) == 0 {
			t.Error("systemInstruction missing from translated request")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"candidates": []any{map[string]any{
				"content":      map[string]any{"role": "model", "parts": []any{map[string]any{"text": "gemini reply"}}},
				"finishReason": "STOP",
			}},
			"usageMetadata": map[string]any{"promptTokenCount": 22, "candidatesTokenCount": 11},
		})
	}))
	defer srv.Close()

	cfg := providers.ProviderConfig{
		Name: "gemini-flash", Type: "gemini", Endpoint: srv.URL,
		Model: "gemini-2.0-flash", Family: "gemini", APIKeyKeychain: "zen-swarm/google-ai",
	}
	kc := fakeResolver{entries: map[string]string{"zen-swarm/google-ai": "ai-studio-test"}}
	backend, err := providers.NewGeminiBackend(cfg, kc)
	if err != nil {
		t.Fatalf("NewGeminiBackend: %v", err)
	}
	resp, err := backend.Forward(context.Background(), providers.TierRequest{
		Body:  []byte(`{"model":"x","system":"sys","messages":[{"role":"user","content":"hi"}],"max_tokens":64}`),
		Model: "gemini-2.0-flash",
	})
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	if resp.TierUsed != providers.TierGemini {
		t.Errorf("TierUsed = %v, want TierGemini", resp.TierUsed)
	}
	if resp.InputTokens != 22 || resp.OutputTokens != 11 {
		t.Errorf("usage = (%d,%d), want (22,11)", resp.InputTokens, resp.OutputTokens)
	}
	if !strings.Contains(string(resp.Body), "gemini reply") {
		t.Errorf("canonical body missing text: %s", string(resp.Body))
	}
	if !strings.Contains(string(resp.Body), `"type":"text"`) {
		t.Errorf("response not translated to canonical shape: %s", string(resp.Body))
	}
}

func TestGeminiNameTier(t *testing.T) {
	cfg := providers.ProviderConfig{
		Name: "gemini-pro", Type: "gemini", Endpoint: "https://generativelanguage.googleapis.com",
		Model: "gemini-2.0-pro", Family: "gemini", APIKeyKeychain: "zen-swarm/google-ai",
	}
	kc := fakeResolver{entries: map[string]string{"zen-swarm/google-ai": "ai-studio-test"}}
	backend, err := providers.NewGeminiBackend(cfg, kc)
	if err != nil {
		t.Fatalf("NewGeminiBackend: %v", err)
	}
	if backend.Name() != "gemini-pro" {
		t.Errorf("Name = %q, want gemini-pro", backend.Name())
	}
	if backend.Tier() != providers.TierGemini {
		t.Errorf("Tier = %v, want TierGemini", backend.Tier())
	}
}

func TestGeminiCapabilities(t *testing.T) {
	cfg := providers.ProviderConfig{
		Name: "gemini-pro", Type: "gemini", Endpoint: "https://generativelanguage.googleapis.com",
		Model: "gemini-2.0-pro", Family: "gemini", APIKeyKeychain: "zen-swarm/google-ai",
	}
	kc := fakeResolver{entries: map[string]string{"zen-swarm/google-ai": "ai-studio-test"}}
	backend, err := providers.NewGeminiBackend(cfg, kc)
	if err != nil {
		t.Fatalf("NewGeminiBackend: %v", err)
	}
	caps := backend.Capabilities()
	if caps.SupportsStreaming || caps.SupportsToolUse || caps.SupportsVision || caps.SupportsPromptCaching {
		t.Errorf("Capabilities advertise unsupported feature: %+v", caps)
	}
	if caps.MaxContextTokens != 1_000_000 {
		t.Errorf("MaxContextTokens = %d, want 1_000_000", caps.MaxContextTokens)
	}
	if caps.MaxOutputTokens != 8_192 {
		t.Errorf("MaxOutputTokens = %d, want 8_192", caps.MaxOutputTokens)
	}
}

func TestGeminiConstructorMissingKey(t *testing.T) {
	cfg := providers.ProviderConfig{
		Name: "gemini-flash", Type: "gemini", Endpoint: "https://generativelanguage.googleapis.com",
		Model: "gemini-2.0-flash", Family: "gemini", APIKeyKeychain: "zen-swarm/google-ai",
	}
	_, err := providers.NewGeminiBackend(cfg, fakeResolver{entries: map[string]string{}})
	if !errors.Is(err, keychain.ErrNotFound) {
		t.Errorf("err = %v, want chain to keychain.ErrNotFound", err)
	}
}

func TestGeminiNewResolverNil(t *testing.T) {
	cfg := providers.ProviderConfig{
		Name: "gemini-flash", Type: "gemini", Endpoint: "https://generativelanguage.googleapis.com",
		Model: "gemini-2.0-flash", Family: "gemini", APIKeyKeychain: "zen-swarm/google-ai",
	}
	_, err := providers.NewGeminiBackend(cfg, nil)
	if err == nil {
		t.Fatal("NewGeminiBackend accepted nil keychain.Resolver")
	}
}

func TestGeminiForwardNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"bad request"}}`))
	}))
	defer srv.Close()
	cfg := providers.ProviderConfig{
		Name: "gemini-flash", Type: "gemini", Endpoint: srv.URL,
		Model: "gemini-2.0-flash", Family: "gemini", APIKeyKeychain: "zen-swarm/google-ai",
	}
	kc := fakeResolver{entries: map[string]string{"zen-swarm/google-ai": "ai-studio-test"}}
	backend, err := providers.NewGeminiBackend(cfg, kc)
	if err != nil {
		t.Fatalf("NewGeminiBackend: %v", err)
	}
	resp, err := backend.Forward(context.Background(), providers.TierRequest{
		Body: []byte(`{"messages":[{"role":"user","content":"x"}]}`), Model: "gemini-2.0-flash",
	})
	if err == nil {
		t.Fatal("Forward returned nil error for a 400")
	}
	if resp != nil {
		t.Errorf("Forward returned non-nil resp on error: %+v", resp)
	}
}

func TestGeminiForwardTransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.Close()
	cfg := providers.ProviderConfig{
		Name: "gemini-flash", Type: "gemini", Endpoint: srv.URL,
		Model: "gemini-2.0-flash", Family: "gemini", APIKeyKeychain: "zen-swarm/google-ai",
	}
	kc := fakeResolver{entries: map[string]string{"zen-swarm/google-ai": "ai-studio-test"}}
	backend, err := providers.NewGeminiBackend(cfg, kc)
	if err != nil {
		t.Fatalf("NewGeminiBackend: %v", err)
	}
	resp, err := backend.Forward(context.Background(), providers.TierRequest{
		Body: []byte(`{"messages":[{"role":"user","content":"x"}]}`), Model: "gemini-2.0-flash",
	})
	if err == nil {
		t.Fatal("Forward returned nil error against closed server")
	}
	if resp != nil {
		t.Errorf("Forward returned non-nil resp on error: %+v", resp)
	}
	if !strings.Contains(err.Error(), "gemini-flash") {
		t.Errorf("error %q does not name the provider", err.Error())
	}
}

func TestGeminiForwardTranslateResponseError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("not valid json"))
	}))
	defer srv.Close()
	cfg := providers.ProviderConfig{
		Name: "gemini-flash", Type: "gemini", Endpoint: srv.URL,
		Model: "gemini-2.0-flash", Family: "gemini", APIKeyKeychain: "zen-swarm/google-ai",
	}
	kc := fakeResolver{entries: map[string]string{"zen-swarm/google-ai": "ai-studio-test"}}
	backend, err := providers.NewGeminiBackend(cfg, kc)
	if err != nil {
		t.Fatalf("NewGeminiBackend: %v", err)
	}
	resp, err := backend.Forward(context.Background(), providers.TierRequest{
		Body: []byte(`{"model":"x","messages":[{"role":"user","content":"hi"}]}`), Model: "gemini-2.0-flash",
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

func TestGeminiForwardTranslateRequestError(t *testing.T) {
	cfg := providers.ProviderConfig{
		Name: "gemini-flash", Type: "gemini", Endpoint: "https://generativelanguage.googleapis.com",
		Model: "gemini-2.0-flash", Family: "gemini", APIKeyKeychain: "zen-swarm/google-ai",
	}
	kc := fakeResolver{entries: map[string]string{"zen-swarm/google-ai": "ai-studio-test"}}
	backend, err := providers.NewGeminiBackend(cfg, kc)
	if err != nil {
		t.Fatalf("NewGeminiBackend: %v", err)
	}
	resp, err := backend.Forward(context.Background(), providers.TierRequest{
		Body: []byte("not json"), Model: "gemini-2.0-flash",
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

func TestGeminiProbeSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "gemini-2.0-flash:generateContent") {
			t.Errorf("probe path = %q, want .../models/gemini-2.0-flash:generateContent", r.URL.Path)
		}
		if got := r.URL.Query().Get("key"); got != "ai-studio-test" {
			t.Errorf("probe ?key= = %q, want ai-studio-test", got)
		}

		var got struct {
			Contents []struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"contents"`
			GenerationConfig struct {
				MaxOutputTokens int `json:"maxOutputTokens"`
			} `json:"generationConfig"`
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode probe body: %v", err)
		}
		if got.GenerationConfig.MaxOutputTokens != 1 {
			t.Errorf("probe maxOutputTokens = %d, want 1", got.GenerationConfig.MaxOutputTokens)
		}
		if len(got.Contents) != 1 || len(got.Contents[0].Parts) != 1 || got.Contents[0].Parts[0].Text != "hi" {
			t.Errorf("probe content not fixed 'hi' (inv-zen-071): %+v", got.Contents)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	cfg := providers.ProviderConfig{
		Name: "gemini-flash", Type: "gemini", Endpoint: srv.URL,
		Model: "gemini-2.0-flash", Family: "gemini", APIKeyKeychain: "zen-swarm/google-ai",
	}
	kc := fakeResolver{entries: map[string]string{"zen-swarm/google-ai": "ai-studio-test"}}
	backend, err := providers.NewGeminiBackend(cfg, kc)
	if err != nil {
		t.Fatalf("NewGeminiBackend: %v", err)
	}
	if err := backend.Probe(context.Background()); err != nil {
		t.Errorf("Probe: %v", err)
	}
}

func TestGeminiProbeUnauthorized(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
			}))
			defer srv.Close()
			cfg := providers.ProviderConfig{
				Name: "gemini-flash", Type: "gemini", Endpoint: srv.URL,
				Model: "gemini-2.0-flash", Family: "gemini", APIKeyKeychain: "zen-swarm/google-ai",
			}
			kc := fakeResolver{entries: map[string]string{"zen-swarm/google-ai": "ai-studio-test"}}
			backend, err := providers.NewGeminiBackend(cfg, kc)
			if err != nil {
				t.Fatalf("NewGeminiBackend: %v", err)
			}
			err = backend.Probe(context.Background())
			if !errors.Is(err, providers.ErrGeminiUnavailable) {
				t.Errorf("err = %v, want chain to ErrGeminiUnavailable", err)
			}
		})
	}
}

func TestGeminiProbeTransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.Close()
	cfg := providers.ProviderConfig{
		Name: "gemini-flash", Type: "gemini", Endpoint: srv.URL,
		Model: "gemini-2.0-flash", Family: "gemini", APIKeyKeychain: "zen-swarm/google-ai",
	}
	kc := fakeResolver{entries: map[string]string{"zen-swarm/google-ai": "ai-studio-test"}}
	backend, err := providers.NewGeminiBackend(cfg, kc)
	if err != nil {
		t.Fatalf("NewGeminiBackend: %v", err)
	}
	err = backend.Probe(context.Background())
	if !errors.Is(err, providers.ErrGeminiUnavailable) {
		t.Errorf("err = %v, want chain to ErrGeminiUnavailable on transport error", err)
	}
}

func TestGeminiForwardForwardsHeaders(t *testing.T) {
	var gotXZenProfile, gotXZenSession, gotContentType, gotAuthHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotXZenProfile = r.Header.Get("X-Zen-Profile")
		gotXZenSession = r.Header.Get("X-Zen-Session")
		gotContentType = r.Header.Get("Content-Type")
		gotAuthHeader = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"candidates": []any{map[string]any{
				"content":      map[string]any{"role": "model", "parts": []any{map[string]any{"text": "ok"}}},
				"finishReason": "STOP",
			}},
			"usageMetadata": map[string]any{"promptTokenCount": 1, "candidatesTokenCount": 1},
		})
	}))
	defer srv.Close()
	cfg := providers.ProviderConfig{
		Name: "gemini-flash", Type: "gemini", Endpoint: srv.URL,
		Model: "gemini-2.0-flash", Family: "gemini", APIKeyKeychain: "zen-swarm/google-ai",
		Headers: map[string]string{"X-Zen-Profile": "test-profile"},
	}
	kc := fakeResolver{entries: map[string]string{"zen-swarm/google-ai": "ai-studio-test"}}
	backend, err := providers.NewGeminiBackend(cfg, kc)
	if err != nil {
		t.Fatalf("NewGeminiBackend: %v", err)
	}
	_, err = backend.Forward(context.Background(), providers.TierRequest{
		Body:  []byte(`{"model":"x","messages":[{"role":"user","content":"hi"}],"max_tokens":1}`),
		Model: "gemini-2.0-flash",
		Headers: map[string]string{
			"X-Zen-Session": "sess-456",

			"Content-Type":  "text/plain",
			"Authorization": "Bearer hijacked",
		},
	})
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	if gotXZenProfile != "test-profile" {
		t.Errorf("X-Zen-Profile = %q, want test-profile (operator-static header)", gotXZenProfile)
	}
	if gotXZenSession != "sess-456" {
		t.Errorf("X-Zen-Session = %q, want sess-456", gotXZenSession)
	}

	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json (backend-managed)", gotContentType)
	}

	if gotAuthHeader == "Bearer hijacked" {
		t.Errorf("Authorization header was not filtered: %q", gotAuthHeader)
	}
}

func TestGeminiForwardForwardsCredentials(t *testing.T) {
	var gotSecretToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSecretToken = r.Header.Get("X-Secret-Token")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"candidates": []any{map[string]any{
				"content":      map[string]any{"role": "model", "parts": []any{map[string]any{"text": "ok"}}},
				"finishReason": "STOP",
			}},
			"usageMetadata": map[string]any{"promptTokenCount": 1, "candidatesTokenCount": 1},
		})
	}))
	defer srv.Close()
	cfg := providers.ProviderConfig{
		Name: "gemini-flash", Type: "gemini", Endpoint: srv.URL,
		Model: "gemini-2.0-flash", Family: "gemini", APIKeyKeychain: "zen-swarm/google-ai",
	}
	kc := fakeResolver{entries: map[string]string{"zen-swarm/google-ai": "ai-studio-test"}}
	backend, err := providers.NewGeminiBackend(cfg, kc)
	if err != nil {
		t.Fatalf("NewGeminiBackend: %v", err)
	}
	_, err = backend.Forward(context.Background(), providers.TierRequest{
		Body:  []byte(`{"model":"x","messages":[{"role":"user","content":"hi"}],"max_tokens":1}`),
		Model: "gemini-2.0-flash",
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

func TestGeminiProbeForwardsOperatorHeaders(t *testing.T) {
	var gotXZenProfile string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotXZenProfile = r.Header.Get("X-Zen-Profile")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	cfg := providers.ProviderConfig{
		Name: "gemini-flash", Type: "gemini", Endpoint: srv.URL,
		Model: "gemini-2.0-flash", Family: "gemini", APIKeyKeychain: "zen-swarm/google-ai",
		Headers: map[string]string{"X-Zen-Profile": "probe-profile"},
	}
	kc := fakeResolver{entries: map[string]string{"zen-swarm/google-ai": "ai-studio-test"}}
	backend, err := providers.NewGeminiBackend(cfg, kc)
	if err != nil {
		t.Fatalf("NewGeminiBackend: %v", err)
	}
	if err := backend.Probe(context.Background()); err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if gotXZenProfile != "probe-profile" {
		t.Errorf("Probe X-Zen-Profile = %q, want probe-profile (operator-static header)", gotXZenProfile)
	}
}

func TestGeminiClose(t *testing.T) {
	cfg := providers.ProviderConfig{
		Name: "gemini-flash", Type: "gemini", Endpoint: "https://generativelanguage.googleapis.com",
		Model: "gemini-2.0-flash", Family: "gemini", APIKeyKeychain: "zen-swarm/google-ai",
	}
	kc := fakeResolver{entries: map[string]string{"zen-swarm/google-ai": "ai-studio-test"}}
	backend, err := providers.NewGeminiBackend(cfg, kc)
	if err != nil {
		t.Fatalf("NewGeminiBackend: %v", err)
	}
	if err := backend.Close(); err != nil {
		t.Errorf("first Close: %v", err)
	}
	if err := backend.Close(); err != nil {
		t.Errorf("second Close (idempotent): %v", err)
	}
}

func TestGeminiForwardHermesShapedBody(t *testing.T) {
	var receivedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "gemini-2.0-flash:generateContent") {
			t.Errorf("path = %q, want .../models/gemini-2.0-flash:generateContent", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		receivedBody = body
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"candidates": []any{map[string]any{
				"content":      map[string]any{"role": "model", "parts": []any{map[string]any{"text": "gemini hermes reply"}}},
				"finishReason": "STOP",
			}},
			"usageMetadata": map[string]any{"promptTokenCount": 80, "candidatesTokenCount": 18},
		})
	}))
	defer srv.Close()
	cfg := providers.ProviderConfig{
		Name: "gemini-flash", Type: "gemini", Endpoint: srv.URL,
		Model: "gemini-2.0-flash", Family: "gemini", APIKeyKeychain: "zen-swarm/google-ai",
	}
	kc := fakeResolver{entries: map[string]string{"zen-swarm/google-ai": "ai-studio-test"}}
	backend, err := providers.NewGeminiBackend(cfg, kc)
	if err != nil {
		t.Fatalf("NewGeminiBackend: %v", err)
	}
	hermesBody := []byte(`{
		"model":"claude-opus-4-7",
		"max_tokens":2048,
		"system":[
			{"type":"text","text":"You are an autonomous coding agent."},
			{"type":"text","text":"Be terse.","cache_control":{"type":"ephemeral"}}
		],
		"messages":[
			{"role":"user","content":[
				{"type":"text","text":"context block","cache_control":{"type":"ephemeral"}},
				{"type":"text","text":"actual question"}
			]},
			{"role":"assistant","content":"prior reply"}
		]
	}`)
	resp, err := backend.Forward(context.Background(), providers.TierRequest{
		Body:  hermesBody,
		Model: "gemini-2.0-flash",
	})
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}

	if bytes.Contains(receivedBody, []byte("cache_control")) {
		t.Errorf("cache_control leaked to Gemini body: %s", string(receivedBody))
	}

	var sent struct {
		Contents []struct {
			Role  string `json:"role"`
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"contents"`
		SystemInstruction struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"systemInstruction"`
		GenerationConfig struct {
			MaxOutputTokens int `json:"maxOutputTokens"`
		} `json:"generationConfig"`
	}
	if err := json.Unmarshal(receivedBody, &sent); err != nil {
		t.Fatalf("unmarshal received gemini body: %v", err)
	}
	if len(sent.SystemInstruction.Parts) == 0 ||
		sent.SystemInstruction.Parts[0].Text != "You are an autonomous coding agent.\nBe terse." {
		t.Errorf("systemInstruction wrong: %+v", sent.SystemInstruction)
	}
	if len(sent.Contents) != 2 {
		t.Fatalf("contents len = %d, want 2", len(sent.Contents))
	}

	if sent.Contents[0].Role != "user" || len(sent.Contents[0].Parts) != 1 {
		t.Errorf("user contents wrong: %+v", sent.Contents[0])
	}
	if sent.Contents[0].Parts[0].Text != "context block\nactual question" {
		t.Errorf("user content not flattened: %q", sent.Contents[0].Parts[0].Text)
	}

	if sent.Contents[1].Role != "model" {
		t.Errorf("assistant role = %q, want model (gemini mapping)", sent.Contents[1].Role)
	}
	if sent.Contents[1].Parts[0].Text != "prior reply" {
		t.Errorf("assistant content wrong: %q", sent.Contents[1].Parts[0].Text)
	}

	if sent.GenerationConfig.MaxOutputTokens != 2048 {
		t.Errorf("maxOutputTokens = %d, want 2048", sent.GenerationConfig.MaxOutputTokens)
	}

	if !strings.Contains(string(resp.Body), `"type":"text"`) {
		t.Errorf("response not in canonical shape: %s", string(resp.Body))
	}
	if resp.InputTokens != 80 || resp.OutputTokens != 18 {
		t.Errorf("usage = (%d,%d), want (80,18)", resp.InputTokens, resp.OutputTokens)
	}
}

func TestGeminiRejectsToolsField(t *testing.T) {
	httpHit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpHit = true
	}))
	defer srv.Close()
	cfg := providers.ProviderConfig{
		Name: "gemini-flash", Type: "gemini", Endpoint: srv.URL,
		Model: "gemini-2.0-flash", Family: "gemini", APIKeyKeychain: "zen-swarm/google-ai",
	}
	kc := fakeResolver{entries: map[string]string{"zen-swarm/google-ai": "ai-studio-test"}}
	backend, err := providers.NewGeminiBackend(cfg, kc)
	if err != nil {
		t.Fatalf("NewGeminiBackend: %v", err)
	}
	_, err = backend.Forward(context.Background(), providers.TierRequest{
		Body: []byte(`{
			"model":"claude-x","max_tokens":1,
			"tools":[{"name":"get_weather","input_schema":{"type":"object"}}],
			"messages":[{"role":"user","content":"hi"}]
		}`),
		Model: "gemini-2.0-flash",
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
