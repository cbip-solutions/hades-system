package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

func fakeDaemonHandler(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/v1/health"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":         "ok",
				"version":        "0.7.0-dev",
				"uptime_seconds": 3600,
			})
		case strings.HasSuffix(r.URL.Path, "/v1/projects"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"projects": []map[string]any{
					{
						"id":               "a1b2c3d400000000000000000000000000000000000000000000000000000000",
						"alias":            "internal-platform-x",
						"path":             "/x/internal-platform-x",
						"autonomous_state": "active",
					},
					{
						"id":               "b2c3d4e500000000000000000000000000000000000000000000000000000000",
						"alias":            "zen-swarm",
						"path":             "/x/zen-swarm",
						"autonomous_state": "active",
					},
				},
			})
		case strings.HasPrefix(r.URL.Path, "/v1/projects/") && strings.HasSuffix(r.URL.Path, "/doctor"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"items": []map[string]any{
					{"aspect": "identity.alias.resolved", "status": "ok", "message": "resolved", "detail": "", "hint": ""},
					{"aspect": "quota.thresholds.applied", "status": "ok", "message": "max-scope: 80%/100%", "detail": "", "hint": ""},
				},
			})
		case strings.HasSuffix(r.URL.Path, "/v1/meta/snapshot"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"panics_last_24h":      0,
				"cost_utilization_pct": 18,
			})
		case strings.HasPrefix(r.URL.Path, "/v1/bypass/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "ok",
				"detail": "verified",
			})
		default:

			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "ok",
				"detail": "verified",
			})
		}
	})
}

func integrationProbersAllOK() (
	*fakeKnowledgeProber, *fakeSchedulerProber, *fakeInboxProber, *fakeTmuxProber,
) {
	return &fakeKnowledgeProber{
			integrity:        "ok",
			lastIndexedAgo:   2 * time.Minute,
			cpuBudgetPct:     20,
			cpuBudgetWarn:    50,
			cpuBudgetFail:    80,
			watcherHeartbeat: time.Now().Add(-5 * time.Second),
			extNullCount:     12,
			extTotalCount:    12,
		}, &fakeSchedulerProber{
			queueDepth:   2,
			wfqMaxPct:    40,
			wfqMaxAlias:  "internal-platform-x",
			dispatcherOK: true,
		}, &fakeInboxProber{
			cacheConsistent: true,
			outboxDepth:     5,
			dedupViolations: 0,
			severityDist:    map[string]int{"info-digest": 8},
		}, &fakeTmuxProber{
			versionInstalled: "tmux 3.5a",
			versionMeetsMin:  true,
			serverReachable:  true,
			sessionCount:     2,
			driftCount:       0,
			socketMode:       "0600",
		}
}

// TestRunFullProbeIntegrationAllOK asserts the all-OK end-to-end pipeline:
// every section in runFullProbeOrder appears in the output, RenderProbes
// is well-formed (contains "ok" glyph + every canonical probe name), and
// ExitCode is 0 in non-strict mode.
//
// Section presence assertion (per spec §J-10 Step 1): probe Names are
// dot-separated subsystem.aspect; we group by the head segment and assert
// every canonical section ID appears at least once. This is the orchestrator
// integration's primary contract — RunFullProbe MUST emit the full surface
// when wired.
func TestRunFullProbeIntegrationAllOK(t *testing.T) {
	srv := httptest.NewServer(fakeDaemonHandler(t))
	defer srv.Close()
	c := newClientForTest(srv.URL)
	k, s, i, m := integrationProbersAllOK()

	deps := DoctorDeps{
		Client:    c,
		Strict:    false,
		Knowledge: k,
		Scheduler: s,
		Inbox:     i,
		Tmux:      m,
	}

	probes, err := RunFullProbe(context.Background(), deps)
	if err != nil {
		t.Fatalf("RunFullProbe: %v", err)
	}
	if len(probes) == 0 {
		t.Fatalf("expected probes, got 0")
	}

	sections := map[string]int{}
	for _, p := range probes {
		head := strings.SplitN(p.Name, ".", 2)[0]
		sections[head]++
	}
	for _, want := range runFullProbeOrder {
		if sections[want] == 0 {
			t.Errorf("integration: missing section %q; section counts=%v", want, sections)
		}
	}

	rendered := RenderProbes(probes)
	if !strings.Contains(rendered, "ok  ") {
		t.Errorf("rendered output missing OK glyph (\"ok  \"):\n%s", rendered)
	}
	for _, want := range []string{
		"daemon.reachable",
		"tmux.binary.installed",
		"knowledge.index.integrity",
		"scheduler.queue.depth",
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("rendered output missing canonical probe %q\n%s", want, rendered)
		}
	}

	// Per-project doctor fan-out probe Names exceed 40 chars so are
	// truncated in rendered output. Assert presence on the Name field
	// directly via the probes slice — the orchestrator MUST emit a
	// per-project doctor row for each active project.
	wantNames := map[string]bool{
		"projects.internal-platform-x.identity.alias.resolved":  false,
		"projects.internal-platform-x.quota.thresholds.applied": false,
		"projects.zen-swarm.identity.alias.resolved":            false,
		"projects.zen-swarm.quota.thresholds.applied":           false,
		"inbox.aggregator.cache.consistent":                     false,
	}
	for _, p := range probes {
		if _, ok := wantNames[p.Name]; ok {
			wantNames[p.Name] = true
		}
	}
	for n, seen := range wantNames {
		if !seen {
			t.Errorf("orchestrator missing canonical probe Name %q (got: %v)", n, namesOf(probes))
		}
	}

	if code := ExitCode(probes, false); code != 0 {
		t.Errorf("ExitCode = %d, want 0 (all OK in non-strict mode)", code)
	}
}

