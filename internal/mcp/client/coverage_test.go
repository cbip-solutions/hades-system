package client_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/mcp/client"
)

func TestNew_DefaultSocketPath(t *testing.T) {

	cfg := client.Config{

		AuthTokenPath: "/nonexistent-coverage-token",
	}
	_, err := client.New(cfg)
	if err == nil {
		t.Fatal("expected error (missing token file), got nil")
	}
}

func TestNew_ExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("UserHomeDir not available")
	}

	dir := t.TempDir()
	realPath := filepath.Join(dir, "auth-token")
	if err := os.WriteFile(realPath, []byte("expand-home-token"), 0600); err != nil {
		t.Fatalf("write token: %v", err)
	}

	rel, err2 := filepath.Rel(home, realPath)
	if err2 != nil || len(rel) == 0 {
		t.Skip("cannot construct relative path from home")
	}
	tildeRelPath := "~/" + rel

	cfg := client.Config{
		BaseURL:       "http://127.0.0.1:9999",
		AuthTokenPath: tildeRelPath,
	}

	c, err3 := client.New(cfg)
	if err3 != nil {
		t.Fatalf("client.New with ~/ path: %v", err3)
	}
	_ = c
}

func TestBaseURL_ProductionPath(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "auth-token")
	if err := os.WriteFile(tokenPath, []byte("production-token"), 0600); err != nil {
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

	u := c.BaseURL()
	if u != "http://daemon" {
		t.Errorf("BaseURL() = %q, want 'http://daemon'", u)
	}
}

func TestDoWithRetry_GetBodyError(t *testing.T) {

	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)

	payload := []byte(`{"key":"value"}`)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		srv.URL+"/v1/test", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	getBodyErr := errors.New("deliberate GetBody failure for coverage")
	req.GetBody = func() (io.ReadCloser, error) {
		return nil, getBodyErr
	}

	_, err = c.Do(req)
	if err == nil {
		t.Fatal("expected error from GetBody failure, got nil")
	}

	if !errors.Is(err, getBodyErr) {
		t.Errorf("err = %v, want wrapping getBodyErr", err)
	}
}

func TestBuildTransport_UnixSocket(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "auth-token")
	if err := os.WriteFile(tokenPath, []byte("unix-token"), 0600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	cfg := client.Config{
		SocketPath:    filepath.Join(dir, "nonexistent.sock"),
		AuthTokenPath: tokenPath,
	}
	c, err := client.New(cfg)
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet,
		"http://arxiv.org/abs/test", nil)
	_, reqErr := c.Do(req)

	if reqErr == nil {
		t.Fatal("expected dial error, got nil")
	}
	if errors.Is(reqErr, client.ErrHostNotAllowed) {
		t.Errorf("got ErrHostNotAllowed — expected dial error from unix socket transport")
	}
}

func TestParseHostFromURL_EdgeCases(t *testing.T) {

	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "auth-token")
	if err := os.WriteFile(tokenPath, []byte("token-edge"), 0600); err != nil {
		t.Fatalf("write token: %v", err)
	}

	cfg := client.Config{
		BaseURL:       "http://127.0.0.1:19999",
		AuthTokenPath: tokenPath,
	}
	c, err := client.New(cfg)
	if err != nil {
		t.Fatalf("client.New with no-path BaseURL: %v", err)
	}
	if c.BaseURL() != "http://127.0.0.1:19999" {
		t.Errorf("BaseURL = %q, want 'http://127.0.0.1:19999'", c.BaseURL())
	}
}

func TestParseHostFromURL_EmptyHost(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "auth-token")
	if err := os.WriteFile(tokenPath, []byte("empty-host-tok"), 0600); err != nil {
		t.Fatalf("write token: %v", err)
	}

	cfg := client.Config{
		BaseURL:       "http://",
		AuthTokenPath: tokenPath,
	}

	c, err := client.New(cfg)
	if err != nil {
		t.Fatalf("client.New with empty-host BaseURL: %v", err)
	}
	_ = c
}

func TestNew_DefaultAuthTokenPath(t *testing.T) {

	cfg := client.Config{
		BaseURL:       "http://127.0.0.1:19999",
		AuthTokenPath: "",
	}
	_, err := client.New(cfg)

	if err == nil {
		t.Skip("auth token file exists at default path; can't test missing-file path")
	}

}

