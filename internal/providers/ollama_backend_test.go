package providers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOllamaBackend_Identity(t *testing.T) {
	b, err := NewOllamaBackend(ProviderConfig{
		Name:     "ollama-qwen-coder",
		Type:     "ollama",
		Endpoint: "http://localhost:11434",
		Model:    "qwen2.5-coder:32b",
		Family:   "local-qwen",
	})
	if err != nil {
		t.Fatalf("NewOllamaBackend: unexpected error: %v", err)
	}
	if got := b.Name(); got != "ollama-qwen-coder" {
		t.Errorf("Name() = %q, want %q", got, "ollama-qwen-coder")
	}
	if got := b.Tier(); got != TierOllama {
		t.Errorf("Tier() = %v, want TierOllama", got)
	}
	caps := b.Capabilities()
	if !caps.SupportsToolUse {
		t.Error("Capabilities().SupportsToolUse = false, want true")
	}
	if caps.MaxContextTokens <= 0 {
		t.Error("Capabilities().MaxContextTokens must be positive")
	}
}

func TestOllamaBackend_ForwardTranslatesAndExtractsUsage(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"model": "qwen2.5-coder:32b",
			"choices": [{"message": {"role": "assistant", "content": "hello"}}],
			"usage": {"prompt_tokens": 11, "completion_tokens": 7}
		}`))
	}))
	defer srv.Close()

	b, err := NewOllamaBackend(ProviderConfig{
		Name: "ollama-qwen-coder", Type: "ollama", Endpoint: srv.URL,
		Model: "qwen2.5-coder:32b", Family: "local-qwen",
	})
	if err != nil {
		t.Fatalf("NewOllamaBackend: %v", err)
	}
	canonical := []byte(`{"model":"qwen2.5-coder:32b","max_tokens":256,"messages":[{"role":"user","content":"hi"}]}`)
	resp, err := b.Forward(context.Background(), TierRequest{Body: canonical, Model: "qwen2.5-coder:32b"})
	if err != nil {
		t.Fatalf("Forward: unexpected error: %v", err)
	}
	if gotPath != "/v1/chat/completions" {
		t.Errorf("request path = %q, want /v1/chat/completions", gotPath)
	}
	if resp.Status != http.StatusOK {
		t.Errorf("resp.Status = %d, want 200", resp.Status)
	}
	if resp.TierUsed != TierOllama {
		t.Errorf("resp.TierUsed = %v, want TierOllama", resp.TierUsed)
	}
	if resp.InputTokens != 11 || resp.OutputTokens != 7 {
		t.Errorf("tokens = (%d,%d), want (11,7)", resp.InputTokens, resp.OutputTokens)
	}
}

func TestOllamaBackend_ForwardNon2xxIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"model not found"}`))
	}))
	defer srv.Close()
	b, _ := NewOllamaBackend(ProviderConfig{
		Name: "ollama-x", Type: "ollama", Endpoint: srv.URL, Model: "m", Family: "local-qwen",
	})
	_, err := b.Forward(context.Background(), TierRequest{Body: []byte(`{}`), Model: "m"})
	if err == nil {
		t.Fatal("Forward on 500: want error, got nil")
	}
	if !strings.Contains(err.Error(), "ollama") {
		t.Errorf("error %q must mention 'ollama'", err.Error())
	}
}

