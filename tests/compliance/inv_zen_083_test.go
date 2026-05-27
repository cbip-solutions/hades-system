// Task L-8 — runtime compliance test for invariant.
//
// invariant: audit emit no-loss. When the daemon HTTP path fails,
// events MUST be appended to the local buffer file; when the daemon
// returns, the next DrainBuffer flushes them. EmitClient
// implements both halves and is the sole emit path used by
// internal/mcp/sshexec/emit.go.
//
// This test exercises three phases against a controllable fake daemon:
//
// Phase 1 (daemon up) — events land at daemon, buffer stays empty.
// Phase 2 (daemon down) — events land in buffer file.
// Phase 3 (daemon back) — DrainBuffer flushes pending events.

package compliance

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
	"github.com/cbip-solutions/hades-system/internal/mcp/sshexec"
)

func TestInvZen083AuditEmitNoLoss(t *testing.T) {
	var down atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if down.Load() {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	bufDir := t.TempDir()
	tokFile := filepath.Join(t.TempDir(), "tok")
	if err := os.WriteFile(tokFile, []byte("test-token"), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	c, err := client.New(client.Config{
		BaseURL:       srv.URL,
		AuthTokenPath: tokFile,
		MCPName:       "ssh-exec",
	})
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}
	ec := client.NewEmitClient(c, bufDir)
	em := sshexec.NewEmitter(ec, "internal-platform-x")

	req := sshexec.ExecRequest{Host: "h", Command: "alembic upgrade", Project: "p"}
	if err := em.EmitStarted(req); err != nil {
		t.Fatalf("phase 1: %v", err)
	}
	if matches, _ := filepath.Glob(filepath.Join(bufDir, "zen-mcp-ssh-exec-emit-buffer-*.jsonl")); len(matches) > 0 {

		for _, m := range matches {
			data, _ := os.ReadFile(m)
			if strings.Count(string(data), "{") > 0 {
				t.Errorf("phase 1: buffer file %q has %d events; want 0", m, strings.Count(string(data), "{"))
			}
		}
	}

	down.Store(true)
	for i := 0; i < 3; i++ {
		if err := em.EmitDenied(req, "test"); err != nil {
			t.Fatalf("phase 2: %v (buffer path must swallow daemon failure)", err)
		}
	}
	matches, _ := filepath.Glob(filepath.Join(bufDir, "zen-mcp-ssh-exec-emit-buffer-*.jsonl"))
	if len(matches) != 1 {
		t.Fatalf("phase 2: buffer file glob = %v, want 1 entry", matches)
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("phase 2 read: %v", err)
	}
	if strings.Count(string(data), "{") < 3 {
		t.Errorf("phase 2: buffer has fewer than 3 events: %s", data)
	}

	down.Store(false)
	n, err := em.DrainBuffer(context.Background())
	if err != nil {
		t.Fatalf("phase 3 DrainBuffer: %v", err)
	}
	if n < 3 {
		t.Errorf("phase 3: drained %d events, want >=3", n)
	}
	if data, err := os.ReadFile(matches[0]); err == nil && strings.Count(string(data), "{") > 0 {
		t.Errorf("phase 3: buffer not drained: %s", data)
	}
}