func TestCacheGet_NonOKNon404Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"internal"}`))
	}))
	defer srv.Close()

	cc := newCacheClient(t, srv)
	_, err := cc.Get(context.Background(), "test query")
	if err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
}

func TestCacheGet_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not valid json`))
	}))
	defer srv.Close()

	cc := newCacheClient(t, srv)
	_, err := cc.Get(context.Background(), "test")
	if err == nil {
		t.Fatal("expected JSON decode error, got nil")
	}
}

func TestCacheSet_NonCreatedError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad"}`))
	}))
	defer srv.Close()

	cc := newCacheClient(t, srv)
	err := cc.Set(context.Background(), client.CacheSetRequest{
		Query:    "test",
		Response: `{"data":"value"}`,
		TTL:      time.Hour,
	})
	if err == nil {
		t.Fatal("expected error for 400 response, got nil")
	}
}

func TestCacheSet_CustomHash(t *testing.T) {
	srv := fakeResearchCacheServer(t)
	defer srv.Close()

	cc := newCacheClient(t, srv)
	const customHash = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	err := cc.Set(context.Background(), client.CacheSetRequest{
		Query:    "any query",
		Hash:     customHash,
		Response: `{"custom":"true"}`,
		TTL:      time.Hour,
	})
	if err != nil {
		t.Fatalf("Set with custom hash: %v", err)
	}

}

func TestNewEmitClient_EmptyBufDir(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "auth-token")
	if err := os.WriteFile(tokenPath, []byte("tok"), 0600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	cfg := client.Config{
		BaseURL:       srv.URL,
		AuthTokenPath: tokenPath,
		MCPName:       "test",
	}
	c, err := client.New(cfg)
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}

	ec := client.NewEmitClient(c, "")
	bufPath := ec.BufferPath()
	if bufPath == "" {
		t.Error("BufferPath() is empty")
	}

	if len(bufPath) < 4 || bufPath[:4] != "/tmp" {
		t.Errorf("BufferPath() = %q, want /tmp prefix", bufPath)
	}
}

func TestBufferPath_CachedAfterFirstCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	ec := newEmitClient(t, srv, "cache-test")
	p1 := ec.BufferPath()
	p2 := ec.BufferPath()
	if p1 != p2 {
		t.Errorf("BufferPath() not stable: %q != %q", p1, p2)
	}
}

func TestEmit_ContextCancelledBeforeEmit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	ec := newEmitClient(t, srv, "ctx-cancel")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	evt := client.AuditEvent{Type: "test.event", Payload: `{}`}

	if err := ec.Emit(ctx, evt); err != nil {
		t.Fatalf("Emit with cancelled ctx: %v", err)
	}
}

func TestDrainBuffer_EmptyLinesSkipped(t *testing.T) {
	var count int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	bufDir := t.TempDir()
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "auth-token")
	_ = os.WriteFile(tokenPath, []byte("tok"), 0600)
	cfg := client.Config{BaseURL: srv.URL, AuthTokenPath: tokenPath, MCPName: "emptylines"}
	c, _ := client.New(cfg)
	ec := client.NewEmitClient(c, bufDir)

	validLine, _ := json.Marshal(client.AuditEvent{Type: "t", Payload: `{}`})
	f, _ := os.OpenFile(ec.BufferPath(), os.O_CREATE|os.O_WRONLY, 0600)
	_, _ = f.WriteString("\n")
	_, _ = f.WriteString("   \n")
	_, _ = f.WriteString(string(validLine) + "\n")
	f.Close()

	n, err := ec.DrainBuffer(context.Background())
	if err != nil {
		t.Fatalf("DrainBuffer with empty lines: %v", err)
	}
	if n != 1 {
		t.Errorf("drained %d events, want 1 (empty lines skipped)", n)
	}
}

func TestDrainBuffer_NoFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	ec := newEmitClient(t, srv, "nofile")
	n, err := ec.DrainBuffer(context.Background())
	if err != nil {
		t.Fatalf("DrainBuffer with no file: %v", err)
	}
	if n != 0 {
		t.Errorf("n = %d, want 0", n)
	}
}

