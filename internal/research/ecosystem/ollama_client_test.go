//go:build cgo
// +build cgo

package ecosystem

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/research/cache"
)

type fakeRevalidator struct {
	mu       sync.Mutex
	response []byte
	status   int
	err      error

	lastURL     string
	lastBody    []byte
	lastHeaders map[string]string
	callCount   int
}

func (f *fakeRevalidator) FetchPOST(_ context.Context, url string, body []byte, headers map[string]string) (*ollamaFetchResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.callCount++
	f.lastURL = url
	f.lastBody = append([]byte(nil), body...)
	f.lastHeaders = make(map[string]string, len(headers))
	for k, v := range headers {
		f.lastHeaders[k] = v
	}
	if f.err != nil {
		return nil, f.err
	}
	return &ollamaFetchResult{Body: f.response, HTTPStatusCode: f.status}, nil
}

func TestOllamaClient_Generate_OK(t *testing.T) {
	body := `{"response":"This is a contextual retrieval prefix","eval_count":10,"prompt_eval_count":50,"done":true}`
	rv := &fakeRevalidator{response: []byte(body), status: 200}
	cli := &OllamaClient{
		BaseURL:    "http://127.0.0.1:11434",
		HTTPPOSTer: rv,
		Timeout:    5 * time.Second,
	}
	out, err := cli.Generate(context.Background(), "qwen2.5:7b", "Hello", 100)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if out != "This is a contextual retrieval prefix" {
		t.Errorf("response = %q; want %q", out, "This is a contextual retrieval prefix")
	}
	if rv.callCount != 1 {
		t.Errorf("callCount = %d; want 1", rv.callCount)
	}
	if rv.lastURL != "http://127.0.0.1:11434/api/generate" {
		t.Errorf("URL = %s; want http://127.0.0.1:11434/api/generate", rv.lastURL)
	}
	if rv.lastHeaders["Content-Type"] != "application/json" {
		t.Errorf("Content-Type header = %q; want application/json", rv.lastHeaders["Content-Type"])
	}
}

func TestOllamaClient_Generate_RequestShape(t *testing.T) {
	rv := &fakeRevalidator{response: []byte(`{"response":"ok","done":true}`), status: 200}
	cli := &OllamaClient{BaseURL: "http://127.0.0.1:11434", HTTPPOSTer: rv, Timeout: 5 * time.Second}
	_, err := cli.Generate(context.Background(), "qwen2.5:7b", "Test prompt", 80)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var sent ollamaRequest
	if err := json.Unmarshal(rv.lastBody, &sent); err != nil {
		t.Fatalf("captured body is not valid ollamaRequest JSON: %v (body=%s)", err, string(rv.lastBody))
	}
	if sent.Model != "qwen2.5:7b" {
		t.Errorf("model = %q; want qwen2.5:7b", sent.Model)
	}
	if sent.Prompt != "Test prompt" {
		t.Errorf("prompt = %q; want %q", sent.Prompt, "Test prompt")
	}
	if sent.Stream {
		t.Error("stream = true; want false")
	}
	if sent.Options.NumPredict != 80 {
		t.Errorf("options.num_predict = %d; want 80", sent.Options.NumPredict)
	}
}

func TestOllamaClient_Generate_ConnectionError(t *testing.T) {
	rv := &fakeRevalidator{err: errors.New("connection refused")}
	cli := &OllamaClient{BaseURL: "http://127.0.0.1:11434", HTTPPOSTer: rv, Timeout: 5 * time.Second}
	_, err := cli.Generate(context.Background(), "qwen2.5:7b", "Hello", 100)
	if err == nil {
		t.Fatal("expected connection error; got nil")
	}
	if !strings.Contains(err.Error(), "connection") {
		t.Errorf("error should mention connection; got %v", err)
	}
}

func TestOllamaClient_Generate_NonJSONResponse(t *testing.T) {
	rv := &fakeRevalidator{response: []byte("not json"), status: 200}
	cli := &OllamaClient{BaseURL: "http://127.0.0.1:11434", HTTPPOSTer: rv, Timeout: 5 * time.Second}
	_, err := cli.Generate(context.Background(), "qwen2.5:7b", "Hello", 100)
	if err == nil {
		t.Fatal("expected JSON parse error; got nil")
	}
	var je *json.SyntaxError
	if !errors.As(err, &je) && !strings.Contains(err.Error(), "json") {
		t.Errorf("expected JSON-related error; got %v", err)
	}
}

func TestOllamaClient_Generate_HTTPErrorStatus(t *testing.T) {
	rv := &fakeRevalidator{response: []byte(`{"error":"model not found"}`), status: 404}
	cli := &OllamaClient{BaseURL: "http://127.0.0.1:11434", HTTPPOSTer: rv, Timeout: 5 * time.Second}
	_, err := cli.Generate(context.Background(), "missing:model", "Hello", 100)
	if err == nil {
		t.Fatal("expected HTTP error for 404; got nil")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("error should mention HTTP 404 status; got %v", err)
	}
}

func TestOllamaClient_Generate_500Error(t *testing.T) {
	rv := &fakeRevalidator{response: []byte(`{"error":"out of memory"}`), status: 500}
	cli := &OllamaClient{BaseURL: "http://127.0.0.1:11434", HTTPPOSTer: rv, Timeout: 5 * time.Second}
	_, err := cli.Generate(context.Background(), "qwen2.5:7b", "Hello", 100)
	if err == nil {
		t.Fatal("expected HTTP error for 500; got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention HTTP 500 status; got %v", err)
	}
}

