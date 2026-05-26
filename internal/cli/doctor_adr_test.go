package cli

import (
	"context"
	"testing"
)

func TestRunAdrIndexProbeAllOK(t *testing.T) {
	deps := DoctorDeps{
		ADRProber: &fakeADRProberJ2{
			results: []ProbeResult{
				{Name: "adr.index.dual_manifest_freshness", Status: ProbeOK, Message: "both _index.json and _graph.json fresh"},
				{Name: "adr.index.json_schema_validation_status", Status: ProbeOK, Message: "all 39 ADRs valid"},
				{Name: "adr.index.id_collision_count", Status: ProbeOK, Message: "0 ID collisions"},
			},
		},
	}
	probes, err := RunAdrIndexProbe(context.Background(), deps)
	if err != nil {
		t.Fatalf("RunAdrIndexProbe error = %v", err)
	}
	if len(probes) != 3 {
		t.Errorf("len(probes) = %d, want 3", len(probes))
	}
	if ExitCode(probes, false) != 0 {
		t.Errorf("ExitCode = %d, want 0 (all OK)", ExitCode(probes, false))
	}
}

func TestRunAdrIndexProbeDegradedWarn(t *testing.T) {
	deps := DoctorDeps{
		ADRProber: &fakeADRProberJ2{
			results: []ProbeResult{
				{Name: "adr.index.dual_manifest_freshness", Status: ProbeWarn, Message: "_index.json stale (1 manifest stale of 2)"},
				{Name: "adr.index.json_schema_validation_status", Status: ProbeOK, Message: "all 39 ADRs valid"},
				{Name: "adr.index.id_collision_count", Status: ProbeOK, Message: "0 ID collisions"},
			},
		},
	}
	probes, err := RunAdrIndexProbe(context.Background(), deps)
	if err != nil {
		t.Fatalf("RunAdrIndexProbe error = %v", err)
	}
	if ExitCode(probes, false) != 0 {
		t.Errorf("ExitCode warn (non-strict) = %d, want 0", ExitCode(probes, false))
	}
	if ExitCode(probes, true) != 1 {
		t.Errorf("ExitCode warn (strict) = %d, want 1", ExitCode(probes, true))
	}
}

func TestRunAdrIndexProbeBrokenFail(t *testing.T) {
	deps := DoctorDeps{
		ADRProber: &fakeADRProberJ2{
			results: []ProbeResult{
				{Name: "adr.index.dual_manifest_freshness", Status: ProbeOK, Message: "both manifests fresh"},
				{Name: "adr.index.json_schema_validation_status", Status: ProbeFail, Message: "5 schema violations (>3 threshold)"},
				{Name: "adr.index.id_collision_count", Status: ProbeFail, Message: "2 ID collisions detected", Hint: "zen adr reindex"},
			},
		},
	}
	probes, err := RunAdrIndexProbe(context.Background(), deps)
	if err != nil {
		t.Fatalf("RunAdrIndexProbe error = %v", err)
	}
	if ExitCode(probes, false) != 1 {
		t.Errorf("ExitCode any-Fail = %d, want 1", ExitCode(probes, false))
	}
}

func TestRunAdrIndexProbeNilProberError(t *testing.T) {
	deps := DoctorDeps{}
	_, err := RunAdrIndexProbe(context.Background(), deps)
	if err == nil {
		t.Error("expected error for nil ADRProber, got nil")
	}
}

func TestRunAdrIndexProbeContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	deps := DoctorDeps{
		ADRProber: &fakeADRProberJ2{
			results: []ProbeResult{
				{Name: "adr.index.dual_manifest_freshness", Status: ProbeOK},
			},
		},
	}
	_, err := RunAdrIndexProbe(ctx, deps)
	if err == nil {
		t.Error("expected error for cancelled context, got nil")
	}
}

func TestNewDoctorAdrCmdShape(t *testing.T) {
	cmd := NewDoctorAdrCmd()
	if cmd.Use != "adr" {
		t.Errorf("Use = %q, want \"adr\"", cmd.Use)
	}
	if cmd.Short == "" {
		t.Error("Short is empty")
	}
	subs := cmd.Commands()
	if len(subs) != 1 {
		t.Errorf("subcommands = %d, want 1 (index)", len(subs))
	}
	if len(subs) > 0 && subs[0].Use != "index" {
		t.Errorf("subcommand Use = %q, want \"index\"", subs[0].Use)
	}
}

type fakeADRProberJ2 struct{ results []ProbeResult }

func (f *fakeADRProberJ2) Probe(_ context.Context) []ProbeResult { return f.results }

var _ ADRProber = (*fakeADRProberJ2)(nil)
