package cli

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeInboxProber struct {
	cacheConsistent  bool
	cacheDriftRows   int
	cacheDriftDetail string
	outboxDepth      int
	dedupViolations  int
	severityDist     map[string]int
	urgentCount24h   int
	statsErr         error
}

func (f *fakeInboxProber) AggregatorCacheConsistent(ctx context.Context) (consistent bool, driftRows int, detail string, err error) {
	return f.cacheConsistent, f.cacheDriftRows, f.cacheDriftDetail, f.statsErr
}

func (f *fakeInboxProber) OutboxQueueDepth(ctx context.Context) (int, error) {
	return f.outboxDepth, f.statsErr
}

func (f *fakeInboxProber) DedupConstraintViolations(ctx context.Context) (int, error) {
	return f.dedupViolations, f.statsErr
}

func (f *fakeInboxProber) SeverityDistribution24h(ctx context.Context) (dist map[string]int, urgentCount int, err error) {
	return f.severityDist, f.urgentCount24h, f.statsErr
}

func TestRunInboxProbeAllOK(t *testing.T) {
	p := &fakeInboxProber{
		cacheConsistent: true,
		outboxDepth:     5,
		dedupViolations: 0,
		severityDist:    map[string]int{"urgent": 0, "action-needed": 1, "info-immediate": 8, "info-digest": 3},
		urgentCount24h:  0,
	}
	probes, err := RunInboxProbe(context.Background(), p)
	if err != nil {
		t.Fatalf("RunInboxProbe: %v", err)
	}
	if len(probes) != 4 {
		t.Fatalf("want 4 probes, got %d", len(probes))
	}
	for _, r := range probes {
		if r.Status != ProbeOK {
			t.Errorf("probe %s: status=%v message=%q", r.Name, r.Status, r.Message)
		}
	}
}

func TestRunInboxProbeAggregatorDriftWarn(t *testing.T) {
	p := &fakeInboxProber{
		cacheConsistent:  false,
		cacheDriftRows:   1,
		cacheDriftDetail: "internal-platform-x: per-project=42 cache=43",
	}
	probes, _ := RunInboxProbe(context.Background(), p)
	for _, r := range probes {
		if r.Name == "inbox.aggregator.cache.consistent" {
			if r.Status != ProbeWarn {
				t.Errorf("status=%v, want Warn (1 drift row in tolerance)", r.Status)
			}
			if !strings.Contains(r.Detail, "internal-platform-x") {
				t.Errorf("detail should include drift project: %q", r.Detail)
			}
			return
		}
	}
	t.Fatal("aggregator probe missing")
}

func TestRunInboxProbeAggregatorDriftFail(t *testing.T) {
	p := &fakeInboxProber{
		cacheConsistent: false,
		cacheDriftRows:  5,
	}
	probes, _ := RunInboxProbe(context.Background(), p)
	for _, r := range probes {
		if r.Name == "inbox.aggregator.cache.consistent" {
			if r.Status != ProbeFail {
				t.Errorf("status=%v, want Fail (>2 drift rows)", r.Status)
			}
			if !strings.Contains(r.Hint, "rebuild") {
				t.Errorf("hint should suggest rebuild: %q", r.Hint)
			}
			return
		}
	}
	t.Fatal("aggregator probe missing")
}

func TestRunInboxProbeAggregatorErrorFails(t *testing.T) {
	p := &fakeInboxProber{
		statsErr: errors.New("daemon.db locked"),
	}
	probes, _ := RunInboxProbe(context.Background(), p)
	for _, r := range probes {
		if r.Name == "inbox.aggregator.cache.consistent" {
			if r.Status != ProbeFail {
				t.Errorf("status=%v, want Fail", r.Status)
			}
			if !strings.Contains(r.Detail, "locked") {
				t.Errorf("detail should mention err: %q", r.Detail)
			}
			return
		}
	}
	t.Fatal("aggregator probe missing")
}

func TestRunInboxProbeOutboxBackloggedWarn(t *testing.T) {
	p := &fakeInboxProber{
		cacheConsistent: true,
		outboxDepth:     100,
	}
	probes, _ := RunInboxProbe(context.Background(), p)
	for _, r := range probes {
		if r.Name == "inbox.outbox.queue.depth" {
			if r.Status != ProbeWarn {
				t.Errorf("status=%v, want Warn (>50)", r.Status)
			}
			return
		}
	}
	t.Fatal("outbox probe missing")
}

