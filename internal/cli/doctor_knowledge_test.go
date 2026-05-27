package cli

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeKnowledgeProber struct {
	integrity        string
	integrityErr     error
	lastIndexedAgo   time.Duration
	lastIndexedZero  bool
	cpuBudgetPct     int
	cpuBudgetWarn    int
	cpuBudgetFail    int
	watcherHeartbeat time.Time
	extNullCount     int
	extTotalCount    int
	statsErr         error
}

func (f *fakeKnowledgeProber) IntegrityCheck(ctx context.Context) (string, error) {
	return f.integrity, f.integrityErr
}

func (f *fakeKnowledgeProber) LastIndexedAt(ctx context.Context) (time.Time, error) {
	if f.statsErr != nil {
		return time.Time{}, f.statsErr
	}
	if f.lastIndexedZero {
		return time.Time{}, nil
	}
	return time.Now().Add(-f.lastIndexedAgo), nil
}

func (f *fakeKnowledgeProber) IndexerCPUBudget(ctx context.Context) (used, warn, fail int, err error) {
	return f.cpuBudgetPct, f.cpuBudgetWarn, f.cpuBudgetFail, f.statsErr
}

func (f *fakeKnowledgeProber) WatcherHeartbeat(ctx context.Context) (time.Time, error) {
	return f.watcherHeartbeat, f.statsErr
}

func (f *fakeKnowledgeProber) ExtensionHookNullCount(ctx context.Context) (nullCount, totalCount int, err error) {
	return f.extNullCount, f.extTotalCount, f.statsErr
}

func TestRunKnowledgeProbeAllOK(t *testing.T) {
	p := &fakeKnowledgeProber{
		integrity:        "ok",
		lastIndexedAgo:   2 * time.Minute,
		cpuBudgetPct:     20,
		cpuBudgetWarn:    50,
		cpuBudgetFail:    80,
		watcherHeartbeat: time.Now().Add(-5 * time.Second),
		extNullCount:     12,
		extTotalCount:    12,
	}
	probes, err := RunKnowledgeProbe(context.Background(), p)
	if err != nil {
		t.Fatalf("RunKnowledgeProbe: %v", err)
	}
	if len(probes) != 5 {
		t.Fatalf("want 5 probes, got %d", len(probes))
	}
	for _, r := range probes {
		if r.Status != ProbeOK {
			t.Errorf("probe %s: status=%v, message=%q", r.Name, r.Status, r.Message)
		}
	}
}

func TestRunKnowledgeProbeIntegrityCorrupt(t *testing.T) {
	p := &fakeKnowledgeProber{
		integrity:        "*** in database main ***\nrow 1 missing from index knowledge_fts",
		lastIndexedAgo:   2 * time.Minute,
		watcherHeartbeat: time.Now().Add(-5 * time.Second),
		extNullCount:     12,
		extTotalCount:    12,
	}
	probes, _ := RunKnowledgeProbe(context.Background(), p)
	var integrityProbe *ProbeResult
	for i := range probes {
		if probes[i].Name == "knowledge.index.integrity" {
			integrityProbe = &probes[i]
		}
	}
	if integrityProbe == nil {
		t.Fatal("integrity probe missing")
	}
	if integrityProbe.Status != ProbeFail {
		t.Errorf("integrity probe status=%v, want Fail", integrityProbe.Status)
	}
	if !strings.Contains(integrityProbe.Hint, "knowledge reindex") {
		t.Errorf("hint should suggest reindex: %q", integrityProbe.Hint)
	}
}

func TestRunKnowledgeProbeIntegrityErrorFails(t *testing.T) {
	p := &fakeKnowledgeProber{
		integrityErr:     errors.New("daemon.db locked"),
		lastIndexedAgo:   2 * time.Minute,
		watcherHeartbeat: time.Now().Add(-5 * time.Second),
		extNullCount:     1,
		extTotalCount:    1,
	}
	probes, _ := RunKnowledgeProbe(context.Background(), p)
	for _, r := range probes {
		if r.Name == "knowledge.index.integrity" {
			if r.Status != ProbeFail {
				t.Errorf("status=%v, want Fail", r.Status)
			}
			if !strings.Contains(r.Detail, "locked") {
				t.Errorf("detail should mention error: %q", r.Detail)
			}
			return
		}
	}
	t.Fatal("integrity probe missing")
}

