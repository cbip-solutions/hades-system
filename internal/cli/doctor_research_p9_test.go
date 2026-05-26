package cli

import (
	"context"
	"testing"
)

func TestRunResearchCacheProbeAllOK(t *testing.T) {
	deps := DoctorDeps{
		ResearchCacheProber: &fakeResearchCacheProberJ2{
			results: []ProbeResult{
				{Name: "research.cache.hit_rate", Status: ProbeOK, Message: "72% hit rate (24h rolling)"},
				{Name: "research.cache.volume", Status: ProbeOK, Message: "1240 dispatches; 3800 findings; 42 MB CAS"},
				{Name: "research.cache.freshness_lag", Status: ProbeOK, Message: "median age 4h (< TTL 24h)"},
				{Name: "research.cache.revalidation_queue_depth", Status: ProbeOK, Message: "3 pending revalidations (< 10 threshold)"},
				{Name: "research.cache.stuck_queries_count", Status: ProbeOK, Message: "0 stuck queries"},
			},
		},
	}
	probes, err := RunResearchCacheProbe(context.Background(), deps)
	if err != nil {
		t.Fatalf("RunResearchCacheProbe error = %v", err)
	}
	if len(probes) != 5 {
		t.Errorf("len(probes) = %d, want 5", len(probes))
	}
	if ExitCode(probes, false) != 0 {
		t.Errorf("ExitCode = %d, want 0 (all OK)", ExitCode(probes, false))
	}
}

func TestRunResearchCacheProbeDegradedWarn(t *testing.T) {
	deps := DoctorDeps{
		ResearchCacheProber: &fakeResearchCacheProberJ2{
			results: []ProbeResult{
				{Name: "research.cache.hit_rate", Status: ProbeWarn, Message: "38% hit rate (25%-50% band)"},
				{Name: "research.cache.volume", Status: ProbeOK, Message: "500 dispatches"},
				{Name: "research.cache.freshness_lag", Status: ProbeWarn, Message: "median age 36h (1×-2× TTL 24h)"},
				{Name: "research.cache.revalidation_queue_depth", Status: ProbeWarn, Message: "22 pending (10-50 band)"},
				{Name: "research.cache.stuck_queries_count", Status: ProbeOK, Message: "0 stuck queries"},
			},
		},
	}
	probes, err := RunResearchCacheProbe(context.Background(), deps)
	if err != nil {
		t.Fatalf("RunResearchCacheProbe error = %v", err)
	}
	if ExitCode(probes, false) != 0 {
		t.Errorf("ExitCode warn (non-strict) = %d, want 0", ExitCode(probes, false))
	}
	if ExitCode(probes, true) != 1 {
		t.Errorf("ExitCode warn (strict) = %d, want 1", ExitCode(probes, true))
	}
}

func TestRunResearchCacheProbeBrokenFail(t *testing.T) {
	deps := DoctorDeps{
		ResearchCacheProber: &fakeResearchCacheProberJ2{
			results: []ProbeResult{
				{Name: "research.cache.hit_rate", Status: ProbeFail, Message: "18% hit rate (<25% threshold — broken cache)", Hint: "zen research cache flush"},
				{Name: "research.cache.volume", Status: ProbeOK, Message: "100 dispatches"},
				{Name: "research.cache.freshness_lag", Status: ProbeFail, Message: "median age 72h (>2× TTL 24h)"},
				{Name: "research.cache.revalidation_queue_depth", Status: ProbeFail, Message: "63 pending (>50 threshold — worker stuck)", Hint: "zen research cache revalidate --force"},
				{Name: "research.cache.stuck_queries_count", Status: ProbeFail, Message: "5 queries stuck >1h", Hint: "zen research cache purge-stuck"},
			},
		},
	}
	probes, err := RunResearchCacheProbe(context.Background(), deps)
	if err != nil {
		t.Fatalf("RunResearchCacheProbe error = %v", err)
	}
	if ExitCode(probes, false) != 1 {
		t.Errorf("ExitCode any-Fail = %d, want 1", ExitCode(probes, false))
	}
}

func TestRunResearchCacheProbeNilProberError(t *testing.T) {
	deps := DoctorDeps{}
	_, err := RunResearchCacheProbe(context.Background(), deps)
	if err == nil {
		t.Error("expected error for nil ResearchCacheProber, got nil")
	}
}

func TestRunResearchCacheProbeContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	deps := DoctorDeps{
		ResearchCacheProber: &fakeResearchCacheProberJ2{
			results: []ProbeResult{
				{Name: "research.cache.hit_rate", Status: ProbeOK},
			},
		},
	}
	_, err := RunResearchCacheProbe(ctx, deps)
	if err == nil {
		t.Error("expected error for cancelled context, got nil")
	}
}

func TestNewDoctorResearchCacheCmdShape(t *testing.T) {
	cmd := NewDoctorResearchCacheCmd()
	if cmd.Use != "cache" {
		t.Errorf("Use = %q, want \"cache\"", cmd.Use)
	}
	if cmd.Short == "" {
		t.Error("Short is empty")
	}
	if len(cmd.Commands()) != 0 {
		t.Errorf("subcommands = %d, want 0 (leaf command)", len(cmd.Commands()))
	}
}

type fakeResearchCacheProberJ2 struct{ results []ProbeResult }

func (f *fakeResearchCacheProberJ2) Probe(_ context.Context) []ProbeResult { return f.results }

var _ ResearchCacheProber = (*fakeResearchCacheProberJ2)(nil)
