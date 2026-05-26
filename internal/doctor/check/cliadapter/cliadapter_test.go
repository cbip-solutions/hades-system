package cliadapter_test

import (
	"context"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/cli"
	"github.com/cbip-solutions/hades-system/internal/doctor/check"
	"github.com/cbip-solutions/hades-system/internal/doctor/check/cliadapter"
)

func TestCLIProbeAdapterSatisfiesCheck(t *testing.T) {
	var _ check.Check = (*cliadapter.CLIProbeAdapter)(nil)
}

func TestCLIProbeAdapterName(t *testing.T) {
	a := cliadapter.NewCLIProbeAdapter(cliadapter.CLIProbeAdapterConfig{
		Name:     "bypass.config",
		Category: check.CategoryConfiguration,
	})
	if a.Name() != "bypass.config" {
		t.Errorf("Name = %q, want bypass.config", a.Name())
	}
	if a.Category() != check.CategoryConfiguration {
		t.Errorf("Category = %v, want CategoryConfiguration", a.Category())
	}
}

func TestCLIProbeAdapterDescriptionDefault(t *testing.T) {
	a := cliadapter.NewCLIProbeAdapter(cliadapter.CLIProbeAdapterConfig{
		Name:     "test.x",
		Category: check.CategoryRuntime,
	})
	if a.Description() == "" {
		t.Errorf("Description empty; default should populate")
	}
}

func TestCLIProbeAdapterDescriptionOverride(t *testing.T) {
	a := cliadapter.NewCLIProbeAdapter(cliadapter.CLIProbeAdapterConfig{
		Name:        "test.x",
		Category:    check.CategoryRuntime,
		Description: "specialized description",
	})
	if a.Description() != "specialized description" {
		t.Errorf("Description = %q, want specialized description", a.Description())
	}
}

func TestCLIProbeAdapterIsNonDestructive(t *testing.T) {
	a := cliadapter.NewCLIProbeAdapter(cliadapter.CLIProbeAdapterConfig{
		Name:     "test.x",
		Category: check.CategoryRuntime,
	})
	if a.IsDestructive() {
		t.Errorf("IsDestructive = true; adapted probes are read-only")
	}
}

func TestCLIProbeAdapterFixIsNoop(t *testing.T) {
	a := cliadapter.NewCLIProbeAdapter(cliadapter.CLIProbeAdapterConfig{
		Name:     "test.x",
		Category: check.CategoryRuntime,
	})
	for _, mode := range []check.FixMode{check.FixModeReadOnly, check.FixModeInteractive, check.FixModeAutoSafe, check.FixModeYes} {
		if err := a.Fix(context.Background(), mode); err != nil {
			t.Errorf("Fix(mode=%v) = %v; want nil", mode, err)
		}
	}
}

func TestCLIProbeAdapterRunSelectsWorstStatus(t *testing.T) {
	a := cliadapter.NewCLIProbeAdapter(cliadapter.CLIProbeAdapterConfig{
		Name:     "test.aggregate",
		Category: check.CategoryRuntime,
		ProbeFunc: func(ctx context.Context) []cli.ProbeResult {
			return []cli.ProbeResult{
				{Name: "a", Status: cli.ProbeOK, Message: "all good"},
				{Name: "b", Status: cli.ProbeWarn, Message: "soft fail", Hint: "do x"},
				{Name: "c", Status: cli.ProbeFail, Message: "hard fail", Hint: "do y"},
			}
		},
	})
	d := a.Run(context.Background())
	if d.Status != check.StatusFail {
		t.Errorf("Status = %v, want StatusFail (worst across rows)", d.Status)
	}
	if d.Message != "hard fail" {
		t.Errorf("Message = %q, want 'hard fail'", d.Message)
	}
	if d.Hint != "do y" {
		t.Errorf("Hint = %q, want 'do y'", d.Hint)
	}
	if d.Detail == "" {
		t.Errorf("Detail empty; want per-row aggregate")
	}
	if !strings.Contains(d.Detail, "a: all good") {
		t.Errorf("Detail missing row a; got %q", d.Detail)
	}
	if !strings.Contains(d.Detail, "c: hard fail") {
		t.Errorf("Detail missing row c; got %q", d.Detail)
	}
}

