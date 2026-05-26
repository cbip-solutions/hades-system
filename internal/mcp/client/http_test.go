package client_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/mcp/client"
)

func newTestClient(t *testing.T, srv *httptest.Server) *client.Client {
	t.Helper()
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "auth-token")
	if err := os.WriteFile(tokenPath, []byte("test-token-abc"), 0600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	cfg := client.Config{
		BaseURL:       srv.URL,
		AuthTokenPath: tokenPath,
	}
	c, err := client.New(cfg)
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}
	return c
}

func TestNew_MissingTokenFile(t *testing.T) {
	cfg := client.Config{
		BaseURL:       "http://127.0.0.1:9999",
		AuthTokenPath: "/nonexistent/path/auth-token",
	}
	_, err := client.New(cfg)
	if err == nil {
		t.Fatal("expected error for missing token file, got nil")
	}
}

func TestNew_EmptyTokenRejected(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "auth-token")
	if err := os.WriteFile(tokenPath, []byte(""), 0600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	_, err := client.New(client.Config{
		BaseURL:       "http://127.0.0.1:9999",
		AuthTokenPath: tokenPath,
	})
	if err == nil {
		t.Fatal("expected error for empty token, got nil")
	}
}

func TestDo_BearerTokenSent(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL+"/v1/health", nil)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if gotAuth != "Bearer test-token-abc" {
		t.Errorf("Authorization = %q, want 'Bearer test-token-abc'", gotAuth)
	}
}

func TestDo_ContentTypeJSON(t *testing.T) {
	var gotContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			gotContentType = r.Header.Get("Content-Type")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	body := strings.NewReader(`{"key":"value"}`)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, srv.URL+"/v1/test", body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want 'application/json'", gotContentType)
	}
}

func TestWhitelist_AllowedHostsAccepted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL+"/v1/health", nil)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("allowed host rejected: %v", err)
	}
	defer resp.Body.Close()
}

func TestWhitelist_UnknownHostRejected(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "auth-token")
	if err := os.WriteFile(tokenPath, []byte("tok"), 0600); err != nil {
		t.Fatalf("write token: %v", err)
	}

	cfg := client.Config{
		SocketPath:    "/nonexistent.sock",
		AuthTokenPath: tokenPath,
	}
	c, err := client.New(cfg)
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet,
		"http://evil.example.com/steal-data", nil)
	_, err = c.Do(req)
	if err == nil {
		t.Fatal("expected ErrHostNotAllowed, got nil")
	}
	if !errors.Is(err, client.ErrHostNotAllowed) {
		t.Errorf("err = %v, want errors.Is(err, ErrHostNotAllowed)", err)
	}
}

func TestWhitelist_CustomAllowedHostAccepted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "auth-token")
	if err := os.WriteFile(tokenPath, []byte("tok"), 0600); err != nil {
		t.Fatalf("write token: %v", err)
	}

	srvHost := strings.TrimPrefix(srv.URL, "http://")
	cfg := client.Config{
		BaseURL:       srv.URL,
		AuthTokenPath: tokenPath,
		AllowedHosts:  []string{srvHost},
	}
	c, err := client.New(cfg)
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL+"/v1/test", nil)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("custom allowed host rejected: %v", err)
	}
	defer resp.Body.Close()
}

type retryTrackingHandler struct {
	calls     int
	failCount int
	failCode  int
}

func (h *retryTrackingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.calls++
	if h.calls <= h.failCount {
		w.WriteHeader(h.failCode)
		_, _ = w.Write([]byte(`{"error":"server error"}`))
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"ok":true}`))
}

func TestRetry_SucceedsOnFirstAttempt(t *testing.T) {
	h := &retryTrackingHandler{failCount: 0}
	srv := httptest.NewServer(h)
	defer srv.Close()

	c := newTestClient(t, srv)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet,
		srv.URL+"/v1/test", nil)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if h.calls != 1 {
		t.Errorf("calls = %d, want 1", h.calls)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestRetry_RetriesOn503ThenSucceeds(t *testing.T) {
	h := &retryTrackingHandler{failCount: 2, failCode: http.StatusServiceUnavailable}
	srv := httptest.NewServer(h)
	defer srv.Close()

	c := newTestClient(t, srv)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet,
		srv.URL+"/v1/test", nil)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()

	if h.calls != 3 {
		t.Errorf("calls = %d, want 3", h.calls)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestRetry_ExhaustedAfterThreeAttempts(t *testing.T) {
	h := &retryTrackingHandler{failCount: 10, failCode: http.StatusInternalServerError}
	srv := httptest.NewServer(h)
	defer srv.Close()

	c := newTestClient(t, srv)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet,
		srv.URL+"/v1/test", nil)
	_, err := c.Do(req)
	if err == nil {
		t.Fatal("expected error after retry exhaustion, got nil")
	}

	if h.calls != 3 {
		t.Errorf("calls = %d, want 3", h.calls)
	}
}

func TestRetry_ContextCancelledDuringBackoff(t *testing.T) {
	h := &retryTrackingHandler{failCount: 10, failCode: http.StatusBadGateway}
	srv := httptest.NewServer(h)
	defer srv.Close()

	c := newTestClient(t, srv)
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/v1/test", nil)

	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	_, err := c.Do(req)
	if err == nil {
		t.Fatal("expected error after context cancel, got nil")
	}
	if !errors.Is(err, context.Canceled) {

		if !strings.Contains(err.Error(), "context canceled") &&
			!strings.Contains(err.Error(), "context deadline exceeded") {
			t.Errorf("err = %v, want context cancellation error", err)
		}
	}

	if h.calls > 2 {
		t.Errorf("calls = %d, expected ≤2 given early cancel", h.calls)
	}
}

func TestRetry_4xxNotRetried(t *testing.T) {
	h := &retryTrackingHandler{failCount: 10, failCode: http.StatusUnauthorized}
	srv := httptest.NewServer(h)
	defer srv.Close()

	c := newTestClient(t, srv)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet,
		srv.URL+"/v1/test", nil)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()

	if h.calls != 1 {
		t.Errorf("calls = %d, want 1 (4xx must not be retried)", h.calls)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}
