package aggregator_test

import (
	"testing"

	"github.com/cbip-solutions/hades-system/internal/doctor/aggregator"
	"github.com/cbip-solutions/hades-system/internal/doctor/check"
)

func TestExitCodeAllPass(t *testing.T) {
	r := &aggregator.Report{
		Diagnostics: []check.DiagnosticResult{
			{Status: check.StatusPass},
			{Status: check.StatusPass},
		},
	}
	if got := aggregator.ExitCode(r, false); got != 0 {
		t.Errorf("ExitCode = %d, want 0", got)
	}
}

func TestExitCodeWarnOnly(t *testing.T) {
	r := &aggregator.Report{
		Diagnostics: []check.DiagnosticResult{
			{Status: check.StatusPass},
			{Status: check.StatusWarn},
		},
	}
	if got := aggregator.ExitCode(r, false); got != 1 {
		t.Errorf("ExitCode = %d, want 1 (warn bit)", got)
	}
}

func TestExitCodeFailOnly(t *testing.T) {
	r := &aggregator.Report{
		Diagnostics: []check.DiagnosticResult{
			{Status: check.StatusFail},
		},
	}
	if got := aggregator.ExitCode(r, false); got != 2 {
		t.Errorf("ExitCode = %d, want 2 (fail bit)", got)
	}
}

func TestExitCodeSkipOnly(t *testing.T) {
	r := &aggregator.Report{
		Diagnostics: []check.DiagnosticResult{
			{Status: check.StatusSkip},
		},
	}
	if got := aggregator.ExitCode(r, false); got != 4 {
		t.Errorf("ExitCode = %d, want 4 (skip bit)", got)
	}
}

func TestExitCodeWarnFail(t *testing.T) {
	r := &aggregator.Report{
		Diagnostics: []check.DiagnosticResult{
			{Status: check.StatusWarn},
			{Status: check.StatusFail},
		},
	}
	if got := aggregator.ExitCode(r, false); got != 3 {
		t.Errorf("ExitCode = %d, want 3 (warn+fail)", got)
	}
}

func TestExitCodeWarnFailSkip(t *testing.T) {
	r := &aggregator.Report{
		Diagnostics: []check.DiagnosticResult{
			{Status: check.StatusWarn},
			{Status: check.StatusFail},
			{Status: check.StatusSkip},
		},
	}
	if got := aggregator.ExitCode(r, false); got != 7 {
		t.Errorf("ExitCode = %d, want 7 (all three)", got)
	}
}

func TestExitCodeStrictSkipPromotes(t *testing.T) {
	r := &aggregator.Report{
		Diagnostics: []check.DiagnosticResult{
			{Status: check.StatusSkip},
		},
	}
	if got := aggregator.ExitCode(r, true); got != 2 {
		t.Errorf("ExitCode strictSkip=true = %d, want 2", got)
	}
}

func TestExitCodeStrictSkipWithWarn(t *testing.T) {
	r := &aggregator.Report{
		Diagnostics: []check.DiagnosticResult{
			{Status: check.StatusWarn},
			{Status: check.StatusSkip},
		},
	}
	if got := aggregator.ExitCode(r, true); got != 3 {
		t.Errorf("ExitCode strictSkip=true warn+skip = %d, want 3", got)
	}
}

func TestExitCodeNilReport(t *testing.T) {
	if got := aggregator.ExitCode(nil, false); got != 0 {
		t.Errorf("ExitCode(nil) = %d, want 0", got)
	}
}

func TestExitCodeEmptyDiagnostics(t *testing.T) {
	r := &aggregator.Report{}
	if got := aggregator.ExitCode(r, false); got != 0 {
		t.Errorf("ExitCode empty = %d, want 0", got)
	}
}

func TestExitCodeBitmaskValuesStable(t *testing.T) {
	if aggregator.ExitWarnBit != 1 {
		t.Errorf("ExitWarnBit = %d, want 1", aggregator.ExitWarnBit)
	}
	if aggregator.ExitFailBit != 2 {
		t.Errorf("ExitFailBit = %d, want 2", aggregator.ExitFailBit)
	}
	if aggregator.ExitSkipBit != 4 {
		t.Errorf("ExitSkipBit = %d, want 4", aggregator.ExitSkipBit)
	}
}
