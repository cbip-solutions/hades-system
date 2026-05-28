//go:build cgo && !nocgo
// +build cgo,!nocgo

// SPDX-License-Identifier: MIT

package semantic

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"time"

	"github.com/cbip-solutions/hades-system/internal/caronte/intent"
)

var ErrEcosystemMCPProbeNotWired = errors.New("caronte/semantic: ecosystem-mcp http probe unavailable (composition-root injection required for http endpoints)")

// Selector chooses the embedder per fallback chain at NewEngine time.
// Implementations MUST be deterministic for the same EmbedderConfig +
// filesystem state; MUST NOT panic on missing model files; MUST log a single
// WARN line at degrade (not per-call).
type Selector interface {
	Select(ctx context.Context, cfg EmbedderConfig) (intent.CodeEmbedder, EmbedderMode, error)
}

type defaultSelector struct {
	logger *slog.Logger

	ecosystemMCPProber func(ctx context.Context, endpoint string) error
}

func NewDefaultSelector() Selector {
	return &defaultSelector{
		logger:             slog.Default(),
		ecosystemMCPProber: defaultEcosystemMCPProbe,
	}
}

func NewDefaultSelectorWithLogger(logger *slog.Logger) Selector {
	if logger == nil {
		logger = slog.Default()
	}
	return &defaultSelector{
		logger:             logger,
		ecosystemMCPProber: defaultEcosystemMCPProbe,
	}
}

func (s *defaultSelector) Select(ctx context.Context, cfg EmbedderConfig) (intent.CodeEmbedder, EmbedderMode, error) {
	if err := cfg.Validate(); err != nil {
		return nil, "", err
	}

	switch cfg.Mode {
	case "jina-local":

		emb, err := NewJinaEmbedder(cfg)
		if err != nil {
			return nil, "", fmt.Errorf("caronte/semantic: pinned mode=jina-local: %w", err)
		}
		return emb, EmbedderJinaLocal, nil

	case "ecosystem-mcp":

		if cfg.EcosystemMCPEndpoint == "" {
			return nil, "", fmt.Errorf("caronte/semantic: pinned mode=ecosystem-mcp but EcosystemMCPEndpoint empty")
		}
		if err := s.ecosystemMCPProber(ctx, cfg.EcosystemMCPEndpoint); err != nil {
			return nil, "", fmt.Errorf("caronte/semantic: pinned mode=ecosystem-mcp: %w", err)
		}
		return newEcosystemMCPEmbedder(cfg.EcosystemMCPEndpoint), EmbedderEcosystemMCP, nil

	case "bm25-only":

		return NewBM25OnlyEmbedder(), EmbedderBM25Only, nil

	case "auto":

	default:

		return nil, "", fmt.Errorf("caronte/semantic: unknown Mode %q", cfg.Mode)
	}

	if emb, err := NewJinaEmbedder(cfg); err == nil {
		return emb, EmbedderJinaLocal, nil
	} else if !errors.Is(err, ErrJinaModelMissing) && !errors.Is(err, ErrONNXRuntimeUnavailable) {

		s.logger.WarnContext(ctx, "caronte.embedder.jina-local unavailable", "err", err.Error())
	}

	if cfg.EcosystemMCPEndpoint != "" {
		if err := s.ecosystemMCPProber(ctx, cfg.EcosystemMCPEndpoint); err == nil {
			return newEcosystemMCPEmbedder(cfg.EcosystemMCPEndpoint), EmbedderEcosystemMCP, nil
		}

	}

	s.logger.WarnContext(ctx, "caronte.embedder degraded to bm25-only (no jina-local, no ecosystem-mcp)")
	return NewBM25OnlyEmbedder(), EmbedderBM25Only, nil
}

func defaultEcosystemMCPProbe(ctx context.Context, endpoint string) error {
	u, err := url.Parse(endpoint)
	if err != nil {
		return fmt.Errorf("ecosystem-mcp parse %q: %w", endpoint, err)
	}
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	switch u.Scheme {
	case "unix":

		d := net.Dialer{Timeout: 2 * time.Second}
		conn, err := d.DialContext(probeCtx, "unix", u.Path)
		if err != nil {
			return fmt.Errorf("ecosystem-mcp dial unix %q: %w", u.Path, err)
		}
		return conn.Close()
	case "http", "https":

		return fmt.Errorf("ecosystem-mcp probe endpoint=%s: %w", endpoint, ErrEcosystemMCPProbeNotWired)
	default:
		return fmt.Errorf("ecosystem-mcp: unsupported endpoint scheme %q", u.Scheme)
	}
}

func NewDefaultSelectorWithProber(logger *slog.Logger, prober func(ctx context.Context, endpoint string) error) Selector {
	if logger == nil {
		logger = slog.Default()
	}
	if prober == nil {
		prober = defaultEcosystemMCPProbe
	}
	return &defaultSelector{logger: logger, ecosystemMCPProber: prober}
}

type ecosystemMCPEmbedder struct {
	endpoint string
}

func newEcosystemMCPEmbedder(endpoint string) intent.CodeEmbedder {
	return &ecosystemMCPEmbedder{endpoint: endpoint}
}

func (e *ecosystemMCPEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {

	return nil, fmt.Errorf("caronte/semantic: ecosystem-mcp placeholder (endpoint=%s) — daemon adapter unavailable: %w", e.endpoint, ErrEmbedderUnavailable)
}

func (e *ecosystemMCPEmbedder) Dimensions() int { return 1536 }