func TestRunInboxProbeOutboxSaturatedFail(t *testing.T) {
	p := &fakeInboxProber{
		cacheConsistent: true,
		outboxDepth:     250,
	}
	probes, _ := RunInboxProbe(context.Background(), p)
	for _, r := range probes {
		if r.Name == "inbox.outbox.queue.depth" {
			if r.Status != ProbeFail {
				t.Errorf("status=%v, want Fail (>200)", r.Status)
			}
			if !strings.Contains(r.Hint, "Plan 11") && !strings.Contains(r.Hint, "osascript") {
				t.Errorf("hint should reference Plan 11 / osascript: %q", r.Hint)
			}
			return
		}
	}
	t.Fatal("outbox probe missing")
}

func TestRunInboxProbeDedupViolationFail(t *testing.T) {
	p := &fakeInboxProber{
		cacheConsistent: true,
		dedupViolations: 1,
	}
	probes, _ := RunInboxProbe(context.Background(), p)
	for _, r := range probes {
		if r.Name == "inbox.dedup.window.health" {
			if r.Status != ProbeFail {
				t.Errorf("status=%v, want Fail", r.Status)
			}
			if !strings.Contains(r.Hint, "constraint") {
				t.Errorf("hint should mention constraint: %q", r.Hint)
			}
			return
		}
	}
	t.Fatal("dedup probe missing")
}

func TestRunInboxProbeSeverityHighUrgentWarn(t *testing.T) {
	p := &fakeInboxProber{
		cacheConsistent: true,
		severityDist:    map[string]int{"urgent": 8, "action-needed": 2, "info-immediate": 1, "info-digest": 0},
		urgentCount24h:  8,
	}
	probes, _ := RunInboxProbe(context.Background(), p)
	for _, r := range probes {
		if r.Name == "inbox.severity.distribution" {
			if r.Status != ProbeWarn {
				t.Errorf("status=%v, want Warn (8 urgent in 24h > threshold)", r.Status)
			}
			return
		}
	}
	t.Fatal("severity probe missing")
}

func TestRunInboxProbeSeverityRendersDetail(t *testing.T) {

	p := &fakeInboxProber{
		cacheConsistent: true,
		severityDist:    map[string]int{"urgent": 1, "action-needed": 2, "info-immediate": 3, "info-digest": 4},
		urgentCount24h:  1,
	}
	probes, _ := RunInboxProbe(context.Background(), p)
	for _, r := range probes {
		if r.Name == "inbox.severity.distribution" {
			want := "urgent=1, action-needed=2, info-immediate=3, info-digest=4"
			if r.Detail != want {
				t.Errorf("detail = %q, want %q", r.Detail, want)
			}
			return
		}
	}
	t.Fatal("severity probe missing")
}

func TestRunInboxProbeProberError(t *testing.T) {
	p := &fakeInboxProber{
		statsErr: errors.New("daemon.db locked"),
	}
	probes, _ := RunInboxProbe(context.Background(), p)
	if len(probes) != 4 {
		t.Errorf("expected 4 probes, got %d", len(probes))
	}
	hasFail := false
	for _, r := range probes {
		if r.Status == ProbeFail {
			hasFail = true
		}
	}
	if !hasFail {
		t.Error("expected at least one Fail probe")
	}
}

func TestRunInboxProbeAggregatorDefensiveDefault(t *testing.T) {
	p := &fakeInboxProber{
		cacheConsistent: false,
		cacheDriftRows:  0,
	}
	probes, _ := RunInboxProbe(context.Background(), p)
	for _, r := range probes {
		if r.Name == "inbox.aggregator.cache.consistent" {
			if r.Status != ProbeOK {
				t.Errorf("status=%v, want OK (defensive default)", r.Status)
			}
			return
		}
	}
	t.Fatal("aggregator probe missing")
}

func TestRunInboxProbeSeverityUnknownTier(t *testing.T) {
	p := &fakeInboxProber{
		cacheConsistent: true,
		severityDist: map[string]int{
			"urgent":      1,
			"future-tier": 2,
			"alpha-tier":  3,
		},
		urgentCount24h: 1,
	}
	probes, _ := RunInboxProbe(context.Background(), p)
	for _, r := range probes {
		if r.Name == "inbox.severity.distribution" {

			alphaIdx := strings.Index(r.Detail, "alpha-tier")
			futureIdx := strings.Index(r.Detail, "future-tier")
			if alphaIdx == -1 || futureIdx == -1 {
				t.Errorf("missing extras: %q", r.Detail)
				return
			}
			if alphaIdx >= futureIdx {
				t.Errorf("extras out of alpha order: %q", r.Detail)
			}
			return
		}
	}
	t.Fatal("severity probe missing")
}
