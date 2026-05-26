// SPDX-License-Identifier: MIT
// Package fix — daemon_running_fix.go ships the Fix impl for the
// daemon.running check (Plan 1+ daemon health).
//
// Non-destructive: starts the daemon idempotently via `zen daemon start`.
// Existing running daemon → no-op; stopped daemon → reachable post-fix.
package fix

import (
	"context"
	"errors"
	"fmt"
	"os/exec"

	"github.com/cbip-solutions/hades-system/internal/doctor/check"
)

type DaemonRunningFix struct{}

func (d *DaemonRunningFix) Name() string { return "daemon.running" }

func (d *DaemonRunningFix) IsDestructive() bool { return false }

func (d *DaemonRunningFix) Apply(ctx context.Context, mode check.FixMode) error {
	if mode == check.FixModeReadOnly {
		return errors.New("fix: read-only mode; run `zen daemon start` to start daemon")
	}
	if _, err := exec.LookPath("zen"); err != nil {
		return fmt.Errorf("fix: `zen` binary not on PATH: %w", err)
	}
	cmd := exec.CommandContext(ctx, "zen", "daemon", "start")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("fix: zen daemon start failed: %w; output:\n%s", err, string(out))
	}
	return nil
}

var (
	_ Destructive = (*DaemonRunningFix)(nil)
	_ Applier     = (*DaemonRunningFix)(nil)
)
