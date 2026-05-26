package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/client"
)

func newClientForTest(baseURL string) *client.Client {
	return client.NewWithBaseURL(baseURL)
}

func healthyDoctorProbers() (
	*fakeKnowledgeProber, *fakeSchedulerProber, *fakeInboxProber, *fakeTmuxProber,
) {
	return &fakeKnowledgeProber{
			integrity:        "ok",
			lastIndexedAgo:   2 * time.Minute,
			cpuBudgetPct:     20,
			cpuBudgetWarn:    50,
			cpuBudgetFail:    80,
			watcherHeartbeat: time.Now().Add(-5 * time.Second),
			extNullCount:     5,
			extTotalCount:    5,
		}, &fakeSchedulerProber{
			queueDepth:   0,
			missedFires:  0,
			wfqMaxPct:    0,
			dispatcherOK: true,
		}, &fakeInboxProber{
			cacheConsistent: true,
			outboxDepth:     0,
			dedupViolations: 0,
			severityDist:    map[string]int{"info-digest": 1},
		}, &fakeTmuxProber{
			versionInstalled: "tmux 3.4",
			versionMeetsMin:  true,
			serverReachable:  true,
			sessionCount:     0,
			driftCount:       0,
			socketMode:       "0600",
		}
}

func newFakeDeps(t *testing.T, daemonOK bool) DoctorDeps {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !daemonOK {
			w.WriteHeader(503)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/v1/health"):
			_, _ = w.Write([]byte(`{"status":"ok","version":"0.7.0-dev","uptime_seconds":3600}`))
		case strings.HasSuffix(r.URL.Path, "/v1/projects"):
			_, _ = w.Write([]byte(`{"projects":[]}`))
		case strings.HasSuffix(r.URL.Path, "/v1/meta/snapshot"):
			_, _ = w.Write([]byte(`{"panics_last_24h":0,"cost_utilization_pct":42}`))
		case strings.Contains(r.URL.Path, "/v1/bypass/doctor"):

			_, _ = w.Write([]byte(`{"status":"ok","detail":"verified"}`))
		default:
			_, _ = w.Write([]byte(`{"status":"ok","detail":"verified"}`))
		}
	}))
	t.Cleanup(srv.Close)
	c := newClientForTest(srv.URL)
	k, s, i, m := healthyDoctorProbers()
	return DoctorDeps{
		Client:    c,
		Knowledge: k,
		Scheduler: s,
		Inbox:     i,
		Tmux:      m,
	}
}

func TestRunFullProbeAllOKReturnsExpectedNamesInOrder(t *testing.T) {
	deps := newFakeDeps(t, true)
	probes, err := RunFullProbe(context.Background(), deps)
	if err != nil {
		t.Fatalf("RunFullProbe: %v", err)
	}
	if len(probes) == 0 {
		t.Fatalf("expected probes, got 0")
	}
	wantPrefix := "daemon."
	if !strings.HasPrefix(probes[0].Name, wantPrefix) {
		t.Errorf("probes[0].Name=%q, want prefix %q", probes[0].Name, wantPrefix)
	}
	have := map[string]bool{}
	for _, p := range probes {
		head := strings.SplitN(p.Name, ".", 2)[0]
		have[head] = true
	}
	for _, want := range []string{"daemon", "tmux", "knowledge", "scheduler", "inbox"} {
		if !have[want] {
			t.Errorf("RunFullProbe missing %q section; got names: %v", want, namesOf(probes))
		}
	}
}

func TestRunFullProbeDaemonDownStopsEarly(t *testing.T) {
	deps := newFakeDeps(t, false)
	probes, err := RunFullProbe(context.Background(), deps)
	if err != nil {
		t.Fatalf("RunFullProbe: %v", err)
	}
	if len(probes) != 1 {
		t.Fatalf("expected 1 probe (daemon down stops early), got %d: %v", len(probes), namesOf(probes))
	}
	if probes[0].Status != ProbeFail {
		t.Errorf("probe[0].Status = %v, want Fail", probes[0].Status)
	}
	if probes[0].Name != "daemon.reachable" {
		t.Errorf("probe[0].Name = %q, want daemon.reachable", probes[0].Name)
	}
}