func TestDrainBuffer_MalformedLines(t *testing.T) {
	var count int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	bufDir := t.TempDir()
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "auth-token")
	_ = os.WriteFile(tokenPath, []byte("tok"), 0600)
	cfg := client.Config{BaseURL: srv.URL, AuthTokenPath: tokenPath, MCPName: "malformed"}
	c, _ := client.New(cfg)
	ec := client.NewEmitClient(c, bufDir)

	validLine, _ := json.Marshal(client.AuditEvent{Type: "valid", Payload: `{}`})
	f, _ := os.OpenFile(ec.BufferPath(), os.O_CREATE|os.O_WRONLY, 0600)
	_, _ = f.WriteString(string(validLine) + "\n")
	_, _ = f.WriteString("not json at all\n")
	_, _ = f.WriteString(string(validLine) + "\n")
	f.Close()

	n, err := ec.DrainBuffer(context.Background())
	if err != nil {
		t.Fatalf("DrainBuffer with malformed lines: %v", err)
	}

	if n != 2 {
		t.Errorf("drained %d, want 2", n)
	}
}

func TestBudgetCapStatus_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"internal"}`))
	}))
	defer srv.Close()

	bc := newBudgetClient(t, srv)
	_, err := bc.CapStatus(context.Background(), "project", "internal-platform-x")
	if err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
}

func TestBudgetCapStatus_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	bc := newBudgetClient(t, srv)
	_, err := bc.CapStatus(context.Background(), "project", "test")
	if err == nil {
		t.Fatal("expected JSON decode error, got nil")
	}
}

func TestBudgetAxes_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	bc := newBudgetClient(t, srv)
	_, err := bc.Axes(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for 404, got nil")
	}
}

func TestBudgetAxes_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	bc := newBudgetClient(t, srv)
	_, err := bc.Axes(context.Background(), "cost-id")
	if err == nil {
		t.Fatal("expected JSON decode error, got nil")
	}
}

func TestBudgetAnomalyCheck_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	bc := newBudgetClient(t, srv)
	_, err := bc.AnomalyCheck(context.Background(), "project", "1h")
	if err == nil {
		t.Fatal("expected error for 500, got nil")
	}
}

func TestBudgetAnomalyCheck_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	bc := newBudgetClient(t, srv)
	_, err := bc.AnomalyCheck(context.Background(), "project", "1h")
	if err == nil {
		t.Fatal("expected JSON decode error, got nil")
	}
}

func TestBudgetEvents_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	bc := newBudgetClient(t, srv)
	_, err := bc.Events(context.Background(), time.Now().Add(-1*time.Hour))
	if err == nil {
		t.Fatal("expected error for 500, got nil")
	}
}

func TestBudgetEvents_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	bc := newBudgetClient(t, srv)
	_, err := bc.Events(context.Background(), time.Time{})
	if err == nil {
		t.Fatal("expected JSON decode error, got nil")
	}
}

func TestCacheGet_TransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	cc := newCacheClient(t, srv)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := cc.Get(ctx, "transport error test")
	if err == nil {
		t.Fatal("expected transport error, got nil")
	}
}

func TestCacheSet_TransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	cc := newCacheClient(t, srv)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := cc.Set(ctx, client.CacheSetRequest{
		Query:    "transport error",
		Response: `{}`,
		TTL:      time.Hour,
	})
	if err == nil {
		t.Fatal("expected transport error from Set, got nil")
	}
}

func TestBudgetCapStatus_TransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	bc := newBudgetClient(t, srv)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := bc.CapStatus(ctx, "project", "test")
	if err == nil {
		t.Fatal("expected transport error from CapStatus, got nil")
	}
}

func TestBudgetRecord_TransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	bc := newBudgetClient(t, srv)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := bc.Record(ctx, client.RecordRequest{CostID: "c1", AmountUSD: 0.01})
	if err == nil {
		t.Fatal("expected transport error from Record, got nil")
	}
}

func TestBudgetAxes_TransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	bc := newBudgetClient(t, srv)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := bc.Axes(ctx, "cost-id")
	if err == nil {
		t.Fatal("expected transport error from Axes, got nil")
	}
}

func TestBudgetAnomalyCheck_TransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	bc := newBudgetClient(t, srv)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := bc.AnomalyCheck(ctx, "project", "1h")
	if err == nil {
		t.Fatal("expected transport error from AnomalyCheck, got nil")
	}
}

func TestBudgetEvents_TransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	bc := newBudgetClient(t, srv)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := bc.Events(ctx, time.Now().Add(-1*time.Hour))
	if err == nil {
		t.Fatal("expected transport error from Events, got nil")
	}
}

func TestEmit_4xxFallsBackToBuffer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer srv.Close()

	ec := newEmitClient(t, srv, "fourxx")
	evt := client.AuditEvent{Type: "test.4xx", Payload: `{}`}

	if err := ec.Emit(context.Background(), evt); err != nil {
		t.Fatalf("Emit should not return error on 4xx fallback: %v", err)
	}

	bufPath := ec.BufferPath()
	data, err := os.ReadFile(bufPath)
	if err != nil {
		t.Fatalf("buffer file not created after 4xx fallback: %v", err)
	}
	if !strings.Contains(string(data), "test.4xx") {
		t.Errorf("buffer does not contain event: %s", string(data))
	}
}

func TestEmit_FallbackBufferMkdirFail(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root can always mkdir; skip test")
	}

	parentDir := t.TempDir()
	fileBlock := filepath.Join(parentDir, "blockfile")
	if err := os.WriteFile(fileBlock, []byte("x"), 0444); err != nil {
		t.Fatalf("setup: %v", err)
	}

	bufDirInsideFile := filepath.Join(fileBlock, "subdir")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "auth-token")
	_ = os.WriteFile(tokenPath, []byte("tok"), 0600)
	cfg := client.Config{BaseURL: srv.URL, AuthTokenPath: tokenPath, MCPName: "mkdirfail"}
	c, err := client.New(cfg)
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}
	ec := client.NewEmitClient(c, bufDirInsideFile)
	evt := client.AuditEvent{Type: "t", Payload: `{}`}

	if err := ec.Emit(context.Background(), evt); err != nil {
		t.Fatalf("Emit should not return error even on buffer mkdir fail: %v", err)
	}
}

func TestEmit_FallbackBufferOpenFileFail(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses permissions; skip test")
	}

	bufDir := t.TempDir()
	if err := os.Chmod(bufDir, 0555); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(bufDir, 0755) })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "auth-token")
	_ = os.WriteFile(tokenPath, []byte("tok"), 0600)
	cfg := client.Config{BaseURL: srv.URL, AuthTokenPath: tokenPath, MCPName: "openfilefail"}
	c, err := client.New(cfg)
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}
	ec := client.NewEmitClient(c, bufDir)
	evt := client.AuditEvent{Type: "t", Payload: `{}`}

	if err := ec.Emit(context.Background(), evt); err != nil {
		t.Fatalf("Emit should not return error even on buffer open fail: %v", err)
	}
}

func TestEmit_NonZeroEmittedAt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	ec := newEmitClient(t, srv, "ts-test")
	fixedTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	evt := client.AuditEvent{
		Type:      "test.event",
		Payload:   `{}`,
		EmittedAt: fixedTime,
	}
	if err := ec.Emit(context.Background(), evt); err != nil {
		t.Fatalf("Emit: %v", err)
	}
}

func TestBufferPath_EmptyMCPName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "auth-token")
	_ = os.WriteFile(tokenPath, []byte("tok"), 0600)
	cfg := client.Config{
		BaseURL:       srv.URL,
		AuthTokenPath: tokenPath,
		MCPName:       "",
	}
	c, err := client.New(cfg)
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}
	ec := client.NewEmitClient(c, t.TempDir())
	path := ec.BufferPath()
	if path == "" {
		t.Error("BufferPath() should not be empty for empty MCPName")
	}
}

func TestDoWithRetry_TransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		hj, ok := w.(http.Hijacker)
		if !ok {
			w.WriteHeader(http.StatusOK)
			return
		}
		conn, _, _ := hj.Hijack()
		conn.Close()
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet,
		srv.URL+"/v1/test", nil)
	_, err := c.Do(req)

	if err == nil {
		t.Fatal("expected error from transport failure, got nil")
	}
}

func TestEmitDirect_BuildError(t *testing.T) {

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	bufDir := t.TempDir()
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "auth-token")
	_ = os.WriteFile(tokenPath, []byte("tok"), 0600)
	cfg := client.Config{BaseURL: srv.URL, AuthTokenPath: tokenPath, MCPName: "direct-err"}
	c, _ := client.New(cfg)
	ec := client.NewEmitClient(c, bufDir)

	validLine, _ := json.Marshal(client.AuditEvent{Type: "t", Payload: `{}`})
	f, _ := os.OpenFile(ec.BufferPath(), os.O_CREATE|os.O_WRONLY, 0600)
	_, _ = f.WriteString(string(validLine) + "\n")
	f.Close()

	n, err := ec.DrainBuffer(context.Background())

	if err == nil {
		t.Fatal("expected error from DrainBuffer when daemon returns 400")
	}
	if n != 0 {
		t.Errorf("n = %d, want 0 (no events drained on 400)", n)
	}
}
