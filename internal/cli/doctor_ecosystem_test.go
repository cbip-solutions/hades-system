package cli

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

type fakeEcosystemProberG4 struct {
	results []ProbeResult
}

func (f *fakeEcosystemProberG4) Probe(_ context.Context) []ProbeResult {
	return f.results
}

var _ EcosystemProber = (*fakeEcosystemProberG4)(nil)

// TestRunEcosystemProbe_NilProber_Error asserts a nil EcosystemProber returns
// a clear "mis-wired deps" error rather than a panic. The error message MUST
// mention "EcosystemProber" so the operator's first remediation step is to
// look at the named field.
func TestRunEcosystemProbe_NilProber_Error(t *testing.T) {
	deps := DoctorDeps{EcosystemProber: nil}
	_, err := RunEcosystemProbe(context.Background(), deps)
	if err == nil {
		t.Fatal("expected error when EcosystemProber is nil")
	}
	if !strings.Contains(err.Error(), "EcosystemProber") {
		t.Errorf("error should mention EcosystemProber; got %q", err.Error())
	}
}

func TestRunEcosystemProbe_CancelledContext_Error(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	deps := DoctorDeps{EcosystemProber: &fakeEcosystemProberG4{}}
	_, err := RunEcosystemProbe(ctx, deps)
	if err == nil {
		t.Fatal("expected error on cancelled context")
	}
}

func TestRunEcosystemProbe_ReturnsProbeResults(t *testing.T) {
	expected := []ProbeResult{
		{Name: "ecosystem.go.db_size", Status: ProbeOK, Message: "go.db 12.3 GB"},
		{Name: "ecosystem.budget", Status: ProbeWarn, Message: "Yellow: 35.2 GB / 40 GB target (88%)"},
		{Name: "ecosystem.cron.pid", Status: ProbeOK, Message: "pid=4321"},
	}
	deps := DoctorDeps{
		EcosystemProber: &fakeEcosystemProberG4{results: expected},
	}
	got, err := RunEcosystemProbe(context.Background(), deps)
	if err != nil {
		t.Fatalf("RunEcosystemProbe: %v", err)
	}
	if len(got) != len(expected) {
		t.Fatalf("want %d results, got %d", len(expected), len(got))
	}
	for i, r := range got {
		if r.Name != expected[i].Name {
			t.Errorf("[%d] Name: want %q, got %q", i, expected[i].Name, r.Name)
		}
		if r.Status != expected[i].Status {
			t.Errorf("[%d] Status: want %v, got %v", i, expected[i].Status, r.Status)
		}
	}
}

func TestRunEcosystemProbe_AllOK(t *testing.T) {
	deps := DoctorDeps{
		EcosystemProber: &fakeEcosystemProberG4{
			results: []ProbeResult{
				{Name: "ecosystem.go.db_size", Status: ProbeOK, Message: "go.db 12.3 GB"},
				{Name: "ecosystem.python.db_size", Status: ProbeOK, Message: "python.db 8.1 GB"},
				{Name: "ecosystem.budget", Status: ProbeOK, Message: "Green: 24 GB / 40 GB target (60%)"},
			},
		},
	}
	probes, err := RunEcosystemProbe(context.Background(), deps)
	if err != nil {
		t.Fatalf("RunEcosystemProbe: %v", err)
	}
	if code := ExitCode(probes, false); code != 0 {
		t.Errorf("ExitCode = %d, want 0 (all OK)", code)
	}
}

func TestRunEcosystemProbe_DegradedWarn(t *testing.T) {
	deps := DoctorDeps{
		EcosystemProber: &fakeEcosystemProberG4{
			results: []ProbeResult{
				{Name: "ecosystem.budget", Status: ProbeWarn, Message: "Yellow: 35.2 GB / 40 GB target"},
				{Name: "ecosystem.cron.pid", Status: ProbeOK, Message: "pid=4321"},
			},
		},
	}
	probes, err := RunEcosystemProbe(context.Background(), deps)
	if err != nil {
		t.Fatalf("RunEcosystemProbe: %v", err)
	}
	if code := ExitCode(probes, false); code != 0 {
		t.Errorf("ExitCode warn (non-strict) = %d, want 0", code)
	}
	if code := ExitCode(probes, true); code != 1 {
		t.Errorf("ExitCode warn (strict) = %d, want 1", code)
	}
}

func TestRunEcosystemProbe_BrokenFail(t *testing.T) {
	deps := DoctorDeps{
		EcosystemProber: &fakeEcosystemProberG4{
			results: []ProbeResult{
				{
					Name:    "ecosystem.budget",
					Status:  ProbeFail,
					Message: "Overflow: 62 GB ≥ 60 GB ceiling — all writes blocked",
					Hint:    "zen docs prune --ecosystem <X> --version <Y> --confirm",
				},
				{Name: "ecosystem.cron.pid", Status: ProbeOK, Message: "pid=4321"},
			},
		},
	}
	probes, err := RunEcosystemProbe(context.Background(), deps)
	if err != nil {
		t.Fatalf("RunEcosystemProbe: %v", err)
	}
	if code := ExitCode(probes, false); code != 1 {
		t.Errorf("ExitCode any-Fail = %d, want 1", code)
	}
}

