package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

func TestNewDoctorCmdHasPlan7Subcommands(t *testing.T) {
	cmd := NewDoctorCmd()
	want := []string{"knowledge", "scheduler", "inbox", "tmux"}
	have := map[string]bool{}
	for _, c := range cmd.Commands() {
		have[c.Use] = true
	}
	for _, w := range want {
		if !have[w] {
			t.Errorf("doctor missing Plan 7 subcommand %q; have %v", w, mapKeysSorted(have))
		}
	}
}

func TestNewDoctorCmdHasStrictFlag(t *testing.T) {
	cmd := NewDoctorCmd()
	f := cmd.PersistentFlags().Lookup("strict")
	if f == nil {
		t.Error("doctor missing persistent --strict flag (Plan 7 J-7)")
	}
}

func TestRunKnowledgeProbeWithDepsNilProberWarns(t *testing.T) {
	probes, err := RunKnowledgeProbeWithDeps(context.Background(), DoctorDeps{Knowledge: nil})
	if err != nil {
		t.Fatalf("RunKnowledgeProbeWithDeps: %v", err)
	}
	if len(probes) != 1 {
		t.Fatalf("expected 1 probe (nil-Warn no-op), got %d", len(probes))
	}
	if probes[0].Status != ProbeWarn {
		t.Errorf("nil-Prober probe status = %v, want Warn", probes[0].Status)
	}
}

func TestRunSchedulerProbeWithDepsNilProberWarns(t *testing.T) {
	probes, err := RunSchedulerProbeWithDeps(context.Background(), DoctorDeps{Scheduler: nil})
	if err != nil {
		t.Fatalf("RunSchedulerProbeWithDeps: %v", err)
	}
	if len(probes) != 1 {
		t.Fatalf("expected 1 probe, got %d", len(probes))
	}
	if probes[0].Status != ProbeWarn {
		t.Errorf("status = %v, want Warn", probes[0].Status)
	}
}

func TestRunInboxProbeWithDepsNilProberWarns(t *testing.T) {
	probes, err := RunInboxProbeWithDeps(context.Background(), DoctorDeps{Inbox: nil})
	if err != nil {
		t.Fatalf("RunInboxProbeWithDeps: %v", err)
	}
	if len(probes) != 1 {
		t.Fatalf("expected 1 probe, got %d", len(probes))
	}
	if probes[0].Status != ProbeWarn {
		t.Errorf("status = %v, want Warn", probes[0].Status)
	}
}

func TestRunTmuxProbeWithDepsNilProberWarns(t *testing.T) {
	probes, err := RunTmuxProbeWithDeps(context.Background(), DoctorDeps{Tmux: nil})
	if err != nil {
		t.Fatalf("RunTmuxProbeWithDeps: %v", err)
	}
	if len(probes) != 1 {
		t.Fatalf("expected 1 probe, got %d", len(probes))
	}
	if probes[0].Status != ProbeWarn {
		t.Errorf("status = %v, want Warn", probes[0].Status)
	}
}

func TestRunKnowledgeProbeWithDepsRealProberInvokes(t *testing.T) {
	deps := DoctorDeps{Knowledge: &fakeKnowledgeProber{
		integrity:        "ok",
		lastIndexedAgo:   0,
		cpuBudgetPct:     10,
		cpuBudgetWarn:    50,
		cpuBudgetFail:    90,
		watcherHeartbeat: time.Now().Add(-5 * time.Second),
		extNullCount:     5,
		extTotalCount:    5,
	}}
	probes, err := RunKnowledgeProbeWithDeps(context.Background(), deps)
	if err != nil {
		t.Fatalf("RunKnowledgeProbeWithDeps: %v", err)
	}
	if len(probes) <= 1 {
		t.Errorf("expected >1 probes from real Prober, got %d", len(probes))
	}
}

func TestRunSchedulerProbeWithDepsRealProberInvokes(t *testing.T) {
	deps := DoctorDeps{Scheduler: &fakeSchedulerProber{
		queueDepth:   0,
		missedFires:  0,
		wfqMaxPct:    0,
		dispatcherOK: true,
	}}
	probes, err := RunSchedulerProbeWithDeps(context.Background(), deps)
	if err != nil {
		t.Fatalf("RunSchedulerProbeWithDeps: %v", err)
	}
	if len(probes) <= 1 {
		t.Errorf("expected >1 probes from real Prober, got %d", len(probes))
	}
}

