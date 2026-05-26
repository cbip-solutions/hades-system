//go:build cgo
// +build cgo

package compliance

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/caronte/intent"
	"github.com/cbip-solutions/hades-system/internal/caronte/semantic"
)

func TestInvZen274SourceAnchors(t *testing.T) {
	root := repoRoot(t)

	cases := []struct {
		path     string
		patterns []*regexp.Regexp
	}{
		{
			path: filepath.Join("internal", "caronte", "semantic", "jina_embedder.go"),
			patterns: []*regexp.Regexp{
				regexp.MustCompile(`EmbedderJinaLocal\s+EmbedderMode\s*=\s*"jina-local"`),
				regexp.MustCompile(`EmbedderEcosystemMCP\s+EmbedderMode\s*=\s*"ecosystem-mcp"`),
				regexp.MustCompile(`EmbedderBM25Only\s+EmbedderMode\s*=\s*"bm25-only"`),
				regexp.MustCompile(`ErrEmbedderUnavailable\s*=\s*errors\.New\(`),
				regexp.MustCompile(`ErrJinaModelMissing\s*=\s*errors\.New\(`),
				regexp.MustCompile(`func\s+NewJinaEmbedder\(cfg\s+EmbedderConfig\)`),
				regexp.MustCompile(`func\s+NewBM25OnlyEmbedder\(\)\s+intent\.CodeEmbedder`),
			},
		},
		{
			path: filepath.Join("internal", "caronte", "semantic", "selector.go"),
			patterns: []*regexp.Regexp{
				regexp.MustCompile(`type\s+Selector\s+interface`),
				regexp.MustCompile(`func\s+NewDefaultSelector\(\)\s+Selector`),
				regexp.MustCompile(`case\s+"jina-local":`),
				regexp.MustCompile(`case\s+"ecosystem-mcp":`),
				regexp.MustCompile(`case\s+"bm25-only":`),
				regexp.MustCompile(`case\s+"auto":`),
				regexp.MustCompile(`bm25-only`),
			},
		},
		{
			path: filepath.Join("internal", "caronte", "semantic", "embedder_config.go"),
			patterns: []*regexp.Regexp{
				regexp.MustCompile(`type\s+EmbedderConfig\s+struct`),
				regexp.MustCompile(`func\s+DefaultEmbedderConfig\(\)\s+EmbedderConfig`),
				regexp.MustCompile(`func\s+\(c\s+\*EmbedderConfig\)\s+Validate\(\)`),
				regexp.MustCompile(`"auto"\s*:`),
				regexp.MustCompile(`jina-code.*model\.onnx`),
			},
		},
		{
			path: filepath.Join("internal", "caronte", "engine.go"),
			patterns: []*regexp.Regexp{
				// The load-bearing degrade hook — searchSymbols MUST detect
				// the BM25-only sentinel and short-circuit to lexical retrieval.
				regexp.MustCompile(`errors\.Is\(err,\s*semantic\.ErrEmbedderUnavailable\)`),

				regexp.MustCompile(`sel\.Select\(`),

				regexp.MustCompile(`caronte\.embedder\.mode`),

				regexp.MustCompile(`func\s+\(pe\s+\*projectEngine\)\s+searchSymbolsBM25Only`),
			},
		},
		{
			path: filepath.Join("scripts", "download-jina-model.sh"),
			patterns: []*regexp.Regexp{
				regexp.MustCompile(`jinaai/jina-embeddings-v2-base-code`),
				regexp.MustCompile(`set -euo pipefail`),
				regexp.MustCompile(`--pin-sha`),
				regexp.MustCompile(`expected-sha`),
			},
		},
	}

	for _, c := range cases {
		full := filepath.Join(root, c.path)
		body, err := os.ReadFile(full)
		if err != nil {
			t.Errorf("inv-zen-274: read %s: %v", c.path, err)
			continue
		}
		for _, p := range c.patterns {
			if !p.Match(body) {
				t.Errorf("inv-zen-274 source-anchor violated: %s missing pattern %q",
					c.path, p.String())
			}
		}
	}
}

func TestInvZen274DownloadScriptExecutable(t *testing.T) {
	root := repoRoot(t)
	scriptPath := filepath.Join(root, "scripts", "download-jina-model.sh")
	info, err := os.Stat(scriptPath)
	if err != nil {
		t.Fatalf("inv-zen-274: stat %s: %v", scriptPath, err)
	}
	mode := info.Mode().Perm()
	if mode&0o111 == 0 {
		t.Errorf("inv-zen-274: scripts/download-jina-model.sh perm = %v; want executable", mode)
	}
}

func TestInvZen274NoPlaceholderInDownloadScript(t *testing.T) {
	root := repoRoot(t)
	body, err := os.ReadFile(filepath.Join(root, "scripts", "download-jina-model.sh"))
	if err != nil {
		t.Fatalf("inv-zen-274: read script: %v", err)
	}
	for _, banned := range []string{"<PIN_AT_FIRST_RUN>", "TODO:", "FIXME:", "XXX:"} {
		if bytes.Contains(body, []byte(banned)) {
			t.Errorf("inv-zen-274: download script contains banned token %q", banned)
		}
	}
}