func TestRunEcosystemProbe_RespectsTimeout(t *testing.T) {
	slow := &slowEcosystemProberG4{delay: 100 * time.Millisecond}
	deps := DoctorDeps{EcosystemProber: slow}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	_, err := RunEcosystemProbe(ctx, deps)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if elapsed > 3*time.Second {
		t.Errorf("probe took too long: %v (want < 3s)", elapsed)
	}
}

type slowEcosystemProberG4 struct {
	delay time.Duration
}

func (s *slowEcosystemProberG4) Probe(ctx context.Context) []ProbeResult {
	select {
	case <-ctx.Done():
		return []ProbeResult{{Name: "ecosystem.timeout", Status: ProbeFail, Message: ctx.Err().Error()}}
	case <-time.After(s.delay):
		return []ProbeResult{{Name: "ecosystem.ok", Status: ProbeOK}}
	}
}

// TestRunFullProbe_NilEcosystemProber_DoesNotPanic guards the doctor RunFullProbe
// path against accidental panic on a deps with EcosystemProber=nil. Phase G
// does NOT yet wire ecosystem into RunFullProbe (G-4 ships only the leaf
// subcommand), but the field MUST coexist with the rest of DoctorDeps so the
// future RunFullProbe extension lands without a struct-shape mismatch.
func TestRunFullProbe_NilEcosystemProber_DoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("RunEcosystemProbe panicked with nil EcosystemProber: %v", r)
		}
	}()

	deps := DoctorDeps{
		EcosystemProber: nil,
	}
	ctx := context.Background()
	_, err := RunEcosystemProbe(ctx, deps)
	if err == nil {
		t.Error("expected non-nil error for nil prober")
	}

}

func TestDoctorEcosystem_OutputCoversRequiredFields(t *testing.T) {
	requiredNames := []string{
		"ecosystem.go.db_size",
		"ecosystem.python.db_size",
		"ecosystem.typescript.db_size",
		"ecosystem.rust.db_size",
		"ecosystem.budget",
		"ecosystem.cas_blobs_shared",
		"ecosystem.last_upstream_poll",
		"ecosystem.last_weekly_sweep",
		"ecosystem.cron.pid",
		"ecosystem.symbol_index.count",
		"ecosystem.symbol_index.last_rebuild",
		"ecosystem.verifier.go",
		"ecosystem.verifier.python",
		"ecosystem.verifier.npm",
		"ecosystem.verifier.cargo",
	}

	results := make([]ProbeResult, len(requiredNames))
	for i, name := range requiredNames {
		results[i] = ProbeResult{Name: name, Status: ProbeOK, Message: "test"}
	}
	deps := DoctorDeps{
		EcosystemProber: &fakeEcosystemProberG4{results: results},
	}

	got, err := RunEcosystemProbe(context.Background(), deps)
	if err != nil {
		t.Fatalf("RunEcosystemProbe: %v", err)
	}

	nameSet := make(map[string]bool, len(got))
	for _, r := range got {
		nameSet[r.Name] = true
	}
	for _, required := range requiredNames {
		if !nameSet[required] {
			t.Errorf("missing required probe: %q", required)
		}
	}
}

func TestDoctorEcosystem_RenderProbes_IncludesBudgetState(t *testing.T) {
	probes := []ProbeResult{
		{
			Name:    "ecosystem.budget",
			Status:  ProbeWarn,
			Message: "Yellow: 38.0 GB / 40 GB target (95%)",
			Hint:    "Prune older versions with: zen docs prune --ecosystem <X> --version <Y> --confirm",
		},
	}
	out := RenderProbes(probes)
	if !strings.Contains(out, "ecosystem.budget") {
		t.Errorf("render output missing probe name; got: %s", out)
	}
	if !strings.Contains(out, "Yellow") {
		t.Errorf("render output missing Yellow budget state; got: %s", out)
	}
	if !strings.Contains(out, "hint:") {
		t.Errorf("render output missing hint prefix; got: %s", out)
	}
}

func TestNewDoctorEcosystemCmd_UseIsEcosystem(t *testing.T) {
	cmd := NewDoctorEcosystemCmd()
	if cmd.Use != "ecosystem" {
		t.Errorf("Use: want 'ecosystem', got %q", cmd.Use)
	}
}

func TestNewDoctorEcosystemCmd_ShortDescriptionPresent(t *testing.T) {
	cmd := NewDoctorEcosystemCmd()
	if cmd.Short == "" {
		t.Error("Short description must not be empty")
	}
}

func TestNewDoctorEcosystemCmd_LongDescriptionCoversOutputFields(t *testing.T) {
	cmd := NewDoctorEcosystemCmd()
	expectedTerms := []string{
		"db_size", "budget", "cron", "symbol", "verifier",
	}
	for _, term := range expectedTerms {
		if !strings.Contains(strings.ToLower(cmd.Long), term) {
			t.Errorf("Long description should mention %q", term)
		}
	}
}

