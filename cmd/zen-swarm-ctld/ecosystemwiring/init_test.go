//go:build cgo
// +build cgo

package ecosystemwiring_test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/cbip-solutions/hades-system/cmd/zen-swarm-ctld/ecosystemwiring"
	"github.com/cbip-solutions/hades-system/internal/daemon/handlers/ecospec"
)

type fakeServer struct {
	mu sync.Mutex
	h  ecospec.EcosystemHandler
}

func (f *fakeServer) SetEcosystemHandler(h ecospec.EcosystemHandler) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.h = h
}

func (f *fakeServer) handler() ecospec.EcosystemHandler {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.h
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(slogDiscardWriter{}, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

type slogDiscardWriter struct{}

func (slogDiscardWriter) Write(p []byte) (int, error) { return len(p), nil }

func writeF8ConfigStubs(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir configdir: %v", err)
	}
	files := map[string]string{
		"ecosystem-prefix.toml":         `[primary]` + "\n" + `model = "qwen2.5:7b"` + "\n" + `backend = "ollama"` + "\n",
		"ecosystem-embedder.toml":       `[primary]` + "\n" + `model = "jina-code-embeddings-1.5b"` + "\n" + `backend = "mps"` + "\n",
		"ecosystem-reranker.toml":       `[primary]` + "\n" + `model = "BAAI/bge-reranker-v2-m3"` + "\n" + `backend = "mps"` + "\n",
		"ecosystem-router.toml":         `[classifier]` + "\n" + `feature_dim = 1536` + "\n" + `softmax_top_k = 2` + "\n",
		"ecosystem-version-detect.toml": `[defaults]` + "\n" + `go = "latest"` + "\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
}

func TestTryWire_MissingConfigs_NilInjection(t *testing.T) {
	srv := &fakeServer{}
	logger := discardLogger()

	dataRoot := t.TempDir()
	cfgDir := filepath.Join(t.TempDir(), "providers-absent")

	adapter, cleanup := ecosystemwiring.TryWire(context.Background(), srv, logger, dataRoot, cfgDir)
	defer cleanup()

	if adapter != nil {
		t.Fatalf("adapter expected nil on missing-configs path, got %T", adapter)
	}
	if srv.handler() != nil {
		t.Fatalf("SetEcosystemHandler should not be called on missing-configs; got %T", srv.handler())
	}
}

func TestTryWire_MalformedEmbedderConfig_NilInjection(t *testing.T) {
	srv := &fakeServer{}
	logger := discardLogger()
	dataRoot := t.TempDir()
	cfgDir := t.TempDir()

	writeF8ConfigStubs(t, cfgDir)
	if err := os.WriteFile(filepath.Join(cfgDir, "ecosystem-embedder.toml"), []byte("not valid toml [[["), 0o600); err != nil {
		t.Fatalf("corrupt embedder toml: %v", err)
	}

	adapter, cleanup := ecosystemwiring.TryWire(context.Background(), srv, logger, dataRoot, cfgDir)
	defer cleanup()

	if adapter != nil {
		t.Fatalf("adapter expected nil on malformed-config path, got %T", adapter)
	}
	if srv.handler() != nil {
		t.Fatalf("SetEcosystemHandler should not be called on malformed-config")
	}
}

func TestTryWire_MalformedPrefixConfig_NilInjection(t *testing.T) {
	srv := &fakeServer{}
	logger := discardLogger()
	dataRoot := t.TempDir()
	cfgDir := t.TempDir()
	writeF8ConfigStubs(t, cfgDir)
	if err := os.WriteFile(filepath.Join(cfgDir, "ecosystem-prefix.toml"), []byte("garbage [[]"), 0o600); err != nil {
		t.Fatalf("corrupt prefix toml: %v", err)
	}

	adapter, cleanup := ecosystemwiring.TryWire(context.Background(), srv, logger, dataRoot, cfgDir)
	defer cleanup()

	if adapter != nil {
		t.Fatalf("adapter expected nil on malformed prefix-config, got %T", adapter)
	}
	if srv.handler() != nil {
		t.Fatalf("SetEcosystemHandler should not be called")
	}
}

func TestTryWire_HappyPath_InjectsAdapter(t *testing.T) {
	srv := &fakeServer{}
	logger := discardLogger()
	dataRoot := t.TempDir()
	cfgDir := t.TempDir()
	writeF8ConfigStubs(t, cfgDir)

	adapter, cleanup := ecosystemwiring.TryWire(context.Background(), srv, logger, dataRoot, cfgDir)
	defer func() {
		if err := cleanup(); err != nil {
			t.Fatalf("cleanup: %v", err)
		}
	}()

	if adapter == nil {
		t.Fatalf("adapter expected non-nil on happy path")
	}
	if srv.handler() == nil {
		t.Fatalf("SetEcosystemHandler expected to receive non-nil adapter")
	}

	if err := adapter.Pin(context.Background(), "go", "0.0.0"); !errors.Is(err, ecospec.ErrEcosystemVersionNotFound) {
		t.Fatalf("Pin against fresh DB want ErrEcosystemVersionNotFound, got %v", err)
	}
}

func TestTryWire_CleanupIdempotent(t *testing.T) {
	srv := &fakeServer{}
	logger := discardLogger()
	dataRoot := t.TempDir()
	cfgDir := t.TempDir()
	writeF8ConfigStubs(t, cfgDir)

	_, cleanup := ecosystemwiring.TryWire(context.Background(), srv, logger, dataRoot, cfgDir)

	if err := cleanup(); err != nil {
		t.Fatalf("first cleanup: %v", err)
	}

	_ = cleanup()
}

func TestTryWire_PerEcoMkdir_PartialFailureToleratesOthers(t *testing.T) {

	srv := &fakeServer{}
	logger := discardLogger()
	dataRoot := t.TempDir()
	cfgDir := t.TempDir()
	writeF8ConfigStubs(t, cfgDir)

	adapter, cleanup := ecosystemwiring.TryWire(context.Background(), srv, logger, dataRoot, cfgDir)
	defer cleanup()
	if adapter == nil {
		t.Fatalf("adapter nil; expected partial-wire success")
	}

	for _, eco := range []string{"go", "python", "typescript", "rust"} {
		if err := adapter.Pin(context.Background(), eco, "0.0.0"); !errors.Is(err, ecospec.ErrEcosystemVersionNotFound) {
			t.Fatalf("Pin %s want ErrEcosystemVersionNotFound, got %v", eco, err)
		}
	}
}
