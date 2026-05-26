package cli

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeSchedulerProber struct {
	queueDepth      int
	queueByProject  map[string]int
	missedFires     int
	missedByProject map[string]int
	wfqMaxPct       int
	wfqMaxAlias     string
	dispatcherOK    bool
	dispatcherErr   error
	statsErr        error
}

func (f *fakeSchedulerProber) QueueDepth(ctx context.Context) (total int, byProject map[string]int, err error) {
	return f.queueDepth, f.queueByProject, f.statsErr
}

func (f *fakeSchedulerProber) MissedFires24h(ctx context.Context) (total int, byProject map[string]int, err error) {
	return f.missedFires, f.missedByProject, f.statsErr
}

func (f *fakeSchedulerProber) WfqSaturation(ctx context.Context) (maxPct int, maxAlias string, err error) {
	return f.wfqMaxPct, f.wfqMaxAlias, f.statsErr
}

func (f *fakeSchedulerProber) DispatcherBound(ctx context.Context) error {
	if !f.dispatcherOK {
		if f.dispatcherErr != nil {
			return f.dispatcherErr
		}
		return errors.New("dispatcher unbound")
	}
	return nil
}

func TestRunSchedulerProbeAllOK(t *testing.T) {
	p := &fakeSchedulerProber{
		queueDepth:   2,
		wfqMaxPct:    40,
		wfqMaxAlias:  "internal-platform-x",
		dispatcherOK: true,
	}
	probes, err := RunSchedulerProbe(context.Background(), p)
	if err != nil {
		t.Fatalf("RunSchedulerProbe: %v", err)
	}
	if len(probes) != 4 {
		t.Fatalf("want 4 probes, got %d", len(probes))
	}
	for _, r := range probes {
		if r.Status != ProbeOK {
			t.Errorf("probe %s: status=%v, message=%q", r.Name, r.Status, r.Message)
		}
	}
}

func TestRunSchedulerProbeQueueWarn(t *testing.T) {
	p := &fakeSchedulerProber{
		queueDepth:     7,
		queueByProject: map[string]int{"internal-platform-x": 5, "zen-swarm": 2},
		dispatcherOK:   true,
	}
	probes, _ := RunSchedulerProbe(context.Background(), p)
	for _, r := range probes {
		if r.Name == "scheduler.queue.depth" {
			if r.Status != ProbeWarn {
				t.Errorf("queue.depth status=%v, want Warn", r.Status)
			}
			if !strings.Contains(r.Message, "7") {
				t.Errorf("message should include depth: %q", r.Message)
			}
			if !strings.Contains(r.Detail, "internal-platform-x: 5") {
				t.Errorf("detail should include per-project breakdown: %q", r.Detail)
			}
			return
		}
	}
	t.Fatal("queue.depth probe missing")
}

func TestRunSchedulerProbeQueueSaturatedFail(t *testing.T) {
	p := &fakeSchedulerProber{
		queueDepth:     12,
		queueByProject: map[string]int{"internal-platform-x": 8, "zen-swarm": 4},
		dispatcherOK:   true,
	}
	probes, _ := RunSchedulerProbe(context.Background(), p)
	for _, r := range probes {
		if r.Name == "scheduler.queue.depth" {
			if r.Status != ProbeFail {
				t.Errorf("queue.depth status=%v, want Fail (>=10)", r.Status)
			}
			if !strings.Contains(r.Hint, "WFQ") {
				t.Errorf("hint should mention WFQ: %q", r.Hint)
			}
			return
		}
	}
	t.Fatal("queue.depth probe missing")
}

func TestRunSchedulerProbeMissedFiresWarn(t *testing.T) {
	p := &fakeSchedulerProber{
		missedFires:     3,
		missedByProject: map[string]int{"internal-platform-x": 3},
		dispatcherOK:    true,
	}
	probes, _ := RunSchedulerProbe(context.Background(), p)
	for _, r := range probes {
		if r.Name == "scheduler.missed_fires.recent" {
			if r.Status != ProbeWarn {
				t.Errorf("missed_fires status=%v, want Warn (3 in 24h)", r.Status)
			}
			return
		}
	}
	t.Fatal("missed_fires probe missing")
}