func TestNewDoctorEcosystemCmd_IsLeaf(t *testing.T) {
	cmd := NewDoctorEcosystemCmd()
	if len(cmd.Commands()) != 0 {
		t.Errorf("subcommands = %d, want 0 (leaf)", len(cmd.Commands()))
	}
}

func TestNewDoctorEcosystemCmd_HasRunE(t *testing.T) {
	cmd := NewDoctorEcosystemCmd()
	if cmd.RunE == nil {
		t.Error("RunE is nil; cobra will not dispatch the action")
	}
}

func TestNewDoctorEcosystemCmd_LongMentionsBudgetStates(t *testing.T) {
	cmd := NewDoctorEcosystemCmd()
	low := strings.ToLower(cmd.Long)
	for _, state := range []string{"green", "yellow", "red", "overflow"} {
		if !strings.Contains(low, state) {
			t.Errorf("Long should mention budget state %q", state)
		}
	}
}

func TestDoctorDeps_HasEcosystemProberField(t *testing.T) {
	deps := DoctorDeps{
		EcosystemProber: &fakeEcosystemProberG4{},
	}
	if deps.EcosystemProber == nil {
		t.Error("EcosystemProber field assignment yielded nil — struct shape regression")
	}
}

func TestNewDoctorEcosystemCmdRunE_PropagatesBuildDepsError(t *testing.T) {
	prev := buildDoctorDepsFunc
	sentinel := context.DeadlineExceeded
	buildDoctorDepsFunc = func(_ string, _ bool) (DoctorDeps, error) {
		return DoctorDeps{}, sentinel
	}
	t.Cleanup(func() { buildDoctorDepsFunc = prev })

	cmd := NewDoctorCmd()
	sub := findEcosystemSubcommand(cmd)
	if sub == nil {
		t.Fatal("ecosystem subcommand not registered under doctor")
	}
	err := sub.RunE(sub, nil)
	if err == nil {
		t.Fatal("expected error from buildDoctorDepsFunc seam, got nil")
	}
	if !strings.Contains(err.Error(), sentinel.Error()) {
		t.Errorf("expected sentinel %v propagated, got: %v", sentinel, err)
	}
}

func TestNewDoctorEcosystemCmdRunE_PropagatesProbeError(t *testing.T) {
	prev := buildDoctorDepsFunc
	buildDoctorDepsFunc = func(_ string, _ bool) (DoctorDeps, error) {

		return DoctorDeps{EcosystemProber: nil}, nil
	}
	t.Cleanup(func() { buildDoctorDepsFunc = prev })

	cmd := NewDoctorCmd()
	sub := findEcosystemSubcommand(cmd)
	if sub == nil {
		t.Fatal("ecosystem subcommand not registered under doctor")
	}
	err := sub.RunE(sub, nil)
	if err == nil {
		t.Fatal("expected error from RunEcosystemProbe via RunE, got nil")
	}
	if !strings.Contains(err.Error(), "ecosystem probe") {
		t.Errorf("expected wrap prefix 'ecosystem probe', got: %v", err)
	}
	if !strings.Contains(err.Error(), "EcosystemProber") {
		t.Errorf("expected nested EcosystemProber message, got: %v", err)
	}
}

func TestNewDoctorEcosystemCmdRunE_HappyPath(t *testing.T) {
	prev := buildDoctorDepsFunc
	buildDoctorDepsFunc = func(_ string, _ bool) (DoctorDeps, error) {
		return DoctorDeps{
			EcosystemProber: &fakeEcosystemProberG4{
				results: []ProbeResult{
					{Name: "ecosystem.go.db_size", Status: ProbeOK, Message: "go.db 12.3 GB"},
					{Name: "ecosystem.budget", Status: ProbeOK, Message: "Green: 24 GB / 40 GB target"},
					{Name: "ecosystem.cron.pid", Status: ProbeOK, Message: "pid=4321"},
				},
			},
		}, nil
	}
	t.Cleanup(func() { buildDoctorDepsFunc = prev })

	cmd := NewDoctorCmd()
	sub := findEcosystemSubcommand(cmd)
	if sub == nil {
		t.Fatal("ecosystem subcommand not registered under doctor")
	}

	var buf strings.Builder
	sub.SetOut(&buf)

	if err := sub.RunE(sub, nil); err != nil {
		t.Fatalf("RunE returned error on all-OK probes: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Ecosystem:") {
		t.Errorf("output missing section header; got: %q", out)
	}
	if !strings.Contains(out, "ecosystem.go.db_size") {
		t.Errorf("output missing probe name; got: %q", out)
	}
	if !strings.Contains(out, "Green") {
		t.Errorf("output missing budget message detail; got: %q", out)
	}
}

func findEcosystemSubcommand(root *cobra.Command) *cobra.Command {
	for _, c := range root.Commands() {
		if c.Use == "ecosystem" {
			return c
		}
	}
	return nil
}