func TestRunFullProbeIntegrationOneSubsystemFail(t *testing.T) {
	srv := httptest.NewServer(fakeDaemonHandler(t))
	defer srv.Close()
	c := newClientForTest(srv.URL)
	_, s, i, m := integrationProbersAllOK()

	failingKnowledge := &fakeKnowledgeProber{

		integrity:        "*** corruption: row 1 missing ***",
		lastIndexedAgo:   2 * time.Minute,
		cpuBudgetPct:     20,
		cpuBudgetWarn:    50,
		cpuBudgetFail:    80,
		watcherHeartbeat: time.Now().Add(-5 * time.Second),

		extNullCount:  1,
		extTotalCount: 1,
	}

	deps := DoctorDeps{
		Client:    c,
		Knowledge: failingKnowledge,
		Scheduler: s,
		Inbox:     i,
		Tmux:      m,
	}

	probes, err := RunFullProbe(context.Background(), deps)
	if err != nil {
		t.Fatalf("RunFullProbe: %v", err)
	}

	if code := ExitCode(probes, false); code != 1 {
		t.Errorf("ExitCode = %d, want 1 (knowledge integrity Fail)", code)
	}

	rendered := RenderProbes(probes)
	if !strings.Contains(rendered, "x    knowledge.index.integrity") {
		t.Errorf("rendered missing fail glyph for integrity probe:\n%s", rendered)
	}

	if !strings.Contains(rendered, "      *** corruption: row 1 missing ***") {
		t.Errorf("rendered missing corruption detail (with 6-col indent):\n%s", rendered)
	}
}

func TestRunFullProbeIntegrationDaemonDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	c := newClientForTest(srv.URL)
	k, s, i, m := integrationProbersAllOK()

	deps := DoctorDeps{
		Client:    c,
		Knowledge: k,
		Scheduler: s,
		Inbox:     i,
		Tmux:      m,
	}

	probes, err := RunFullProbe(context.Background(), deps)
	if err != nil {
		t.Fatalf("RunFullProbe: %v", err)
	}
	if len(probes) != 1 {
		t.Fatalf("daemon down should stop early with 1 probe, got %d: %v", len(probes), namesOf(probes))
	}
	if probes[0].Status != ProbeFail {
		t.Errorf("probe[0].Status = %v, want Fail", probes[0].Status)
	}
	if !strings.HasPrefix(probes[0].Name, "daemon.") {
		t.Errorf("probe[0].Name = %q, want prefix daemon.", probes[0].Name)
	}
	if code := ExitCode(probes, false); code != 1 {
		t.Errorf("ExitCode = %d, want 1 (daemon down)", code)
	}
}