func TestOllamaBackend_ProbeOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"models":[]}`))
	}))
	defer srv.Close()
	b, _ := NewOllamaBackend(ProviderConfig{
		Name: "ollama-x", Type: "ollama", Endpoint: srv.URL, Model: "m", Family: "local-qwen",
	})
	if err := b.Probe(context.Background()); err != nil {
		t.Errorf("Probe: unexpected error: %v", err)
	}
}

func TestOllamaBackend_ProbeUnreachable(t *testing.T) {
	b, _ := NewOllamaBackend(ProviderConfig{
		Name: "ollama-x", Type: "ollama", Endpoint: "http://127.0.0.1:1", Model: "m", Family: "local-qwen",
	})
	err := b.Probe(context.Background())
	if err == nil {
		t.Fatal("Probe on dead endpoint: want error, got nil")
	}
	if !errors.Is(err, ErrOllamaUnavailable) {
		t.Errorf("Probe error must wrap ErrOllamaUnavailable, got %v", err)
	}
}

func TestNewOllamaBackend_RejectsEmptyFields(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  ProviderConfig
	}{
		{"empty name", ProviderConfig{Type: "ollama", Endpoint: "http://x", Model: "m"}},
		{"empty endpoint", ProviderConfig{Name: "n", Type: "ollama", Model: "m"}},
		{"empty model", ProviderConfig{Name: "n", Type: "ollama", Endpoint: "http://x"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewOllamaBackend(tc.cfg); err == nil {
				t.Errorf("NewOllamaBackend(%s): want error, got nil", tc.name)
			}
		})
	}
}

func TestOllamaBackend_ForwardCtxCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
	}))
	defer srv.Close()
	b, _ := NewOllamaBackend(ProviderConfig{
		Name: "ollama-x", Type: "ollama", Endpoint: srv.URL, Model: "m", Family: "local-qwen",
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := b.Forward(ctx, TierRequest{Body: []byte(`{}`), Model: "m"}); err == nil {
		t.Fatal("Forward with cancelled ctx: want error, got nil")
	}
}

func TestOllamaBackend_Close(t *testing.T) {
	b, _ := NewOllamaBackend(ProviderConfig{
		Name: "ollama-x", Type: "ollama", Endpoint: "http://localhost:11434", Model: "m", Family: "local-qwen",
	})
	if err := b.Close(); err != nil {
		t.Errorf("Close: unexpected error: %v", err)
	}
}

func TestOllamaBackend_ForwardBadCanonicalBody(t *testing.T) {

	b, _ := NewOllamaBackend(ProviderConfig{
		Name: "ollama-x", Type: "ollama", Endpoint: "http://localhost:11434", Model: "m", Family: "local-qwen",
	})
	_, err := b.Forward(context.Background(), TierRequest{Body: []byte("not-json"), Model: "m"})
	if err == nil {
		t.Fatal("Forward with malformed canonical body: want error, got nil")
	}
	if !strings.Contains(err.Error(), "ollama") {
		t.Errorf("error %q must mention 'ollama'", err.Error())
	}
}

func TestOllamaBackend_ForwardBadResponseBody(t *testing.T) {

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not-valid-json`))
	}))
	defer srv.Close()
	b, _ := NewOllamaBackend(ProviderConfig{
		Name: "ollama-x", Type: "ollama", Endpoint: srv.URL, Model: "m", Family: "local-qwen",
	})
	_, err := b.Forward(context.Background(), TierRequest{
		Body:  []byte(`{"model":"m","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`),
		Model: "m",
	})
	if err == nil {
		t.Fatal("Forward with non-JSON 200 response: want error, got nil")
	}
	if !strings.Contains(err.Error(), "ollama") {
		t.Errorf("error %q must mention 'ollama'", err.Error())
	}
}

func TestOllamaBackend_ForwardPassesHeaders(t *testing.T) {

	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Zen-Test")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"model": "m",
			"choices": [{"message": {"role": "assistant", "content": "ok"}}],
			"usage": {"prompt_tokens": 1, "completion_tokens": 1}
		}`))
	}))
	defer srv.Close()
	b, _ := NewOllamaBackend(ProviderConfig{
		Name: "ollama-x", Type: "ollama", Endpoint: srv.URL, Model: "m", Family: "local-qwen",
	})
	_, err := b.Forward(context.Background(), TierRequest{
		Body:    []byte(`{"model":"m","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`),
		Model:   "m",
		Headers: map[string]string{"X-Zen-Test": "sentinel", "Content-Type": "text/plain"},
	})
	if err != nil {
		t.Fatalf("Forward: unexpected error: %v", err)
	}
	if gotHeader != "sentinel" {
		t.Errorf("X-Zen-Test header = %q, want %q", gotHeader, "sentinel")
	}
}

func TestOllamaBackend_ProbeNon2xx(t *testing.T) {

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	b, _ := NewOllamaBackend(ProviderConfig{
		Name: "ollama-x", Type: "ollama", Endpoint: srv.URL, Model: "m", Family: "local-qwen",
	})
	err := b.Probe(context.Background())
	if err == nil {
		t.Fatal("Probe on 503: want error, got nil")
	}
	if !errors.Is(err, ErrOllamaUnavailable) {
		t.Errorf("Probe error must wrap ErrOllamaUnavailable, got %v", err)
	}
}

func TestOllamaBackend_RejectsToolsField(t *testing.T) {
	httpHit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpHit = true
	}))
	defer srv.Close()
	b, err := NewOllamaBackend(ProviderConfig{
		Name: "ollama-x", Type: "ollama", Endpoint: srv.URL, Model: "qwen2.5-coder:32b", Family: "local-qwen",
	})
	if err != nil {
		t.Fatalf("NewOllamaBackend: %v", err)
	}
	_, err = b.Forward(context.Background(), TierRequest{
		Body: []byte(`{
			"model":"qwen","max_tokens":1,
			"tools":[{"name":"get_weather","input_schema":{"type":"object"}}],
			"messages":[{"role":"user","content":"hi"}]
		}`),
		Model: "qwen2.5-coder:32b",
	})
	if err == nil {
		t.Fatal("Forward accepted body with tools array")
	}
	if !errors.Is(err, ErrToolsUnsupported) {
		t.Errorf("err = %v, want errors.Is(err, ErrToolsUnsupported)", err)
	}
	if httpHit {
		t.Error("Forward MUST short-circuit without an HTTP call when tools field present")
	}
}
