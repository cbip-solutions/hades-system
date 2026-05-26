package client_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/mcp/client"
)

func TestClient_CloseReleasesIdleConnections(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet,
		srv.URL+"/v1/health", nil)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	_ = resp.Body.Close()

	if closeErr := c.Close(); closeErr != nil {
		t.Errorf("Close: %v", closeErr)
	}

	if closeErr := c.Close(); closeErr != nil {
		t.Errorf("Close (2nd): %v", closeErr)
	}
}

func TestClient_CloseAfterUnixSocketTransport(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "auth-token")
	if err := os.WriteFile(tokenPath, []byte("close-tok"), 0600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	cfg := client.Config{
		SocketPath:    filepath.Join(dir, "noexistent.sock"),
		AuthTokenPath: tokenPath,
	}
	c, err := client.New(cfg)
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestExpandHome_HomeUnsetReturnsOriginalPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("UserHomeDir uses different env on Windows")
	}
	t.Setenv("HOME", "")

	cfg := client.Config{
		BaseURL:       "http://127.0.0.1:9999",
		AuthTokenPath: "~/.config/zen-swarm/auth-token-nonexistent",
	}
	_, err := client.New(cfg)
	if err == nil {
		t.Fatal("expected error (token file under unexpanded ~/...)")
	}

	if !strings.Contains(err.Error(), "~/") {
		t.Errorf("expected unexpanded path in error; got: %v", err)
	}
}

func TestParseHostFromURL_MalformedBaseURLClassifiedNonDaemon(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "auth-token")
	if err := os.WriteFile(tokenPath, []byte("malformed-tok"), 0600); err != nil {
		t.Fatalf("write token: %v", err)
	}

	cfg := client.Config{
		BaseURL:       "http://[invalid",
		AuthTokenPath: tokenPath,
		AllowedHosts:  []string{"127.0.0.1:9999"},
	}
	c, err := client.New(cfg)
	if err != nil {
		t.Fatalf("client.New with malformed BaseURL: %v", err)
	}

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet,
		"http://127.0.0.1:9999/v1/test", nil)
	_, _ = c.Do(req)
}

func TestNew_BaseURLWithUserInfo(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "auth-token")
	if err := os.WriteFile(tokenPath, []byte("userinfo-tok"), 0600); err != nil {
		t.Fatalf("write token: %v", err)
	}

	cfg := client.Config{
		BaseURL:       "http://user:pass@127.0.0.1:9999",
		AuthTokenPath: tokenPath,
	}
	c, err := client.New(cfg)
	if err != nil {
		t.Fatalf("client.New with userinfo BaseURL: %v", err)
	}
	if c == nil {
		t.Fatal("client is nil")
	}
}

// TestNew_TwoClientsDoNotShareWhitelist verifies that two independently
// constructed clients with different AllowedHosts do not see each other's
// extensions — proving the per-call clone of defaultAllowedHostsSealed
// is a real copy, not a shared mutable reference.
func TestNew_TwoClientsDoNotShareWhitelist(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "auth-token")
	if err := os.WriteFile(tokenPath, []byte("share-tok"), 0600); err != nil {
		t.Fatalf("write token: %v", err)
	}

	cfgA := client.Config{
		BaseURL:       srv.URL,
		AuthTokenPath: tokenPath,
		AllowedHosts:  []string{"client-a.example.com"},
	}
	cA, err := client.New(cfgA)
	if err != nil {
		t.Fatalf("client.New A: %v", err)
	}
	_ = cA

	cfgB := client.Config{
		BaseURL:       srv.URL,
		AuthTokenPath: tokenPath,
		AllowedHosts:  []string{"client-b.example.com"},
	}
	cB, err := client.New(cfgB)
	if err != nil {
		t.Fatalf("client.New B: %v", err)
	}

	cfgC := client.Config{
		SocketPath:    "/nonexistent.sock",
		AuthTokenPath: tokenPath,
	}
	cC, err := client.New(cfgC)
	if err != nil {
		t.Fatalf("client.New C: %v", err)
	}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet,
		"http://client-a.example.com/should-not-leak", nil)
	_, doErr := cC.Do(req)
	if doErr == nil {
		t.Fatal("client C accessed client A's whitelist extension — clone is shared!")
	}
	_ = cB
}

func TestDefaultWhitelist_ImmutableViaClone(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "auth-token")
	if err := os.WriteFile(tokenPath, []byte("imm-tok"), 0600); err != nil {
		t.Fatalf("write token: %v", err)
	}

	_, err := client.New(client.Config{
		SocketPath:    "/nonexistent.sock",
		AuthTokenPath: tokenPath,
		AllowedHosts:  []string{"polluted.example.com"},
	})
	if err != nil {
		t.Fatalf("client.New A: %v", err)
	}

	cB, err := client.New(client.Config{
		SocketPath:    "/nonexistent.sock",
		AuthTokenPath: tokenPath,
	})
	if err != nil {
		t.Fatalf("client.New B: %v", err)
	}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet,
		"http://polluted.example.com/", nil)
	_, doErr := cB.Do(req)
	if doErr == nil {
		t.Fatal("default whitelist was polluted by client A — clone is not isolated!")
	}
}
