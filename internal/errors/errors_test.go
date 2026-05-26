package errors

import (
	stderrors "errors"
	"strings"
	"testing"
)

func TestNotImplementedErrorMessage(t *testing.T) {
	e := &NotImplementedError{Plan: 5, PlanRef: "Worktree + apply"}
	got := e.Error()
	if !strings.Contains(got, "Plan 5") {
		t.Errorf("error msg = %q, want substring 'Plan 5'", got)
	}
	if !strings.Contains(got, "Worktree + apply") {
		t.Errorf("error msg = %q, want substring 'Worktree + apply'", got)
	}
}

func TestPredeclaredConstants(t *testing.T) {
	cases := []struct {
		name string
		err  *NotImplementedError
		plan int
	}{
		{"Plan2", ErrNotImplementedPlan2, 2},
		{"Plan3", ErrNotImplementedPlan3, 3},
		{"Plan4", ErrNotImplementedPlan4, 4},
		{"Plan5", ErrNotImplementedPlan5, 5},
		{"Plan6", ErrNotImplementedPlan6, 6},
		{"Plan7", ErrNotImplementedPlan7, 7},
		{"Plan8", ErrNotImplementedPlan8, 8},
		{"Plan9", ErrNotImplementedPlan9, 9},
		{"Plan10", ErrNotImplementedPlan10, 10},
		{"Plan11", ErrNotImplementedPlan11, 11},
		{"Plan12", ErrNotImplementedPlan12, 12},
		{"Plan13", ErrNotImplementedPlan13, 13},
		{"Plan14", ErrNotImplementedPlan14, 14},
		{"Plan15", ErrNotImplementedPlan15, 15},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.err == nil {
				t.Fatalf("%s is nil", c.name)
			}
			if c.err.Plan != c.plan {
				t.Errorf("%s.Plan = %d, want %d", c.name, c.err.Plan, c.plan)
			}
			if c.err.PlanRef == "" {
				t.Errorf("%s.PlanRef is empty", c.name)
			}
		})
	}
}

func TestIsNotImplemented(t *testing.T) {
	if !IsNotImplemented(ErrNotImplementedPlan5) {
		t.Error("IsNotImplemented(ErrNotImplementedPlan5) = false, want true")
	}
	if IsNotImplemented(stderrors.New("other error")) {
		t.Error("IsNotImplemented(unrelated error) = true, want false")
	}
	if IsNotImplemented(nil) {
		t.Error("IsNotImplemented(nil) = true, want false")
	}
}

func TestErrorsIsCompatible(t *testing.T) {

	wrapped := stderrors.Join(stderrors.New("wrapper"), ErrNotImplementedPlan5)
	if !stderrors.Is(wrapped, ErrNotImplementedPlan5) {
		t.Error("errors.Is should find wrapped ErrNotImplementedPlan5")
	}
}