func TestRenderProbesGoldenLikeIntegration(t *testing.T) {
	probes := []ProbeResult{
		{Name: "daemon.reachable", Status: ProbeOK, Message: "uptime 1h"},
		{Name: "tmux.server.reachable", Status: ProbeWarn, Message: "1 drift", Hint: "see: zen sessions ls"},
		{Name: "knowledge.index.integrity", Status: ProbeFail, Message: "corruption", Detail: "row 1 missing\nrow 2 missing", Hint: "run: zen knowledge reindex --full"},
	}
	out := RenderProbes(probes)
	lines := strings.Split(out, "\n")

	hasOK, hasWarn, hasFail := false, false, false
	for _, line := range lines {
		if strings.Contains(line, "ok  ") {
			hasOK = true
		}
		if strings.Contains(line, "warn") {
			hasWarn = true
		}
		if strings.Contains(line, "x   ") {
			hasFail = true
		}
	}
	if !hasOK || !hasWarn || !hasFail {
		t.Errorf("missing glyphs OK=%v warn=%v fail=%v in output:\n%s", hasOK, hasWarn, hasFail, out)
	}

	if !strings.Contains(out, "      hint: see: zen sessions ls") {
		t.Errorf("warn hint not indented to 6 cols / not prefixed with \"hint: \":\n%s", out)
	}
	if !strings.Contains(out, "      hint: run: zen knowledge reindex --full") {
		t.Errorf("fail hint not indented to 6 cols / not prefixed with \"hint: \":\n%s", out)
	}

	if !strings.Contains(out, "      row 1 missing") {
		t.Errorf("detail line 1 not indented to 6 cols:\n%s", out)
	}
	if !strings.Contains(out, "      row 2 missing") {
		t.Errorf("detail line 2 not indented to 6 cols:\n%s", out)
	}
}

func TestInvokeKnowledgeProberOrchestrationErrorEmitsFail(t *testing.T) {
	prev := runKnowledgeProbeFunc
	runKnowledgeProbeFunc = func(ctx context.Context, p KnowledgeProber) ([]ProbeResult, error) {
		return nil, context.DeadlineExceeded
	}
	t.Cleanup(func() { runKnowledgeProbeFunc = prev })

	probes := invokeKnowledgeProber(context.Background(), &fakeKnowledgeProber{})
	if len(probes) != 1 {
		t.Fatalf("expected 1 Fail probe from orchestration-error branch, got %d: %v", len(probes), namesOf(probes))
	}
	got := probes[0]
	if got.Name != "knowledge.probe.error" {
		t.Errorf("Name = %q, want knowledge.probe.error", got.Name)
	}
	if got.Status != ProbeFail {
		t.Errorf("Status = %v, want Fail", got.Status)
	}
	if !strings.Contains(got.Detail, context.DeadlineExceeded.Error()) {
		t.Errorf("Detail = %q, want to contain %q", got.Detail, context.DeadlineExceeded.Error())
	}
}

func TestInvokeSchedulerProberOrchestrationErrorEmitsFail(t *testing.T) {
	prev := runSchedulerProbeFunc
	runSchedulerProbeFunc = func(ctx context.Context, p SchedulerProber) ([]ProbeResult, error) {
		return nil, context.DeadlineExceeded
	}
	t.Cleanup(func() { runSchedulerProbeFunc = prev })

	probes := invokeSchedulerProber(context.Background(), &fakeSchedulerProber{})
	if len(probes) != 1 || probes[0].Name != "scheduler.probe.error" || probes[0].Status != ProbeFail {
		t.Errorf("invokeSchedulerProber error branch wrong: %+v", probes)
	}
}

func TestInvokeInboxProberOrchestrationErrorEmitsFail(t *testing.T) {
	prev := runInboxProbeFunc
	runInboxProbeFunc = func(ctx context.Context, p InboxProber) ([]ProbeResult, error) {
		return nil, context.DeadlineExceeded
	}
	t.Cleanup(func() { runInboxProbeFunc = prev })

	probes := invokeInboxProber(context.Background(), &fakeInboxProber{})
	if len(probes) != 1 || probes[0].Name != "inbox.probe.error" || probes[0].Status != ProbeFail {
		t.Errorf("invokeInboxProber error branch wrong: %+v", probes)
	}
}