func TestRunFullProbeAggregatesAcrossSubsystems(t *testing.T) {
	deps := newFakeDeps(t, true)
	probes, err := RunFullProbe(context.Background(), deps)
	if err != nil {
		t.Fatalf("RunFullProbe: %v", err)
	}
	wantNames := map[string]bool{
		"knowledge.index.integrity":         false,
		"scheduler.queue.depth":             false,
		"inbox.aggregator.cache.consistent": false,
		"tmux.binary.installed":             false,
	}
	for _, p := range probes {
		if _, ok := wantNames[p.Name]; ok {
			wantNames[p.Name] = true
		}
	}
	for n, seen := range wantNames {
		if !seen {
			t.Errorf("RunFullProbe missing aggregated probe name: %q (got: %v)", n, namesOf(probes))
		}
	}
}

// TestRunFullProbeContextCancellationStopsLater asserts the orchestrator
// honors ctx cancellation by either returning context.Canceled or
// emitting partial results — it MUST NOT hang.
func TestRunFullProbeContextCancellationStopsLater(t *testing.T) {
	deps := newFakeDeps(t, true)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	probes, err := RunFullProbe(ctx, deps)
	if err == nil && len(probes) == 0 {
		t.Errorf("expected probes or error, got neither")
	}
}

func TestRunFullProbeNilProberWiresWarn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/v1/health"):
			_, _ = w.Write([]byte(`{"status":"ok","version":"0.7.0-dev","uptime_seconds":1}`))
		case strings.HasSuffix(r.URL.Path, "/v1/projects"):
			_, _ = w.Write([]byte(`{"projects":[]}`))
		case strings.HasSuffix(r.URL.Path, "/v1/meta/snapshot"):
			_, _ = w.Write([]byte(`{"panics_last_24h":0,"cost_utilization_pct":0}`))
		default:
			_, _ = w.Write([]byte(`{"status":"ok","detail":"verified"}`))
		}
	}))
	t.Cleanup(srv.Close)
	deps := DoctorDeps{
		Client:    newClientForTest(srv.URL),
		Knowledge: nil,
		Scheduler: nil,
		Inbox:     nil,
		Tmux:      nil,
	}
	probes, err := RunFullProbe(context.Background(), deps)
	if err != nil {
		t.Fatalf("RunFullProbe: %v", err)
	}
	want := map[string]bool{
		"tmux.prober":      false,
		"inbox.prober":     false,
		"knowledge.prober": false,
		"scheduler.prober": false,
	}
	for _, p := range probes {
		if _, ok := want[p.Name]; ok {
			if p.Status != ProbeWarn {
				t.Errorf("nil prober probe %q status = %v, want Warn", p.Name, p.Status)
			}
			want[p.Name] = true
		}
	}
	for n, seen := range want {
		if !seen {
			t.Errorf("expected nil-prober Warn probe for %q, missing", n)
		}
	}
}

