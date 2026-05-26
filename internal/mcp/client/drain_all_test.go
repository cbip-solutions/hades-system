package client_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/mcp/client"
)

func TestDrainAllBuffers_RecoversMultipleOrphans(t *testing.T) {
	var received atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	bufDir := t.TempDir()
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "auth-token")
	_ = os.WriteFile(tokenPath, []byte("tok"), 0600)
	cfg := client.Config{
		BaseURL:       srv.URL,
		AuthTokenPath: tokenPath,
		MCPName:       "research",
	}
	c, err := client.New(cfg)
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}
	ec := client.NewEmitClient(c, bufDir)

	pids := []string{"1001", "1002", "1003"}
	for _, pid := range pids {
		path := filepath.Join(bufDir, "zen-mcp-research-emit-buffer-"+pid+".jsonl")
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatalf("create orphan %s: %v", path, err)
		}

		for i := 0; i < 2; i++ {
			line, _ := json.Marshal(client.AuditEvent{
				Type:    "orphan." + pid,
				Payload: `{"i":` + pid + `}`,
			})
			_, _ = f.Write(append(line, '\n'))
		}
		_ = f.Close()
	}

	n, err := ec.DrainAllBuffers(context.Background(), bufDir)
	if err != nil {
		t.Fatalf("DrainAllBuffers: %v", err)
	}
	if n != 6 {
		t.Errorf("drained %d, want 6 (2 per orphan × 3 orphans)", n)
	}
	if got := received.Load(); got != 6 {
		t.Errorf("daemon received %d, want 6", got)
	}

	for _, pid := range pids {
		path := filepath.Join(bufDir, "zen-mcp-research-emit-buffer-"+pid+".jsonl")
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Errorf("orphan %s should be removed, got: %v", path, statErr)
		}
		drainingPath := path + ".draining"
		if _, statErr := os.Stat(drainingPath); !os.IsNotExist(statErr) {
			t.Errorf(".draining for %s should be removed, got: %v", path, statErr)
		}
	}
}

func TestDrainAllBuffers_OnlyMatchingMCPName(t *testing.T) {
	var received atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	bufDir := t.TempDir()
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "auth-token")
	_ = os.WriteFile(tokenPath, []byte("tok"), 0600)
	cfg := client.Config{
		BaseURL:       srv.URL,
		AuthTokenPath: tokenPath,
		MCPName:       "audit",
	}
	c, _ := client.New(cfg)
	ec := client.NewEmitClient(c, bufDir)

	auditPath := filepath.Join(bufDir, "zen-mcp-audit-emit-buffer-9001.jsonl")
	researchPath := filepath.Join(bufDir, "zen-mcp-research-emit-buffer-9001.jsonl")
	for _, p := range []string{auditPath, researchPath} {
		f, _ := os.OpenFile(p, os.O_CREATE|os.O_WRONLY, 0600)
		line, _ := json.Marshal(client.AuditEvent{Type: "x", Payload: `{}`})
		_, _ = f.Write(append(line, '\n'))
		_ = f.Close()
	}

	n, err := ec.DrainAllBuffers(context.Background(), bufDir)
	if err != nil {
		t.Fatalf("DrainAllBuffers: %v", err)
	}
	if n != 1 {
		t.Errorf("audit drainer drained %d, want 1 (only audit orphan)", n)
	}

	if _, statErr := os.Stat(auditPath); !os.IsNotExist(statErr) {
		t.Errorf("audit orphan should be drained, got: %v", statErr)
	}

	if _, statErr := os.Stat(researchPath); os.IsNotExist(statErr) {
		t.Errorf("research orphan should NOT be touched by audit drainer")
	}
}

func TestDrainAllBuffers_EmptyDirReturnsZero(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	bufDir := t.TempDir()
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "auth-token")
	_ = os.WriteFile(tokenPath, []byte("tok"), 0600)
	cfg := client.Config{BaseURL: srv.URL, AuthTokenPath: tokenPath, MCPName: "empty"}
	c, _ := client.New(cfg)
	ec := client.NewEmitClient(c, bufDir)

	n, err := ec.DrainAllBuffers(context.Background(), bufDir)
	if err != nil {
		t.Fatalf("DrainAllBuffers on empty dir: %v", err)
	}
	if n != 0 {
		t.Errorf("n = %d, want 0", n)
	}
}

func TestDrainAllBuffers_NonexistentDirReturnsZero(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	bufDir := t.TempDir()
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "auth-token")
	_ = os.WriteFile(tokenPath, []byte("tok"), 0600)
	cfg := client.Config{BaseURL: srv.URL, AuthTokenPath: tokenPath, MCPName: "noexist"}
	c, _ := client.New(cfg)
	ec := client.NewEmitClient(c, bufDir)

	n, err := ec.DrainAllBuffers(context.Background(), filepath.Join(bufDir, "doesnotexist"))
	if err != nil {
		t.Fatalf("DrainAllBuffers on nonexistent dir: %v", err)
	}
	if n != 0 {
		t.Errorf("n = %d, want 0", n)
	}
}

func TestDrainAllBuffers_RecoversBothLiveAndDraining(t *testing.T) {
	var received atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	bufDir := t.TempDir()
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "auth-token")
	_ = os.WriteFile(tokenPath, []byte("tok"), 0600)
	cfg := client.Config{BaseURL: srv.URL, AuthTokenPath: tokenPath, MCPName: "mid-crash"}
	c, _ := client.New(cfg)
	ec := client.NewEmitClient(c, bufDir)

	livePath := filepath.Join(bufDir, "zen-mcp-mid-crash-emit-buffer-7001.jsonl")
	drainingPath := livePath + ".draining"

	lf, _ := os.OpenFile(livePath, os.O_CREATE|os.O_WRONLY, 0600)
	line1, _ := json.Marshal(client.AuditEvent{Type: "live", Payload: `{}`})
	_, _ = lf.Write(append(line1, '\n'))
	_ = lf.Close()

	df, _ := os.OpenFile(drainingPath, os.O_CREATE|os.O_WRONLY, 0600)
	for i := 0; i < 2; i++ {
		line, _ := json.Marshal(client.AuditEvent{Type: "draining", Payload: `{}`})
		_, _ = df.Write(append(line, '\n'))
	}
	_ = df.Close()

	n, err := ec.DrainAllBuffers(context.Background(), bufDir)
	if err != nil {
		t.Fatalf("DrainAllBuffers: %v", err)
	}
	if n != 3 {
		t.Errorf("drained %d, want 3 (1 live + 2 draining)", n)
	}
	if got := received.Load(); got != 3 {
		t.Errorf("daemon received %d, want 3", got)
	}
}

func TestDrainAllBuffers_RequiresMCPName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	bufDir := t.TempDir()
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "auth-token")
	_ = os.WriteFile(tokenPath, []byte("tok"), 0600)
	cfg := client.Config{BaseURL: srv.URL, AuthTokenPath: tokenPath, MCPName: ""}
	c, _ := client.New(cfg)

	ec := client.NewEmitClient(c, bufDir)

	path := filepath.Join(bufDir, "zen-mcp-unknown-emit-buffer-1.jsonl")
	f, _ := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0600)
	line, _ := json.Marshal(client.AuditEvent{Type: "u", Payload: `{}`})
	_, _ = f.Write(append(line, '\n'))
	_ = f.Close()

	n, err := ec.DrainAllBuffers(context.Background(), bufDir)
	if err != nil {
		t.Fatalf("DrainAllBuffers with unknown MCPName: %v", err)
	}
	if n != 1 {
		t.Errorf("n = %d, want 1 (unknown-scoped orphan)", n)
	}
}
