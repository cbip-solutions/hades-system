// SPDX-License-Identifier: MIT
// Package errors centralises sentinel error values used across hades-system.
//
// The most important export is the family of ErrNotImplementedPlanN
// constants — every stub created during HADES design (and replaced by subsequent
// plans) returns one of these. This lets `hades doctor --verbose` count
// references and report implementation progress.
//
// Using errors.Is(err, ErrNotImplementedPlan5) tests for a specific stub.
// Using IsNotImplemented(err) tests for any stub of any plan.
package errors

import (
	stderrors "errors"
	"fmt"
)

type NotImplementedError struct {
	Plan int

	PlanRef string
}

func (e *NotImplementedError) Error() string {
	return fmt.Sprintf("not implemented; pending Plan %d (%s)", e.Plan, e.PlanRef)
}

func (e *NotImplementedError) Is(target error) bool {
	t, ok := target.(*NotImplementedError)
	if !ok {
		return false
	}
	return e.Plan == t.Plan
}

func IsNotImplemented(err error) bool {
	if err == nil {
		return false
	}
	var nie *NotImplementedError
	return stderrors.As(err, &nie)
}

// Predeclared sentinels for HADES design Each plan's stubs return its sentinel.
//
// The ErrNotImplementedPlanN sentinels below are declared as `var`
// (not `const`) because Go does not permit struct or pointer literals
// in const declarations. They are intentionally pointer-typed so
// `errors.Is`/`errors.As` chain through wrapped errors correctly via
// the `*NotImplementedError.Is` method. Always use the pointer form
// (these sentinels) — comparing a value-form NotImplementedError will
// not match through Is.
//
// Note these vars are technically mutable; do not modify them.
// A test in tests/compliance/ (Plan K) asserts immutability.
var (
	ErrNotImplementedPlan2 = &NotImplementedError{
		Plan: 2, PlanRef: "Bypass module implementation",
	}
	ErrNotImplementedPlan3 = &NotImplementedError{
		Plan: 3, PlanRef: "Daemon dispatcher + orchestrator failover",
	}
	ErrNotImplementedPlan4 = &NotImplementedError{
		Plan: 4, PlanRef: "Workforce + MCPs implementations",
	}
	ErrNotImplementedPlan5 = &NotImplementedError{
		Plan: 5, PlanRef: "Worktree + opencode spawn + apply stage",
	}
	ErrNotImplementedPlan6 = &NotImplementedError{
		Plan: 6, PlanRef: "Archive + delta application",
	}
	ErrNotImplementedPlan7 = &NotImplementedError{
		Plan: 7, PlanRef: "Multi-project + tmux + scheduling",
	}
	ErrNotImplementedPlan8 = &NotImplementedError{
		Plan: 8, PlanRef: "Doctrines (max-scope, capa-firewall)",
	}
	ErrNotImplementedPlan9 = &NotImplementedError{
		Plan: 9, PlanRef: "Persistencia + memoria + trace + continuity",
	}
	ErrNotImplementedPlan10 = &NotImplementedError{
		Plan: 10, PlanRef: "MLX local stack + cold-start",
	}
	ErrNotImplementedPlan11 = &NotImplementedError{
		Plan: 11, PlanRef: "Notifications + 5 channels + error handling",
	}
	ErrNotImplementedPlan12 = &NotImplementedError{
		Plan: 12, PlanRef: "TUI views complete + Glamour + lipgloss custom",
	}
	ErrNotImplementedPlan13 = &NotImplementedError{
		Plan: 13, PlanRef: "Onboarding wizard + hades doctor full",
	}
	ErrNotImplementedPlan14 = &NotImplementedError{
		Plan: 14, PlanRef: "Documentation system + RAG hybrid",
	}
	ErrNotImplementedPlan15 = &NotImplementedError{
		Plan: 15, PlanRef: "Migration tooling + distribution tooling",
	}
)
