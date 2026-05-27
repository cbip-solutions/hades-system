// SPDX-License-Identifier: MIT
// Package fix — bypass_config_fix.go ships the Fix impl for the
// bypass.config check.
//
// Non-destructive: delegates to the existing `bin/zen bypass extract-config`
// interactive command. The extract-config command requires operator
// login (Anthropic OAuth) so AutoSafe + Interactive both require operator
// presence; --yes propagates the operator's authorization through.
package fix

import (
	"context"
	"errors"
	"fmt"
	"os/exec"

	"github.com/cbip-solutions/hades-system/internal/doctor/check"
)

type BypassConfigFix struct{}

func (b *BypassConfigFix) Name() string { return "bypass.config" }

func (b *BypassConfigFix) IsDestructive() bool { return false }

func (b *BypassConfigFix) Apply(ctx context.Context, mode check.FixMode) error {
	if mode == check.FixModeReadOnly {
		return errors.New("fix: read-only mode; run `bin/zen bypass extract-config` interactively (requires operator login)")
	}
	if _, err := exec.LookPath("zen"); err != nil {
		return fmt.Errorf("fix: `zen` binary not on PATH; build via `make build`: %w", err)
	}
	cmd := exec.CommandContext(ctx, "zen", "bypass", "extract-config")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("fix: zen bypass extract-config failed: %w; output:\n%s", err, string(out))
	}
	return nil
}

var (
	_ Destructive = (*BypassConfigFix)(nil)
	_ Applier     = (*BypassConfigFix)(nil)
)