func TestRunFullProbeProberErrorEmitsFail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/v1/health"):
			_, _ = w.Write([]byte(`{"status":"ok","version":"x","uptime_seconds":1}`))
		case strings.HasSuffix(r.URL.Path, "/v1/projects"):
			_, _ = w.Write([]byte(`{"projects":[]}`))
		case strings.HasSuffix(r.URL.Path, "/v1/meta/snapshot"):
			_, _ = w.Write([]byte(`{"panics_last_24h":0,"cost_utilization_pct":0}`))
		default:
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		}
	}))
	t.Cleanup(srv.Close)
	failingKnowledge := &fakeKnowledgeProber{
		integrityErr:     context.DeadlineExceeded,
		watcherHeartbeat: time.Now().Add(-1 * time.Second),
	}
	_, s, i, m := healthyDoctorProbers()
	deps := DoctorDeps{
		Client:    newClientForTest(srv.URL),
		Knowledge: failingKnowledge,
		Scheduler: s,
		Inbox:     i,
		Tmux:      m,
	}
	probes, err := RunFullProbe(context.Background(), deps)
	if err != nil {
		t.Fatalf("RunFullProbe: %v", err)
	}
	var foundFail bool
	for _, p := range probes {
		if strings.HasPrefix(p.Name, "knowledge.") && p.Status == ProbeFail {
			foundFail = true
			if !strings.Contains(p.Detail, context.DeadlineExceeded.Error()) {
				t.Errorf("knowledge probe %q Detail = %q, want to contain %q",
					p.Name, p.Detail, context.DeadlineExceeded.Error())
			}
			break
		}
	}
	if !foundFail {
		t.Errorf("expected at least one knowledge.* Fail probe from injected error; got: %v", namesOf(probes))
	}
}

func TestRunFullProbeNilClientFailsDaemonProbe(t *testing.T) {
	deps := DoctorDeps{}
	probes, err := RunFullProbe(context.Background(), deps)
	if err != nil {
		t.Fatalf("RunFullProbe: %v", err)
	}
	if len(probes) != 1 {
		t.Fatalf("expected 1 probe (nil client short-circuit), got %d", len(probes))
	}
	if probes[0].Status != ProbeFail {
		t.Errorf("probes[0].Status = %v, want Fail", probes[0].Status)
	}
}

func TestRunFullProbeProjectsListErrorEmitsWarn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/v1/health"):
			_, _ = w.Write([]byte(`{"status":"ok","version":"x","uptime_seconds":1}`))
		case strings.HasSuffix(r.URL.Path, "/v1/projects"):
			w.WriteHeader(501)
			_, _ = w.Write([]byte(`{"error":"not implemented"}`))
		case strings.HasSuffix(r.URL.Path, "/v1/meta/snapshot"):
			_, _ = w.Write([]byte(`{"panics_last_24h":0,"cost_utilization_pct":0}`))
		default:
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		}
	}))
	t.Cleanup(srv.Close)
	k, s, i, m := healthyDoctorProbers()
	deps := DoctorDeps{
		Client:    newClientForTest(srv.URL),
		Knowledge: k,
		Scheduler: s,
		Inbox:     i,
		Tmux:      m,
	}
	probes, err := RunFullProbe(context.Background(), deps)
	if err != nil {
		t.Fatalf("RunFullProbe: %v", err)
	}
	var found bool
	for _, p := range probes {
		if p.Name == "projects.list" {
			found = true
			if p.Status != ProbeWarn {
				t.Errorf("projects.list status = %v, want Warn", p.Status)
			}
		}
	}
	if !found {
		t.Errorf("expected projects.list probe, got: %v", namesOf(probes))
	}
}

func TestRunFullProbeMetaPanicsRecentEmitsFail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/v1/health"):
			_, _ = w.Write([]byte(`{"status":"ok","version":"x","uptime_seconds":1}`))
		case strings.HasSuffix(r.URL.Path, "/v1/projects"):
			_, _ = w.Write([]byte(`{"projects":[]}`))
		case strings.HasSuffix(r.URL.Path, "/v1/meta/snapshot"):
			_, _ = w.Write([]byte(`{"panics_last_24h":3,"cost_utilization_pct":85}`))
		default:
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		}
	}))
	t.Cleanup(srv.Close)
	k, s, i, m := healthyDoctorProbers()
	deps := DoctorDeps{
		Client:    newClientForTest(srv.URL),
		Knowledge: k,
		Scheduler: s,
		Inbox:     i,
		Tmux:      m,
	}
	probes, err := RunFullProbe(context.Background(), deps)
	if err != nil {
		t.Fatalf("RunFullProbe: %v", err)
	}
	var foundPanic, foundCost bool
	for _, p := range probes {
		switch p.Name {
		case "meta.panics.24h":
			foundPanic = true
			if p.Status != ProbeFail {
				t.Errorf("meta.panics.24h status = %v, want Fail (3 panics)", p.Status)
			}
		case "meta.cost.utilization":
			foundCost = true
			if p.Status != ProbeWarn {
				t.Errorf("meta.cost.utilization status = %v, want Warn (85%%)", p.Status)
			}
		}
	}
	if !foundPanic {
		t.Errorf("expected meta.panics.24h probe, got: %v", namesOf(probes))
	}
	if !foundCost {
		t.Errorf("expected meta.cost.utilization probe, got: %v", namesOf(probes))
	}
}