func TestRunKnowledgeProbeLastIndexedFreshOK(t *testing.T) {
	p := &fakeKnowledgeProber{
		integrity:        "ok",
		lastIndexedAgo:   2 * time.Minute,
		watcherHeartbeat: time.Now().Add(-5 * time.Second),
		extNullCount:     1,
		extTotalCount:    1,
	}
	probes, _ := RunKnowledgeProbe(context.Background(), p)
	for _, r := range probes {
		if r.Name == "knowledge.index.last_indexed" {
			if r.Status != ProbeOK {
				t.Errorf("last_indexed status=%v, want OK (2min ago)", r.Status)
			}
			return
		}
	}
	t.Fatal("last_indexed probe missing")
}

func TestRunKnowledgeProbeLastIndexedZeroWarn(t *testing.T) {

	p := &fakeKnowledgeProber{
		integrity:        "ok",
		lastIndexedZero:  true,
		watcherHeartbeat: time.Now().Add(-5 * time.Second),
		extNullCount:     0,
		extTotalCount:    0,
	}
	probes, _ := RunKnowledgeProbe(context.Background(), p)
	for _, r := range probes {
		if r.Name == "knowledge.index.last_indexed" {
			if r.Status != ProbeWarn {
				t.Errorf("zero time status=%v, want Warn", r.Status)
			}
			if !strings.Contains(r.Hint, "reindex") {
				t.Errorf("hint should suggest reindex: %q", r.Hint)
			}
			return
		}
	}
	t.Fatal("last_indexed probe missing")
}

func TestRunKnowledgeProbeLastIndexedStaleWarn(t *testing.T) {
	p := &fakeKnowledgeProber{
		integrity:        "ok",
		lastIndexedAgo:   45 * time.Minute,
		watcherHeartbeat: time.Now().Add(-5 * time.Second),
		extNullCount:     1,
		extTotalCount:    1,
	}
	probes, _ := RunKnowledgeProbe(context.Background(), p)
	for _, r := range probes {
		if r.Name == "knowledge.index.last_indexed" {
			if r.Status != ProbeWarn {
				t.Errorf("last_indexed status=%v, want Warn (45min ago)", r.Status)
			}
			return
		}
	}
	t.Fatal("last_indexed probe missing")
}

func TestRunKnowledgeProbeLastIndexedDeadFail(t *testing.T) {
	p := &fakeKnowledgeProber{
		integrity:        "ok",
		lastIndexedAgo:   3 * time.Hour,
		watcherHeartbeat: time.Now().Add(-5 * time.Second),
		extNullCount:     1,
		extTotalCount:    1,
	}
	probes, _ := RunKnowledgeProbe(context.Background(), p)
	for _, r := range probes {
		if r.Name == "knowledge.index.last_indexed" {
			if r.Status != ProbeFail {
				t.Errorf("last_indexed status=%v, want Fail (>2h)", r.Status)
			}
			return
		}
	}
	t.Fatal("last_indexed probe missing")
}

func TestRunKnowledgeProbeCPUBudgetWarn(t *testing.T) {
	p := &fakeKnowledgeProber{
		integrity:        "ok",
		lastIndexedAgo:   2 * time.Minute,
		cpuBudgetPct:     55,
		cpuBudgetWarn:    50,
		cpuBudgetFail:    80,
		watcherHeartbeat: time.Now().Add(-5 * time.Second),
		extNullCount:     1,
		extTotalCount:    1,
	}
	probes, _ := RunKnowledgeProbe(context.Background(), p)
	for _, r := range probes {
		if r.Name == "knowledge.indexer.cpu_budget" {
			if r.Status != ProbeWarn {
				t.Errorf("cpu_budget status=%v, want Warn", r.Status)
			}
			return
		}
	}
	t.Fatal("cpu_budget probe missing")
}

func TestRunKnowledgeProbeCPUBudgetOverFail(t *testing.T) {
	p := &fakeKnowledgeProber{
		integrity:        "ok",
		lastIndexedAgo:   2 * time.Minute,
		cpuBudgetPct:     85,
		cpuBudgetWarn:    50,
		cpuBudgetFail:    80,
		watcherHeartbeat: time.Now().Add(-5 * time.Second),
		extNullCount:     1,
		extTotalCount:    1,
	}
	probes, _ := RunKnowledgeProbe(context.Background(), p)
	for _, r := range probes {
		if r.Name == "knowledge.indexer.cpu_budget" {
			if r.Status != ProbeFail {
				t.Errorf("cpu_budget status=%v, want Fail", r.Status)
			}
			return
		}
	}
	t.Fatal("cpu_budget probe missing")
}