func TestRunInboxProbeWithDepsRealProberInvokes(t *testing.T) {
	deps := DoctorDeps{Inbox: &fakeInboxProber{
		cacheConsistent: true,
		outboxDepth:     0,
		dedupViolations: 0,
		severityDist:    map[string]int{"info-digest": 1},
	}}
	probes, err := RunInboxProbeWithDeps(context.Background(), deps)
	if err != nil {
		t.Fatalf("RunInboxProbeWithDeps: %v", err)
	}
	if len(probes) <= 1 {
		t.Errorf("expected >1 probes from real Prober, got %d", len(probes))
	}
}

func TestRunTmuxProbeWithDepsRealProberInvokes(t *testing.T) {
	deps := DoctorDeps{Tmux: &fakeTmuxProber{
		versionInstalled: "tmux 3.4",
		versionMeetsMin:  true,
		serverReachable:  true,
		sessionCount:     0,
		driftCount:       0,
		socketMode:       "0600",
	}}
	probes, err := RunTmuxProbeWithDeps(context.Background(), deps)
	if err != nil {
		t.Fatalf("RunTmuxProbeWithDeps: %v", err)
	}
	if len(probes) <= 1 {
		t.Errorf("expected >1 probes from real Prober, got %d", len(probes))
	}
}

func TestRenderProbesEmitsToWriter(t *testing.T) {
	var buf bytes.Buffer
	probes := []ProbeResult{
		{Name: "x.y", Status: ProbeOK, Message: "ok"},
	}
	out := RenderProbes(probes)
	buf.WriteString(out)
	if !strings.Contains(buf.String(), "x.y") {
		t.Errorf("buffer missing expected name: %q", buf.String())
	}
}

func TestNewDoctorKnowledgeCmdRunEEmitsProbeRow(t *testing.T) {
	cmd := NewDoctorCmd()
	sub := findDoctorSubcommand(cmd, "knowledge")
	if sub == nil {
		t.Fatal("knowledge subcommand not registered — wiring regression")
	}
	var buf bytes.Buffer
	sub.SetOut(&buf)
	sub.SetErr(&buf)

	_ = sub.RunE(sub, nil)
	got := buf.String()
	if !strings.Contains(got, "knowledge") {
		t.Errorf("expected knowledge.* line in output, got %q", got)
	}
}

func TestNewDoctorSchedulerCmdRunEEmitsProbeRow(t *testing.T) {
	cmd := NewDoctorCmd()
	sub := findDoctorSubcommand(cmd, "scheduler")
	if sub == nil {
		t.Fatal("scheduler subcommand not registered")
	}
	var buf bytes.Buffer
	sub.SetOut(&buf)
	sub.SetErr(&buf)
	_ = sub.RunE(sub, nil)
	got := buf.String()
	if !strings.Contains(got, "scheduler") {
		t.Errorf("expected scheduler.* line in output, got %q", got)
	}
}

func TestNewDoctorInboxCmdRunEEmitsProbeRow(t *testing.T) {
	cmd := NewDoctorCmd()
	sub := findDoctorSubcommand(cmd, "inbox")
	if sub == nil {
		t.Fatal("inbox subcommand not registered")
	}
	var buf bytes.Buffer
	sub.SetOut(&buf)
	sub.SetErr(&buf)
	_ = sub.RunE(sub, nil)
	got := buf.String()
	if !strings.Contains(got, "inbox") {
		t.Errorf("expected inbox.* line in output, got %q", got)
	}
}

func TestNewDoctorTmuxCmdRunEEmitsProbeRow(t *testing.T) {
	cmd := NewDoctorCmd()
	sub := findDoctorSubcommand(cmd, "tmux")
	if sub == nil {
		t.Fatal("tmux subcommand not registered")
	}
	var buf bytes.Buffer
	sub.SetOut(&buf)
	sub.SetErr(&buf)
	_ = sub.RunE(sub, nil)
	got := buf.String()
	if !strings.Contains(got, "tmux") {
		t.Errorf("expected tmux.* line in output, got %q", got)
	}
}