func TestRunFullProbeMetaCostOverEmitsFail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/v1/health"):
			_, _ = w.Write([]byte(`{"status":"ok","version":"x","uptime_seconds":1}`))
		case strings.HasSuffix(r.URL.Path, "/v1/projects"):
			_, _ = w.Write([]byte(`{"projects":[]}`))
		case strings.HasSuffix(r.URL.Path, "/v1/meta/snapshot"):
			_, _ = w.Write([]byte(`{"panics_last_24h":0,"cost_utilization_pct":120}`))
		default:
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		}
	}))
	t.Cleanup(srv.Close)
	k, s, i, m := healthyDoctorProbers()
	deps := DoctorDeps{
		Client:    newClientForTest(srv.URL),
		Knowledge: k,
		Scheduler: s,
		Inbox:     i,
		Tmux:      m,
	}
	probes, _ := RunFullProbe(context.Background(), deps)
	var found bool
	for _, p := range probes {
		if p.Name == "meta.cost.utilization" {
			found = true
			if p.Status != ProbeFail {
				t.Errorf("meta.cost.utilization status = %v, want Fail (120%%)", p.Status)
			}
		}
	}
	if !found {
		t.Errorf("expected meta.cost.utilization probe, got: %v", namesOf(probes))
	}
}

func TestRunFullProbeProjectsWithActiveItem(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/v1/health"):
			_, _ = w.Write([]byte(`{"status":"ok","version":"x","uptime_seconds":1}`))
		case strings.HasSuffix(r.URL.Path, "/v1/projects"):
			_, _ = w.Write([]byte(`{"projects":[{"id":"abc","alias":"internal-platform-x","path":"/p","autonomous_state":"active"}]}`))
		case strings.HasSuffix(r.URL.Path, "/v1/projects/internal-platform-x/doctor"):
			_, _ = w.Write([]byte(`{"items":[{"aspect":"path.exists","status":"ok","message":"verified","detail":"","hint":""},{"aspect":"sessions.alive","status":"warn","message":"1 stale","detail":"","hint":"zen sessions prune"}]}`))
		case strings.HasSuffix(r.URL.Path, "/v1/meta/snapshot"):
			_, _ = w.Write([]byte(`{"panics_last_24h":0,"cost_utilization_pct":0}`))
		default:
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		}
	}))
	t.Cleanup(srv.Close)
	k, s, i, m := healthyDoctorProbers()
	deps := DoctorDeps{
		Client:    newClientForTest(srv.URL),
		Knowledge: k,
		Scheduler: s,
		Inbox:     i,
		Tmux:      m,
	}
	probes, err := RunFullProbe(context.Background(), deps)
	if err != nil {
		t.Fatalf("RunFullProbe: %v", err)
	}
	want := map[string]ProbeStatus{
		"projects.count": ProbeOK,
		"projects.internal-platform-x.path.exists":    ProbeOK,
		"projects.internal-platform-x.sessions.alive": ProbeWarn,
	}
	got := map[string]ProbeStatus{}
	for _, p := range probes {
		got[p.Name] = p.Status
	}
	for n, st := range want {
		if got[n] != st {
			t.Errorf("probe %q status = %v, want %v (all probes: %v)", n, got[n], st, namesOf(probes))
		}
	}
}