func TestRunKnowledgeProbeWatcherZeroFail(t *testing.T) {

	p := &fakeKnowledgeProber{
		integrity:        "ok",
		lastIndexedAgo:   2 * time.Minute,
		watcherHeartbeat: time.Time{},
		extNullCount:     1,
		extTotalCount:    1,
	}
	probes, _ := RunKnowledgeProbe(context.Background(), p)
	for _, r := range probes {
		if r.Name == "knowledge.watcher.status" {
			if r.Status != ProbeFail {
				t.Errorf("watcher status=%v, want Fail (never started)", r.Status)
			}
			if !strings.Contains(r.Hint, "restart") {
				t.Errorf("hint should suggest restart: %q", r.Hint)
			}
			return
		}
	}
	t.Fatal("watcher probe missing")
}

func TestRunKnowledgeProbeWatcherDeadFail(t *testing.T) {
	p := &fakeKnowledgeProber{
		integrity:        "ok",
		lastIndexedAgo:   2 * time.Minute,
		watcherHeartbeat: time.Now().Add(-2 * time.Minute),
		extNullCount:     1,
		extTotalCount:    1,
	}
	probes, _ := RunKnowledgeProbe(context.Background(), p)
	for _, r := range probes {
		if r.Name == "knowledge.watcher.status" {
			if r.Status != ProbeFail {
				t.Errorf("watcher status=%v, want Fail (heartbeat 2min stale)", r.Status)
			}
			if !strings.Contains(r.Hint, "restart") {
				t.Errorf("hint should suggest restart: %q", r.Hint)
			}
			return
		}
	}
	t.Fatal("watcher probe missing")
}

func TestRunKnowledgeProbeExtensionHooksAllSetWarn(t *testing.T) {
	// Spec §7.2 invariant: extension columns NULL by default. If 100%
	// of rows have non-NULL extensions, that means either wired
	// prematurely or test fixture leaked. Probe MUST surface as Warn.
	p := &fakeKnowledgeProber{
		integrity:        "ok",
		lastIndexedAgo:   2 * time.Minute,
		watcherHeartbeat: time.Now().Add(-5 * time.Second),
		extNullCount:     0,
		extTotalCount:    100,
	}
	probes, _ := RunKnowledgeProbe(context.Background(), p)
	for _, r := range probes {
		if r.Name == "knowledge.extension_hooks.null_default" {
			if r.Status != ProbeWarn {
				t.Errorf("extension_hooks status=%v, want Warn (Plan 7 expects NULL default)", r.Status)
			}
			if !strings.Contains(r.Hint, "Plan 9") && !strings.Contains(r.Hint, "Plan 14") {
				t.Errorf("hint should reference Plan 9/14: %q", r.Hint)
			}
			return
		}
	}
	t.Fatal("extension_hooks probe missing")
}

func TestRunKnowledgeProbeExtensionHooksEmptyOK(t *testing.T) {

	p := &fakeKnowledgeProber{
		integrity:        "ok",
		lastIndexedAgo:   2 * time.Minute,
		watcherHeartbeat: time.Now().Add(-5 * time.Second),
		extNullCount:     0,
		extTotalCount:    0,
	}
	probes, _ := RunKnowledgeProbe(context.Background(), p)
	for _, r := range probes {
		if r.Name == "knowledge.extension_hooks.null_default" {
			if r.Status != ProbeOK {
				t.Errorf("extension_hooks status=%v, want OK (no rows)", r.Status)
			}
			return
		}
	}
	t.Fatal("extension_hooks probe missing")
}

func TestRunKnowledgeProbeProberError(t *testing.T) {

	p := &fakeKnowledgeProber{
		statsErr: errors.New("daemon.db locked"),
	}
	probes, _ := RunKnowledgeProbe(context.Background(), p)
	if len(probes) == 0 {
		t.Fatal("expected at least one probe")
	}

	if len(probes) != 5 {
		t.Errorf("expected 5 probes even on error, got %d", len(probes))
	}
	hasFail := false
	for _, r := range probes {
		if r.Status == ProbeFail {
			hasFail = true
		}
	}
	if !hasFail {
		t.Errorf("expected at least one Fail probe when prober errors")
	}
}
