// SPDX-License-Identifier: MIT
// Package cli — daemon_restart_mcp.go
//
// manual restart of one MCP child managed by the daemon's mcpgateway.
// Use this when a stuck child needs a manual nudge; daemon's normal
// health-probe + rate-limited auto-restart governor handles the
// majority of cases automatically (per inv-zen-168).
//
// Closed set of valid MCP names (matches Phase A's tool registry):
//   - research     (code_graph + research MCP)
//   - budget       (4-axis budget MCP)
//   - audit        (Tessera-anchored audit MCP)
//   - sshexec      (Plan 6 SSH executor)
//   - codegen      (Plan 5 codegen MCP)
//
// caronte is in-process (Plan 19) — no restart-mcp entry; use 'zen doctor caronte' for engine health.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cbip-solutions/hades-system/internal/client"
	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
)

const mcpRestartTimeout = 30 * time.Second

var validMCPNames = map[string]bool{
	"research": true,
	"budget":   true,
	"audit":    true,
	"sshexec":  true,
	"codegen":  true,
}

var validMCPNamesSorted = []string{"audit", "budget", "codegen", "research", "sshexec"}

type MCPRestartClient interface {
	MCPRestart(ctx context.Context, name string) (*client.MCPRestartResponse, error)
}

type MCPRestartClientFactory func(cmd *cobra.Command) MCPRestartClient

type productionMCPRestartClient struct {
	c *client.Client
}

func (p *productionMCPRestartClient) MCPRestart(ctx context.Context, name string) (*client.MCPRestartResponse, error) {
	return p.c.MCPRestart(ctx, name)
}

type MCPRestartFlags struct {
	Name string
}

func NewDaemonRestartMCPCmd(factory MCPRestartClientFactory) *cobra.Command {
	flags := MCPRestartFlags{}
	cmd := &cobra.Command{
		Use:   "restart-mcp <name>",
		Short: "Manually restart one MCP child (research|budget|audit|sshexec|codegen)",
		Long: `Trigger a manual restart of one MCP child managed by the daemon's
mcpgateway. Use this when a stuck child needs a nudge; the daemon's
normal health-probe + rate-limited auto-restart governor (inv-zen-168)
handles most cases automatically.

The daemon enforces a per-child restart rate-limit (3 restarts in 5
minutes). Hitting the limit returns 429; the CLI surfaces a recoverable
error pointing at the rate-limit window.

Note: caronte is in-process (Plan 19) — use 'zen doctor caronte' for
engine health instead of restart-mcp.`,
		Example: `  # Restart the research MCP
  zen daemon restart-mcp research

  # Restart the budget MCP
  zen daemon restart-mcp budget`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				flags.Name = args[0]
			}
			c := factory(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), mcpRestartTimeout)
			defer cancel()
			return RunDaemonRestartMCP(ctx, c, flags, cmd.OutOrStdout())
		},
	}
	return cmd
}

func NewDaemonRestartMCPCmdProd() *cobra.Command {
	return NewDaemonRestartMCPCmd(func(cmd *cobra.Command) MCPRestartClient {
		return &productionMCPRestartClient{c: newClientFromCmd(cmd)}
	})
}

func RunDaemonRestartMCP(ctx context.Context, c MCPRestartClient, flags MCPRestartFlags, w io.Writer) error {
	name := strings.TrimSpace(flags.Name)
	if name == "" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("daemon restart-mcp: <name> is required (one of %s)",
			strings.Join(validMCPNamesSorted, "|")))
	}
	if !validMCPNames[name] {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("daemon restart-mcp: unknown MCP %q (valid: %s)",
			name, strings.Join(validMCPNamesSorted, "|")))
	}
	resp, err := c.MCPRestart(ctx, name)
	if err != nil {
		return classifyMCPRestartError(err)
	}
	fmt.Fprintf(w, "restart-mcp %s: status=%s duration_ms=%d\n", resp.Name, resp.Status, resp.DurationMs)
	return nil
}

func classifyMCPRestartError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrRecoverable) {
		return err
	}
	if client.IsHTTPStatus(err, http.StatusTooManyRequests) {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverableWrap(err,
			"restart-mcp: rate-limit hit (per inv-zen-168 — 3 restarts in 5min); wait and retry"))
	}
	if client.IsHTTPStatus(err, http.StatusUnprocessableEntity) {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverableWrap(err, "restart-mcp: daemon rejected request"))
	}
	return ierrors.Wrap(ierrors.Code("daemon.unreachable"), fmt.Errorf("restart-mcp: %w", err))
}
