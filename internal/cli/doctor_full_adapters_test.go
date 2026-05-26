package cli

import (
	"context"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/doctor/check"
)

func TestBuildPlan1To9DoctorFullAdapters_ReturnsAtLeast12Adapters(t *testing.T) {
	adapters := BuildPlan1To9DoctorFullAdapters("/tmp/zen-swarm.sock")
	if len(adapters) < 12 {
		t.Errorf("Plan 1-9 adapter count = %d; want ≥12", len(adapters))
	}
}

func TestBuildPlan1To9DoctorFullAdapters_AllImplementCheckInterface(t *testing.T) {
	adapters := BuildPlan1To9DoctorFullAdapters("/tmp/zen-swarm.sock")
	for i, a := range adapters {
		var _ check.Check = a
		if a.Name() == "" {
			t.Errorf("adapter[%d] empty Name()", i)
		}
		if a.Description() == "" {
			t.Errorf("adapter[%d] %s: empty Description()", i, a.Name())
		}
		if a.IsDestructive() {
			t.Errorf("adapter[%d] %s: IsDestructive=true; want false (Plan 1-9 adapters are read-only)", i, a.Name())
		}
	}
}

func TestBuildPlan1To9DoctorFullAdapters_NamesUnique(t *testing.T) {
	adapters := BuildPlan1To9DoctorFullAdapters("/tmp/zen-swarm.sock")
	seen := map[string]bool{}
	for _, a := range adapters {
		if seen[a.Name()] {
			t.Errorf("duplicate adapter name: %s", a.Name())
		}
		seen[a.Name()] = true
	}
}

func TestBuildPlan1To9DoctorFullAdapters_KnownNamesPresent(t *testing.T) {
	adapters := BuildPlan1To9DoctorFullAdapters("/tmp/zen-swarm.sock")
	names := map[string]bool{}
	for _, a := range adapters {
		names[a.Name()] = true
	}
	wanted := []string{
		"subsystem.knowledge",
		"subsystem.scheduler",
		"subsystem.inbox",
		"subsystem.tmux",
		"subsystem.merge",
		"subsystem.hermes",
	}
	for _, w := range wanted {
		if !names[w] {
			t.Errorf("expected adapter name %q in catalog; not found", w)
		}
	}
}

func TestBuildPlan1To9DoctorFullAdaptersForTesting_OverrideWorks(t *testing.T) {
	cleanup := BuildPlan1To9DoctorFullAdaptersForTesting(func() []check.Check {
		return []check.Check{}
	})
	defer cleanup()
	adapters := BuildPlan1To9DoctorFullAdapters("/tmp/zen-swarm.sock")
	if len(adapters) != 0 {
		t.Errorf("override returned %d adapters; want 0", len(adapters))
	}
}

func TestCliProbeCheckAdapter_Methods(t *testing.T) {
	probe := func(_ context.Context) []ProbeResult {
		return []ProbeResult{
			{Name: "a", Status: ProbeOK, Message: "ok"},
			{Name: "b", Status: ProbeWarn, Message: "warn"},
			{Name: "c", Status: ProbeFail, Message: "fail"},
		}
	}
	adapter := newCLIProbeAdapter("test.cluster", check.CategoryConfiguration, "test description", probe)
	if adapter.Name() != "test.cluster" {
		t.Errorf("Name = %q; want test.cluster", adapter.Name())
	}
	if adapter.Category() != check.CategoryConfiguration {
		t.Errorf("Category = %v; want CategoryConfiguration", adapter.Category())
	}
	if adapter.Description() != "test description" {
		t.Errorf("Description = %q; want 'test description'", adapter.Description())
	}
	if adapter.IsDestructive() {
		t.Errorf("IsDestructive = true; want false (Plan 1-9 read-only)")
	}
	if err := adapter.Fix(context.Background(), check.FixModeYes); err != nil {
		t.Errorf("Fix returned err: %v; want nil (Plan 1-9 no-op)", err)
	}
	result := adapter.Run(context.Background())
	if result.Status != check.StatusFail {
		t.Errorf("worst-status collapse = %v; want StatusFail (one probe failed)", result.Status)
	}
}

