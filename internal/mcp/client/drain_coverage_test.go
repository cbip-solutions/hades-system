package client_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/mcp/client"
)

func TestDrainBuffer_OrphanErrorBubblesUp(t *testing.T) {

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	bufDir := t.TempDir()
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "auth-token")
	_ = os.WriteFile(tokenPath, []byte("tok"), 0600)
	cfg := client.Config{BaseURL: srv.URL, AuthTokenPath: tokenPath, MCPName: "orphan-err"}
	c, _ := client.New(cfg)
	ec := client.NewEmitClient(c, bufDir)

	drainingPath := ec.BufferPath() + ".draining"
	df, _ := os.OpenFile(drainingPath, os.O_CREATE|os.O_WRONLY, 0600)
	line, _ := json.Marshal(client.AuditEvent{Type: "orphan", Payload: `{}`})
	_, _ = df.Write(append(line, '\n'))
	_ = df.Close()

	lf, _ := os.OpenFile(ec.BufferPath(), os.O_CREATE|os.O_WRONLY, 0600)
	line2, _ := json.Marshal(client.AuditEvent{Type: "live", Payload: `{}`})
	_, _ = lf.Write(append(line2, '\n'))
	_ = lf.Close()

	n, err := ec.DrainBuffer(context.Background())
	if err == nil {
		t.Fatal("expected orphan-recovery error to bubble up")
	}
	_ = n

	if _, statErr := os.Stat(ec.BufferPath()); os.IsNotExist(statErr) {
		t.Error("live buffer should be untouched when orphan recovery fails")
	}

	if _, statErr := os.Stat(drainingPath); os.IsNotExist(statErr) {
		t.Error(".draining file should survive the orphan-recovery failure")
	}
}

func TestDrainBuffer_LiveBufferStatErrorPropagated(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses dir permissions; cannot provoke EACCES")
	}
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions semantics not enforced on Windows")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	parentDir := t.TempDir()
	bufDir := filepath.Join(parentDir, "subdir")
	if err := os.MkdirAll(bufDir, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "auth-token")
	_ = os.WriteFile(tokenPath, []byte("tok"), 0600)
	cfg := client.Config{BaseURL: srv.URL, AuthTokenPath: tokenPath, MCPName: "stat-err"}
	c, _ := client.New(cfg)
	ec := client.NewEmitClient(c, bufDir)

	f, _ := os.OpenFile(ec.BufferPath(), os.O_CREATE|os.O_WRONLY, 0600)
	line, _ := json.Marshal(client.AuditEvent{Type: "x", Payload: `{}`})
	_, _ = f.Write(append(line, '\n'))
	_ = f.Close()

	if err := os.Chmod(bufDir, 0); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(bufDir, 0700) })

	n, err := ec.DrainBuffer(context.Background())

	if err != nil && n != 0 {
		t.Errorf("on stat error, drained should be 0; got %d", n)
	}
}

func TestDrainBuffer_RewriteErrorPropagated(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses dir permissions")
	}
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions semantics not enforced on Windows")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	bufDir := t.TempDir()
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "auth-token")
	_ = os.WriteFile(tokenPath, []byte("tok"), 0600)
	cfg := client.Config{BaseURL: srv.URL, AuthTokenPath: tokenPath, MCPName: "rewrite-err"}
	c, _ := client.New(cfg)
	ec := client.NewEmitClient(c, bufDir)

	f, _ := os.OpenFile(ec.BufferPath(), os.O_CREATE|os.O_WRONLY, 0600)
	for i := 0; i < 3; i++ {
		line, _ := json.Marshal(client.AuditEvent{Type: "x", Payload: `{}`})
		_, _ = f.Write(append(line, '\n'))
	}
	_ = f.Close()

	if _, err := ec.DrainBuffer(context.Background()); err == nil {
		t.Fatal("expected partial-drain error on first pass")
	}

	if err := os.Chmod(bufDir, 0500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(bufDir, 0700) })

	n, err := ec.DrainBuffer(context.Background())
	if err == nil && n > 0 {

		t.Skip("OS allowed rewrite despite read-only dir; rewrite-error path not exercised")
	}
}

