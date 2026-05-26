package fix_test

import (
	"context"
	"errors"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/doctor/check"
	"github.com/cbip-solutions/hades-system/internal/doctor/fix"
)

func TestDestructiveFixRejectedInNonInteractiveWithoutYes(t *testing.T) {
	destructive := &fakeDestructiveFix{}
	ctx := fix.WithTTY(context.Background(), false)

	err := fix.GuardDestructive(ctx, destructive, check.FixModeInteractive)
	if !errors.Is(err, fix.ErrConfirmationRequired) {
		t.Errorf("FixModeInteractive non-TTY: err=%v, want ErrConfirmationRequired", err)
	}
}

func TestDestructiveFixAllowedWithExplicitYes(t *testing.T) {
	destructive := &fakeDestructiveFix{}
	ctx := fix.WithTTY(context.Background(), false)
	if err := fix.GuardDestructive(ctx, destructive, check.FixModeYes); err != nil {
		t.Errorf("FixModeYes guard returned err: %v", err)
	}
}

func TestDestructiveFixAllowedInInteractiveTTY(t *testing.T) {
	destructive := &fakeDestructiveFix{}
	ctx := fix.WithTTY(context.Background(), true)
	if err := fix.GuardDestructive(ctx, destructive, check.FixModeInteractive); err != nil {
		t.Errorf("TTY+Interactive guard returned err: %v", err)
	}
}

func TestNonDestructiveBypassesGuard(t *testing.T) {
	nonDestructive := &fakeNonDestructiveFix{}
	for _, tty := range []bool{true, false} {
		ctx := fix.WithTTY(context.Background(), tty)
		for _, mode := range []check.FixMode{
			check.FixModeReadOnly,
			check.FixModeInteractive,
			check.FixModeAutoSafe,
			check.FixModeYes,
		} {
			if err := fix.GuardDestructive(ctx, nonDestructive, mode); err != nil {
				t.Errorf("non-destructive: TTY=%v mode=%v: err=%v", tty, mode, err)
			}
		}
	}
}

func TestDestructiveFixAutoSafeRejected(t *testing.T) {
	destructive := &fakeDestructiveFix{}
	ctx := fix.WithTTY(context.Background(), true)
	err := fix.GuardDestructive(ctx, destructive, check.FixModeAutoSafe)
	if !errors.Is(err, fix.ErrSkippedAutoSafe) {
		t.Errorf("FixModeAutoSafe + destructive: err=%v, want ErrSkippedAutoSafe", err)
	}
}

func TestDestructiveReadOnlyAllowed(t *testing.T) {
	destructive := &fakeDestructiveFix{}
	ctx := fix.WithTTY(context.Background(), false)
	if err := fix.GuardDestructive(ctx, destructive, check.FixModeReadOnly); err != nil {
		t.Errorf("FixModeReadOnly + destructive: err=%v, want nil", err)
	}
}

func TestUnknownFixModeRejected(t *testing.T) {
	destructive := &fakeDestructiveFix{}
	ctx := fix.WithTTY(context.Background(), true)
	err := fix.GuardDestructive(ctx, destructive, check.FixMode(99))
	if err == nil {
		t.Errorf("unknown FixMode: err=nil, want non-nil")
	}
}

func TestIsTTYDefaultsFalse(t *testing.T) {
	if fix.IsTTY(context.Background()) {
		t.Errorf("IsTTY without WithTTY annotation: true, want false")
	}
}

func TestIsTTYAfterWithTTYTrue(t *testing.T) {
	ctx := fix.WithTTY(context.Background(), true)
	if !fix.IsTTY(ctx) {
		t.Errorf("IsTTY after WithTTY(true): false, want true")
	}
}

var (
	_ fix.Destructive = (*fakeDestructiveFix)(nil)
	_ fix.Destructive = (*fakeNonDestructiveFix)(nil)
)

type fakeDestructiveFix struct{}

func (f *fakeDestructiveFix) Name() string        { return "fake.destructive" }
func (f *fakeDestructiveFix) IsDestructive() bool { return true }

type fakeNonDestructiveFix struct{}

func (f *fakeNonDestructiveFix) Name() string        { return "fake.non-destructive" }
func (f *fakeNonDestructiveFix) IsDestructive() bool { return false }