func TestRunFullProbeProjectsArchivedSkipped(t *testing.T) {
	var perProjectDoctorCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/v1/health"):
			_, _ = w.Write([]byte(`{"status":"ok","version":"x","uptime_seconds":1}`))
		case strings.HasSuffix(r.URL.Path, "/v1/projects"):
			_, _ = w.Write([]byte(`{"projects":[{"id":"abc","alias":"old","path":"/x","autonomous_state":"complete"}]}`))
		case strings.HasPrefix(r.URL.Path, "/v1/projects/") && strings.HasSuffix(r.URL.Path, "/doctor"):
			perProjectDoctorCalls++
			_, _ = w.Write([]byte(`{"items":[]}`))
		case strings.HasSuffix(r.URL.Path, "/v1/meta/snapshot"):
			_, _ = w.Write([]byte(`{"panics_last_24h":0,"cost_utilization_pct":0}`))
		default:
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		}
	}))
	t.Cleanup(srv.Close)
	k, s, i, m := healthyDoctorProbers()
	deps := DoctorDeps{
		Client:    newClientForTest(srv.URL),
		Knowledge: k,
		Scheduler: s,
		Inbox:     i,
		Tmux:      m,
	}
	if _, err := RunFullProbe(context.Background(), deps); err != nil {
		t.Fatalf("RunFullProbe: %v", err)
	}
	if perProjectDoctorCalls != 0 {
		t.Errorf("archived project should not trigger per-project doctor; got %d calls", perProjectDoctorCalls)
	}
}

func namesOf(p []ProbeResult) []string {
	out := make([]string, 0, len(p))
	for _, r := range p {
		out = append(out, r.Name)
	}
	return out
}

func TestRunProjectsProbeNilClient(t *testing.T) {
	got := runProjectsProbe(context.Background(), DoctorDeps{Client: nil})
	if got != nil {
		t.Errorf("runProjectsProbe(nil client) = %v, want nil", got)
	}
}

func TestRunBypassAdaptedNilClient(t *testing.T) {
	got := runBypassAdapted(context.Background(), DoctorDeps{Client: nil})
	if got != nil {
		t.Errorf("runBypassAdapted(nil client) = %v, want nil", got)
	}
}

func TestRunMetaProbeNilClient(t *testing.T) {
	got := runMetaProbe(context.Background(), DoctorDeps{Client: nil})
	if got != nil {
		t.Errorf("runMetaProbe(nil client) = %v, want nil", got)
	}
}

func TestRunMetaProbeSnapshotError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
	}))
	t.Cleanup(srv.Close)
	deps := DoctorDeps{Client: newClientForTest(srv.URL)}
	got := runMetaProbe(context.Background(), deps)
	if len(got) != 1 {
		t.Fatalf("expected 1 probe, got %d", len(got))
	}
	if got[0].Name != "meta.snapshot" || got[0].Status != ProbeWarn {
		t.Errorf("got %+v, want meta.snapshot Warn", got[0])
	}
}

func TestRunOneProjectDoctorErrorEmitsWarn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	t.Cleanup(srv.Close)
	deps := DoctorDeps{Client: newClientForTest(srv.URL)}
	got := runOneProjectDoctor(context.Background(), deps, "ghost")
	if len(got) != 1 {
		t.Fatalf("expected 1 probe, got %d", len(got))
	}
	if got[0].Status != ProbeWarn {
		t.Errorf("got status %v, want Warn", got[0].Status)
	}
	if got[0].Name != "projects.ghost.doctor" {
		t.Errorf("got name %q, want projects.ghost.doctor", got[0].Name)
	}
}

