// SPDX-License-Identifier: MIT
// Package cli — doctor_mcps.go.
package cli

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/cbip-solutions/hades-system/internal/client"
	"github.com/spf13/cobra"
)

func doctorMCPsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcps",
		Short: "MCP children health (research, budget, audit, sshexec)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runOneSection(cmd, "MCPs (Plan 4)", runMCPsChecks)
		},
	}
}

func runMCPsChecks(ctx context.Context, c *client.Client) []CheckResult {
	mcps := []string{"zen-mcp-research", "zen-mcp-budget", "zen-mcp-audit", "zen-mcp-sshexec"}
	out := make([]CheckResult, 0, len(mcps))
	for _, name := range mcps {
		cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		out = append(out, checkMCPBinaryExists(cctx, name))
		cancel()
	}
	return out
}

func checkMCPBinaryExists(_ context.Context, name string) CheckResult {
	candidates := []string{
		filepath.Join("bin", name),
		name,
	}
	for _, p := range candidates {
		if _, err := exec.LookPath(p); err == nil {
			return CheckResult{Name: "mcp." + name + ".binary", Status: "ok",
				Detail: fmt.Sprintf("found at %s", p)}
		}
	}
	return CheckResult{Name: "mcp." + name + ".binary", Status: "fail",
		Detail: "binary not found",
		Hint:   fmt.Sprintf("run `make build-%s` or `make build`", name)}
}
