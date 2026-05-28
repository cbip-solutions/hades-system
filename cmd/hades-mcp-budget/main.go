// SPDX-License-Identifier: MIT
// cmd/hades-mcp-budget/main.go
//
// hades-mcp-budget — budget MCP binary.
//
// Invoked by OpenCode plugin via stdio transport (design choice B: stdio canonical).
// Reads daemon socket path, auth-token-path from flags. On missing or
// invalid config, exits non-zero with a human-readable error (never panics).
//
// Usage (by plugin):
//
// hades-mcp-budget --socket /var/run/hades-system/hades-system.sock --auth-token-path /path/to/token
//
// Usage (operator debug with TCP URL):
//
// hades-mcp-budget --daemon-url http://localhost:4471 --auth-token-path /path/to/token
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/cbip-solutions/hades-system/internal/mcp/budget"
	"github.com/cbip-solutions/hades-system/internal/mcp/client"
)

type buildOptions struct {
	Socket        string
	DaemonURL     string
	AuthTokenPath string
}

func buildServer(opts buildOptions) (*budget.Server, func(), error) {
	if opts.AuthTokenPath == "" {
		return nil, func() {}, fmt.Errorf("--auth-token-path required")
	}

	cfg := client.Config{
		AuthTokenPath: opts.AuthTokenPath,
	}
	if opts.DaemonURL != "" {
		cfg.BaseURL = opts.DaemonURL
	} else {
		cfg.SocketPath = opts.Socket
	}

	c, err := client.New(cfg)
	if err != nil {
		return nil, func() {}, fmt.Errorf("build client: %w", err)
	}

	bc := client.NewBudgetClient(c)
	srv := budget.NewServer(bc)
	return srv, func() { _ = c.Close() }, nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "hades-mcp-budget: %v\n", err)
		os.Exit(1)
	}
}

func run() error {

	fs := flag.NewFlagSet("hades-mcp-budget", flag.ContinueOnError)
	socket := fs.String("socket", client.DefaultSocketPath(), "daemon Unix socket path")
	daemonURL := fs.String("daemon-url", "", "daemon HTTP base URL (overrides --socket; used in tests)")
	authTokenPath := fs.String("auth-token-path", "", "path to daemon auth-token file (required)")
	if err := fs.Parse(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return fmt.Errorf("parse flags: %w", err)
	}

	srv, cleanup, err := buildServer(buildOptions{
		Socket:        *socket,
		DaemonURL:     *daemonURL,
		AuthTokenPath: *authTokenPath,
	})
	if err != nil {
		return err
	}
	defer cleanup()

	return srv.Run()
}
