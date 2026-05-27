// SPDX-License-Identifier: MIT
// internal/providers/sidecar_backend_test.go
//
// External-package tests for SidecarBackend. The
// SidecarBackend is the Tier 1 HTTP client that talks to the private
// zen-bypass-tier1 sidecar running on loopback. invariant graceful
// degradation: a connection-refused / timeout / 5xx response surfaces as
// a typed sentinel (ErrSidecarUnavailable / ErrSidecarDegraded) so the
// dispatcher's existing cascade-iteration logic (BackendRegistry +
// ProfileResolver per invariant frozen contract C8) falls through to
// the next named provider in the operator's profile cascade (
// direct backends per ADR-0093).
package providers_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/providers"
)

func TestSidecarBackend_ForwardSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("path = %q; want /v1/messages", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %q; want POST", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %q; want application/json", r.Header.Get("Content-Type"))
		}

		respBody, _ := json.Marshal(map[string]any{
			"id":    "msg_01SIDECAR",
			"model": "claude-haiku-4-5",
			"content": []any{
				map[string]any{"type": "text", "text": "sidecar reply"},
			},
			"usage": map[string]any{
				"input_tokens":  25,
				"output_tokens": 12,
			},
		})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(respBody)
	}))
	defer srv.Close()

	backend := providers.NewSidecarBackend(srv.URL, 30*time.Second)
	defer backend.Close()

	resp, err := backend.Forward(context.Background(), providers.TierRequest{
		Method: "POST",
		Path:   "/v1/messages",
		Body:   []byte(`{"model":"claude-haiku-4-5","messages":[{"role":"user","content":"hi"}]}`),
		Model:  "claude-haiku-4-5",
	})
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	if resp.Status != 200 {
		t.Errorf("Status = %d; want 200", resp.Status)
	}
	if resp.TierUsed != providers.TierInHouse {
		t.Errorf("TierUsed = %v; want TierInHouse", resp.TierUsed)
	}
	if !strings.Contains(string(resp.Body), "sidecar reply") {
		t.Errorf("Body missing canned text:\n%s", string(resp.Body))
	}
	if resp.InputTokens != 25 || resp.OutputTokens != 12 {
		t.Errorf("token usage = (%d, %d); want (25, 12)", resp.InputTokens, resp.OutputTokens)
	}
	if resp.ModelUsed != "claude-haiku-4-5" {
		t.Errorf("ModelUsed = %q; want claude-haiku-4-5", resp.ModelUsed)
	}
	if resp.LatencyMs < 0 {
		t.Errorf("LatencyMs = %d; want non-negative", resp.LatencyMs)
	}
}

func TestSidecarBackend_ForwardModelFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer srv.Close()

	backend := providers.NewSidecarBackend(srv.URL, 5*time.Second)
	defer backend.Close()

	resp, err := backend.Forward(context.Background(), providers.TierRequest{
		Body:  []byte(`{"model":"claude-haiku-4-5"}`),
		Model: "claude-haiku-4-5",
	})
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	if resp.ModelUsed != "claude-haiku-4-5" {
		t.Errorf("ModelUsed = %q; want fallback to req.Model claude-haiku-4-5", resp.ModelUsed)
	}
}

func TestSidecarBackend_IdempotencyKeyHeaderInjected(t *testing.T) {
	var observedKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observedKey = r.Header.Get("Idempotency-Key")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	backend := providers.NewSidecarBackend(srv.URL, 5*time.Second)
	defer backend.Close()

	_, err := backend.Forward(context.Background(), providers.TierRequest{
		Body:           []byte(`{}`),
		IdempotencyKey: "idem-99",
	})
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	if observedKey != "idem-99" {
		t.Errorf("Idempotency-Key header = %q; want idem-99", observedKey)
	}
}

func TestSidecarBackend_EmptyIdempotencyKeyOmitsHeader(t *testing.T) {
	var observedHasKey bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, observedHasKey = r.Header["Idempotency-Key"]
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	backend := providers.NewSidecarBackend(srv.URL, 5*time.Second)
	defer backend.Close()

	_, err := backend.Forward(context.Background(), providers.TierRequest{Body: []byte(`{}`)})
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	if observedHasKey {
		t.Errorf("Idempotency-Key header present; expected omitted for empty IdempotencyKey")
	}
}

func TestSidecarBackend_ConnectionRefused_ReturnsErrSidecarUnavailable(t *testing.T) {

	backend := providers.NewSidecarBackend("http://127.0.0.1:1", 1*time.Second)
	defer backend.Close()

	_, err := backend.Forward(context.Background(), providers.TierRequest{Body: []byte(`{}`)})
	if err == nil {
		t.Fatal("err = nil; want ErrSidecarUnavailable")
	}
	if !errors.Is(err, providers.ErrSidecarUnavailable) {
		t.Errorf("err = %v; want ErrSidecarUnavailable (errors.Is match for dispatcher's fallback condition)", err)
	}
}

