package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/client"
)

func findResult(t *testing.T, got []CheckResult, name string) CheckResult {
	t.Helper()
	for _, r := range got {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("result %q not found", name)
	return CheckResult{}
}

type fakeAugmentProber struct {
	resps map[string]*client.AugmentProbeResp
	err   error
}

func (f *fakeAugmentProber) AugmentProbe(_ context.Context, check string) (*client.AugmentProbeResp, error) {
	if f.err != nil {
		return nil, f.err
	}
	if r, ok := f.resps[check]; ok {
		return r, nil
	}
	return &client.AugmentProbeResp{Status: "ok", Detail: "stub-ok"}, nil
}

func TestRunAugmentChecksReturnsSix(t *testing.T) {
	t.Parallel()
	prober := &fakeAugmentProber{
		resps: map[string]*client.AugmentProbeResp{
			"endpoint-reachable":    {Status: "ok", Detail: "/v1/augment 204"},
			"budget-headroom":       {Status: "ok", Detail: "consumed=2400/25000 (10%)"},
			"cache-hit-rate":        {Status: "ok", Detail: "hit_rate=0.62 over 100 calls"},
			"latency-p50-p99":       {Status: "ok", Detail: "p50=620ms p99=1820ms (ceiling=2000ms)"},
			"five-lane-rrf-healthy": {Status: "ok", Detail: "all 5 lanes ok"},
			"privacy-filter-tested": {Status: "ok", Detail: "last_test=2026-05-09T14:00:00Z passed"},
		},
	}
	got := runAugmentChecksWith(context.Background(), prober)
	if len(got) != 6 {
		t.Fatalf("len=%d, want 6", len(got))
	}
	wantOrder := []string{
		"augment.endpoint-reachable",
		"augment.budget.headroom",
		"augment.cache.hit-rate",
		"augment.latency.p50-p99",
		"augment.5-lane-rrf.healthy",
		"augment.privacy-filter.tested",
	}
	for i, name := range wantOrder {
		if got[i].Name != name {
			t.Errorf("results[%d].Name=%q, want %q", i, got[i].Name, name)
		}
	}
}

func TestAugmentBudgetHeadroomFailHasHint(t *testing.T) {
	t.Parallel()
	prober := &fakeAugmentProber{
		resps: map[string]*client.AugmentProbeResp{
			"budget-headroom": {Status: "fail", Detail: "consumed=24500/25000 (98%)"},
		},
	}
	got := runAugmentChecksWith(context.Background(), prober)
	r := findResult(t, got, "augment.budget.headroom")
	if r.Status != "fail" {
		t.Fatalf("status=%q, want fail", r.Status)
	}
	if !strings.Contains(r.Hint, "tighten max_kg_tokens") && !strings.Contains(r.Hint, "doctrine") {
		t.Errorf("hint=%q, want includes doctrine guidance", r.Hint)
	}
}

func TestAugmentPrivacyFilterStaleWarn(t *testing.T) {
	t.Parallel()
	prober := &fakeAugmentProber{
		resps: map[string]*client.AugmentProbeResp{
			"privacy-filter-tested": {Status: "warn", Detail: "last_test 9 days ago"},
		},
	}
	got := runAugmentChecksWith(context.Background(), prober)
	r := findResult(t, got, "augment.privacy-filter.tested")
	if r.Status != "warn" {
		t.Fatalf("status=%q, want warn", r.Status)
	}
	if !strings.Contains(r.Hint, "go test -tags=adversarial") {
		t.Errorf("hint=%q, want includes adversarial test invocation", r.Hint)
	}
}

func TestRunAugmentChecksDaemonError(t *testing.T) {
	t.Parallel()
	prober := &fakeAugmentProber{err: errors.New("daemon dead")}
	got := runAugmentChecksWith(context.Background(), prober)
	if len(got) != 6 {
		t.Fatalf("len=%d, want 6", len(got))
	}
	for _, r := range got {
		if r.Status != "fail" {
			t.Errorf("check %q status=%q, want fail", r.Name, r.Status)
		}
	}
}

func TestDoctorAugmentCmdRegistered(t *testing.T) {
	t.Parallel()
	cmd := NewDoctorAugmentCmd()
	if cmd.Use != "augment" {
		t.Fatalf("Use=%q, want %q", cmd.Use, "augment")
	}
}
