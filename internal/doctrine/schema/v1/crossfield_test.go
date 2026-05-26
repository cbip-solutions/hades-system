package v1_test

import (
	"errors"
	"strings"
	"testing"

	v1 "github.com/cbip-solutions/hades-system/internal/doctrine/schema/v1"
)

// crossfield_test.go — external (v1_test) tests of cross-field invariants
// after reviewer IMPORTANT #4. The validateCrossField implementation is
// package-private; external callers MUST go through Schema.Validate(),
// which is the ONLY public surface that exercises cross-field checks.
//
// This file's tests deliberately drive each invariant via Validate() to
// confirm: (a) the check fires from external callers' perspective, and
// (b) the function-pointer-bypass attack vector closed by IMPORTANT #4
// is not reintroduced (no test reaches in via a swappable var because
// no swappable var exists).

func expectCrossFieldViolation(t *testing.T, s v1.Schema, wantInvariantSubstring string) {
	t.Helper()
	err := s.Validate()
	if err == nil {
		t.Fatalf("expected error; goodSchema with mutation should fail Validate")
	}
	if !errors.Is(err, v1.ErrValidationFailed) {
		t.Errorf("expected ErrValidationFailed; got %v", err)
	}
	var cv *v1.CrossFieldViolation
	if !errors.As(err, &cv) {
		t.Errorf("expected *CrossFieldViolation in chain; got %v", err)
	}
	if !strings.Contains(err.Error(), wantInvariantSubstring) {
		t.Errorf("error message must contain %q; got %q", wantInvariantSubstring, err.Error())
	}
}

func TestCrossField_GoodSchema_PassesViaValidate(t *testing.T) {
	s := goodSchema()
	if err := s.Validate(); err != nil {
		t.Fatalf("good schema must validate; got %v", err)
	}
}

func TestCrossField_MergeWeightsSumNotEqual100(t *testing.T) {
	s := goodSchema()
	s.Merge.ScoringWeights.TestPass = 30
	s.Merge.ScoringWeights.LintPass = 30
	s.Merge.ScoringWeights.Coverage = 30
	expectCrossFieldViolation(t, s, "ScoringWeights")
}

func TestCrossField_HRALayersNonMonotone(t *testing.T) {
	s := goodSchema()
	s.HRA.LayersEnabled = []int{2, 3}
	expectCrossFieldViolation(t, s, "HRALayersMonotoneFromOne")
}

func TestCrossField_HRALayersGap(t *testing.T) {
	s := goodSchema()
	s.HRA.LayersEnabled = []int{1, 3}
	expectCrossFieldViolation(t, s, "HRALayersMonotoneFromOne")
}

func TestCrossField_QuiethoursEqual(t *testing.T) {
	s := goodSchema()
	s.Notifications.QuietHoursStart = "22:00"
	s.Notifications.QuietHoursEnd = "22:00"
	expectCrossFieldViolation(t, s, "QuietHoursStartEndDistinct")
}

func TestCrossField_QuotaExceedsPoolCapacity(t *testing.T) {
	s := goodSchema()
	s.Workforce.MaxDepth = 4
	s.Workforce.MaxWidthPerLayer = 2
	s.Quota.MaxConcurrentTasks = 100
	expectCrossFieldViolation(t, s, "QuotaWithinPoolCapacity")
}

func TestCrossField_TestTiersEmpty(t *testing.T) {
	s := goodSchema()
	s.Gates.TestTiers.Enabled = []string{}
	expectCrossFieldViolation(t, s, "TestTiersNonEmpty")
}

func TestCrossField_HardStopEqualsSoftCheck(t *testing.T) {
	s := goodSchema()
	s.Autonomy.CostDegradation.SoftCheckUSD = 100
	s.Autonomy.CostDegradation.HardStopUSD = 100
	expectCrossFieldViolation(t, s, "CostDegradationHardStopStrictlyGreater")
}

func TestCrossField_MorningBriefWithinTooLarge(t *testing.T) {
	s := goodSchema()
	s.ZenDayCadence.MorningBriefIfWithinHours = 48
	expectCrossFieldViolation(t, s, "MorningBriefWithinAtMostOneDay")
}

func TestCrossField_AppearsInValidate(t *testing.T) {
	s := goodSchema()
	s.Merge.ScoringWeights.TestPass = 50
	s.Merge.ScoringWeights.LintPass = 50
	s.Merge.ScoringWeights.Coverage = 50
	err := s.Validate()
	if err == nil {
		t.Fatal("Validate() must surface cross-field violations")
	}
	if !errors.Is(err, v1.ErrValidationFailed) {
		t.Errorf("expected ErrValidationFailed wrapping cross-field; got %v", err)
	}
	if !strings.Contains(err.Error(), "ScoringWeights") {
		t.Errorf("error should cite ScoringWeights; got %q", err.Error())
	}
}

func TestCrossField_ExternalCannotBypass(t *testing.T) {

	s := goodSchema()
	s.Merge.ScoringWeights.TestPass = 1
	s.Merge.ScoringWeights.LintPass = 0
	s.Merge.ScoringWeights.Coverage = 0
	s.Merge.ScoringWeights.Diff = 0
	s.Merge.ScoringWeights.Duration = 0
	if err := s.Validate(); err == nil {
		t.Fatal("expected error; cross-field check on ScoringWeights sum must fire from external caller")
	}
}