func TestCLIProbeAdapterRunSingleRowNoDetail(t *testing.T) {
	a := cliadapter.NewCLIProbeAdapter(cliadapter.CLIProbeAdapterConfig{
		Name:     "test.single",
		Category: check.CategoryRuntime,
		ProbeFunc: func(ctx context.Context) []cli.ProbeResult {
			return []cli.ProbeResult{
				{Name: "only", Status: cli.ProbeOK, Message: "fine"},
			}
		},
	})
	d := a.Run(context.Background())
	if d.Status != check.StatusPass {
		t.Errorf("Status = %v, want StatusPass", d.Status)
	}
	if d.Detail != "" {
		t.Errorf("Detail = %q; want empty for single-row probe", d.Detail)
	}
}

func TestCLIProbeAdapterRunEmptyResults(t *testing.T) {
	a := cliadapter.NewCLIProbeAdapter(cliadapter.CLIProbeAdapterConfig{
		Name:     "test.empty",
		Category: check.CategoryRuntime,
		ProbeFunc: func(ctx context.Context) []cli.ProbeResult {
			return nil
		},
	})
	d := a.Run(context.Background())
	if d.Status != check.StatusSkip {
		t.Errorf("Status = %v, want StatusSkip (no rows)", d.Status)
	}
	if d.Hint == "" {
		t.Errorf("Hint empty; want contract-violation hint")
	}
}

func TestCLIProbeAdapterRunNilProbeFunc(t *testing.T) {
	a := cliadapter.NewCLIProbeAdapter(cliadapter.CLIProbeAdapterConfig{
		Name:     "test.nil",
		Category: check.CategoryRuntime,
	})
	d := a.Run(context.Background())
	if d.Status != check.StatusSkip {
		t.Errorf("Status = %v, want StatusSkip (nil probeFunc)", d.Status)
	}
}

func TestCLIProbeAdapterRunDurationRecorded(t *testing.T) {
	a := cliadapter.NewCLIProbeAdapter(cliadapter.CLIProbeAdapterConfig{
		Name:     "test.dur",
		Category: check.CategoryRuntime,
		ProbeFunc: func(ctx context.Context) []cli.ProbeResult {
			return []cli.ProbeResult{{Name: "x", Status: cli.ProbeOK}}
		},
	})
	d := a.Run(context.Background())
	if d.DurationMs < 0 {
		t.Errorf("DurationMs = %d, want ≥0", d.DurationMs)
	}
}

func TestTranslateCLIStatusViaAdapter(t *testing.T) {
	tests := []struct {
		cliStatus cli.ProbeStatus
		want      check.Status
	}{
		{cli.ProbeOK, check.StatusPass},
		{cli.ProbeWarn, check.StatusWarn},
		{cli.ProbeFail, check.StatusFail},
		{cli.ProbeStatus(99), check.StatusSkip},
	}
	for _, tc := range tests {
		t.Run(tc.cliStatus.String(), func(t *testing.T) {
			a := cliadapter.NewCLIProbeAdapter(cliadapter.CLIProbeAdapterConfig{
				Name:     "test.translate",
				Category: check.CategoryRuntime,
				ProbeFunc: func(ctx context.Context) []cli.ProbeResult {
					return []cli.ProbeResult{{Name: "x", Status: tc.cliStatus}}
				},
			})
			d := a.Run(context.Background())
			if d.Status != tc.want {
				t.Errorf("translate(cli.ProbeStatus=%v) → %v, want %v", tc.cliStatus, d.Status, tc.want)
			}
		})
	}
}
