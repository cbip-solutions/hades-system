package cli

import (
	"context"
	"testing"
)

func TestRunKnowledgeAggregatorProbeAllOK(t *testing.T) {
	deps := DoctorDeps{
		AggregatorProber: &fakeAggregatorProberJ2{
			results: []ProbeResult{
				{Name: "knowledge.aggregator.sqlite_vec_loaded", Status: ProbeOK, Message: "sqlite-vec 0.1.3 loaded"},
				{Name: "knowledge.aggregator.embedding_model_active", Status: ProbeOK, Message: "embedding model active: Mac MPS path nomic-embed-text"},
				{Name: "knowledge.aggregator.fts5_index_size", Status: ProbeOK, Message: "FTS5 index 12 MB (47 notes; expected ~11 MB)"},
				{Name: "knowledge.aggregator.pinned_notes_count", Status: ProbeOK, Message: "47 pinned notes; last promote 2h ago"},
			},
		},
	}
	probes, err := RunKnowledgeAggregatorProbe(context.Background(), deps)
	if err != nil {
		t.Fatalf("RunKnowledgeAggregatorProbe error = %v", err)
	}
	if len(probes) != 4 {
		t.Errorf("len(probes) = %d, want 4", len(probes))
	}
	if ExitCode(probes, false) != 0 {
		t.Errorf("ExitCode = %d, want 0 (all OK)", ExitCode(probes, false))
	}
}

func TestRunKnowledgeAggregatorProbeDegradedWarn(t *testing.T) {
	deps := DoctorDeps{
		AggregatorProber: &fakeAggregatorProberJ2{
			results: []ProbeResult{
				{Name: "knowledge.aggregator.sqlite_vec_loaded", Status: ProbeOK, Message: "loaded"},
				{Name: "knowledge.aggregator.embedding_model_active", Status: ProbeWarn, Message: "embedding model degraded: fallback to FTS5-only search"},
				{Name: "knowledge.aggregator.fts5_index_size", Status: ProbeOK, Message: "ok"},
				{Name: "knowledge.aggregator.pinned_notes_count", Status: ProbeOK, Message: "0 pinned notes"},
			},
		},
	}
	probes, err := RunKnowledgeAggregatorProbe(context.Background(), deps)
	if err != nil {
		t.Fatalf("RunKnowledgeAggregatorProbe error = %v", err)
	}
	if ExitCode(probes, false) != 0 {
		t.Errorf("ExitCode warn (non-strict) = %d, want 0", ExitCode(probes, false))
	}
	if ExitCode(probes, true) != 1 {
		t.Errorf("ExitCode warn (strict) = %d, want 1", ExitCode(probes, true))
	}
}

func TestRunKnowledgeAggregatorProbeBrokenFail(t *testing.T) {
	deps := DoctorDeps{
		AggregatorProber: &fakeAggregatorProberJ2{
			results: []ProbeResult{
				{Name: "knowledge.aggregator.sqlite_vec_loaded", Status: ProbeFail, Message: "sqlite-vec extension load failed at daemon start", Hint: "verify //go:build cgo"},
				{Name: "knowledge.aggregator.embedding_model_active", Status: ProbeFail, Message: "embedding model unavailable"},
				{Name: "knowledge.aggregator.fts5_index_size", Status: ProbeFail, Message: "FTS5 index empty but pinned notes exist"},
				{Name: "knowledge.aggregator.pinned_notes_count", Status: ProbeOK, Message: "12 pinned notes"},
			},
		},
	}
	probes, err := RunKnowledgeAggregatorProbe(context.Background(), deps)
	if err != nil {
		t.Fatalf("RunKnowledgeAggregatorProbe error = %v", err)
	}
	if ExitCode(probes, false) != 1 {
		t.Errorf("ExitCode any-Fail = %d, want 1", ExitCode(probes, false))
	}
}

func TestRunKnowledgeAggregatorProbeNilProberError(t *testing.T) {
	deps := DoctorDeps{}
	_, err := RunKnowledgeAggregatorProbe(context.Background(), deps)
	if err == nil {
		t.Error("expected error for nil AggregatorProber, got nil")
	}
}

func TestRunKnowledgeAggregatorProbeContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	deps := DoctorDeps{
		AggregatorProber: &fakeAggregatorProberJ2{
			results: []ProbeResult{
				{Name: "knowledge.aggregator.sqlite_vec_loaded", Status: ProbeOK},
			},
		},
	}
	_, err := RunKnowledgeAggregatorProbe(ctx, deps)
	if err == nil {
		t.Error("expected error for cancelled context, got nil")
	}
}

func TestNewDoctorKnowledgeAggregatorCmdShape(t *testing.T) {
	cmd := NewDoctorKnowledgeAggregatorCmd()
	if cmd.Use != "aggregator" {
		t.Errorf("Use = %q, want \"aggregator\"", cmd.Use)
	}
	if cmd.Short == "" {
		t.Error("Short is empty")
	}
	if len(cmd.Commands()) != 0 {
		t.Errorf("subcommands = %d, want 0 (leaf command)", len(cmd.Commands()))
	}
}

type fakeAggregatorProberJ2 struct{ results []ProbeResult }

func (f *fakeAggregatorProberJ2) Probe(_ context.Context) []ProbeResult { return f.results }

var _ AggregatorProber = (*fakeAggregatorProberJ2)(nil)
