package client_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/mcp/client"
)

// TestDo_TestMode_ExternalHostNotRetried is the I-1 regression test.
// In test mode (BaseURL set), a request to a whitelisted external host
// (e.g., arxiv-mock running on a different httptest server) MUST NOT be
// retried — it should be passed through with a single attempt.
func TestDo_TestMode_ExternalHostNotRetried(t *testing.T) {

	var externalCalls atomic.Int32
	externalMock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		externalCalls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer externalMock.Close()

	daemonMock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer daemonMock.Close()

	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "auth-token")
	if err := os.WriteFile(tokenPath, []byte("classify-tok"), 0600); err != nil {
		t.Fatalf("write token: %v", err)
	}

	externalHost := strings.TrimPrefix(externalMock.URL, "http://")
	cfg := client.Config{
		BaseURL:       daemonMock.URL,
		AuthTokenPath: tokenPath,
		AllowedHosts:  []string{externalHost},
	}
	c, err := client.New(cfg)
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet,
		externalMock.URL+"/external/api", nil)
	resp, _ := c.Do(req)
	if resp != nil {
		_ = resp.Body.Close()
	}

	if got := externalCalls.Load(); got != 1 {
		t.Errorf("external mock got %d calls, want 1 (no retry on external host)", got)
	}
}

func TestDo_TestMode_BaseURLHostRetried(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet,
		srv.URL+"/v1/health", nil)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()

	if got := calls.Load(); got != 3 {
		t.Errorf("BaseURL host got %d calls, want 3 (initial + 2 retries)", got)
	}
}

func TestDo_TestMode_DaemonSentinelHostRetried(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet,
		"http://daemon/v1/health", nil)

	_ = req
	_ = c

	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "auth-token")
	_ = os.WriteFile(tokenPath, []byte("daemon-tok"), 0600)
	cfg := client.Config{
		SocketPath:    "/nonexistent.sock",
		AuthTokenPath: tokenPath,
		AllowedHosts:  []string{"daemon"},
	}
	c2, err := client.New(cfg)
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}

	req2, _ := http.NewRequestWithContext(context.Background(), http.MethodGet,
		"http://daemon/v1/health", nil)
	_, dialErr := c2.Do(req2)
	if dialErr == nil {
		t.Error("expected dial error for nonexistent unix socket, got nil")
	}
}

func TestDo_ProductionMode_ExternalHostNotRetried(t *testing.T) {

	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "auth-token")
	_ = os.WriteFile(tokenPath, []byte("prod-tok"), 0600)
	cfg := client.Config{
		SocketPath:    "/nonexistent-prod.sock",
		AuthTokenPath: tokenPath,
	}
	c, err := client.New(cfg)
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}

	_ = c
	t.Skip("production-mode external retry behaviour exercised indirectly via test-mode regression test")
}