func TestSidecarBackend_5xx_ReturnsErrSidecarDegraded(t *testing.T) {
	for _, status := range []int{500, 502, 503, 504, 599} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"error":"sidecar overloaded"}`))
			}))
			defer srv.Close()

			backend := providers.NewSidecarBackend(srv.URL, 5*time.Second)
			defer backend.Close()

			_, err := backend.Forward(context.Background(), providers.TierRequest{Body: []byte(`{}`)})
			if err == nil {
				t.Fatalf("err = nil; want ErrSidecarDegraded for %d", status)
			}
			if !errors.Is(err, providers.ErrSidecarDegraded) {
				t.Errorf("err = %v; want ErrSidecarDegraded", err)
			}
		})
	}
}

func TestSidecarBackend_Timeout_ReturnsErrSidecarUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		time.Sleep(500 * time.Millisecond)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	backend := providers.NewSidecarBackend(srv.URL, 50*time.Millisecond)
	defer backend.Close()

	_, err := backend.Forward(context.Background(), providers.TierRequest{Body: []byte(`{}`)})
	if err == nil {
		t.Fatal("err = nil; want ErrSidecarUnavailable (timeout maps to unavailable)")
	}
	if !errors.Is(err, providers.ErrSidecarUnavailable) {
		t.Errorf("err = %v; want ErrSidecarUnavailable", err)
	}
}

func TestSidecarBackend_FallbackChainProceedsToPlan16Cascade(t *testing.T) {
	backend := providers.NewSidecarBackend("http://127.0.0.1:1", 200*time.Millisecond)
	defer backend.Close()

	_, err := backend.Forward(context.Background(), providers.TierRequest{Body: []byte(`{}`)})
	if !errors.Is(err, providers.ErrSidecarUnavailable) {
		t.Errorf("err = %v; want ErrSidecarUnavailable (dispatcher fallback condition sister-test)", err)
	}
}

func TestSidecarBackend_4xxNotMappedToSentinel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad request"}`))
	}))
	defer srv.Close()

	backend := providers.NewSidecarBackend(srv.URL, 5*time.Second)
	defer backend.Close()

	_, err := backend.Forward(context.Background(), providers.TierRequest{Body: []byte(`{}`)})
	if err == nil {
		t.Fatal("err = nil; want a non-sentinel 4xx error")
	}
	if errors.Is(err, providers.ErrSidecarUnavailable) {
		t.Errorf("err = %v; 4xx incorrectly mapped to ErrSidecarUnavailable", err)
	}
	if errors.Is(err, providers.ErrSidecarDegraded) {
		t.Errorf("err = %v; 4xx incorrectly mapped to ErrSidecarDegraded", err)
	}
}

func TestSidecarBackend_ContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		time.Sleep(500 * time.Millisecond)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	backend := providers.NewSidecarBackend(srv.URL, 5*time.Second)
	defer backend.Close()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	_, err := backend.Forward(ctx, providers.TierRequest{Body: []byte(`{}`)})
	if err == nil {
		t.Fatal("err = nil; want ErrSidecarUnavailable for cancelled ctx")
	}
	if !errors.Is(err, providers.ErrSidecarUnavailable) {
		t.Errorf("err = %v; want ErrSidecarUnavailable", err)
	}
}

func TestSidecarBackend_ProbeHits_HealthEndpoint(t *testing.T) {
	var observedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	backend := providers.NewSidecarBackend(srv.URL, 5*time.Second)
	defer backend.Close()

	if err := backend.Probe(context.Background()); err != nil {
		t.Errorf("Probe: %v; want nil", err)
	}
	if observedPath != "/health" {
		t.Errorf("Probe path = %q; want /health (inv-zen-071 content-free probe)", observedPath)
	}
}

func TestSidecarBackend_ProbeFails_OnNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	backend := providers.NewSidecarBackend(srv.URL, 5*time.Second)
	defer backend.Close()

	err := backend.Probe(context.Background())
	if err == nil {
		t.Fatal("Probe err = nil; want non-nil for 503 /health")
	}
	if !errors.Is(err, providers.ErrSidecarUnavailable) {
		t.Errorf("Probe err = %v; want ErrSidecarUnavailable", err)
	}
}

func TestSidecarBackend_ProbeFails_OnConnectionRefused(t *testing.T) {
	backend := providers.NewSidecarBackend("http://127.0.0.1:1", 200*time.Millisecond)
	defer backend.Close()

	err := backend.Probe(context.Background())
	if err == nil {
		t.Fatal("Probe err = nil; want non-nil for refused /health")
	}
	if !errors.Is(err, providers.ErrSidecarUnavailable) {
		t.Errorf("Probe err = %v; want ErrSidecarUnavailable", err)
	}
}

func TestSidecarBackend_Name(t *testing.T) {
	backend := providers.NewSidecarBackend("http://127.0.0.1:1", time.Second)
	defer backend.Close()
	if got, want := backend.Name(), "bypass-sidecar"; got != want {
		t.Errorf("Name = %q; want %q (stable registry key — MUST NOT change across releases)", got, want)
	}
}

