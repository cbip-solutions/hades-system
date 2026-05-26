package cli

import (
	"context"
	"errors"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/client"
)

type fakeCoordinationProber struct {
	resps map[string]*client.CoordinationProbeResp
	err   error
}

func (f *fakeCoordinationProber) CoordinationProbe(_ context.Context, check string) (*client.CoordinationProbeResp, error) {
	if f.err != nil {
		return nil, f.err
	}
	if r, ok := f.resps[check]; ok {
		return r, nil
	}
	return &client.CoordinationProbeResp{Status: "ok", Detail: "stub-ok"}, nil
}

func TestRunCoordinationChecksReturnsOne(t *testing.T) {
	t.Parallel()
	prober := &fakeCoordinationProber{
		resps: map[string]*client.CoordinationProbeResp{
			"plan-9-d-substrate": {Status: "ok", Detail: "aggregator.go present"},
		},
	}
	got := runCoordinationChecksWith(context.Background(), prober)

	if len(got) != 1 {
		t.Fatalf("len=%d, want 1 (plan-1-h-prime.executed retired in v0.20.7 per inv-zen-290)", len(got))
	}
	wantOrder := []string{
		"plan-9-d.aggregator.db-substrate-available",
	}
	for i, name := range wantOrder {
		if got[i].Name != name {
			t.Errorf("results[%d].Name=%q, want %q", i, got[i].Name, name)
		}
	}
}

func TestRunCoordinationChecksDaemonError(t *testing.T) {
	t.Parallel()
	prober := &fakeCoordinationProber{err: errors.New("daemon dead")}
	got := runCoordinationChecksWith(context.Background(), prober)
	if len(got) != 1 {
		t.Fatalf("len=%d, want 1 (plan-1-h-prime.executed retired in v0.20.7 per inv-zen-290)", len(got))
	}
	for _, r := range got {
		if r.Status != "fail" {
			t.Errorf("check %q status=%q, want fail", r.Name, r.Status)
		}
	}
}

func TestDoctorCoordinationCmdRegistered(t *testing.T) {
	t.Parallel()
	cmd := NewDoctorCoordinationCmd()
	if cmd.Use != "coordination" {
		t.Fatalf("Use=%q, want %q", cmd.Use, "coordination")
	}
}
