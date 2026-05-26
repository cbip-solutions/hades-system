package client_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/mcp/client"
)

func newEmitClient(t *testing.T, srv *httptest.Server, mcpName string) *client.EmitClient {
	t.Helper()
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "auth-token")
	if err := os.WriteFile(tokenPath, []byte("emit-test-token"), 0600); err != nil {
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

	bufDir := t.TempDir()
	return client.NewEmitClient(c, bufDir)
}

func TestEmit_SuccessNoBuf(t *testing.T) {
	var received []client.AuditEvent
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audit/emit" {
			http.NotFound(w, r)
			return
		}
		var evt client.AuditEvent
		if err := json.NewDecoder(r.Body).Decode(&evt); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		received = append(received, evt)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	ec := newEmitClient(t, srv, "audit")
	evt := client.AuditEvent{
		ProjectID: "internal-platform-x",
		Type:      "ssh_exec.completed",
		Payload:   `{"cmd":"alembic upgrade head","exit_code":0}`,
	}
	if err := ec.Emit(context.Background(), evt); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if len(received) != 1 {
		t.Fatalf("daemon received %d events, want 1", len(received))
	}
	if received[0].Type != evt.Type {
		t.Errorf("Type = %q, want %q", received[0].Type, evt.Type)
	}

	bufPath := ec.BufferPath()
	if _, err := os.Stat(bufPath); !os.IsNotExist(err) {
		t.Errorf("buffer file should not exist on success: %v", err)
	}
}

func TestEmit_DaemonDownWritesBuffer(t *testing.T) {

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	ec := newEmitClient(t, srv, "sshexec")
	evt := client.AuditEvent{
		ProjectID: "test-proj",
		Type:      "ssh_exec.interactive_blocked",
		Payload:   `{"cmd":"sudo su","reason":"interactive prompt detected"}`,
	}

	if err := ec.Emit(context.Background(), evt); err != nil {
		t.Fatalf("Emit should not return error when buffer fallback succeeds: %v", err)
	}

	bufPath := ec.BufferPath()
	data, err := os.ReadFile(bufPath)
	if err != nil {
		t.Fatalf("buffer file not created: %v", err)
	}
	if !strings.Contains(string(data), "ssh_exec.interactive_blocked") {
		t.Errorf("buffer file does not contain event: %s", string(data))
	}

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var decoded client.AuditEvent
		if err := json.Unmarshal([]byte(line), &decoded); err != nil {
			t.Errorf("buffer line is not valid JSON: %q", line)
		}
	}
}

func TestEmit_MultipleEventsBuffered(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	ec := newEmitClient(t, srv, "research")
	for i := 0; i < 5; i++ {
		evt := client.AuditEvent{
			ProjectID: "proj",
			Type:      "research.dispatch.started",
			Payload:   `{"iteration":` + string(rune('0'+i)) + `}`,
		}
		if err := ec.Emit(context.Background(), evt); err != nil {
			t.Fatalf("Emit %d: %v", i, err)
		}
	}

	data, err := os.ReadFile(ec.BufferPath())
	if err != nil {
		t.Fatalf("buffer file not created: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 5 {
		t.Errorf("buffer has %d lines, want 5", len(lines))
	}
}

func TestDrainBuffer_DrainsOnRecovery(t *testing.T) {
	var drainCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/audit/emit" {
			drainCount.Add(1)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	bufDir := t.TempDir()
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "auth-token")
	if err := os.WriteFile(tokenPath, []byte("drain-token"), 0600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	cfg := client.Config{
		BaseURL:       srv.URL,
		AuthTokenPath: tokenPath,
		MCPName:       "audit",
	}
	c, err := client.New(cfg)
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}
	ec := client.NewEmitClient(c, bufDir)

	events := []client.AuditEvent{
		{ProjectID: "p1", Type: "t1", Payload: `{}`},
		{ProjectID: "p2", Type: "t2", Payload: `{}`},
		{ProjectID: "p3", Type: "t3", Payload: `{}`},
	}
	for _, evt := range events {
		line, _ := json.Marshal(evt)
		f, _ := os.OpenFile(ec.BufferPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		_, _ = f.WriteString(string(line) + "\n")
		f.Close()
	}

	n, err := ec.DrainBuffer(context.Background())
	if err != nil {
		t.Fatalf("DrainBuffer: %v", err)
	}
	if n != 3 {
		t.Errorf("drained %d events, want 3", n)
	}
	if drainCount.Load() != 3 {
		t.Errorf("daemon received %d events, want 3", drainCount.Load())
	}

	if _, err := os.Stat(ec.BufferPath()); !os.IsNotExist(err) {
		t.Errorf("buffer file should be removed after drain, got: %v", err)
	}
}

func TestDrainBuffer_PartialDrainOnFailure(t *testing.T) {

	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := callCount.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	bufDir := t.TempDir()
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "auth-token")
	_ = os.WriteFile(tokenPath, []byte("drain-tok"), 0600)
	cfg := client.Config{
		BaseURL:       srv.URL,
		AuthTokenPath: tokenPath,
		MCPName:       "budget",
	}
	c, _ := client.New(cfg)
	ec := client.NewEmitClient(c, bufDir)

	for i := 0; i < 3; i++ {
		line, _ := json.Marshal(client.AuditEvent{Type: "t", Payload: `{}`})
		f, _ := os.OpenFile(ec.BufferPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		_, _ = f.WriteString(string(line) + "\n")
		f.Close()
	}

	n, err := ec.DrainBuffer(context.Background())

	if err == nil {
		t.Fatal("expected error on partial drain failure")
	}

	if n < 1 {
		t.Errorf("drained %d, want >=1", n)
	}

	drainingPath := ec.BufferPath() + ".draining"
	if _, statErr := os.Stat(drainingPath); os.IsNotExist(statErr) {
		t.Error(".draining snapshot must survive partial drain failure (review I-3)")
	}
	_ = time.Now()
}