func TestOllamaClient_Generate_DefaultsApplied(t *testing.T) {
	rv := &fakeRevalidator{response: []byte(`{"response":"ok","done":true}`), status: 200}
	cli := &OllamaClient{HTTPPOSTer: rv}
	_, err := cli.Generate(context.Background(), "qwen2.5:7b", "Hello", 100)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if rv.lastURL != "http://127.0.0.1:11434/api/generate" {
		t.Errorf("default URL = %s; want http://127.0.0.1:11434/api/generate", rv.lastURL)
	}
	if cli.BaseURL != "http://127.0.0.1:11434" {
		t.Errorf("BaseURL not defaulted: got %q", cli.BaseURL)
	}
	if cli.Timeout != 60*time.Second {
		t.Errorf("Timeout not defaulted: got %v", cli.Timeout)
	}
}

func TestOllamaClient_Generate_MissingPOSTer(t *testing.T) {
	cli := &OllamaClient{BaseURL: "http://127.0.0.1:11434"}
	_, err := cli.Generate(context.Background(), "qwen2.5:7b", "Hello", 100)
	if err == nil {
		t.Fatal("expected error for missing HTTPPOSTer; got nil")
	}
	if !strings.Contains(err.Error(), "HTTPPOSTer") {
		t.Errorf("error should mention HTTPPOSTer; got %v", err)
	}
}

func TestOllamaClient_Generate_GeneratorSeam(t *testing.T) {
	calls := 0
	var capturedModel, capturedPrompt string
	var capturedNumPredict int
	cli := &OllamaClient{
		generator: &funcGenerator{
			fn: func(_ context.Context, model, prompt string, numPredict int) (string, error) {
				calls++
				capturedModel = model
				capturedPrompt = prompt
				capturedNumPredict = numPredict
				return "from generator", nil
			},
		},

		HTTPPOSTer: &panicPOSTer{},
	}
	out, err := cli.Generate(context.Background(), "any-model", "any-prompt", 50)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if out != "from generator" {
		t.Errorf("output = %q; want from generator", out)
	}
	if calls != 1 {
		t.Errorf("calls = %d; want 1", calls)
	}
	if capturedModel != "any-model" || capturedPrompt != "any-prompt" || capturedNumPredict != 50 {
		t.Errorf("unexpected captured: model=%q prompt=%q numPredict=%d", capturedModel, capturedPrompt, capturedNumPredict)
	}
}

type funcGenerator struct {
	fn func(ctx context.Context, model, prompt string, numPredict int) (string, error)
}

func (g *funcGenerator) Generate(ctx context.Context, model, prompt string, numPredict int) (string, error) {
	return g.fn(ctx, model, prompt, numPredict)
}

type panicPOSTer struct{}

func (panicPOSTer) FetchPOST(context.Context, string, []byte, map[string]string) (*ollamaFetchResult, error) {
	panic("FetchPOST must not run when generator seam is set")
}

func TestNewOllamaPOSTerAdapter_NilRevalidator(t *testing.T) {
	_, err := NewOllamaPOSTerAdapter(nil)
	if err == nil {
		t.Fatal("expected error for nil revalidator; got nil")
	}
	if !strings.Contains(err.Error(), "non-nil") {
		t.Errorf("error should mention non-nil requirement; got %v", err)
	}
}

func TestNewOllamaPOSTerAdapter_HappyPath(t *testing.T) {
	rv := cache.NewRevalidator(cache.ValidateOpts{})
	adapter, err := NewOllamaPOSTerAdapter(rv)
	if err != nil {
		t.Fatalf("NewOllamaPOSTerAdapter: %v", err)
	}
	if adapter == nil {
		t.Fatal("adapter is nil")
	}
}

func TestOllamaPOSTerAdapter_FetchPOST_UpstreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
	}))

	srv.Close()

	rv := cache.NewRevalidator(cache.ValidateOpts{Client: srv.Client(), Timeout: 500 * time.Millisecond})
	adapter, err := NewOllamaPOSTerAdapter(rv)
	if err != nil {
		t.Fatalf("NewOllamaPOSTerAdapter: %v", err)
	}
	cli := &OllamaClient{
		BaseURL:    srv.URL,
		HTTPPOSTer: adapter,
		Timeout:    1 * time.Second,
	}
	_, err = cli.Generate(context.Background(), "qwen2.5:7b", "Hello", 100)
	if err == nil {
		t.Fatal("expected error when server is closed; got nil")
	}
}

func TestOllamaPOSTerAdapter_FetchPOST_EndToEnd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s; want POST", r.Method)
		}

		var got ollamaRequest
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		if got.Model != "qwen2.5:7b" {
			t.Errorf("model = %s; want qwen2.5:7b", got.Model)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"response":"e2e adapter ok","done":true}`))
	}))
	defer srv.Close()

	rv := cache.NewRevalidator(cache.ValidateOpts{Client: srv.Client(), Timeout: 5 * time.Second})
	adapter, err := NewOllamaPOSTerAdapter(rv)
	if err != nil {
		t.Fatalf("NewOllamaPOSTerAdapter: %v", err)
	}
	cli := &OllamaClient{
		BaseURL:    srv.URL,
		HTTPPOSTer: adapter,
		Timeout:    5 * time.Second,
	}
	out, err := cli.Generate(context.Background(), "qwen2.5:7b", "Hello", 100)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if out != "e2e adapter ok" {
		t.Errorf("response = %q; want %q", out, "e2e adapter ok")
	}
}
