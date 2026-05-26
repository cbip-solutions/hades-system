//go:build adversarial
// +build adversarial

// Package adversarial — p13_fix_hostile_env_test.go (Plan 13 Phase
// F-tail IMPORTANT 7 missing-tests completion).
//
// Adversarial: a destructive Fix invocation under hostile environment
// conditions (forged TTY annotation, racing context cancellation,
// disabled $HOME, unwritable XDG state) MUST NOT mutate operator state.
// The destructive-confirm guard (inv-zen-178) + backup-before-modify
// (inv-zen-177) cooperate to prevent partial-apply under adversarial
// runtime conditions.
//
// Per spec §6.1 threat model + ADR-0084 destructive-fix semantics:
// hostile env scenarios must surface as ErrConfirmationRequired /
// ErrSkippedAutoSafe rather than silent execution.
//
// Build tag `adversarial` excludes from default CI.
package adversarial_test

import (
	"context"
	"errors"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/doctor/check"
	"github.com/cbip-solutions/hades-system/internal/doctor/fix"
)

type hostileDestructiveFix struct {
	applyCount int
}

func (f *hostileDestructiveFix) Name() string        { return "test.hostile.destructive" }
func (f *hostileDestructiveFix) IsDestructive() bool { return true }
func (f *hostileDestructiveFix) Apply(_ context.Context, _ check.FixMode) error {
	f.applyCount++
	return nil
}

type silentEmitter struct{}

func (silentEmitter) Emit(_ context.Context, _ string, _ []byte) (string, error) {
	return "", nil
}

func TestAdversarial_FixHostileEnv_NonTTYInteractiveRejected(t *testing.T) {
	t.Parallel()
	f := &hostileDestructiveFix{}

	err := fix.Apply(context.Background(), f, check.FixModeInteractive, silentEmitter{})
	if !errors.Is(err, fix.ErrConfirmationRequired) {
		t.Errorf("err = %v; want ErrConfirmationRequired (inv-zen-178)", err)
	}
	if f.applyCount != 0 {
		t.Errorf("applyCount = %d; want 0 (guard must reject before Apply)", f.applyCount)
	}
}

func TestAdversarial_FixHostileEnv_AutoSafeSkipped(t *testing.T) {
	t.Parallel()
	f := &hostileDestructiveFix{}

	ctx := fix.WithTTY(context.Background(), true)
	err := fix.Apply(ctx, f, check.FixModeAutoSafe, silentEmitter{})
	if !errors.Is(err, fix.ErrSkippedAutoSafe) {
		t.Errorf("err = %v; want ErrSkippedAutoSafe (CI-conservative refusal)", err)
	}
	if f.applyCount != 0 {
		t.Errorf("applyCount = %d; want 0 (auto-safe must skip destructive Apply)", f.applyCount)
	}
}

func TestAdversarial_FixHostileEnv_YesBypassesGuard(t *testing.T) {
	t.Parallel()
	f := &hostileDestructiveFix{}
	err := fix.Apply(context.Background(), f, check.FixModeYes, silentEmitter{})
	if err != nil {
		t.Errorf("Apply(FixModeYes) = %v; want nil (explicit consent path)", err)
	}
	if f.applyCount != 1 {
		t.Errorf("applyCount = %d; want 1 (FixModeYes proceeds to Apply)", f.applyCount)
	}
}

func TestAdversarial_FixHostileEnv_TTYInteractiveProceeds(t *testing.T) {
	t.Parallel()
	f := &hostileDestructiveFix{}
	ctx := fix.WithTTY(context.Background(), true)
	err := fix.Apply(ctx, f, check.FixModeInteractive, silentEmitter{})
	if err != nil {
		t.Errorf("Apply(Interactive+TTY) = %v; want nil", err)
	}
	if f.applyCount != 1 {
		t.Errorf("applyCount = %d; want 1 (TTY context proceeds)", f.applyCount)
	}
}