func TestRunSchedulerProbeMissedFiresFail(t *testing.T) {
	p := &fakeSchedulerProber{
		missedFires:     8,
		missedByProject: map[string]int{"internal-platform-x": 8},
		dispatcherOK:    true,
	}
	probes, _ := RunSchedulerProbe(context.Background(), p)
	for _, r := range probes {
		if r.Name == "scheduler.missed_fires.recent" {
			if r.Status != ProbeFail {
				t.Errorf("missed_fires status=%v, want Fail (>=6)", r.Status)
			}
			if !strings.Contains(r.Hint, "schedule history") {
				t.Errorf("hint should suggest schedule history: %q", r.Hint)
			}
			return
		}
	}
	t.Fatal("missed_fires probe missing")
}

func TestRunSchedulerProbeWfqWarn(t *testing.T) {
	p := &fakeSchedulerProber{
		wfqMaxPct:    85,
		wfqMaxAlias:  "internal-platform-x",
		dispatcherOK: true,
	}
	probes, _ := RunSchedulerProbe(context.Background(), p)
	for _, r := range probes {
		if r.Name == "scheduler.wfq.saturation" {
			if r.Status != ProbeWarn {
				t.Errorf("wfq.saturation status=%v, want Warn (85)", r.Status)
			}
			return
		}
	}
	t.Fatal("wfq.saturation probe missing")
}

func TestRunSchedulerProbeWfqSaturated(t *testing.T) {
	p := &fakeSchedulerProber{
		wfqMaxPct:    97,
		wfqMaxAlias:  "internal-platform-x",
		dispatcherOK: true,
	}
	probes, _ := RunSchedulerProbe(context.Background(), p)
	for _, r := range probes {
		if r.Name == "scheduler.wfq.saturation" {
			if r.Status != ProbeFail {
				t.Errorf("wfq.saturation status=%v, want Fail", r.Status)
			}
			if !strings.Contains(r.Message, "internal-platform-x") {
				t.Errorf("message should name the saturated project: %q", r.Message)
			}
			return
		}
	}
	t.Fatal("wfq.saturation probe missing")
}

func TestRunSchedulerProbeWfqEmpty(t *testing.T) {
	p := &fakeSchedulerProber{
		dispatcherOK: true,
	}
	probes, _ := RunSchedulerProbe(context.Background(), p)
	for _, r := range probes {
		if r.Name == "scheduler.wfq.saturation" {
			if r.Status != ProbeOK {
				t.Errorf("wfq.saturation status=%v, want OK (no active queues)", r.Status)
			}
			if !strings.Contains(r.Message, "no active") {
				t.Errorf("message should describe empty: %q", r.Message)
			}
			return
		}
	}
	t.Fatal("wfq.saturation probe missing")
}

func TestRunSchedulerProbeDispatcherUnbound(t *testing.T) {
	p := &fakeSchedulerProber{
		dispatcherOK: false,
	}
	probes, _ := RunSchedulerProbe(context.Background(), p)
	for _, r := range probes {
		if r.Name == "scheduler.dispatcher.bound" {
			if r.Status != ProbeFail {
				t.Errorf("dispatcher.bound status=%v, want Fail", r.Status)
			}
			if !strings.Contains(r.Hint, "inv-zen-080") && !strings.Contains(r.Hint, "Plan 3") {
				t.Errorf("hint should reference inv-zen-080 / Plan 3: %q", r.Hint)
			}
			return
		}
	}
	t.Fatal("dispatcher.bound probe missing")
}

func TestRunSchedulerProbeProberError(t *testing.T) {
	p := &fakeSchedulerProber{
		statsErr:     errors.New("daemon.db locked"),
		dispatcherOK: true,
	}
	probes, _ := RunSchedulerProbe(context.Background(), p)
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

func TestRunSchedulerProbeFormatProjectMapStable(t *testing.T) {

	p := &fakeSchedulerProber{
		queueDepth:     8,
		queueByProject: map[string]int{"a": 1, "b": 4, "c": 1, "d": 2},
		dispatcherOK:   true,
	}
	probes, _ := RunSchedulerProbe(context.Background(), p)
	for _, r := range probes {
		if r.Name == "scheduler.queue.depth" {

			want := "b: 4\nd: 2\na: 1\nc: 1"
			if r.Detail != want {
				t.Errorf("detail = %q, want %q", r.Detail, want)
			}
			return
		}
	}
	t.Fatal("queue.depth probe missing")
}
