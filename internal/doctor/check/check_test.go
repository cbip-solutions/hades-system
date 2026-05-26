// Package check_test exercises the canonical Check interface contract
// + DiagnosticResult value type + Status/FixMode/Category enums declared
// in package check.
//
// Per Plan 13 Phase F Task F1 (TDD step 1): tests are written FIRST and
// MUST fail (no implementation yet); step 3 implements check.go to
// satisfy them.
package check_test

import (
	"context"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/doctor/check"
)

func TestCheckInterfaceShape(t *testing.T) {
	var _ check.Check = (*fakeCheck)(nil)
}

// TestDiagnosticResultZeroValueIsPass asserts the zero-value
// DiagnosticResult has Status=StatusPass (numeric 0). Trivial-pass checks
// MAY return `DiagnosticResult{}` without explicit field assignment.
//
// Status enum ordering is load-bearing for the bitmask exit-code
// computation (see exit_codes.go); StatusPass MUST be the iota zero so
// the worst-status selector works.
func TestDiagnosticResultZeroValueIsPass(t *testing.T) {
	var r check.DiagnosticResult
	if r.Status != check.StatusPass {
		t.Errorf("zero value Status = %v, want StatusPass", r.Status)
	}
	if r.Name != "" || r.Message != "" || r.Hint != "" || r.Detail != "" {
		t.Errorf("zero value not empty: %+v", r)
	}
	if r.DurationMs != 0 || r.AuditEventHash != "" {
		t.Errorf("zero value DurationMs/AuditEventHash nonzero: %+v", r)
	}
}

func TestRunAgainstFakeReturnsDiagnostic(t *testing.T) {
	fake := &fakeCheck{
		name:     "test.example",
		category: check.CategoryRuntime,
		runFunc: func(ctx context.Context) check.DiagnosticResult {
			return check.DiagnosticResult{
				Name:    "test.example",
				Status:  check.StatusWarn,
				Message: "warning condition",
				Hint:    "do X",
			}
		},
	}
	got := fake.Run(context.Background())
	if got.Name != "test.example" {
		t.Errorf("Name = %q, want test.example", got.Name)
	}
	if got.Status != check.StatusWarn {
		t.Errorf("Status = %v, want StatusWarn", got.Status)
	}
	if got.Hint != "do X" {
		t.Errorf("Hint = %q, want do X", got.Hint)
	}
}

// TestRunRespectsContextCancellation asserts a long-running check honors
// ctx.Done() and emits StatusSkip with a "context cancelled" hint.
//
// Defense in depth: every Check impl MUST honor ctx cancellation; the
// Aggregator wraps each Run with a per-check timeout, but the inner
// Check is the load-bearing cancellation surface.
func TestRunRespectsContextCancellation(t *testing.T) {
	fake := &fakeCheck{
		name:     "test.long",
		category: check.CategoryRuntime,
		runFunc: func(ctx context.Context) check.DiagnosticResult {
			select {
			case <-ctx.Done():
				return check.DiagnosticResult{
					Name:    "test.long",
					Status:  check.StatusSkip,
					Message: "context cancelled",
				}
			case <-time.After(100 * time.Millisecond):
				return check.DiagnosticResult{
					Name:   "test.long",
					Status: check.StatusPass,
				}
			}
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got := fake.Run(ctx)
	if got.Status != check.StatusSkip {
		t.Errorf("ctx cancelled Status = %v, want StatusSkip", got.Status)
	}
}

func TestIsDestructiveDefaultsFalse(t *testing.T) {
	fake := &fakeCheck{}
	if fake.IsDestructive() {
		t.Errorf("IsDestructive default = true, want false")
	}
}

func TestCategoryString(t *testing.T) {
	tests := []struct {
		cat  check.Category
		want string
	}{
		{check.CategoryPreflight, "preflight"},
		{check.CategoryRuntime, "runtime"},
		{check.CategoryConfiguration, "configuration"},
		{check.CategoryHints, "hints"},
		{check.Category(99), "unknown"},
	}
	for _, tc := range tests {
		if got := tc.cat.String(); got != tc.want {
			t.Errorf("Category(%d).String() = %q, want %q", tc.cat, got, tc.want)
		}
	}
}

func TestCategoriesReturnsCanonicalOrder(t *testing.T) {
	got := check.Categories()
	want := []check.Category{
		check.CategoryPreflight,
		check.CategoryRuntime,
		check.CategoryConfiguration,
		check.CategoryHints,
	}
	if len(got) != len(want) {
		t.Fatalf("len(Categories()) = %d, want %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("Categories()[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

type fakeCheck struct {
	name        string
	category    check.Category
	destructive bool
	runFunc     func(ctx context.Context) check.DiagnosticResult
	fixFunc     func(ctx context.Context, mode check.FixMode) error
}

func (f *fakeCheck) Name() string             { return f.name }
func (f *fakeCheck) Category() check.Category { return f.category }
func (f *fakeCheck) Description() string      { return "fake check for tests" }
func (f *fakeCheck) IsDestructive() bool      { return f.destructive }
func (f *fakeCheck) Run(ctx context.Context) check.DiagnosticResult {
	if f.runFunc != nil {
		return f.runFunc(ctx)
	}
	return check.DiagnosticResult{Name: f.name, Status: check.StatusPass}
}
func (f *fakeCheck) Fix(ctx context.Context, mode check.FixMode) error {
	if f.fixFunc != nil {
		return f.fixFunc(ctx, mode)
	}
	return nil
}