func TestCliProbeCheckAdapter_EmptyProbeResults(t *testing.T) {
	probe := func(_ context.Context) []ProbeResult {
		return []ProbeResult{}
	}
	adapter := newCLIProbeAdapter("test.empty", check.CategoryConfiguration, "empty", probe)
	result := adapter.Run(context.Background())
	if result.Status != check.StatusSkip {
		t.Errorf("empty probe results: status = %v; want StatusSkip", result.Status)
	}
}

func TestCliProbeCheckAdapter_NilProbeFunc(t *testing.T) {
	adapter := newCLIProbeAdapter("test.nil", check.CategoryConfiguration, "nil", nil)
	result := adapter.Run(context.Background())
	if result.Status != check.StatusSkip {
		t.Errorf("nil probe func: status = %v; want StatusSkip", result.Status)
	}
}

func TestTranslateProbeStatusToCheckStatus(t *testing.T) {
	cases := []struct {
		in   ProbeStatus
		want check.Status
	}{
		{ProbeOK, check.StatusPass},
		{ProbeWarn, check.StatusWarn},
		{ProbeFail, check.StatusFail},
		{ProbeStatus(99), check.StatusSkip},
	}
	for _, tc := range cases {
		if got := translateProbeStatusToCheckStatus(tc.in); got != tc.want {
			t.Errorf("translateProbeStatusToCheckStatus(%v) = %v; want %v", tc.in, got, tc.want)
		}
	}
}

func TestCheckResultStatusToProbeStatus(t *testing.T) {
	cases := []struct {
		in   string
		want ProbeStatus
	}{
		{"ok", ProbeOK},
		{"warn", ProbeWarn},
		{"fail", ProbeFail},
		{"unknown", ProbeFail},
	}
	for _, tc := range cases {
		if got := checkResultStatusToProbeStatus(tc.in); got != tc.want {
			t.Errorf("checkResultStatusToProbeStatus(%q) = %v; want %v", tc.in, got, tc.want)
		}
	}
}

func TestCheckResultsToProbeResults(t *testing.T) {
	in := []CheckResult{
		{Name: "a", Status: "ok", Detail: "all good", Hint: "no action"},
		{Name: "b", Status: "warn", Detail: "warning", Hint: "investigate"},
		{Name: "c", Status: "fail", Detail: "broken", Hint: "fix me"},
	}
	out := checkResultsToProbeResults(in)
	if len(out) != 3 {
		t.Fatalf("len out = %d; want 3", len(out))
	}
	if out[0].Status != ProbeOK {
		t.Errorf("out[0].Status = %v; want ProbeOK", out[0].Status)
	}
	if out[1].Status != ProbeWarn {
		t.Errorf("out[1].Status = %v; want ProbeWarn", out[1].Status)
	}
	if out[2].Status != ProbeFail {
		t.Errorf("out[2].Status = %v; want ProbeFail", out[2].Status)
	}
}

func TestBuildDoctorFullConfig_PopulatesAdapters(t *testing.T) {
	cfg := buildDoctorFullConfig()
	if len(cfg.Plan1To9Adapters) < 12 {
		t.Errorf("buildDoctorFullConfig Plan1To9Adapters = %d; want ≥12", len(cfg.Plan1To9Adapters))
	}
	if cfg.AuditEmitter == nil {
		t.Errorf("AuditEmitter nil; production wiring should populate DaemonAuditEmitter")
	}
	if cfg.FixEmitter == nil {
		t.Errorf("FixEmitter nil; production wiring should populate")
	}
	if cfg.Plan13FixAppliers == nil {
		t.Errorf("Plan13FixAppliers nil; production wiring should populate 3 Fix impls")
	}
	if cfg.RecoverableSentinel == nil {
		t.Errorf("RecoverableSentinel nil; should be ErrRecoverable")
	}

	wantedFixKeys := []string{"hermes.install", "mcp.curated-availability", "hermes.plugin-format"}
	for _, k := range wantedFixKeys {
		if _, ok := cfg.Plan13FixAppliers[k]; !ok {
			t.Errorf("Plan13FixAppliers missing key %q", k)
		}
	}
}