func TestRunOneProjectDoctorFailItem(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[{"aspect":"sessions.dead","status":"fail","message":"all stale","detail":"3 dead","hint":"reset"}]}`))
	}))
	t.Cleanup(srv.Close)
	deps := DoctorDeps{Client: newClientForTest(srv.URL)}
	got := runOneProjectDoctor(context.Background(), deps, "x")
	if len(got) != 1 {
		t.Fatalf("expected 1 probe, got %d", len(got))
	}
	if got[0].Status != ProbeFail {
		t.Errorf("got status %v, want Fail", got[0].Status)
	}
	if got[0].Name != "projects.x.sessions.dead" {
		t.Errorf("got name %q, want projects.x.sessions.dead", got[0].Name)
	}
}

func TestRunBypassAdaptedTranslatesAllStatuses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		check := r.URL.Query().Get("check")
		switch check {
		case "credentials.readable":
			_, _ = w.Write([]byte(`{"status":"ok","detail":"verified"}`))
		case "credentials.fresh":
			_, _ = w.Write([]byte(`{"status":"warn","detail":"about to expire"}`))
		default:
			_, _ = w.Write([]byte(`{"status":"fail","detail":"oops"}`))
		}
	}))
	t.Cleanup(srv.Close)
	deps := DoctorDeps{Client: newClientForTest(srv.URL)}
	got := runBypassAdapted(context.Background(), deps)
	if len(got) == 0 {
		t.Fatal("expected probes from runBypassAdapted, got 0")
	}

	statuses := map[ProbeStatus]bool{}
	for _, p := range got {
		statuses[p.Status] = true
	}
	for _, want := range []ProbeStatus{ProbeOK, ProbeWarn, ProbeFail} {
		if !statuses[want] {
			t.Errorf("runBypassAdapted didn't emit %v probe; got statuses: %v", want, statuses)
		}
	}
}

// TestRunFullProbeOrderConstantHasAllSections is a guard against silent
// drift in the canonical section order — every section invoked by
// RunFullProbe MUST appear in runFullProbeOrder so future maintainers
// don't add a section without updating the documented order.
func TestRunFullProbeOrderConstantHasAllSections(t *testing.T) {
	want := map[string]bool{
		"daemon":    false,
		"tmux":      false,
		"projects":  false,
		"inbox":     false,
		"knowledge": false,
		"scheduler": false,
		"bypass":    false,
		"meta":      false,
	}
	for _, s := range runFullProbeOrder {
		if _, ok := want[s]; !ok {
			t.Errorf("runFullProbeOrder has unexpected section %q", s)
		}
		want[s] = true
	}
	for s, seen := range want {
		if !seen {
			t.Errorf("runFullProbeOrder missing section %q", s)
		}
	}
}

func TestRunFullProbeCtxCancelledBeforeStart(t *testing.T) {
	deps := newFakeDeps(t, true)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	probes, err := RunFullProbe(ctx, deps)
	if err == nil {
		t.Errorf("expected error on pre-cancelled ctx, got probes=%v", namesOf(probes))
	}
}

func TestRunFullProbeCtxCancelledMidPhase(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/v1/health") {
			_, _ = w.Write([]byte(`{"status":"ok","version":"x","uptime_seconds":1}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(srv.Close)
	k, s, i, m := healthyDoctorProbers()
	deps := DoctorDeps{
		Client:    newClientForTest(srv.URL),
		Knowledge: k,
		Scheduler: s,
		Inbox:     i,
		Tmux:      m,
	}
	ctx := &cancelOnSecondErr{Context: parent, cancel: cancel}
	probes, err := RunFullProbe(ctx, deps)
	if err != nil {
		t.Fatalf("RunFullProbe: %v", err)
	}

	if len(probes) != 1 {
		t.Errorf("expected 1 probe (mid-phase ctx-cancel short-circuit), got %d: %v", len(probes), namesOf(probes))
	}
	if len(probes) >= 1 && probes[0].Name != "daemon.reachable" {
		t.Errorf("probes[0].Name = %q, want daemon.reachable", probes[0].Name)
	}
	if ctx.errCalls < 2 {
		t.Errorf("expected at least 2 Err calls, got %d", ctx.errCalls)
	}
}

type cancelOnSecondErr struct {
	context.Context
	cancel   context.CancelFunc
	errCalls int
}

func (c *cancelOnSecondErr) Err() error {
	c.errCalls++
	if c.errCalls >= 2 {
		c.cancel()
	}
	return c.Context.Err()
}
