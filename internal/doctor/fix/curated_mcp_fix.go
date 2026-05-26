// SPDX-License-Identifier: MIT
// Package fix — curated_mcp_fix.go ships the Fix impl for the
// mcp.curated-availability check.
//
// Non-destructive: installs missing curated MCPs via package-manager-
// specific shell-out (npm install -g / brew install / pip install --user).
// Idempotent re-running against an already-installed MCP is a no-op
// per-manager.
package fix

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/cbip-solutions/hades-system/internal/doctor/check"
)

type MCPInstallSpec struct {
	Name           string
	PackageManager string
	PackageName    string
}

type CuratedMCPFix struct {
	MissingMCPs []MCPInstallSpec
}

func (c *CuratedMCPFix) Name() string { return "mcp.curated-availability" }

func (c *CuratedMCPFix) IsDestructive() bool { return false }

func (c *CuratedMCPFix) Apply(ctx context.Context, mode check.FixMode) error {
	if mode == check.FixModeReadOnly {
		if len(c.MissingMCPs) == 0 {
			return errors.New("fix: read-only mode; no missing MCPs to install")
		}
		var lines []string
		for _, m := range c.MissingMCPs {
			lines = append(lines, fmt.Sprintf("  %s install %s", c.installCommand(m), m.PackageName))
		}
		return errors.New("fix: read-only mode; run:\n" + strings.Join(lines, "\n"))
	}
	for _, m := range c.MissingMCPs {
		cmd, err := c.buildCmd(ctx, m)
		if err != nil {
			return err
		}
		out, runErr := cmd.CombinedOutput()
		if runErr != nil {
			return fmt.Errorf("fix: install %s via %s failed: %w; output:\n%s", m.Name, m.PackageManager, runErr, string(out))
		}
	}
	return nil
}

func (c *CuratedMCPFix) installCommand(m MCPInstallSpec) string {
	switch m.PackageManager {
	case "npm":
		return "npm install -g"
	case "brew":
		return "brew install"
	case "pip":
		return "pip install --user"
	default:
		return m.PackageManager
	}
}

func (c *CuratedMCPFix) buildCmd(ctx context.Context, m MCPInstallSpec) (*exec.Cmd, error) {
	switch m.PackageManager {
	case "npm":
		return exec.CommandContext(ctx, "npm", "install", "-g", m.PackageName), nil
	case "brew":
		return exec.CommandContext(ctx, "brew", "install", m.PackageName), nil
	case "pip":
		return exec.CommandContext(ctx, "pip", "install", "--user", m.PackageName), nil
	default:
		return nil, fmt.Errorf("fix: unsupported package manager %q for %s", m.PackageManager, m.Name)
	}
}

var (
	_ Destructive = (*CuratedMCPFix)(nil)
	_ Applier     = (*CuratedMCPFix)(nil)
)
