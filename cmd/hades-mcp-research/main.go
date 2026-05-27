// SPDX-License-Identifier: MIT
// cmd/hades-mcp-research/main.go is the thin entrypoint for the research
// MCP binary. It parses --socket, --auth-token-path, and
// --allow-fallback, constructs the Server with real adapters, and
// calls server.Serve.
//
// post-review I-2 hardening:
//
// - C-16: when the daemon socket is unreachable, the server
// previously swallowed the error and ran with NoOp adapters
// (always-allow budget, drop-on-floor audit). Operators believed
// the MCP was working when every gate was disabled. Now the
// binary refuses to start unless --allow-fallback=true is passed
// explicitly. With --allow-fallback the server emits a
// prominent WARN log on startup so the degraded posture cannot
// hide.
// - C-17: startup line records which adapters are wired (NoOp vs
// real) so log-grep can confirm the production wiring posture
// without inspecting the binary's behaviour.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/cbip-solutions/hades-system/internal/mcp/client"
	"github.com/cbip-solutions/hades-system/internal/mcp/research"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("hades-mcp-research: %v", err)
	}
}

func run() error {
	fs := flag.NewFlagSet("hades-mcp-research", flag.ContinueOnError)
	socket := fs.String("socket", "/var/run/hades-system/hades-system.sock", "daemon Unix socket path")
	authTokenPath := fs.String("auth-token-path", "", "path to daemon auth-token file (required)")
	daemonURL := fs.String("daemon-url", "", "daemon HTTP base URL (overrides --socket for non-unix-socket setups)")
	allowFallback := fs.Bool("allow-fallback", false, "permit start with NoOp budget+audit when daemon unreachable (DANGEROUS — disables the SEMANTIC budget gate; production MUST set false)")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return fmt.Errorf("parse flags: %w", err)
	}

	if *authTokenPath == "" {
		return fmt.Errorf("--auth-token-path required")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	srv, err := buildServer(ctx, buildOptions{
		Socket:        *socket,
		DaemonURL:     *daemonURL,
		AuthTokenPath: *authTokenPath,
		AllowFallback: *allowFallback,
		LogOut:        os.Stderr,
	})
	if err != nil {
		return fmt.Errorf("build server: %w", err)
	}
	defer srv.Close()

	if err := srv.Serve(ctx); err != nil {
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}

type buildOptions struct {
	Socket        string
	DaemonURL     string
	AuthTokenPath string

	AllowFallback bool

	LogOut io.Writer
}

func buildServer(ctx context.Context, opts buildOptions) (*research.Server, error) {
	logOut := opts.LogOut
	if logOut == nil {
		logOut = io.Discard
	}
	logger := log.New(logOut, "", log.LstdFlags)

	cfg := client.Config{
		SocketPath:    opts.Socket,
		BaseURL:       opts.DaemonURL,
		AuthTokenPath: opts.AuthTokenPath,
		MCPName:       "research",
	}
	httpClient, err := client.New(cfg)
	if err != nil {
		// C-16: daemon unreachable. WITHOUT --allow-fallback we refuse
		// to start (production posture: NEVER serve with disabled
		// gates). WITH --allow-fallback we proceed with NoOp adapters
		// AFTER an unmissable warning.
		if !opts.AllowFallback {
			return nil, fmt.Errorf("daemon client unreachable (socket=%q daemonURL=%q): %w (pass --allow-fallback to start with NoOp adapters; production MUST NOT)",
				opts.Socket, opts.DaemonURL, err)
		}
		logger.Printf("WARN: research MCP starting WITHOUT daemon — NoOp budget + NoOp audit wired (--allow-fallback=true). The SEMANTIC budget gate is DISABLED. This is acceptable in dev/CI ONLY.")
		httpClient = nil
	}

	var (
		budgetAdapter *research.BudgetAdapter
		auditAdapter  *research.AuditAdapter
		cacheAdapter  *research.CacheAdapter
	)
	if httpClient != nil {
		budgetAdapter = research.NewBudgetAdapter(client.NewBudgetClient(httpClient))
		bufDir := os.TempDir()
		auditAdapter = research.NewAuditAdapter(client.NewEmitClient(httpClient, bufDir))
	}
	cacheAdapter = research.NewCacheAdapter(research.CacheAdapterOptions{
		DaemonURL:  opts.DaemonURL,
		HTTPClient: nil,
	})

	webSearch := research.NewWebSearch(research.WebSearchOptions{Cache: cacheAdapter})
	arxiv := research.NewArxiv(research.ArxivOptions{Cache: cacheAdapter})
	githubSearch := research.NewGitHubSearch(research.GitHubSearchOptions{Cache: cacheAdapter})
	ecosystem := research.NewEcosystemDocs(research.EcosystemDocsOptions{})
	cite := research.NewCiteVerifier(research.CiteVerifierOptions{})
	synthesizer := research.NewSynthesizer(research.SynthesizerOptions{
		DaemonURL: opts.DaemonURL,
	})

	var gnAdapter research.GitnexusClient = research.NoOpGitnexus{}
	if httpClient != nil {
		gnAdapter = research.NewCaronteCodeGraph(httpClient)
	}

	bAdapter := research.BudgetClient(research.NoOpBudget{})
	if budgetAdapter != nil {
		bAdapter = budgetAdapter
	}
	aAdapter := research.AuditClient(research.NoOpAudit{})
	if auditAdapter != nil {
		aAdapter = auditAdapter
	}

	dispatcher := research.NewDispatcher(research.DispatcherOptions{
		WebSearch:    webSearch,
		Arxiv:        arxiv,
		GitHub:       githubSearch,
		Ecosystem:    ecosystem,
		Gitnexus:     gnAdapter,
		Cite:         cite,
		BudgetClient: bAdapter,
		AuditClient:  aAdapter,
		Cache:        cacheAdapter,
	})

	logger.Printf("research MCP starting: codegraph=%T budget=%T audit=%T cache=%T allow_fallback=%v",
		gnAdapter, bAdapter, aAdapter, cacheAdapter, opts.AllowFallback)

	return research.NewServer(&research.ServerOptions{
		Dispatcher:     dispatcher,
		WebSearchTool:  webSearch,
		ArxivTool:      arxiv,
		GitHubTool:     githubSearch,
		EcosystemTool:  ecosystem,
		GitnexusClient: gnAdapter,
		Synthesizer:    synthesizer,
		Cache:          cacheAdapter,
		BudgetClient:   bAdapter,
		AuditClient:    aAdapter,
		Cite:           cite,
	})
}
