package integration_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/onboard"
	"github.com/cbip-solutions/hades-system/internal/onboard/qna"
	"github.com/cbip-solutions/hades-system/tests/testhelpers"
)

func TestCustomizePathPublicAPIDocumentsNonInteractive(t *testing.T) {
	td := testhelpers.NewOnboardTestDaemon(t)
	defer td.Stop()

	w := qna.NewWizard(qna.Options{Interactive: true})
	defaults := onboard.WizardDefaults{
		LLMProvider:      "anthropic-paygo",
		Doctrine:         "max-scope",
		InstallHermes:    true,
		EnableAuditChain: true,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := w.Run(ctx, onboard.WizardKindGlobal, onboard.ModeCustomize, defaults)
	if !errors.Is(err, onboard.ErrNonInteractive) {
		t.Fatalf("Run(ModeCustomize) in non-TTY runtime: want ErrNonInteractive, got %v", err)
	}
}

func TestCustomizePathSimulatorDrivesBubbleteaWiring(t *testing.T) {
	td := testhelpers.NewOnboardTestDaemon(t)
	defer td.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sim := testhelpers.NewTTYSimulatorWithContext(t, ctx)

	w := qna.NewWizard(qna.Options{
		Interactive: true,
		TTY:         true,
		Input:       sim.InputReader(),
		Output:      sim.OutputWriter(),
	})
	defaults := onboard.WizardDefaults{
		LLMProvider:      "anthropic-paygo",
		Doctrine:         "max-scope",
		InstallHermes:    true,
		EnableAuditChain: true,
	}

	type runResult struct {
		answers onboard.WizardAnswers
		err     error
	}
	resCh := make(chan runResult, 1)
	go func() {
		ans, err := w.Run(ctx, onboard.WizardKindGlobal, onboard.ModeCustomize, defaults)
		resCh <- runResult{answers: ans, err: err}
	}()

	time.Sleep(50 * time.Millisecond)

	sim.WriteKey(testhelpers.KeyEnter)
	time.Sleep(20 * time.Millisecond)

	sim.WriteKey(testhelpers.KeyDown)
	time.Sleep(20 * time.Millisecond)
	sim.WriteKey(testhelpers.KeyEnter)
	time.Sleep(20 * time.Millisecond)

	sim.WriteKey(testhelpers.KeyEnter)
	time.Sleep(20 * time.Millisecond)

	sim.WriteKey(testhelpers.KeyEnter)
	time.Sleep(20 * time.Millisecond)

	sim.WriteKey(testhelpers.KeyEnter)

	var res runResult
	select {
	case res = <-resCh:
	case <-time.After(4 * time.Second):
		t.Fatalf("wizard Run did not return within 4s; likely stuck on stdin read")
	}

	if res.err != nil {
		t.Fatalf("Run(ModeCustomize) with simulator: %v", res.err)
	}

	if res.answers.Kind != onboard.WizardKindGlobal {
		t.Errorf("answers.Kind = %v, want WizardKindGlobal", res.answers.Kind)
	}
	if res.answers.Mode != onboard.ModeCustomize {
		t.Errorf("answers.Mode = %v, want ModeCustomize", res.answers.Mode)
	}
	if res.answers.LLMProvider != "anthropic-paygo" {
		t.Errorf("answers.LLMProvider = %q, want anthropic-paygo", res.answers.LLMProvider)
	}
	if res.answers.Doctrine != "default" {
		t.Errorf("answers.Doctrine = %q, want default (cursor-down once)", res.answers.Doctrine)
	}
	if !res.answers.InstallHermes {
		t.Errorf("answers.InstallHermes = false, want true (y selection)")
	}
	if !res.answers.EnableAuditChain {
		t.Errorf("answers.EnableAuditChain = false, want true (y selection)")
	}

	if out := sim.Output(); !strings.Contains(out, "/5]") {
		t.Errorf("simulator captured no step counter in output; got %q", out)
	}
}

func TestCustomizePathSimulatorEscCancels(t *testing.T) {
	td := testhelpers.NewOnboardTestDaemon(t)
	defer td.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sim := testhelpers.NewTTYSimulatorWithContext(t, ctx)

	w := qna.NewWizard(qna.Options{
		Interactive: true,
		TTY:         true,
		Input:       sim.InputReader(),
		Output:      sim.OutputWriter(),
	})

	type runResult struct {
		err error
	}
	resCh := make(chan runResult, 1)
	go func() {
		_, err := w.Run(ctx, onboard.WizardKindGlobal, onboard.ModeCustomize, onboard.WizardDefaults{})
		resCh <- runResult{err: err}
	}()

	time.Sleep(50 * time.Millisecond)
	sim.WriteKey(testhelpers.KeyEsc)

	select {
	case res := <-resCh:
		if !errors.Is(res.err, onboard.ErrUserCanceled) {
			t.Errorf("expected ErrUserCanceled from in-wizard ESC, got %v", res.err)
		}
	case <-time.After(4 * time.Second):
		t.Fatalf("Run did not return within 4s after ESC; wizard hung")
	}
}

func TestCustomizePathSimulatorHonorsCtxCancel(t *testing.T) {
	td := testhelpers.NewOnboardTestDaemon(t)
	defer td.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	sim := testhelpers.NewTTYSimulatorWithContext(t, ctx)

	w := qna.NewWizard(qna.Options{
		Interactive: true,
		TTY:         true,
		Input:       sim.InputReader(),
		Output:      sim.OutputWriter(),
	})
	_, err := w.Run(ctx, onboard.WizardKindGlobal, onboard.ModeCustomize, onboard.WizardDefaults{})
	if err == nil {
		t.Fatal("Run with canceled ctx: expected error, got nil")
	}
	if !errors.Is(err, onboard.ErrUserCanceled) && !errors.Is(err, context.Canceled) {
		t.Errorf("expected ErrUserCanceled or context.Canceled, got %v", err)
	}
}
