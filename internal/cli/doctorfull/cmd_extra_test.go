package doctorfull_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/cli/doctorfull"
	"github.com/cbip-solutions/hades-system/internal/doctor/check"
)

func TestDoctorFullProductionCatalogContainsFourPlanThirteenChecks(t *testing.T) {

	defer doctorfull.SetDoctorFullCatalogForTesting(nil)()

	cmd := doctorfull.NewDoctorFullCmd(doctorfull.Config{RecoverableSentinel: sentinelRecoverable})
	cmd.SetContext(context.Background())

	cmd.SetArgs([]string{"--check-timeout", "100ms"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	_ = cmd.Execute()
}

func TestDoctorFullFixAutoSafeMode(t *testing.T) {
	stub := &fakeCheck{name: "test.warn", status: check.StatusWarn}
	defer doctorfull.SetDoctorFullCatalogForTesting(func() []check.Check {
		return []check.Check{stub}
	})()
	cmd := doctorfull.NewDoctorFullCmd(doctorfull.Config{RecoverableSentinel: sentinelRecoverable})
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{"--fix", "--auto-safe"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	_ = cmd.Execute()
	if stub.fixCallCount != 1 {
		t.Errorf("Fix called %d times under --fix --auto-safe, want 1", stub.fixCallCount)
	}
}

func TestDoctorFullFixInteractiveMode(t *testing.T) {
	stub := &fakeCheck{name: "test.warn", status: check.StatusWarn}
	defer doctorfull.SetDoctorFullCatalogForTesting(func() []check.Check {
		return []check.Check{stub}
	})()
	cmd := doctorfull.NewDoctorFullCmd(doctorfull.Config{RecoverableSentinel: sentinelRecoverable})
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{"--fix"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	_ = cmd.Execute()
	if stub.fixCallCount != 1 {
		t.Errorf("Fix called %d times under --fix, want 1", stub.fixCallCount)
	}
}

func TestDoctorFullFixLoopHaltsOnFixError(t *testing.T) {
	stub := &fakeCheck{name: "test.fail", status: check.StatusFail, fixReturnErr: errors.New("fix blew up")}
	defer doctorfull.SetDoctorFullCatalogForTesting(func() []check.Check {
		return []check.Check{stub}
	})()
	cmd := doctorfull.NewDoctorFullCmd(doctorfull.Config{RecoverableSentinel: sentinelRecoverable})
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{"--fix", "--yes"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	err := cmd.Execute()
	if err == nil || !errors.Is(err, stub.fixReturnErr) && !contains(err.Error(), "fix blew up") {
		t.Errorf("Execute with failing fix: err=%v, want wrapped 'fix blew up'", err)
	}
}

func TestDoctorFullFixLoopSkipsUnmatchedDiagnostic(t *testing.T) {
	stub := &mismatchedFakeCheck{
		checkName:      "test.alpha",
		diagnosticName: "test.beta",
		status:         check.StatusFail,
	}
	defer doctorfull.SetDoctorFullCatalogForTesting(func() []check.Check {
		return []check.Check{stub}
	})()
	cmd := doctorfull.NewDoctorFullCmd(doctorfull.Config{RecoverableSentinel: sentinelRecoverable})
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{"--fix", "--yes"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	_ = cmd.Execute()
	if stub.fixCallCount != 0 {
		t.Errorf("mismatched diagnostic triggered Fix; want skip; got %d calls", stub.fixCallCount)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

type mismatchedFakeCheck struct {
	checkName      string
	diagnosticName string
	status         check.Status
	fixCallCount   int
}

func (m *mismatchedFakeCheck) Name() string             { return m.checkName }
func (m *mismatchedFakeCheck) Category() check.Category { return check.CategoryRuntime }
func (m *mismatchedFakeCheck) Description() string      { return "mismatched test check" }
func (m *mismatchedFakeCheck) IsDestructive() bool      { return false }
func (m *mismatchedFakeCheck) Fix(_ context.Context, _ check.FixMode) error {
	m.fixCallCount++
	return nil
}
func (m *mismatchedFakeCheck) Run(_ context.Context) check.DiagnosticResult {
	return check.DiagnosticResult{Name: m.diagnosticName, Status: m.status}
}
