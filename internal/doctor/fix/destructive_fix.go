// SPDX-License-Identifier: MIT
// Package fix ships the doctor `Fix(ctx, FixMode) error`
// implementations + the destructive-confirmation guard.
//
// Each per-check Fix impl satisfies internal/doctor/check.Check.Fix; the
// concrete check (e.g., internal/doctor/hermes.InstallCheck) holds a
// reference to its corresponding Fix struct and delegates from Check.Fix
// to fix.Apply (which runs GuardDestructive first, then the per-fix
// Apply method).
//
// Boundary: fix package consumes ONLY internal/doctor/check
// + internal/doctor/backup + internal/migrate/writer (for plugin-format
// scaffolder) + stdlib; MUST NOT import internal/store. Per-fix shell-out
// uses os/exec.CommandContext (no daemon HTTP round-trip — fix runs
// locally regardless of daemon state).
//
// review: the destructive-confirm gate is the load-bearing safety
// net. The compile-check guard `_ Destructive = (*XyzFix)(nil)` in every
// destructive Fix impl file is the canonical enforcement mechanism (per
// invariant). The runtime gate (GuardDestructive) is the second line.
// The third line is the operator backup-before-modify
// shipped in internal/doctor/backup (F4).
package fix

import (
	"context"
	"errors"
	"fmt"

	"github.com/cbip-solutions/hades-system/internal/doctor/check"
)

// Destructive is the interface a destructive Fix MUST satisfy (per
// invariant). The compile-check guard `_ Destructive = (*XyzFix)(nil)`
// in each destructive Fix impl file enforces this at build time.
//
// destructive Fix impls in F3: PluginFormatFix (deletes plugin
// remnants + restores fresh). Future destructive Fix additions
// MUST also declare the interface guard.
type Destructive interface {
	Name() string

	IsDestructive() bool
}

var (
	ErrConfirmationRequired = errors.New("fix: destructive operation requires --fix --yes or interactive TTY confirmation")

	ErrSkippedAutoSafe = errors.New("fix: destructive operation skipped in --auto-safe mode")
)

type ttyContextKey struct{}

func WithTTY(ctx context.Context, tty bool) context.Context {
	return context.WithValue(ctx, ttyContextKey{}, tty)
}

func IsTTY(ctx context.Context) bool {
	v, ok := ctx.Value(ttyContextKey{}).(bool)
	return ok && v
}

func GuardDestructive(ctx context.Context, fix Destructive, mode check.FixMode) error {
	if !fix.IsDestructive() {
		return nil
	}
	switch mode {
	case check.FixModeReadOnly, check.FixModeYes:
		return nil
	case check.FixModeAutoSafe:
		return fmt.Errorf("%w: %s", ErrSkippedAutoSafe, fix.Name())
	case check.FixModeInteractive:
		if IsTTY(ctx) {
			return nil
		}
		return fmt.Errorf("%w: %s (non-TTY context; re-run with --fix --yes for CI or in a TTY)", ErrConfirmationRequired, fix.Name())
	default:
		return fmt.Errorf("fix: unknown FixMode %v (expected ReadOnly/Interactive/AutoSafe/Yes)", mode)
	}
}
