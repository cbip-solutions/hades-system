package client_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/mcp/client"
)

func TestBufferPath_NoRaceConcurrentReaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	ec := newEmitClient(t, srv, "race-bufpath")

	const n = 100
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_ = ec.BufferPath()
		}()
	}
	wg.Wait()
}

func TestBufferPath_NoRaceConcurrentEmitAndRead(t *testing.T) {

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	ec := newEmitClient(t, srv, "race-emit-read")

	const n = 100
	var wg sync.WaitGroup
	wg.Add(n * 2)

	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_ = ec.BufferPath()
		}()
	}

	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_ = ec.Emit(context.Background(), client.AuditEvent{
				Type:    "race.test",
				Payload: `{}`,
			})
		}()
	}
	wg.Wait()
}

func TestBufferPath_StableValue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	ec := newEmitClient(t, srv, "stable-path")

	expected := ec.BufferPath()

	const n = 100
	var wg sync.WaitGroup
	var mismatches atomic.Int32
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			if got := ec.BufferPath(); got != expected {
				mismatches.Add(1)
			}
		}()
	}
	wg.Wait()
	if mismatches.Load() != 0 {
		t.Errorf("BufferPath() returned different values across %d goroutines (%d mismatches)",
			n, mismatches.Load())
	}
}

func TestEmit_ConcurrentSafe(t *testing.T) {
	var received atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	ec := newEmitClient(t, srv, "concurrent-emit")

	const n = 200
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_ = ec.Emit(context.Background(), client.AuditEvent{
				Type:    "concurrent.event",
				Payload: `{}`,
			})
		}()
	}
	wg.Wait()
	if int(received.Load()) != n {
		t.Errorf("daemon received %d events, want %d", received.Load(), n)
	}
}

func writeBufferFixture(t *testing.T, path string, events []client.AuditEvent) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatalf("open buffer fixture: %v", err)
	}
	defer f.Close()
	for _, evt := range events {
		line, _ := json.Marshal(evt)
		if _, err := f.Write(append(line, '\n')); err != nil {
			t.Fatalf("write fixture line: %v", err)
		}
	}
}

func newEmitClientFixed(t *testing.T, srv *httptest.Server, mcpName, bufDir string) *client.EmitClient {
	t.Helper()
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "auth-token")
	if err := os.WriteFile(tokenPath, []byte("fixed-token"), 0600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	cfg := client.Config{
		BaseURL:       srv.URL,
		AuthTokenPath: tokenPath,
		MCPName:       mcpName,
	}
	c, err := client.New(cfg)
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}
	return client.NewEmitClient(c, bufDir)
}
