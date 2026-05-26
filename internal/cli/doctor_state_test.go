package cli

import (
	"context"
	"testing"
)

func TestRunSystemStateProbeAllOK(t *testing.T) {
	deps := DoctorDeps{
		StateProber: &fakeStateProberJ2{
			results: []ProbeResult{
				{Name: "state.last_regenerate_age", Status: ProbeOK, Message: "last regenerate 6h ago (< 24h max-scope threshold)"},
				{Name: "state.manual_field_count", Status: ProbeOK, Message: "3 manual fields: [owner, sla_tier, cost_center]"},
				{Name: "state.missing_source_count", Status: ProbeOK, Message: "0 missing sources"},
			},
		},
	}
	probes, err := RunSystemStateProbe(context.Background(), deps)
	if err != nil {
		t.Fatalf("RunSystemStateProbe error = %v", err)
	}
	if len(probes) != 3 {
		t.Errorf("len(probes) = %d, want 3", len(probes))
	}
	if ExitCode(probes, false) != 0 {
		t.Errorf("ExitCode = %d, want 0 (all OK)", ExitCode(probes, false))
	}
}

func TestRunSystemStateProbeDegradedWarn(t *testing.T) {
	deps := DoctorDeps{
		StateProber: &fakeStateProberJ2{
			results: []ProbeResult{
				{Name: "state.last_regenerate_age", Status: ProbeWarn, Message: "last regenerate 36h ago (1×-2× threshold 24h)"},
				{Name: "state.manual_field_count", Status: ProbeOK, Message: "2 manual fields"},
				{Name: "state.missing_source_count", Status: ProbeWarn, Message: "1 missing source (1-2 band)"},
			},
		},
	}
	probes, err := RunSystemStateProbe(context.Background(), deps)
	if err != nil {
		t.Fatalf("RunSystemStateProbe error = %v", err)
	}
	if ExitCode(probes, false) != 0 {
		t.Errorf("ExitCode warn (non-strict) = %d, want 0", ExitCode(probes, false))
	}
	if ExitCode(probes, true) != 1 {
		t.Errorf("ExitCode warn (strict) = %d, want 1", ExitCode(probes, true))
	}
}

func TestRunSystemStateProbeBrokenFail(t *testing.T) {
	deps := DoctorDeps{
		StateProber: &fakeStateProberJ2{
			results: []ProbeResult{
				{Name: "state.last_regenerate_age", Status: ProbeFail, Message: "last regenerate 72h ago (>2× threshold — stale TOML per inv-zen-149)", Hint: "zen state regenerate"},
				{Name: "state.manual_field_count", Status: ProbeOK, Message: "1 manual field"},
				{Name: "state.missing_source_count", Status: ProbeFail, Message: "4 missing sources (>3 threshold — broken auto-derive walker)", Hint: "zen state validate"},
			},
		},
	}
	probes, err := RunSystemStateProbe(context.Background(), deps)
	if err != nil {
		t.Fatalf("RunSystemStateProbe error = %v", err)
	}
	if ExitCode(probes, false) != 1 {
		t.Errorf("ExitCode any-Fail = %d, want 1", ExitCode(probes, false))
	}
}

func TestRunSystemStateProbeNilProberError(t *testing.T) {
	deps := DoctorDeps{}
	_, err := RunSystemStateProbe(context.Background(), deps)
	if err == nil {
		t.Error("expected error for nil StateProber, got nil")
	}
}

func TestRunSystemStateProbeContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	deps := DoctorDeps{
		StateProber: &fakeStateProberJ2{
			results: []ProbeResult{
				{Name: "state.last_regenerate_age", Status: ProbeOK},
			},
		},
	}
	_, err := RunSystemStateProbe(ctx, deps)
	if err == nil {
		t.Error("expected error for cancelled context, got nil")
	}
}

func TestNewDoctorStateCmdShape(t *testing.T) {
	cmd := NewDoctorStateCmd()
	if cmd.Use != "state-system" {
		t.Errorf("Use = %q, want \"state-system\"", cmd.Use)
	}
	if cmd.Short == "" {
		t.Error("Short is empty")
	}
	if len(cmd.Commands()) != 0 {
		t.Errorf("subcommands = %d, want 0 (leaf command)", len(cmd.Commands()))
	}
}

type fakeStateProberJ2 struct{ results []ProbeResult }

func (f *fakeStateProberJ2) Probe(_ context.Context) []ProbeResult { return f.results }

var _ StateProber = (*fakeStateProberJ2)(nil)
