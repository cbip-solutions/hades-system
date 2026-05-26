// tests/compliance/inv_zen_178_destructive_fix_confirm_gate_test.go
//
// Spec §8.6 inv-zen-178 compliance test: destructive Fix impls in
// internal/doctor/fix/ MUST be guarded by GuardDestructive, which
// rejects FixModeAutoSafe + FixModeInteractive-without-TTY for any
// fix.Destructive impl returning IsDestructive=true.
//
// location per spec §8.6. The pkg-internal test
// (internal/doctor/fix/destructive_fix_test.go) exhaustively covers the
// truth table; this compliance test asserts the runtime contract
// holds against the actual production destructive Fix impl(s).
package compliance

import (
	"context"
	"errors"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/doctor/check"
	"github.com/cbip-solutions/hades-system/internal/doctor/fix"
)

func TestInvZen178_PluginFormatFixIsDestructive(t *testing.T) {
	t.Parallel()
	f := fix.NewPluginFormatFix(fix.PluginFormatFixConfig{})
	if !f.IsDestructive() {
		t.Errorf("PluginFormatFix.IsDestructive() = false; want true (inv-zen-178 destructive Fix MUST declare destructive intent)")
	}
}

func TestInvZen178_GuardDestructiveRejectsAutoSafe(t *testing.T) {
	t.Parallel()
	f := fix.NewPluginFormatFix(fix.PluginFormatFixConfig{})
	ctx := fix.WithTTY(context.Background(), true)
	err := fix.GuardDestructive(ctx, f, check.FixModeAutoSafe)
	if !errors.Is(err, fix.ErrSkippedAutoSafe) {
		t.Errorf("GuardDestructive(AutoSafe) = %v; want ErrSkippedAutoSafe (inv-zen-178)", err)
	}
}

func TestInvZen178_GuardDestructiveRejectsNonTTYInteractive(t *testing.T) {
	t.Parallel()
	f := fix.NewPluginFormatFix(fix.PluginFormatFixConfig{})
	ctx := fix.WithTTY(context.Background(), false)
	err := fix.GuardDestructive(ctx, f, check.FixModeInteractive)
	if !errors.Is(err, fix.ErrConfirmationRequired) {
		t.Errorf("GuardDestructive(Interactive, non-TTY) = %v; want ErrConfirmationRequired (inv-zen-178)", err)
	}
}

func TestInvZen178_GuardDestructiveAllowsTTYInteractive(t *testing.T) {
	t.Parallel()
	f := fix.NewPluginFormatFix(fix.PluginFormatFixConfig{})
	ctx := fix.WithTTY(context.Background(), true)
	if err := fix.GuardDestructive(ctx, f, check.FixModeInteractive); err != nil {
		t.Errorf("GuardDestructive(Interactive, TTY) = %v; want nil (caller prompts inside Apply)", err)
	}
}

func TestInvZen178_GuardDestructiveAllowsFixModeYes(t *testing.T) {
	t.Parallel()
	f := fix.NewPluginFormatFix(fix.PluginFormatFixConfig{})
	ctx := fix.WithTTY(context.Background(), false)
	if err := fix.GuardDestructive(ctx, f, check.FixModeYes); err != nil {
		t.Errorf("GuardDestructive(Yes) = %v; want nil", err)
	}
}

func TestInvZen178_NonDestructiveBypassesGuard(t *testing.T) {
	t.Parallel()
	nonDestructive := []fix.Destructive{
		&fix.HermesInstallFix{},
		&fix.CuratedMCPFix{},
		&fix.DaemonRunningFix{},
		&fix.SchemaVersionFix{},
	}
	modes := []check.FixMode{
		check.FixModeReadOnly,
		check.FixModeInteractive,
		check.FixModeAutoSafe,
		check.FixModeYes,
	}
	for _, fix := range nonDestructive {
		if fix.IsDestructive() {
			t.Errorf("%s.IsDestructive() = true; expected false (non-destructive)", fix.Name())
			continue
		}
	}
	for _, fixImpl := range nonDestructive {
		for _, mode := range modes {
			if err := guardWrap(fixImpl, mode); err != nil {
				t.Errorf("%s + mode=%v: guard returned %v; want nil (non-destructive)", fixImpl.Name(), mode, err)
			}
		}
	}
}

func guardWrap(d fix.Destructive, mode check.FixMode) error {
	return fix.GuardDestructive(context.Background(), d, mode)
}
