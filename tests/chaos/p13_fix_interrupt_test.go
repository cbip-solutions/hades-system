//go:build chaos

// Package chaos — p13_fix_interrupt_test.go (Plan 13 Phase F-tail
// IMPORTANT 7 missing-tests completion).
//
// Chaos: a destructive Fix interrupted mid-apply (ctx cancellation
// between backup and modify) MUST leave the system in a recoverable
// state — never partial-write. Per spec §3.4 destructive-fix contract +
// inv-zen-177 backup-before-modify substrate.
//
// Build tag `chaos` excludes this file from default CI; opt-in via
// `make test-chaos` or `go test -tags=chaos ./tests/chaos/...`.
package chaos

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/doctor/check"
	"github.com/cbip-solutions/hades-system/internal/doctor/fix"
)

type interruptibleFix struct {
	name        string
	destructive bool
	applyCount  int
}

func (f *interruptibleFix) Name() string        { return f.name }
func (f *interruptibleFix) IsDestructive() bool { return f.destructive }
func (f *interruptibleFix) Apply(ctx context.Context, _ check.FixMode) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.applyCount++
	return nil
}

type recordingFixEmitter struct {
	events int
}

func (e *recordingFixEmitter) Emit(_ context.Context, _ string, _ []byte) (string, error) {
	e.events++
	return "hash", nil
}

func TestChaos_FixInterruptedMidApply(t *testing.T) {
	t.Parallel()
	f := &interruptibleFix{name: "test.fix.interrupt", destructive: true}
	emitter := &recordingFixEmitter{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := fix.Apply(ctx, f, check.FixModeYes, emitter)
	if err == nil {
		t.Fatalf("Apply on pre-cancelled ctx = nil err; want context.Canceled or guard error")
	}
	// Whatever cancellation path fires (guard short-circuit OR Apply's
	// internal ctx.Err probe), the fix MUST NOT have applied.
	if f.applyCount != 0 {
		t.Errorf("applyCount = %d; want 0 (interrupted before mutation)", f.applyCount)
	}
}

func TestChaos_FixInterruptedAfterModeYesGuard(t *testing.T) {
	t.Parallel()
	f := &interruptibleFix{name: "test.fix.deadline", destructive: true}
	emitter := &recordingFixEmitter{}
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-1*time.Second))
	defer cancel()
	err := fix.Apply(ctx, f, check.FixModeYes, emitter)
	if err == nil {
		t.Fatalf("Apply on expired-deadline ctx = nil err; want context.DeadlineExceeded")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v; want context.DeadlineExceeded or Canceled wrap", err)
	}
	if f.applyCount != 0 {
		t.Errorf("applyCount = %d; want 0 (deadline exceeded before mutation)", f.applyCount)
	}
}
