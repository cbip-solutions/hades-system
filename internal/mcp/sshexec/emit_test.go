package sshexec

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
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

type fakeDaemon struct {
	server   *httptest.Server
	down     atomic.Bool
	received atomic.Int64
	bodies   chan []byte
}

func newFakeDaemon() *fakeDaemon {
	fd := &fakeDaemon{bodies: make(chan []byte, 32)}
	fd.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fd.down.Load() {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		fd.received.Add(1)
		select {
		case fd.bodies <- body:
		default:
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	return fd
}

func (f *fakeDaemon) URL() string  { return f.server.URL }
func (f *fakeDaemon) Close() error { f.server.Close(); return nil }

func newTestEmitter(t *testing.T, daemonURL, bufDir string) *Emitter {
	t.Helper()
	tokFile := filepath.Join(t.TempDir(), "tok")
	if err := os.WriteFile(tokFile, []byte("test-token"), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	c, err := client.New(client.Config{
		BaseURL:       daemonURL,
		AuthTokenPath: tokFile,
		MCPName:       "ssh-exec",
	})
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}
	ec := client.NewEmitClient(c, bufDir)
	return NewEmitter(ec, "internal-platform-x")
}

func TestEmitStartedHappyPath(t *testing.T) {
	fd := newFakeDaemon()
	defer fd.Close()
	em := newTestEmitter(t, fd.URL(), t.TempDir())
	req := ExecRequest{Host: "h", Command: "alembic upgrade", Project: "p"}
	if err := em.EmitStarted(req); err != nil {
		t.Fatalf("EmitStarted: %v", err)
	}
	body := <-fd.bodies
	if !strings.Contains(string(body), "ssh_exec.started") {
		t.Errorf("body = %q, want type ssh_exec.started", body)
	}
}

func TestEmitFallsThroughToBuffer(t *testing.T) {
	fd := newFakeDaemon()
	fd.down.Store(true)
	defer fd.Close()
	bufDir := t.TempDir()
	em := newTestEmitter(t, fd.URL(), bufDir)
	req := ExecRequest{Host: "h", Command: "alembic upgrade", Project: "p"}
	if err := em.EmitDenied(req, "test"); err != nil {
		t.Fatalf("EmitDenied: %v (buffer path should swallow daemon failure)", err)
	}

	matches, _ := filepath.Glob(filepath.Join(bufDir, "zen-mcp-ssh-exec-emit-buffer-*.jsonl"))
	if len(matches) != 1 {
		t.Fatalf("buffer files = %v, want 1", matches)
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("read buffer: %v", err)
	}
	if !strings.Contains(string(data), "ssh_exec.denied") {
		t.Errorf("buffer = %q, want contains denied event", data)
	}
}

func TestEmitDrainsBufferWhenDaemonReturns(t *testing.T) {
	fd := newFakeDaemon()
	fd.down.Store(true)
	defer fd.Close()
	bufDir := t.TempDir()
	em := newTestEmitter(t, fd.URL(), bufDir)
	req := ExecRequest{Host: "h", Command: "alembic upgrade", Project: "p"}
	for i := 0; i < 3; i++ {
		_ = em.EmitDenied(req, "queued-while-down")
	}

	fd.down.Store(false)
	if _, err := em.DrainBuffer(context.Background()); err != nil {
		t.Fatalf("DrainBuffer: %v", err)
	}

	if got := fd.received.Load(); got < 3 {
		t.Errorf("daemon received = %d, want >=3 after drain", got)
	}

	matches, _ := filepath.Glob(filepath.Join(bufDir, "zen-mcp-ssh-exec-emit-buffer-*.jsonl"))
	if len(matches) != 0 {
		t.Errorf("buffer not drained: %v", matches)
	}
}

func TestEmitInteractiveBlockedSnippetBase64(t *testing.T) {
	fd := newFakeDaemon()
	defer fd.Close()
	em := newTestEmitter(t, fd.URL(), t.TempDir())
	snip := []byte("[sudo] password for testuser:")
	req := ExecRequest{Host: "h", Command: "sudo apt update", Project: "p"}
	if err := em.EmitInteractiveBlocked(req, snip); err != nil {
		t.Fatalf("EmitInteractiveBlocked: %v", err)
	}
	body := <-fd.bodies
	var raw struct {
		Type    string `json:"type"`
		Payload string `json:"payload"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("outer unmarshal: %v (body=%q)", err, body)
	}
	if raw.Type != "ssh_exec.interactive_blocked" {
		t.Errorf("type = %q", raw.Type)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw.Payload), &payload); err != nil {
		t.Fatalf("inner unmarshal: %v", err)
	}
	got, _ := base64.StdEncoding.DecodeString(payload["interactive_snippet_b64"].(string))
	if string(got) != string(snip) {
		t.Errorf("snippet roundtrip mismatch: %q vs %q", got, snip)
	}
}

func TestEmitCompletedSerialisesResult(t *testing.T) {
	fd := newFakeDaemon()
	defer fd.Close()
	em := newTestEmitter(t, fd.URL(), t.TempDir())
	req := ExecRequest{Host: "h", Command: "alembic upgrade", Project: "p"}
	res := ExecResult{
		ExitCode:        0,
		ExitReason:      ExitReasonNormal,
		StdoutBytes:     1024,
		StderrBytes:     0,
		StdoutTruncated: false,
		Duration:        500 * time.Millisecond,
	}
	if err := em.EmitCompleted(req, res); err != nil {
		t.Fatalf("EmitCompleted: %v", err)
	}
	body := <-fd.bodies
	if !strings.Contains(string(body), "ssh_exec.completed") {
		t.Errorf("body = %q", body)
	}
	if !strings.Contains(string(body), "exit_code") {
		t.Errorf("body missing exit_code: %q", body)
	}
	if !strings.Contains(string(body), "exit_reason") {
		t.Errorf("body missing exit_reason: %q", body)
	}
}

func TestEmitCompletedExitReasonTimeout(t *testing.T) {
	fd := newFakeDaemon()
	defer fd.Close()
	em := newTestEmitter(t, fd.URL(), t.TempDir())
	req := ExecRequest{Host: "h", Command: "alembic upgrade", Project: "p"}
	res := ExecResult{ExitCode: -1, ExitReason: ExitReasonTimeout, Duration: 5 * time.Second}
	if err := em.EmitCompleted(req, res); err != nil {
		t.Fatalf("EmitCompleted: %v", err)
	}
	body := <-fd.bodies

	if !strings.Contains(string(body), "exit_reason") || !strings.Contains(string(body), "timeout") {
		t.Errorf("body missing exit_reason=timeout markers: %q", body)
	}
}

func TestEmitNilEmitClientNoOp(t *testing.T) {
	em := NewEmitter(nil, "internal-platform-x")
	req := ExecRequest{Host: "h", Command: "alembic upgrade", Project: "p"}
	if err := em.EmitStarted(req); err != nil {
		t.Errorf("EmitStarted: %v", err)
	}
	if err := em.EmitCompleted(req, ExecResult{}); err != nil {
		t.Errorf("EmitCompleted: %v", err)
	}
	if err := em.EmitDenied(req, "x"); err != nil {
		t.Errorf("EmitDenied: %v", err)
	}
	if err := em.EmitInteractiveBlocked(req, []byte("x")); err != nil {
		t.Errorf("EmitInteractiveBlocked: %v", err)
	}
	if _, err := em.DrainBuffer(context.Background()); err != nil {
		t.Errorf("DrainBuffer: %v", err)
	}
}

func TestEmitPreviewTruncates(t *testing.T) {
	long := strings.Repeat("a", 200)
	short := "alembic"
	if got := preview(long, 80); !strings.HasSuffix(got, "…") || len(got) > 200 {
		t.Errorf("preview long: %q (len=%d)", got, len(got))
	}
	if got := preview(short, 80); got != short {
		t.Errorf("preview short = %q, want %q", got, short)
	}
}

func TestEmitMethodsHandleEmptyPayload(t *testing.T) {
	em := NewEmitter(nil, "internal-platform-x")

	if err := em.EmitInteractiveBlocked(ExecRequest{}, nil); err != nil {
		t.Errorf("nil snippet: %v", err)
	}
}

func TestEmitJSONShapeMatchesSpec(t *testing.T) {
	fd := newFakeDaemon()
	defer fd.Close()
	em := newTestEmitter(t, fd.URL(), t.TempDir())
	req := ExecRequest{Host: "vps", Command: strings.Repeat("z", 100), Project: "internal-platform-x"}
	if err := em.EmitStarted(req); err != nil {
		t.Fatalf("EmitStarted: %v", err)
	}
	body := <-fd.bodies
	t.Logf("body=%s", body)
	for _, k := range []string{"ssh_exec.started", "host", "cmd_preview", "internal-platform-x"} {
		if !strings.Contains(string(body), k) {
			t.Errorf("body missing %q: %q", k, body)
		}
	}
}

var _ = fmt.Sprintf