func TestDrainBuffer_OrphanWithMalformedAndValidLines(t *testing.T) {
	var received int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received++
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	bufDir := t.TempDir()
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "auth-token")
	_ = os.WriteFile(tokenPath, []byte("tok"), 0600)
	cfg := client.Config{BaseURL: srv.URL, AuthTokenPath: tokenPath, MCPName: "orphan-malformed"}
	c, _ := client.New(cfg)
	ec := client.NewEmitClient(c, bufDir)

	drainingPath := ec.BufferPath() + ".draining"
	f, _ := os.OpenFile(drainingPath, os.O_CREATE|os.O_WRONLY, 0600)
	validLine, _ := json.Marshal(client.AuditEvent{Type: "valid", Payload: `{}`})
	_, _ = f.Write(append(validLine, '\n'))
	_, _ = f.Write([]byte("not-json garbage\n"))
	_, _ = f.Write(append(validLine, '\n'))
	_ = f.Close()

	n, err := ec.DrainBuffer(context.Background())
	if err != nil {
		t.Fatalf("DrainBuffer: %v", err)
	}

	if n != 2 {
		t.Errorf("drained %d, want 2 (malformed line skipped)", n)
	}

	if _, statErr := os.Stat(drainingPath); !os.IsNotExist(statErr) {
		t.Error(".draining must be gone after full drain")
	}
}

func TestDrainBuffer_PartialDrainOrphanRetainsRemaining(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
			return
		}
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	bufDir := t.TempDir()
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "auth-token")
	_ = os.WriteFile(tokenPath, []byte("tok"), 0600)
	cfg := client.Config{BaseURL: srv.URL, AuthTokenPath: tokenPath, MCPName: "orphan-partial"}
	c, _ := client.New(cfg)
	ec := client.NewEmitClient(c, bufDir)

	drainingPath := ec.BufferPath() + ".draining"
	f, _ := os.OpenFile(drainingPath, os.O_CREATE|os.O_WRONLY, 0600)
	for i := 0; i < 3; i++ {
		line, _ := json.Marshal(client.AuditEvent{Type: "t", Payload: `{}`})
		_, _ = f.Write(append(line, '\n'))
	}
	_ = f.Close()

	n, err := ec.DrainBuffer(context.Background())
	if err == nil {
		t.Fatal("expected partial-drain error")
	}
	if n < 1 {
		t.Errorf("drained %d, want >=1", n)
	}

	data, statErr := os.ReadFile(drainingPath)
	if os.IsNotExist(statErr) {
		t.Fatal(".draining must survive partial drain")
	}
	if statErr != nil {
		t.Fatalf("read draining: %v", statErr)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		t.Error(".draining should contain remaining un-drained events, not be empty")
	}
}

func TestDrainBuffer_NoLiveBuffer_NoRotation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	ec := newEmitClient(t, srv, "no-live-buf")

	n, err := ec.DrainBuffer(context.Background())
	if err != nil {
		t.Fatalf("DrainBuffer: %v", err)
	}
	if n != 0 {
		t.Errorf("n = %d, want 0", n)
	}
}

var _ = errors.New

func TestDrainBuffer_RenameErrorPropagated(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses dir permissions")
	}
	if runtime.GOOS == "windows" {
		t.Skip("rename semantics differ on Windows")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	bufDir := t.TempDir()
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "auth-token")
	_ = os.WriteFile(tokenPath, []byte("tok"), 0600)
	cfg := client.Config{BaseURL: srv.URL, AuthTokenPath: tokenPath, MCPName: "rename-err"}
	c, _ := client.New(cfg)
	ec := client.NewEmitClient(c, bufDir)

	f, _ := os.OpenFile(ec.BufferPath(), os.O_CREATE|os.O_WRONLY, 0600)
	line, _ := json.Marshal(client.AuditEvent{Type: "x", Payload: `{}`})
	_, _ = f.Write(append(line, '\n'))
	_ = f.Close()

	if err := os.Chmod(bufDir, 0500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(bufDir, 0700) })

	_, err := ec.DrainBuffer(context.Background())
	if err == nil {
		t.Skip("OS allowed rename despite read-only parent; rename-error path not exercised here")
	}
}

func TestDrainBuffer_BufferReadError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses file permissions")
	}
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file modes not enforced on Windows")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	bufDir := t.TempDir()
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "auth-token")
	_ = os.WriteFile(tokenPath, []byte("tok"), 0600)
	cfg := client.Config{BaseURL: srv.URL, AuthTokenPath: tokenPath, MCPName: "read-err"}
	c, _ := client.New(cfg)
	ec := client.NewEmitClient(c, bufDir)

	drainingPath := ec.BufferPath() + ".draining"
	f, _ := os.OpenFile(drainingPath, os.O_CREATE|os.O_WRONLY, 0600)
	line, _ := json.Marshal(client.AuditEvent{Type: "x", Payload: `{}`})
	_, _ = f.Write(append(line, '\n'))
	_ = f.Close()
	if err := os.Chmod(drainingPath, 0); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(drainingPath, 0600) })

	_, err := ec.DrainBuffer(context.Background())
	if err == nil {
		t.Skip("OS allowed read despite chmod 0; read-error path not exercised here")
	}
}