func TestBuildDoctorDepsReturnsClient(t *testing.T) {
	deps, err := buildDoctorDeps("/tmp/zen-swarm.sock", false)
	if err != nil {
		t.Fatalf("buildDoctorDeps: %v", err)
	}
	if deps.Client == nil {
		t.Error("buildDoctorDeps returned nil Client; production composition root must wire one")
	}
	if deps.Strict {
		t.Error("buildDoctorDeps(strict=false) returned Strict=true")
	}
}

func TestBuildDoctorDepsHonoursStrictFlag(t *testing.T) {
	deps, err := buildDoctorDeps("/tmp/zen-swarm.sock", true)
	if err != nil {
		t.Fatalf("buildDoctorDeps: %v", err)
	}
	if !deps.Strict {
		t.Error("buildDoctorDeps(strict=true) returned Strict=false")
	}
}

func findDoctorSubcommand(parent *cobra.Command, name string) *cobra.Command {
	for _, c := range parent.Commands() {
		if c.Use == name {
			return c
		}
	}
	return nil
}

func TestNewDoctorKnowledgeCmdRunEFailsUnderStrict(t *testing.T) {
	cmd := NewDoctorCmd()

	if err := cmd.PersistentFlags().Set("strict", "true"); err != nil {
		t.Fatalf("Set strict: %v", err)
	}
	sub := findDoctorSubcommand(cmd, "knowledge")
	if sub == nil {
		t.Fatal("knowledge subcommand not registered")
	}
	var buf bytes.Buffer
	sub.SetOut(&buf)
	sub.SetErr(&buf)
	err := sub.RunE(sub, nil)
	if err == nil {
		t.Error("expected non-nil error when --strict promotes Warn to Fail")
	}
	if !strings.Contains(buf.String(), "knowledge") {
		t.Errorf("expected knowledge.* line in output, got %q", buf.String())
	}
}

func TestNewDoctorSchedulerCmdRunEFailsUnderStrict(t *testing.T) {
	cmd := NewDoctorCmd()
	if err := cmd.PersistentFlags().Set("strict", "true"); err != nil {
		t.Fatalf("Set strict: %v", err)
	}
	sub := findDoctorSubcommand(cmd, "scheduler")
	if sub == nil {
		t.Fatal("scheduler subcommand not registered")
	}
	var buf bytes.Buffer
	sub.SetOut(&buf)
	sub.SetErr(&buf)
	err := sub.RunE(sub, nil)
	if err == nil {
		t.Error("expected non-nil error when --strict promotes Warn to Fail")
	}
}

func TestNewDoctorInboxCmdRunEFailsUnderStrict(t *testing.T) {
	cmd := NewDoctorCmd()
	if err := cmd.PersistentFlags().Set("strict", "true"); err != nil {
		t.Fatalf("Set strict: %v", err)
	}
	sub := findDoctorSubcommand(cmd, "inbox")
	if sub == nil {
		t.Fatal("inbox subcommand not registered")
	}
	var buf bytes.Buffer
	sub.SetOut(&buf)
	sub.SetErr(&buf)
	err := sub.RunE(sub, nil)
	if err == nil {
		t.Error("expected non-nil error when --strict promotes Warn to Fail")
	}
}

func TestNewDoctorTmuxCmdRunEFailsUnderStrict(t *testing.T) {
	cmd := NewDoctorCmd()
	if err := cmd.PersistentFlags().Set("strict", "true"); err != nil {
		t.Fatalf("Set strict: %v", err)
	}
	sub := findDoctorSubcommand(cmd, "tmux")
	if sub == nil {
		t.Fatal("tmux subcommand not registered")
	}
	var buf bytes.Buffer
	sub.SetOut(&buf)
	sub.SetErr(&buf)
	err := sub.RunE(sub, nil)
	if err == nil {
		t.Error("expected non-nil error when --strict promotes Warn to Fail")
	}
}

func TestResolveDoctorFlagsDefaults(t *testing.T) {
	sub := NewDoctorKnowledgeCmd()
	uds, strict := resolveDoctorFlags(sub)
	if uds != "/tmp/zen-swarm.sock" {
		t.Errorf("default uds = %q, want /tmp/zen-swarm.sock", uds)
	}
	if strict {
		t.Errorf("default strict = %v, want false", strict)
	}
}

func mapKeysSorted(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