func TestSidecarBackend_Tier(t *testing.T) {
	backend := providers.NewSidecarBackend("http://127.0.0.1:1", time.Second)
	defer backend.Close()
	if got, want := backend.Tier(), providers.TierInHouse; got != want {
		t.Errorf("Tier = %v; want TierInHouse", got)
	}
}

func TestSidecarBackend_Capabilities(t *testing.T) {
	backend := providers.NewSidecarBackend("http://127.0.0.1:1", time.Second)
	defer backend.Close()
	caps := backend.Capabilities()
	if !caps.SupportsToolUse {
		t.Error("SupportsToolUse = false; want true (Anthropic API supports tool use)")
	}
	if !caps.SupportsVision {
		t.Error("SupportsVision = false; want true")
	}
	if !caps.SupportsPromptCaching {
		t.Error("SupportsPromptCaching = false; want true")
	}
	if caps.MaxContextTokens < 200_000 {
		t.Errorf("MaxContextTokens = %d; want >= 200000", caps.MaxContextTokens)
	}
	if caps.MaxOutputTokens < 64_000 {
		t.Errorf("MaxOutputTokens = %d; want >= 64000", caps.MaxOutputTokens)
	}
}

func TestSidecarBackend_CloseIsIdempotent(t *testing.T) {
	backend := providers.NewSidecarBackend("http://127.0.0.1:1", time.Second)
	if err := backend.Close(); err != nil {
		t.Errorf("Close #1: %v; want nil", err)
	}
	if err := backend.Close(); err != nil {
		t.Errorf("Close #2: %v; want nil (idempotent)", err)
	}
}

func TestSidecarBackend_BaseURLWithTrailingSlash(t *testing.T) {
	var observedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	backend := providers.NewSidecarBackend(srv.URL+"/", 5*time.Second)
	defer backend.Close()

	_, err := backend.Forward(context.Background(), providers.TierRequest{Body: []byte(`{}`)})
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	if observedPath != "/v1/messages" {
		t.Errorf("path = %q; want /v1/messages (trailing-slash tolerance)", observedPath)
	}
}

func TestSidecarBackend_InvalidBaseURL_FailsAtForward(t *testing.T) {

	backend := providers.NewSidecarBackend("://bad", 1*time.Second)
	defer backend.Close()

	_, err := backend.Forward(context.Background(), providers.TierRequest{Body: []byte(`{}`)})
	if err == nil {
		t.Fatal("err = nil; want some error for malformed URL")
	}

	if !errors.Is(err, providers.ErrSidecarUnavailable) {
		t.Errorf("err = %v; want ErrSidecarUnavailable for invalid URL (uniform fallback signal)", err)
	}
}

func TestSidecarBackend_InvalidBaseURL_ProbeAlsoFails(t *testing.T) {
	backend := providers.NewSidecarBackend("://bad", 1*time.Second)
	defer backend.Close()
	err := backend.Probe(context.Background())
	if err == nil {
		t.Fatal("Probe err = nil; want ErrSidecarUnavailable for malformed URL")
	}
	if !errors.Is(err, providers.ErrSidecarUnavailable) {
		t.Errorf("Probe err = %v; want ErrSidecarUnavailable", err)
	}
}

func TestSidecarBackend_ResponseBodyTruncationInErrorMessage(t *testing.T) {

	head := strings.Repeat("A", 512)
	tail := strings.Repeat("B", 88)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(head + tail))
	}))
	defer srv.Close()

	backend := providers.NewSidecarBackend(srv.URL, 5*time.Second)
	defer backend.Close()

	_, err := backend.Forward(context.Background(), providers.TierRequest{Body: []byte(`{}`)})
	if err == nil {
		t.Fatal("err = nil; want ErrSidecarDegraded for 500")
	}
	msg := err.Error()
	// The error message should contain only the first 512 bytes + the
	// truncation marker. The "B" suffix MUST NOT appear (credential-leak
	// guard validates the cap functioned).
	if !strings.Contains(msg, "…[truncated]") {
		t.Errorf("err = %v; want truncation marker present", err)
	}
	if strings.Contains(msg, tail) {
		t.Errorf("err = %v; truncation failed (tail B's leaked into error message)", err)
	}
}

func TestSidecarBackend_BodyReadInterrupted_ReturnsDegraded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("ResponseWriter is not Hijacker; test setup error")
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			t.Fatalf("Hijack: %v", err)
		}

		_, _ = conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 1000\r\n\r\n"))
		_ = conn.Close()
	}))
	defer srv.Close()

	backend := providers.NewSidecarBackend(srv.URL, 5*time.Second)
	defer backend.Close()

	_, err := backend.Forward(context.Background(), providers.TierRequest{Body: []byte(`{}`)})
	if err == nil {
		t.Fatal("err = nil; want ErrSidecarDegraded for body-read interruption")
	}
	if !errors.Is(err, providers.ErrSidecarDegraded) {
		t.Errorf("err = %v; want ErrSidecarDegraded for body-read interruption", err)
	}
}
