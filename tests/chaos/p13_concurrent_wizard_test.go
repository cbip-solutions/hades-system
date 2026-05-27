// go:build chaos

// Package chaos — p13_concurrent_wizard_test.go (
// IMPORTANT 7 missing-tests completion).
//
// Chaos: concurrent wizard invocations MUST NOT share state — each
// wizard instance is self-contained + thread-safe. The
// NonInteractiveWizard surface is exercised because production
// concurrent invocations would be CI / scripted use rather than TTY-
// driven (operators rarely run two TTY wizards simultaneously).
//
// Per spec §3.1 wizard contract: each Run returns an independent
// WizardAnswers; defaults flow in, no shared mutable state leaks
// between Run calls.
//
// Build tag `chaos` excludes from default CI.
package chaos

import (
	"context"
	"sync"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/onboard"
	"github.com/cbip-solutions/hades-system/internal/onboard/qna"
)

func TestChaos_ConcurrentWizards_NonInteractive(t *testing.T) {
	t.Parallel()
	const N = 10
	var wg sync.WaitGroup
	results := make([]onboard.WizardAnswers, N)
	errs := make([]error, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			w := qna.NewNonInteractiveWizard()
			ans, err := w.Run(context.Background(), onboard.WizardKindGreenfield, onboard.ModeRecommended, onboard.WizardDefaults{})
			results[idx] = ans
			errs[idx] = err
		}(i)
	}
	wg.Wait()
	for i := 0; i < N; i++ {
		if errs[i] != nil {
			t.Errorf("wizard %d err = %v; want nil under non-interactive Recommended mode", i, errs[i])
		}
		if results[i].Kind != onboard.WizardKindGreenfield {
			t.Errorf("wizard %d Kind = %v; want WizardKindGreenfield", i, results[i].Kind)
		}
	}
}

func TestChaos_ConcurrentWizards_DifferentKinds(t *testing.T) {
	t.Parallel()
	kinds := []onboard.WizardKind{
		onboard.WizardKindGlobal,
		onboard.WizardKindGreenfield,
		onboard.WizardKindBrownfield,
	}
	var wg sync.WaitGroup
	results := make([]onboard.WizardAnswers, len(kinds))
	errs := make([]error, len(kinds))
	for i, k := range kinds {
		wg.Add(1)
		go func(idx int, kind onboard.WizardKind) {
			defer wg.Done()
			w := qna.NewNonInteractiveWizard()
			ans, err := w.Run(context.Background(), kind, onboard.ModeRecommended, onboard.WizardDefaults{})
			results[idx] = ans
			errs[idx] = err
		}(i, k)
	}
	wg.Wait()
	for i, expected := range kinds {
		if errs[i] != nil {
			t.Errorf("wizard kind=%v err = %v; want nil", expected, errs[i])
			continue
		}
		if results[i].Kind != expected {
			t.Errorf("wizard %d Kind = %v; want %v (no cross-contamination)", i, results[i].Kind, expected)
		}
	}
}
