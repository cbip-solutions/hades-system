// SPDX-License-Identifier: MIT
// Package fix — hermes_install_fix.go ships the Fix impl for the
// hermes.install check (inv-zen-175). Non-destructive: shells out to
// `brew install hermes-agent` on macOS; surfaces manual install guidance
// on other platforms.
package fix

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"

	"github.com/cbip-solutions/hades-system/internal/doctor/check"
)

type HermesInstallFix struct{}

// Name returns the canonical Fix name. MUST match the hermes.install
// Check.Name() for catalog lookup.
func (h *HermesInstallFix) Name() string { return "hermes.install" }

func (h *HermesInstallFix) IsDestructive() bool { return false }

func (h *HermesInstallFix) Apply(ctx context.Context, mode check.FixMode) error {
	if mode == check.FixModeReadOnly {
		return errors.New("fix: read-only mode; run `brew install hermes-agent` to install (macOS)")
	}
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("fix: auto-install unsupported on %s; install hermes manually from https://hermes-agent.dev/install", runtime.GOOS)
	}
	if _, err := exec.LookPath("brew"); err != nil {
		return fmt.Errorf("fix: brew not found on PATH; install Homebrew (https://brew.sh) then retry: %w", err)
	}
	cmd := exec.CommandContext(ctx, "brew", "install", "hermes-agent")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("fix: brew install failed: %w; output:\n%s", err, string(out))
	}
	return nil
}

var (
	_ Destructive = (*HermesInstallFix)(nil)
	_ Applier     = (*HermesInstallFix)(nil)
)
