// SPDX-License-Identifier: MIT
// cmd/zen-mcp-sshexec/main.go
//
// zen-mcp-sshexec — ssh-exec MCP binary.
//
// Invoked by OpenCode plugin via stdio transport (Q9 B: stdio canonical;
// invariant: no HTTP/TCP/Unix listen). SSH connections are OUTBOUND
// (client role) — this binary never binds a server listen socket.
//
// The MCP server is implemented in internal/mcp/sshexec/; this file
// handles OS-level wiring only: flag parsing, doctrine loading, allowlist
// resolver construction, SSH agent auth, and graceful signal handling.
//
// Usage (by plugin):
//
// zen-mcp-sshexec --doctrine default --project-id zen-swarm
//
// Usage (operator debug with custom doctrine file):
//
// zen-mcp-sshexec --doctrine-file /path/to/doctrine.toml --project-id zen-swarm
//
// Flags
//
// --doctrine named built-in doctrine: default | max-scope | capa-firewall
// (mutually exclusive with --doctrine-file; default: "default")
// --doctrine-file path to a TOML doctrine file (overrides --doctrine)
// --project-id per-project doctrine identifier (default: "zen-swarm")
// --project-toml path to per-project zenswarm.toml (default: "")
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/cbip-solutions/hades-system/internal/doctrine"
	"github.com/cbip-solutions/hades-system/internal/mcp/sshexec"
)

type buildOptions struct {
	DoctrineName string

	DoctrineFile string

	ProjectID string

	ProjectTOMLPath string
}

func buildServer(opts buildOptions) (*sshexec.Server, error) {

	var doc doctrine.Schema
	if opts.DoctrineFile != "" {
		loaded, err := doctrine.LoadFile(opts.DoctrineFile)
		if err != nil {
			return nil, fmt.Errorf("load doctrine file %q: %w", opts.DoctrineFile, err)
		}
		doc = loaded.Schema
	} else {
		name := opts.DoctrineName
		if name == "" {
			name = "default"
		}
		built, err := doctrine.Builtin(name)
		if err != nil {
			return nil, fmt.Errorf("unknown doctrine %q: %w", name, err)
		}
		doc = built
	}

	resolver := func(project string) (*sshexec.Allowlist, error) {
		tomlPath := opts.ProjectTOMLPath
		return sshexec.ResolveAllowlist(&doc, tomlPath, project)
	}

	auth, _ := sshexec.AgentAuth()

	cfg := sshexec.ServerConfig{
		Component:         "ssh-exec",
		AllowlistResolver: resolver,
		Auth:              auth,
	}

	return sshexec.NewServer(cfg), nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "zen-mcp-sshexec: %v\n", err)
		os.Exit(1)
	}
}

func run() error {

	fs := flag.NewFlagSet("zen-mcp-sshexec", flag.ContinueOnError)
	doctrine_ := fs.String("doctrine", "default", "named built-in doctrine: default | max-scope | capa-firewall")
	doctrineFile := fs.String("doctrine-file", "", "path to TOML doctrine file (overrides --doctrine)")
	projectID := fs.String("project-id", "zen-swarm", "per-project doctrine identifier")
	projectTOML := fs.String("project-toml", "", "path to per-project zenswarm.toml (empty = doctrine-only)")

	if err := fs.Parse(os.Args[1:]); err != nil {
		return fmt.Errorf("parse flags: %w", err)
	}

	srv, err := buildServer(buildOptions{
		DoctrineName:    *doctrine_,
		DoctrineFile:    *doctrineFile,
		ProjectID:       *projectID,
		ProjectTOMLPath: *projectTOML,
	})
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	return srv.Run(ctx)
}