func TestInvZen274FallbackChain(t *testing.T) {
	t.Run("JinaPresent", func(t *testing.T) {
		dir := t.TempDir()
		modelPath := filepath.Join(dir, "model.onnx")
		if err := os.WriteFile(modelPath, []byte("fake"), 0o600); err != nil {
			t.Fatalf("seed fake model: %v", err)
		}

		logBuf := &lockedBuf{}
		logger := slog.New(slog.NewTextHandler(logBuf, &slog.HandlerOptions{Level: slog.LevelInfo}))
		sel := semantic.NewDefaultSelectorWithLogger(logger)

		cfg := semantic.EmbedderConfig{
			Mode:                 "auto",
			JinaModelPath:        modelPath,
			EcosystemMCPEndpoint: "",
		}
		emb, mode, err := sel.Select(context.Background(), cfg)
		if err != nil {
			t.Fatalf("Select: %v", err)
		}
		if emb == nil {
			t.Fatal("Select returned nil embedder")
		}

		if mode != semantic.EmbedderBM25Only {
			t.Errorf("default build mode = %q; want bm25-only (no ONNX runtime linked)", mode)
		}
	})

	t.Run("EcosystemMCPPresent", func(t *testing.T) {

		reachable := func(_ context.Context, _ string) error { return nil }
		logBuf := &lockedBuf{}
		logger := slog.New(slog.NewTextHandler(logBuf, &slog.HandlerOptions{Level: slog.LevelInfo}))
		sel := semantic.NewDefaultSelectorWithProber(logger, reachable)

		cfg := semantic.EmbedderConfig{
			Mode:                 "auto",
			JinaModelPath:        "/nonexistent",
			EcosystemMCPEndpoint: "http://stub",
		}
		_, mode, err := sel.Select(context.Background(), cfg)
		if err != nil {
			t.Fatalf("Select: %v", err)
		}
		if mode != semantic.EmbedderEcosystemMCP {
			t.Errorf("mode = %q; want ecosystem-mcp", mode)
		}
	})

	t.Run("FullDegrade", func(t *testing.T) {
		logBuf := &lockedBuf{}
		logger := slog.New(slog.NewTextHandler(logBuf, &slog.HandlerOptions{Level: slog.LevelInfo}))
		sel := semantic.NewDefaultSelectorWithLogger(logger)

		cfg := semantic.EmbedderConfig{
			Mode:                 "auto",
			JinaModelPath:        "/nonexistent",
			EcosystemMCPEndpoint: "",
		}
		emb, mode, err := sel.Select(context.Background(), cfg)
		if err != nil {
			t.Fatalf("Select: %v", err)
		}
		if mode != semantic.EmbedderBM25Only {
			t.Errorf("mode = %q; want bm25-only", mode)
		}

		warnCount := strings.Count(logBuf.String(), "level=WARN")
		if warnCount != 1 {
			t.Errorf("WARN count = %d; want 1 (single degrade WARN)", warnCount)
		}

		_, embErr := emb.Embed(context.Background(), "x")
		if !errors.Is(embErr, semantic.ErrEmbedderUnavailable) {
			t.Errorf("BM25-only Embed err = %v; want ErrEmbedderUnavailable", embErr)
		}
	})
}

func TestInvZen274SentinelsDistinct(t *testing.T) {
	if semantic.ErrEmbedderUnavailable == semantic.ErrJinaModelMissing {
		t.Error("ErrEmbedderUnavailable == ErrJinaModelMissing (sentinels collapsed)")
	}
	if semantic.ErrEmbedderUnavailable.Error() == "" {
		t.Error("ErrEmbedderUnavailable has empty message")
	}
	if semantic.ErrJinaModelMissing.Error() == "" {
		t.Error("ErrJinaModelMissing has empty message")
	}
	if !strings.Contains(semantic.ErrEmbedderUnavailable.Error(), "bm25") &&
		!strings.Contains(semantic.ErrEmbedderUnavailable.Error(), "BM25") {
		t.Errorf("ErrEmbedderUnavailable message missing bm25 mention: %v", semantic.ErrEmbedderUnavailable)
	}
}

// TestInvZen274BM25OnlySatisfiesCodeEmbedder is the compile-anchor witness:
// the BM25-only sentinel MUST satisfy intent.CodeEmbedder so the selector
// can return it from Select without an adapter layer.
func TestInvZen274BM25OnlySatisfiesCodeEmbedder(t *testing.T) {
	emb := semantic.NewBM25OnlyEmbedder()
	if emb == nil {
		t.Fatal("NewBM25OnlyEmbedder() returned nil")
	}
	var _ intent.CodeEmbedder = emb
	if got := emb.Dimensions(); got != 1536 {
		t.Errorf("BM25-only Dimensions = %d; want 1536 (parity with Jina)", got)
	}
}

func TestInvZen274SelectorNeverReturnsNilEmbedder(t *testing.T) {
	logBuf := &lockedBuf{}
	logger := slog.New(slog.NewTextHandler(logBuf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	sel := semantic.NewDefaultSelectorWithLogger(logger)

	for _, mode := range []string{"auto", "bm25-only"} {
		t.Run(mode, func(t *testing.T) {
			cfg := semantic.EmbedderConfig{Mode: mode}
			emb, _, err := sel.Select(context.Background(), cfg)
			if err != nil {
				t.Fatalf("Select(%s): %v", mode, err)
			}
			if emb == nil {
				t.Errorf("Select(%s) returned nil embedder; selector contract violated", mode)
			}
		})
	}
}

func TestInvZen274ConfigDefaultModeIsAuto(t *testing.T) {
	cfg := semantic.DefaultEmbedderConfig()
	if cfg.Mode != "auto" {
		t.Errorf("DefaultEmbedderConfig.Mode = %q; want auto", cfg.Mode)
	}

	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate(default) = %v; want nil", err)
	}
}

type lockedBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuf) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
