// SPDX-License-Identifier: MIT
// cmd/hades-mcp-audit/main.go
//
// hades-mcp-audit — audit MCP binary.
//
// Invoked by OpenCode plugin via stdio transport (design choice B: stdio canonical).
// Reads daemon socket path, auth-token-path, reviewer pool, and min pool size
// from flags. On missing or invalid config, exits non-zero with a human-
// readable error (never panics).
//
// Usage (by plugin):
//
// hades-mcp-audit --socket /var/run/hades-system/hades-system.sock --auth-token-path /path/to/token
//
// Usage (operator debug with TCP URL):
//
// hades-mcp-audit --daemon-url http://localhost:4471 --auth-token-path /path/to/token
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/cbip-solutions/hades-system/internal/mcp/audit"
)

type buildOptions struct {
	Socket               string
	DaemonURL            string
	AuthTokenPath        string
	ReviewerFamilyPool   string
	MinPoolSize          int
	DefaultReviewerModel string
	Doctrine             string
}

func buildServer(opts buildOptions) (*audit.Server, error) {
	if opts.AuthTokenPath == "" {
		return nil, fmt.Errorf("--auth-token-path required")
	}

	tokenBytes, err := os.ReadFile(opts.AuthTokenPath)
	if err != nil {
		return nil, fmt.Errorf("read auth token: %w", err)
	}
	authToken := strings.TrimSpace(string(tokenBytes))
	if authToken == "" {
		return nil, fmt.Errorf("auth token file is empty: %s", opts.AuthTokenPath)
	}

	daemonBaseURL := opts.DaemonURL
	if daemonBaseURL == "" {
		if opts.Socket == "" {
			return nil, fmt.Errorf("one of --socket or --daemon-url required")
		}
		daemonBaseURL = "http+unix://" + opts.Socket
	}

	rawFamilies := strings.Split(opts.ReviewerFamilyPool, ",")
	nameToFamily := make(map[string]string, len(rawFamilies))
	for i, f := range rawFamilies {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}

		nameToFamily[fmt.Sprintf("flag-%d", i)] = f
	}
	families := audit.ReviewerFamilyPoolFromRegistry(nameToFamily)
	if len(families) == 0 {

		families = audit.ReviewerFamilyPoolFromRegistry(map[string]string{
			"flag-default-0": "anthropic",
			"flag-default-1": "google",
			"flag-default-2": "deepseek",
			"flag-default-3": "local-qwen",
			"flag-default-4": "openai",
		})
	}

	var policy audit.EmptyPoolPolicy
	switch strings.ToLower(strings.TrimSpace(opts.Doctrine)) {
	case "default", "":
		policy = audit.EmptyPoolWarnAndDegrade
	case "max-scope", "capa-firewall", "max_scope", "capa_firewall":
		policy = audit.EmptyPoolHardStop
	default:
		return nil, fmt.Errorf(
			"unknown doctrine %q; valid: max-scope, default, capa-firewall",
			opts.Doctrine,
		)
	}

	minPoolSize := opts.MinPoolSize
	if minPoolSize < 1 {
		minPoolSize = 2
	}

	cfg := audit.ServerConfig{
		DaemonBaseURL:        daemonBaseURL,
		AuthToken:            authToken,
		ReviewerFamilyPool:   families,
		MinPoolSize:          minPoolSize,
		DefaultReviewerModel: opts.DefaultReviewerModel,
		EmptyPoolPolicy:      policy,
	}
	return audit.NewServer(cfg)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "hades-mcp-audit: %v\n", err)
		os.Exit(1)
	}
}

func run() error {

	fs := flag.NewFlagSet("hades-mcp-audit", flag.ContinueOnError)
	socket := fs.String("socket", "/var/run/hades-system/hades-system.sock", "daemon Unix socket path")
	daemonURL := fs.String("daemon-url", "", "daemon HTTP base URL (overrides --socket; used in tests)")
	authTokenPath := fs.String("auth-token-path", "", "path to daemon auth-token file (required)")
	reviewerPool := fs.String("reviewer-pool", "", "comma-separated reviewer family pool (default: anthropic,google,deepseek,local-qwen,openai)")
	minPoolSize := fs.Int("min-pool-size", 2, "minimum disjoint reviewer pool size (invariant)")
	defaultModel := fs.String("default-model", "gemini-2.6-pro", "default reviewer model hint")
	doctrine := fs.String("doctrine", "default", "doctrine mode: max-scope | default | capa-firewall")

	if err := fs.Parse(os.Args[1:]); err != nil {
		return fmt.Errorf("parse flags: %w", err)
	}

	srv, err := buildServer(buildOptions{
		Socket:               *socket,
		DaemonURL:            *daemonURL,
		AuthTokenPath:        *authTokenPath,
		ReviewerFamilyPool:   *reviewerPool,
		MinPoolSize:          *minPoolSize,
		DefaultReviewerModel: *defaultModel,
		Doctrine:             *doctrine,
	})
	if err != nil {
		return err
	}

	return srv.Run(context.Background())
}