func TestInvokeTmuxProberOrchestrationErrorEmitsFail(t *testing.T) {
	prev := runTmuxProbeFunc
	runTmuxProbeFunc = func(ctx context.Context, p TmuxProber) ([]ProbeResult, error) {
		return nil, context.DeadlineExceeded
	}
	t.Cleanup(func() { runTmuxProbeFunc = prev })

	probes := invokeTmuxProber(context.Background(), &fakeTmuxProber{})
	if len(probes) != 1 || probes[0].Name != "tmux.probe.error" || probes[0].Status != ProbeFail {
		t.Errorf("invokeTmuxProber error branch wrong: %+v", probes)
	}
}

func TestResolveDoctorFlagsLocalPersistent(t *testing.T) {

	root := &cobra.Command{Use: "synthetic-root"}
	leaf := &cobra.Command{Use: "synthetic-leaf"}
	leaf.PersistentFlags().String("uds", "/var/run/synthetic.sock", "test override")
	root.AddCommand(leaf)

	uds, strict := resolveDoctorFlags(leaf)
	if uds != "/var/run/synthetic.sock" {
		t.Errorf("local-PersistentFlags fallback: uds = %q, want /var/run/synthetic.sock", uds)
	}
	if strict {
		t.Errorf("strict default = true, want false")
	}
}

// TestResolveDoctorFlagsParentWalk covers the parent-traversal --uds
// branch in resolveDoctorFlags (lines 71-76). Setup: a synthetic chain
// root → mid → leaf, with --uds set ONLY on `mid`. The function MUST
// walk parents and pick up mid's value.
//
// Coverage rationale: companion to TestResolveDoctorFlagsLocalPersistent;
// both close out the 86.7% gap on resolveDoctorFlags.
func TestResolveDoctorFlagsParentWalk(t *testing.T) {
	root := &cobra.Command{Use: "synthetic-root"}
	mid := &cobra.Command{Use: "synthetic-mid"}
	mid.PersistentFlags().String("uds", "/var/run/synthetic-mid.sock", "test override")
	leaf := &cobra.Command{Use: "synthetic-leaf"}
	root.AddCommand(mid)
	mid.AddCommand(leaf)

	uds, _ := resolveDoctorFlags(leaf)
	if uds != "/var/run/synthetic-mid.sock" {
		t.Errorf("parent-walk: uds = %q, want /var/run/synthetic-mid.sock", uds)
	}
}

func TestResolveDoctorFlagsStrictTrue(t *testing.T) {
	root := NewDoctorCmd()
	if err := root.PersistentFlags().Set("strict", "true"); err != nil {
		t.Fatalf("Set strict: %v", err)
	}
	sub := findDoctorSubcommand(root, "knowledge")
	if sub == nil {
		t.Fatal("knowledge subcommand not registered")
	}
	_, strict := resolveDoctorFlags(sub)
	if !strict {
		t.Error("resolveDoctorFlags strict = false; want true after Set(strict, true)")
	}
}

func TestBuildDoctorDepsFuncErrorPropagatesToSubcommand(t *testing.T) {
	prev := buildDoctorDepsFunc
	sentinel := context.DeadlineExceeded
	buildDoctorDepsFunc = func(udsPath string, strict bool) (DoctorDeps, error) {
		return DoctorDeps{}, sentinel
	}
	t.Cleanup(func() { buildDoctorDepsFunc = prev })

	cmd := NewDoctorCmd()
	for _, name := range []string{"knowledge", "scheduler", "inbox", "tmux"} {
		sub := findDoctorSubcommand(cmd, name)
		if sub == nil {
			t.Fatalf("%s subcommand not registered", name)
		}
		err := sub.RunE(sub, nil)
		if err == nil {
			t.Errorf("%s: expected error from buildDoctorDepsFunc, got nil", name)
		}
		if err != nil && !strings.Contains(err.Error(), sentinel.Error()) {
			t.Errorf("%s: expected sentinel %v propagated, got: %v", name, sentinel, err)
		}
	}
}

func TestNewDoctorKnowledgeCmdRunEPropagatesProbeError(t *testing.T) {
	prev := runKnowledgeProbeWithDepsFunc
	runKnowledgeProbeWithDepsFunc = func(ctx context.Context, deps DoctorDeps) ([]ProbeResult, error) {
		return nil, context.DeadlineExceeded
	}
	t.Cleanup(func() { runKnowledgeProbeWithDepsFunc = prev })

	cmd := NewDoctorCmd()
	sub := findDoctorSubcommand(cmd, "knowledge")
	if sub == nil {
		t.Fatal("knowledge subcommand not registered")
	}
	err := sub.RunE(sub, nil)
	if err == nil {
		t.Error("expected error propagated from runKnowledgeProbeWithDepsFunc, got nil")
	}
	if !strings.Contains(err.Error(), context.DeadlineExceeded.Error()) {
		t.Errorf("expected DeadlineExceeded propagated, got: %v", err)
	}
}

