// go:build adversarial
//go:build adversarial
// +build adversarial

// Package adversarial — p13_non_interactive_coverage_test.go (
// IMPORTANT 7 missing-tests completion).
//
// Adversarial: a wizard invoked in non-interactive context MUST reject
// any mode that requires operator input. Per spec §3.1 contract:
// ModeCustomize is operator-driven (Path 3) and CANNOT proceed without
// a TTY; non-interactive Customize MUST surface ErrNonInteractive.
//
// Per Q3=D hybrid wizard contract: the non-interactive surface is the
// CI / scripted execution path; defaults-only Recommended works, but
// Customize requires interactivity. This adversarial test guards
// against accidentally permissive non-interactive mode handling that
// could silently default Customize → Recommended (operator surprise).
//
// Build tag `adversarial` excludes from default CI.
package adversarial_test

import (
	"context"
	"errors"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/onboard"
	"github.com/cbip-solutions/hades-system/internal/onboard/qna"
)

func TestAdversarial_NonInteractive_CustomizeRejected(t *testing.T) {
	t.Parallel()
	w := qna.NewNonInteractiveWizard()
	_, err := w.Run(context.Background(), onboard.WizardKindGreenfield, onboard.ModeCustomize, onboard.WizardDefaults{})
	if err == nil {
		t.Fatalf("Run(Customize) on NonInteractiveWizard = nil err; want ErrNonInteractive (spec §3.1)")
	}
	if !errors.Is(err, onboard.ErrNonInteractive) {
		t.Errorf("err = %v; want ErrNonInteractive wrap (spec §3.1)", err)
	}
}

func TestAdversarial_NonInteractive_RecommendedProceeds(t *testing.T) {
	t.Parallel()
	kinds := []onboard.WizardKind{
		onboard.WizardKindGlobal,
		onboard.WizardKindGreenfield,
		onboard.WizardKindBrownfield,
	}
	for _, k := range kinds {
		k := k
		t.Run(k.String(), func(t *testing.T) {
			w := qna.NewNonInteractiveWizard()
			ans, err := w.Run(context.Background(), k, onboard.ModeRecommended, onboard.WizardDefaults{})
			if err != nil {
				t.Errorf("Run(%v, Recommended) = %v; want nil (CI happy path)", k, err)
			}
			if ans.Kind != k {
				t.Errorf("answers.Kind = %v; want %v", ans.Kind, k)
			}
		})
	}
}

func TestAdversarial_NonInteractive_UnknownKindRejected(t *testing.T) {
	t.Parallel()
	w := qna.NewNonInteractiveWizard()
	_, err := w.Run(context.Background(), onboard.WizardKindUnknown, onboard.ModeRecommended, onboard.WizardDefaults{})
	if err == nil {
		t.Fatalf("Run(Unknown kind) = nil err; want ErrUnknownWizardKind")
	}
	if !errors.Is(err, onboard.ErrUnknownWizardKind) {
		t.Errorf("err = %v; want ErrUnknownWizardKind wrap", err)
	}
}

func TestAdversarial_NonInteractive_UnknownModeRejected(t *testing.T) {
	t.Parallel()
	w := qna.NewNonInteractiveWizard()
	_, err := w.Run(context.Background(), onboard.WizardKindGreenfield, onboard.ModeUnknown, onboard.WizardDefaults{})
	if err == nil {
		t.Fatalf("Run(Unknown mode) = nil err; want ErrUnknownWizardMode")
	}
	if !errors.Is(err, onboard.ErrUnknownWizardMode) {
		t.Errorf("err = %v; want ErrUnknownWizardMode wrap", err)
	}
}

func TestAdversarial_NonInteractive_CancelledCtxRejected(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w := qna.NewNonInteractiveWizard()
	_, err := w.Run(ctx, onboard.WizardKindGreenfield, onboard.ModeRecommended, onboard.WizardDefaults{})
	if err == nil {
		t.Fatalf("Run(cancelled ctx) = nil err; want context-cancellation error")
	}

	if !errors.Is(err, onboard.ErrUserCanceled) && !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v; want ErrUserCanceled or context.Canceled wrap", err)
	}
}
