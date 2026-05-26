package doctorfull_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/cli/doctorfull"
	"github.com/cbip-solutions/hades-system/internal/doctor/aggregator"
	"github.com/cbip-solutions/hades-system/internal/doctor/check"
)

var sentinelRecoverable = errors.New("test-recoverable")

func TestDoctorFullHelpDocumentsExitCodes(t *testing.T) {
	cmd := doctorfull.NewDoctorFullCmd(doctorfull.Config{RecoverableSentinel: sentinelRecoverable})
	if !strings.Contains(cmd.Long, "EXIT CODES") {
		t.Errorf("Long help missing 'EXIT CODES' section")
	}
	for _, expected := range []string{
		"0 = all pass",
		"1 = any warn",
		"2 = any fail",
		"4 = any skip",
		"OR'd",
	} {
		if !strings.Contains(cmd.Long, expected) {
			t.Errorf("Long help missing %q in EXIT CODES section", expected)
		}
	}
}

func TestDoctorFullCmdRegistersAllFlags(t *testing.T) {
	cmd := doctorfull.NewDoctorFullCmd(doctorfull.Config{RecoverableSentinel: sentinelRecoverable})
	expectedFlags := []string{
		"fix",
		"auto-safe",
		"yes",
		"non-interactive",
		"quick",
		"spotlight",
		"ascii",
		"format",
		"check-timeout",
		"no-color",
		"strict-skip",
	}
	for _, name := range expectedFlags {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("flag %q not registered", name)
		}
	}
}

func TestDoctorFullAllPassReturnsNil(t *testing.T) {
	defer doctorfull.SetDoctorFullCatalogForTesting(func() []check.Check {
		return []check.Check{&fakeCheck{name: "test.pass", status: check.StatusPass}}
	})()
	cmd := doctorfull.NewDoctorFullCmd(doctorfull.Config{RecoverableSentinel: sentinelRecoverable})
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if err := cmd.Execute(); err != nil {
		t.Errorf("Execute all-pass: %v", err)
	}
}

func TestDoctorFullFailReturnsRecoverable(t *testing.T) {
	defer doctorfull.SetDoctorFullCatalogForTesting(func() []check.Check {
		return []check.Check{&fakeCheck{name: "test.fail", status: check.StatusFail}}
	})()
	cmd := doctorfull.NewDoctorFullCmd(doctorfull.Config{RecoverableSentinel: sentinelRecoverable})
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("Execute with fail diagnostic: err=nil, want non-nil")
	}
	if !errors.Is(err, sentinelRecoverable) {
		t.Errorf("err missing sentinel wrap: %v", err)
	}
	if !strings.Contains(err.Error(), "bitmask 2") {
		t.Errorf("err missing 'bitmask 2': %v", err)
	}
}

func TestDoctorFullFixNonInteractiveWithoutYesErrors(t *testing.T) {
	defer doctorfull.SetDoctorFullCatalogForTesting(func() []check.Check {
		return []check.Check{&fakeCheck{name: "test.pass", status: check.StatusPass}}
	})()
	cmd := doctorfull.NewDoctorFullCmd(doctorfull.Config{RecoverableSentinel: sentinelRecoverable})
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{"--fix", "--non-interactive"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "requires --yes") {
		t.Errorf("err = %v, want 'requires --yes'", err)
	}
}

func TestDoctorFullJSONFormat(t *testing.T) {
	defer doctorfull.SetDoctorFullCatalogForTesting(func() []check.Check {
		return []check.Check{&fakeCheck{name: "test.pass", status: check.StatusPass}}
	})()
	cmd := doctorfull.NewDoctorFullCmd(doctorfull.Config{RecoverableSentinel: sentinelRecoverable})
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{"--format", "json"})
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute --format json: %v", err)
	}
	var parsed aggregator.Report
	if err := json.Unmarshal(stdout.Bytes(), &parsed); err != nil {
		t.Fatalf("Unmarshal JSON output: %v\nraw=%q", err, stdout.String())
	}
	if parsed.SchemaVersion != "1.0" {
		t.Errorf("SchemaVersion = %q, want '1.0'", parsed.SchemaVersion)
	}
}

func TestDoctorFullFixLoopRunsForNonPassChecks(t *testing.T) {
	stub := &fakeCheck{name: "test.warn", status: check.StatusWarn}
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
	if stub.fixCallCount != 1 {
		t.Errorf("Fix called %d times, want 1 (warn diagnostic)", stub.fixCallCount)
	}
}

func TestDoctorFullStrictSkipPromotesToFail(t *testing.T) {
	defer doctorfull.SetDoctorFullCatalogForTesting(func() []check.Check {
		return []check.Check{&fakeCheck{name: "test.skip", status: check.StatusSkip}}
	})()
	cmd := doctorfull.NewDoctorFullCmd(doctorfull.Config{RecoverableSentinel: sentinelRecoverable})
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{"--strict-skip"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("Execute with skip + --strict-skip: err=nil, want non-nil (promoted)")
	}
	if !errors.Is(err, sentinelRecoverable) {
		t.Errorf("err missing sentinel: %v", err)
	}
}

func TestDoctorFullCheckTimeoutHonored(t *testing.T) {
	defer doctorfull.SetDoctorFullCatalogForTesting(func() []check.Check {
		return []check.Check{&fakeCheck{name: "test.pass", status: check.StatusPass}}
	})()
	cmd := doctorfull.NewDoctorFullCmd(doctorfull.Config{RecoverableSentinel: sentinelRecoverable})
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{"--check-timeout", "1s"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if err := cmd.Execute(); err != nil {
		t.Errorf("Execute --check-timeout 1s: %v", err)
	}
}

type fakeCheck struct {
	name         string
	status       check.Status
	fixCallCount int
	fixReturnErr error
}

func (f *fakeCheck) Name() string             { return f.name }
func (f *fakeCheck) Category() check.Category { return check.CategoryRuntime }
func (f *fakeCheck) Description() string      { return "fake test check" }
func (f *fakeCheck) IsDestructive() bool      { return false }
func (f *fakeCheck) Fix(_ context.Context, _ check.FixMode) error {
	f.fixCallCount++
	return f.fixReturnErr
}
func (f *fakeCheck) Run(_ context.Context) check.DiagnosticResult {
	return check.DiagnosticResult{Name: f.name, Status: f.status, DurationMs: int64(time.Millisecond.Milliseconds())}
}