func TestNewDoctorSchedulerCmdRunEPropagatesProbeError(t *testing.T) {
	prev := runSchedulerProbeWithDepsFunc
	runSchedulerProbeWithDepsFunc = func(ctx context.Context, deps DoctorDeps) ([]ProbeResult, error) {
		return nil, context.DeadlineExceeded
	}
	t.Cleanup(func() { runSchedulerProbeWithDepsFunc = prev })

	cmd := NewDoctorCmd()
	sub := findDoctorSubcommand(cmd, "scheduler")
	if sub == nil {
		t.Fatal("scheduler subcommand not registered")
	}
	err := sub.RunE(sub, nil)
	if err == nil {
		t.Error("expected error propagated, got nil")
	}
}

func TestNewDoctorInboxCmdRunEPropagatesProbeError(t *testing.T) {
	prev := runInboxProbeWithDepsFunc
	runInboxProbeWithDepsFunc = func(ctx context.Context, deps DoctorDeps) ([]ProbeResult, error) {
		return nil, context.DeadlineExceeded
	}
	t.Cleanup(func() { runInboxProbeWithDepsFunc = prev })

	cmd := NewDoctorCmd()
	sub := findDoctorSubcommand(cmd, "inbox")
	if sub == nil {
		t.Fatal("inbox subcommand not registered")
	}
	err := sub.RunE(sub, nil)
	if err == nil {
		t.Error("expected error propagated, got nil")
	}
}

func TestNewDoctorTmuxCmdRunEPropagatesProbeError(t *testing.T) {
	prev := runTmuxProbeWithDepsFunc
	runTmuxProbeWithDepsFunc = func(ctx context.Context, deps DoctorDeps) ([]ProbeResult, error) {
		return nil, context.DeadlineExceeded
	}
	t.Cleanup(func() { runTmuxProbeWithDepsFunc = prev })

	cmd := NewDoctorCmd()
	sub := findDoctorSubcommand(cmd, "tmux")
	if sub == nil {
		t.Fatal("tmux subcommand not registered")
	}
	err := sub.RunE(sub, nil)
	if err == nil {
		t.Error("expected error propagated, got nil")
	}
}

func TestDoctorAggregateRunEFlagConflictReturnsError(t *testing.T) {
	cmd := NewDoctorCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"--quiet", "--verbose"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error from --quiet + --verbose conflict, got nil")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("expected mutually-exclusive error, got: %v", err)
	}
}

func TestRunFullProbeIntegrationStrictPromotesWarn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/v1/health"):
			_, _ = w.Write([]byte(`{"status":"ok","version":"0.7.0-dev","uptime_seconds":1}`))
		case strings.HasSuffix(r.URL.Path, "/v1/projects"):
			_, _ = w.Write([]byte(`{"projects":[]}`))
		case strings.HasSuffix(r.URL.Path, "/v1/meta/snapshot"):

			_, _ = w.Write([]byte(`{"panics_last_24h":0,"cost_utilization_pct":85}`))
		default:
			_, _ = w.Write([]byte(`{"status":"ok","detail":"verified"}`))
		}
	}))
	defer srv.Close()
	c := newClientForTest(srv.URL)
	k, s, i, m := integrationProbersAllOK()

	deps := DoctorDeps{
		Client:    c,
		Knowledge: k,
		Scheduler: s,
		Inbox:     i,
		Tmux:      m,
	}

	probes, err := RunFullProbe(context.Background(), deps)
	if err != nil {
		t.Fatalf("RunFullProbe: %v", err)
	}

	if code := ExitCode(probes, false); code != 0 {
		t.Errorf("ExitCode(non-strict) = %d, want 0 (only Warn, no Fail)", code)
	}
	if code := ExitCode(probes, true); code != 1 {
		t.Errorf("ExitCode(strict) = %d, want 1 (Warn promoted)", code)
	}
}
