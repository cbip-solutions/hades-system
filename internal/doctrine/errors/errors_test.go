package errors_test

import (
	stderr "errors"
	"testing"

	doctrineerrors "github.com/cbip-solutions/hades-system/internal/doctrine/errors"
)

func TestSentinelsDistinct(t *testing.T) {
	all := []error{
		doctrineerrors.ErrSchemaVersionUnsupported,
		doctrineerrors.ErrSchemaVersionTooOld,
		doctrineerrors.ErrTightenViolation,
		doctrineerrors.ErrParseFailed,
		doctrineerrors.ErrValidationFailed,
		doctrineerrors.ErrMigrationFailed,
		doctrineerrors.ErrReinforcementTemplateExec,
		doctrineerrors.ErrAmendmentApplyFailed,
		doctrineerrors.ErrDoctrineNotFound,
		doctrineerrors.ErrWatcherStalled,
		doctrineerrors.ErrSchemaVersionDowngradeRejected,
		doctrineerrors.ErrTransverseOverrideAttempted,
	}
	for i, a := range all {
		for j, b := range all {
			if i == j {
				continue
			}
			if stderr.Is(a, b) {
				t.Errorf("sentinels collapsed: %v Is %v", a, b)
			}
		}
	}
}

func TestSentinelsNonEmpty(t *testing.T) {
	all := []error{
		doctrineerrors.ErrSchemaVersionUnsupported,
		doctrineerrors.ErrSchemaVersionTooOld,
		doctrineerrors.ErrTightenViolation,
		doctrineerrors.ErrParseFailed,
		doctrineerrors.ErrValidationFailed,
		doctrineerrors.ErrMigrationFailed,
		doctrineerrors.ErrReinforcementTemplateExec,
		doctrineerrors.ErrAmendmentApplyFailed,
		doctrineerrors.ErrDoctrineNotFound,
		doctrineerrors.ErrWatcherStalled,
		doctrineerrors.ErrSchemaVersionDowngradeRejected,
		doctrineerrors.ErrTransverseOverrideAttempted,
	}
	for _, e := range all {
		if e == nil || e.Error() == "" {
			t.Errorf("nil or empty sentinel: %v", e)
		}
	}
}

func TestTransverseOverrideAttempt_Error_WithFields(t *testing.T) {
	e := &doctrineerrors.TransverseOverrideAttempt{
		Source:  "user-baseline",
		Section: "doctrine_transverse",
		Fields:  []string{"no_stubs", "no_tech_debt"},
	}
	got := e.Error()
	for _, want := range []string{"user-baseline", "doctrine_transverse", "no_stubs", "no_tech_debt", "inv-zen-135"} {
		if !contains(got, want) {
			t.Errorf("Error() = %q; want substring %q", got, want)
		}
	}
}

func TestTransverseOverrideAttempt_Error_WithoutFields(t *testing.T) {
	e := &doctrineerrors.TransverseOverrideAttempt{
		Source:  "user-override",
		Section: "doctrine_transverse",
	}
	got := e.Error()
	for _, want := range []string{"user-override", "doctrine_transverse", "inv-zen-135"} {
		if !contains(got, want) {
			t.Errorf("Error() = %q; want substring %q", got, want)
		}
	}
}

func TestTransverseOverrideAttempt_IsSentinel(t *testing.T) {
	e := &doctrineerrors.TransverseOverrideAttempt{Source: "x", Section: "y"}
	if !stderr.Is(e, doctrineerrors.ErrTransverseOverrideAttempted) {
		t.Error("expected typed error to satisfy errors.Is(*, ErrTransverseOverrideAttempted)")
	}
	if stderr.Is(e, doctrineerrors.ErrTightenViolation) {
		t.Error("typed error must NOT satisfy errors.Is(*, ErrTightenViolation)")
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
